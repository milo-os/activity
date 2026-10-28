# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> **Context Optimization**: This file is structured for efficient agent usage. The "Agent Routing" section defines what context each agent needs. When spawning subagents, pass only relevant sections—not the entire file. Sections marked `<!-- reference -->` are lookup tables; don't include them in agent prompts unless specifically needed.

## Project Overview

Activity is a Kubernetes extension that provides queryable audit logs, events, and human-readable activity summaries. It's built as an aggregated API server, making it work natively with kubectl and Kubernetes clients.

**Module**: `go.miloapis.com/activity`

## Agent Routing

**MANDATORY: All implementation work MUST be performed by subagents.** Never directly edit code, configuration, or documentation in the parent conversation. Instead, always delegate to the appropriate specialized agent from the table below. The parent conversation should only coordinate agents, pass context between them, and communicate results to the user.

Do NOT ask the user which agent to use - pick the appropriate one based on what files or features are being modified.

| Task Type | Agent | When to Use |
|-----------|-------|-------------|
| UI/Frontend | `datum-platform:frontend-dev` | React, TypeScript, CSS, anything in `ui/` directory |
| Go Backend | `datum-platform:api-dev` | Go code in `cmd/`, `internal/`, `pkg/` directories |
| Infrastructure | `datum-platform:sre` | Kustomize, Dockerfile, CI/CD, `config/` directory, `.infra/` for deployment |
| Tests | `datum-platform:test-engineer` | Writing or fixing Go tests |
| Code Review | `datum-platform:code-reviewer` | After implementation, before committing |
| Documentation | `datum-platform:tech-writer` | README, docs/, guides, API documentation |
| Architecture | `Plan` | Designing new features or significant refactors |
| Exploration | `Explore` | Understanding codebase structure or finding code |

**Key principles:**
- **Always use subagents** — never write code, edit files, or run build/test commands directly in the parent conversation
- Use agents proactively without being asked
- For multi-step tasks, use the appropriate agent for each step (launch independent agents in parallel when possible)
- After making code changes, always use `code-reviewer` to validate
- For UI changes, run `npm run build` and `npm run test:e2e` to verify
- **Always test infrastructure changes in a test environment before opening a PR** - Deploy to the test-infra KIND cluster (`task test-infra:cluster-up`) and verify resources work correctly before pushing changes to staging/production repos
- **Use Telepresence for debugging staging issues** - When investigating bugs that only reproduce in staging, intercept the service and run it locally with `task test-infra:telepresence:intercept SERVICE=<name>`. See "Remote Debugging with Telepresence" section.

### Agent Context Requirements

Each agent only needs specific context. When spawning agents, pass minimal relevant info in prompts—don't repeat the entire CLAUDE.md:

| Agent | Required Context | Skip (don't include in prompt) |
|-------|-----------------|--------------------------------|
| `frontend-dev` | UI commands, file paths in `ui/` | Go architecture, ClickHouse, NATS, data pipeline |
| `api-dev` | Go patterns, API resource types, key directories | UI commands, dev environment setup, migrations |
| `sre` | Config structure, build commands, deployment | Code architecture details, CEL patterns |
| `test-engineer` | Test commands, package being tested | Full architecture, deployment, UI |
| `Explore` | Key directories, architecture overview | Build commands, dev setup, deployment |
| `code-reviewer` | Architecture, multi-tenancy model, conventions | Dev environment, build commands |
| `tech-writer` | API resources, architecture overview | Implementation details, build commands |

### Agent Output Guidelines

Agents should return **concise summaries** to minimize context bloat in the parent conversation:

| Agent | Return | Don't Return |
|-------|--------|--------------|
| `Explore` | File paths + 1-line descriptions | Full file contents, extensive code quotes |
| `api-dev` | What was changed + file paths | Full diffs, unchanged code |
| `frontend-dev` | Components modified + any build errors | Full file contents |
| `code-reviewer` | Numbered findings list with file:line refs | Full code blocks for context |
| `test-engineer` | Pass/fail summary + failure messages only | Full test output, passing test details |
| `sre` | Changed manifests + deployment notes | Full YAML contents |

### Multi-Step Task Decomposition

For complex tasks, decompose to minimize per-agent context:

1. **Explore first** (use `model: "haiku"`): Find relevant files → return only paths
2. **Plan if needed**: Design approach → return bullet points only
3. **Implement** (sonnet): Work on specific files identified in step 1
4. **Review**: Check only the changed files

**Critical**: Pass only what's needed between steps. Don't re-explore what's already known.

## Build and Development Commands <!-- reference -->

All development tasks use [Task](https://taskfile.dev). Run `task --list` to see all available commands.

### Building

```bash
task build                    # Build the activity binary to bin/activity
task dev:build                # Build container image (ghcr.io/datum-cloud/activity:dev)
```

### Testing

```bash
go test ./...                 # Run all Go tests
go test ./internal/cel/...    # Run tests in a specific package
go test -run TestName ./...   # Run a specific test
```

### Code Generation

```bash
task generate                 # Run all code generation (OpenAPI, RBAC, migrations, docs)
task generate:openapi         # Generate Kubernetes OpenAPI definitions
task generate:rbac            # Generate RBAC manifests from kubebuilder annotations
task generate:docs            # Generate API reference documentation
```

### UI Component Library

The UI is a React component library in `/ui`:

```bash
cd ui && npm install && npm run build    # Build the library
cd ui && npm run lint                    # Lint TypeScript
cd ui && npm run type-check              # Type check
```

Or via Task:
```bash
task ui:build                            # Build component library
task ui:example:dev                      # Run example app in dev mode
```

## Architecture

### Components

- **activity-apiserver**: Kubernetes aggregated API server that handles queries and Watch streams
- **activity-processor**: Processes audit logs/events through ActivityPolicy rules to generate Activities
- **activity-controller-manager**: Manages ActivityPolicy lifecycle and status
- **kubectl-activity**: CLI plugin for command-line querying
- **activity-ui**: React component library for web interfaces

### Data Pipeline

1. **Audit logs/Events** → Published to NATS JetStream by control plane
2. **Vector** → Receives from NATS, routes to ClickHouse and back to NATS for processing
3. **activity-processor** → Consumes audit events, applies ActivityPolicy rules, produces Activities
4. **ClickHouse** → Stores audit logs, events, and activities for long-term querying
5. **etcd** → Stores ActivityPolicy resources with Watch support

### API Resources (`activity.miloapis.com/v1alpha1`) <!-- reference -->

| Resource | Type | Purpose |
|----------|------|---------|
| AuditLogQuery | Ephemeral | Execute audit log searches |
| AuditLogFacetsQuery | Ephemeral | Get distinct values for autocomplete |
| Activity | Read-only | Query translated activity records |
| ActivityFacetQuery | Ephemeral | Get distinct activity field values |
| ActivityPolicy | Persistent | Define translation rules (CEL-based) |
| PolicyPreview | Ephemeral | Test policies against sample inputs |
| EventQuery | Ephemeral | Query cluster events |
| EventFacetQuery | Ephemeral | Get distinct event field values |

### Key Directories

- `cmd/activity/` - Main binary entrypoint (subcommands: apiserver, processor, controller-manager)
- `internal/apiserver/` - Aggregated API server implementation
- `internal/storage/` - ClickHouse storage backend
- `internal/cel/` - CEL expression engine for ActivityPolicy rules
- `internal/processor/` - Activity translation processor
- `internal/controller/` - Kubernetes controller for ActivityPolicy
- `internal/watch/` - Watch API implementation via NATS consumers
- `pkg/apis/activity/v1alpha1/` - API type definitions
- `pkg/mcp/` - MCP (Model Context Protocol) server implementation
- `config/` - Kustomize deployment manifests
- `migrations/` - ClickHouse schema migrations
- `.infra/` - Cloned infra repo for deployment configuration (see Infrastructure Management below)

### Infrastructure Management

The `.infra/` directory contains a clone of the `datum-cloud/infra` repository. **Always use this folder for managing Activity's deployment infrastructure**, including:

- Flux Kustomizations
- Environment-specific patches (staging/production)
- Secret configurations
- Dependencies and deployment ordering

**Key paths in `.infra/`:**

| Path | Purpose |
|------|---------|
| `.infra/apps/activity-system/base/` | Base Flux Kustomizations for all Activity components |
| `.infra/apps/activity-system/overlays/staging/` | Staging-specific patches and resources |
| `.infra/apps/activity-system/overlays/production/` | Production-specific patches |
| `.infra/clusters/staging/apps/activity-system.yaml` | Staging cluster entry point |
| `.infra/clusters/production/apps/activity-system.yaml` | Production cluster entry point |

**Workflow for infrastructure changes:**

1. Make changes in `.infra/apps/activity-system/`
2. Commit and push to `datum-cloud/infra` repo (not this repo)
3. FluxCD will reconcile changes to the cluster

**Important:** The `.infra/` folder is gitignored from this repo. Changes must be committed to the infra repo separately.

## Development Environment <!-- reference -->

### Lightweight Dev Setup (single-replica, minimal resources)

```bash
task dev:setup               # Full setup: cluster + dependencies + deploy
task dev:redeploy            # Quick rebuild and redeploy after code changes
```

### Full Test Environment (HA with S3 storage)

```bash
task test:setup              # Full HA setup with 3-replica ClickHouse
task test:redeploy           # Quick rebuild and redeploy
```

### Cluster Access

```bash
task test-infra:kubectl -- <args>        # Run kubectl against dev cluster
task test-infra:kubectl -- get pods -n activity-system
task test-infra:kubectl -- logs -l app=activity-apiserver -n activity-system -f
```

### Database Migrations

```bash
task migrations:new NAME=description     # Create new migration file
task migrations:generate                 # Generate ConfigMap from migrations
task migrations:cluster:verify           # Verify schema in cluster
```

**Important:** After adding or modifying any SQL file in `migrations/`, always run `task migrations:generate` to regenerate the ConfigMap at `config/components/clickhouse-migrations/configmap.yaml`. The raw SQL files are not deployed directly — the ConfigMap is what gets deployed to the cluster.

### Remote Debugging with Telepresence

When debugging issues in staging, use Telepresence to intercept a service and run it locally with full access to cluster resources (NATS, ClickHouse, etcd, milo).

```bash
# Install Telepresence CLI (one-time)
task test-infra:telepresence:install

# Connect to staging cluster
KUBECONFIG=~/.kube/gke-staging task test-infra:telepresence:connect

# Intercept a service to run locally
task test-infra:telepresence:intercept SERVICE=activity-apiserver NAMESPACE=activity-system PORT=6443

# Load environment variables from intercepted service
source /tmp/telepresence-activity-apiserver.env

# Run the service locally with debugger
go run ./cmd/activity apiserver --secure-port=6443

# When done, release the intercept
telepresence leave activity-apiserver
telepresence quit
```

**Available services to intercept:**
- `activity-apiserver` (port 6443) - API server handling queries
- `activity-processor` (port 8080) - Event processing pipeline
- `activity-controller-manager` (port 8080) - ActivityPolicy controller

**When to use Telepresence:**
- Debugging issues that only reproduce in staging with real data
- Testing changes against production-like NATS streams and ClickHouse data
- Investigating connectivity or configuration issues

## Multi-Tenancy Model

Data is scoped by tenant:
- **Platform**: All data across all tenants
- **Organization**: Data within a specific organization
- **Project**: Data within a specific project
- **User**: Actions performed by a specific user

Scopes are NOT hierarchically inclusive - query each scope directly.

## ActivityPolicy Translation

ActivityPolicy resources define how audit logs/events are translated into human-readable Activities using CEL expressions:

```yaml
apiVersion: activity.miloapis.com/v1alpha1
kind: ActivityPolicy
spec:
  resource:
    apiGroup: networking.datumapis.com
    kind: HTTPProxy
  auditRules:
    - match: "audit.verb == 'create'"
      summary: "{{ actor }} created {{ link(kind + ' ' + audit.objectRef.name, audit.responseObject) }}"
```

## Stable Codebase Facts <!-- reference -->

These locations rarely change—agents should use these directly without re-exploring:

| What | Location | Notes |
|------|----------|-------|
| API type definitions | `pkg/apis/activity/v1alpha1/` | All CRD types live here |
| Storage implementations | `internal/storage/` | ClickHouse queries |
| CEL expression logic | `internal/cel/` | Policy matching engine |
| UI components | `ui/src/components/` | React component library |
| API server handlers | `internal/apiserver/` | REST handlers for each resource |
| Kustomize base | `config/base/` | Core deployment manifests |
| Kustomize components | `config/components/` | Optional features (ui, vector, etc.) |
| Database migrations | `migrations/` | ClickHouse schema files |

## Technology Stack

- **Go 1.25+** - Backend implementation
- **NATS JetStream** - Durable event streaming
- **Vector** - Data pipeline routing
- **ClickHouse** - Analytics storage
- **etcd** - Policy persistence
- **React/TypeScript** - UI components
