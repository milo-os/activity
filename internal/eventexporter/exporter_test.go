package eventexporter

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
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
