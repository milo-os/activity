package eventexporter

import (
	"strings"

	corelisters "k8s.io/client-go/listers/core/v1"

	"go.miloapis.com/activity/internal/types"
)

// upstreamClusterNameLabel is the label Karmada propagates onto a namespace
// in edge and member clusters, naming the project it was federated from.
const upstreamClusterNameLabel = "meta.datumapis.com/upstream-cluster-name"

// upstreamNamespaceLabel is the label Karmada propagates onto a namespace in
// edge and member clusters, naming the upstream namespace it projects.
const upstreamNamespaceLabel = "meta.datumapis.com/upstream-namespace"

// eventScope is the tenant scope recovered for an event.
type eventScope struct {
	Type      string
	Name      string
	Namespace string
}

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

// Resolve returns the project scope and upstream namespace for namespace.
// It returns false when the namespace can't be read or either upstream label
// is missing - callers should treat the event as unscoped rather than guessing.
func (r *namespaceScopeResolver) Resolve(namespace string) (eventScope, bool) {
	ns, err := r.namespaces.Get(namespace)
	if err != nil {
		return eventScope{}, false
	}

	project := upstreamClusterNameFromLabel(ns.Labels[upstreamClusterNameLabel])
	if project == "" {
		return eventScope{}, false
	}

	upstreamNamespace := ns.Labels[upstreamNamespaceLabel]
	if upstreamNamespace == "" {
		return eventScope{}, false
	}

	return eventScope{
		Type:      types.TenantTypeProject,
		Name:      project,
		Namespace: upstreamNamespace,
	}, true
}
