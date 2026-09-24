package processor

import (
	"testing"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// TestEventBuildersScenarios asserts EventProcessor.buildActivity (live) and
// ActivityBuilder.BuildFromEvent (PolicyPreview/reindex) agree on
// Spec.Source, Spec.Resource, and Spec.Origin/Name across three cases: no
// source annotations (with a stray "related" field ignored), a non-core
// apiGroup without source annotations, and the full federated case. Resource
// always resolves from regarding/involvedObject; "related" is never used
// for Resource resolution, federated or not.
func TestEventBuildersScenarios(t *testing.T) {
	tests := []struct {
		name    string
		event   map[string]interface{}
		matched *MatchedPolicy

		wantSource   *v1alpha1.ActivitySource
		wantResource v1alpha1.ActivityResource
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
			name: "no source annotations: backward compatible, related field ignored",
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
				// Present, but always ignored for Resource resolution.
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
			// from regarding (Pod) even though "related" (WorkloadDeployment)
			// is present and is ignored; Origin.ID/Name are still qualified.
			name: "federated case: resource still from regarding, related ignored, origin qualified",
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
			wantOriginID:              "edge/cluster-dfw-1/a1b2c3",
			wantResourceMatchesPolicy: true,
			wantQualified:             true,
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
