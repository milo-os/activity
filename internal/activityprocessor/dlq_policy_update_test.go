package activityprocessor

import (
	"context"
	"encoding/json"
	"fmt"
	"go.miloapis.com/activity/internal/processor"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
	"time"
)

func TestDLQPolicyUpdateWakesDelayedEvent(t *testing.T) {
	js, _ := startJetStream(t)
	setupDLQ(t, js)
	future := metav1.NewTime(time.Now().Add(24 * time.Hour))
	publishDLQEvent(t, js, "delayed", &future)
	c := newTestController(js)
	c.config.BatchSize = 1
	c.config.Interval = 150 * time.Millisecond
	c.policyGeneration = func(string) int64 { return 0 }
	if p, s, f := c.processRetryBatch(context.Background(), "periodic", nil); p != 1 || s != 0 || f != 0 {
		t.Fatalf("defer=%d/%d/%d", p, s, f)
	}
	// A fresh replica with the updated cache must recover the message without
	// receiving the informer update that originally triggered a policy scan.
	fresh := newTestController(js)
	fresh.config.BatchSize = 1
	fresh.policyGeneration = func(string) int64 { return 1 }
	fresh.evaluator = func(context.Context, *processor.DeadLetterEvent) RetryOutcome { return RetryOutcome{Resolved: true} }
	started := time.Now()
	if p, s, f := fresh.processRetryBatch(context.Background(), "periodic", nil); p != 1 || s != 1 || f != 0 {
		t.Fatalf("after policy fix=%d/%d/%d", p, s, f)
	}
	if time.Since(started) > 4*time.Second {
		t.Fatal("policy fix remained hidden by broker backoff")
	}
	// ACK is asynchronous; observe the eventual broker state rather than
	// assuming the message disappears before the retry method returns.
	info, err := js.StreamInfo(testDLQStream)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); info.State.Msgs != 0 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
		info, err = js.StreamInfo(testDLQStream)
		if err != nil {
			t.Fatal(err)
		}
	}
	if info.State.Msgs != 0 {
		t.Fatalf("DLQ did not drain: %d messages remain", info.State.Msgs)
	}
}

func TestDLQNewPolicyFailureResumesBackoff(t *testing.T) {
	js, _ := startJetStream(t)
	setupDLQ(t, js)
	future := metav1.NewTime(time.Now().Add(24 * time.Hour))
	publishDLQEvent(t, js, "still-fails", &future)
	c := newTestController(js)
	c.config.BatchSize = 1
	c.policyGeneration = func(string) int64 { return 2 }
	c.evaluator = func(context.Context, *processor.DeadLetterEvent) RetryOutcome {
		return RetryOutcome{Err: fmt.Errorf("still invalid"), ErrorType: processor.ErrorTypeCELSummary}
	}
	if p, s, f := c.processRetryBatch(context.Background(), "periodic", nil); p != 1 || s != 0 || f != 1 {
		t.Fatalf("failed retry=%d/%d/%d", p, s, f)
	}
	info, err := js.StreamInfo(testDLQStream)
	if err != nil {
		t.Fatal(err)
	}
	// The retry uses asynchronous ACKs; wait for the server to remove the old
	// entry before checking that only its updated replacement remains.
	for deadline := time.Now().Add(2 * time.Second); info.State.Msgs != 1 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
		info, err = js.StreamInfo(testDLQStream)
		if err != nil {
			t.Fatal(err)
		}
	}
	if info.State.Msgs != 1 {
		t.Fatalf("failed event lost or duplicated: %d", info.State.Msgs)
	}
	raw, err := js.GetMsg(testDLQStream, info.State.LastSeq)
	if err != nil {
		t.Fatal(err)
	}
	var event processor.DeadLetterEvent
	if err = json.Unmarshal(raw.Data, &event); err != nil {
		t.Fatal(err)
	}
	if event.PolicyVersion != 2 || event.RetryCount != 1 || event.NextRetryAfter == nil {
		t.Fatalf("retry metadata=%+v", event)
	}
	if c.isEligibleForRetry(&event, time.Now()) {
		t.Fatal("same failed policy bypasses backoff")
	}
	c.policyGeneration = func(string) int64 { return 3 }
	if !c.isEligibleForRetry(&event, time.Now()) {
		t.Fatal("next policy fix did not bypass backoff")
	}
}

func TestActivePolicyGeneration(t *testing.T) {
	cache := NewPolicyCache()
	p := &v1alpha1.ActivityPolicy{ObjectMeta: metav1.ObjectMeta{Name: "test", Generation: 2}}
	if err := cache.Add(p, "widgets"); err != nil {
		t.Fatal(err)
	}
	if cache.Generation("test") != 2 || cache.Generation("other") != 0 {
		t.Fatal("incorrect policy generation")
	}
	updated := p.DeepCopy()
	updated.Generation = 3
	if err := cache.Update(p, updated, "widgets", "widgets"); err != nil {
		t.Fatal(err)
	}
	if cache.Generation("test") != 3 {
		t.Fatal("stale generation")
	}
	cache.Remove(updated, "widgets")
	if cache.Generation("test") != 0 {
		t.Fatal("deleted policy still present")
	}
}

func TestDLQPolicyChangeDoesNotWakeOtherPolicies(t *testing.T) {
	c := &DLQRetryController{policyGeneration: func(name string) int64 {
		if name == "updated" {
			return 3
		}
		return 0
	}}
	future := metav1.NewTime(time.Now().Add(24 * time.Hour))
	for _, tt := range []struct {
		name     string
		version  int64
		eligible bool
	}{
		{"updated", 2, true}, {"updated", 3, false}, {"updated", 4, false}, {"unchanged", 0, false},
	} {
		event := &processor.DeadLetterEvent{PolicyName: tt.name, PolicyVersion: tt.version, NextRetryAfter: &future}
		if got := c.isEligibleForRetry(event, time.Now()); got != tt.eligible {
			t.Errorf("%s version %d eligible=%v", tt.name, tt.version, got)
		}
	}
}
