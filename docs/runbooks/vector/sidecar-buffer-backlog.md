# Audit Sidecar Backing Up on Disk

**Alert**: `VectorSidecarBufferBacklog`
**Severity**: Warning
**Team**: Platform SRE

## Symptoms

`{{ $labels.pod }}` has >25,000 audit events in its `nats_jetstream` disk buffer
for 15+ minutes. Healthy peak is ~3,200 events (p95 ~400).

## Impact

Audit events from this apiserver replica are delayed and not yet queryable. The
buffer is 10 GB with `when_full: block` — once full, the sidecar stops accepting
batches from the apiserver and events are lost outright.

## Investigation

Start with the sink's drain rate:

```
rate(vector_component_received_events_total{namespace="activity-system",component_id="nats_jetstream"}[5m])
```

- **Zero** — the sink is wedged. Follow
  [sidecar-nats-publish-stalled.md](sidecar-nats-publish-stalled.md);
  that alert should also be firing.
- **Draining** — a stall already fixed. Nothing to do but watch it clear.
- **Non-zero but below intake** — NATS is slow, or volume has outgrown the sink:

```bash
kubectl -n nats-system get pods
kubectl -n nats-system exec nats-0 -c nats -- \
  wget -qO- 'http://localhost:8222/jsz?streams=1&config=1'
kubectl -n activity-system logs <pod> --tail=200 | grep -i "nats\|timed out"
# Buffer size comes from metrics; the vector image has no shell or df:
#   vector_buffer_byte_size{namespace="activity-system", pod="<pod>"}
```

Publish-ack timeouts without payload violations point at a degraded NATS cluster.

## Resolution

- **NATS degraded** — restore NATS; the buffer drains on its own
- **Sustained overload** — escalate to the Activity team; this needs a config
  change, not an operational fix

> [!NOTE]
> Do not restart the sidecar to clear a backlog. The buffer is on disk and
> survives restarts; a restart only stops the drain already in progress.

## Confirming recovery

```
vector_buffer_events{namespace="activity-system",component_id="nats_jetstream"}
```

Clears below 25,000. Draining runs at roughly 200–300 events/s per pod. Once the
buffer is empty, query ClickHouse for the affected window to confirm the events
arrived.

## Escalation

- NATS cluster health: Infrastructure team
- Volume beyond sink throughput: Activity team

## Prevention

Keep NATS `max_payload` above the largest audit event the apiserver emits. Buffer
depth is a stable leading indicator — any sustained rise is real.
