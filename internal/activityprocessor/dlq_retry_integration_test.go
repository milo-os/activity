package activityprocessor

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"go.miloapis.com/activity/internal/processor"
)

const (
	testDLQStream   = "ACTIVITY_DEAD_LETTER"
	testAuditStream = "AUDIT_EVENTS"
	testDLQPrefix   = "activity.dlq"
	testDLQSubject  = "activity.dlq.audit.resourcemanager.miloapis.com.Project"
)

// startJetStream boots an in-process NATS server with JetStream enabled and
// returns a connected JetStream context plus the raw connection (for opening a
// second connection to the same server).
func startJetStream(t *testing.T) (nats.JetStreamContext, *nats.Conn) {
	t.Helper()

	srv, err := natsserver.NewServer(&natsserver.Options{
		Port:      -1,
		JetStream: true,
		StoreDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new nats server: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	t.Cleanup(srv.Shutdown)

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(nc.Close)

	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("jetstream context: %v", err)
	}
	return js, nc
}

// setupDLQ provisions the workqueue DLQ stream, the audit stream that captures
// republished retries, and the shared durable consumer — mirroring
// dlq-stream.yaml, audit-stream.yaml, and dlq-retry-consumer.yaml.
func setupDLQ(t *testing.T, js nats.JetStreamContext) {
	t.Helper()

	if _, err := js.AddStream(&nats.StreamConfig{
		Name:      testDLQStream,
		Subjects:  []string{"activity.dlq.>"},
		Retention: nats.WorkQueuePolicy,
		Storage:   nats.FileStorage,
	}); err != nil {
		t.Fatalf("add dlq stream: %v", err)
	}

	if _, err := js.AddStream(&nats.StreamConfig{
		Name:      testAuditStream,
		Subjects:  []string{"audit.k8s.>"},
		Retention: nats.LimitsPolicy,
		Storage:   nats.FileStorage,
	}); err != nil {
		t.Fatalf("add audit stream: %v", err)
	}

	if _, err := js.AddConsumer(testDLQStream, &nats.ConsumerConfig{
		Durable:       dlqRetryDurable,
		FilterSubject: "activity.dlq.>",
		AckPolicy:     nats.AckExplicitPolicy,
		AckWait:       60 * time.Second,
		MaxAckPending: 100,
		MaxDeliver:    -1,
	}); err != nil {
		t.Fatalf("add durable consumer: %v", err)
	}
}

func newTestController(js nats.JetStreamContext) *DLQRetryController {
	cfg := DefaultDLQRetryConfig()
	cfg.BatchSize = 100
	return NewDLQRetryController(js, cfg, testAuditStream, "EVENTS", testDLQStream, testDLQPrefix, nil)
}

func publishDLQEvent(t *testing.T, js nats.JetStreamContext, name string, nextRetry *metav1.Time) {
	t.Helper()
	ev := processor.DeadLetterEvent{
		Type:            processor.EventTypeAudit,
		OriginalPayload: json.RawMessage(fmt.Sprintf(`{"name":%q}`, name)),
		PolicyName:      "resourcemanager.miloapis.com-project",
		ErrorType:       processor.ErrorTypeCELSummary,
		Timestamp:       metav1.Now(),
		Resource: &processor.DeadLetterResource{
			APIGroup: "resourcemanager.miloapis.com",
			Kind:     "Project",
			Name:     name,
		},
		NextRetryAfter: nextRetry,
	}
	data, err := json.Marshal(&ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if _, err := js.Publish(testDLQSubject, data); err != nil {
		t.Fatalf("publish dlq event: %v", err)
	}
}

// TestDLQRetryDrainsWorkqueueStream is the primary regression for #216: the
// retry controller must bind a consumer on a workqueue-retention DLQ stream and
// actually drain it. The old per-batch ephemeral consumer failed to attach here.
func TestDLQRetryDrainsWorkqueueStream(t *testing.T) {
	js, _ := startJetStream(t)
	setupDLQ(t, js)

	const n = 5
	for i := 0; i < n; i++ {
		publishDLQEvent(t, js, fmt.Sprintf("project-%d", i), nil)
	}

	newTestController(js).periodicRetry(context.Background())

	dlq, err := js.StreamInfo(testDLQStream)
	if err != nil {
		t.Fatalf("dlq stream info: %v", err)
	}
	if dlq.State.Msgs != 0 {
		t.Fatalf("DLQ did not drain: %d messages remain", dlq.State.Msgs)
	}

	audit, err := js.StreamInfo(testAuditStream)
	if err != nil {
		t.Fatalf("audit stream info: %v", err)
	}
	if audit.State.Msgs != n {
		t.Fatalf("expected %d republished events, got %d", n, audit.State.Msgs)
	}
}

// TestDLQRetryTwoReplicasNoConsumerCollision reproduces the exact failure in the
// issue: two processor replicas binding the retry consumer on a workqueue stream.
// With overlapping ephemeral consumers this failed with
// "filtered consumer not unique on workqueue stream"; both must now bind cleanly.
func TestDLQRetryTwoReplicasNoConsumerCollision(t *testing.T) {
	js1, nc := startJetStream(t)
	setupDLQ(t, js1)

	nc2, err := nats.Connect(nc.ConnectedUrl())
	if err != nil {
		t.Fatalf("second connection: %v", err)
	}
	t.Cleanup(nc2.Close)
	js2, err := nc2.JetStream()
	if err != nil {
		t.Fatalf("second jetstream context: %v", err)
	}

	if _, err := newTestController(js1).bindRetryConsumer(); err != nil {
		t.Fatalf("replica 1 failed to bind DLQ consumer: %v", err)
	}
	if _, err := newTestController(js2).bindRetryConsumer(); err != nil {
		t.Fatalf("replica 2 failed to bind DLQ consumer: %v", err)
	}
}

// TestDLQRetryDefersBackedOffEvents guards the totalProcessed spin: a backed-off
// event must not be redelivered within the same run. A plain Nak redelivered it
// immediately, so processRetryBatch kept re-fetching the same event and the run
// spun until its deadline. NakWithDelay defers it, so the second batch is empty.
func TestDLQRetryDefersBackedOffEvents(t *testing.T) {
	js, _ := startJetStream(t)
	setupDLQ(t, js)

	future := metav1.NewTime(time.Now().Add(time.Hour))
	publishDLQEvent(t, js, "backed-off", &future)

	c := newTestController(js)
	ctx := context.Background()

	processed, succeeded, _ := c.processRetryBatch(ctx, "periodic", nil)
	if processed != 1 || succeeded != 0 {
		t.Fatalf("first batch: processed=%d succeeded=%d, want 1/0", processed, succeeded)
	}

	processed2, _, _ := c.processRetryBatch(ctx, "periodic", nil)
	if processed2 != 0 {
		t.Fatalf("backed-off event redelivered immediately: processed=%d (spin regression)", processed2)
	}
}
