// Package activitysubject builds the NATS subjects activities are published
// on and the filter subjects Watch consumers subscribe with. Publishers and
// the watcher share it so the two sides cannot drift apart.
//
// Subject format:
//
//	<prefix>.<tenant_type>.<tenant_name>.<api_group>.<origin>.<kind>.<namespace>.<name>
//
// Every token is passed through EncodeToken, so a dotted API group such as
// networking.datumapis.com occupies a single token (networking_datumapis_com).
package activitysubject

import (
	"strings"
	"unicode"

	"go.miloapis.com/activity/internal/types"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

const (
	// coreAPIGroup stands in for the empty (core) API group.
	coreAPIGroup = "core"

	// emptyToken stands in for an empty value so a subject never has an
	// empty token.
	emptyToken = "_"
)

// EncodeToken returns s as a single NATS subject token. The token separator
// '.', the wildcards '*' and '>', and whitespace all become '_'; an empty
// string becomes "_". API groups, kinds, namespaces and names are DNS
// subdomains or labels, which cannot contain '_', so the mapping does not
// make two real values collide.
func EncodeToken(s string) string {
	if s == "" {
		return emptyToken
	}
	return strings.Map(func(r rune) rune {
		if r == '.' || r == '*' || r == '>' || unicode.IsSpace(r) {
			return '_'
		}
		return r
	}, s)
}

// ForActivity returns the subject an activity is published on. An empty
// tenant type is "platform", an empty API group is "core", and any other
// empty value is "_".
func ForActivity(prefix string, activity *v1alpha1.Activity) string {
	tenantType := activity.Spec.Tenant.Type
	if tenantType == "" {
		tenantType = types.TenantTypePlatform
	}
	apiGroup := activity.Spec.Resource.APIGroup
	if apiGroup == "" {
		apiGroup = coreAPIGroup
	}

	return strings.Join([]string{
		prefix,
		EncodeToken(tenantType),
		EncodeToken(activity.Spec.Tenant.Name),
		EncodeToken(apiGroup),
		EncodeToken(activity.Spec.Origin.Type),
		EncodeToken(activity.Spec.Resource.Kind),
		EncodeToken(activity.Spec.Resource.Namespace),
		EncodeToken(activity.Name),
	}, ".")
}

// ForWatch returns the filter subject for a watch in the given tenant scope.
// Empty apiGroup, kind and namespace match any value. A platform-scope watch
// matches every activity: tenant-wide filters are applied per message instead.
func ForWatch(prefix, tenantType, tenantName, apiGroup, kind, namespace string) string {
	if tenantType == types.TenantTypePlatform {
		return prefix + ".>"
	}

	return strings.Join([]string{
		prefix,
		EncodeToken(tenantType),
		EncodeToken(tenantName),
		wildcardOrToken(apiGroup),
		"*", // origin
		wildcardOrToken(kind),
		wildcardOrToken(namespace),
		">", // name
	}, ".")
}

func wildcardOrToken(s string) string {
	if s == "" {
		return "*"
	}
	return EncodeToken(s)
}
