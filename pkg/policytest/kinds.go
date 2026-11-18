package policytest

import (
	"strings"

	"github.com/gobuffalo/flect"
)

// Ways a kind or resource name was resolved offline.
const (
	resolvedExplicit = "explicit" // declared in the test file's resource_kinds
	resolvedPolicy   = "policy"   // kind taken from a loaded policy's spec.resource
	resolvedGuessed  = "guessed"  // inferred by English pluralization
)

// kindIndex resolves between plural resource names and kinds without a
// cluster. Production uses the API server's RESTMapper; offline the tool
// prefers explicit resource_kinds mappings, then kinds known from loaded
// policies, and only then guesses by pluralization.
type kindIndex struct {
	explicitKind     map[string]string // group/resource -> kind
	explicitResource map[string]string // group/kind -> resource
	policyKind       map[string]string // group/resource -> kind
}

func normalizeGroup(group string) string {
	g := strings.ToLower(group)
	if g == "core" {
		return ""
	}
	return g
}

func indexKey(group, name string) string {
	return normalizeGroup(group) + "/" + strings.ToLower(name)
}

func newKindIndex(resourceKinds map[string]map[string]string, hints []LoadedPolicy) *kindIndex {
	k := &kindIndex{
		explicitKind:     make(map[string]string),
		explicitResource: make(map[string]string),
		policyKind:       make(map[string]string),
	}
	for group, mapping := range resourceKinds {
		for resource, kind := range mapping {
			k.explicitKind[indexKey(group, resource)] = kind
			k.explicitResource[indexKey(group, kind)] = strings.ToLower(resource)
		}
	}
	for _, lp := range hints {
		if lp.Policy == nil || lp.Policy.Spec.Resource.Kind == "" {
			continue
		}
		group := lp.Policy.Spec.Resource.APIGroup
		kind := lp.Policy.Spec.Resource.Kind
		if resource, ok := k.explicitResource[indexKey(group, kind)]; ok {
			k.policyKind[indexKey(group, resource)] = kind
		}
		k.policyKind[indexKey(group, guessResource(kind))] = kind
		k.policyKind[indexKey(group, kind)] = kind
	}
	return k
}

// guessResource pluralizes a kind the way most Kubernetes APIs name resources.
func guessResource(kind string) string {
	return strings.ToLower(flect.Pluralize(kind))
}

// resourceForKind returns the plural resource for a policy's kind, which
// production obtains from the RESTMapper to route audit events.
func (k *kindIndex) resourceForKind(group, kind string) (resource string, how string) {
	if r, ok := k.explicitResource[indexKey(group, kind)]; ok {
		return r, resolvedExplicit
	}
	return guessResource(kind), resolvedGuessed
}

// kindForResource returns the kind for a plural resource name, used when a
// link() target has a resource but no kind (e.g. audit.objectRef).
func (k *kindIndex) kindForResource(group, resource string) (kind string, how string) {
	if kind, ok := k.explicitKind[indexKey(group, resource)]; ok {
		return kind, resolvedExplicit
	}
	if kind, ok := k.policyKind[indexKey(group, resource)]; ok {
		return kind, resolvedPolicy
	}
	return flect.Pascalize(flect.Singularize(resource)), resolvedGuessed
}
