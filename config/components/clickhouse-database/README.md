# ClickHouse Database Component

Deploys a highly-available ClickHouse cluster for storing audit events.

## Configuration

- **3 replicas, 1 shard** - Survives 1 node failure
- **ReplicatedReplacingMergeTree** - Automatic replication with deduplication
- **Quorum writes** (2/3) - Strong consistency guarantees
- **Hot/cold tiering** - Local SSD → S3 after 90 days
- **Keeper coordination** - Replaces ZooKeeper for HA

## Prerequisites

1. Deploy ClickHouse Keeper first (see `../clickhouse-keeper/`)
2. ClickHouse Operator v0.25.6+
3. S3-compatible storage for cold tier

The ClickHouse Operator creates the PodDisruptionBudget for these pods, allowing one down at a time. Do not add a second budget over them: Kubernetes refuses to evict a pod under more than one, so neither would apply.

## Usage

```yaml
# config/overlays/{env}/kustomization.yaml
components:
  - ../../components/clickhouse-keeper
  - ../../components/clickhouse-database
```

Override storage settings via patches in your overlay.

## How It Works

- **Writes**: Insert on any replica → replicate via Keeper → quorum (2/3)
  acknowledges
- **Reads**: Query any replica with read-after-write consistency
- **Failures**: Tolerates 1 replica down (2/3 quorum maintained)

**Learn more**:

- [Data Replication](https://clickhouse.com/docs/en/engines/table-engines/mergetree-family/replication)
- [ClickHouse Keeper](https://clickhouse.com/docs/en/guides/sre/keeper/clickhouse-keeper)
