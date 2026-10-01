package edgeaudit

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"go.miloapis.com/activity/internal/storage"
)

const (
	clickHouseImage = "clickhouse/clickhouse-server:25.3"
	natsImage       = "nats:2.10-alpine"
)

// requireDocker skips the suite when no container runtime is available, so the
// package stays runnable on a laptop without one.
func requireDocker(t *testing.T) {
	t.Helper()
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("docker is not available; skipping edge audit e2e")
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed reserving a port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func runContainer(t *testing.T, name string, args ...string) {
	t.Helper()

	full := append([]string{"run", "-d", "--rm", "--name", name}, args...)
	output, err := exec.Command("docker", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("failed starting %s: %v\n%s", name, err, output)
	}

	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", name).Run()
	})
}

// startClickHouse boots a single-node ClickHouse, applies the audit_logs
// schema, and returns storage wired to it.
func startClickHouse(t *testing.T) *storage.ClickHouseStorage {
	t.Helper()

	port := freePort(t)
	name := fmt.Sprintf("activity-e2e-clickhouse-%d", port)

	runContainer(t, name,
		"-p", fmt.Sprintf("127.0.0.1:%d:9000", port),
		"-e", "CLICKHOUSE_SKIP_USER_SETUP=1",
		clickHouseImage,
	)

	address := fmt.Sprintf("127.0.0.1:%d", port)

	var store *storage.ClickHouseStorage
	waitFor(t, 120*time.Second, "clickhouse", func() error {
		if err := applySchema(name); err != nil {
			return err
		}

		connected, err := storage.NewClickHouseStorage(storage.ClickHouseConfig{
			Address:        address,
			Database:       "audit",
			Username:       "default",
			MaxQueryWindow: 30 * 24 * time.Hour,
			MaxPageSize:    1000,
		})
		if err != nil {
			return err
		}
		store = connected
		return nil
	})

	t.Cleanup(func() { _ = store.Close() })

	return store
}

func applySchema(container string) error {
	schema, err := readTestdata("schema.sql")
	if err != nil {
		return err
	}

	cmd := exec.Command("docker", "exec", "-i", container, "clickhouse-client", "--multiquery")
	cmd.Stdin = strings.NewReader(string(schema))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed applying schema: %w\n%s", err, output)
	}
	return nil
}

// startNATS boots a JetStream-enabled NATS and creates the audit stream the
// core pipeline uses, so the emitter publishes exactly as it does in
// production.
func startNATS(t *testing.T) (string, nats.JetStreamContext) {
	t.Helper()

	port := freePort(t)
	name := fmt.Sprintf("activity-e2e-nats-%d", port)

	runContainer(t, name,
		"-p", fmt.Sprintf("127.0.0.1:%d:4222", port),
		natsImage, "-js",
	)

	url := fmt.Sprintf("nats://127.0.0.1:%d", port)

	var js nats.JetStreamContext
	waitFor(t, 60*time.Second, "nats", func() error {
		conn, err := nats.Connect(url, nats.Timeout(2*time.Second))
		if err != nil {
			return err
		}

		context, err := conn.JetStream()
		if err != nil {
			conn.Close()
			return err
		}

		if _, err := context.AddStream(&nats.StreamConfig{
			Name:      "AUDIT_EVENTS",
			Subjects:  []string{"audit.k8s.>"},
			Retention: nats.LimitsPolicy,
		}); err != nil {
			conn.Close()
			return err
		}

		js = context
		t.Cleanup(conn.Close)
		return nil
	})

	return url, js
}

func waitFor(t *testing.T, timeout time.Duration, what string, attempt func() error) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		if last = attempt(); last == nil {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("%s was not ready within %s: %v", what, timeout, last)
}

// drainToClickHouse plays the role Vector plays in production: it consumes
// published audit events and inserts the raw JSON into audit_logs.
func drainToClickHouse(t *testing.T, js nats.JetStreamContext, store *storage.ClickHouseStorage, expected int) {
	t.Helper()

	subscription, err := js.PullSubscribe("audit.k8s.>", "e2e-clickhouse-ingest")
	if err != nil {
		t.Fatalf("failed subscribing: %v", err)
	}
	defer subscription.Unsubscribe()

	batch, err := store.Conn().PrepareBatch(context.Background(), "INSERT INTO audit.audit_logs (event_json)")
	if err != nil {
		t.Fatalf("failed preparing batch: %v", err)
	}

	drained := 0
	deadline := time.Now().Add(30 * time.Second)
	for drained < expected && time.Now().Before(deadline) {
		messages, err := subscription.Fetch(expected-drained, nats.MaxWait(2*time.Second))
		if err != nil && !isTimeout(err) {
			t.Fatalf("failed fetching: %v", err)
		}
		for _, message := range messages {
			if err := batch.Append(string(message.Data)); err != nil {
				t.Fatalf("failed appending row: %v", err)
			}
			_ = message.Ack()
			drained++
		}
	}

	if drained != expected {
		t.Fatalf("drained %d events from NATS, want %d", drained, expected)
	}

	if err := batch.Send(); err != nil {
		t.Fatalf("failed inserting rows: %v", err)
	}
}

func isTimeout(err error) bool {
	return err != nil && strings.Contains(err.Error(), "timeout")
}
