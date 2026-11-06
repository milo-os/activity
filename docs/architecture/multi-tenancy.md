# Multi-Tenancy

The activity system isolates data by tenant, enabling users to query their own
data while allowing platform operators to query across tenants.

## Scope Hierarchy

The system supports four scope levels:

| Scope | Description | Query Behavior |
|-------|-------------|----------------|
| Platform | Platform operators | Queries all events across all tenants |
| Organization | Organization members | Queries events within a specific organization |
| Project | Project members | Queries events within a specific project |
| User | Individual users | Queries events performed by a specific user |

User scope differs from organization and project scope: it returns events
performed **by** the user across all organizations and projects, not events
**within** a user's namespace. This enables users to view their own activity
across the platform.

For activities, user scope also includes **platform events about the user's own
account**: event-origin activities whose resource is the user's
`iam.miloapis.com` `User`. The platform acts on a user's account on their behalf
(signup creates the `User`, passkeys are managed by the identity provider), so
those activities have a service as their actor and an actor-only filter would
hide them from the user. A user's actor UID and their `User` name are the same
identifier, the identity provider's subject.

Audit-origin activities about the user's `User` made by someone else, such as a
staff member editing the account, are not included.

> [!NOTE]
>
> Scopes are not hierarchically inclusive. Organization-scoped queries return
> only events tagged with that organization—they do not include events from
> projects within the organization. To view project activity, query with project
> scope directly. This behavior may change in a future release.

## Event Tagging

Audit events and Kubernetes events include annotations to indicate their tenant
scope. The control plane API server automatically adds these annotations when
generating audit logs:

| Annotation | Description |
|------------|-------------|
| `platform.miloapis.com/scope.type` | Tenant type (Organization, Project) |
| `platform.miloapis.com/scope.name` | Tenant identifier |

**Example: Audit Event**

```json
{
  "kind": "Event",
  "apiVersion": "audit.k8s.io/v1",
  "auditID": "abc-123",
  "verb": "create",
  "objectRef": {
    "resource": "httpproxies",
    "name": "api-gateway",
    "apiGroup": "networking.datumapis.com"
  },
  "annotations": {
    "platform.miloapis.com/scope.type": "project",
    "platform.miloapis.com/scope.name": "prod-cluster"
  }
}
```

**Example: Kubernetes Event**

```yaml
apiVersion: events.k8s.io/v1
kind: Event
metadata:
  name: api-gateway.abc123
  namespace: default
  annotations:
    platform.miloapis.com/scope.type: project
    platform.miloapis.com/scope.name: prod-cluster
regarding:
  apiVersion: networking.datumapis.com/v1
  kind: HTTPProxy
  name: api-gateway
  namespace: default
reason: Programmed
note: "HTTPProxy is now programmed"
```

The Vector aggregator extracts these annotations into materialized ClickHouse
columns (`scope_type`, `scope_name`) for efficient filtering.

## Scope Resolution

The API server determines query scope from the user's authentication context.
The platform's identity system sets extra fields on the authenticated user:

| Extra Field | Description |
|-------------|-------------|
| `iam.miloapis.com/parent-type` | Parent resource type (Organization, Project, User) |
| `iam.miloapis.com/parent-name` | Parent resource name or user UID |

When no parent resource is specified, the API server defaults to platform scope.

### Query Filtering

The query builder adds appropriate WHERE clauses based on the resolved scope:

| Scope | ClickHouse Filter |
|-------|-------------------|
| Platform | No scope filter (all data) |
| Organization | `scope_type = 'organization' AND scope_name = ?` |
| Project | `scope_type = 'project' AND scope_name = ?` |
| User | `user_uid = ?` (audit logs); `actor_uid = ? OR (origin_type = 'event' AND api_group = 'iam.miloapis.com' AND resource_kind = 'User' AND resource_name = ?)` (activities) |

> [!IMPORTANT]
>
> The platform is responsible for authorizing users before they reach the
> activity API. The activity service trusts the scope provided by the
> authentication system and does not perform additional authorization checks.

## NATS Subject Conventions

NATS subjects encode tenant context to enable filtered subscriptions.

### Audit Events

```
audit.k8s.activity
```

Audit events use a single subject. Tenant filtering occurs at the consumer
level via ClickHouse queries.

### Kubernetes Events

```
events.<tenant_type>.<tenant_name>.<api_group_kind>.<namespace>.<name>
```

Tenant type values:
- `global` - Platform-wide events (use `_` as tenant_name placeholder)
- `organization` - Organization-scoped events
- `project` - Project-scoped events
- `user` - User-scoped events

### Activities

```
activities.<tenant_type>.<tenant_name>.<api_group>.<source>.<kind>.<namespace>.<name>
```

The API group enables service providers to subscribe to all activities for
their service. Dots in API groups are replaced with underscores.

### Wildcard Subscriptions

| Pattern | Use Case |
|---------|----------|
| `events.project.prod-cluster.>` | All events in prod-cluster project |
| `events.organization.acme-corp.>` | All events for acme-corp organization |
| `activities.*.*.networking_datumapis_com.>` | All networking activities across tenants |
| `activities.project.prod.>` | All activities in prod project |

## Related Documentation

- [Architecture Overview](./README.md)
- [Audit Pipeline](./audit-pipeline.md)
- [Event Pipeline](./event-pipeline.md)
- [Activity Pipeline](./activity-pipeline.md)
