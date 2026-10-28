// +k8s:openapi-gen=true
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Activity is a human-readable summary of something that happened in your cluster.
// Think of it as the "what changed and who did it" record that powers activity feeds,
// audit trails, and change history views.
//
// Activities are created automatically from audit logs and Kubernetes events based on
// your ActivityPolicy rules. They're read-only - you query them, not create them.
//
// # Accessing Activities
//
// There are three ways to get activity data, depending on what you need:
//
// | What you need | API to use | Notes |
// | --- | --- | --- |
// | Live feed | GET /activities?watch=true | Streams new activities as they happen. List only returns the last hour. |
// | Search history | POST /activityqueries | Query any time range with filters, search, and pagination. |
// | Filter options | POST /activityfacetqueries | Get values for dropdowns (e.g., "which actors have activities?"). |
//
// # Quick Examples
//
// Watch for new activities:
//
//	kubectl get activities --watch
//
// List recent human-initiated changes:
//
//	kubectl get activities --field-selector spec.changeSource=human
//
// For historical queries or advanced filtering, use ActivityQuery instead.
type Activity struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ActivitySpec `json:"spec"`
}

// ActivitySpec contains the translated activity details.
type ActivitySpec struct {
	// Summary is the human-readable description of what happened.
	// Generated from ActivityPolicy templates.
	//
	// Example: "alice created HTTP proxy api-gateway"
	//
	// +required
	Summary string `json:"summary"`

	// ChangeSource indicates who initiated the change.
	// Used to filter human actions from system reconciliation noise.
	//
	// Values:
	//   - "human": User action via kubectl, API, or UI
	//   - "system": Controller reconciliation, operator actions, scheduled jobs
	//
	// +required
	ChangeSource string `json:"changeSource"`

	// Actor identifies who performed the action.
	//
	// +required
	Actor ActivityActor `json:"actor"`

	// Resource identifies the Kubernetes resource that was affected.
	//
	// +required
	Resource ActivityResource `json:"resource"`

	// Links contains clickable references found in the summary.
	// The portal uses these to make resource names in the summary clickable.
	//
	// +optional
	// +listType=atomic
	Links []ActivityLink `json:"links,omitempty"`

	// Tenant identifies the scope for multi-tenant isolation.
	//
	// +required
	Tenant ActivityTenant `json:"tenant"`

	// Changes contains field-level changes for update/patch operations.
	// Shows old and new values for modified fields.
	//
	// NOTE: This field may be empty in the initial implementation.
	// Populating old values requires resource history lookups.
	//
	// +optional
	// +listType=atomic
	Changes []ActivityChange `json:"changes,omitempty"`

	// Origin identifies the source record for correlation.
	//
	// +required
	Origin ActivityOrigin `json:"origin"`
}

// ActivityActor identifies who performed an action.
type ActivityActor struct {
	// Type indicates the actor category.
	// Values: "user", "serviceaccount", "controller"
	//
	// +required
	Type string `json:"type"`

	// Name is the display name for the actor.
	// For users, this is typically the email address.
	// For service accounts, this is the full name (e.g., "system:serviceaccount:default:my-sa").
	// For controllers, this is the controller name.
	//
	// +required
	Name string `json:"name"`

	// UID is the unique identifier for the actor.
	// Stable across username changes.
	//
	// +optional
	UID string `json:"uid,omitempty"`

	// Email is the actor's email address.
	// Only populated for user actors when available.
	//
	// +optional
	Email string `json:"email,omitempty"`

	// DisplayName is the actor's human-readable name (e.g., "Smith Nelson").
	// For user actors, populated from the iam User's spec.givenName and
	// spec.familyName when available. Empty if no User record is found or the
	// actor is not a human user.
	//
	// UIs SHOULD prefer DisplayName for visible text and use Name/Email/UID
	// only when DisplayName is empty.
	//
	// +optional
	DisplayName string `json:"displayName,omitempty"`
}

// ActivityResource identifies the Kubernetes resource affected by an activity.
type ActivityResource struct {
	// APIGroup is the API group of the resource.
	// Empty string for core API group.
	//
	// +optional
	APIGroup string `json:"apiGroup,omitempty"`

	// APIVersion is the API version of the resource.
	//
	// +required
	APIVersion string `json:"apiVersion"`

	// Kind is the kind of the resource.
	//
	// +required
	Kind string `json:"kind"`

	// Name is the name of the resource.
	//
	// +required
	Name string `json:"name"`

	// Namespace is the namespace of the resource.
	// Empty for cluster-scoped resources.
	//
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// UID is the unique identifier of the resource.
	//
	// +optional
	UID string `json:"uid,omitempty"`
}

// ActivityLink represents a clickable reference in an activity summary.
type ActivityLink struct {
	// Marker is the text substring in the summary that should be linked.
	// The portal scans the summary for this marker and makes it clickable.
	//
	// Example: "HTTP proxy api-gateway"
	//
	// +required
	Marker string `json:"marker"`

	// Resource identifies what the marker links to.
	//
	// +required
	Resource ActivityResource `json:"resource"`

	// DisplayName is the human-readable name for the linked entity, when one
	// is available. Populated server-side for User-typed links from the iam
	// User's givenName + familyName so the UI can render names instead of
	// raw UIDs in the summary.
	//
	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// Email is the email address for the linked entity, when one is
	// available. Populated server-side for User-typed links so the UI can
	// surface the email on hover.
	//
	// +optional
	Email string `json:"email,omitempty"`
}

// ActivityTenant identifies the scope for multi-tenant isolation.
type ActivityTenant struct {
	// Type is the scope level.
	// Values: "global", "organization", "project", "user"
	//
	// +required
	Type string `json:"type"`

	// Name is the tenant identifier within the scope type.
	//
	// +required
	Name string `json:"name"`
}

// ActivityChange represents a field-level change in an update/patch operation.
type ActivityChange struct {
	// Field is the path to the changed field (e.g., "spec.virtualhost.fqdn").
	//
	// +required
	Field string `json:"field"`

	// Old is the previous value. May be empty for new fields.
	//
	// +optional
	Old string `json:"old,omitempty"`

	// New is the new value. May be empty for deleted fields.
	//
	// +optional
	New string `json:"new,omitempty"`
}

// ActivityOrigin identifies the source record for an activity.
type ActivityOrigin struct {
	// Type indicates the source type.
	// Values: "audit" (from audit logs), "event" (from Kubernetes events)
	//
	// +required
	Type string `json:"type"`

	// ID is the correlation ID to the source record.
	// For audit: the auditID from the audit log entry.
	// For event: the metadata.uid of the Kubernetes Event.
	//
	// +required
	ID string `json:"id"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ActivityList contains a list of Activity resources.
type ActivityList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Activity `json:"items"`
}

