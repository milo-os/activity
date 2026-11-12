// +k8s:openapi-gen=true
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +genclient:onlyVerbs=create
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AuditLogFacetsQuery is an ephemeral resource for getting distinct field values from audit logs.
// Use this to power autocomplete, filter dropdowns, and faceted search in UIs for audit log queries.
//
// The query returns counts for each distinct value, allowing you to show both
// available options and their frequency.
//
// Example:
//
//	apiVersion: activity.miloapis.com/v1alpha1
//	kind: AuditLogFacetsQuery
//	metadata:
//	  name: get-verbs
//	spec:
//	  timeRange:
//	    start: "now-7d"
//	  facets:
//	    - field: verb
//	      limit: 10
//	    - field: objectRef.resource
//
// In organization and project scopes, Datum staff who are not members of the
// organization are counted under a single user.username value, "Datum
// Support" (and an empty user.uid), with the same redaction as AuditLogQuery.
type AuditLogFacetsQuery struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AuditLogFacetsQuerySpec   `json:"spec"`
	Status AuditLogFacetsQueryStatus `json:"status,omitempty"`
}

// AuditLogFacetsQuerySpec defines which facets to retrieve from audit logs.
type AuditLogFacetsQuerySpec struct {
	// TimeRange limits the time window for facet aggregation.
	// If not specified, defaults to the last 7 days.
	//
	// +optional
	TimeRange FacetTimeRange `json:"timeRange,omitempty"`

	// Filter narrows the audit logs before computing facets using CEL.
	// This allows you to get facet values for a subset of audit logs.
	//
	// Available Fields:
	//   verb               - API action: get, list, create, update, patch, delete, watch
	//   user.username      - who made the request (user or service account)
	//   user.uid           - unique user identifier
	//   responseStatus.code - HTTP response code (200, 201, 404, 500, etc.)
	//   objectRef.namespace - target resource namespace
	//   objectRef.resource  - resource type (pods, deployments, secrets, configmaps, etc.)
	//   objectRef.apiGroup  - API group of the resource
	//   objectRef.name     - specific resource name
	//
	// Operators: ==, !=, <, >, <=, >=, &&, ||, !, in
	// String Functions: startsWith(), endsWith(), contains()
	//
	// Examples:
	//   "verb in ['create', 'update', 'delete']"        - Facets for write operations only
	//   "!(verb in ['get', 'list', 'watch'])"           - Exclude read-only operations
	//   "!user.username.startsWith('system:')"          - Exclude system users
	//   "objectRef.namespace == 'production'"           - Facets for production namespace
	//
	// +optional
	Filter string `json:"filter,omitempty"`

	// Facets specifies which fields to get distinct values for.
	// Each facet returns the top N values with counts.
	//
	// Supported fields:
	//   - verb: API action (get, list, create, update, patch, delete, watch)
	//   - user.username: Actor display names
	//   - user.uid: Unique user identifiers
	//   - responseStatus.code: HTTP response codes
	//   - objectRef.namespace: Namespaces
	//   - objectRef.resource: Resource types
	//   - objectRef.apiGroup: API groups
	//
	// +required
	// +listType=atomic
	Facets []FacetSpec `json:"facets"`
}

// AuditLogFacetsQueryStatus contains the facet results.
type AuditLogFacetsQueryStatus struct {
	// Facets contains the results for each requested facet.
	//
	// +optional
	// +listType=atomic
	Facets []FacetResult `json:"facets,omitempty"`
}
