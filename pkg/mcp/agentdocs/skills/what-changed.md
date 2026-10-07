# Skill: what changed

Use when someone asks what changed, what happened, or what is different —
"what changed in the last hour?", "anything happen overnight?", "what did we
change before it broke?" — without naming a single resource.

## Procedure

1. **Pin the window.** Turn the question into `startTime` / `endTime`. "Last
   hour" is `now-1h` to `now`; "overnight" or "yesterday" become absolute RFC
   3339 times in UTC. If they mention when a problem started, end the window
   at that moment and start it a few hours earlier: the cause precedes the
   symptom.

2. **Get the shape first.** Call `summarize_recent_activity` for the window.
   It gives the total, the human/system split, top actors, most-changed kinds,
   and the latest summaries. If the total is 0, say nothing was recorded in
   this project for that window and offer to widen it. Stop there.

3. **Separate people from automation.** When the question is about what
   someone did, repeat with `changeSource: "human"`. System changes
   (controllers reconciling, certificates renewing) are usually noise unless
   the question is about automation.

4. **Find when.** If the window is longer than a few hours, call
   `get_activity_timeline` (`bucketSize: "hour"`, or `"day"` beyond two days).
   The peak bucket is where to look; a spike right before a reported problem is
   the lead.

5. **List the changes.** Call `query_activities` for the narrowed window,
   filtering by `resourceKind`, `actorName` or `changeSource` as the earlier
   steps suggest. Prefer a narrower window to a higher `limit`.

6. **Drill into a suspect.** For any resource that looks related, call
   `get_resource_history` with its `name` and `kind` to see its full sequence
   of changes and who made them.

7. **Compare with normal, if asked.** "Is this unusual?" is
   `compare_activity_periods`: the same length of window a day or a week
   earlier as the baseline, the window in question as the comparison.

## Answer

Lead with the few changes that matter, in time order: who, what, which
resource, when. Then the counts. Name the window you searched. If a total is
exactly 1000, it is capped: say "at least 1000".

## Do not

Do not report "nothing changed" from an empty result alone. Only resource types
with translation rules produce Activities; if the person expected a change,
check `find_failed_operations` (it may have been refused) and `query_events`
for the resource before concluding.
