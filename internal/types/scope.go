package types

// Annotation keys carrying an event or activity's tenant scope.
const (
	ScopeTypeAnnotation = "platform.miloapis.com/scope.type"
	ScopeNameAnnotation = "platform.miloapis.com/scope.name"

	// ScopeNamespaceAnnotation names the upstream namespace a federated event's
	// edge-local namespace projects. An Activity filed under the edge-local
	// namespace is invisible to the user it belongs to.
	ScopeNamespaceAnnotation = "platform.miloapis.com/scope.namespace"
)
