# DLQ Queue Growing

**Alert**: `DLQQueueGrowing`
**Severity**: Warning
**Team**: Platform SRE

## Symptoms

DLQ message count increasing over 15 minutes. Events failing faster than retry can handle.

## Impact

Failed events accumulating. May indicate:
- Systematic policy bug affecting all events
- Processor issue preventing event processing
- Transient cluster state change (CRD deleted, API group unavailable)

## Investigation

### 1. Check DLQ metrics

```bash
# View current DLQ publish rate by error type
kubectl exec -n activity-system deploy/prometheus -- \
  promtool query instant 'sum by (error_type) (rate(activity_processor_dlq_events_published_total[5m]))'

# View which policies are failing
kubectl exec -n activity-system deploy/prometheus -- \
  promtool query instant 'topk(10, sum by (policy_name) (rate(activity_processor_dlq_events_published_total[5m])))'
```

### 2. Check processor logs (Loki)

DLQ failures often predate the current pods, so query Loki rather than
`kubectl logs`. Full query guide: [querying-logs-with-loki.md](./querying-logs-with-loki.md).

```logql
# what's failing, grouped by policy + error class (last 15m)
sum by (policy, errorType) (
  count_over_time({namespace="activity-system", container="processor"} | json | errorType != "" [15m])
)
```

Read `err` on a sample line to see the failing rule/field, then drill into one policy:

```logql
{namespace="activity-system", container="processor"} | json | policy="<policy-name>" |~ "(?i)evaluat|dlq"
```

### 3. Common error patterns

**If error_type=cel_match or cel_summary**:
- Policy has CEL bug
- Check policy with `kubectl get activitypolicy <name> -o yaml`
- Review recent policy changes
- Fix: Update policy with correct CEL expression

**If error_type=unmarshal**:
- Upstream event format changed
- Check sample event structure
- Fix: Update event schema or add validation

**If error_type=kind_resolve**:
- CRD deleted or API group unavailable
- Check if resource exists: `kubectl api-resources | grep <kind>`
- Fix: Restore CRD or update policy to reference correct kind

### 4. Check processor health

```bash
kubectl get pods -n activity-system -l app=activity-processor
kubectl logs -n activity-system -l app=activity-processor --tail=100
```

## Resolution

### For policy CEL errors

```bash
# Identify the failing policy
kubectl get activitypolicy -o wide

# Edit the policy
kubectl edit activitypolicy <policy-name>

# Verify fix with PolicyPreview
kubectl create -f - <<EOF
apiVersion: activity.miloapis.com/v1alpha1
kind: PolicyPreview
metadata:
  name: test-fix
spec:
  policy:
    # Copy policy spec here
  inputs:
    # Add sample audit event from DLQ
EOF

# Policy update triggers immediate retry of failed events
# Monitor DLQ metrics for queue to drain
```

### For resource resolution errors

```bash
# If CRD was deleted, restore it
kubectl apply -f <crd-manifest>

# If policy references wrong kind, update policy
kubectl edit activitypolicy <policy-name>
```

### For unmarshal errors

- Requires code change to handle new event format
- Escalate to Activity development team

## Escalation

- If DLQ continues growing after policy fix: Escalate to Activity team
- If processor appears unhealthy: Restart processor pods
- If widespread across many policies: Check for cluster-wide issue

## Prevention

- Test policies with PolicyPreview before deploying
- Monitor policy validation metrics
- Add integration tests for new event formats
