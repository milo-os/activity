# ActivityPolicy DLQ Errors

**Alert**: `ActivityPolicyDLQErrors`
**Severity**: Warning
**Team**: Policy owner (from policy annotation)

## Symptoms

A specific ActivityPolicy is sending events to the Dead Letter Queue (DLQ).

## Impact

Activities not being generated for this resource type. Users see incomplete timeline.

## Investigation

Alert includes these labels:
- `policy_name`: Which policy is failing
- `api_group`: Resource API group
- `kind`: Resource kind
- `error_type`: Type of error (cel_match, cel_summary, unmarshal, kind_resolve)

### 1. Get policy details

```bash
kubectl get activitypolicy <policy-name> -o yaml
```

### 2. Check processor logs for this policy (Loki)

Query Loki (failures may predate the current pods) — see
[querying-logs-with-loki.md](./querying-logs-with-loki.md):

```logql
{namespace="activity-system", container="processor"} | json | policy="<policy-name>" |~ "(?i)evaluat|dlq"
```

Read the `err` field — for `errorType="cel_summary"` it names the failing rule
and the missing key (e.g. `no such key: name`), which tells you exactly which
field the summary/match dereferences unsafely.

### 3. Test policy with sample event

```bash
# Create PolicyPreview with a sample audit event
kubectl create -f - <<EOF
apiVersion: activity.miloapis.com/v1alpha1
kind: PolicyPreview
metadata:
  name: debug-<policy-name>
spec:
  policy:
    # Copy the failing policy spec here
  inputs:
    - type: audit
      audit:
        # Paste sample audit event that's failing
EOF

# Check result
kubectl get policypreview debug-<policy-name> -o yaml
```

### 4. Common issues

**CEL expression references field that doesn't exist**:
```yaml
# Before - fails if responseObject is nil (DELETE operations)
match: "audit.responseObject.spec.replicas > 0"

# After - safely check existence
match: "has(audit.responseObject) && has(audit.responseObject.spec) && audit.responseObject.spec.replicas > 0"
```

**CEL syntax error in match or summary**:
- Check CEL expression syntax
- Verify all variables are defined

**Template variable undefined**:
```yaml
# Before - fails when actor is not available
summary: "{{ actor }} created resource"

# After - provide fallback
summary: "{{ has(actor) ? actor : 'system' }} created resource"
```

## Resolution

Fix policy CEL expression:

```bash
kubectl edit activitypolicy <policy-name>

# Common fixes:

# 1. Add field existence checks
# Before: audit.verb == 'create'
# After:  has(audit.verb) && audit.verb == 'create'

# 2. Add null checks for nested fields
# Before: audit.responseObject.metadata.name
# After:  has(audit.responseObject) ? audit.responseObject.metadata.name : audit.objectRef.name

# 3. Handle DELETE operations (no responseObject)
# Before: {{ audit.responseObject.spec.type }}
# After:  {{ has(audit.responseObject) ? audit.responseObject.spec.type : 'deleted' }}
```

Policy update triggers immediate retry of all failed events.

## Verification

After fix:
1. Monitor DLQ metrics - events for this policy should stop increasing
2. Check retry metrics - retries should succeed
3. Verify activities are being generated:
   ```bash
   kubectl activity query --api-group <group> --kind <kind> --start-time "now-1h"
   ```

## Prevention

- Always test policies with PolicyPreview before deploying
- Use `has()` function to check field existence before accessing
- Add fallback values for optional fields
- Test policies against DELETE, CREATE, UPDATE, and PATCH operations
