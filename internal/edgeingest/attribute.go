package edgeingest

import (
	authnv1 "k8s.io/api/authentication/v1"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.miloapis.com/activity/internal/types"
)

// Tenancy and provenance keys stamped onto an edge audit event.
//
// Two of these carry the same fact, and both are load-bearing. Milo's audit
// filter records tenancy as annotations, which is what ClickHouse materializes
// scope_type and scope_name from and what every query scopes against; the
// activity processor instead reads it from user extras when it builds an
// Activity. Writing only one of the pair leaves the record correctly scoped in
// exactly one of the two systems and silently misfiled in the other.
const (
	// ScopeTypeAnnotation matches Milo's filters.ScopeTypeKey.
	ScopeTypeAnnotation = "platform.miloapis.com/scope.type"

	// ScopeNameAnnotation matches Milo's filters.ScopeNameKey.
	ScopeNameAnnotation = "platform.miloapis.com/scope.name"

	// ParentTypeExtraKey matches Milo's filters.ParentTypeExtraKey and the key
	// processor.ExtractTenant reads.
	ParentTypeExtraKey = "iam.miloapis.com/parent-type"

	// ParentNameExtraKey matches Milo's filters.ParentNameExtraKey and the key
	// processor.ExtractTenant reads.
	ParentNameExtraKey = "iam.miloapis.com/parent-name"

	// LocationAnnotation records where an edge record was produced. The value
	// is opaque to Activity: it is stored and queried as a string, with no
	// dependency on any locations service.
	LocationAnnotation = "locations.miloapis.com/location"

	// SourceClusterAnnotation records the edge cluster a record arrived from,
	// taken from the transport credential rather than the payload.
	SourceClusterAnnotation = "activity.miloapis.com/source-cluster"
)

// ScopeTypeGlobal is the scope type Milo's audit filter writes for a request
// with no tenant parent. Edge records that resolve to no project use the same
// value, so platform-scoped rows look identical whichever plane produced them.
const ScopeTypeGlobal = "global"

// AttributeToProject scopes an event to a project, writing both the annotation
// pair the storage layer reads and the user extras the activity processor
// reads.
func AttributeToProject(event *auditv1.Event, projectName string) {
	setAnnotation(event, ScopeTypeAnnotation, types.TenantTypeProject)
	setAnnotation(event, ScopeNameAnnotation, projectName)
	setUserExtra(event, ParentTypeExtraKey, types.TenantTypeProject)
	setUserExtra(event, ParentNameExtraKey, projectName)
}

// AttributeToPlatform scopes an event to the platform. Tenant extras are
// cleared rather than left alone, because an edge shipper must not be able to
// promote its own records into a tenant by putting extras on the wire.
func AttributeToPlatform(event *auditv1.Event) {
	setAnnotation(event, ScopeTypeAnnotation, ScopeTypeGlobal)
	delete(event.Annotations, ScopeNameAnnotation)
	delete(event.User.Extra, ParentTypeExtraKey)
	delete(event.User.Extra, ParentNameExtraKey)
}

// StampLocation records the cluster and location an event came from.
func StampLocation(event *auditv1.Event, identity ClusterIdentity) {
	setAnnotation(event, LocationAnnotation, identity.Location)
	setAnnotation(event, SourceClusterAnnotation, identity.Name)
}

func setAnnotation(event *auditv1.Event, key, value string) {
	if event.Annotations == nil {
		event.Annotations = map[string]string{}
	}
	event.Annotations[key] = value
}

func setUserExtra(event *auditv1.Event, key, value string) {
	if event.User.Extra == nil {
		event.User.Extra = map[string]authnv1.ExtraValue{}
	}
	event.User.Extra[key] = authnv1.ExtraValue{value}
}
