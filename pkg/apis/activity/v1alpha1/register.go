package v1alpha1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupName is the group name for the activity API
const GroupName = "activity.miloapis.com"

// SchemeGroupVersion is group version used to register these objects
var SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: "v1alpha1"}

var (
	// SchemeBuilder is the scheme builder for this API group
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes, RegisterConversions)
	// AddToScheme adds the types in this group-version to the given scheme
	AddToScheme = SchemeBuilder.AddToScheme
)

// Resource takes an unqualified resource and returns a Group qualified GroupResource
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

// addKnownTypes adds the set of types defined in this package to the supplied scheme
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&AuditLogQuery{},
		&AuditLogFacetsQuery{},
		&ActivityPolicy{},
		&ActivityPolicyList{},
		&Activity{},
		&ActivityList{},
		&ActivityQuery{},
		&ActivityFacetQuery{},
		&EventFacetQuery{},
		&EventQuery{},
		&EventQueryList{},
		&PolicyPreview{},
		&ReindexJob{},
		&ReindexJobList{},
	)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)

	// Register field label conversions for Activity
	// This enables field selectors like spec.changeSource=human, spec.resource.kind=HTTPProxy, etc.
	activityGVK := SchemeGroupVersion.WithKind("Activity")
	if err := scheme.AddFieldLabelConversionFunc(activityGVK,
		ActivityFieldLabelConversionFunc); err != nil {
		return err
	}

	return nil
}

// EventFieldLabelConversionFunc converts field selectors for Event resources.
// This allows filtering events by fields beyond the default metadata.name and metadata.namespace.
func EventFieldLabelConversionFunc(label, value string) (string, string, error) {
	switch label {
	// Metadata fields
	case "metadata.name",
		"metadata.namespace",
		"metadata.uid":
		return label, value, nil

	// Regarding object fields (commonly used with field selectors)
	case "regarding.apiVersion",
		"regarding.kind",
		"regarding.namespace",
		"regarding.name",
		"regarding.uid",
		"regarding.fieldPath":
		return label, value, nil

	// Event classification
	case "reason",
		"type":
		return label, value, nil

	// Source fields
	case "source.component",
		"source.host":
		return label, value, nil

	// Reporting fields (for newer Event API)
	case "reportingComponent",
		"reportingInstance":
		return label, value, nil

	default:
		return "", "", fmt.Errorf("%q is not a known field selector: only %q",
			label, SupportedEventFieldSelectors)
	}
}

// SupportedEventFieldSelectors lists all supported field selectors for Events
var SupportedEventFieldSelectors = []string{
	"metadata.name",
	"metadata.namespace",
	"metadata.uid",
	"regarding.apiVersion",
	"regarding.kind",
	"regarding.namespace",
	"regarding.name",
	"regarding.uid",
	"regarding.fieldPath",
	"reason",
	"type",
	"source.component",
	"source.host",
	"reportingComponent",
	"reportingInstance",
}

// ActivityFieldLabelConversionFunc converts field selectors for Activity resources.
// This allows filtering activities by fields beyond the default metadata.name and metadata.namespace.
func ActivityFieldLabelConversionFunc(label, value string) (string, string, error) {
	switch label {
	// Metadata fields
	case "metadata.name",
		"metadata.namespace":
		return label, value, nil

	// Change source (human vs system)
	case "spec.changeSource":
		return label, value, nil

	// Resource fields
	case "spec.resource.apiGroup",
		"spec.resource.kind",
		"spec.resource.name",
		"spec.resource.namespace",
		"spec.resource.uid":
		return label, value, nil

	// Actor fields
	case "spec.actor.name",
		"spec.actor.type",
		"spec.actor.uid",
		"spec.actor.email":
		return label, value, nil

	// CEL expression, same language as ActivityQuery spec.filter. Clients
	// escape the value with fields.EscapeValue (backslash, comma and '=').
	case ActivityFilterFieldSelector:
		return label, value, nil

	// Watch-only time bounds; List ignores them.
	case ActivityStartTimeFieldSelector,
		ActivityMaxAgeFieldSelector:
		return label, value, nil

	default:
		return "", "", fmt.Errorf("%q is not a known field selector: only %q",
			label, SupportedActivityFieldSelectors)
	}
}

// SupportedActivityFieldSelectors lists all supported field selectors for Activities
var SupportedActivityFieldSelectors = []string{
	"metadata.name",
	"metadata.namespace",
	"spec.changeSource",
	"spec.resource.apiGroup",
	"spec.resource.kind",
	"spec.resource.name",
	"spec.resource.namespace",
	"spec.resource.uid",
	"spec.actor.name",
	"spec.actor.type",
	"spec.actor.uid",
	"spec.actor.email",
	ActivityFilterFieldSelector,
	ActivityStartTimeFieldSelector,
	ActivityMaxAgeFieldSelector,
}

const (
	// ActivityFilterFieldSelector carries a CEL filter expression for List and
	// Watch, ANDed with every other field selector.
	ActivityFilterFieldSelector = "filter"

	// ActivityStartTimeFieldSelector drops watch events whose
	// creationTimestamp is before the given time (RFC3339 or relative, e.g.
	// "now-5m"), resolved when the watch starts. Watch only.
	ActivityStartTimeFieldSelector = "startTime"

	// ActivityMaxAgeFieldSelector drops watch events whose creationTimestamp is
	// older than the given Go duration (e.g. "10m") at delivery. Watch only.
	ActivityMaxAgeFieldSelector = "maxAge"
)
