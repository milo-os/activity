package eventexporter

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"go.miloapis.com/activity/internal/natsconn"
	"go.miloapis.com/activity/internal/types"
)

func TestExporter_ResolveScope(t *testing.T) {
	scopedNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "scoped-ns",
			Labels: map[string]string{
				upstreamClusterNameLabel: "cluster-my-project",
				upstreamNamespaceLabel:   "default",
			},
		},
	}
	unscopedNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "unscoped-ns"},
	}

	tests := []struct {
		name         string
		planeType    string
		namespace    string
		wantScope    eventScope
		wantUnscoped bool
		// wantAnnotations is what stampScope writes for the same resolution.
		wantAnnotations map[string]string
	}{
		{
			name:      "management deployment: always uses static flags",
			planeType: "management",
			namespace: "unscoped-ns",
			wantScope: eventScope{Type: "organization", Name: "dev-org"},
			wantAnnotations: map[string]string{
				types.ScopeTypeAnnotation: "organization",
				types.ScopeNameAnnotation: "dev-org",
			},
		},
		{
			name:      "empty plane type: treated as management",
			planeType: "",
			namespace: "scoped-ns",
			wantScope: eventScope{Type: "organization", Name: "dev-org"},
			// The namespace carries upstream labels, but the management path
			// never reads them, so no upstream namespace annotation.
			wantAnnotations: map[string]string{
				types.ScopeTypeAnnotation: "organization",
				types.ScopeNameAnnotation: "dev-org",
			},
		},
		{
			name:      "edge deployment, namespace resolves: uses recovered project scope",
			planeType: planeTypeEdge,
			namespace: "scoped-ns",
			wantScope: eventScope{Type: types.TenantTypeProject, Name: "my-project", Namespace: "default"},
			wantAnnotations: map[string]string{
				types.ScopeTypeAnnotation:      types.TenantTypeProject,
				types.ScopeNameAnnotation:      "my-project",
				types.ScopeNamespaceAnnotation: "default",
			},
		},
		{
			name:         "edge deployment, namespace does not resolve: unscoped",
			planeType:    planeTypeEdge,
			namespace:    "unscoped-ns",
			wantUnscoped: true,
			// Present and empty, not omitted.
			wantAnnotations: map[string]string{
				types.ScopeTypeAnnotation: "",
				types.ScopeNameAnnotation: "",
			},
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
			got := exporter.resolveScope(tt.namespace)
			after := testutil.ToFloat64(unscopedEvents)

			if tt.wantUnscoped {
				if got != (eventScope{}) {
					t.Errorf("resolveScope() = %+v, want the zero eventScope", got)
				}
				if after != before+1 {
					t.Errorf("unscopedEvents counter did not increment: before=%v after=%v", before, after)
				}
			} else {
				if got != tt.wantScope {
					t.Errorf("resolveScope() = %+v, want %+v", got, tt.wantScope)
				}
				if after != before {
					t.Errorf("unscopedEvents counter incremented unexpectedly: before=%v after=%v", before, after)
				}
			}

			// stampScope resolves again, so it must follow every counter check.
			annotations := map[string]string{}
			exporter.stampScope(annotations, tt.namespace)

			if !reflect.DeepEqual(annotations, tt.wantAnnotations) {
				t.Errorf("stampScope() wrote %v, want %v", annotations, tt.wantAnnotations)
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

func TestExporter_Subject(t *testing.T) {
	tests := []struct {
		name      string
		planeType string
		cluster   string
		namespace string
		want      string
	}{
		{
			name:      "edge deployment: cluster token inserted",
			planeType: planeTypeEdge,
			cluster:   "cluster-dfw-1",
			namespace: "default",
			want:      "activity.federated.cluster-dfw-1.default",
		},
		{
			name:      "empty plane type: unqualified",
			namespace: "default",
			want:      "activity.federated.default",
		},
		{
			name:      "cluster set but plane type empty: unqualified",
			cluster:   "cluster-dfw-1",
			namespace: "default",
			want:      "activity.federated.default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exporter := &Exporter{
				subjectPrefix: "activity.federated",
				planeType:     tt.planeType,
				clusterName:   tt.cluster,
			}
			if got := exporter.subject(tt.namespace); got != tt.want {
				t.Errorf("subject() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestFederatedCluster covers the single gate deciding both the subject's
// cluster token and the connection's inbox prefix. If the two were scoped to
// different clusters, the hub's grants would reject one of them.
func TestFederatedCluster(t *testing.T) {
	tests := []struct {
		name      string
		planeType string
		cluster   string
		want      string
	}{
		{name: "edge deployment", planeType: planeTypeEdge, cluster: "cluster-dfw-1", want: "cluster-dfw-1"},
		{name: "management plane: no plane type or cluster", want: ""},
		{name: "cluster set but plane type empty", cluster: "cluster-dfw-1", want: ""},
		{name: "plane type set but cluster empty", planeType: planeTypeEdge, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FederatedCluster(tt.planeType, tt.cluster)
			if got != tt.want {
				t.Fatalf("FederatedCluster() = %q, want %q", got, tt.want)
			}

			// A non-empty cluster must yield a prefix CustomInboxPrefix accepts.
			if got == "" {
				return
			}
			prefix := types.EventInboxPrefix(got)
			if err := nats.CustomInboxPrefix(prefix)(&nats.Options{}); err != nil {
				t.Fatalf("CustomInboxPrefix(%q) rejected: %v", prefix, err)
			}
			if want := "_INBOX_cluster-dfw-1"; prefix != want {
				t.Fatalf("inbox prefix = %q, want %q", prefix, want)
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

// applyNATSOptions resolves an option list the way nats.Connect does.
func applyNATSOptions(t *testing.T, opts []nats.Option) nats.Options {
	t.Helper()
	var o nats.Options
	for _, opt := range opts {
		if err := opt(&o); err != nil {
			t.Fatalf("apply option: %v", err)
		}
	}
	return o
}

func TestNATSOptions(t *testing.T) {
	t.Run("reconnect settings and handlers are always supplied", func(t *testing.T) {
		o := applyNATSOptions(t, natsOptions(Config{}))

		if o.MaxReconnect != -1 {
			t.Errorf("MaxReconnect = %d, want -1", o.MaxReconnect)
		}
		if o.ReconnectWait != 2*time.Second {
			t.Errorf("ReconnectWait = %v, want 2s", o.ReconnectWait)
		}
		if o.DisconnectedErrCB == nil {
			t.Error("DisconnectErrHandler not set")
		}
		if o.ReconnectedCB == nil {
			t.Error("ReconnectHandler not set")
		}
	})

	t.Run("non-federated source uses the default inbox prefix", func(t *testing.T) {
		for _, cfg := range []Config{
			{},
			{PlaneType: planeTypeEdge},
			{ClusterName: "us-central-1-alice"},
		} {
			if got := applyNATSOptions(t, natsOptions(cfg)).InboxPrefix; got != "" {
				t.Errorf("InboxPrefix = %q for %+v, want empty", got, cfg)
			}
		}
	})

	t.Run("federated source pins its per-cluster inbox prefix", func(t *testing.T) {
		cfg := Config{PlaneType: planeTypeEdge, ClusterName: "us-central-1-alice"}

		got := applyNATSOptions(t, natsOptions(cfg)).InboxPrefix
		if want := types.EventInboxPrefix("us-central-1-alice"); got != want {
			t.Errorf("InboxPrefix = %q, want %q", got, want)
		}
	})
}

func TestConnectRejectsUnusableTLSMaterial(t *testing.T) {
	cfg := Config{NATS: natsconn.Endpoint{
		URL: "nats://localhost:4222",
		TLS: natsconn.TLSFiles{Enabled: true, CAFile: filepath.Join(t.TempDir(), "absent.crt")},
	}}

	if _, err := cfg.NATS.Connect(natsClientName, natsOptions(cfg)...); err == nil {
		t.Error("expected an error for a missing CA file")
	}
}
