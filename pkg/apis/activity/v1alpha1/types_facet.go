// +k8s:openapi-gen=true
package v1alpha1

// FacetTimeRange specifies the time window for facet queries.
type FacetTimeRange struct {
	// Start is the beginning of the time window (inclusive).
	// Supports RFC3339 timestamps and relative times (e.g., "now-7d").
	//
	// +optional
	Start string `json:"start,omitempty"`

	// End is the end of the time window (exclusive).
	// Supports RFC3339 timestamps and relative times. Defaults to "now".
	//
	// +optional
	End string `json:"end,omitempty"`
}

// FacetSpec defines a single facet to retrieve.
type FacetSpec struct {
	// Field is the activity field path to get distinct values for.
	//
	// Supported fields:
	//   - spec.actor.name: Actor display names
	//   - spec.actor.type: Actor types (user, serviceaccount, system, controller, staff)
	//   - spec.resource.apiGroup: API groups
	//   - spec.resource.kind: Resource kinds
	//   - spec.resource.namespace: Namespaces
	//   - spec.changeSource: Change sources (human, system)
	//   - spec.interface.type: How changes were made, as reported by the client
	//
	// +required
	Field string `json:"field"`

	// Limit is the maximum number of distinct values to return.
	// Default: 20, Maximum: 100.
	//
	// +optional
	Limit int32 `json:"limit,omitempty"`
}

// FacetResult contains the distinct values for a single facet.
type FacetResult struct {
	// Field is the field path that was queried.
	Field string `json:"field"`

	// Values contains the distinct values and their counts.
	//
	// +optional
	// +listType=atomic
	Values []FacetValue `json:"values,omitempty"`
}

// FacetValue represents a single distinct value with its occurrence count.
type FacetValue struct {
	// Value is the distinct field value.
	Value string `json:"value"`

	// Count is the number of activities with this value.
	Count int64 `json:"count"`
}
