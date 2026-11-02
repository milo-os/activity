# Live Streaming Architecture

The activity service provides real-time streaming of activities via the
Kubernetes Watch API. This enables clients to receive immediate notifications
when resources change, rather than continuously polling for updates.

![Live Streaming Architecture](../diagrams/live-streaming.png)

All Watch operations are backed by NATS JetStream consumers rather than
traditional approaches like etcd watches or in-memory caches. When a client
initiates a Watch request, the API server creates an ephemeral NATS consumer
that subscribes to the relevant subject pattern. As new messages arrive in the
stream, they flow through the consumer to the API server, which applies any
requested filters (CEL expressions, field selectors) before forwarding matching
events to the client over a long-lived HTTP connection.

This architecture provides several key benefits:

- **Decoupled from storage**: Watch operations don't query ClickHouse, reducing
  database load and eliminating polling latency
- **Scalable fan-out**: Multiple clients can watch the same resources without
  duplicating work; each gets an independent consumer
- **Resumable connections**: Clients can reconnect from any point using
  resourceVersion (backed by NATS sequence numbers)
- **Efficient filtering**: Subject-based routing narrows the message stream
  before it reaches the API server

## Ephemeral Consumers

The API server creates an ephemeral push consumer for each Watch connection:

| Setting | Value | Purpose |
|---------|-------|---------|
| Durable name | None (ephemeral) | Deleted when the watch stops |
| Deliver policy | New, or by start sequence | Live only, or resume from resourceVersion |
| Ack policy | Explicit | Acked once delivered to the client, or once filtered out |
| Flow control / heartbeat | On / 30 seconds | Backpressure and stale-consumer detection |
| Inactive threshold | 5 minutes | Cleanup after client disconnect |

## resourceVersion Semantics

NATS JetStream sequence numbers serve as resourceVersion, allowing clients to
resume a Watch from any point in the stream. Passing `0` or omitting the value
starts from the current position (new messages only). Watch requests with an
expired resourceVersion receive a 410 Gone error, and clients should fall back
to a List operation to resync state.

## Filtering

Filtering happens in two stages. First, the API server constructs a NATS subject
pattern based on request parameters (tenant, resource kind, namespace) to narrow
the message stream at the source. Then, any CEL expressions or field selectors
are evaluated server-side against each message, with only matching activities
forwarded to the client.

### Subject encoding

Activities are published on
`activities.<tenant_type>.<tenant_name>.<api_group>.<origin>.<kind>.<namespace>.<name>`.
Publishers and the Watch API encode every token the same way
(`internal/activitysubject`): `.`, `*`, `>` and whitespace become `_`, and empty
values become `_`. An activity for `networking.datumapis.com` therefore lands on
`activities.Project.prod.networking_datumapis_com.audit.HTTPProxy.default.<name>`,
and a project-scoped watch with `spec.resource.apiGroup=networking.datumapis.com`
subscribes to `activities.Project.prod.networking_datumapis_com.*.*.*.>`.

Platform-scope watches subscribe to `activities.>` and rely entirely on the
per-message filters below.

### Field selectors

Every field selector is enforced per message. Exact matches on
`spec.resource.apiGroup` and `spec.resource.kind`, and the request namespace,
also narrow the subject.

| Selector | Operators | Meaning |
|----------|-----------|---------|
| `metadata.name`, `metadata.namespace` | `=`, `==`, `!=` | Activity name / namespace |
| `spec.changeSource` | `=`, `==`, `!=` | `human` or `system` |
| `spec.resource.apiGroup`, `.kind`, `.name`, `.namespace`, `.uid` | `=`, `==`, `!=` | Affected resource |
| `spec.actor.name`, `.type`, `.uid` | `=`, `==`, `!=` | Who made the change |
| `filter` | `=` | CEL expression, same language as ActivityQuery `spec.filter` |
| `startTime` | `=` | Watch only. Drop activities created before this time |
| `maxAge` | `=` | Watch only. Drop activities older than this at delivery |

All selectors are ANDed. `spec.actor.email` is accepted by the field label
conversion but rejected with 400 because it is not a filterable field; use
`spec.actor.uid` or `spec.actor.name`. List honours every selector except
`startTime` and `maxAge`, which it ignores (List always covers the last hour).

#### CEL `filter`

Field selector values reserve `\`, `,` and `=`, so the expression must be escaped
with `fields.EscapeValue` (prefix each of those characters with `\`). The
expression is validated on its own before it is combined, and the combined
expression is validated again; an invalid expression returns 400 Bad Request.
List translates the filter to ClickHouse SQL, which supports a subset of CEL
(no ternaries or arithmetic, for example); List rejects those expressions with
400 even though Watch accepts them.

```bash
# spec.actor.name == 'alice' && spec.resource.kind != 'Secret'
kubectl get activities --watch \
  --field-selector "filter=spec.actor.name \=\= 'alice' && spec.resource.kind !\= 'Secret'"
```

The same request as a raw URL, with `maxAge` added:

```
GET /apis/activity.miloapis.com/v1alpha1/activities?watch=true&fieldSelector=filter%3Dspec.actor.name%20%5C%3D%5C%3D%20%27alice%27%20%26%26%20spec.resource.kind%20%21%5C%3D%20%27Secret%27%2CmaxAge%3D10m
```

In Go, build the selector with
`"filter=" + fields.EscapeValue(expr)`.

#### Time bounds: `startTime` and `maxAge`

An activity's `metadata.creationTimestamp` is when the underlying request was
received, but a watch delivers activities in the order they were published.
Processor catch-up after a restart, durable-consumer replays, DLQ retries and
federated clusters reconnecting can all publish activities whose timestamps are
minutes or days old, and a live feed would otherwise show them as new.

- `startTime` takes RFC3339 (`2026-01-02T15:04:05Z`) or a relative time
  (`now-5m`), parsed like ActivityQuery `spec.startTime` and resolved once when
  the watch starts. Activities created before it are dropped. An absolute time
  in the future (client clock skew, or a client sending its own "now") is
  clamped to the server's current time.
- `maxAge` takes a positive Go duration (`10m`, `1h`). The cutoff slides: at
  delivery, activities older than `now - maxAge` are dropped.

Dropped activities are acknowledged and never sent. Both bounds compare at
whole-second precision (the precision of `creationTimestamp`), and both apply
to resourceVersion replays as well. Invalid values return 400 Bad Request.

### Compatibility: messages published before the subject encoding fix

Before the encoding fix, publishers wrote dotted API groups verbatim
(`...prod.networking.datumapis.com.audit...`), which split into extra tokens,
while the Watch API already subscribed with `_`. Tenant-scoped watches filtered
by API group, kind, or namespace therefore never matched activities of dotted
API groups. The API server encoding of the API group is unchanged, so the
publishers and the API server can be upgraded in any order.

Messages already in the ACTIVITIES stream (7-day retention) keep their dotted
subjects. Only a resourceVersion replay of those older messages through a
tenant-scoped watch filtered by API group, kind or namespace misses them.
Platform-scope watches (`activities.>`), unfiltered tenant-scoped watches, and
everything published after the upgrade are unaffected.

## Scaling

Each API server replica creates independent NATS consumers, requiring no
coordination between replicas. If a client reads slowly, messages buffer in the
consumer until the inactive threshold triggers cleanup. Slow clients should use
the List + Watch pattern to resync state.

## Related Documentation

- [Architecture Overview](./README.md)
- [Activity Pipeline](./activity-pipeline.md) - How activities are generated
- [Multi-tenancy](./multi-tenancy.md) - Scope-based subject filtering
