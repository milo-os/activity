package processor

import (
	"testing"
)

func TestGetInvolvedObject(t *testing.T) {
	tests := []struct {
		name     string
		event    map[string]any
		wantNil  bool
		wantKind string
	}{
		{
			name: "events.k8s.io/v1 with regarding",
			event: map[string]any{
				"regarding": map[string]any{
					"kind":       "Pod",
					"name":       "my-pod",
					"namespace":  "default",
					"apiVersion": "v1",
				},
			},
			wantNil:  false,
			wantKind: "Pod",
		},
		{
			name: "v1 with involvedObject",
			event: map[string]any{
				"involvedObject": map[string]any{
					"kind":       "Deployment",
					"name":       "my-deployment",
					"namespace":  "default",
					"apiVersion": "apps/v1",
				},
			},
			wantNil:  false,
			wantKind: "Deployment",
		},
		{
			name:    "no involved object",
			event:   map[string]any{},
			wantNil: true,
		},
		{
			name: "prefers regarding over involvedObject",
			event: map[string]any{
				"regarding": map[string]any{
					"kind": "Service",
				},
				"involvedObject": map[string]any{
					"kind": "Pod",
				},
			},
			wantNil:  false,
			wantKind: "Service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveInvolvedObject(tt.event)
			if tt.wantNil {
				if got != nil {
					t.Errorf("ResolveInvolvedObject() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Errorf("ResolveInvolvedObject() = nil, want non-nil")
				return
			}
			if kind := getStringFromMap(got, "kind"); kind != tt.wantKind {
				t.Errorf("ResolveInvolvedObject() kind = %v, want %v", kind, tt.wantKind)
			}
		})
	}
}

func TestParseAPIGroup(t *testing.T) {
	tests := []struct {
		apiVersion string
		want       string
	}{
		{"v1", ""},
		{"apps/v1", "apps"},
		{"networking.k8s.io/v1", "networking.k8s.io"},
		{"projectcontour.io/v1", "projectcontour.io"},
		{"v1beta1", ""},
		{"batch/v1", "batch"},
	}

	for _, tt := range tests {
		t.Run(tt.apiVersion, func(t *testing.T) {
			if got := parseAPIGroup(tt.apiVersion); got != tt.want {
				t.Errorf("parseAPIGroup(%q) = %q, want %q", tt.apiVersion, got, tt.want)
			}
		})
	}
}

func TestResolveEventActor(t *testing.T) {
	tests := []struct {
		name     string
		event    map[string]any
		wantType string
		wantName string
	}{
		{
			name: "events.k8s.io/v1 with reportingController",
			event: map[string]any{
				"reportingController": "deployment-controller",
			},
			wantType: ActorTypeController,
			wantName: "deployment-controller",
		},
		{
			name: "v1 with source.component",
			event: map[string]any{
				"source": map[string]any{
					"component": "kubelet",
				},
			},
			wantType: ActorTypeController,
			wantName: "kubelet",
		},
		{
			name: "prefers reportingController over source",
			event: map[string]any{
				"reportingController": "scheduler",
				"source": map[string]any{
					"component": "kubelet",
				},
			},
			wantType: ActorTypeController,
			wantName: "scheduler",
		},
		{
			name:     "no actor info",
			event:    map[string]any{},
			wantType: ActorTypeController,
			wantName: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actor := resolveActorFromEvent(tt.event)
			if actor.Type != tt.wantType {
				t.Errorf("resolveActorFromEvent() Type = %v, want %v", actor.Type, tt.wantType)
			}
			if actor.Name != tt.wantName {
				t.Errorf("resolveActorFromEvent() Name = %v, want %v", actor.Name, tt.wantName)
			}
		})
	}
}

func TestNormalizeEvent(t *testing.T) {
	p := &EventProcessor{}

	tests := []struct {
		name           string
		event          map[string]any
		involvedObject map[string]any
		wantRegarding  bool
	}{
		{
			name: "event already has regarding",
			event: map[string]any{
				"reason": "Scheduled",
				"regarding": map[string]any{
					"kind": "Pod",
					"name": "my-pod",
				},
			},
			involvedObject: map[string]any{
				"kind": "Pod",
				"name": "my-pod",
			},
			wantRegarding: true,
		},
		{
			name: "event has involvedObject, should add regarding",
			event: map[string]any{
				"reason": "Scheduled",
				"involvedObject": map[string]any{
					"kind": "Deployment",
					"name": "my-deployment",
				},
			},
			involvedObject: map[string]any{
				"kind": "Deployment",
				"name": "my-deployment",
			},
			wantRegarding: true,
		},
		{
			name: "event has neither, should add regarding",
			event: map[string]any{
				"reason": "Unknown",
			},
			involvedObject: map[string]any{
				"kind": "Service",
				"name": "my-service",
			},
			wantRegarding: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized := p.normalizeEvent(tt.event, tt.involvedObject)

			// Check that regarding exists
			regarding, ok := normalized["regarding"].(map[string]any)
			if tt.wantRegarding && !ok {
				t.Error("normalizeEvent() should have 'regarding' field")
				return
			}

			// Verify the regarding content matches involvedObject
			if regarding != nil {
				if regarding["kind"] != tt.involvedObject["kind"] {
					t.Errorf("regarding.kind = %v, want %v", regarding["kind"], tt.involvedObject["kind"])
				}
				if regarding["name"] != tt.involvedObject["name"] {
					t.Errorf("regarding.name = %v, want %v", regarding["name"], tt.involvedObject["name"])
				}
			}

			// Verify original event is not modified when it had regarding
			// (we return the same map reference if it already has regarding)

			// Verify other fields are preserved
			if normalized["reason"] != tt.event["reason"] {
				t.Errorf("reason field not preserved: got %v, want %v", normalized["reason"], tt.event["reason"])
			}
		})
	}
}

func TestBuildActivityTenantFromScopeAnnotations(t *testing.T) {
	p := &EventProcessor{}

	involvedObject := map[string]any{
		"kind":       "DNSRecord",
		"name":       "my-record",
		"namespace":  "project-ns",
		"uid":        "dns-789",
		"apiVersion": "dns.miloapis.com/v1alpha1",
	}

	matched := &MatchedPolicy{
		PolicyName: "dns-records",
		Generation: 1,
		APIGroup:   "dns.miloapis.com",
		Kind:       "DNSRecord",
		Summary:    "DNS record created",
	}

	tests := []struct {
		name           string
		event          map[string]any
		involvedObject map[string]any
		wantTenant     string
		wantName       string
	}{
		{
			name: "project scope annotation is used",
			event: map[string]any{
				"metadata": map[string]any{
					"uid": "event-001",
					"annotations": map[string]any{
						"platform.miloapis.com/scope.type": TenantTypeProject,
						"platform.miloapis.com/scope.name": "my-project",
					},
				},
				"reportingController": "dns-operator",
				"regarding":           involvedObject,
			},
			involvedObject: involvedObject,
			wantTenant:     TenantTypeProject,
			wantName:       "my-project",
		},
		{
			name: "organization scope annotation is used",
			event: map[string]any{
				"metadata": map[string]any{
					"uid": "event-002",
					"annotations": map[string]any{
						"platform.miloapis.com/scope.type": TenantTypeOrganization,
						"platform.miloapis.com/scope.name": "acme-corp",
					},
				},
				"reportingController": "dns-operator",
				"regarding":           involvedObject,
			},
			involvedObject: involvedObject,
			wantTenant:     TenantTypeOrganization,
			wantName:       "acme-corp",
		},
		{
			name: "missing annotations fall back to platform scope",
			event: map[string]any{
				"metadata": map[string]any{
					"uid": "event-003",
				},
				"reportingController": "dns-operator",
				"regarding":           involvedObject,
			},
			involvedObject: involvedObject,
			wantTenant:     TenantTypePlatform,
			wantName:       "",
		},
		{
			name: "scope annotations with nil regarding does not panic",
			event: map[string]any{
				"metadata": map[string]any{
					"uid": "event-004",
					"annotations": map[string]any{
						"platform.miloapis.com/scope.type": TenantTypeProject,
						"platform.miloapis.com/scope.name": "my-project",
					},
				},
				"reportingController": "dns-operator",
			},
			involvedObject: nil,
			wantTenant:     TenantTypeProject,
			wantName:       "my-project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			activity := p.buildActivity(tt.event, matched, tt.involvedObject, matched.Summary, nil)
			if activity.Spec.Tenant.Type != tt.wantTenant {
				t.Errorf("Tenant.Type = %q, want %q", activity.Spec.Tenant.Type, tt.wantTenant)
			}
			if activity.Spec.Tenant.Name != tt.wantName {
				t.Errorf("Tenant.Name = %q, want %q", activity.Spec.Tenant.Name, tt.wantName)
			}
		})
	}
}

func TestBuildActivityFromEvent(t *testing.T) {
	p := &EventProcessor{}

	event := map[string]any{
		"metadata": map[string]any{
			"uid":               "event-123",
			"creationTimestamp": "2024-01-15T10:30:00Z",
		},
		"reason":              "Scheduled",
		"message":             "Successfully assigned default/my-pod to node-1",
		"reportingController": "default-scheduler",
		"regarding": map[string]any{
			"kind":       "Pod",
			"name":       "my-pod",
			"namespace":  "default",
			"uid":        "pod-456",
			"apiVersion": "v1",
		},
	}

	involvedObject := map[string]any{
		"kind":       "Pod",
		"name":       "my-pod",
		"namespace":  "default",
		"uid":        "pod-456",
		"apiVersion": "v1",
	}

	matched := &MatchedPolicy{
		PolicyName: "core-pods",
		Generation: 1,
		APIGroup:   "",
		Kind:       "Pod",
		Summary:    "Pod my-pod was scheduled",
	}

	activity := p.buildActivity(event, matched, involvedObject, matched.Summary, nil)

	// Verify activity fields
	if activity.Spec.Summary != "Pod my-pod was scheduled" {
		t.Errorf("Summary = %q, want %q", activity.Spec.Summary, "Pod my-pod was scheduled")
	}

	if activity.Spec.ChangeSource != ChangeSourceSystem {
		t.Errorf("ChangeSource = %q, want %q", activity.Spec.ChangeSource, ChangeSourceSystem)
	}

	if activity.Spec.Actor.Type != ActorTypeController {
		t.Errorf("Actor.Type = %q, want %q", activity.Spec.Actor.Type, ActorTypeController)
	}

	if activity.Spec.Actor.Name != "default-scheduler" {
		t.Errorf("Actor.Name = %q, want %q", activity.Spec.Actor.Name, "default-scheduler")
	}

	if activity.Spec.Resource.Kind != "Pod" {
		t.Errorf("Resource.Kind = %q, want %q", activity.Spec.Resource.Kind, "Pod")
	}

	if activity.Spec.Resource.Name != "my-pod" {
		t.Errorf("Resource.Name = %q, want %q", activity.Spec.Resource.Name, "my-pod")
	}

	if activity.Spec.Resource.Namespace != "default" {
		t.Errorf("Resource.Namespace = %q, want %q", activity.Spec.Resource.Namespace, "default")
	}

	if activity.Spec.Origin.Type != "event" {
		t.Errorf("Origin.Type = %q, want %q", activity.Spec.Origin.Type, "event")
	}

	if activity.Spec.Origin.ID != "event-123" {
		t.Errorf("Origin.ID = %q, want %q", activity.Spec.Origin.ID, "event-123")
	}

	// Verify labels
	if activity.Labels["activity.miloapis.com/origin-type"] != "event" {
		t.Errorf("origin-type label = %q, want %q", activity.Labels["activity.miloapis.com/origin-type"], "event")
	}

	if activity.Labels["activity.miloapis.com/event-reason"] != "Scheduled" {
		t.Errorf("event-reason label = %q, want %q", activity.Labels["activity.miloapis.com/event-reason"], "Scheduled")
	}
}
