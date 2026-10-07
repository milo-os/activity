# Skill: who changed a resource

Use when someone asks who changed, created, deleted or last touched a specific
resource — "who deleted the api-gateway proxy?", "when was this domain last
modified?", "who's been editing secret db-creds?".

## Procedure

1. **Identify the resource.** You need its `name` and, whenever possible, its
   `kind` (HTTPProxy, Domain, Workload, Secret, ...): names repeat across kinds.
   If the kind is unclear, call `get_activity_facets` with field
   `spec.resource.kind` and a filter on the name, e.g.
   `spec.resource.name == "api-gateway"`, to see which kinds carry that name.

2. **Get the history.** Call `get_resource_history` with `name`, `kind`, and
   `apiGroup` / `namespace` if known. The default window is the last 30 days;
   widen `startTime` (e.g. `now-90d`) if the history is empty and the resource
   is older.

3. **Read it newest-first.** Each entry has a timestamp, actor, summary and
   changeSource. The most recent human entry usually answers "who changed it";
   the earliest "created" entry answers "who made it".

4. **Explain system entries.** If the latest changes are `changeSource:
   "system"`, a controller changed it, often in response to an earlier human
   change. Report the human change that preceded it as well.

5. **Check refused attempts.** If the person believes someone tried to change
   it and nothing shows, call `find_failed_operations` with the plural
   `resource` name (e.g. `httpproxies`) and the same window. A 403 means the
   person lacked permission; a 409 or 422 means the change was rejected.

6. **Widen to the actor, if asked.** "What else did they change?" is
   `get_user_activity_summary` with that actor's name exactly as it appears in
   the history, `includeDetails: true`.

## Answer

Who, what they did, when (UTC), quoting the actor and resource names verbatim.
If the history is empty, say no changes to that resource were recorded in this
project in the window, and name the window.

## Do not

Do not guess an actor from timing alone. Only state who changed a resource
when an Activity or failed operation names them.
