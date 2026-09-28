package policy

import (
	"context"
	"go.miloapis.com/activity/pkg/apis/activity"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestPolicyGeneration(t *testing.T) {
	strategy := NewStrategy(nil)
	created := &activity.ActivityPolicy{ObjectMeta: metav1.ObjectMeta{Generation: 99}}
	strategy.PrepareForCreate(context.Background(), created)
	if created.Generation != 1 {
		t.Fatalf("create generation=%d", created.Generation)
	}
	for _, start := range []int64{0, 1, 7} {
		old := &activity.ActivityPolicy{ObjectMeta: metav1.ObjectMeta{Generation: start}, Spec: activity.ActivityPolicySpec{Resource: activity.ActivityPolicyResource{APIGroup: "example.com", Kind: "Widget"}}}
		metadata := old.DeepCopy()
		metadata.Labels = map[string]string{"owner": "delivery"}
		metadata.Generation = 999
		strategy.PrepareForUpdate(context.Background(), metadata, old)
		if metadata.Generation != start {
			t.Fatalf("metadata changed generation from %d to %d", start, metadata.Generation)
		}
		changed := old.DeepCopy()
		changed.Spec.Resource.Kind = "Gadget"
		strategy.PrepareForUpdate(context.Background(), changed, old)
		if changed.Generation != start+1 {
			t.Fatalf("spec generation=%d want %d", changed.Generation, start+1)
		}
		status := old.DeepCopy()
		status.Generation = 999
		status.Spec.Resource.Kind = "Gadget"
		NewStatusStrategy(nil).PrepareForUpdate(context.Background(), status, old)
		if status.Generation != start || status.Spec.Resource.Kind != "Widget" {
			t.Fatal("status update changed spec or generation")
		}
	}
}
