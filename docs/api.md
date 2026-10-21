# API Reference

## Packages
- [activity.miloapis.com/v1alpha1](#activitymiloapiscomv1alpha1)


## activity.miloapis.com/v1alpha1

Package v1alpha1 contains API Schema definitions for the activity v1alpha1 API group















#### Activity



Activity is a human-readable summary of something that happened in your cluster.
Think of it as the "what changed and who did it" record that powers activity feeds,
audit trails, and change history views.


Activities are created automatically from audit logs and Kubernetes events based on
your ActivityPolicy rules. They're read-only - you query them, not create them.


# Accessing Activities


There are three ways to get activity data, depending on what you need:


| What you need | API to use | Notes |
| --- | --- | --- |
| Live feed | GET /activities?watch=true | Streams new activities as they happen. List only returns the last hour. |
| Search history | POST /activityqueries | Query any time range with filters, search, and pagination. |
| Filter options | POST /activityfacetqueries | Get values for dropdowns (e.g., "which actors have activities?"). |


# Quick Examples


Watch for new activities:


	kubectl get activities --watch


List recent human-initiated changes:


	kubectl get activities --field-selector spec.changeSource=human


For historical queries or advanced filtering, use ActivityQuery instead.



_Appears in:_
- [ActivityList](#activitylist)
- [ActivityQueryStatus](#activityquerystatus)
- [PolicyPreviewStatus](#policypreviewstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ActivitySpec](#activityspec)_ |  |  |  |


#### ActivityActor



ActivityActor identifies who performed an action.



_Appears in:_
- [ActivitySpec](#activityspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _string_ | Type indicates the actor category.<br />Values: "user", "serviceaccount", "controller" |  |  |
| `name` _string_ | Name is the display name for the actor.<br />For users, this is typically the email address.<br />For service accounts, this is the full name (e.g., "system:serviceaccount:default:my-sa").<br />For controllers, this is the controller name. |  |  |
| `uid` _string_ | UID is the unique identifier for the actor.<br />Stable across username changes. |  |  |
| `email` _string_ | Email is the actor's email address.<br />Only populated for user actors when available. |  |  |


#### ActivityChange



ActivityChange represents a field-level change in an update/patch operation.



_Appears in:_
- [ActivitySpec](#activityspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `field` _string_ | Field is the path to the changed field (e.g., "spec.virtualhost.fqdn"). |  |  |
| `old` _string_ | Old is the previous value. May be empty for new fields. |  |  |
| `new` _string_ | New is the new value. May be empty for deleted fields. |  |  |




#### ActivityFacetQuerySpec



ActivityFacetQuerySpec defines what you want facet data for.



_Appears in:_
- [ActivityFacetQuery](#activityfacetquery)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `timeRange` _[FacetTimeRange](#facettimerange)_ | TimeRange sets how far back to look. Defaults to the last 7 days if not set.<br />Use relative times like "now-7d" or absolute timestamps. |  |  |
| `filter` _string_ | Filter lets you narrow down which activities to include before computing facets.<br />Uses CEL (Common Expression Language) syntax.<br /><br />This is useful when you want facet values for a specific subset - for example,<br />"show me actors, but only for human-initiated changes."<br /><br />Fields you can filter on:<br />  spec.changeSource       - "human" or "system"<br />  spec.actor.name         - who did it (e.g., "alice@example.com")<br />  spec.actor.type         - user, serviceaccount, or controller<br />  spec.resource.kind      - what type of resource (Deployment, Pod, etc.)<br />  spec.resource.namespace - which namespace<br />  spec.resource.name      - resource name<br />  spec.resource.apiGroup  - API group (empty string for core resources)<br /><br />Example filters:<br />  "spec.changeSource == 'human'"              - Only human actions<br />  "spec.resource.kind == 'Deployment'"        - Only Deployment changes<br />  "!spec.actor.name.startsWith('system:')"    - Exclude system accounts |  |  |
| `facets` _[FacetSpec](#facetspec) array_ | Facets specifies which fields to get distinct values for.<br />Each facet returns the top N values with counts. |  |  |


#### ActivityFacetQueryStatus



ActivityFacetQueryStatus contains the facet results.



_Appears in:_
- [ActivityFacetQuery](#activityfacetquery)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `facets` _[FacetResult](#facetresult) array_ | Facets contains the results for each requested facet. |  |  |


#### ActivityLink



ActivityLink represents a clickable reference in an activity summary.



_Appears in:_
- [ActivitySpec](#activityspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `marker` _string_ | Marker is the text substring in the summary that should be linked.<br />The portal scans the summary for this marker and makes it clickable.<br /><br />Example: "HTTP proxy api-gateway" |  |  |
| `resource` _[ActivityResource](#activityresource)_ | Resource identifies what the marker links to. |  |  |




#### ActivityOrigin



ActivityOrigin identifies the source record for an activity.



_Appears in:_
- [ActivitySpec](#activityspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _string_ | Type indicates the source type.<br />Values: "audit" (from audit logs), "event" (from Kubernetes events) |  |  |
| `id` _string_ | ID is the correlation ID to the source record.<br />For audit: the auditID from the audit log entry.<br />For event: the metadata.uid of the Kubernetes Event. |  |  |


#### ActivityPolicy



ActivityPolicy defines translation rules for a specific resource type. Service providers
create one ActivityPolicy per resource kind to customize activity descriptions without
modifying the Activity Processor.


Example:


	apiVersion: activity.miloapis.com/v1alpha1
	kind: ActivityPolicy
	metadata:
	  name: networking-httpproxy
	spec:
	  resource:
	    apiGroup: networking.datumapis.com
	    kind: HTTPProxy
	  auditRules:
	    - match: "audit.verb == 'create'"
	      summary: "{{ actor }} created {{ link(kind + ' ' + audit.objectRef.name, audit.objectRef) }}"
	  eventRules:
	    - match: "event.reason == 'Programmed'"
	      summary: "{{ link(kind + ' ' + event.regarding.name, event.regarding) }} is now programmed"



_Appears in:_
- [ActivityPolicyList](#activitypolicylist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ActivityPolicySpec](#activitypolicyspec)_ |  |  |  |
| `status` _[ActivityPolicyStatus](#activitypolicystatus)_ |  |  |  |




#### ActivityPolicyResource



ActivityPolicyResource identifies the target Kubernetes resource for a policy.



_Appears in:_
- [ActivityPolicySpec](#activitypolicyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiGroup` _string_ | APIGroup is the API group of the target resource (e.g., "networking.datumapis.com").<br />Use an empty string for core API group resources. |  |  |
| `kind` _string_ | Kind is the kind of the target resource (e.g., "HTTPProxy", "Network"). |  |  |


#### ActivityPolicyRule



ActivityPolicyRule defines a single translation rule that matches input events
and generates human-readable activity summaries.



_Appears in:_
- [ActivityPolicySpec](#activitypolicyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is a unique identifier for this rule within the policy.<br />Used for strategic merge patching and error reporting. |  |  |
| `description` _string_ | Description is an optional human-readable description of what this rule does. |  |  |
| `match` _string_ | Match is a CEL expression that determines if this rule applies to the input.<br />For audit rules, use the `audit` variable (e.g., "audit.verb == 'create'", "audit.objectRef.namespace == 'default'").<br />For event rules, use the `event` variable (e.g., "event.reason == 'Programmed'").<br /><br />Examples:<br />  "audit.verb == 'create'"<br />  "audit.verb in ['update', 'patch']"<br />  "event.reason.startsWith('Failed')"<br />  "true"  (fallback rule that always matches) |  |  |
| `summary` _string_ | Summary is a CEL template for generating the activity summary.<br />Use \{\{ \}\} delimiters to embed CEL expressions within strings.<br /><br />Available variables:<br />  - For audit rules: audit (map), actor, actorRef, kind<br />    Access audit fields via: audit.verb, audit.objectRef, audit.user, audit.responseStatus, audit.responseObject<br />  - For event rules: event, actor, actorRef<br /><br />Available functions:<br />  - link(displayText, resourceRef): Creates a clickable reference<br /><br />Examples:<br />  "\{\{ actor \}\} created \{\{ link(kind + ' ' + audit.objectRef.name, audit.objectRef) \}\}"<br />  "\{\{ link(kind + ' ' + event.regarding.name, event.regarding) \}\} is now programmed" |  |  |


#### ActivityPolicySpec



ActivityPolicySpec defines the translation rules for a resource type.



_Appears in:_
- [ActivityPolicy](#activitypolicy)
- [PolicyPreviewSpec](#policypreviewspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `resource` _[ActivityPolicyResource](#activitypolicyresource)_ | Resource identifies the Kubernetes resource this policy applies to.<br />One ActivityPolicy should exist per resource kind. |  |  |
| `auditRules` _[ActivityPolicyRule](#activitypolicyrule) array_ | AuditRules define how to translate audit log entries into activity summaries.<br />Rules are evaluated in order; the first matching rule wins.<br />Available variables: audit (map with verb, objectRef, user, responseStatus,<br />  responseObject, requestObject), actor, actorRef, kind |  |  |
| `eventRules` _[ActivityPolicyRule](#activitypolicyrule) array_ | EventRules define how to translate Kubernetes events into activity summaries.<br />Rules are evaluated in order; the first matching rule wins.<br />The `event` variable contains the full Kubernetes Event structure.<br />Convenience variables available: actor |  |  |


#### ActivityPolicyStatus



ActivityPolicyStatus represents the current state of an ActivityPolicy.



_Appears in:_
- [ActivityPolicy](#activitypolicy)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#condition-v1-meta) array_ | Conditions represent the current state of the policy.<br />The "Ready" condition indicates whether all rules compile successfully. |  |  |
| `observedGeneration` _integer_ | ObservedGeneration is the generation last processed by the controller. |  |  |




#### ActivityQuerySpec



ActivityQuerySpec defines the search parameters for activities.


Required: startTime and endTime define your search window.
Optional: filter (CEL expression), search, limit, continue.


CEL is the primary filtering mechanism. All dedicated filter fields have been
removed in favor of the expressive filter field.


Available CEL Fields:


	spec.changeSource      - "human" or "system"
	spec.actor.name        - who performed the action
	spec.actor.type        - "user", "serviceaccount", "controller"
	spec.actor.uid         - actor's unique identifier
	spec.resource.apiGroup - resource API group (empty for core)
	spec.resource.kind     - resource kind (Deployment, Pod, etc.)
	spec.resource.name     - resource name
	spec.resource.namespace - resource namespace
	spec.resource.uid      - resource UID
	spec.summary           - activity summary text
	spec.origin.type       - "audit" or "event"
	spec.source.planeType  - plane the record originated from (management, edge)
	spec.source.cluster    - cluster the record originated from
	spec.source.region     - region the record originated from
	spec.source.city       - city the record originated from
	spec.relatedUIDs       - UIDs of the resource, its related resources, and
	                         linked resources (list; use '<uid>' in spec.relatedUIDs)
	metadata.namespace     - activity namespace


CEL Filter Examples:


	"spec.changeSource == 'human'"
	"spec.resource.kind == 'Deployment'"
	"spec.actor.name.contains('admin')"
	"spec.resource.kind in ['Deployment', 'StatefulSet']"
	"spec.resource.apiGroup == 'networking.datumapis.com'"
	"spec.actor.uid == 'abc123'"
	"'abc123' in spec.relatedUIDs && spec.source.region == 'us-east-1'"



_Appears in:_
- [ActivityQuery](#activityquery)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `startTime` _string_ | StartTime is the beginning of your search window (inclusive).<br /><br />Format Options:<br />- Relative: "now-7d", "now-2h", "now-30m" (units: s, m, h, d, w)<br />- Absolute: "2024-01-01T00:00:00Z" (RFC3339 with timezone) |  |  |
| `endTime` _string_ | EndTime is the end of your search window (exclusive).<br /><br />Uses the same formats as StartTime. Commonly "now" for current moment.<br />Must be greater than StartTime. |  |  |
| `filter` _string_ | Filter narrows results using CEL (Common Expression Language).<br /><br />This is the primary filtering mechanism. See the ActivityQuerySpec godoc<br />for available fields and examples.<br /><br />Operators: ==, !=, &&, \|\|, !, in<br />String Functions: startsWith(), endsWith(), contains() |  |  |
| `search` _string_ | Search performs full-text search on activity summaries.<br /><br />Example: "created deployment" matches activities with those words in the summary. |  |  |
| `limit` _integer_ | Limit sets the maximum number of results per page.<br />Default: 100, Maximum: 1000. |  |  |
| `continue` _string_ | Continue is the pagination cursor for fetching additional pages.<br /><br />Leave empty for the first page. Copy status.continue here to get the next page.<br />Keep all other parameters identical across paginated requests. |  |  |


#### ActivityQueryStatus



ActivityQueryStatus contains the query results and pagination state.



_Appears in:_
- [ActivityQuery](#activityquery)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `results` _[Activity](#activity) array_ | Results contains matching activities, sorted newest-first. |  |  |
| `continue` _string_ | Continue is the pagination cursor.<br />Non-empty means more results are available. |  |  |
| `effectiveStartTime` _string_ | EffectiveStartTime is the actual start time used (RFC3339 format).<br />Shows the resolved timestamp when relative times are used. |  |  |
| `effectiveEndTime` _string_ | EffectiveEndTime is the actual end time used (RFC3339 format).<br />Shows the resolved timestamp when relative times are used. |  |  |


#### ActivityResource



ActivityResource identifies the Kubernetes resource affected by an activity.



_Appears in:_
- [ActivityLink](#activitylink)
- [ActivitySpec](#activityspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiGroup` _string_ | APIGroup is the API group of the resource.<br />Empty string for core API group. |  |  |
| `apiVersion` _string_ | APIVersion is the API version of the resource. |  |  |
| `kind` _string_ | Kind is the kind of the resource. |  |  |
| `name` _string_ | Name is the name of the resource. |  |  |
| `namespace` _string_ | Namespace is the namespace of the resource.<br />Empty for cluster-scoped resources. |  |  |
| `uid` _string_ | UID is the unique identifier of the resource. |  |  |


#### ActivitySource



ActivitySource describes where the underlying event or activity originated.



_Appears in:_
- [ActivitySpec](#activityspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `planeType` _string_ | PlaneType indicates which plane the event or activity originated from.<br />Example values: "management", "edge". |  |  |
| `cluster` _string_ | Cluster is the name of the cluster the event or activity originated from. |  |  |
| `region` _string_ | Region is the region the event or activity originated from. |  |  |
| `city` _string_ | City is the city the event or activity originated from. |  |  |


#### ActivitySpec



ActivitySpec contains the translated activity details.



_Appears in:_
- [Activity](#activity)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `summary` _string_ | Summary is the human-readable description of what happened.<br />Generated from ActivityPolicy templates.<br /><br />Example: "alice created HTTP proxy api-gateway" |  |  |
| `changeSource` _string_ | ChangeSource indicates who initiated the change.<br />Used to filter human actions from system reconciliation noise.<br /><br />Values:<br />  - "human": User action via kubectl, API, or UI<br />  - "system": Controller reconciliation, operator actions, scheduled jobs |  |  |
| `actor` _[ActivityActor](#activityactor)_ | Actor identifies who performed the action. |  |  |
| `resource` _[ActivityResource](#activityresource)_ | Resource identifies the Kubernetes resource that was affected. |  |  |
| `links` _[ActivityLink](#activitylink) array_ | Links contains clickable references found in the summary.<br />The portal uses these to make resource names in the summary clickable. |  |  |
| `related` _[ActivityResource](#activityresource) array_ | Related lists other resources the source record references, such as an<br />event's related object (e.g., the Workload an Instance belongs to). |  |  |
| `tenant` _[ActivityTenant](#activitytenant)_ | Tenant identifies the scope for multi-tenant isolation. |  |  |
| `changes` _[ActivityChange](#activitychange) array_ | Changes contains field-level changes for update/patch operations.<br />Shows old and new values for modified fields.<br /><br />NOTE: This field may be empty in the initial implementation.<br />Populating old values requires resource history lookups. |  |  |
| `origin` _[ActivityOrigin](#activityorigin)_ | Origin identifies the source record for correlation. |  |  |
| `source` _[ActivitySource](#activitysource)_ | Source describes where the underlying event or activity originated:<br />plane type, cluster, region, and city.<br /><br />Unset means this information is unknown or not yet populated. |  |  |


#### ActivityTenant



ActivityTenant identifies the scope for multi-tenant isolation.



_Appears in:_
- [ActivitySpec](#activityspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _string_ | Type is the scope level.<br />Values: "global", "organization", "project", "user" |  |  |
| `name` _string_ | Name is the tenant identifier within the scope type. |  |  |




#### AuditLogFacetsQuerySpec



AuditLogFacetsQuerySpec defines which facets to retrieve from audit logs.



_Appears in:_
- [AuditLogFacetsQuery](#auditlogfacetsquery)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `timeRange` _[FacetTimeRange](#facettimerange)_ | TimeRange limits the time window for facet aggregation.<br />If not specified, defaults to the last 7 days. |  |  |
| `filter` _string_ | Filter narrows the audit logs before computing facets using CEL.<br />This allows you to get facet values for a subset of audit logs.<br /><br />Available Fields:<br />  verb               - API action: get, list, create, update, patch, delete, watch<br />  user.username      - who made the request (user or service account)<br />  user.uid           - unique user identifier<br />  responseStatus.code - HTTP response code (200, 201, 404, 500, etc.)<br />  objectRef.namespace - target resource namespace<br />  objectRef.resource  - resource type (pods, deployments, secrets, configmaps, etc.)<br />  objectRef.apiGroup  - API group of the resource<br />  objectRef.name     - specific resource name<br /><br />Operators: ==, !=, <, >, <=, >=, &&, \|\|, !, in<br />String Functions: startsWith(), endsWith(), contains()<br /><br />Examples:<br />  "verb in ['create', 'update', 'delete']"        - Facets for write operations only<br />  "!(verb in ['get', 'list', 'watch'])"           - Exclude read-only operations<br />  "!user.username.startsWith('system:')"          - Exclude system users<br />  "objectRef.namespace == 'production'"           - Facets for production namespace |  |  |
| `facets` _[FacetSpec](#facetspec) array_ | Facets specifies which fields to get distinct values for.<br />Each facet returns the top N values with counts.<br /><br />Supported fields:<br />  - verb: API action (get, list, create, update, patch, delete, watch)<br />  - user.username: Actor display names<br />  - user.uid: Unique user identifiers<br />  - responseStatus.code: HTTP response codes<br />  - objectRef.namespace: Namespaces<br />  - objectRef.resource: Resource types<br />  - objectRef.apiGroup: API groups |  |  |


#### AuditLogFacetsQueryStatus



AuditLogFacetsQueryStatus contains the facet results.



_Appears in:_
- [AuditLogFacetsQuery](#auditlogfacetsquery)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `facets` _[FacetResult](#facetresult) array_ | Facets contains the results for each requested facet. |  |  |




#### AuditLogQuerySpec



AuditLogQuerySpec defines the search parameters.


Required: startTime and endTime define your search window.
Optional: filter (narrow results), limit (page size, default 100), continue (pagination).


Performance: Smaller time ranges and specific filters perform better. The maximum time window
is typically 30 days. If your range is too large, you'll get an error with guidance on splitting
your query into smaller chunks.



_Appears in:_
- [AuditLogQuery](#auditlogquery)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `startTime` _string_ | StartTime is the beginning of your search window (inclusive).<br /><br />Format Options:<br />- Relative: "now-30d", "now-2h", "now-30m" (units: s, m, h, d, w)<br />  Use for dashboards and recurring queries - they adjust automatically.<br />- Absolute: "2024-01-01T00:00:00Z" (RFC3339 with timezone)<br />  Use for historical analysis of specific time periods.<br /><br />Examples:<br />  "now-30d"                     → 30 days ago<br />  "2024-06-15T14:30:00-05:00"   → specific time with timezone offset |  |  |
| `endTime` _string_ | EndTime is the end of your search window (exclusive).<br /><br />Uses the same formats as StartTime. Commonly "now" for current moment.<br />Must be greater than StartTime.<br /><br />Examples:<br />  "now"                  → current time<br />  "2024-01-02T00:00:00Z" → specific end point |  |  |
| `filter` _string_ | Filter narrows results using CEL (Common Expression Language). Leave empty to get all events.<br /><br />Available Fields:<br />  verb               - API action: get, list, create, update, patch, delete, watch<br />  auditID            - unique event identifier<br />  requestReceivedTimestamp - when the API server received the request (RFC3339 timestamp)<br />  user.username      - who made the request (user or service account)<br />  user.uid           - unique user identifier (stable across username changes)<br />  responseStatus.code - HTTP response code (200, 201, 404, 500, etc.)<br />  objectRef.namespace - target resource namespace<br />  objectRef.resource  - resource type (pods, deployments, secrets, configmaps, etc.)<br />  objectRef.name     - specific resource name<br /><br />Operators: ==, !=, <, >, <=, >=, &&, \|\|, !, in<br />String Functions: startsWith(), endsWith(), contains()<br /><br />Common Patterns:<br />  "verb == 'delete'"                                    - All deletions<br />  "objectRef.namespace == 'production'"                 - Activity in production namespace<br />  "verb in ['create', 'update', 'delete', 'patch']"     - All write operations<br />  "!(verb in ['get', 'list', 'watch'])"                 - Exclude read-only operations<br />  "responseStatus.code >= 400"                          - Failed requests<br />  "user.username.startsWith('system:serviceaccount:')"  - Service account activity<br />  "!user.username.startsWith('system:')"                - Exclude system users<br />  "user.uid == '550e8400-e29b-41d4-a716-446655440000'"  - Specific user by UID<br />  "objectRef.resource == 'secrets'"                     - Secret access<br />  "verb == 'delete' && objectRef.namespace == 'production'" - Production deletions<br /><br />Note: Use single quotes for strings. Field names are case-sensitive.<br />CEL reference: https://cel.dev |  |  |
| `limit` _integer_ | Limit sets the maximum number of results per page.<br />Default: 100, Maximum: 1000.<br /><br />Use smaller values (10-50) for exploration, larger (500-1000) for data collection.<br />Use continue to fetch additional pages. |  |  |
| `continue` _string_ | Continue is the pagination cursor for fetching additional pages.<br /><br />Leave empty for the first page. If status.continue is non-empty after a query,<br />copy that value here in a new query with identical parameters to get the next page.<br />Repeat until status.continue is empty.<br /><br />Important: Keep all other parameters (startTime, endTime, filter, limit) identical<br />across paginated requests. The cursor is opaque - copy it exactly without modification. |  |  |


#### AuditLogQueryStatus



AuditLogQueryStatus contains the query results and pagination state.



_Appears in:_
- [AuditLogQuery](#auditlogquery)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `results` _Event array_ | Results contains matching audit events, sorted newest-first.<br /><br />Each event follows the Kubernetes audit.Event format with fields like:<br />  verb, user.username, objectRef.\{namespace,resource,name\}, requestReceivedTimestamp,<br />  stageTimestamp, responseStatus.code, requestObject, responseObject<br /><br />Empty results? Try broadening your filter or time range.<br />Full documentation: https://kubernetes.io/docs/reference/config-api/apiserver-audit.v1/ |  |  |
| `continue` _string_ | Continue is the pagination cursor.<br />Non-empty means more results are available - copy this to spec.continue for the next page.<br />Empty means you have all results. |  |  |
| `effectiveStartTime` _string_ | EffectiveStartTime is the actual start time used for this query (RFC3339 format).<br /><br />When you use relative times like "now-7d", this shows the exact timestamp that was<br />calculated. Useful for understanding exactly what time range was queried, especially<br />for auditing, debugging, or recreating queries with absolute timestamps.<br /><br />Example: If you query with startTime="now-7d" at 2025-12-17T12:00:00Z,<br />this will be "2025-12-10T12:00:00Z". |  |  |
| `effectiveEndTime` _string_ | EffectiveEndTime is the actual end time used for this query (RFC3339 format).<br /><br />When you use relative times like "now", this shows the exact timestamp that was<br />calculated. Useful for understanding exactly what time range was queried.<br /><br />Example: If you query with endTime="now" at 2025-12-17T12:00:00Z,<br />this will be "2025-12-17T12:00:00Z". |  |  |


#### AutoFetchSpec



AutoFetchSpec configures automatic sample data retrieval.



_Appears in:_
- [PolicyPreviewSpec](#policypreviewspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `limit` _integer_ | Limit is the maximum number of sample inputs to fetch (default: 10, max: 50).<br />The API fetches up to this many audit logs and/or events. | 10 | Maximum: 50 <br />Minimum: 1 <br /> |
| `timeRange` _string_ | TimeRange specifies how far back to look for samples (default: "24h").<br />Supports relative format: "1h", "24h", "7d", "30d" | 24h |  |
| `sources` _string_ | Sources specifies which data sources to query: "audit", "events", or "both" (default: "both").<br />- "audit": Only fetch audit logs (only tests auditRules)<br />- "events": Only fetch Kubernetes events (only tests eventRules)<br />- "both": Fetch both types (tests all rules) | both | Enum: [audit events both] <br /> |




#### EventFacetQuerySpec



EventFacetQuerySpec defines which facets to retrieve from Kubernetes Events.



_Appears in:_
- [EventFacetQuery](#eventfacetquery)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `timeRange` _[FacetTimeRange](#facettimerange)_ | TimeRange limits the time window for facet aggregation.<br />If not specified, defaults to the last 7 days. |  |  |
| `facets` _[FacetSpec](#facetspec) array_ | Facets specifies which fields to get distinct values for.<br />Each facet returns the top N values with counts.<br /><br />Supported fields:<br />  - regarding.kind: Resource kinds (Pod, Deployment, etc.)<br />  - regarding.namespace: Namespaces of regarding objects<br />  - reason: Event reasons (Scheduled, Pulled, Created, etc.)<br />  - type: Event types (Normal, Warning)<br />  - source.component: Source components (kubelet, scheduler, etc.)<br />  - namespace: Event namespace<br />  - related.kind: Related resource kinds (Node, ConfigMap, etc.)<br />  - related.namespace: Namespaces of related objects |  |  |


#### EventFacetQueryStatus



EventFacetQueryStatus contains the facet results.



_Appears in:_
- [EventFacetQuery](#eventfacetquery)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `facets` _[FacetResult](#facetresult) array_ | Facets contains the results for each requested facet. |  |  |


#### EventQuery



EventQuery searches Kubernetes Events stored in ClickHouse.


Unlike the native Events list (limited to 24 hours), EventQuery supports
up to 60 days of history. Results are returned in the Status field,
ordered newest-first.


Quick Start:


	apiVersion: activity.miloapis.com/v1alpha1
	kind: EventQuery
	metadata:
	  name: recent-pod-failures
	spec:
	  startTime: "now-7d"          # last 7 days
	  endTime: "now"
	  namespace: "production"      # optional: limit to namespace
	  fieldSelector: "type=Warning" # optional: standard K8s field selector
	  limit: 100


Time Formats:
- Relative: "now-30d" (great for dashboards and recurring queries)
- Absolute: "2024-01-01T00:00:00Z" (great for historical analysis)



_Appears in:_
- [EventQueryList](#eventquerylist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[EventQuerySpec](#eventqueryspec)_ |  |  |  |
| `status` _[EventQueryStatus](#eventquerystatus)_ |  |  |  |




#### EventQuerySpec



EventQuerySpec defines the search parameters.


Required: startTime and endTime define your search window (max 60 days).
Optional: namespace (limit to namespace), fieldSelector (standard K8s syntax),
limit (page size, default 100), continue (pagination).



_Appears in:_
- [EventQuery](#eventquery)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `startTime` _string_ | StartTime is the beginning of your search window (inclusive).<br /><br />Format Options:<br />- Relative: "now-30d", "now-2h", "now-30m" (units: s, m, h, d, w)<br />  Use for dashboards and recurring queries - they adjust automatically.<br />- Absolute: "2024-01-01T00:00:00Z" (RFC3339 with timezone)<br />  Use for historical analysis of specific time periods.<br /><br />Maximum lookback is 60 days from now.<br /><br />Examples:<br />  "now-7d"                      → 7 days ago<br />  "2024-06-15T14:30:00-05:00"   → specific time with timezone offset |  |  |
| `endTime` _string_ | EndTime is the end of your search window (exclusive).<br /><br />Uses the same formats as StartTime. Commonly "now" for the current moment.<br />Must be greater than StartTime.<br /><br />Examples:<br />  "now"                  → current time<br />  "2024-01-02T00:00:00Z" → specific end point |  |  |
| `namespace` _string_ | Namespace limits results to events from a specific namespace.<br />Leave empty to query events across all namespaces. |  |  |
| `fieldSelector` _string_ | FieldSelector filters events using standard Kubernetes field selector syntax.<br /><br />Supported Fields:<br />  metadata.name               - event name<br />  metadata.namespace          - event namespace<br />  metadata.uid                - event UID<br />  regarding.apiVersion        - regarding resource API version<br />  regarding.kind              - regarding resource kind (e.g., Pod, Deployment)<br />  regarding.namespace         - regarding resource namespace<br />  regarding.name              - regarding resource name<br />  regarding.uid               - regarding resource UID<br />  regarding.fieldPath         - regarding resource field path<br />  related.apiVersion          - related resource API version<br />  related.kind                - related resource kind (e.g., Node)<br />  related.namespace           - related resource namespace<br />  related.name                - related resource name<br />  reason                      - event reason (e.g., FailedMount, Pulled)<br />  type                        - event type (Normal or Warning)<br />  source.component            - reporting component<br />  source.host                 - reporting host<br />  reportingComponent          - reporting component (alias for source.component)<br />  reportingInstance           - reporting instance (alias for source.host)<br /><br />Operators: = (or ==), !=<br />Multiple conditions: comma-separated (all must match)<br /><br />Common Patterns:<br />  "type=Warning"                                  - Warning events only<br />  "regarding.kind=Pod"                            - Events for pods<br />  "reason=FailedMount"                            - Mount failure events<br />  "regarding.name=my-pod,type=Warning"            - Warnings for a specific pod<br />  "related.kind=Node"                              - Events related to nodes |  |  |
| `limit` _integer_ | Limit sets the maximum number of results per page.<br />Default: 100, Maximum: 1000.<br /><br />Use smaller values (10-50) for exploration, larger (500-1000) for data collection.<br />Use continue to fetch additional pages. |  |  |
| `continue` _string_ | Continue is the pagination cursor for fetching additional pages.<br /><br />Leave empty for the first page. If status.continue is non-empty after a query,<br />copy that value here in a new query with identical parameters to get the next page.<br />Repeat until status.continue is empty.<br /><br />Important: Keep all other parameters (startTime, endTime, namespace, fieldSelector,<br />limit) identical across paginated requests. The cursor is opaque - copy it exactly<br />without modification. |  |  |


#### EventQueryStatus



EventQueryStatus contains the query results and pagination state.



_Appears in:_
- [EventQuery](#eventquery)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `results` _[EventRecord](#eventrecord) array_ | Results contains matching Kubernetes Events, sorted newest-first.<br /><br />Each event follows the eventsv1.Event format with fields like:<br />  regarding.\{kind,name,namespace\}, reason, note, type,<br />  eventTime, series.count, reportingController<br /><br />Empty results? Try broadening your field selector or time range. |  |  |
| `continue` _string_ | Continue is the pagination cursor.<br />Non-empty means more results are available - copy this to spec.continue for the next page.<br />Empty means you have all results. |  |  |
| `effectiveStartTime` _string_ | EffectiveStartTime is the actual start time used for this query (RFC3339 format).<br /><br />When you use relative times like "now-7d", this shows the exact timestamp that was<br />calculated. Useful for understanding exactly what time range was queried, especially<br />for auditing, debugging, or recreating queries with absolute timestamps.<br /><br />Example: If you query with startTime="now-7d" at 2025-12-17T12:00:00Z,<br />this will be "2025-12-10T12:00:00Z". |  |  |
| `effectiveEndTime` _string_ | EffectiveEndTime is the actual end time used for this query (RFC3339 format).<br /><br />When you use relative times like "now", this shows the exact timestamp that was<br />calculated. Useful for understanding exactly what time range was queried.<br /><br />Example: If you query with endTime="now" at 2025-12-17T12:00:00Z,<br />this will be "2025-12-17T12:00:00Z". |  |  |


#### EventRecord



EventRecord represents a Kubernetes Event returned in EventQuery results.
This is a wrapper type registered under activity.miloapis.com/v1alpha1 that
embeds the events.k8s.io/v1 Event to avoid OpenAPI GVK conflicts while
preserving full event data.



_Appears in:_
- [EventQueryStatus](#eventquerystatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `event` _[Event](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#event-v1-events)_ | Event contains the full Kubernetes Event data in events.k8s.io/v1 format.<br />This includes fields like eventTime, regarding, note, type, reason,<br />reportingController, reportingInstance, series, and action. |  |  |


#### FacetResult



FacetResult contains the distinct values for a single facet.



_Appears in:_
- [ActivityFacetQueryStatus](#activityfacetquerystatus)
- [AuditLogFacetsQueryStatus](#auditlogfacetsquerystatus)
- [EventFacetQueryStatus](#eventfacetquerystatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `field` _string_ | Field is the field path that was queried. |  |  |
| `values` _[FacetValue](#facetvalue) array_ | Values contains the distinct values and their counts. |  |  |


#### FacetSpec



FacetSpec defines a single facet to retrieve.



_Appears in:_
- [ActivityFacetQuerySpec](#activityfacetqueryspec)
- [AuditLogFacetsQuerySpec](#auditlogfacetsqueryspec)
- [EventFacetQuerySpec](#eventfacetqueryspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `field` _string_ | Field is the activity field path to get distinct values for.<br /><br />Supported fields:<br />  - spec.actor.name: Actor display names<br />  - spec.actor.type: Actor types (user, serviceaccount, controller)<br />  - spec.resource.apiGroup: API groups<br />  - spec.resource.kind: Resource kinds<br />  - spec.resource.namespace: Namespaces<br />  - spec.changeSource: Change sources (human, system) |  |  |
| `limit` _integer_ | Limit is the maximum number of distinct values to return.<br />Default: 20, Maximum: 100. |  |  |


#### FacetTimeRange



FacetTimeRange specifies the time window for facet queries.



_Appears in:_
- [ActivityFacetQuerySpec](#activityfacetqueryspec)
- [AuditLogFacetsQuerySpec](#auditlogfacetsqueryspec)
- [EventFacetQuerySpec](#eventfacetqueryspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `start` _string_ | Start is the beginning of the time window (inclusive).<br />Supports RFC3339 timestamps and relative times (e.g., "now-7d"). |  |  |
| `end` _string_ | End is the end of the time window (exclusive).<br />Supports RFC3339 timestamps and relative times. Defaults to "now". |  |  |


#### FacetValue



FacetValue represents a single distinct value with its occurrence count.



_Appears in:_
- [FacetResult](#facetresult)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `value` _string_ | Value is the distinct field value. |  |  |
| `count` _integer_ | Count is the number of activities with this value. |  |  |




#### PolicyPreviewInput



PolicyPreviewInput contains the sample input for policy testing.



_Appears in:_
- [PolicyPreviewSpec](#policypreviewspec)
- [PolicyPreviewStatus](#policypreviewstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _string_ | Type indicates whether this is an audit log or event input.<br />Values: "audit", "event" |  |  |
| `audit` _[Event](#event)_ | Audit contains a sample audit log entry.<br />Required when Type is "audit". |  |  |
| `event` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#rawextension-runtime-pkg)_ | Event contains a sample Kubernetes event.<br />Required when Type is "event".<br />Uses RawExtension to allow flexible event structure. |  |  |


#### PolicyPreviewInputResult



PolicyPreviewInputResult contains the result for a single input.



_Appears in:_
- [PolicyPreviewStatus](#policypreviewstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `inputIndex` _integer_ | InputIndex is the index of this input in spec.inputs (0-based). |  |  |
| `matched` _boolean_ | Matched indicates whether any rule matched this input. |  |  |
| `matchedRuleIndex` _integer_ | MatchedRuleIndex is the index of the rule that matched (0-based).<br />-1 if no rule matched. |  |  |
| `matchedRuleType` _string_ | MatchedRuleType indicates whether the matched rule was an audit or event rule.<br />Empty if no rule matched. |  |  |
| `matchedRuleName` _string_ | MatchedRuleName is the name of the rule that matched this input.<br />This is the value from the rule's Name field in the policy spec.<br />Empty if no rule matched. |  |  |
| `error` _string_ | Error contains any error message if evaluating this input failed.<br />This could be a CEL compilation error or evaluation error. |  |  |


#### PolicyPreviewSpec



PolicyPreviewSpec defines the policy and inputs to test.



_Appears in:_
- [PolicyPreview](#policypreview)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `policy` _[ActivityPolicySpec](#activitypolicyspec)_ | Policy is the ActivityPolicy spec to test.<br />You can use the full spec from an existing policy or create a new one. |  |  |
| `inputs` _[PolicyPreviewInput](#policypreviewinput) array_ | Inputs contains sample audit logs and/or events to test against the policy.<br />Each input is evaluated independently and produces an Activity if a rule matches.<br />You can mix audit logs and events in the same request.<br />Optional when AutoFetch is specified. |  |  |
| `autoFetch` _[AutoFetchSpec](#autofetchspec)_ | AutoFetch automatically retrieves sample inputs based on the policy resource type.<br />When specified, the API queries recent audit logs and/or events matching the policy.<br />Mutually exclusive with manual inputs - only one should be provided. |  |  |
| `kindLabel` _string_ | KindLabel overrides the display label for the resource kind. |  |  |
| `kindLabelPlural` _string_ | KindLabelPlural overrides the plural display label. |  |  |


#### PolicyPreviewStatus



PolicyPreviewStatus contains the preview results.



_Appears in:_
- [PolicyPreview](#policypreview)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `activities` _[Activity](#activity) array_ | Activities contains the rendered Activity objects for inputs that matched a rule.<br />The order corresponds to the order of matched inputs (not necessarily the input order).<br />Inputs that don't match any rule are not included here. |  |  |
| `results` _[PolicyPreviewInputResult](#policypreviewinputresult) array_ | Results contains detailed results for each input, in the same order as spec.inputs.<br />Use this to see which inputs matched and any errors that occurred. |  |  |
| `fetchedInputs` _[PolicyPreviewInput](#policypreviewinput) array_ | FetchedInputs contains the auto-fetched sample inputs (only present when autoFetch was used).<br />This allows clients to see what data was tested. |  |  |
| `error` _string_ | Error contains a general error message if the preview failed entirely.<br />Individual input errors are reported in results[].error. |  |  |


#### ReindexConfig



ReindexConfig contains processing configuration options.



_Appears in:_
- [ReindexJobSpec](#reindexjobspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `batchSize` _integer_ | BatchSize is the number of events to process per batch.<br />Larger batches are faster but use more memory.<br />Default: 1000 | 1000 | Maximum: 10000 <br />Minimum: 100 <br /> |
| `rateLimit` _integer_ | RateLimit is the maximum events per second to process.<br />Prevents overwhelming ClickHouse.<br />Default: 100 | 100 | Maximum: 1000 <br />Minimum: 10 <br /> |
| `dryRun` _boolean_ | DryRun previews changes without writing activities.<br />Useful for estimating impact before execution.<br />Default: false |  |  |


#### ReindexJob



ReindexJob triggers re-processing of historical audit logs and events through
current ActivityPolicy rules. Use this to fix policy bugs retroactively, add
coverage for new policies, or refine activity summaries after policy improvements.


ReindexJob is a one-shot resource: once completed or failed, it cannot be
re-run. Create a new ReindexJob for subsequent re-indexing operations.


KUBERNETES EVENT LIMITATION:


When a Kubernetes Event is updated (e.g., count incremented from 1 to 5),
it retains the same UID. Re-indexing will produce ONE activity per Event UID,
reflecting the Event's final state. Historical activity occurrences from earlier
Event states are lost.


Example: Event "pod-oom" fires 5 times (count=5) → Re-indexing produces 1 activity (not 5)


Mitigation: Scope re-indexing to audit logs only via spec.policySelector to
preserve activities from earlier Event occurrences.


Example:


	kubectl apply -f - <<EOF
	apiVersion: activity.miloapis.com/v1alpha1
	kind: ReindexJob
	metadata:
	  name: fix-policy-bug-2026-02-27
	spec:
	  timeRange:
	    startTime: "now-7d"       # last 7 days (or use absolute: "2026-02-25T00:00:00Z")
	    endTime: "now"            # defaults to "now" if omitted
	  policySelector:
	    names: ["httpproxy-policy"]
	EOF


	kubectl get reindexjobs -w  # Watch progress



_Appears in:_
- [ReindexJobList](#reindexjoblist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ReindexJobSpec](#reindexjobspec)_ |  |  |  |
| `status` _[ReindexJobStatus](#reindexjobstatus)_ |  |  |  |




#### ReindexJobPhase

_Underlying type:_ _string_

ReindexJobPhase represents the lifecycle phase of a ReindexJob.



_Appears in:_
- [ReindexJobStatus](#reindexjobstatus)

| Field | Description |
| --- | --- |
| `Pending` |  |
| `Running` |  |
| `Succeeded` |  |
| `Failed` |  |


#### ReindexJobSpec



ReindexJobSpec defines the parameters for a re-indexing operation.



_Appears in:_
- [ReindexJob](#reindexjob)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `timeRange` _[ReindexTimeRange](#reindextimerange)_ | TimeRange specifies the time window of events to re-index.<br />Events outside this range are not processed. |  |  |
| `policySelector` _[ReindexPolicySelector](#reindexpolicyselector)_ | PolicySelector optionally limits re-indexing to specific policies.<br />If omitted, all active ActivityPolicies are evaluated. |  |  |
| `config` _[ReindexConfig](#reindexconfig)_ | Config contains processing configuration options. |  |  |
| `ttlSecondsAfterFinished` _integer_ | TTLSecondsAfterFinished limits the lifetime of a ReindexJob after it finishes<br />execution (either Succeeded or Failed). If set, the controller will delete the<br />ReindexJob resource after it has been in a terminal state for this many seconds.<br /><br />This field is optional. If unset, completed jobs are retained indefinitely.<br /><br />Example: Setting to 3600 (1 hour) allows users to inspect job results for an<br />hour after completion, after which the job is automatically cleaned up. |  | Minimum: 0 <br /> |


#### ReindexJobStatus



ReindexJobStatus represents the current state of a ReindexJob.



_Appears in:_
- [ReindexJob](#reindexjob)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _[ReindexJobPhase](#reindexjobphase)_ | Phase is the current lifecycle phase.<br />Values: Pending, Running, Succeeded, Failed |  |  |
| `message` _string_ | Message is a human-readable description of the current state. |  |  |
| `progress` _[ReindexProgress](#reindexprogress)_ | Progress contains detailed progress information. |  |  |
| `startedAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#time-v1-meta)_ | StartedAt is when processing began. |  |  |
| `completedAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#time-v1-meta)_ | CompletedAt is when processing finished (success or failure). |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#condition-v1-meta) array_ | Conditions represent the latest observations of the job's state. |  |  |


#### ReindexPolicySelector



ReindexPolicySelector specifies which policies to include in re-indexing.



_Appears in:_
- [ReindexJobSpec](#reindexjobspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `names` _string array_ | Names is a list of ActivityPolicy names to include.<br />Mutually exclusive with MatchLabels. |  |  |
| `matchLabels` _object (keys:string, values:string)_ | MatchLabels selects policies by label.<br />Mutually exclusive with Names. |  |  |


#### ReindexProgress



ReindexProgress contains detailed progress metrics.



_Appears in:_
- [ReindexJobStatus](#reindexjobstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `totalEvents` _integer_ | TotalEvents is the estimated total events to process. |  |  |
| `processedEvents` _integer_ | ProcessedEvents is the number of events processed so far. |  |  |
| `activitiesGenerated` _integer_ | ActivitiesGenerated is the number of activities created. |  |  |
| `errors` _integer_ | Errors is the count of non-fatal errors encountered. |  |  |
| `currentBatch` _integer_ | CurrentBatch is the batch number currently being processed. |  |  |
| `totalBatches` _integer_ | TotalBatches is the estimated total number of batches. |  |  |


#### ReindexTimeRange



ReindexTimeRange specifies the time window for re-indexing.



_Appears in:_
- [ReindexJobSpec](#reindexjobspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `startTime` _string_ | StartTime is the beginning of the time range (inclusive).<br />Must be within the ClickHouse retention window (60 days).<br /><br />Format Options:<br />- Relative: "now-30d", "now-2h", "now-30m" (units: s, m, h, d, w)<br />  Use for recent time windows - they adjust automatically at job start.<br />- Absolute: "2026-02-01T00:00:00Z" (RFC3339 with timezone)<br />  Use for specific historical time periods.<br /><br />Examples:<br />  "now-7d"                      → 7 days before job starts<br />  "2026-02-25T00:00:00Z"        → specific time with UTC<br />  "2026-02-25T00:00:00-08:00"   → specific time with timezone offset<br /><br />Note: Relative times are resolved when the job STARTS processing,<br />not when the resource is created. This ensures consistent time ranges<br />even if the job is queued. |  |  |
| `endTime` _string_ | EndTime is the end of the time range (exclusive).<br />Defaults to "now" (job start time) if omitted.<br /><br />Uses the same formats as StartTime.<br />Must be greater than StartTime.<br /><br />Examples:<br />  "now"                  → current time when job starts<br />  "2026-03-01T00:00:00Z" → specific end point<br />  "now-1h"               → 1 hour before job starts |  |  |


