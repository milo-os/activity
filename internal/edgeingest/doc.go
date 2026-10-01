// Package edgeingest receives kube-apiserver audit events from edge control
// planes and republishes them onto the same NATS stream the core control plane
// audit pipeline uses.
//
// Edge control planes are downstream clusters. Milo projects a project's
// resources into them under namespaces named ns-<upstream-namespace-uid>, so a
// raw edge audit record refers to namespaces that mean nothing to the customer
// who owns the resource, and that must never be shown to them. This package
// reverses that mapping before anything is emitted.
//
// # Trust boundary
//
// An edge shipper asserts nothing about itself. The cluster it speaks for is
// taken from the authenticated transport credential and looked up in a
// [ClusterRegistry] supplied out of band; a cluster name or location that
// arrives in a request body is ignored. The same applies to tenancy: scope
// annotations and user extras on an incoming record are overwritten from the
// resolved namespace, never trusted.
//
// # Pipeline
//
// Receive (mTLS listener) → Identify (client certificate → cluster and
// location) → Resolve (downstream namespace → upstream project) → Attribute
// (scope annotations and user extras) → Stamp (location annotation) →
// Emit (NATS JetStream).
//
// Every stage fails closed. A record whose namespaces cannot be resolved is
// dropped rather than attributed to the platform, because "no known tenant" and
// "belongs to no tenant" are not the same claim, and conflating them publishes
// one customer's activity where every operator can read it.
package edgeingest
