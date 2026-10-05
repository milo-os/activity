# Kustomize Components

Optional components that can be selectively included in overlays.

## Available Components

- **namespace** - Creates the activity-system namespace
- **api-registration** - Kubernetes API aggregation (APIService registration)
- **cert-manager-ca** - CA infrastructure for TLS certificates
- **clickhouse-database** - ClickHouse database deployment
- **clickhouse-migrations** - Database schema migrations
- **grafana-clickhouse** - Grafana datasource configuration
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
