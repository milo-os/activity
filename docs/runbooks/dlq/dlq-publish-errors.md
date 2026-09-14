# DLQ Publish Errors

**Alert**: `DLQPublishErrors`
**Severity**: Warning
**Team**: Platform SRE

## Symptoms

Failed to publish events to the Dead Letter Queue (DLQ). Events may be lost entirely.

## Impact

When DLQ publishing fails, events that fail processing are not captured. This means:
- Complete data loss for affected events
- No visibility into what's failing
- No retry possible since events aren't preserved

## Investigation

### 1. Check DLQ stream health

```bash
# Check NATS JetStream streams
kubectl exec -n nats-system deploy/nats-box -- nats stream info ACTIVITY_DEAD_LETTER

# Check for stream errors
kubectl logs -n nats-system -l app.kubernetes.io/name=nats --tail=100 | grep -i error
```

### 2. Check processor logs (Loki)

Query Loki — see [querying-logs-with-loki.md](./querying-logs-with-loki.md):

```logql
{namespace="activity-system", container="processor"} |~ "(?i)failed to publish.*dlq|NAK"
```

These lines mean the processor could not even write to the DLQ — events are
being lost. This is almost always a NATS/JetStream problem (see below), not a
policy problem.

### 3. Check NATS connectivity

```bash
# Verify processor can reach NATS
kubectl exec -n activity-system deploy/activity-processor -- nc -zv nats.nats-system.svc 4222
```

### 4. Common causes

**NATS unavailable**:
- NATS cluster down or restarting
- Network partition between processor and NATS

**DLQ stream not created**:
- Stream resources not deployed
- NATS JetStream controller not reconciling

**Stream full**:
- DLQ retention exceeded
- Storage quota reached

## Resolution

### If NATS is unavailable

```bash
# Check NATS cluster status
kubectl get pods -n nats-system

# Restart NATS if needed
kubectl rollout restart statefulset/nats -n nats-system
```

### If DLQ stream missing

```bash
# Check if stream exists
kubectl get stream -n nats-system activity-dead-letter

# If missing, verify NATS JetStream resources are deployed
kubectl get stream,consumer -n nats-system
```

### If stream is full

```bash
# Check stream state
kubectl exec -n nats-system deploy/nats-box -- nats stream info ACTIVITY_DEAD_LETTER

# If approaching limits, consider:
# 1. Increasing retention policy
# 2. Manually purging old messages
# 3. Fixing underlying policy issues causing DLQ growth
```

## Escalation

- If NATS cluster is unhealthy: Escalate to NATS/Infrastructure team
- If DLQ stream configuration issue: Review NATS JetStream resources
- If persistent publish failures: Escalate to Activity development team

## Prevention

- Monitor NATS cluster health
- Set up alerts for DLQ stream storage utilization
- Ensure DLQ stream has appropriate retention and storage limits
