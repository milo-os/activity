# Activity Policy Events Repeatedly Fail Retries

**Alert**: `ActivityPolicyHighRetryCount`
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

### 3. Examine processor logs

```bash
# Look for repeated errors for this policy
kubectl logs -n activity-system -l app=activity-processor --tail=500 | grep "<policy-name>"
```

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

- Set up alerts for policy CEL errors (ActivityPolicyEventProcessingErrors)
- Review high-retry events periodically to identify patterns
- Ensure policies handle edge cases (DELETE, partial objects, etc.)
