package edgeingest

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
)

const (
	projectNamespaceUID = "9f3c8a21-4c1b-4d0e-9a3f-2b1c7e5d8a90"
	otherNamespaceUID   = "11112222-3333-4444-5555-666677778888"
)

var (
	downstreamNS = "ns-" + projectNamespaceUID
	otherNS      = "ns-" + otherNamespaceUID
)

func newRawObject(t *testing.T, value any) *runtime.Unknown {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("failed encoding fixture: %v", err)
	}
	return &runtime.Unknown{Raw: raw}
}

// edgeEvent is a recorded-shape audit event that names the downstream namespace
// in every place one can appear.
func edgeEvent(t *testing.T) *auditv1.Event {
	t.Helper()
	return &auditv1.Event{
		TypeMeta:   metav1.TypeMeta{APIVersion: "audit.k8s.io/v1", Kind: "Event"},
		Level:      auditv1.LevelRequestResponse,
		AuditID:    "3f2504e0-4f89-11d3-9a0c-0305e82c3301",
		Stage:      auditv1.StageResponseComplete,
		RequestURI: "/apis/compute.datumapis.com/v1alpha1/namespaces/" + downstreamNS + "/instances/web-0?fieldSelector=metadata.namespace%3D" + downstreamNS,
		Verb:       "update",
		ObjectRef: &auditv1.ObjectReference{
			Resource:   "instances",
			Namespace:  downstreamNS,
			Name:       "web-0",
			APIGroup:   "compute.datumapis.com",
			APIVersion: "v1alpha1",
		},
		RequestObject: newRawObject(t, map[string]any{
			"metadata": map[string]any{"name": "web-0", "namespace": downstreamNS},
		}),
		ResponseObject: newRawObject(t, map[string]any{
			"metadata": map[string]any{
				"name":      "web-0",
				"namespace": downstreamNS,
				"selfLink":  "/apis/compute.datumapis.com/v1alpha1/namespaces/" + downstreamNS + "/instances/web-0",
			},
			"items": []any{
				map[string]any{"metadata": map[string]any{"namespace": downstreamNS, "name": "web-1"}},
			},
		}),
	}
}

func TestDownstreamNamespaceReferencesFindsEveryField(t *testing.T) {
	references, err := DownstreamNamespaceReferences(edgeEvent(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(references) != 1 || references[0] != downstreamNS {
		t.Fatalf("references = %v, want [%s]", references, downstreamNS)
	}
}

func TestDownstreamNamespaceReferencesIgnoresCustomerNamespaces(t *testing.T) {
	event := &auditv1.Event{
		RequestURI: "/api/v1/namespaces/ns-team-alpha/pods",
		ObjectRef:  &auditv1.ObjectReference{Namespace: "ns-team-alpha", Resource: "pods"},
	}

	references, err := DownstreamNamespaceReferences(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(references) != 0 {
		t.Fatalf("references = %v, want none", references)
	}
}

func TestPrimaryDownstreamNamespace(t *testing.T) {
	tests := []struct {
		name  string
		event *auditv1.Event
		want  string
		found bool
	}{
		{
			name:  "object reference",
			event: &auditv1.Event{ObjectRef: &auditv1.ObjectReference{Namespace: downstreamNS, Resource: "instances"}},
			want:  downstreamNS,
			found: true,
		},
		{
			name:  "namespace object itself",
			event: &auditv1.Event{ObjectRef: &auditv1.ObjectReference{Resource: "namespaces", Name: downstreamNS}},
			want:  downstreamNS,
			found: true,
		},
		{
			name:  "request uri path only",
			event: &auditv1.Event{RequestURI: "/api/v1/namespaces/" + downstreamNS + "/pods"},
			want:  downstreamNS,
			found: true,
		},
		{
			name:  "request uri query only",
			event: &auditv1.Event{RequestURI: "/api/v1/pods?fieldSelector=metadata.namespace%3D" + downstreamNS},
			want:  downstreamNS,
			found: true,
		},
		{
			name:  "cluster scoped request",
			event: &auditv1.Event{RequestURI: "/api/v1/nodes", ObjectRef: &auditv1.ObjectReference{Resource: "nodes"}},
			found: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := PrimaryDownstreamNamespace(tt.event)
			if found != tt.found {
				t.Fatalf("found = %v, want %v", found, tt.found)
			}
			if found && got != tt.want {
				t.Errorf("namespace = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUnmapNamespacesRewritesEveryField(t *testing.T) {
	event := edgeEvent(t)

	if err := UnmapNamespaces(event, map[string]string{downstreamNS: "default"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.ObjectRef.Namespace != "default" {
		t.Errorf("objectRef.namespace = %q", event.ObjectRef.Namespace)
	}
	if strings.Contains(event.RequestURI, downstreamNamespacePrefix) {
		t.Errorf("requestURI still carries a downstream namespace: %q", event.RequestURI)
	}
	if !strings.Contains(event.RequestURI, "/namespaces/default/") {
		t.Errorf("requestURI was not rewritten: %q", event.RequestURI)
	}

	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed encoding event: %v", err)
	}
	if downstreamNamespacePattern.Match(encoded) {
		t.Errorf("event still references a downstream namespace: %s", encoded)
	}
	if strings.Count(string(encoded), `"default"`) < 3 {
		t.Errorf("expected the upstream namespace throughout the event: %s", encoded)
	}
}

func TestUnmapNamespacesRewritesNamespaceObjectName(t *testing.T) {
	event := &auditv1.Event{
		RequestURI: "/api/v1/namespaces/" + downstreamNS,
		ObjectRef:  &auditv1.ObjectReference{Resource: "namespaces", Name: downstreamNS},
	}

	if err := UnmapNamespaces(event, map[string]string{downstreamNS: "default"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.ObjectRef.Name != "default" {
		t.Errorf("objectRef.name = %q, want default", event.ObjectRef.Name)
	}
}

func TestUnmapNamespacesFailsClosedOnPartialMapping(t *testing.T) {
	event := edgeEvent(t)
	event.ResponseObject = newRawObject(t, map[string]any{
		"items": []any{
			map[string]any{"metadata": map[string]any{"namespace": downstreamNS}},
			map[string]any{"metadata": map[string]any{"namespace": otherNS}},
		},
	})

	err := UnmapNamespaces(event, map[string]string{downstreamNS: "default"})
	if !errors.Is(err, ErrDownstreamNamespaceLeak) {
		t.Fatalf("err = %v, want ErrDownstreamNamespaceLeak", err)
	}
}

func TestUnmapNamespacesRewritesAnnotations(t *testing.T) {
	event := &auditv1.Event{
		Annotations: map[string]string{"authorization.k8s.io/reason": "allowed in " + downstreamNS},
	}

	if err := UnmapNamespaces(event, map[string]string{downstreamNS: "default"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := event.Annotations["authorization.k8s.io/reason"]; got != "allowed in default" {
		t.Errorf("annotation = %q", got)
	}
}
