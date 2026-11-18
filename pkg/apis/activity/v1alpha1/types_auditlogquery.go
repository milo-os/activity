// +k8s:openapi-gen=true
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
)

// +genclient
// +genclient:nonNamespaced
// +genclient:onlyVerbs=create
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AuditLogQuery searches your control plane's audit logs.
//
// Use this to investigate incidents, track resource changes, generate compliance reports,
// or analyze user activity. Results are returned in the Status field, ordered newest-first.
//
// Quick Start:
//
//	apiVersion: activity.miloapis.com/v1alpha1
//	kind: AuditLogQuery
//	metadata:
//	  name: recent-deletions
//	spec:
//	  startTime: "now-30d"       # last 30 days
//	  endTime: "now"
//	  filter: "verb == 'delete'" # optional: narrow your search
//	  limit: 100
//
// Time Formats:
// - Relative: "now-30d" (great for dashboards and recurring queries)
// - Absolute: "2024-01-01T00:00:00Z" (great for historical analysis)
//
// Datum staff: in organization and project scopes, an event counts as staff
// access when its actor is a Datum staff member who is not a member of that
// organization, or when it produced an activity already recorded as staff
// access. Such events are returned as a minimal event with only apiVersion,
// kind, level, auditID, stage, verb, objectRef (apiGroup, apiVersion,
// resource, subresource, namespace, name, uid), responseStatus.code,
// requestReceivedTimestamp, stageTimestamp and user.username "Datum
// Support". Other events lose impersonatedUser when it is a staff identity.
// Filters on user.username and user.uid see the redacted values, so a staff
// member's identity matches nothing and "Datum Support" matches all such
// events. Until staff memberships are loaded, these queries return 503 with
// Retry-After. Platform and user scopes are not redacted.
type AuditLogQuery struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AuditLogQuerySpec   `json:"spec"`
	Status AuditLogQueryStatus `json:"status,omitempty"`
}

// AuditLogQuerySpec defines the search parameters.
//
// Required: startTime and endTime define your search window.
// Optional: filter (narrow results), limit (page size, default 100), continue (pagination).
//
// Performance: Smaller time ranges and specific filters perform better. The maximum time window
// is typically 30 days. If your range is too large, you'll get an error with guidance on splitting
// your query into smaller chunks.
type AuditLogQuerySpec struct {
	// StartTime is the beginning of your search window (inclusive).
	//
	// Format Options:
	// - Relative: "now-30d", "now-2h", "now-30m" (units: s, m, h, d, w)
	//   Use for dashboards and recurring queries - they adjust automatically.
	// - Absolute: "2024-01-01T00:00:00Z" (RFC3339 with timezone)
	//   Use for historical analysis of specific time periods.
	//
	// Examples:
	//   "now-30d"                     → 30 days ago
	//   "2024-06-15T14:30:00-05:00"   → specific time with timezone offset
	//
	// +required
	StartTime string `json:"startTime"`

	// EndTime is the end of your search window (exclusive).
	//
	// Uses the same formats as StartTime. Commonly "now" for current moment.
	// Must be greater than StartTime.
	//
	// Examples:
	//   "now"                  → current time
	//   "2024-01-02T00:00:00Z" → specific end point
	//
	// +required
	EndTime string `json:"endTime"`

	// Filter narrows results using CEL (Common Expression Language). Leave empty to get all events.
	//
	// Available Fields:
	//   verb               - API action: get, list, create, update, patch, delete, watch
	//   auditID            - unique event identifier
	//   requestReceivedTimestamp - when the API server received the request (RFC3339 timestamp)
	//   user.username      - who made the request (user or service account)
	//   user.uid           - unique user identifier (stable across username changes)
	//   responseStatus.code - HTTP response code (200, 201, 404, 500, etc.)
	//   objectRef.namespace - target resource namespace
	//   objectRef.resource  - resource type (pods, deployments, secrets, configmaps, etc.)
	//   objectRef.name     - specific resource name
	//
	// Operators: ==, !=, <, >, <=, >=, &&, ||, !, in
	// String Functions: startsWith(), endsWith(), contains()
	//
	// Common Patterns:
	//   "verb == 'delete'"                                    - All deletions
	//   "objectRef.namespace == 'production'"                 - Activity in production namespace
	//   "verb in ['create', 'update', 'delete', 'patch']"     - All write operations
	//   "!(verb in ['get', 'list', 'watch'])"                 - Exclude read-only operations
	//   "responseStatus.code >= 400"                          - Failed requests
	//   "user.username.startsWith('system:serviceaccount:')"  - Service account activity
	//   "!user.username.startsWith('system:')"                - Exclude system users
	//   "user.uid == '550e8400-e29b-41d4-a716-446655440000'"  - Specific user by UID
	//   "objectRef.resource == 'secrets'"                     - Secret access
	//   "verb == 'delete' && objectRef.namespace == 'production'" - Production deletions
	//
	// Note: Use single quotes for strings. Field names are case-sensitive.
	// CEL reference: https://cel.dev
	//
	// +optional
	Filter string `json:"filter,omitempty"`

	// Limit sets the maximum number of results per page.
	// Default: 100, Maximum: 1000.
	//
	// Use smaller values (10-50) for exploration, larger (500-1000) for data collection.
	// Use continue to fetch additional pages.
	//
	// +optional
	Limit int32 `json:"limit,omitempty"`

	// Continue is the pagination cursor for fetching additional pages.
	//
	// Leave empty for the first page. If status.continue is non-empty after a query,
	// copy that value here in a new query with identical parameters to get the next page.
	// Repeat until status.continue is empty.
	//
	// Important: Keep all other parameters (startTime, endTime, filter, limit) identical
	// across paginated requests. The cursor is opaque - copy it exactly without modification.
	//
	// +optional
	Continue string `json:"continue,omitempty"`
}

// AuditLogQueryStatus contains the query results and pagination state.
type AuditLogQueryStatus struct {
	// Results contains matching audit events, sorted newest-first.
	//
	// Each event follows the Kubernetes audit.Event format with fields like:
	//   verb, user.username, objectRef.{namespace,resource,name}, requestReceivedTimestamp,
	//   stageTimestamp, responseStatus.code, requestObject, responseObject
	//
	// Empty results? Try broadening your filter or time range.
	// Full documentation: https://kubernetes.io/docs/reference/config-api/apiserver-audit.v1/
	//
	// +listType=atomic
	Results []auditv1.Event `json:"results,omitempty"`

	// Continue is the pagination cursor.
	// Non-empty means more results are available - copy this to spec.continue for the next page.
	// Empty means you have all results.
	Continue string `json:"continue,omitempty"`

	// EffectiveStartTime is the actual start time used for this query (RFC3339 format).
	//
	// When you use relative times like "now-7d", this shows the exact timestamp that was
	// calculated. Useful for understanding exactly what time range was queried, especially
	// for auditing, debugging, or recreating queries with absolute timestamps.
	//
	// Example: If you query with startTime="now-7d" at 2025-12-17T12:00:00Z,
	// this will be "2025-12-10T12:00:00Z".
	//
	// +optional
	EffectiveStartTime string `json:"effectiveStartTime,omitempty"`

	// EffectiveEndTime is the actual end time used for this query (RFC3339 format).
	//
	// When you use relative times like "now", this shows the exact timestamp that was
	// calculated. Useful for understanding exactly what time range was queried.
	//
	// Example: If you query with endTime="now" at 2025-12-17T12:00:00Z,
	// this will be "2025-12-17T12:00:00Z".
	//
	// +optional
	EffectiveEndTime string `json:"effectiveEndTime,omitempty"`
}

