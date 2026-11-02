# ClickHouse Keeper Component

This component deploys a 3-node ClickHouse Keeper cluster to provide
coordination and metadata management for ClickHouse replication. ClickHouse
Keeper is the modern replacement for ZooKeeper.

Our configuration provides high availability (survives 1 node failure) with pod
anti-affinity ensuring nodes are distributed across the cluster.

The ClickHouse Operator creates the PodDisruptionBudget for these pods, allowing
one down at a time. Do not add a second budget over them: Kubernetes refuses to
evict a pod under more than one, so neither would apply.

## Usage

Include in your overlay:

```yaml
# config/overlays/{env}/kustomization.yaml
components:
  - ../../components/clickhouse-keeper
  - ../../components/clickhouse-database
```

> [!IMPORTANT]
>
> Deploy Keeper before ClickHouse or update ClickHouse after Keeper is ready.

## Learn More

For detailed information about ClickHouse Keeper, including architecture,
configuration, monitoring, and troubleshooting:

- [ClickHouse Keeper
  Documentation](https://clickhouse.com/docs/en/guides/sre/keeper/clickhouse-keeper)
- [Altinity Kubernetes Operator - ClickHouse
  Keeper](https://docs.altinity.com/altinitykubernetesoperator/)
