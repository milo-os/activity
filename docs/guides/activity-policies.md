# Writing Activity Policies

When something happens in your control plane — a resource is created, a
controller reports an error, a configuration change is applied — the activity
system can describe it in plain language. ActivityPolicy is how you teach it
what to say.

Without policies, audit logs and events flow through the system but produce no
Activity records. You write one policy per resource kind, and from that point
on the system knows how to translate raw events into summaries that appear in
activity feeds.

This guide walks you through writing policies from scratch, understanding the
CEL (Common Expression Language) expressions and template syntax, and testing
your work with PolicyPreview before deploying.

## How ActivityPolicy works

When an audit log or control plane event arrives, the activity processor looks up
the ActivityPolicy for that event's `apiGroup` and `kind`. If one exists, it
evaluates each rule from top to bottom and stops at the first match. That
rule's summary template is rendered and stored as an Activity record.

If no policy matches the event's kind, or the policy exists but no rule
matches, no Activity is created — the event is dropped silently.

```
Audit log arrives
  -> Find policy for (apiGroup, kind)
  -> Evaluate auditRules top-to-bottom
  -> First match wins -> render summary -> publish Activity
  -> No match -> drop silently
```

The same logic applies to control plane events from controllers, using the
`eventRules` list instead.

## Policy anatomy

```yaml
apiVersion: activity.miloapis.com/v1alpha1
kind: ActivityPolicy
metadata:
  name: networking-httpproxy        # Unique name, applies to the whole control plane
  labels:
    meta.datumapis.com/service: network-services
    meta.datumapis.com/team: delivery
spec:
  resource:
    apiGroup: networking.datumapis.com
    kind: HTTPProxy

  auditRules:
    - name: create
      match: "audit.verb == 'create'"
      summary: "{{ actor }} created {{ link('HTTP proxy ' + audit.objectRef.name, audit.responseObject) }}"

    - name: delete
      match: "audit.verb == 'delete'"
      summary: "{{ actor }} deleted HTTP proxy {{ audit.objectRef.name }}"

    - name: update
      match: "audit.verb in ['update', 'patch']"
      summary: "{{ actor }} updated {{ link('HTTP proxy ' + audit.objectRef.name, audit.objectRef) }}"

    - name: fallback
      match: "true"
      summary: "{{ actor }} modified HTTP proxy {{ audit.objectRef.name }}"

  eventRules:
    - name: status-programmed
      match: "event.reason == 'Programmed'"
      summary: "{{ link('HTTP proxy ' + event.regarding.name, event.regarding) }} is now programmed"

    - name: failed
      match: "event.reason.startsWith('Failed')"
      summary: "{{ link('HTTP proxy ' + event.regarding.name, event.regarding) }} failed: {{ event.note }}"
```

`spec.resource` identifies which Kubernetes kind this policy covers.
`auditRules` match against API operations (create, update, delete). `eventRules`
match against events emitted by controllers. A policy can have one, the other,
or both. A policy with neither is valid but produces no activities.

Each rule needs a unique `name` within its list. The name is used to merge
rules correctly when you update the policy with `kubectl apply` — two rules
with the same name will silently collide. Choose names that describe the rule's
intent: `create`, `delete`, `status`, `fallback`.

## Policy ownership and alerts

Set `meta.datumapis.com/service` and `meta.datumapis.com/team` to the service
and team responsible for maintaining the policy. For example, NSO policies use
`network-services` and `delivery`. These are policy owners, independent of the
service running the activity processor.

The processor exports these labels as `policy_service` and `policy_team` on
`activity_processor_policy_info` for active policies. The policy failure alerts
join this metadata by cluster, processor namespace, and policy name, then set
alert `service` and `team` from the policy. The separate metric labels avoid
collisions with the Kubernetes service label added during scraping.

Ownership updates apply to existing retry failures after the policy cache and
metrics refresh. Deleted or inactive policies stop exporting ownership. Failures
without ownership telemetry still alert: their service is unset and their team
falls back to `sre`. Queue publishing failures have no policy identity and remain
processor infrastructure alerts. Configure Alertmanager routes for your service
and team values; resource labels alone do not create routes.

## CEL match expressions

The `match` field must be a CEL expression that returns `true` or `false`.
Rules are evaluated top to bottom; the first rule where `match` is true is
used.

### Common match patterns

To catch only create operations:

```cel
audit.verb == 'create'
```

To catch updates and patches together:

```cel
audit.verb in ['update', 'patch']
```

To match a specific subresource write (useful when controllers update `/status`
or `/scale`):

```cel
audit.verb in ['update', 'patch'] && audit.objectRef.subresource == 'status'
```

To match only the main resource and ignore subresource writes:

```cel
audit.verb in ['update', 'patch'] && !has(audit.objectRef.subresource)
```

To surface failed requests so you can write a distinct summary for errors:

```cel
audit.verb == 'create' && audit.responseStatus.code >= 400
```

To match a specific controller event reason:

```cel
event.reason == 'Programmed'
```

To match event reasons by prefix (controllers often prefix failures with
`Failed`):

```cel
event.reason.startsWith('Failed')
```

To match only warning events:

```cel
event.type == 'Warning'
```

To write a fallback rule that catches everything not already matched:

```cel
true
```

### Audit rule variables

You don't need to memorize these — start with the examples above and refer
back here when you need a specific field.

**Who did it**

| Variable | Type | Description |
|----------|------|-------------|
| `actor` | string | Convenience shorthand for `audit.user.username` |
| `actorRef` | map | Actor reference with `type` and `name` keys, for use with `link()` |
| `audit.user.username` | string | Username (e.g., `alice@example.com`, `system:serviceaccount:default:my-sa`) |

**What was affected**

| Variable | Type | Description |
|----------|------|-------------|
| `audit.objectRef.name` | string | Resource name |
| `audit.objectRef.namespace` | string | Resource namespace (empty for cluster-scoped resources) |
| `audit.objectRef.subresource` | string | Subresource, if any (e.g., `status`, `scale`) |
| `kind` | string | Convenience: extracted from `audit.objectRef.resource` (plural resource name, e.g., `httpproxies`). Useful in match expressions but usually too technical for summary text — prefer string literals like `'HTTP proxy '` in summaries. |

**What happened**

| Variable | Type | Description |
|----------|------|-------------|
| `audit.verb` | string | The API verb: `create`, `update`, `patch`, `delete`, `get`, `list`, `watch` |
| `audit.responseStatus.code` | number | HTTP status code |
| `audit.responseObject` | map | The resource as it exists after the request |

For the full list of available fields, see the [API reference](../api.md).

### Event rule variables

You don't need to memorize these — start with the examples above and refer
back here when you need a specific field.

**Who did it**

| Variable | Type | Description |
|----------|------|-------------|
| `actor` | string | Resolved from `event.reportingController`, falling back to `event.source.component` |
| `actorRef` | map | Actor reference with `type: "controller"` and the controller name |

**What was affected**

| Variable | Type | Description |
|----------|------|-------------|
| `event.regarding.name` | string | Object name |
| `event.regarding.namespace` | string | Object namespace |

**What happened**

| Variable | Type | Description |
|----------|------|-------------|
| `event.reason` | string | Short camelCase reason (e.g., `Programmed`, `FailedScheduling`) |
| `event.type` | string | `Normal` or `Warning` |
| `event.note` | string | Human-readable message |

For the full list of available fields, see the [API reference](../api.md).

## Summary templates

Summaries are the text your users actually see in the activity feed. The
`summary` field is a string that may contain one or more `{{ expression }}`
blocks. Each block must be a CEL expression that returns a string. The text
between blocks is included as-is.

### Available functions

**`link(displayText, resourceRef)`** — Creates a clickable reference in the
portal. `displayText` is a string; `resourceRef` is any map that contains
enough fields for the portal to construct a URL (typically `audit.responseObject`,
`audit.objectRef`, or `event.regarding`).

```
{{ link('HTTP proxy ' + audit.objectRef.name, audit.responseObject) }}
```

The portal renders this as a hyperlink with the display text. If `resourceRef`
lacks the fields needed to build a URL, the text is shown without a link.

### Rendering actor

The `actor` variable holds the raw username from the audit log, which is often
an email address or a `system:` prefixed name. For user-facing summaries you
usually want to render it as a link for human actors and as "System" for service
accounts:

```
{{ actor.startsWith('system:') ? 'System' : link(actor, actorRef) }}
```

This pattern appears throughout the built-in example policies and is the
recommended default.

### Referencing resource names and fields

```
{{ audit.objectRef.name }}
{{ audit.objectRef.namespace }}
{{ event.regarding.name }}
{{ event.note }}
```

String concatenation uses `+`:

```
{{ 'HTTP proxy ' + audit.objectRef.name }}
```

### Multiple template blocks in one summary

You can mix template blocks and static text freely:

```
{{ actor }} updated {{ link('HTTP proxy ' + audit.objectRef.name, audit.objectRef) }} in {{ audit.objectRef.namespace }}
```

## Common patterns

### CRUD operations for any resource

This structure covers the most common cases. Add a rule for each verb and
a `true` fallback to catch anything else:

```yaml
auditRules:
  - name: create
    match: "audit.verb == 'create'"
    summary: "{{ actor.startsWith('system:') ? 'System' : link(actor, actorRef) }} created {{ link('network ' + audit.objectRef.name, audit.responseObject) }}"

  - name: delete
    match: "audit.verb == 'delete'"
    summary: "{{ actor.startsWith('system:') ? 'System' : link(actor, actorRef) }} deleted network {{ audit.objectRef.name }}"

  - name: update
    match: "audit.verb in ['update', 'patch']"
    summary: "{{ actor.startsWith('system:') ? 'System' : link(actor, actorRef) }} updated {{ link('network ' + audit.objectRef.name, audit.objectRef) }}"

  - name: fallback
    match: "true"
    summary: "{{ actor.startsWith('system:') ? 'System' : link(actor, actorRef) }} modified network {{ audit.objectRef.name }}"
```

### Subresource operations

Controllers frequently write to `/status` and `/scale` subresources. Handling
these separately prevents noisy "updated" entries from status reconciliation:

```yaml
auditRules:
  - name: scale
    match: "audit.verb in ['update', 'patch'] && audit.objectRef.subresource == 'scale'"
    summary: "{{ actor.startsWith('system:') ? 'System' : link(actor, actorRef) }} scaled {{ link('deployment ' + audit.objectRef.name, audit.objectRef) }}"

  - name: status
    match: "audit.verb in ['update', 'patch'] && audit.objectRef.subresource == 'status'"
    summary: "{{ link('deployment ' + audit.objectRef.name, audit.objectRef) }} status changed"

  - name: update
    match: "audit.verb in ['update', 'patch']"
    summary: "{{ actor.startsWith('system:') ? 'System' : link(actor, actorRef) }} updated {{ link('deployment ' + audit.objectRef.name, audit.objectRef) }}"
```

The more-specific rules must appear before the general `update` rule because
evaluation stops at the first match.

### Controller event rules

Events emitted by controllers use `eventRules`. Match on `event.reason`, which
controllers set as a short camelCase string:

```yaml
eventRules:
  - name: status-programmed
    match: "event.reason == 'Programmed'"
    summary: "{{ link('network ' + event.regarding.name, event.regarding) }} is programmed"

  - name: warning-event
    match: "event.type == 'Warning'"
    summary: "{{ link('network ' + event.regarding.name, event.regarding) }} warning: {{ event.note }}"

  - name: fallback
    match: "true"
    summary: "{{ link('network ' + event.regarding.name, event.regarding) }}: {{ event.reason }}"
```

### Combining audit and event rules

A single policy can handle both audit logs (who changed the resource) and
events (what the controller observed). They are evaluated independently — an
incoming audit log only matches `auditRules`, an incoming control plane event only
matches `eventRules`:

```yaml
apiVersion: activity.miloapis.com/v1alpha1
kind: ActivityPolicy
metadata:
  name: networking-network
spec:
  resource:
    apiGroup: networking.datumapis.com
    kind: Network

  auditRules:
    - name: create
      match: "audit.verb == 'create'"
      summary: "{{ actor.startsWith('system:') ? 'System' : link(actor, actorRef) }} created {{ link('network ' + audit.objectRef.name, audit.responseObject) }}"
    - name: delete
      match: "audit.verb == 'delete'"
      summary: "{{ actor.startsWith('system:') ? 'System' : link(actor, actorRef) }} deleted network {{ audit.objectRef.name }}"
    - name: update
      match: "audit.verb in ['update', 'patch'] && !has(audit.objectRef.subresource)"
      summary: "{{ actor.startsWith('system:') ? 'System' : link(actor, actorRef) }} updated {{ link('network ' + audit.objectRef.name, audit.objectRef) }}"

  eventRules:
    - name: status-ready
      match: "event.reason == 'Ready'"
      summary: "{{ link('network ' + event.regarding.name, event.regarding) }} is ready"
    - name: warning-event
      match: "event.type == 'Warning'"
      summary: "{{ link('network ' + event.regarding.name, event.regarding) }} warning: {{ event.note }}"
```

### Core Kubernetes resources

Core API group resources (Pod, Service, ConfigMap, etc.) use an empty string for
`apiGroup`:

```yaml
spec:
  resource:
    apiGroup: ""
    kind: Pod
```

## Testing with PolicyPreview

Before applying a policy to the control plane, use PolicyPreview to verify that
your CEL expressions compile, your rules match the right inputs, and your
summaries render correctly. PolicyPreview is an ephemeral resource — you create
it and the API server evaluates it immediately, returning results in the
response. Nothing is stored.

### Manual inputs

Supply sample audit logs or events that represent real traffic for your
resource. Copy these from actual audit log data or construct them to match the
cases your rules need to handle:

```yaml
apiVersion: activity.miloapis.com/v1alpha1
kind: PolicyPreview
metadata:
  name: test-httpproxy
spec:
  policy:
    resource:
      apiGroup: networking.datumapis.com
      kind: HTTPProxy
    auditRules:
      - name: create
        match: "audit.verb == 'create'"
        summary: "{{ actor }} created {{ link('HTTP proxy ' + audit.objectRef.name, audit.responseObject) }}"
      - name: delete
        match: "audit.verb == 'delete'"
        summary: "{{ actor }} deleted HTTP proxy {{ audit.objectRef.name }}"
  inputs:
    - type: audit
      audit:
        verb: create
        objectRef:
          apiGroup: networking.datumapis.com
          resource: httpproxies
          name: api-gateway
          namespace: default
        user:
          username: alice@example.com
        responseObject:
          apiVersion: networking.datumapis.com/v1alpha1
          kind: HTTPProxy
          metadata:
            name: api-gateway
            namespace: default
    - type: audit
      audit:
        verb: delete
        objectRef:
          apiGroup: networking.datumapis.com
          resource: httpproxies
          name: old-proxy
          namespace: default
        user:
          username: bob@example.com
```

Apply it and read back the results:

```bash
kubectl create -f preview.yaml -o yaml
```

The response `status.results` shows which rule matched each input and whether
evaluation succeeded. `status.activities` contains the rendered Activity objects
so you can see exactly what would appear in the activity feed.

### Auto-fetch inputs

Instead of constructing sample inputs by hand, you can ask the API server to
fetch real samples from the last N hours of activity data. This is useful when
you want to verify a policy against actual traffic for your resource type.

`autoFetch` is mutually exclusive with manual `inputs` — provide one or the
other, not both.

```yaml
apiVersion: activity.miloapis.com/v1alpha1
kind: PolicyPreview
metadata:
  name: test-httpproxy-autofetch
spec:
  policy:
    resource:
      apiGroup: networking.datumapis.com
      kind: HTTPProxy
    auditRules:
      - name: create
        match: "audit.verb == 'create'"
        summary: "{{ actor }} created {{ link('HTTP proxy ' + audit.objectRef.name, audit.responseObject) }}"
  autoFetch:
    limit: 10        # 1-50, default 10
    timeRange: "24h"  # default "24h", supports "1h", "7d", "30d"
    sources: "both"   # "audit", "events", or "both" (default)
```

When `autoFetch` is used, the response includes a `status.fetchedInputs` field
containing the actual samples that were tested. Inspect this field to understand
exactly what data the policy was evaluated against.

### Reading preview results

The response status looks like this:

```yaml
status:
  results:
    - inputIndex: 0
      matched: true
      matchedRuleIndex: 0
      matchedRuleName: create
      matchedRuleType: audit
    - inputIndex: 1
      matched: true
      matchedRuleIndex: 1
      matchedRuleName: delete
      matchedRuleType: audit
  activities:
    - spec:
        summary: "alice@example.com created HTTP proxy api-gateway"
        ...
    - spec:
        summary: "bob@example.com deleted HTTP proxy old-proxy"
        ...
```

If a rule fails to compile or a summary expression errors, the result for that
input will include an `error` field explaining what went wrong. Fix the CEL
expression and re-run the preview.

## Deploying a policy

ActivityPolicy applies to the whole control plane, not a specific namespace.
Apply it like any other Kubernetes resource:

```bash
kubectl apply -f my-policy.yaml
```

The activity-controller-manager validates and compiles all CEL expressions when
the policy is created or updated. Check the policy status to confirm it was
accepted:

```bash
kubectl get activitypolicy my-policy -o yaml
```

Look for a `Ready` condition in `status.conditions`:

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: PolicyReady
      message: All rules compiled successfully
```

If compilation fails, the `Ready` condition will be `False` with a message
identifying which expression is invalid. The processor will not use a policy
that has not reached `Ready: True`.

## Things to watch out for

**Each rule must have a unique `name` within its list (`auditRules` or `eventRules`).** Names are how rules merge correctly when you update the policy
with `kubectl apply`. If two rules share a name, one will silently overwrite the
other. Choose names that describe the rule's intent: `create`, `delete`,
`status`, `fallback`.

**Rules are evaluated in order; order matters.** Put specific rules before
general ones. A `true` fallback must be last or it will shadow everything below
it.

**The `match` expression must return a boolean.** An expression like
`audit.verb` (which returns a string) causes a compilation error. Always write
a comparison: `audit.verb == 'create'`.

**Summary expressions must return a string.** Each `{{ }}` block must evaluate
to a string. `link()` returns a string. Numeric fields like
`audit.responseStatus.code` must be converted: `string(audit.responseStatus.code)`.

**`audit.responseObject` is empty for deletes.** The API server does not return
a response body for `delete` operations. Use `audit.objectRef` instead when
linking to deleted resources.

**`audit.requestObject` is empty for status subresource writes.** Controllers
typically patch `/status` with a partial object. Do not rely on
`audit.requestObject` fields for status rules.

**Use `has()` to safely check optional fields.** Some audit log fields may not
be present for all requests. The CEL `has()` macro tests for field presence
before accessing it:

```cel
has(audit.objectRef.subresource) && audit.objectRef.subresource == 'status'
```

Alternatively, use `!has(audit.objectRef.subresource)` to match only requests
to the main resource:

```cel
audit.verb in ['update', 'patch'] && !has(audit.objectRef.subresource)
```

**If a summary fails to evaluate at runtime, the failed message is acknowledged
and will not be retried automatically.** Use PolicyPreview to catch errors
before they reach production. Persistent failures can be investigated with the
DLQ runbooks.

**One policy per resource kind.** The processor matches on `(apiGroup, kind)`.
If you create two policies for the same kind, the behavior is undefined. Name
your policies with a consistent convention such as `{apigroup-slug}-{kind-lowercase}`.

For the complete field reference, see the [API reference](../api.md).

## Related documentation

- [API Reference](../api.md) — Complete ActivityPolicy and PolicyPreview field specs
- [Activity Pipeline Architecture](../architecture/activity-pipeline.md) — How the processor evaluates policies
- [DLQ Runbooks](../runbooks/dlq/) — Troubleshooting evaluation failures
