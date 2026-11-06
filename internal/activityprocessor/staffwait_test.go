package activityprocessor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/rest"
)

// The processor waits for the staff indexes indefinitely: a never-syncing
// index blocks until the context ends.
func TestWaitForStaffIndexes_BlocksUntilSyncedOrCancelled(t *testing.T) {
	orig := staffIndexBackoff
	staffIndexBackoff.initial, staffIndexBackoff.max = 10*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { staffIndexBackoff = orig })

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := waitForStaffIndexes(ctx, NewUnavailableStaffIndex(nil))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the context to end the wait", err)
	}
	if time.Since(start) < 250*time.Millisecond {
		t.Errorf("returned after %s, before the context ended", time.Since(start))
	}

	// The domain-only mode never waits.
	if err := waitForStaffIndexes(context.Background(), NewDomainOnlyStaffIndex(nil)); err != nil {
		t.Fatalf("domain-only: %v", err)
	}

	client := newFakeStaffClient()
	x := startStaffIndex(t, client, defaultFatalGrace)
	if err := waitForStaffIndexes(context.Background(), x); err != nil {
		t.Fatalf("synced index: %v", err)
	}
}

// In the required mode a staff index that cannot be built is an error (the
// processor exits), not a silent never-syncing index.
func TestNewStaffIndex_ErrorReturnsNoIndex(t *testing.T) {
	bad := &rest.Config{Host: "https://milo.invalid", TLSClientConfig: rest.TLSClientConfig{CAFile: "/nonexistent/ca.crt"}}
	idx, err := NewStaffIndex(bad, nil)
	if err == nil || idx != nil {
		t.Fatalf("NewStaffIndex = (%v, %v), want (nil, error)", idx, err)
	}
}

func TestParseStaffRedactionMode(t *testing.T) {
	for in, want := range map[string]string{"": "required", "required": "required", "domain-only": "domain-only"} {
		if got, err := ParseStaffRedactionMode(in); err != nil || got != want {
			t.Errorf("ParseStaffRedactionMode(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseStaffRedactionMode("off"); err == nil {
		t.Error("invalid mode accepted")
	}
}

// The API server's index metrics use its own prefix, so the processor keeps
// only activity_processor_* families.
func TestUseIndexMetricsNamespace(t *testing.T) {
	orig := currentIndexMetrics()
	t.Cleanup(func() {
		indexMetricsMu.Lock()
		activeIndexMetrics = orig
		indexMetricsMu.Unlock()
	})
	reg := prometheus.NewPedanticRegistry()
	for _, c := range UseIndexMetricsNamespace("activity_apiserver") {
		reg.MustRegister(c)
	}
	staffGauges("projects").synced.Set(1)
	currentIndexMetrics().userSynced.Set(1)
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range families {
		names[f.GetName()] = true
	}
	for _, want := range []string{"activity_apiserver_staff_index_synced", "activity_apiserver_user_index_synced"} {
		if !names[want] {
			t.Errorf("missing %s in %v", want, names)
		}
	}
}
