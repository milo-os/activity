# Edge Audit Ingest

Requests a customer makes against an edge control plane are audited by that
cluster's API server, not by the core control plane, so none of them reach the
customer's project timeline. Edge audit ingest closes that gap: it accepts
audit batches from edge control planes and republishes them onto the same
`AUDIT_EVENTS` stream the [core audit pipeline](./audit-pipeline.md) already
consumes, so Vector, the processor, ClickHouse and the query API need no
edge-specific handling at all.

It runs as `activity ingest`, a subcommand of the same binary that serves
`serve`, `processor` and the rest.

## Why the records need rewriting first

Edge control planes are downstream clusters. Milo projects a project's
resources into them under namespaces named `ns-<upstream-namespace-uid>`, so a
raw edge audit record refers to namespaces the customer who owns the resource
has never seen and must never be shown. Replaying such a record unchanged would
publish an internal identifier into a customer-visible audit trail — and, worse,
would carry no usable tenant, so it would land in the platform bucket where
every operator can read it.

## Pipeline

| Stage | What happens |
|---|---|
| **Receive** | An mTLS listener accepts an `audit.k8s.io/v1` `EventList` on `POST /v1/audit` — the shape a kube-apiserver audit webhook backend already sends, so an edge control plane needs no shipper of its own. |
| **Identify** | The cluster and its location come from the client certificate's common name, looked up in the cluster registry. |
| **Resolve** | Every `ns-<uid>` the record mentions is reversed to the upstream project and namespace it was projected from. |
| **Attribute** | Scope annotations and user extras are set from the resolved project. |
| **Stamp** | The location and source cluster are recorded as annotations. |
| **Emit** | The rewritten record is published to `audit.k8s.edge.<cluster>`, keyed by audit ID so a retried batch does not duplicate. |

## Trust boundary

An edge shipper asserts nothing about itself.

Cluster identity and location come from the authenticated transport credential
and the registry the platform renders — never from the request body. Scope
annotations and user extras that arrive on the wire are overwritten, not merged,
so a compromised edge site cannot promote its own records into someone else's
project or relabel where they came from.

The registry maps a client certificate common name to a cluster name and an
opaque location string:

```yaml
clusters:
  - clientCommonName: audit-shipper.dfw1.edge.datum.net
    name: dfw1
    location: us-central-1
```

Infrastructure derives `location` from the Karmada cluster's
`topology.datum.net/location` label. Activity stores and queries it as a string
and takes no dependency on any locations service.

## Reverse namespace lookup

The index is built from **upstream** project control plane namespaces, not from
the edge clusters. Each upstream namespace's UID gives the downstream name it
projects to, so the lookup needs no credential into any edge cluster, and a
compromised edge site cannot influence what a namespace resolves to.

Two properties of that index are load-bearing:

- **A cold cache is not an unknown namespace.** Readiness gates on cache sync
  rather than on the listener being bound, and a record that arrives against a
  still-warming cache is parked and retried before the shipper is asked to
  resend. Failing it straight through to the platform bucket would misfile a
  window of records on every deploy.
- **The map only grows.** Informers drop deleted objects, but audit records
  *about* a deleted namespace stay queryable for their full retention window.
  Entries are never removed, and the index is persisted so mappings for
  namespaces deleted while the process was down survive a restart.

## Failure handling

| Condition | Outcome |
|---|---|
| Client identity not in the registry | `403`, nothing accepted |
| Namespace caches cold past the park timeout | `503` with `Retry-After`, batch resent |
| Namespace resolves to nothing | Record dropped, counted in `activity_edge_audit_events_dropped_total{reason="namespace_unresolved"}` |
| One record spans namespaces from two projects | Dropped, `reason="ambiguous_project"` |
| A downstream namespace survives the rewrite | Dropped, `reason="namespace_leak"` |

Dropping is deliberate. There is no safe tenant to file an unresolvable record
under, and the platform bucket is not a neutral default — it is the one place
every operator can read.

## Querying

Edge records are ordinary audit logs. They carry
`locations.miloapis.com/location`, materialized into a `location` column, so
they can be filtered and faceted alongside everything else:

```
location == 'us-central-1' && objectRef.resource == 'instances'
```

## Scope

Core control plane audit is untouched — it already reaches NATS directly and
needs no rewriting. `ActivitySpec.Source` and edge **Event** ingestion are
covered separately by the [federated event ingestion
RFC](../enhancements/federated-event-ingestion.md).
