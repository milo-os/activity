# Audit Sidecar Stopped Publishing to NATS

**Alert**: `VectorSidecarNATSPublishStalled`
**Severity**: Critical
**Team**: Platform SRE

## Symptoms

`{{ $labels.pod }}` is still transforming audit events but its `nats_jetstream`
sink has consumed none for 5+ minutes. Events are piling up in its disk buffer.

## Impact

One sidecar runs per milo-apiserver replica, so a wedged pod silently drops that
replica's share of the audit trail — about a third with three replicas — while
aggregate metrics stay healthy. Loss is permanent once the 10 GB buffer fills.

## Investigation

### 1. Read the sidecar logs

```bash
kubectl -n activity-system logs <pod> --tail=200 | grep -i "nats\|error"
```

The signature of this failure is the sink retrying one unchanging `request_id`:

```
NATS Server Error: NATS Publish Error: timed out: didn't receive ack in time
event: server error: nats: Maximum Payload Violation
```

A single audit event exceeds the NATS server's `max_payload`. The sink is
ordered, so it head-of-line blocks everything behind it and retries forever.
Go to [Resolution](#maximum-payload-violation).

### 2. If no payload violation, check NATS

```bash
kubectl -n nats-system get pods
kubectl -n nats-system exec nats-0 -c nats -- \
  wget -qO- 'http://localhost:8222/jsz?streams=1&config=1'
```

Publish-ack timeouts alone mean a degraded or unreachable NATS, or a stream at a
limit. Restore NATS; the sidecars drain on their own.

## Resolution

> [!WARNING]
> Restarting the sidecar does **not** clear this. The buffer is `type: disk` with
> `when_full: block`, so the poison message survives the restart and the sink
> wedges again immediately.

### Maximum Payload Violation

Check the current limit, then raise it. `max_payload` is reloadable — SIGHUP, no
pod restart, no loss of buffered messages:

```bash
# Current limit, per server — repeat for nats-1 and nats-2
kubectl -n nats-system exec nats-0 -c nats -- wget -qO- http://localhost:8222/varz \
  | grep -o '"max_payload":[0-9]*'

# The reloader sidecar picks the new config up on its own. Only if it does not:
kubectl -n nats-system exec nats-0 -c nats -- nats-server --signal reload
```

The 1 MiB default is below the size of a large audit event. Land the change in
the deployment repo so it survives the next reconcile, and confirm every server
picked it up with the `varz` command above.

## Confirming recovery

The sinks resume on their own. Buffers drain at roughly 200–300 events/s per pod
and can take an hour after a long stall:

```
vector_buffer_events{namespace="activity-system",component_id="nats_jetstream"}
rate(vector_component_received_events_total{namespace="activity-system",component_id="nats_jetstream"}[5m])
```

`VectorSidecarBufferBacklog` stays up until they drain — expected. Once empty,
query ClickHouse for the stalled window and compare event counts per apiserver
replica to confirm the events landed.

## Escalation

- NATS configuration changes: Infrastructure team
- Audit events larger than any sane `max_payload`: Activity team — the sidecar
  has no dead-letter path for unpublishable events, so it blocks instead

## Prevention

Keep `max_payload` above the largest audit event the apiserver can emit.
