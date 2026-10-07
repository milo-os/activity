# Skill: failed operation triage

Use when a change did not take — "my deploy failed", "I can't delete this",
"why was my update rejected?", "are we getting permission errors?".

Failed requests never become Activities. `find_failed_operations` is the only
tool that sees them.

## Procedure

1. **Search the failures.** Call `find_failed_operations` with a tight window
   around when it happened (default to `now-1h` if they say "just now"). Narrow
   with `username`, `verb` (create, update, patch, delete) and `resource` (the
   plural API name, e.g. `httpproxies`, `domains`, `workloads`) when known.

2. **Group by status code.** `byStatusCode` gives the split. Read the codes:

   | Code | What it means | Who acts |
   |---|---|---|
   | 401 | The request was not signed in | The person or their tool: sign in again |
   | 403 | Signed in, but not allowed to do this | Someone who manages access to the project |
   | 404 | The resource, or something it refers to, does not exist | The person: check the name |
   | 409 | Conflict: it already exists, or was changed by someone else first | The person: re-read and retry |
   | 422 / 400 | The request was invalid; the message says which field | The person: fix the field |
   | 429 | Too many requests | Wait and retry |
   | 5xx | A fault on Datum's side | Datum, if it persists |

3. **Quote the message.** Each failure carries the server's `message`. It
   usually names the field or permission involved. Quote it verbatim after
   your plain-language explanation.

4. **Check for a pattern.** Many failures from one user or one resource point
   to a broken script or a missing permission rather than a one-off. Say so.

5. **Confirm whether it later succeeded.** Call `get_resource_history` for the
   resource. A later successful change means the failure was transient or was
   fixed.

6. **For errors on Datum's side,** call `query_events` with `type: "Warning"`
   and `regardingName` set to the resource to see whether the platform
   reported a related problem.

## Answer

What failed, why in plain words, and who can fix it — then the status code,
timestamp, and quoted message.

## Do not

Do not tell the person to retry a 403 or 422 unchanged; it will fail the same
way. Do not tell them to re-authenticate for anything but a 401.
