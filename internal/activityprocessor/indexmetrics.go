package activityprocessor

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// indexMetrics are the readiness metrics of the enrichment indexes (the iam
// User index and the staff disclosure indexes), named for the component that
// runs them: activity_processor_* in the processor, activity_apiserver_* in
// the API server.
type indexMetrics struct {
	userSynced, userHealthy, userLastSync    prometheus.Gauge
	staffSynced, staffHealthy, staffLastSync *prometheus.GaugeVec
}

func newIndexMetrics(namespace string) *indexMetrics {
	return &indexMetrics{
		userSynced: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "user_index_synced",
			Help:      "1 once the iam User index has completed its initial sync and activities are enriched with actor display names, 0 before that",
		}),
		userHealthy: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "user_index_healthy",
			Help:      "1 when the iam User index's last list succeeded and no list/watch error has occurred since, 0 otherwise. While 0 after a sync, the last-known index keeps being served",
		}),
		userLastSync: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "user_index_last_sync_timestamp_seconds",
			Help:      "Unix time of the iam User index's last successful list",
		}),
		staffSynced: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "staff_index_synced",
			Help: "1 once a staff disclosure index (staff group memberships, organization memberships, projects) has synced. " +
				"In the required staff redaction mode, the processor does not process events and the API server answers " +
				"tenant-scoped audit log queries with 503 until all three have synced",
		}, []string{"index"}),
		staffHealthy: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "staff_index_healthy",
			Help:      "1 when a staff disclosure index's last list succeeded and no list/watch error has occurred since; while 0 after a sync, the last-known index keeps being served",
		}, []string{"index"}),
		staffLastSync: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "staff_index_last_sync_timestamp_seconds",
			Help:      "Unix time of a staff disclosure index's last successful list",
		}, []string{"index"}),
	}
}

func (m *indexMetrics) userGauges() indexGauges {
	return indexGauges{synced: m.userSynced, healthy: m.userHealthy, lastSync: m.userLastSync}
}

func (m *indexMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{m.userSynced, m.userHealthy, m.userLastSync, m.staffSynced, m.staffHealthy, m.staffLastSync}
}

var (
	indexMetricsMu sync.Mutex
	// activeIndexMetrics defaults to the processor's names.
	activeIndexMetrics = newIndexMetrics("activity_processor")
)

func currentIndexMetrics() *indexMetrics {
	indexMetricsMu.Lock()
	defer indexMetricsMu.Unlock()
	return activeIndexMetrics
}

// UseIndexMetricsNamespace names the index metrics for another component
// (for example "activity_apiserver") and returns the collectors to register.
// Call it before creating any index.
func UseIndexMetricsNamespace(namespace string) []prometheus.Collector {
	indexMetricsMu.Lock()
	defer indexMetricsMu.Unlock()
	activeIndexMetrics = newIndexMetrics(namespace)
	return activeIndexMetrics.collectors()
}
