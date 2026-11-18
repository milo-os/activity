package processor

import "go.miloapis.com/activity/pkg/apis/activity/v1alpha1"

// Test-only exports so external test packages (policytest drift tests) can
// drive the live event path without NATS.

// ResolveKindAndAPIGroupForTest exposes the lookup key derivation used by
// EventProcessor.processMessage.
var ResolveKindAndAPIGroupForTest = resolveKindAndAPIGroup

// NormalizeEventMapForTest exposes the event normalization used by
// EventProcessor.processMessage.
var NormalizeEventMapForTest = normalizeEventMap

// BuildEventActivityForTest calls the live EventProcessor.buildActivity.
func BuildEventActivityForTest(event map[string]any, matched *MatchedPolicy) *v1alpha1.Activity {
	return (&EventProcessor{}).buildActivity(event, matched, matched.Summary, matched.Links)
}
