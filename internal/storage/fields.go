package storage

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// AuditLogFacetFields defines the supported fields for audit log facet queries.
// Keys are API field paths (as used in queries), values are human-readable descriptions.
var AuditLogFacetFields = map[string]string{
	"verb":                "The API verb (get, list, create, update, delete, etc.)",
	"user.username":       "The username of the actor",
	"user.uid":            "The UID of the actor",
	"responseStatus.code": "The HTTP response status code",
	"objectRef.namespace": "The namespace of the target object",
	"objectRef.resource":  "The resource type",
	"objectRef.apiGroup":  "The API group of the target resource",
	"location":            "The location the request was served from, for records originating outside the core control plane",
}

// IsValidAuditLogFacetField checks if a field is supported for audit log faceting.
func IsValidAuditLogFacetField(field string) bool {
	_, ok := AuditLogFacetFields[field]
	return ok
}

// AuditLogFacetFieldNames returns a sorted list of supported audit log facet field names.
func AuditLogFacetFieldNames() []string {
	return sortedKeys(AuditLogFacetFields)
}

// FormatSupportedFields returns a comma-separated string of supported field names for error messages.
func FormatSupportedFields(fields map[string]string) string {
	names := sortedKeys(fields)
	return strings.Join(names, ", ")
}

// sortedKeys returns the keys of a map in sorted order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// auditLogFacetColumnMapping maps API field paths to ClickHouse column names for audit logs.
// This is internal to the storage layer - only the field names are exposed publicly.
var auditLogFacetColumnMapping = map[string]string{
	"verb":                "verb",
	"user.username":       "user",
	"user.uid":            "user_uid",
	"responseStatus.code": "status_code",
	"objectRef.namespace": "namespace",
	"objectRef.resource":  "resource",
	"objectRef.apiGroup":  "api_group",
	"location":            "location",
}

// GetAuditLogFacetColumn returns the ClickHouse column name for an audit log facet field.
// Returns an error if the field is not supported.
func GetAuditLogFacetColumn(field string) (string, error) {
	col, ok := auditLogFacetColumnMapping[field]
	if !ok {
		return "", fmt.Errorf("unsupported audit log facet field: %s", field)
	}
	return col, nil
}

// ActivityFacetFields defines the supported fields for activity facet queries.
// Keys are API field paths (as used in queries), values are human-readable descriptions.
var ActivityFacetFields = map[string]string{
	"spec.actor.name":         "The name of the actor who performed the action",
	"spec.actor.type":         "The type of actor (user, service, system)",
	"spec.resource.apiGroup":  "The API group of the target resource",
	"spec.resource.kind":      "The kind of the target resource",
	"spec.resource.namespace": "The namespace of the target resource",
	"spec.changeSource":       "The source of the change (human, automation, system)",
}

// IsValidActivityFacetField checks if a field is supported for activity faceting.
func IsValidActivityFacetField(field string) bool {
	_, ok := ActivityFacetFields[field]
	return ok
}

// GetActivityFacetFieldNames returns a slice of supported activity facet field names.
// Useful for error messages showing valid options.
func GetActivityFacetFieldNames() []string {
	return sortedKeys(ActivityFacetFields)
}

// activityFacetColumnMapping maps API field paths to ClickHouse column names for activities.
var activityFacetColumnMapping = map[string]string{
	"spec.actor.name":         "actor_name",
	"spec.actor.type":         "actor_type",
	"spec.resource.apiGroup":  "api_group",
	"spec.resource.kind":      "resource_kind",
	"spec.resource.namespace": "resource_namespace",
	"spec.changeSource":       "change_source",
}

// GetActivityFacetColumn returns the ClickHouse column name for an activity facet field.
// Returns an error if the field is not supported.
func GetActivityFacetColumn(field string) (string, error) {
	col, ok := activityFacetColumnMapping[field]
	if !ok {
		return "", fmt.Errorf("unsupported activity facet field: %s", field)
	}
	return col, nil
}

// EventFacetFields defines the supported fields for Kubernetes Event facet queries.
// Keys are API field paths (as used in queries), values are human-readable descriptions.
var EventFacetFields = map[string]string{
	"regarding.kind":      "The kind of resource the event is about (Pod, Deployment, etc.)",
	"regarding.namespace": "The namespace of the regarding object",
	"related.kind":        "The kind of the related secondary object (if present)",
	"related.namespace":   "The namespace of the related secondary object (if present)",
	"reason":              "The event reason (Scheduled, Pulled, Created, etc.)",
	"type":                "The event type (Normal, Warning)",
	"source.component":    "The component that generated the event (kubelet, scheduler, etc.)",
	"namespace":           "The namespace of the event itself",
}

// IsValidEventFacetField checks if a field is supported for event faceting.
func IsValidEventFacetField(field string) bool {
	_, ok := EventFacetFields[field]
	return ok
}

// EventFacetFieldNames returns a sorted list of supported event facet field names.
func EventFacetFieldNames() []string {
	return sortedKeys(EventFacetFields)
}

// eventFacetColumnMapping maps API field paths to ClickHouse column names for events.
var eventFacetColumnMapping = map[string]string{
	"regarding.kind":      "regarding_kind",
	"regarding.namespace": "regarding_namespace",
	"related.kind":        "related_kind",
	"related.namespace":   "related_namespace",
	"reason":              "reason",
	"type":                "type",
	"source.component":    "source_component",
	"namespace":           "namespace",
}

// GetEventFacetColumn returns the ClickHouse column name for an event facet field.
// Returns an error if the field is not supported.
func GetEventFacetColumn(field string) (string, error) {
	col, ok := eventFacetColumnMapping[field]
	if !ok {
		return "", fmt.Errorf("unsupported event facet field: %s", field)
	}
	return col, nil
}


// GetEventFieldValue extracts a field value from a Kubernetes Event object
// given a ClickHouse column name. This is the shared implementation used by
// both the watch and registry layers to apply field-selector filters in memory.
func GetEventFieldValue(event *corev1.Event, column string) string {
	switch column {
	case "namespace":
		return event.Namespace
	case "name":
		return event.Name
	case "uid":
		return string(event.UID)
	case "regarding_api_version":
		return event.InvolvedObject.APIVersion
	case "regarding_kind":
		return event.InvolvedObject.Kind
	case "regarding_namespace":
		return event.InvolvedObject.Namespace
	case "regarding_name":
		return event.InvolvedObject.Name
	case "regarding_uid":
		return string(event.InvolvedObject.UID)
	case "regarding_field_path":
		return event.InvolvedObject.FieldPath
	case "related_api_version":
		if event.Related == nil {
			return ""
		}
		return event.Related.APIVersion
	case "related_kind":
		if event.Related == nil {
			return ""
		}
		return event.Related.Kind
	case "related_namespace":
		if event.Related == nil {
			return ""
		}
		return event.Related.Namespace
	case "related_name":
		if event.Related == nil {
			return ""
		}
		return event.Related.Name
	case "reason":
		return event.Reason
	case "type":
		return event.Type
	case "source_component":
		return event.Source.Component
	case "source_host":
		return event.Source.Host
	default:
		return ""
	}
}

func init() {
	// Validate that all defined fields have corresponding column mappings.
	// This catches mismatches at startup rather than at runtime.
	for field := range AuditLogFacetFields {
		if _, ok := auditLogFacetColumnMapping[field]; !ok {
			panic(fmt.Sprintf("missing ClickHouse column mapping for audit log facet field %q", field))
		}
	}

	// Also validate the reverse: all mappings should have field definitions
	for field := range auditLogFacetColumnMapping {
		if _, ok := AuditLogFacetFields[field]; !ok {
			panic(fmt.Sprintf("audit log facet column mapping %q has no field definition", field))
		}
	}

	// Validate activity facet fields
	for field := range ActivityFacetFields {
		if _, ok := activityFacetColumnMapping[field]; !ok {
			panic(fmt.Sprintf("missing ClickHouse column mapping for activity facet field %q", field))
		}
	}

	for field := range activityFacetColumnMapping {
		if _, ok := ActivityFacetFields[field]; !ok {
			panic(fmt.Sprintf("activity facet column mapping %q has no field definition", field))
		}
	}

	// Validate event facet fields
	for field := range EventFacetFields {
		if _, ok := eventFacetColumnMapping[field]; !ok {
			panic(fmt.Sprintf("missing ClickHouse column mapping for event facet field %q", field))
		}
	}

	for field := range eventFacetColumnMapping {
		if _, ok := EventFacetFields[field]; !ok {
			panic(fmt.Sprintf("event facet column mapping %q has no field definition", field))
		}
	}
}
