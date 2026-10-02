package eventexporter

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	locationsv1alpha1 "go.miloapis.com/locations/api/v1alpha1"
)

func newServingLocation(name, cityCode string) *locationsv1alpha1.ServingLocation {
	return &locationsv1alpha1.ServingLocation{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: locationsv1alpha1.ServingLocationSpec{
			Topology: map[string]string{
				locationsv1alpha1.TopologyCityCodeKey: cityCode,
			},
		},
	}
}

func TestCityResolver(t *testing.T) {
	tests := []struct {
		name           string
		locationName   string
		delivered      []*locationsv1alpha1.ServingLocation
		wantCity       string
		wantUnresolved bool
	}{
		{
			name:           "nothing delivered, no configured fallback: unresolved",
			wantCity:       "",
			wantUnresolved: true,
		},
		{
			name:           "one delivered ServingLocation: resolves its city",
			delivered:      []*locationsv1alpha1.ServingLocation{newServingLocation("us-central-1", "DFW")},
			wantCity:       "DFW",
			wantUnresolved: false,
		},
		{
			name:           "configured fallback but nothing delivered: no local topology to read",
			locationName:   "us-central-1",
			wantCity:       "",
			wantUnresolved: true,
		},
		{
			name: "two delivered ServingLocations, no configured fallback: ambiguous",
			delivered: []*locationsv1alpha1.ServingLocation{
				newServingLocation("us-central-1", "DFW"),
				newServingLocation("us-east-1", "IAD"),
			},
			wantCity:       "",
			wantUnresolved: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(locationsScheme)
			for _, sl := range tt.delivered {
				builder = builder.WithObjects(sl)
			}
			fakeClient := builder.Build()

			resolver := newCityResolver(fakeClient, tt.locationName)
			gotResolved := resolver.resolveOnce(context.Background())

			if gotResolved == tt.wantUnresolved {
				t.Errorf("resolveOnce() = %v, want resolved=%v", gotResolved, !tt.wantUnresolved)
			}
			if got := resolver.City(); got != tt.wantCity {
				t.Errorf("City() = %q, want %q", got, tt.wantCity)
			}
			if got := testutil.ToFloat64(locationUnresolved); (got == 1) != tt.wantUnresolved {
				t.Errorf("locationUnresolved gauge = %v, want unresolved=%v", got, tt.wantUnresolved)
			}
		})
	}
}

func TestCityResolver_RetriesAfterInitialFailure(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(locationsScheme).Build()
	resolver := newCityResolver(fakeClient, "")

	if resolver.resolveOnce(context.Background()) {
		t.Fatal("resolveOnce() = true, want false before delivery")
	}
	if got := resolver.City(); got != "" {
		t.Fatalf("City() = %q, want empty before delivery", got)
	}

	if err := fakeClient.Create(context.Background(), newServingLocation("us-central-1", "DFW")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if !resolver.resolveOnce(context.Background()) {
		t.Fatal("resolveOnce() = false, want true after delivery")
	}
	if got := resolver.City(); got != "DFW" {
		t.Errorf("City() = %q, want %q after delivery", got, "DFW")
	}
}
