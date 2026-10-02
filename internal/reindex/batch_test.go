package reindex

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"go.miloapis.com/activity/internal/activityprocessor"
	"go.miloapis.com/activity/internal/processor"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// newTestReindexerForPodEvents builds a *Reindexer whose PolicyCache matches
// any core/v1 Pod event with reason "SomeReason".
func newTestReindexerForPodEvents(t *testing.T) *Reindexer {
	t.Helper()

	policy := &v1alpha1.ActivityPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "core-pods"},
		Spec: v1alpha1.ActivityPolicySpec{
			Resource: v1alpha1.ActivityPolicyResource{
				APIGroup: "",
				Kind:     "Pod",
			},
			EventRules: []v1alpha1.ActivityPolicyRule{
				{
					Name:    "rule-pod-events",
					Match:   `event.reason == "SomeReason"`,
					Summary: `Pod event occurred`,
				},
			},
		},
	}

	cache := activityprocessor.NewPolicyCache()
	if err := cache.Add(policy, "pods"); err != nil {
		t.Fatalf("failed to add policy to cache: %v", err)
	}

	fakeKindResolver := processor.KindResolver(func(apiGroup, resource string) (string, error) {
		return "Pod", nil
	})

	return &Reindexer{
		policyCache:  cache,
		kindResolver: fakeKindResolver,
	}
}

// TestEvaluateEventBatch_LegacyInvolvedObject asserts evaluateEventBatch
// resolves the legacy "involvedObject" field (it previously only checked
// "regarding", producing zero activities for this input).
func TestEvaluateEventBatch_LegacyInvolvedObject(t *testing.T) {
	r := newTestReindexerForPodEvents(t)

	batch := []map[string]interface{}{
		{
			"metadata": map[string]interface{}{"uid": "test-uid-1"},
			"reason":   "SomeReason",
			"involvedObject": map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Pod",
				"namespace":  "default",
				"name":       "my-pod",
				"uid":        "pod-uid-1",
			},
		},
	}

	activities, err := r.evaluateEventBatch(context.Background(), batch)
	if err != nil {
		t.Fatalf("evaluateEventBatch() error = %v", err)
	}

	if len(activities) != 1 {
		t.Fatalf("len(activities) = %d, want 1", len(activities))
	}

	got := activities[0].Spec.Resource
	if got == (v1alpha1.ActivityResource{}) {
		t.Fatalf("Spec.Resource is empty; want it populated from legacy involvedObject fields")
	}
	if got.Namespace != "default" {
		t.Errorf("Resource.Namespace = %q, want %q", got.Namespace, "default")
	}
	if got.Name != "my-pod" {
		t.Errorf("Resource.Name = %q, want %q", got.Name, "my-pod")
	}
	if got.UID != "pod-uid-1" {
		t.Errorf("Resource.UID = %q, want %q", got.UID, "pod-uid-1")
	}
	if got.APIVersion != "v1" {
		t.Errorf("Resource.APIVersion = %q, want %q", got.APIVersion, "v1")
	}
	if got.Kind != "Pod" {
		t.Errorf("Resource.Kind = %q, want %q", got.Kind, "Pod")
	}
}

// TestEvaluateEventBatch_ModernRegarding confirms the "regarding" path still
// works alongside the legacy fallback.
func TestEvaluateEventBatch_ModernRegarding(t *testing.T) {
	r := newTestReindexerForPodEvents(t)

	batch := []map[string]interface{}{
		{
			"metadata": map[string]interface{}{"uid": "test-uid-2"},
			"reason":   "SomeReason",
			"regarding": map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Pod",
				"namespace":  "default",
				"name":       "my-pod-2",
				"uid":        "pod-uid-2",
			},
		},
	}

	activities, err := r.evaluateEventBatch(context.Background(), batch)
	if err != nil {
		t.Fatalf("evaluateEventBatch() error = %v", err)
	}

	if len(activities) != 1 {
		t.Fatalf("len(activities) = %d, want 1", len(activities))
	}

	got := activities[0].Spec.Resource
	if got == (v1alpha1.ActivityResource{}) {
		t.Fatalf("Spec.Resource is empty; want it populated from regarding fields")
	}
	if got.Namespace != "default" {
		t.Errorf("Resource.Namespace = %q, want %q", got.Namespace, "default")
	}
	if got.Name != "my-pod-2" {
		t.Errorf("Resource.Name = %q, want %q", got.Name, "my-pod-2")
	}
	if got.UID != "pod-uid-2" {
		t.Errorf("Resource.UID = %q, want %q", got.UID, "pod-uid-2")
	}
	if got.APIVersion != "v1" {
		t.Errorf("Resource.APIVersion = %q, want %q", got.APIVersion, "v1")
	}
	if got.Kind != "Pod" {
		t.Errorf("Resource.Kind = %q, want %q", got.Kind, "Pod")
	}
}
