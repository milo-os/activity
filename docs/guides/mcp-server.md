# Connecting AI Assistants to Your Activity Data

The Activity service includes a built-in MCP (Model Context Protocol) server.
Once connected, your AI assistant can query your control plane's audit logs, activity
history, and events directly — no copy-pasting kubectl output, no manual log
searches.

Ask questions in plain language:

- "What changed in the last hour?"
- "Who modified the `api-gateway` HTTP proxy?"
- "Show me all failed operations from the last 24 hours."
- "What has alice@example.com been doing this week?"

The assistant uses live data from your control plane to answer.

## How it works

The MCP server is a subcommand of the `activity` binary. It runs as a local
process on your machine and connects to your AI assistant. It uses your existing
credentials to query the Activity API on the assistant's behalf.

No data is sent to any remote service. The MCP server reads from your control plane
using your existing kubeconfig credentials.

## Prerequisites

- The `activity` binary installed and in your `$PATH`
- A kubeconfig with access to the Activity API resources
- An MCP-compatible AI client (Claude Desktop, Claude Code, Cursor, VS Code
  with an MCP extension, etc.)

## Connection setup

### Claude Desktop

Add the following to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "activity": {
      "command": "activity",
      "args": ["mcp", "--kubeconfig", "/path/to/your/kubeconfig"]
    }
  }
}
```

The config file is located at:
- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Linux: `~/.config/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`

### Claude Code

Add the server to your project or global MCP configuration:

```json
{
  "mcpServers": {
    "activity": {
      "command": "activity",
      "args": ["mcp"]
    }
  }
}
```

When `--kubeconfig` is omitted, the server auto-detects the configuration:
in-cluster config when running inside a pod, or the default kubeconfig
location and current context otherwise.

### Using a specific kubeconfig context

To point at a specific cluster context:

```json
{
  "mcpServers": {
    "activity": {
      "command": "activity",
      "args": ["mcp", "--context", "my-staging-cluster"]
    }
  }
}
```

The `activity mcp` subcommand accepts `--kubeconfig`, `--context`, and `--namespace` flags. Run `activity mcp --help` for the full flag reference.

## Available tools

The MCP server registers 14 tools across six categories. Your AI assistant
selects the right tool automatically based on your question.

### Audit log tools

Detailed records of every API operation — useful when you need the exact details
of what was called, by whom, and what the response was.

| Tool | What it does |
|------|-------------|
| `query_audit_logs` | Search audit logs with CEL filters, time ranges, and result limits |
| `get_audit_log_facets` | Get distinct values and counts for audit log fields (users, verbs, resources, namespaces) |

### Activity tools

Human-readable summaries translated from audit logs via ActivityPolicy rules.
Use these when you want plain-language descriptions like "alice created HTTP
proxy api-gateway".

| Tool | What it does |
|------|-------------|
| `query_activities` | Search activity summaries with filters for actor, resource kind, change source, and full-text search |
| `get_activity_facets` | Get distinct values for activity fields to understand who's active and what's changing |

### Investigation tools

Higher-level tools designed for specific investigation patterns, built on top
of audit logs and activities.

| Tool | What it does |
|------|-------------|
| `find_failed_operations` | Find API calls that returned 4xx or 5xx responses — useful for debugging permission denials and failed deployments |
| `get_resource_history` | Get the full change history for a specific resource by name, kind, or UID |
| `get_user_activity_summary` | Get a summary of a specific user's recent actions, including resource types touched and activity by day |

### Analytics tools

Tools for trend analysis, summaries, and period comparisons.

| Tool | What it does |
|------|-------------|
| `get_activity_timeline` | Activity counts grouped by hour or day — useful for correlating incidents with activity spikes |
| `summarize_recent_activity` | Generate a summary with top actors, most-changed resources, and key highlights for a time period |
| `compare_activity_periods` | Compare activity between two time windows to identify what changed, new actors, and volume trends |

### Event tools

Control plane events (separate from audit logs) that capture resource lifecycle
changes, provisioning status, warnings, and errors.

| Tool | What it does |
|------|-------------|
| `query_events` | Search control plane events with filters and time ranges |
| `get_event_facets` | Get distinct values for event fields (type, reason, source component, involved resource) |

### Policy tools

Tools for working with ActivityPolicy resources that define how audit logs are
translated into human-readable activities.

| Tool | What it does |
|------|-------------|
| `list_activity_policies` | List configured ActivityPolicies and their status |
| `preview_activity_policy` | Test a policy against sample audit events before deploying it |

## Example queries

The following examples show natural-language prompts you can give your AI
assistant once the MCP server is connected.

**Incident investigation**

```
What changed in the production namespace in the last two hours?
```

```
Show me all failed operations from the last 24 hours, grouped by status code.
```

```
Who last modified the secret named database-credentials?
```

**User activity review**

```
What has alice@example.com done in the last week?
```

```
Show me all resources that bob@example.com created or deleted in March.
```

**Trend analysis**

```
Compare activity this week vs last week. What changed?
```

```
When was the busiest period of control plane activity in the last 30 days?
```

**Resource history**

```
Show me the full change history for the HTTPProxy named api-gateway.
```

```
What operations were performed on deployments in the default namespace today?
```

**Policy development**

```
I'm writing an ActivityPolicy for NetworkPolicy resources. Preview it against
recent audit events to see what summaries it would generate.
```

## Time expressions

All tools that accept `startTime` and `endTime` support relative time
expressions:

| Expression | Meaning |
|-----------|---------|
| `now` | Current time |
| `now-1h` | One hour ago |
| `now-24h` | 24 hours ago |
| `now-7d` | Seven days ago |
| `now-30d` | 30 days ago |

Absolute timestamps in RFC 3339 format (`2026-03-10T14:00:00Z`) are also
accepted.

## HTTP mode (Patch assistant)

Besides the local stdio mode above, `activity mcp` can serve the tools over
[Streamable HTTP](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports#streamable-http)
so Datum's assistant, Patch, can call them on behalf of a signed-in user:

```bash
activity mcp --transport=http --addr=:8080 --kubeconfig=/etc/activity-mcp/kubeconfig
```

`--transport` defaults to `stdio`, so existing local setups are unchanged.

### Routes

| Route | Purpose |
|-------|---------|
| `POST /mcp` | MCP endpoint, stateless (no session is kept between requests) |
| `GET /llms-full.txt` | Knowledge document the assistant reads before calling tools: the Activity model, time syntax, scoping, and every exposed tool |
| `GET /runbooks/<name>.md` | Investigation skills: `what-changed`, `who-changed-resource`, `failed-operation-triage`, `event-investigation` |
| `GET /healthz` | Liveness |

The knowledge and runbook documents are static, contain no tenant data, and
need no credentials. Only `/mcp` reads data.

### Request headers

Every `/mcp` request must carry two headers, which Patch forwards from the
signed-in user's session:

| Header | Value |
|--------|-------|
| `Authorization` | `Bearer <token>` — the user's own token |
| `X-Datum-Project` | The project the user is working in |

The server holds no credentials of its own. For each request it builds a
client that sends the caller's token to Milo at that project's control plane
(`/apis/resourcemanager.miloapis.com/v1alpha1/projects/<project>/control-plane`).
Milo authenticates the user, enforces their access, and forwards the query to
Activity with the project attached, so every result is scoped to that one
project. A tool call can never see more than the user could see themselves.

If the token or project header is missing, or the project is not a valid
DNS-1123 name, the tool call fails with an error explaining that the calling
client is misconfigured, and no request is sent to the API. This matters
because Activity treats a request with no project attached as a
platform-wide query; the server never lets one through.

The project comes only from the header, never from a tool argument, so the
model cannot be talked into reading another project.

### Access

Tool calls read with the user's own permissions, so the user needs read access
to Activity resources in the project. Organization members with Datum's
default roles already have it: `viewer` (`datum-cloud-viewer`) inherits
`activity.miloapis.com-viewer`, and `editor` and `owner` inherit `viewer`.
Only members who hold nothing but custom roles may lack it; give them a role
that includes `activity.miloapis.com-viewer`.

### Project-only scoping

HTTP mode answers questions about the current project only. It cannot query
other projects, an organization as a whole, or the platform. An empty result
means nothing was recorded in that project for the window and filters used.

### Exposed tools

HTTP mode registers ten tools, all read-only:

| Category | Tools |
|----------|-------|
| Activity | `query_activities`, `get_activity_facets` |
| Investigation | `get_resource_history`, `find_failed_operations`, `get_user_activity_summary` |
| Analytics | `summarize_recent_activity`, `get_activity_timeline`, `compare_activity_periods` |
| Events | `query_events`, `get_event_facets` |

Not exposed:

- **Raw audit log tools** (`query_audit_logs`, `get_audit_log_facets`). Audit
  records are the full API request log, including request and response
  bodies. That is an operator's view, not something a project member needs
  from an assistant. `find_failed_operations` does read audit logs, because
  failed requests never become Activities, but it returns only a fixed set of
  summarised fields (time, user, verb, resource, name, status code, message).
- **Policy tools** (`list_activity_policies`, `preview_activity_policy`).
  ActivityPolicies are platform-managed translation rules, not project data.

Stdio mode still registers all 14 tools.

### Deployment notes

- The kubeconfig supplies **only** the Milo API server address and CA. Any
  credentials in it (token, client certificate, exec plugin, impersonation)
  are ignored, so it can contain an empty user.
- The server never falls back to in-cluster configuration. It refuses to
  start if no kubeconfig is found (via `--kubeconfig`, `KUBECONFIG`, or
  `~/.kube/config`).
- As a best-effort check, it also refuses to start if the kubeconfig's server
  address is exactly `https://$KUBERNETES_SERVICE_HOST:$KUBERNETES_SERVICE_PORT`,
  the address in-cluster config resolves to. Other names for the local API
  server, such as `https://kubernetes.default.svc`, are not detected. The
  check only catches a common misconfiguration: the server's own credentials
  are stripped from every read regardless of the address.
- The kubeconfig's server address must be bare: no path, no query or
  fragment, and no `user:password@` credentials. The server appends each
  caller's project path itself and refuses to start otherwise.
- Each `/mcp` request body is limited to 1 MiB, and each tool call to 50
  seconds overall, however many queries it runs.
- On SIGTERM the server stops accepting connections and lets in-flight
  requests finish for up to 25 seconds.
- Point liveness and readiness probes at `/healthz`.
- The server is stateless, so it can run with any number of replicas behind a
  plain load balancer; no session affinity is needed.

### Deploying and registering with Patch

- `config/overlays/mcp-server` deploys the `activity-mcp` Deployment, Service
  and NetworkPolicy (component `config/components/mcp-server`); the published
  `activity-kustomize` bundle pins its image tag.
- `config/milo/assistant-capability` registers the server with Patch through
  the service catalog (Service `activity`, ServiceAgent `activity-assistant`,
  ServiceAgentConfiguration `activity-assistant-v1`). It is a standalone
  Kustomization, separate from `config/milo`, and is applied to Milo **in
  staging only**, at `./milo/assistant-capability` in the bundle.
- Production stays unregistered for now. Production Patch reaches service MCP
  servers only through AI-gateway endpoints, and there is no production
  MCPRoute for `activity-mcp` yet. Once one exists, production can apply the
  same registration with the gateway URL.

## Troubleshooting

**The server fails to start with "failed to create kubernetes config"**

The server cannot locate or parse your kubeconfig. Pass the path explicitly:

```json
"args": ["mcp", "--kubeconfig", "/absolute/path/to/kubeconfig"]
```

**Tools return empty results**

The Activity API server may not be deployed, or your kubeconfig context may
be pointing at the wrong control plane. Verify access with:

```bash
kubectl get activities --context your-context
```

**Permission denied errors in tool responses**

Your credentials need permission to query Activity resources. On Datum
Cloud, the default organization roles (`viewer`, `editor`, `owner`) include it
through `activity.miloapis.com-viewer`; members with only custom roles may
not. Ask your administrator to verify access.

**The assistant doesn't use the Activity tools**

Restart your AI client after modifying the MCP configuration. Most clients
only load MCP servers at startup.
