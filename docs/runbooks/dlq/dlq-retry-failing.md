# Activity Policy Retries Failing

**Alert**: `ActivityPolicyRetriesFailing`
**Severity**: Warning
**Team**: Platform SRE

## What this means

A named activity policy has at least three failed retry re-evaluations in a
rolling one-hour window, and that condition has persisted for 30 minutes.
The alert keeps `cluster`, `namespace`, `policy_name`, `api_group`, `kind`, and
`error_type`, aggregating processor replicas within those dimensions. Its
`subject` is the policy name so notifications can identify and group by policy.

Affected events are not producing activity entries. The count is retry attempts,
not unique events; one event can fail repeatedly. This measures policy
re-evaluation failures, not a success percentage or current queue depth. A clear
alert means the recent failure count fell below the threshold, not necessarily
that every retained event recovered; retries can back off for up to 24 hours.

`activity_processor_dlq_retry_failed_total` includes the policy name and error
category. The older `activity_processor_dlq_retry_attempts_total` has no policy
label, so it cannot provide a per-policy success ratio. Failures to republish an
event to NATS are not counted by the re-evaluation metric; investigate processor
or queue health alerts separately when delivery fails.

## Investigation

1. Use the notification's policy, cluster, resource type, and error category to
   locate the failing evaluation. Query VictoriaMetrics or Prometheus:

   ```promql
   sum by (cluster, namespace, policy_name, api_group, kind, error_type) (
     increase(activity_processor_dlq_retry_failed_total{policy_name!=""}[1h])
   )
   ```

   Filter to the alert's labels when multiple policies or clusters are present.

2. Read both activity-processor replicas in the affected cluster:

   ```bash
   kubectl logs -n activity-system -l app=activity-processor \
     --prefix --since=2h --tail=2000 | grep 'DLQ event re-failed evaluation'
   ```

   Match the logged `policy`, `errorType`, failing rule, and `retryCount` to the
   notification. The initial `Failed to evaluate policy` log may also include
   the audit ID and event payload; payloads can be truncated.

3. Inspect the ActivityPolicy through the Milo API context that owns it:

   ```bash
   kubectl --context <milo-context> get activitypolicy <policy-name> -o yaml
   ```

   Reproduce the failure with the captured payload. In particular, audit
   `requestObject` can be a JSON Patch array rather than an object. Accessing
   `.spec` on that array can fail with `unsupported index type 'string' in list`.
   Check match expressions as well as summary expressions; `cel_summary` can
   accompany a logged match-expression error.

## Resolution and verification

Fix the affected rule in its source repository and add regression coverage for
the failing payload shape. Deploy the policy through its normal release path.
Do not discard retained events or disable retries to clear this alert.

Policy updates can trigger retries; periodic retries also use backoff. Verify
successful re-evaluation and resulting activity entries for the affected events.
Watch the policy's failure counter stop increasing. The one-hour window can
retain old failures after a fix, so the alert may take up to an hour to clear.
If failures continue, inspect the latest error rather than repeatedly replaying
the same payload against an unchanged policy.

Escalate policy evaluation errors to the policy owner. Escalate unexpected
processor behavior to the Activity maintainers. A processor or NATS delivery
failure requires separate investigation; this alert does not measure it.
