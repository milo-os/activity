# Querying activity-processor logs with Loki

Every DLQ runbook in this directory eventually needs you to read the
processor's logs. **Use Loki, not `kubectl logs`, for DLQ triage.**
Dead-lettered events are frequently older than the current pods — they survive
rollouts and can sit unprocessed for days — and triage usually means
*aggregating* or *counting* across time, which `kubectl logs --tail` cannot do.

## Running these queries

Open **Grafana → Explore → select the Loki datasource**, then paste any LogQL
query below into the query box and run it. Expand a log line to see all of its
parsed fields. For the aggregating queries (the `sum ... count_over_time` and
`unwrap` examples), switch the Explore panel to **Table** or **Graph** to read
the result.

## Base selector & log shape

Start every query from:

```logql
{namespace="activity-system", container="processor"}
```

The processor logs **structured JSON** — pipe through `| json` to filter and
extract fields. The useful ones:

| Field | Meaning |
|---|---|
| `msg` | log message (wording varies across releases — see note below) |
| `policy` | ActivityPolicy name, e.g. `networking.datumapis.com-connector` |
| `errorType` | DLQ failure class: `cel_summary`, `cel_match`, `unmarshal`, `kind_resolve` — **the primary triage dimension** |
| `err` | underlying error string; for CEL errors it names the failing rule and missing key |
| `auditID` | source audit event ID — use it to follow one event across retries |
| `subject` | NATS subject the event was dead-lettered to (`activity.dlq.<group>.<Kind>`) |
| `retryCount`, `totalProcessed`, `totalSucceeded`, `totalFailed` | on retry-run lines |

> Prefer filtering on the **stable structured fields** (`errorType`, `policy`,
> `err`) and broad keywords (`|~ "(?i)dlq|dead.?letter|evaluat"`) over full
> `msg` strings — message wording is refactored from time to time, the fields
> and the `errorType` taxonomy are not.

## Triage recipes (by alert)

### Which policies / fields are failing — `DLQQueueGrowing`, `ActivityPolicyDLQErrors`

```logql
# everything dead-lettered, grouped by policy + error class, last hour
sum by (policy, errorType) (
  count_over_time({namespace="activity-system", container="processor"} | json | errorType != "" [1h])
)
```

Then read `err` on a sample line. For `errorType="cel_summary"`, `err` looks
like:

```
rule N summary: ... eval "link(audit.objectRef.name, audit.objectRef)": no such key: name
```

i.e. the summary dereferences a field that isn't present on this event shape.
The classic case: `objectRef.name` is empty for `generateName` creates (the
assigned name is in `responseObject.metadata.name`). See
[policy-dlq-errors.md](./policy-dlq-errors.md) for the fix pattern.

### Pin the exact failing expression for one policy

```logql
{namespace="activity-system", container="processor"} | json | policy="<policy-name>" |~ "(?i)evaluat|dlq"
```

### Real backlog vs. a churn loop — `DLQHighRetryCount`, `DLQRetryIneffective`

A single event re-failing over and over (same `auditID`) is a stuck loop, not
progress. Pull the auditIDs and look for repeats:

```logql
{namespace="activity-system", container="processor"} | json | errorType="cel_summary" | line_format "{{.auditID}}"
```

Few distinct IDs across many lines ⇒ a handful of events looping (fix the
policy). Many distinct IDs ⇒ broad breakage (likely a shared field/shape bug).

### Retry effectiveness — `DLQRetryIneffective`

```logql
# failed vs succeeded across retry runs, last hour
sum(sum_over_time({namespace="activity-system", container="processor"} | json |~ "retry run" | unwrap totalFailed [1h]))
sum(sum_over_time({namespace="activity-system", container="processor"} | json |~ "retry run" | unwrap totalSucceeded [1h]))
```

### DLQ publish failures (events being lost) — `DLQPublishErrors`

```logql
{namespace="activity-system", container="processor"} |~ "(?i)failed to publish.*dlq|NAK"
```

## Gotchas

- **Lines are truncated (~4096 chars)** by the log collector. `auditID`,
  `policy`, `errorType`, and `err` sit early and survive, but the full
  `eventJSON` payload is usually cut off. To inspect a complete failing event,
  look it up live by name/auditID rather than relying on the logged payload.
- **Mind the query window.** Loki enforces `max_query_length` (commonly ~30d) —
  very wide ranges error out — and retains logs only for a limited period.
  **Failures older than retention are not in Loki**; fall back to metrics or the
  NATS stream.
- **DLQ "depth" is a metric, not a log, and can mislead.** Real depth is
  `nats_stream_total_messages{stream_name="ACTIVITY_DEAD_LETTER"}` in
  VictoriaMetrics. It can read **0 even while events are actively failing**,
  because the stream is WorkQueue with a dedup window — failing events are
  acked/dropped, not retained. Never infer "healthy" from depth alone;
  corroborate with the eval-failure logs above.
