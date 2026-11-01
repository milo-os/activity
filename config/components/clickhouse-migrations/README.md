# ClickHouse Migrations Component

Runs database schema migrations as a Kubernetes Job.

## What It Does

This component deploys a Job that:
- Runs migrations from the `migrations/` directory
- Tracks applied migrations in `audit.schema_migrations`
- Auto-deletes after 5 minutes
- Gets recreated by GitOps (Flux/ArgoCD) to apply new migrations

## Files

- `job.yaml` - Kubernetes Job definition
- `configmap.yaml` - Auto-generated from `migrations/` directory
- `kustomization.yaml` - Component definition

## Usage

Include in your overlay:

```yaml
# config/overlays/{env}/kustomization.yaml
components:
  - ../../components/clickhouse-migrations
```

## Adding New Migrations

1. Create migration: `task migrations:new NAME=add_field`
2. Generate ConfigMap: `task migrations:generate`
3. Update `job.yaml` to mount the new migration file
4. Deploy: Apply your overlay

The Job will run automatically and apply new migrations.

## Environment-Specific Migrations

An environment that needs migrations of its own deploys
`config/overlays/clickhouse-migrations-<env>/` instead of this component. That
overlay is generated from `migrations/environments/<env>/`; see the
[migrations README](../../../migrations/README.md#environment-specific-migrations).

## Troubleshooting

```bash
# Check Job status
kubectl get jobs -n activity-system

# View logs
kubectl logs job/clickhouse-migrate -n activity-system

# Verify applied migrations
task migrations:cluster:verify
```
