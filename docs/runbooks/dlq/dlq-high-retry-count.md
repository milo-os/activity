# DLQ High Retry Count

**Alert**: `DLQHighRetryCount`
**Severity**: Warning
**Team**: Platform SRE

## Symptoms

DLQ events have been retried many times without success, exceeding the high retry threshold (default: 10 retries).

## Impact

Events failing persistently indicate:
- Policy bug not yet fixed
- Persistent cluster state issue (CRD deleted, API unavailable)
- Retry backoff too aggressive or underlying issue unresolvable

## Investigation

### 1. Identify high-retry events

```bash
# Check metrics for high retry events
kubectl exec -n activity-system deploy/prometheus -- \
  promtool query instant 'activity_processor_dlq_retry_events_high_retry_total'

# The labels show which api_group/kind/policy are affected
```

### 2. Check the policy status

```bash
# Get the policy mentioned in the alert
kubectl get activitypolicy <policy-name> -o yaml

# Check if policy is Ready
kubectl get activitypolicy <policy-name> -o jsonpath='{.status.conditions}'
```

### 3. Examine processor logs (Loki)

Query Loki — see [querying-logs-with-loki.md](./querying-logs-with-loki.md).
High retry counts usually mean one policy is broken and a small set of events is
looping. Confirm by checking how many *distinct* `auditID`s are re-failing:

```logql
{namespace="activity-system", container="processor"} | json | policy="<policy-name>" | line_format "{{.auditID}}"
```

A few IDs across many lines ⇒ those events are stuck on an unfixed policy;
read `err` on a sample line for the exact failing rule/field.

### 4. Common causes

**Policy never fixed**:
- Policy has CEL error that was never corrected
- Team responsible for policy unaware of the issue

**Transient issue became permanent**:
- CRD deleted and never restored
- API group no longer available

**Edge case events**:
- Some events have unusual structure that policy can't handle
- DELETE operations missing responseObject

## Resolution

### If policy has CEL error

```bash
# Review and fix the policy
kubectl edit activitypolicy <policy-name>

# After fixing, policy update triggers immediate retry
# High-retry events should succeed on next attempt
```

### If resource type no longer exists

```bash
# Check if the API group/kind still exists
kubectl api-resources | grep <kind>

# If resource type is gone:
# 1. Delete the policy (it's no longer needed)
kubectl delete activitypolicy <policy-name>

# 2. Or update to point to correct kind
kubectl edit activitypolicy <policy-name>
```

### If events are truly unrecoverable

Some events may be malformed or represent resources that no longer exist. After investigation, if events cannot be processed:

```bash
# Events will eventually age out based on DLQ retention policy
# Or manually purge old events if needed (use with caution)
kubectl exec -n nats-system deploy/nats-box -- \
  nats stream purge ACTIVITY_DEAD_LETTER --subject "activity.dlq.*.*.HTTPProxy"
```

## Verification

After fixing the policy:
1. Monitor `activity_processor_dlq_retry_attempts_total{result="succeeded"}` - should increase
2. Monitor high retry metric - should stop growing
3. Verify activities being generated for affected resource type

## Prevention

- Set up alerts for policy CEL errors (ActivityPolicyDLQErrors)
- Review high-retry events periodically to identify patterns
- Ensure policies handle edge cases (DELETE, partial objects, etc.)
