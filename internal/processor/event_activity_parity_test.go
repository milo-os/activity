package processor

import (
	"reflect"
	"testing"
)

// TestEventBuildersParity asserts EventProcessor.buildActivity (the live path)
// and ActivityBuilder.BuildFromEvent (PolicyPreview/reindex) produce identical
// output for the same legacy-format event.
func TestEventBuildersParity(t *testing.T) {
	event := map[string]any{
		"metadata": map[string]any{
			"uid": "event-legacy-123",
			// Differs from lastTimestamp so a wrong fallback choice is visible.
			"creationTimestamp": "2024-01-15T10:30:00Z",
		},
		// No eventTime; the fallback chain must pick this over creationTimestamp.
		"lastTimestamp": "2024-01-14T09:00:00Z",
		"reason":        "Scheduled",
		"message":       "Successfully assigned default/my-pod to node-1",
		// Legacy actor field; no reportingController.
		"source": map[string]any{
			"component": "kubelet",
		},
		// Legacy subject field; no regarding.
		"involvedObject": map[string]any{
			"kind":       "Pod",
			"name":       "my-pod",
			"namespace":  "default",
			"uid":        "pod-456",
			"apiVersion": "v1",
		},
	}

	involvedObject := ResolveInvolvedObject(event)
	if involvedObject == nil {
		t.Fatal("ResolveInvolvedObject() = nil, want the legacy involvedObject map")
	}

	matched := &MatchedPolicy{
		PolicyName: "core-pods",
		Generation: 1,
		APIGroup:   "",
		Kind:       "Pod",
		Summary:    "Pod my-pod was scheduled",
	}

	// Live path: EventProcessor.buildActivity.
	p := &EventProcessor{}
	liveActivity := p.buildActivity(event, matched, involvedObject, matched.Summary, nil)

	// PolicyPreview/reindex path: ActivityBuilder.BuildFromEvent.
	builder := &ActivityBuilder{APIGroup: matched.APIGroup, Kind: matched.Kind}
	previewActivity, err := builder.BuildFromEvent(event, matched.Summary, nil, nil)
	if err != nil {
		t.Fatalf("BuildFromEvent() error = %v", err)
	}

	// Confirm the fallbacks actually fired, not just that both sides agree.

	if liveActivity.Spec.Resource.Name != "my-pod" ||
		liveActivity.Spec.Resource.Namespace != "default" ||
		liveActivity.Spec.Resource.UID != "pod-456" ||
		liveActivity.Spec.Resource.APIVersion != "v1" {
		t.Fatalf("live Resource not populated from legacy involvedObject fallback: %+v", liveActivity.Spec.Resource)
	}

	if liveActivity.Spec.Actor.Type != ActorTypeController || liveActivity.Spec.Actor.Name != "kubelet" {
		t.Fatalf("live Actor not resolved via source.component fallback: %+v", liveActivity.Spec.Actor)
	}

	wantTimestamp := "2024-01-14T09:00:00Z" // lastTimestamp, not metadata.creationTimestamp
	if got := liveActivity.CreationTimestamp.UTC().Format("2006-01-02T15:04:05Z"); got != wantTimestamp {
		t.Fatalf("live CreationTimestamp not resolved via lastTimestamp fallback: got %q, want %q", got, wantTimestamp)
	}

	// The two paths must now agree exactly.
	if !previewActivity.CreationTimestamp.Time.Equal(liveActivity.CreationTimestamp.Time) {
		t.Errorf("CreationTimestamp mismatch between buildActivity and BuildFromEvent: live=%v preview=%v",
			liveActivity.CreationTimestamp.Time, previewActivity.CreationTimestamp.Time)
	}

	if previewActivity.Name != liveActivity.Name {
		t.Errorf("Name mismatch between buildActivity and BuildFromEvent: live=%q preview=%q",
			liveActivity.Name, previewActivity.Name)
	}

	if previewActivity.Spec.Resource != liveActivity.Spec.Resource {
		t.Errorf("Resource mismatch between buildActivity and BuildFromEvent:\n live:    %+v\n preview: %+v",
			liveActivity.Spec.Resource, previewActivity.Spec.Resource)
	}

	if previewActivity.Spec.Actor != liveActivity.Spec.Actor {
		t.Errorf("Actor mismatch between buildActivity and BuildFromEvent:\n live:    %+v\n preview: %+v",
			liveActivity.Spec.Actor, previewActivity.Spec.Actor)
	}

	if !reflect.DeepEqual(previewActivity.Labels, liveActivity.Labels) {
		t.Errorf("Labels mismatch between buildActivity and BuildFromEvent:\n live:    %+v\n preview: %+v",
			liveActivity.Labels, previewActivity.Labels)
	}

	// BuildFromEvent previously omitted this label entirely.
	if got := previewActivity.Labels["activity.miloapis.com/event-reason"]; got != "Scheduled" {
		t.Errorf("preview activity missing event-reason label: got %q, want %q", got, "Scheduled")
	}

	if liveActivity.Spec.ChangeSource != previewActivity.Spec.ChangeSource {
		t.Errorf("ChangeSource mismatch: live=%q preview=%q", liveActivity.Spec.ChangeSource, previewActivity.Spec.ChangeSource)
	}
}

// TestActivityBuilderBuildFromEventUsesControllerActorType asserts
// BuildFromEvent uses ActorTypeController, matching the live path (it
// previously used ActorTypeSystem).
func TestActivityBuilderBuildFromEventUsesControllerActorType(t *testing.T) {
	event := map[string]any{
		"reportingController": "deployment-controller",
		"regarding": map[string]any{
			"kind": "Deployment",
			"name": "my-deployment",
		},
	}

	builder := &ActivityBuilder{APIGroup: "apps", Kind: "Deployment"}
	activity, err := builder.BuildFromEvent(event, "summary", nil, nil)
	if err != nil {
		t.Fatalf("BuildFromEvent() error = %v", err)
	}

	if activity.Spec.Actor.Type != ActorTypeController {
		t.Errorf("Actor.Type = %q, want %q", activity.Spec.Actor.Type, ActorTypeController)
	}
	if activity.Spec.Actor.Name != "deployment-controller" {
		t.Errorf("Actor.Name = %q, want %q", activity.Spec.Actor.Name, "deployment-controller")
	}
}

// TestActivityBuilderBuildFromEventTimestampFallbackChain asserts
// BuildFromEvent falls back to lastTimestamp (it previously skipped straight
// to creationTimestamp).
func TestActivityBuilderBuildFromEventTimestampFallbackChain(t *testing.T) {
	event := map[string]any{
		"lastTimestamp": "2024-03-01T12:00:00Z",
		"regarding": map[string]any{
			"kind": "Pod",
			"name": "my-pod",
		},
	}

	builder := &ActivityBuilder{APIGroup: "", Kind: "Pod"}
	activity, err := builder.BuildFromEvent(event, "summary", nil, nil)
	if err != nil {
		t.Fatalf("BuildFromEvent() error = %v", err)
	}

	want := "2024-03-01T12:00:00Z"
	got := activity.CreationTimestamp.UTC().Format("2006-01-02T15:04:05Z")
	if got != want {
		t.Errorf("CreationTimestamp = %q, want %q (should fall back to lastTimestamp)", got, want)
	}
}
