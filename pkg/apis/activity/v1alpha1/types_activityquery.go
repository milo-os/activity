// +k8s:openapi-gen=true
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +genclient:onlyVerbs=create
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ActivityQuery searches for activity records.
//
// Activities are human-readable summaries of resource changes, generated from audit logs
// and Kubernetes events. Use this to display activity feeds, investigate changes, or
// filter by actor, resource type, or time range.
//
// Quick Start:
//
//	apiVersion: activity.miloapis.com/v1alpha1
//	kind: ActivityQuery
//	metadata:
//	  name: recent-human-changes
//	spec:
//	  startTime: "now-7d"
//	  endTime: "now"
//	  filter: "spec.changeSource == 'human'"  # filter out system noise
//	  limit: 50
//
// Time Formats:
// - Relative: "now-7d", "now-2h" (great for dashboards)
// - Absolute: "2024-01-01T00:00:00Z" (great for historical analysis)
type ActivityQuery struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ActivityQuerySpec   `json:"spec"`
	Status ActivityQueryStatus `json:"status,omitempty"`
}

// ActivityQuerySpec defines the search parameters for activities.
//
// Required: startTime and endTime define your search window.
// Optional: filter (CEL expression), search, limit, continue.
//
// CEL is the primary filtering mechanism. All dedicated filter fields have been
// removed in favor of the expressive filter field.
//
// Available CEL Fields:
//
//	spec.changeSource      - "human" or "system"
//	spec.actor.name        - who performed the action
//	spec.actor.type        - "user", "serviceaccount", "controller"
//	spec.actor.uid         - actor's unique identifier
//	spec.resource.apiGroup - resource API group (empty for core)
//	spec.resource.kind     - resource kind (Deployment, Pod, etc.)
//	spec.resource.name     - resource name
//	spec.resource.namespace - resource namespace
//	spec.resource.uid      - resource UID
//	spec.summary           - activity summary text
//	spec.origin.type       - "audit" or "event"
//	spec.source.planeType  - plane the record originated from (management, edge)
//	spec.source.cluster    - cluster the record originated from
//	spec.source.region     - region the record originated from
//	spec.source.city       - city the record originated from
//	spec.relatedUIDs       - UIDs of the resource, its related resources, and
//	                         linked resources (list; use '<uid>' in spec.relatedUIDs)
//	metadata.namespace     - activity namespace
//
// CEL Filter Examples:
//
//	"spec.changeSource == 'human'"
//	"spec.resource.kind == 'Deployment'"
//	"spec.actor.name.contains('admin')"
//	"spec.resource.kind in ['Deployment', 'StatefulSet']"
//	"spec.resource.apiGroup == 'networking.datumapis.com'"
//	"spec.actor.uid == 'abc123'"
//	"'abc123' in spec.relatedUIDs && spec.source.region == 'us-east-1'"
type ActivityQuerySpec struct {
	// StartTime is the beginning of your search window (inclusive).
	//
	// Format Options:
	// - Relative: "now-7d", "now-2h", "now-30m" (units: s, m, h, d, w)
	// - Absolute: "2024-01-01T00:00:00Z" (RFC3339 with timezone)
	//
	// +required
	StartTime string `json:"startTime"`

	// EndTime is the end of your search window (exclusive).
	//
	// Uses the same formats as StartTime. Commonly "now" for current moment.
	// Must be greater than StartTime.
	//
	// +required
	EndTime string `json:"endTime"`

	// Filter narrows results using CEL (Common Expression Language).
	//
	// This is the primary filtering mechanism. See the ActivityQuerySpec godoc
	// for available fields and examples.
	//
	// Operators: ==, !=, &&, ||, !, in
	// String Functions: startsWith(), endsWith(), contains()
	//
	// +optional
	Filter string `json:"filter,omitempty"`

	// Search performs full-text search on activity summaries.
	//
	// Example: "created deployment" matches activities with those words in the summary.
	//
	// +optional
	Search string `json:"search,omitempty"`

	// Limit sets the maximum number of results per page.
	// Default: 100, Maximum: 1000.
	//
	// +optional
	Limit int32 `json:"limit,omitempty"`

	// Continue is the pagination cursor for fetching additional pages.
	//
	// Leave empty for the first page. Copy status.continue here to get the next page.
	// Keep all other parameters identical across paginated requests.
	//
	// +optional
	Continue string `json:"continue,omitempty"`
}

// ActivityQueryStatus contains the query results and pagination state.
type ActivityQueryStatus struct {
	// Results contains matching activities, sorted newest-first.
	//
	// +listType=atomic
	Results []Activity `json:"results,omitempty"`

	// Continue is the pagination cursor.
	// Non-empty means more results are available.
	Continue string `json:"continue,omitempty"`

	// EffectiveStartTime is the actual start time used (RFC3339 format).
	// Shows the resolved timestamp when relative times are used.
	//
	// +optional
	EffectiveStartTime string `json:"effectiveStartTime,omitempty"`

	// EffectiveEndTime is the actual end time used (RFC3339 format).
	// Shows the resolved timestamp when relative times are used.
	//
	// +optional
	EffectiveEndTime string `json:"effectiveEndTime,omitempty"`
}
