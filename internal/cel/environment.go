package cel

import (
	"fmt"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"

	activitytypes "go.miloapis.com/activity/internal/types"
)

// NewAuditEnvironment creates a CEL environment for audit rule expressions.
// Available variables: audit (map containing all audit fields), actor, actorRef, kind.
// Access audit fields via the audit map: audit.verb, audit.objectRef, audit.user, etc.
// If collector is non-nil, link() calls will capture link information.
func NewAuditEnvironment(collector *linkCollector) (*cel.Env, error) {
	actorRefType := cel.MapType(cel.StringType, cel.DynType)

	return cel.NewEnv(
		// All audit log fields are nested under the "audit" map variable.
		// Access them as: audit.verb, audit.objectRef, audit.user, audit.responseStatus,
		// audit.responseObject, audit.requestObject, etc.
		cel.Variable("audit", cel.MapType(cel.StringType, cel.DynType)),

		// Convenience variables shared between audit and event contexts
		cel.Variable("actor", cel.StringType),
		cel.Variable("actorRef", actorRefType),

		// Also expose "kind" for convenience (extracted from audit.objectRef)
		cel.Variable("kind", cel.StringType),

		// link function declaration with implementation: link(displayText string, resourceRef map) -> string
		// Returns the display text and optionally captures link info in the collector.
		cel.Function("link",
			cel.Overload("link_string_dyn",
				[]*cel.Type{cel.StringType, cel.DynType},
				cel.StringType,
				cel.BinaryBinding(func(displayText, resourceRef ref.Val) ref.Val {
					text := fmt.Sprintf("%v", displayText.Value())
					if collector != nil {
						collector.addLink(text, resourceRef.Value())
					}
					return types.String(text)
				}),
			),
		),
	)
}

// NewEventEnvironment creates a CEL environment for event rule expressions.
// Available variables: event (full Kubernetes event as a map), actor, actorRef.
//
// The event map contains all fields from the events.k8s.io/v1.Event struct.
// Key nested fields:
//   - event.regarding.{kind,name,namespace,apiVersion,uid} — the primary subject
//   - event.related.{kind,name,namespace,apiVersion,uid} — optional secondary object;
//     only present when the event includes a related object (use has(event.related) to check)
//   - event.reason, event.type, event.note, event.action
//   - event.reportingController, event.reportingInstance
//
// If collector is non-nil, link() calls will capture link information.
func NewEventEnvironment(collector *linkCollector) (*cel.Env, error) {
	// The event variable is a map containing the full Kubernetes Event
	eventType := cel.MapType(cel.StringType, cel.DynType)
	// The actorRef variable is a map with {type, name} for linking
	actorRefType := cel.MapType(cel.StringType, cel.DynType)
	// The source variable is a map with {planeType, cluster, region, city}
	// describing where the event originated, populated from the exporter's
	// source-* annotations (see BuildEventSourceVars).
	sourceType := cel.MapType(cel.StringType, cel.StringType)

	return cel.NewEnv(
		cel.Variable("event", eventType),
		cel.Variable("actor", cel.StringType),
		cel.Variable("actorRef", actorRefType),
		cel.Variable("source", sourceType),

		// link function declaration with implementation: link(displayText string, resourceRef map) -> string
		// Returns the display text and optionally captures link info in the collector.
		cel.Function("link",
			cel.Overload("link_string_dyn",
				[]*cel.Type{cel.StringType, cel.DynType},
				cel.StringType,
				cel.BinaryBinding(func(displayText, resourceRef ref.Val) ref.Val {
					text := fmt.Sprintf("%v", displayText.Value())
					if collector != nil {
						collector.addLink(text, resourceRef.Value())
					}
					return types.String(text)
				}),
			),
		),
	)
}

// BuildAuditVars creates the CEL variable map for audit evaluation.
// All audit log fields are nested under the "audit" key for consistency with
// event rules that use the "event" prefix (e.g., event.reason, event.type).
// Convenience variables actor, actorRef, and kind remain top-level.
//
// Nested fields that may be absent from the raw audit log (objectRef, user,
// responseStatus, responseObject, requestObject) are populated with empty maps
// when not present. This ensures that expressions using has() on nested fields
// (e.g. has(audit.objectRef.name)) evaluate safely instead of failing with a
// "no such key" error when the parent map is missing entirely.
func BuildAuditVars(auditMap map[string]interface{}) map[string]interface{} {
	// Copy the original map so we don't mutate the caller's data.
	auditWithDefaults := make(map[string]interface{}, len(auditMap))
	for k, v := range auditMap {
		auditWithDefaults[k] = v
	}

	// Ensure nested map fields exist so has() checks on their sub-fields don't
	// fail at the parent level.
	for _, field := range []string{"objectRef", "user", "responseStatus", "responseObject", "requestObject"} {
		if _, ok := auditWithDefaults[field]; !ok {
			auditWithDefaults[field] = map[string]interface{}{}
		}
	}

	vars := map[string]interface{}{
		"audit":    auditWithDefaults,
		"actor":    ExtractString(auditMap, "user", "username"),
		"actorRef": BuildActorRef(auditMap),
	}

	// Extract kind for top-level convenience (from audit.objectRef.resource)
	if objRef, ok := auditWithDefaults["objectRef"].(map[string]interface{}); ok {
		if kind, ok := objRef["resource"].(string); ok {
			vars["kind"] = kind
		} else {
			vars["kind"] = ""
		}
	} else {
		vars["kind"] = ""
	}

	return vars
}

// BuildEventVars creates the CEL variable map for event evaluation.
func BuildEventVars(eventMap map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"event":    eventMap,
		"actor":    ExtractEventActor(eventMap),
		"actorRef": BuildEventActorRef(eventMap),
		"source":   BuildEventSourceVars(eventMap),
	}
}

// BuildEventSourceVars builds the "source" CEL variable map from an event's
// source-* annotations.
//
// It always returns all four keys, defaulting to "" when the annotation (or
// the annotations map itself) is absent - the source variable must never be
// omitted from the activation. CEL errors on an undefined variable rather
// than evaluating false, so a builder that only emitted "source" when
// annotations were present would break every ActivityPolicy event rule that
// references source.* the moment this variable was declared.
func BuildEventSourceVars(eventMap map[string]interface{}) map[string]interface{} {
	annotations := ExtractMap(eventMap, "metadata", "annotations")
	return map[string]interface{}{
		"planeType": ExtractString(annotations, activitytypes.SourcePlaneTypeAnnotation),
		"cluster":   ExtractString(annotations, activitytypes.SourceClusterAnnotation),
		"region":    ExtractString(annotations, activitytypes.SourceRegionAnnotation),
		"city":      ExtractString(annotations, activitytypes.SourceCityAnnotation),
	}
}

// ExtractString extracts a nested string value from a map.
func ExtractString(m map[string]interface{}, keys ...string) string {
	current := m
	for i, key := range keys {
		if i == len(keys)-1 {
			// Last key - expect string
			if v, ok := current[key].(string); ok {
				return v
			}
			return ""
		}
		// Not last key - expect nested map
		if nested, ok := current[key].(map[string]interface{}); ok {
			current = nested
		} else {
			return ""
		}
	}
	return ""
}

// ExtractMap extracts a nested map from a map using a sequence of keys.
// Returns an empty map if the path doesn't exist or the value is not a map.
func ExtractMap(m map[string]interface{}, keys ...string) map[string]interface{} {
	current := m
	for _, key := range keys {
		if nested, ok := current[key].(map[string]interface{}); ok {
			current = nested
		} else {
			return map[string]interface{}{}
		}
	}
	return current
}

// BuildActorRef builds an actor reference map from audit user info.
// Returns a map with {type, name} structure matching the Activity actor format.
func BuildActorRef(auditMap map[string]interface{}) map[string]interface{} {
	username := ExtractString(auditMap, "user", "username")
	if username == "" {
		return map[string]interface{}{
			"type": "unknown",
			"name": "",
		}
	}

	// Determine actor type based on username pattern
	actorType := "user"
	if strings.HasPrefix(username, "system:serviceaccount:") {
		actorType = "serviceaccount"
	} else if strings.HasPrefix(username, "system:") {
		actorType = "system"
	}

	return map[string]interface{}{
		"type": actorType,
		"name": username,
	}
}

// ExtractEventActor extracts the actor name from a Kubernetes event.
func ExtractEventActor(eventMap map[string]interface{}) string {
	if controller := ExtractString(eventMap, "reportingController"); controller != "" {
		return controller
	}
	return ExtractString(eventMap, "source", "component")
}

// BuildEventActorRef builds an actor reference map from a Kubernetes event.
func BuildEventActorRef(eventMap map[string]interface{}) map[string]interface{} {
	controller := ExtractEventActor(eventMap)
	return map[string]interface{}{
		"type": "controller",
		"name": controller,
	}
}
