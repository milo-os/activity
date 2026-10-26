# ClickHouse Schema Migrations

Versioned SQL migrations for the ClickHouse audit database.

## Quick Start

```bash
# Create a new migration
task migrations:new NAME=add_field_name

# Test locally
task migrations:local

# Generate ConfigMap for Kubernetes
task migrations:generate
```

## File Naming

Migrations use the pattern: `{version}_{description}.sql`

- Version: 3-digit number (001, 002, 003)
- Description: snake_case description

Examples:
- `001_initial_schema.sql`
- `002_add_user_field.sql`

## How It Works

Migrations run automatically on deployment via a Kubernetes Job that:
- Checks the `audit.schema_migrations` table for applied migrations
- Runs only new migrations in order
- Self-deletes after 5 minutes (recreated by GitOps)

Always use `IF NOT EXISTS` / `IF EXISTS` to make migrations idempotent.

## Environment-Specific Migrations

A change that only one environment should get, such as a shorter retention in
staging, goes in `environments/<env>/` instead of this directory.

- Versions **001–899** are shared. Versions **900–999** are for environment
  migrations, so no shared migration takes a number one environment would skip.
  `task migrations:generate` fails if a file is in the wrong range.
- `task migrations:generate` writes `config/overlays/clickhouse-migrations-<env>/`:
  the shared component plus a patch that adds that environment's files to the
  same ConfigMap. The Job applies them after the shared ones.
- The environment's deployment points its migrations at that overlay instead of
  `components/clickhouse-migrations`. Every other environment is unaffected.

Current environment migrations:

| Environment | Migration | What it does |
|---|---|---|
| staging | `900_audit_logs_30_day_retention` | `audit_logs` keeps 30 days and deletes older days, instead of moving them to cold storage after 90 days |

A shared migration that changes `audit_logs`' TTL would override staging's
30 days. Pair it with a matching staging migration.

## See Also

- [Migration Component README](../config/components/clickhouse-migrations/README.md) - Kubernetes deployment details
