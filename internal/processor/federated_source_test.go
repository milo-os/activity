package processor

import (
	"reflect"
	"testing"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// TestEventBuildersScenarios asserts EventProcessor.buildActivity (live) and
// ActivityBuilder.BuildFromEvent (PolicyPreview/reindex) build identical
// Activities. Resource always resolves from regarding/involvedObject;
// "related" goes to Related, never Resource.
func TestEventBuildersScenarios(t *testing.T) {
	tests := []struct {
		name    string
		event   map[string]interface{}
		matched *MatchedPolicy

		wantSource   *v1alpha1.ActivitySource
		wantResource v1alpha1.ActivityResource
		wantRelated  []v1alpha1.ActivityResource
		wantOriginID string

		// wantResourceMatchesPolicy is true only when resourceObject is the
		// policy-matched object, i.e. Resource.APIGroup/Kind must equal
		// matched.APIGroup/Kind.
		wantResourceMatchesPolicy bool
		// wantQualified asserts Origin.ID/Name differ from the bare-UID hash,
		// proving qualification fired.
		wantQualified bool
	}{
		{
			name: "no source annotations: backward compatible, related goes to Related",
			event: map[string]interface{}{
				"metadata": map[string]interface{}{
					"uid": "event-uid-no-source",
				},
				"reason": "Started",
				"regarding": map[string]interface{}{
					"kind":       "Pod",
					"name":       "my-pod",
					"namespace":  "default",
					"uid":        "pod-uid-1",
					"apiVersion": "v1",
				},
				// Present, but never used for Resource resolution.
				"related": map[string]interface{}{
					"kind": "Node",
					"name": "node-1",
					"uid":  "node-uid-1",
				},
			},
			matched: &MatchedPolicy{
				PolicyName: "core-pods",
				APIGroup:   "",
				Kind:       "Pod",
				Summary:    "Pod my-pod started",
			},
			wantSource: nil,
			wantResource: v1alpha1.ActivityResource{
				APIGroup:   "",
				APIVersion: "v1",
				Kind:       "Pod",
				Name:       "my-pod",
				Namespace:  "default",
				UID:        "pod-uid-1",
			},
			wantRelated:               []v1alpha1.ActivityResource{{Kind: "Node", Name: "node-1", UID: "node-uid-1"}},
			wantOriginID:              "event-uid-no-source",
			wantResourceMatchesPolicy: true,
		},
		{
			// Exercises the parseAPIGroup fallback while proving APIGroup/Kind
			// still match the policy match, since resourceObject is involvedObject
			// here.
			name: "non-core apiGroup without source annotations: backward compatible",
			event: map[string]interface{}{
				"metadata": map[string]interface{}{
					"uid": "event-uid-noncoregroup",
				},
				"reason": "ScalingReplicaSet",
				"regarding": map[string]interface{}{
					"kind":       "Deployment",
					"name":       "my-deployment",
					"namespace":  "default",
					"uid":        "deployment-uid-1",
					"apiVersion": "apps/v1",
				},
			},
			matched: &MatchedPolicy{
				PolicyName: "apps-deployments",
				APIGroup:   "apps",
				Kind:       "Deployment",
				Summary:    "Deployment my-deployment scaled",
			},
			wantSource: nil,
			wantResource: v1alpha1.ActivityResource{
				APIGroup:   "apps",
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "my-deployment",
				Namespace:  "default",
				UID:        "deployment-uid-1",
			},
			wantOriginID:              "event-uid-noncoregroup",
			wantResourceMatchesPolicy: true,
		},
		{
			// Full federated case: Source populated, Resource still resolves
			// from regarding (Pod) while "related" (WorkloadDeployment) goes to
			// Related; Origin.ID/Name are still qualified.
			name: "federated case: resource still from regarding, related goes to Related, origin qualified",
			event: map[string]interface{}{
				"metadata": map[string]interface{}{
					"uid": "a1b2c3",
					"annotations": map[string]interface{}{
						"activity.miloapis.com/source-plane-type": "edge",
						"activity.miloapis.com/source-cluster":    "cluster-dfw-1",
						"activity.miloapis.com/source-region":     "us-central1",
						"activity.miloapis.com/source-city":       "dfw",
					},
				},
				"reason": "InstanceCrashed",
				"regarding": map[string]interface{}{
					"kind":      "Pod",
					"name":      "instance-pod",
					"namespace": "default",
					"uid":       "instance-pod-uid",
				},
				"related": map[string]interface{}{
					"kind":       "WorkloadDeployment",
					"name":       "my-workload-deployment",
					"namespace":  "default",
					"uid":        "wd-uid-1",
					"apiVersion": "compute.datumapis.com/v1alpha1",
				},
			},
			matched: &MatchedPolicy{APIGroup: "", Kind: "Pod", Summary: "Instance crashed in dfw"},
			wantSource: &v1alpha1.ActivitySource{
				PlaneType: "edge",
				Cluster:   "cluster-dfw-1",
				Region:    "us-central1",
				City:      "dfw",
			},
			wantResource: v1alpha1.ActivityResource{
				APIGroup:  "",
				Kind:      "Pod",
				Name:      "instance-pod",
				Namespace: "default",
				UID:       "instance-pod-uid",
			},
			wantRelated: []v1alpha1.ActivityResource{{
				APIGroup:   "compute.datumapis.com",
				APIVersion: "compute.datumapis.com/v1alpha1",
				Kind:       "WorkloadDeployment",
				Name:       "my-workload-deployment",
				Namespace:  "default",
				UID:        "wd-uid-1",
			}},
			wantOriginID:              "edge/cluster-dfw-1/a1b2c3",
			wantResourceMatchesPolicy: true,
			wantQualified:             true,
		},
		{
			// An edge cell's ns-<uuid> doesn't exist in the serving control
			// plane, so the upstream namespace the exporter recorded wins.
			name: "federated case: upstream namespace annotation wins over the event's own namespace",
			event: map[string]interface{}{
				"metadata": map[string]interface{}{
					"uid": "d4e5f6",
					"annotations": map[string]interface{}{
						"activity.miloapis.com/source-plane-type": "edge",
						"activity.miloapis.com/source-cluster":    "cluster-dfw-1",
						"platform.miloapis.com/scope.namespace":   "default",
					},
				},
				"reason": "InstanceCrashed",
				"regarding": map[string]interface{}{
					"kind":      "Pod",
					"name":      "instance-pod",
					"namespace": "ns-0f1e2d3c-4b5a",
					"uid":       "instance-pod-uid",
				},
			},
			matched: &MatchedPolicy{APIGroup: "", Kind: "Pod", Summary: "Instance crashed in dfw"},
			wantSource: &v1alpha1.ActivitySource{
				PlaneType: "edge",
				Cluster:   "cluster-dfw-1",
			},
			wantResource: v1alpha1.ActivityResource{
				APIGroup:  "",
				Kind:      "Pod",
				Name:      "instance-pod",
				Namespace: "default",
				UID:       "instance-pod-uid",
			},
			wantOriginID:              "edge/cluster-dfw-1/d4e5f6",
			wantResourceMatchesPolicy: true,
			wantQualified:             true,
		},
		{
			// The hub, and edge cells predating the annotation, send neither.
			name: "no upstream namespace annotation: falls back to the event's own namespace",
			event: map[string]interface{}{
				"metadata": map[string]interface{}{
					"uid": "g7h8i9",
					"annotations": map[string]interface{}{
						"activity.miloapis.com/source-plane-type": "edge",
						"activity.miloapis.com/source-cluster":    "cluster-dfw-1",
					},
				},
				"reason": "InstanceCrashed",
				"regarding": map[string]interface{}{
					"kind":      "Pod",
					"name":      "instance-pod",
					"namespace": "ns-0f1e2d3c-4b5a",
					"uid":       "instance-pod-uid",
				},
			},
			matched: &MatchedPolicy{APIGroup: "", Kind: "Pod", Summary: "Instance crashed in dfw"},
			wantSource: &v1alpha1.ActivitySource{
				PlaneType: "edge",
				Cluster:   "cluster-dfw-1",
			},
			wantResource: v1alpha1.ActivityResource{
				APIGroup:  "",
				Kind:      "Pod",
				Name:      "instance-pod",
				Namespace: "ns-0f1e2d3c-4b5a",
				UID:       "instance-pod-uid",
			},
			wantOriginID:              "edge/cluster-dfw-1/g7h8i9",
			wantResourceMatchesPolicy: true,
			wantQualified:             true,
		},
		{
			name: "empty upstream namespace annotation: falls back to the event's own namespace",
			event: map[string]interface{}{
				"metadata": map[string]interface{}{
					"uid": "j1k2l3",
					"annotations": map[string]interface{}{
						"activity.miloapis.com/source-plane-type": "edge",
						"activity.miloapis.com/source-cluster":    "cluster-dfw-1",
						"platform.miloapis.com/scope.namespace":   "",
					},
				},
				"reason": "InstanceCrashed",
				"regarding": map[string]interface{}{
					"kind":      "Pod",
					"name":      "instance-pod",
					"namespace": "ns-0f1e2d3c-4b5a",
					"uid":       "instance-pod-uid",
				},
			},
			matched: &MatchedPolicy{APIGroup: "", Kind: "Pod", Summary: "Instance crashed in dfw"},
			wantSource: &v1alpha1.ActivitySource{
				PlaneType: "edge",
				Cluster:   "cluster-dfw-1",
			},
			wantResource: v1alpha1.ActivityResource{
				APIGroup:  "",
				Kind:      "Pod",
				Name:      "instance-pod",
				Namespace: "ns-0f1e2d3c-4b5a",
				UID:       "instance-pod-uid",
			},
			wantOriginID:              "edge/cluster-dfw-1/j1k2l3",
			wantResourceMatchesPolicy: true,
			wantQualified:             true,
		},
		{
			name: "cluster-scoped resource with no namespace anywhere: cluster-scoped Activity",
			event: map[string]interface{}{
				"metadata": map[string]interface{}{
					"uid": "event-uid-cluster-scoped",
				},
				"reason": "NodeReady",
				"regarding": map[string]interface{}{
					"kind":       "Node",
					"name":       "node-1",
					"uid":        "node-uid-1",
					"apiVersion": "v1",
				},
			},
			matched: &MatchedPolicy{
				PolicyName: "core-nodes",
				APIGroup:   "",
				Kind:       "Node",
				Summary:    "Node node-1 is ready",
			},
			wantSource: nil,
			wantResource: v1alpha1.ActivityResource{
				APIGroup:   "",
				APIVersion: "v1",
				Kind:       "Node",
				Name:       "node-1",
				Namespace:  "",
				UID:        "node-uid-1",
			},
			wantOriginID:              "event-uid-cluster-scoped",
			wantResourceMatchesPolicy: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &EventProcessor{}
			liveActivity := p.buildActivity(tt.event, tt.matched, tt.matched.Summary, nil)

			builder := &ActivityBuilder{APIGroup: tt.matched.APIGroup, Kind: tt.matched.Kind}
			previewActivity, err := builder.BuildFromEvent(tt.event, tt.matched.Summary, nil, nil)
			if err != nil {
				t.Fatalf("BuildFromEvent() error = %v", err)
			}

			// Covers both bare and qualified cases: activityName over wantOriginID.
			wantName := activityName("event", tt.wantOriginID, tt.matched.APIGroup, tt.matched.Kind)

			for name, activity := range map[string]*v1alpha1.Activity{"live": liveActivity, "preview": previewActivity} {
				t.Run(name, func(t *testing.T) {
					if tt.wantSource == nil {
						if activity.Spec.Source != nil {
							t.Errorf("Spec.Source = %+v, want nil", activity.Spec.Source)
						}
					} else if activity.Spec.Source == nil || *activity.Spec.Source != *tt.wantSource {
						t.Errorf("Spec.Source = %+v, want %+v", activity.Spec.Source, tt.wantSource)
					}

					if activity.Spec.Resource != tt.wantResource {
						t.Errorf("Spec.Resource = %+v, want %+v", activity.Spec.Resource, tt.wantResource)
					}

					if !reflect.DeepEqual(activity.Spec.Related, tt.wantRelated) {
						t.Errorf("Spec.Related = %+v, want %+v", activity.Spec.Related, tt.wantRelated)
					}

					if activity.Namespace != tt.wantResource.Namespace {
						t.Errorf("ObjectMeta.Namespace = %q, want %q", activity.Namespace, tt.wantResource.Namespace)
					}

					if activity.Spec.Origin.ID != tt.wantOriginID {
						t.Errorf("Spec.Origin.ID = %q, want %q", activity.Spec.Origin.ID, tt.wantOriginID)
					}

					if activity.Name != wantName {
						t.Errorf("Name = %q, want %q (hash of Origin.ID)", activity.Name, wantName)
					}

					if tt.wantResourceMatchesPolicy {
						if activity.Spec.Resource.APIGroup != tt.matched.APIGroup {
							t.Errorf("Spec.Resource.APIGroup = %q, want it to equal matched.APIGroup %q", activity.Spec.Resource.APIGroup, tt.matched.APIGroup)
						}
						if activity.Spec.Resource.Kind != tt.matched.Kind {
							t.Errorf("Spec.Resource.Kind = %q, want it to equal matched.Kind %q", activity.Spec.Resource.Kind, tt.matched.Kind)
						}
					}

					if tt.wantQualified {
						bareUID := GetNestedString(tt.event, "metadata", "uid")
						bareName := activityName("event", bareUID, tt.matched.APIGroup, tt.matched.Kind)
						if activity.Name == bareName {
							t.Errorf("Name = %q equals the bare-UID hash %q; Origin.ID and Name must qualify together", activity.Name, bareName)
						}
					}
				})
			}

			// The two paths must always agree with each other.
			if previewActivity.Name != liveActivity.Name {
				t.Errorf("Name mismatch between buildActivity and BuildFromEvent: live=%q preview=%q", liveActivity.Name, previewActivity.Name)
			}
			if previewActivity.Spec.Resource != liveActivity.Spec.Resource {
				t.Errorf("Resource mismatch between buildActivity and BuildFromEvent: live=%+v preview=%+v", liveActivity.Spec.Resource, previewActivity.Spec.Resource)
			}
			if previewActivity.Spec.Origin != liveActivity.Spec.Origin {
				t.Errorf("Origin mismatch between buildActivity and BuildFromEvent: live=%+v preview=%+v", liveActivity.Spec.Origin, previewActivity.Spec.Origin)
			}
		})
	}
}
