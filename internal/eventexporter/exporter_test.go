package eventexporter

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestExporter_ResolveScope(t *testing.T) {
	scopedNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "scoped-ns",
			Labels: map[string]string{upstreamClusterNameLabel: "cluster-my-project"},
		},
	}
	unscopedNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "unscoped-ns"},
	}

	tests := []struct {
		name          string
		planeType     string
		namespace     string
		wantScopeType string
		wantScopeName string
		wantUnscoped  bool
	}{
		{
			name:          "management deployment: always uses static flags",
			planeType:     "management",
			namespace:     "unscoped-ns",
			wantScopeType: "organization",
			wantScopeName: "dev-org",
		},
		{
			name:          "empty plane type: treated as management",
			planeType:     "",
			namespace:     "scoped-ns",
			wantScopeType: "organization",
			wantScopeName: "dev-org",
		},
		{
			name:          "edge deployment, namespace resolves: uses recovered project scope",
			planeType:     planeTypeEdge,
			namespace:     "scoped-ns",
			wantScopeType: scopeTypeProject,
			wantScopeName: "my-project",
		},
		{
			name:         "edge deployment, namespace does not resolve: unscoped",
			planeType:    planeTypeEdge,
			namespace:    "unscoped-ns",
			wantUnscoped: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exporter := &Exporter{
				scopeType: "organization",
				scopeName: "dev-org",
				planeType: tt.planeType,
				scope:     newNamespaceScopeResolver(newNamespaceLister(scopedNamespace, unscopedNamespace)),
			}

			before := testutil.ToFloat64(unscopedEvents)
			gotType, gotName := exporter.resolveScope(tt.namespace)
			after := testutil.ToFloat64(unscopedEvents)

			if tt.wantUnscoped {
				if gotType != "" || gotName != "" {
					t.Errorf("resolveScope() = (%q, %q), want unscoped (\"\", \"\")", gotType, gotName)
				}
				if after != before+1 {
					t.Errorf("unscopedEvents counter did not increment: before=%v after=%v", before, after)
				}
				return
			}

			if gotType != tt.wantScopeType || gotName != tt.wantScopeName {
				t.Errorf("resolveScope() = (%q, %q), want (%q, %q)", gotType, gotName, tt.wantScopeType, tt.wantScopeName)
			}
			if after != before {
				t.Errorf("unscopedEvents counter incremented unexpectedly: before=%v after=%v", before, after)
			}
		})
	}
}

// resolvedCityResolver returns a cityResolver already primed with cityCode.
func resolvedCityResolver(t *testing.T, cityCode string) *cityResolver {
	t.Helper()
	fakeClient := fake.NewClientBuilder().WithScheme(locationsScheme).
		WithObjects(newServingLocation("us-central-1", cityCode)).
		Build()
	resolver := newCityResolver(fakeClient, "")
	if !resolver.resolveOnce(context.Background()) {
		t.Fatal("resolveOnce() = false, want true")
	}
	return resolver
}

func TestExporter_SourceAnnotations(t *testing.T) {
	tests := []struct {
		name       string
		planeType  string
		city       *cityResolver
		wantCity   string
		wantNoCity bool
	}{
		{
			name:      "management deployment: no city resolver",
			planeType: "management",
		},
		{
			name:      "edge deployment, city resolved",
			planeType: planeTypeEdge,
			city:      resolvedCityResolver(t, "DFW"),
			wantCity:  "DFW",
		},
		{
			name:       "edge deployment, city not yet resolved",
			planeType:  planeTypeEdge,
			city:       newCityResolver(fake.NewClientBuilder().WithScheme(locationsScheme).Build(), ""),
			wantNoCity: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exporter := &Exporter{
				planeType:     tt.planeType,
				clusterName:   "us-central1-a",
				clusterRegion: "us-central1",
				city:          tt.city,
			}

			before := testutil.ToFloat64(noCityEvents)
			gotPlaneType, gotCluster, gotRegion, gotCity := exporter.sourceAnnotations()
			after := testutil.ToFloat64(noCityEvents)

			if gotPlaneType != tt.planeType || gotCluster != "us-central1-a" || gotRegion != "us-central1" {
				t.Errorf("sourceAnnotations() = (%q, %q, %q, _), want (%q, %q, %q, _)",
					gotPlaneType, gotCluster, gotRegion, tt.planeType, "us-central1-a", "us-central1")
			}
			if gotCity != tt.wantCity {
				t.Errorf("sourceAnnotations() city = %q, want %q", gotCity, tt.wantCity)
			}

			wantDelta := 0.0
			if tt.wantNoCity {
				wantDelta = 1
			}
			if after != before+wantDelta {
				t.Errorf("noCityEvents counter delta = %v, want %v", after-before, wantDelta)
			}
		})
	}
}

func TestExporter_QualifiedMsgID(t *testing.T) {
	event := &eventsv1.Event{
		ObjectMeta: metav1.ObjectMeta{UID: "a1b2c3", ResourceVersion: "42"},
	}

	tests := []struct {
		name      string
		planeType string
		cluster   string
		eventType string
		want      string
	}{
		{
			name:      "edge deployment, added event: qualified UID",
			planeType: planeTypeEdge,
			cluster:   "cluster-dfw-1",
			eventType: "ADDED",
			want:      "edge/cluster-dfw-1/a1b2c3",
		},
		{
			name:      "edge deployment, modified event: qualified UID-ResourceVersion",
			planeType: planeTypeEdge,
			cluster:   "cluster-dfw-1",
			eventType: "MODIFIED",
			want:      "edge/cluster-dfw-1/a1b2c3-42",
		},
		{
			name:      "empty plane type: unqualified",
			eventType: "ADDED",
			want:      "a1b2c3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exporter := &Exporter{planeType: tt.planeType, clusterName: tt.cluster}
			if got := exporter.qualifiedMsgID(event, tt.eventType); got != tt.want {
				t.Errorf("qualifiedMsgID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExporter_Enqueue(t *testing.T) {
	t.Run("room in queue: job enqueued, depth updated", func(t *testing.T) {
		exporter := &Exporter{queue: make(chan eventJob, 2)}

		beforeDropped := testutil.ToFloat64(droppedEvents)
		exporter.enqueue(&eventsv1.Event{ObjectMeta: metav1.ObjectMeta{Name: "event-a"}}, "ADDED")

		if got := testutil.ToFloat64(droppedEvents); got != beforeDropped {
			t.Errorf("droppedEvents incremented unexpectedly: before=%v after=%v", beforeDropped, got)
		}
		if got := testutil.ToFloat64(queueDepth); got != 1 {
			t.Errorf("queueDepth = %v, want 1", got)
		}
		if got := len(exporter.queue); got != 1 {
			t.Errorf("len(queue) = %d, want 1", got)
		}
	})

	t.Run("queue full: incoming event dropped, queued job kept", func(t *testing.T) {
		exporter := &Exporter{queue: make(chan eventJob, 1)}
		exporter.queue <- eventJob{event: &eventsv1.Event{ObjectMeta: metav1.ObjectMeta{Name: "sentinel"}}, eventType: "ADDED"}

		beforeDropped := testutil.ToFloat64(droppedEvents)
		exporter.enqueue(&eventsv1.Event{ObjectMeta: metav1.ObjectMeta{Name: "incoming"}}, "ADDED")

		if got := testutil.ToFloat64(droppedEvents); got != beforeDropped+1 {
			t.Errorf("droppedEvents did not increment: before=%v after=%v", beforeDropped, got)
		}
		if got := testutil.ToFloat64(queueDepth); got != 1 {
			t.Errorf("queueDepth = %v, want 1", got)
		}
		if got := len(exporter.queue); got != 1 {
			t.Errorf("len(queue) = %d, want 1", got)
		}

		queued := <-exporter.queue
		if queued.event.Name != "sentinel" {
			t.Errorf("queue kept %q, want the already-queued sentinel event", queued.event.Name)
		}
	})
}
