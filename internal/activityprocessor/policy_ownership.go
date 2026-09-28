package activityprocessor

import "github.com/prometheus/client_golang/prometheus"

// Ownership labels are explicit policy metadata, independent of the service
// that hosts the processor. Only these two labels are exposed as metrics.
const (
	policyServiceLabel = "meta.datumapis.com/service"
	policyTeamLabel    = "meta.datumapis.com/team"
)

var policyInfoDesc = prometheus.NewDesc(
	"activity_processor_policy_info",
	"Ownership of an active ActivityPolicy (value 1).",
	[]string{"policy_name", "policy_service", "policy_team"}, nil,
)

func (c *PolicyCache) Describe(ch chan<- *prometheus.Desc) {
	ch <- policyInfoDesc
}

// Collect reads current metadata rather than copying owners into queued events.
// Ownership updates and deletions therefore apply to existing retry failures.
func (c *PolicyCache) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, policies := range c.policies {
		for _, policy := range policies {
			labels := policy.OriginalPolicy.Labels
			ch <- prometheus.MustNewConstMetric(policyInfoDesc, prometheus.GaugeValue, 1,
				policy.Name, labels[policyServiceLabel], labels[policyTeamLabel])
		}
	}
}
