package eventexporter

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	locationsv1alpha1 "go.miloapis.com/locations/api/v1alpha1"
	"go.miloapis.com/locations/pkg/locationidentity"
)

// locationsScheme is scoped to this resolver alone; the exporter's main
// informer watch uses a plain client-go Clientset, not controller-runtime.
var locationsScheme = runtime.NewScheme()

func init() {
	utilruntime.Must(locationsv1alpha1.AddToScheme(locationsScheme))
}

var locationUnresolved = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Namespace: "event_exporter",
		Name:      "location_unresolved",
		Help:      "1 if this cell's location has not resolved to a delivered ServingLocation, so Source.City is empty; 0 once resolved.",
	},
)

func init() {
	metrics.Registry.MustRegister(locationUnresolved)
}

// locationResolveRetryInterval is how often cityResolver retries while
// unresolved. Every event published during this window carries a
// permanently empty Source.City, since nothing backfills it later, so this
// stays short to minimize that window.
const locationResolveRetryInterval = 30 * time.Second

// locationResolveInterval is the steady-state retry interval once resolved.
// It is not per-event: the informer watch never blocks on this, so a slow
// interval is fine here, kept alive on purpose to pick up a rare
// re-delivered or corrected ServingLocation without a restart.
const locationResolveInterval = 5 * time.Minute

// cityResolver resolves this cell's city code and caches it for the
// publish path to read without a per-event lookup. It never blocks event
// export on resolution: if a ServingLocation hasn't been delivered yet, it
// keeps City() empty and retries on a timer rather than failing startup.
type cityResolver struct {
	reader     client.Reader
	configured locationidentity.LocationConfig

	mu   sync.RWMutex
	city string
}

// newCityResolver builds a cityResolver. locationName is an optional
// fallback Location name, used only until a ServingLocation is delivered to
// this cell (see locationidentity.LocationConfig).
func newCityResolver(reader client.Reader, locationName string) *cityResolver {
	return &cityResolver{
		reader:     reader,
		configured: locationidentity.LocationConfig{Name: locationName},
	}
}

// City returns the cached city code, or "" if not yet resolved.
func (r *cityResolver) City() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.city
}

// Run resolves the cell's city code, retrying fast until it first succeeds
// and slow afterward, so a ServingLocation delivered after startup, or
// corrected later, is picked up without a restart. Blocks until ctx is
// cancelled.
func (r *cityResolver) Run(ctx context.Context) {
	interval := locationResolveRetryInterval
	if r.resolveOnce(ctx) {
		interval = locationResolveInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if r.resolveOnce(ctx) && interval != locationResolveInterval {
				interval = locationResolveInterval
				ticker.Reset(interval)
			}
		}
	}
}

// resolveOnce attempts resolution and reports whether it succeeded.
func (r *cityResolver) resolveOnce(ctx context.Context) bool {
	identity, err := locationidentity.Resolve(ctx, r.reader, r.configured)
	if err != nil {
		locationUnresolved.Set(1)
		klog.ErrorS(err, "Cell location unresolved; Source.City will be empty until it resolves")
		return false
	}

	// Only a delivered ServingLocation carries a local copy of its topology.
	// A configured-only fallback names a Location with nothing local to read.
	if identity.Source != locationidentity.LocationIdentitySourceDelivered {
		locationUnresolved.Set(1)
		klog.V(2).InfoS("Cell has a configured location but no delivered ServingLocation yet; Source.City will be empty",
			"location", identity.Reference.Name)
		return false
	}

	var location locationsv1alpha1.ServingLocation
	if err := r.reader.Get(ctx, client.ObjectKey{Name: identity.Reference.Name}, &location); err != nil {
		locationUnresolved.Set(1)
		klog.ErrorS(err, "Resolved a delivered location but could not read its ServingLocation", "location", identity.Reference.Name)
		return false
	}

	locationUnresolved.Set(0)
	r.mu.Lock()
	r.city = location.CityCode()
	r.mu.Unlock()
	return true
}
