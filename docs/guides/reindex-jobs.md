# Backfilling Activity from Historical Data

You wrote a new policy, but your activity feed only shows new events. What about
everything that happened before? Activities are generated from audit logs and
control plane events as they happen — when you create or update an ActivityPolicy,
only events that arrive *after* that change get translated. Events that already
occurred are not retroactively processed.

A **ReindexJob** fixes this. It replays historical audit logs and events through
your current ActivityPolicy rules and writes the resulting activities. When the
job finishes, your activity feed reflects the full history for the policies and
time range you specified.

## When to use a ReindexJob

| Situation | What to do |
|-----------|------------|
| You added a new ActivityPolicy | Reindex to generate activities for events that predate the policy |
| You updated a policy's match rules | Reindex to apply the corrected matching logic to past events |
| You fixed a policy's summary template | Reindex to regenerate activity text for past events |
| You want to backfill activity for a specific time window | Reindex with an explicit start and end time |

ReindexJob is not necessary for ongoing processing. The activity processor
handles new events continuously — ReindexJob is only for the historical data
that was never translated.

## How reindexing works

When you create a ReindexJob, the system reads through your stored audit logs
and events for the specified time range, applies your current ActivityPolicy
rules, and writes the resulting activities. When it is done, your feed includes
the full history for the policies and time range you selected.

The job runs once and cannot be re-run. To re-process the same time range
again, create a new ReindexJob.

## Creating a ReindexJob

### Minimal example

This reindexes the last 7 days of events against all active ActivityPolicies:

```yaml
apiVersion: activity.miloapis.com/v1alpha1
kind: ReindexJob
metadata:
  name: backfill-last-7-days
spec:
  timeRange:
    startTime: "now-7d"
```

Apply it:

```bash
kubectl apply -f reindexjob.yaml
```

### Targeting a specific policy

If you only updated one policy, you can target that policy to keep things
focused:

```yaml
apiVersion: activity.miloapis.com/v1alpha1
kind: ReindexJob
metadata:
  name: fix-httpproxy-policy
spec:
  timeRange:
    startTime: "now-30d"
    endTime: "now"
  policySelector:
    names:
      - networking-httpproxy
```

You can also select policies by label:

```yaml
spec:
  policySelector:
    matchLabels:
      team: networking
```

### Using an absolute time range

Use RFC3339 timestamps when you need to target a specific historical period:

```yaml
apiVersion: activity.miloapis.com/v1alpha1
kind: ReindexJob
metadata:
  name: backfill-february
spec:
  timeRange:
    startTime: "2026-02-01T00:00:00Z"
    endTime: "2026-03-01T00:00:00Z"
  policySelector:
    names:
      - networking-httpproxy
```

### Time range reference

| Format | Example | Notes |
|--------|---------|-------|
| Relative | `now-7d` | Resolved when processing starts, not when you apply the resource |
| Relative | `now-2h` | Units: `s`, `m`, `h`, `d`, `w` |
| Absolute | `2026-02-01T00:00:00Z` | RFC3339, UTC or with offset |
| Absolute | `2026-02-01T00:00:00-08:00` | RFC3339 with timezone offset |

`endTime` defaults to `"now"` (the moment processing starts) if omitted.

The maximum lookback window is 60 days, matching the retention period. This
limit is enforced when processing starts, not when the job is created — so a
job that sits in `Pending` for a long time could fail if its start time ages
past the 60-day window.

In staging, audit logs are kept for 30 days only. A staging reindex that
reaches further back than 30 days finds no audit logs for the older part of
the window, so the activities it rebuilds there are incomplete.

### Dry run

To see how many events would be processed without writing any activities, set
`dryRun: true`:

```yaml
spec:
  timeRange:
    startTime: "now-7d"
  config:
    dryRun: true
```

The job runs normally and reports progress, but no activities are written.
This is useful for estimating the scope of a reindex before committing.

### Automatic cleanup

Set `ttlSecondsAfterFinished` to have the job resource deleted automatically
after a period. This keeps your control plane tidy without manual cleanup:

```yaml
spec:
  timeRange:
    startTime: "now-7d"
  ttlSecondsAfterFinished: 3600  # Delete 1 hour after completion
```

If omitted, completed jobs are retained indefinitely.

## Monitoring progress

Watch the job status:

```bash
kubectl get reindexjob backfill-last-7-days --watch
```

ReindexJob is cluster-scoped, not namespaced, so you do not need to specify
`-n <namespace>` when running these commands.

The `PHASE` column shows the lifecycle state:

| Phase | Meaning |
|-------|---------|
| `Pending` | Waiting for a processing slot (a concurrency limit is in effect) |
| `Running` | Processing events |
| `Succeeded` | All events processed successfully |
| `Failed` | Processing stopped due to an error |

Get detailed progress including event counts:

```bash
kubectl get reindexjob backfill-last-7-days -o yaml
```

The `status` section includes:

Shortly after creation, before processing has started:

```yaml
status:
  phase: Pending
  message: "Job created, waiting for execution"
  conditions:
    - type: Ready
      status: "False"
      reason: Pending
      message: "Waiting for processing slot"
```

Once processing is underway:

```yaml
status:
  phase: Running
  message: "Processing batch 13 of 46"
  startedAt: "2026-03-10T14:00:00Z"
  progress:
    totalEvents: 45200
    processedEvents: 12300
    activitiesGenerated: 8750
    errors: 0
    currentBatch: 13
    totalBatches: 46
  conditions:
    - type: Ready
      status: "False"
      reason: InProgress
      message: "Re-indexing in progress"
```

`activitiesGenerated` is the count of activities written so far. It will be
less than `processedEvents` because not every event matches a policy rule.

`errors` counts non-fatal processing errors. The job continues processing when
non-fatal errors occur, but those events are skipped.

## What happens when it completes

When the phase reaches `Succeeded`, all matched events have been translated and
written. Your activity feed and any ActivityQuery results for that time range
will now include the backfilled activities.

```bash
kubectl get reindexjob backfill-last-7-days
# NAME                    PHASE       AGE
# backfill-last-7-days    Succeeded   12m
```

You can then query for the newly generated activities using the Activity API.

## What happens when it fails

If the phase is `Failed`, check `status.message` for the reason:

```bash
kubectl get reindexjob backfill-last-7-days -o jsonpath='{.status.message}'
```

Common causes:

- The time range extends beyond the 60-day retention window
- A policy named in `policySelector.names` does not exist
- Processing was interrupted due to resource pressure

Failed jobs are not retried automatically. Create a new ReindexJob to try
again, adjusting the spec to address the root cause.

## Known limitation: control plane events

When a control plane event is updated (for example, a pod OOM event fires five
times and its `count` field increments from 1 to 5), the event retains the same
UID throughout its lifetime. Reindexing produces one activity per event UID,
reflecting the event's final state — the intermediate occurrences are not
recoverable.

**Example:** An event fires 5 times (count = 5). Reindexing generates 1
activity, not 5.

If preserving individual event occurrences matters, scope your ReindexJob to
audit logs only by using `policySelector` to select policies that only have
`auditRules` (no `eventRules`). This avoids reprocessing control plane events
entirely.

For the complete field reference, see the [API reference](../api.md).

## Related documentation

- [ActivityPolicy reference](../api.md#activitypolicy) — how to write policy rules
- [Activity pipeline](../architecture/activity-pipeline.md) — how activities are generated from events
- [API reference](../api.md) — complete spec for all activity resources
