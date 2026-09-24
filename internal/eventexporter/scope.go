package eventexporter

import (
	"strings"

	corelisters "k8s.io/client-go/listers/core/v1"
)

// upstreamClusterNameLabel is the label Karmada propagates onto a namespace
// in edge and member clusters, naming the project it was federated from.
const upstreamClusterNameLabel = "meta.datumapis.com/upstream-cluster-name"

// scopeTypeProject is the platform.miloapis.com/scope.type value for a
// project-scoped Activity, recovered from an event's namespace.
const scopeTypeProject = "project"

// upstreamClusterNameFromLabel decodes a project name from
// upstreamClusterNameLabel's value ("cluster-" prefix, slashes encoded as
// underscores), matching the encoding Datum's federating controllers already
// use when writing the label.
func upstreamClusterNameFromLabel(value string) string {
	name := strings.TrimPrefix(strings.ReplaceAll(value, "_", "/"), "cluster-")
	return strings.TrimPrefix(name, "/")
}

// namespaceScopeResolver recovers an event's tenant scope from its
// namespace's Karmada-propagated upstream-cluster-name label, using a
// lister-backed cache so resolution never costs a live read per event.
type namespaceScopeResolver struct {
	namespaces corelisters.NamespaceLister
}

func newNamespaceScopeResolver(namespaces corelisters.NamespaceLister) *namespaceScopeResolver {
	return &namespaceScopeResolver{namespaces: namespaces}
}

// Resolve returns the project scope for namespace, and whether it resolved.
// It returns false when the namespace can't be read or carries no
// upstream-cluster-name label - callers should treat the event as unscoped
// rather than guessing.
func (r *namespaceScopeResolver) Resolve(namespace string) (scopeType, scopeName string, ok bool) {
	ns, err := r.namespaces.Get(namespace)
	if err != nil {
		return "", "", false
	}

	label, present := ns.Labels[upstreamClusterNameLabel]
	if !present || label == "" {
		return "", "", false
	}

	project := upstreamClusterNameFromLabel(label)
	if project == "" {
		return "", "", false
	}

	return scopeTypeProject, project, true
}
