# Skill: event investigation

Use when a resource is not working, stuck, or degraded and the question is
why — "why isn't my proxy ready?", "what's wrong with this workload?", "are
there any warnings?".

Events are what the platform reports about a resource after it changes:
progress, warnings, errors. Activities say who changed it; events say what
happened next.

## Procedure

1. **See what is being reported.** Call `get_event_facets` with fields
   `type`, `reason` and `regarding.kind` over a recent window (e.g. `now-24h`).
   A rise in Warning events, or one reason dominating, is the lead.

2. **Read the warnings.** Call `query_events` with `type: "Warning"` for the
   window. If the person named a resource, add `regardingKind` and
   `regardingName`. Read `reason`, `message`, `count` and `source.component`.

3. **Get the full sequence for the resource.** Call `query_events` again for
   that resource without the type filter. Normal events show what did succeed;
   the last one before the warnings shows where it stopped.

4. **Find the triggering change.** Call `get_resource_history` for the same
   resource. A human change shortly before the first warning is the likely
   cause; quote it.

5. **Check whether the change was accepted at all.** If there are no events
   and no history, call `find_failed_operations` for the resource: the change
   may have been refused before anything ran.

6. **Count and time it.** A high `count` means the platform is retrying. Say
   whether it is still happening: compare the latest event timestamp with now.

## Answer

The resource's state in one sentence, the most likely cause, and what to do —
then the event reason, message (verbatim), count and timestamps.

## Do not

Do not present every Normal event as a problem. Do not infer a fix the event
message does not support; if the reason is unfamiliar, quote it and say what
the message says.
