package processor

import (
	"fmt"
	"strings"
	"time"

	authnv1 "k8s.io/api/authentication/v1"

	"go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/internal/types"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// Re-export tenant type constants for use within the processor package.
const (
	TenantTypePlatform     = types.TenantTypePlatform
	TenantTypeOrganization = types.TenantTypeOrganization
	TenantTypeProject      = types.TenantTypeProject
	TenantTypeUser         = types.TenantTypeUser
)

// KindResolver resolves a plural resource name to its Kind using API discovery.
// Returns error if resolution fails.
type KindResolver func(apiGroup, resource string) (string, error)

// GetNestedString extracts a string from a map, supporting nested access with multiple keys.
func GetNestedString(m map[string]any, keys ...string) string {
	if m == nil || len(keys) == 0 {
		return ""
	}

	current := m
	for i, key := range keys {
		if i == len(keys)-1 {
			if v, ok := current[key].(string); ok {
				return v
			}
			return ""
		}
		if nested, ok := current[key].(map[string]any); ok {
			current = nested
		} else {
			return ""
		}
	}
	return ""
}

// ExtractTenant extracts tenant information from user extra fields.
func ExtractTenant(user authnv1.UserInfo) v1alpha1.ActivityTenant {
	tenant := v1alpha1.ActivityTenant{
		Type: TenantTypePlatform,
		Name: "",
	}

	// Look for parent type/name in extra fields
	if parentType := getExtraValue(user.Extra, "iam.miloapis.com/parent-type"); parentType != "" {
		tenant.Type = parentType
	}

	if parentName := getExtraValue(user.Extra, "iam.miloapis.com/parent-name"); parentName != "" {
		tenant.Name = parentName
	}

	// Check for organization (alternative/legacy field)
	if tenant.Type == TenantTypePlatform {
		if org := getExtraValue(user.Extra, "organization"); org != "" {
			tenant.Type = TenantTypeOrganization
			tenant.Name = org
		}
	}

	// Check for project (more specific than organization, legacy field)
	if project := getExtraValue(user.Extra, "project"); project != "" {
		tenant.Type = TenantTypeProject
		tenant.Name = project
	}

	return tenant
}

// getExtraValue extracts the first value from a UserInfo extra field.
func getExtraValue(extra map[string]authnv1.ExtraValue, key string) string {
	if values, ok := extra[key]; ok && len(values) > 0 {
		return values[0]
	}
	return ""
}

// ConvertLinks converts CEL links to Activity links.
// If resolveKind is provided, it will be used to resolve plural resource names to Kind.
// Returns error if kind resolution fails.
func ConvertLinks(celLinks []cel.Link, resolveKind KindResolver) ([]v1alpha1.ActivityLink, error) {
	if len(celLinks) == 0 {
		return nil, nil
	}

	links := make([]v1alpha1.ActivityLink, len(celLinks))
	for i, l := range celLinks {
		kind := getStringFromMap(l.Resource, "kind")
		apiGroup := getStringFromMap(l.Resource, "apiGroup")

		// Fallback 1: if kind is empty, try to get it from the "resource" field
		// This handles Kubernetes audit objectRef which has "resource" (plural) instead of "kind"
		if kind == "" {
			if resource := getStringFromMap(l.Resource, "resource"); resource != "" {
				if resolveKind != nil {
					resolvedKind, err := resolveKind(apiGroup, resource)
					if err != nil {
						return nil, fmt.Errorf("%w: resource %q in apiGroup %q: %v", ErrKindResolution, resource, apiGroup, err)
					}
					kind = resolvedKind
				}
			}
		}

		// Fallback 2: if still empty, try to get it from the "type" field
		// This handles actorRef which has {type, name} structure
		if kind == "" {
			kind = getStringFromMap(l.Resource, "type")
		}

		// Extract name/namespace/uid: top-level first, then nested metadata
		// for full Kubernetes resource objects (e.g. responseObject).
		name := getStringFromMap(l.Resource, "name")
		if name == "" {
			name = GetNestedString(l.Resource, "metadata", "name")
		}

		namespace := getStringFromMap(l.Resource, "namespace")
		if namespace == "" {
			namespace = GetNestedString(l.Resource, "metadata", "namespace")
		}

		uid := getStringFromMap(l.Resource, "uid")
		if uid == "" {
			uid = GetNestedString(l.Resource, "metadata", "uid")
		}

		// Parse combined apiVersion (e.g. "dns.networking.miloapis.com/v1alpha1")
		// to extract apiGroup and version separately when apiGroup is absent.
		rawAPIVersion := getStringFromMap(l.Resource, "apiVersion")
		apiVersion := rawAPIVersion
		if apiGroup == "" && rawAPIVersion != "" {
			if idx := strings.Index(rawAPIVersion, "/"); idx != -1 {
				apiGroup = rawAPIVersion[:idx]
				apiVersion = rawAPIVersion[idx+1:]
			}
		}

		links[i] = v1alpha1.ActivityLink{
			Marker: l.Marker,
			Resource: v1alpha1.ActivityResource{
				APIGroup:   apiGroup,
				APIVersion: apiVersion,
				Kind:       kind,
				Name:       name,
				Namespace:  namespace,
				UID:        uid,
			},
		}
	}
	return links, nil
}

// getStringFromMap safely extracts a string from a map (for cel.Link.Resource).
func getStringFromMap(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

const (
	// scopeTypeAnnotation is the annotation key carrying the tenant scope type.
	scopeTypeAnnotation = "platform.miloapis.com/scope.type"
	// scopeNameAnnotation is the annotation key carrying the tenant scope name.
	scopeNameAnnotation = "platform.miloapis.com/scope.name"
)

// ExtractTenantFromAnnotations reads scope annotations from event metadata and
// returns the corresponding ActivityTenant. Falls back to platform scope when
// the type annotation is absent or empty.
func ExtractTenantFromAnnotations(eventMap map[string]any) v1alpha1.ActivityTenant {
	tenant := v1alpha1.ActivityTenant{
		Type: TenantTypePlatform,
		Name: "",
	}

	if eventMap == nil {
		return tenant
	}

	metadata, ok := eventMap["metadata"].(map[string]any)
	if !ok {
		return tenant
	}

	annotations, ok := metadata["annotations"].(map[string]any)
	if !ok {
		return tenant
	}

	scopeType := getStringFromMap(annotations, scopeTypeAnnotation)
	scopeName := getStringFromMap(annotations, scopeNameAnnotation)

	if scopeType != "" {
		tenant.Type = scopeType
		tenant.Name = scopeName
	}

	return tenant
}

// ResolveInvolvedObject extracts an event's subject object, preferring the
// modern "regarding" field (events.k8s.io/v1) over the legacy "involvedObject"
// field (core/v1). Shared by every Event-to-Activity code path to prevent
// divergence.
func ResolveInvolvedObject(event map[string]interface{}) map[string]interface{} {
	if regarding, ok := event["regarding"].(map[string]interface{}); ok {
		return regarding
	}
	if involvedObject, ok := event["involvedObject"].(map[string]interface{}); ok {
		return involvedObject
	}
	return nil
}

// resolveEventTimestamp extracts a timestamp from an event map, trying in
// order: eventTime, lastTimestamp, firstTimestamp, metadata.creationTimestamp,
// then now(). A candidate must parse and be non-zero, or it falls through to
// the next one.
func resolveEventTimestamp(event map[string]interface{}) time.Time {
	if ts := getStringFromMap(event, "eventTime"); ts != "" {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil && !t.IsZero() {
			return t
		}
	}
	if ts := getStringFromMap(event, "lastTimestamp"); ts != "" {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil && !t.IsZero() {
			return t
		}
	}
	if ts := getStringFromMap(event, "firstTimestamp"); ts != "" {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil && !t.IsZero() {
			return t
		}
	}
	if metadata, ok := event["metadata"].(map[string]interface{}); ok {
		if ts := getStringFromMap(metadata, "creationTimestamp"); ts != "" {
			if t, err := time.Parse(time.RFC3339Nano, ts); err == nil && !t.IsZero() {
				return t
			}
		}
	}
	return time.Now()
}

// resolveActorFromEvent resolves an event's acting controller: reportingController,
// then the legacy source.component, then "unknown". Always attributed as a
// controller actor.
func resolveActorFromEvent(event map[string]interface{}) v1alpha1.ActivityActor {
	reportingController := getStringFromMap(event, "reportingController")

	if reportingController == "" {
		if source, ok := event["source"].(map[string]interface{}); ok {
			reportingController = getStringFromMap(source, "component")
		}
	}

	if reportingController == "" {
		reportingController = "unknown"
	}

	return v1alpha1.ActivityActor{
		Type: ActorTypeController,
		Name: reportingController,
	}
}

// eventActivityLabels builds the standard label set for an Activity generated
// from a Kubernetes event, shared across all Event-to-Activity code paths.
func eventActivityLabels(changeSource, apiGroup, kind, eventReason string) map[string]string {
	return map[string]string{
		"activity.miloapis.com/origin-type":   "event",
		"activity.miloapis.com/change-source": changeSource,
		"activity.miloapis.com/api-group":     apiGroup,
		"activity.miloapis.com/resource-kind": kind,
		"activity.miloapis.com/event-reason":  eventReason,
	}
}
