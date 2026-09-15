---
status: proposed
---

# Federated Event Ingestion for Activity

> Tracks [datum-cloud/compute#100](https://github.com/datum-cloud/compute/issues/100).

## Table of Contents

- [Summary](#summary)
- [Problem](#problem)
- [Design](#design)
  - [Event Exporter Source Enrichment](#event-exporter-source-enrichment)
  - [Cross-Cluster Dedup](#cross-cluster-dedup)
  - [`ActivitySpec.Source` API Field](#activityspecsource-api-field)
  - [Correlating Edge Activities Back to Their Origin](#correlating-edge-activities-back-to-their-origin)
  - [Extending the Data Model](#extending-the-data-model)
  - [Collector Deployment Model](#collector-deployment-model)
  - [Compute-Side Follow-On Work](#compute-side-follow-on-work)
- [Alternatives](#alternatives)
- [Failure modes](#failure-modes)
- [Decisions](#decisions)
- [Open questions](#open-questions)

---

## Summary

This RFC extends Activity's existing event-exporter into a source-aware, federated collector. It adds source and plane metadata through the API, CEL, and storage layers, so Compute — and future federated services — can show one timeline that spans both planes. Compute's own follow-on work (new lifecycle Events, new `ActivityPolicy` rules) is covered briefly at the end of Design; it's compute-repo implementation work, not designed in full here.

## Problem

A Compute `Workload` created in a project shows up as an event on the management plane. But everything about its runtime outcome — starting, crashing, getting rescheduled, hitting a quota wall — happens as a Kubernetes Event written by a system controller in the edge cluster where it runs. Activity has no pipeline that reads from edge clusters, so none of that becomes an Activity — the timeline stops at creation.

This gap can't be closed by extending Compute's existing status write-back path. That path exists to sync a single object's state across a cluster boundary — low in volume, one write per reconcile. Kubernetes Events are a different shape of problem: high-volume and high-cardinality, and exactly what Activity already exists to ingest and process for logs today. Routing edge lifecycle data through status write-back would mean bending a narrow object-sync mechanism to carry a payload it wasn't built for, instead of using the system already designed for it.

Activity's `event-exporter` command already exists, but it was built for a single cluster with a fixed identity, not for federation:

- It is single-tenant and static-scope. It gets a fixed `platform.miloapis.com/scope.type`/`scope.name` from command-line flags and publishes every Event to NATS subject `events.<namespace>`. There is exactly one scope per deployment.
- Its dedup and identity logic is cluster-local: NATS message IDs and Activity origin IDs are both just the Event UID (message IDs also add ResourceVersion for updates). Kubernetes UIDs are generated independently by each cluster's apiserver, so two different clusters can produce colliding UIDs, and nothing today tells them apart.
- `ActivitySpec` has no concept of source plane, cluster, region, or city. There's no way to ask "show me everything that happened in DFW," or to tell a management-plane Activity apart from an edge one.

Compute already has working `EventRecorder` plumbing — the gap on the Compute side is which lifecycle transitions get recorded as Events, not whether Events can be emitted at all. And Compute's own resource labels — `compute.datumapis.com/workload-uid`, `workload-name`, `workload-deployment-uid`, `workload-deployment-name`, `placement-name`, `instance-index` — already give a policy what it needs to render a rich summary once an Activity exists, with no remote lookup required. Sourcing the city and region values themselves is a separate, open question (see Open Questions).

## Design

### Event Exporter Source Enrichment

The core mechanism evolves the existing event-exporter rather than standing up a new binary. It already owns the informer watch, the NATS publish path, and the health probe, so duplicating that plumbing buys nothing.

Federation needs scope resolved per event instead of fixed at deploy time, and the event's namespace already carries what's needed: Karmada propagates `meta.datumapis.com/upstream-cluster-name`/`upstream-namespace` labels down onto the namespace object in edge and member clusters. This was confirmed by inspecting a live edge-cluster namespace, which carried both labels as real values marked Karmada-managed — so this scope recovery works from local reads alone, with no live callback to the Karmada API required. For this pathway, `scope.type` is always `project`, and `scope.name` is the Project name already embedded in `upstream-cluster-name`.

Compute owns the namespace labels the collector reads. They're written by Compute's own control plane component acting within a project boundary it already manages — not something an edge site can set on its own behalf. Nothing else running in an edge cluster, including the layer that runs customer instances, has any path to touch them.

The exporter also needs to tag each event with where it came from: which plane (`management` or `edge`), which cluster, and which city and region. It does this with annotations alongside the scope annotations it already sets, such as `activity.miloapis.com/source-plane-type` and `source-cluster`.

Core Kubernetes Events are short-lived at the source cluster — an inherent property of using them as the signal, not something the exporter's own retry or backoff logic can fully solve (see Failure Mode 2).

### Cross-Cluster Dedup

Once more than one cluster can publish an Event with the same UID, identity based on the UID alone breaks (see Problem). It's unlikely, but not impossible — an instance-crash Event with UID `a1b2c3` could legitimately exist in both an edge cluster in Dallas and one in Ashburn at the same time, with nothing today to tell them apart.

Both the NATS message ID and the Activity origin ID need to include the source plane and cluster, not just the event UID — an origin ID becomes something like `edge/cluster-dfw-1/a1b2c3` instead of the bare UID, keeping same-UID Events from two different clusters distinct instead of one silently overwriting the other.

### `ActivitySpec.Source` API Field

Activities gain an optional `Source` field on `ActivitySpec` describing where the underlying event happened: its plane type (`management` or `edge`), cluster, region, and city.

For event-derived Activities, the processor populates this field from the source annotations the exporter attaches. Whether audit-derived Activities should also populate it is addressed in Extending the Data Model.

### Correlating Edge Activities Back to Their Origin

Edge and management-plane Activities need to read as part of the same story, without reattributing the actor — a controller terminating an instance for running out of quota isn't something the user did.

Compute's recommended Event shape carries a `related` reference to the `WorkloadDeployment`. The processor sets `ActivitySpec.Resource` for event-derived Activities from that same reference, the way it already does for any other Activity. A project's Activity feed then naturally groups "Alice created Workload X" with "an instance of Workload X crashed in DFW," without claiming Alice caused the crash. The actor stays honest — controller/system for the edge event, the user for the original one — the resource link is what ties the story together.

### Extending the Data Model

Policies and queries need to reference source metadata the same way they already reference actor, resource, and other Activity/Event fields — through CEL expressions in `ActivityPolicy` rules, and as filters and facets in the query API and portal (for example, filtering Activities to everything in DFW). An event rule can match and summarize directly on source fields:

```yaml
eventRules:
  - match: "event.reason == 'Available'"
    summary: "Instance started running in {{ source.city }}"
```

Reading source data from raw event annotations already works with zero code change; the work here is adding first-class support so policies and queries don't have to reach into annotations directly.

ClickHouse needs the same source fields as indexed columns on both the events and activities tables, so filtering and faceting by plane, cluster, city, and region perform well at scale instead of requiring a full scan. Whether audit-derived rows ever get source data populated is a separate, open question — the schema change doesn't force an answer either way.

### Collector Deployment Model

Deploy the exporter per edge cluster, not as a central collector holding credentials into every edge cluster.

No exporter rearchitecture is needed either. `event-exporter` is already a single-cluster, in-process watcher — informer watch, local NATS publish — and this RFC's per-deployment flags already assume "run one copy per cluster with different flags." A central model would need new multi-cluster fan-out and watch machinery that doesn't exist today, for no real benefit. Publishing also stays on the existing NATS `EVENTS` stream — federating the exporter isn't a reason to stand up a second one.

Per-edge deployment also keeps credential exposure small. A leaked edge-site credential only exposes that one site, not a central store holding credentials to the whole fleet. Onboarding is automatic too — a new edge cluster gets the exporter the same way it gets other edge components, with no separate step to register its credentials centrally.

The trade-off: without a central collector, per-source rate limiting has to be handled by each edge exporter instance or by the NATS consumer side, not at one shared chokepoint — see Failure Mode 4.

Each edge exporter authenticates to NATS with an nkey — NATS's own decentralized credential mechanism, an Ed25519 keypair — rather than mTLS client certificates. nkeys are NATS-native, so there's no separate certificate authority or rotation lifecycle to stand up the way mTLS would need. Distribution is also simpler — a single secret (the seed), not a cert-and-key pair — and it fits how edge components already receive credentials today.

### Compute-Side Follow-On Work

Compute-repo implementation work, not designed in full here:

- New lifecycle reasons extend Compute's existing `EventRecorder` plumbing — these Events carry a controller/system actor, not the original user, and should set `related` to the `WorkloadDeployment` so Activity can correlate them back to it (see Correlating Edge Activities Back to Their Origin).
- New Event `Reason` strings should align with Compute's existing Condition reason vocabulary — the reasons it already uses for instance availability, readiness, and scheduling — rather than inventing a parallel set of names that don't map to anything else in the codebase.
- Compute adds its own `ActivityPolicy` rules in the compute repo, following the pattern other federated services already use there: audit rules for user actions, event rules for system outcomes, and filtering out noisy "success" events so the policy doesn't produce spam.
- None of those existing reference policies handle cross-cluster dedup or foreign-namespace scope recovery — they all emit Events from a controller in the same cluster Activity already watches. They're a good precedent for the policy/CEL convention, but not for the federated-collector mechanism itself.
- New rules should go through `PolicyPreview` against sample federated Events before merging, the same check already expected of other policy changes.

## Alternatives

1. **Build a wholly new, separate event-collector binary instead of evolving `event-exporter`.** This would duplicate the NATS publish, informer watch, and health-probe wiring the existing binary already has, for no benefit.
2. **Represent source metadata only via annotations, with no first-class `ActivitySpec.Source` field.** Annotation-only access works on its own, but it makes ClickHouse materialization and portal-facet filtering clunkier — a typed field costs little and enables both, so annotations alone aren't enough.
3. **Recover edge tenant scope by having the collector call back live to the Karmada API server per-Event**, instead of trusting locally-propagated namespace labels. Adds a synchronous remote dependency and per-Event API load for no real gain — live-cluster verification already confirmed Karmada propagates the needed labels locally.
4. **Leave dedup and origin IDs as Event-UID-only and rely on the `EVENTS` stream's one-minute duplicate window** to make collisions unlikely in practice. Correctness shouldn't depend on timing luck, though — source-qualified IDs are cheap to add now versus debugging silent Activity loss later.

## Failure modes

1. **A namespace is missing the expected `meta.datumapis.com/upstream-*` labels** (e.g., a propagation-policy or managed-labels change drops them) → the collector can't resolve tenant scope for its Events. Handled by emitting an explicitly "unscoped" Activity source, backed by a metric, rather than dropping or misattributing it.
2. **The collector loses connectivity to an edge cluster** → a gap opens in that plane's portion of the Activity timeline. Audit-derived Activities for the same project are unaffected, since the two pipelines are independent. If the outage outlasts core Kubernetes' own Event retention (roughly an hour by default — the API server garbage-collects Events as a best-effort signal, not a durable log the way audit events are), the gap is permanent: there's nothing left to backfill from once the source Events are gone.
3. **An edge exporter can't reach central NATS** (network partition, expired cert/key) → Events queue locally. Needs a bounded queue with drop-and-count-metric behavior; unbounded buffering risks memory growth on the edge node.
4. **An edge cluster emits an Event volume spike** (e.g., mass instance churn during an incident) → this needs per-source rate limiting or backpressure designed into each edge exporter instance or the NATS consumer side before broad rollout — there's no central collector to enforce it at a single chokepoint.
5. **`ActivitySpec.Source` ships before the ClickHouse migration and CEL/storage wiring land** → CEL filters and facets on `spec.source.*` return empty for existing rows. Not a correctness bug, but sequence the change: land schema and storage wiring before turning on exporter annotation emission.
6. **A Karmada-side change stops propagating `upstream-*` labels onto namespaces** → every Event in every affected namespace degrades to the "unscoped" case in failure mode 1 at once. The exporter can't detect the propagation change directly; catch it via the unscoped-source metric crossing a fleet-wide threshold.
7. **A federated Event's namespace resolves to an `upstream-cluster-name`/`upstream-namespace` pair that no longer exists on the management plane** (project deleted mid-teardown) → the resulting Activity carries a scope nothing can authorize against. Treat it the same as failure mode 1 — surfaced as unscoped, not attributed to a dead scope.

## Decisions

- Evolve the existing `event-exporter` and deploy it per edge cluster through our existing edge-deployment mechanism, rather than building a new binary or a central collector — matches how other edge agents are already deployed, needs no exporter rearchitecture, and keeps credential exposure to one site at a time.
- `ActivitySpec.Source` ships as a new first-class typed field, not annotation-only.
- Event identity (NATS message ID and Activity origin ID) includes source plane and cluster, not just the event UID — prevents same-UID Events from two different clusters from colliding.
- Tenant scope for edge-originated Events is recovered from locally-propagated namespace labels, not a live callback to the Karmada API — confirmed reliable via live-cluster inspection.
- Edge Activities correlate back to their origin through `ActivitySpec.Resource`, using the `related` `WorkloadDeployment` reference on the Event — not by reattributing the actor.
- Each edge exporter authenticates to NATS with an nkey (NATS-native Ed25519 keypair), not mTLS client certificates — avoids standing up a separate certificate authority and rotation lifecycle, and distributes as a single secret (the seed) rather than a cert-and-key pair.

## Open questions

1. **Authoritative source for edge city/region metadata.** Still undecided: parse it out of the edge cluster's `cluster_labels` at runtime (looking for something like `topology.datum.net/city-code`), or add it as an explicit literal in cluster config? Nothing parses `cluster_labels` for this today — it's passed through as an opaque string.
2. **Should `ActivitySpec.Source` also be populated for audit-derived management-plane Activities** (e.g., `planeType: management`), or left empty and only set for federated/event-derived ones? This affects whether `spec.source.planeType` filters need an "empty means management" fallback in the portal — worth settling before that filter UI gets built.
