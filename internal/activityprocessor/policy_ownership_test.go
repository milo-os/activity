package activityprocessor

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPolicyOwnershipLifecycle(t *testing.T) {
	cache := NewPolicyCache()
	registry := prometheus.NewPedanticRegistry()
	registry.MustRegister(cache)
	policy := &v1alpha1.ActivityPolicy{ObjectMeta: metav1.ObjectMeta{Name: "httpproxy", Labels: map[string]string{
		policyServiceLabel: "network-services", policyTeamLabel: "delivery", "unrelated": "not-exported",
	}}}
	check := func(service, team string) {
		t.Helper()
		expected := `# HELP activity_processor_policy_info Ownership of an active ActivityPolicy (value 1).
# TYPE activity_processor_policy_info gauge
activity_processor_policy_info{policy_name="httpproxy",policy_service="` + service + `",policy_team="` + team + `"} 1
`
		if err := testutil.GatherAndCompare(registry, strings.NewReader(expected), "activity_processor_policy_info"); err != nil {
			t.Fatal(err)
		}
	}
	if err := cache.Add(policy, "httpproxies"); err != nil {
		t.Fatal(err)
	}
	check("network-services", "delivery")
	updated := policy.DeepCopy()
	updated.Labels[policyServiceLabel] = "dns"
	updated.Labels[policyTeamLabel] = "platform"
	if err := cache.Update(policy, updated, "httpproxies", "httpproxies"); err != nil {
		t.Fatal(err)
	}
	check("dns", "platform")
	unlabeled := updated.DeepCopy()
	unlabeled.Labels = nil
	if err := cache.Update(updated, unlabeled, "httpproxies", "httpproxies"); err != nil {
		t.Fatal(err)
	}
	check("", "")
	cache.Remove(unlabeled, "httpproxies")
	if err := testutil.GatherAndCompare(registry, strings.NewReader(""), "activity_processor_policy_info"); err != nil {
		t.Fatal(err)
	}
}
