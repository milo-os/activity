package controller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

func TestActivityPolicyStatusTracksValidatedGeneration(t *testing.T) {
	for _, tt := range []struct {
		name                        string
		observed, conditionObserved int64
	}{
		{"new spec", 1, 1},
		{"legacy generation", 0, 0},
		{"stale policy status", 1, 2},
		{"stale condition", 2, 1},
		{"already current", 2, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, v1alpha1.AddToScheme(scheme))
			transition := metav1.NewTime(time.Unix(100, 0))
			policy := &v1alpha1.ActivityPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Generation: 2},
				Status: v1alpha1.ActivityPolicyStatus{
					ObservedGeneration: tt.observed,
					Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue,
						Reason: "Valid", Message: "All rules validated successfully",
						ObservedGeneration: tt.conditionObserved, LastTransitionTime: transition}},
				},
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(policy).WithObjects(policy).Build()
			key := types.NamespacedName{Name: policy.Name}
			require.NoError(t, cl.Get(context.Background(), key, policy))
			beforeVersion := policy.ResourceVersion
			condition := policy.Status.Conditions[0]
			condition.ObservedGeneration = policy.Generation
			condition.LastTransitionTime = metav1.Now()
			reconciler := &ActivityPolicyReconciler{Client: cl, Scheme: scheme}
			require.NoError(t, reconciler.updatePolicyStatus(context.Background(), policy, condition))
			var actual v1alpha1.ActivityPolicy
			require.NoError(t, cl.Get(context.Background(), key, &actual))
			require.Equal(t, int64(2), actual.Status.ObservedGeneration)
			require.Equal(t, int64(2), actual.Status.Conditions[0].ObservedGeneration)
			require.Equal(t, transition, actual.Status.Conditions[0].LastTransitionTime)
			if tt.observed == 2 && tt.conditionObserved == 2 {
				require.Equal(t, beforeVersion, actual.ResourceVersion, "unchanged status must not be rewritten")
			}
		})
	}
}
