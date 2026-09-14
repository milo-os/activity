# DLQ Retry Ineffective

**Alert**: `DLQRetryIneffective`
**Severity**: Warning
**Team**: Platform SRE

## Symptoms

More than 80% of DLQ retry attempts are failing. The automatic retry mechanism is not recovering events.

## Impact

When retry is ineffective:
- Events accumulate in DLQ without recovery
- Activities are not being generated for failed events
- Manual intervention required to resolve underlying issues

## Investigation

### 1. Check retry success rate

```bash
# View retry attempt metrics
kubectl exec -n activity-system deploy/prometheus -- \
  promtool query instant 'sum by (result) (rate(activity_processor_dlq_retry_attempts_total[15m]))'

# Check which api_group/kind are failing
kubectl exec -n activity-system deploy/prometheus -- \
  promtool query instant 'sum by (api_group, kind) (rate(activity_processor_dlq_retry_attempts_total{result="failed"}[15m]))'
```

### 2. Check processor logs for retry errors (Loki)

Query Loki — see [querying-logs-with-loki.md](./querying-logs-with-loki.md).
Compare failed vs. succeeded across retry runs:

```logql
sum(sum_over_time({namespace="activity-system", container="processor"} | json |~ "retry run" | unwrap totalFailed [15m]))
sum(sum_over_time({namespace="activity-system", container="processor"} | json |~ "retry run" | unwrap totalSucceeded [15m]))
```

If the same `auditID` keeps re-failing, the underlying policy is still broken
(retry can't fix a bad policy) — pull the IDs to confirm a loop:

```logql
{namespace="activity-system", container="processor"} | json | errorType != "" | line_format "{{.auditID}}"
```

### 3. Identify root cause

Retry can fail for several reasons:

**Source stream unavailable**:
- AUDIT_EVENTS or EVENTS stream not accepting messages
- Stream full or unavailable

**Policy still broken**:
- Policy was updated but CEL error still exists
- Events match old version filter but still fail evaluation

**Republish errors**:
- Network issues between processor and NATS
- Stream subject mismatch

### 4. Check source streams

```bash
# Verify audit stream accepts messages
kubectl exec -n nats-system deploy/nats-box -- nats stream info AUDIT_EVENTS

# Verify events stream accepts messages
kubectl exec -n nats-system deploy/nats-box -- nats stream info EVENTS
```

## Resolution

### If source stream is unavailable

```bash
# Check stream status
kubectl get stream -n nats-system

# If stream is in error state, check NATS logs
kubectl logs -n nats-system -l app.kubernetes.io/name=nats --tail=100
```

### If policies still have errors

```bash
# List policies with recent DLQ events
kubectl exec -n activity-system deploy/prometheus -- \
  promtool query instant 'topk(10, sum by (policy_name) (rate(activity_processor_dlq_events_published_total[10m])))'

# Fix each problematic policy
kubectl edit activitypolicy <policy-name>
```

### If republish subjects don't match stream

The retry controller uses subjects:
- `audit.k8s.retry` for audit events
- `events.retry` for Kubernetes events

Verify these match the stream subject filters:
```bash
kubectl get stream audit-events -n nats-system -o yaml | grep subjects
kubectl get stream events -n nats-system -o yaml | grep subjects
```

If subjects don't match, update the stream configuration or processor subjects.

### Temporary workaround

If retries continue failing, you can:

1. **Disable automatic retry temporarily**:
   ```bash
   kubectl set env deployment/activity-processor -n activity-system DLQ_RETRY_ENABLED=false
   ```

2. **Manually process DLQ events** after fixing underlying issues:
   ```bash
   # Re-enable retry
   kubectl set env deployment/activity-processor -n activity-system DLQ_RETRY_ENABLED=true
   ```

## Escalation

- If NATS streams are unhealthy: Escalate to NATS/Infrastructure team
- If retry logic appears broken: Escalate to Activity development team
- If multiple policies failing: Check for cluster-wide configuration issue

## Prevention

- Monitor retry success rate metrics
- Fix policy CEL errors promptly when ActivityPolicyDLQErrors alerts fire
- Ensure NATS stream configurations match retry subject patterns
