# Kustomize Components

Optional components that can be selectively included in overlays.

## Available Components

- **namespace** - Creates the activity-system namespace
- **api-registration** - Kubernetes API aggregation (APIService registration)
- **cert-manager-ca** - CA infrastructure for TLS certificates
- **clickhouse-database** - ClickHouse database deployment
- **clickhouse-migrations** - Database schema migrations
- **grafana-clickhouse** - Grafana datasource configuration
- **mcp-server** - Activity MCP server in HTTP mode (`activity mcp --transport=http`) for the Patch assistant (see below)
- **nats-edge-relay** - Edge-local NATS relay that leafnodes into the hub
- **nats-streams** - NATS JetStream configuration
- **observability** - ServiceMonitors, alerts, and dashboards
- **rustfs-bucket** - S3-compatible object storage
- **tracing** - OpenTelemetry distributed tracing
- **vector-aggregator** - Vector aggregator for log processing
- **vector-sidecar** - Vector sidecar for audit log collection

## Usage

Include components in your overlay's `kustomization.yaml`:

```yaml
components:
  - ../../components/cert-manager-ca
  - ../../components/clickhouse-database
```

## mcp-server

Runs `activity mcp --transport=http --addr=:8080` as the `activity-mcp`
Deployment and ClusterIP Service in `activity-system`. It serves:

| Path | Purpose |
|------|---------|
| `/mcp` | Streamable HTTP MCP endpoint the assistant calls |
| `/llms-full.txt` | Knowledge document registered in the agent configuration |
| `/runbooks/{name}.md` | Skill bodies, fetched on demand |
| `/healthz` | Liveness and readiness |

The server reads as the caller. Every tool call uses the bearer token the
assistant forwards, so the `activity-mcp` ServiceAccount has no RBAC and no
mounted token. Do not bind roles to it.

The deployer must provide:

- A ConfigMap `activity-mcp-milo-kubeconfig` with key `mcp-milo-kubeconfig.yaml`.
  It is a kubeconfig that names the bare Milo API server and
  `certificate-authority: /etc/kubernetes/pki/trust/ca.crt`, with an empty user.
- The `datum-control-plane-trust-bundle` ConfigMap, mounted at
  `/etc/kubernetes/pki/trust`.

The bundled NetworkPolicy `activity-mcp-assistant-only` admits only
`patch-system` pods labelled `app in (assistant, assistant-apiserver)` on 8080.
It allows egress only to DNS and to Milo at `datum-system:6443`. The assistant
also needs a matching egress rule, and its host has to appear in its
`CAPABILITY_IDENTITY_FORWARD_HOSTS`.

This is a component rather than part of `config/base`. Base adds
`app.kubernetes.io/component: apiserver` to selectors, so if this component
were part of base, the apiserver Service would select these pods. Deploy it on
its own with `config/overlays/mcp-server`, which pins the image tag at publish
time. The dev overlay includes it for route smoke tests only. Dev has no Milo,
so tool calls fail there.

The matching catalog registration (Service `activity`, ServiceAgent
`activity-assistant`, ServiceAgentConfiguration `activity-assistant-v1`) lives
in `config/milo/assistant-capability`. It is a standalone Kustomization, not
part of `config/milo`, because it is applied to Milo **in staging only**: infra
points a staging-only Flux Kustomization at `./milo/assistant-capability` in
the `activity-kustomize` bundle. Production Patch reaches service MCP servers
only through AI-gateway endpoints, and there is no production MCPRoute for
`activity-mcp` yet, so production stays unregistered until there is.

Callers need read access to Activity resources in the project. The default
organization roles already grant it: `viewer` (`datum-cloud-viewer`) inherits
`activity.miloapis.com-viewer`, and `editor` and `owner` inherit `viewer`.
Members who hold only custom roles need a role that includes
`activity.miloapis.com-viewer`.
