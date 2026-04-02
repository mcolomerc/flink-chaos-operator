# Safety

The operator enforces multiple layers of safety checks **before** any disruptive action.

## Default Protections

| Check | Default | Description |
|-------|---------|-------------|
| Namespace denylist | `kube-system`, `kube-public`, `kube-node-lease` | System namespaces are never targeted. |
| Max concurrent runs | 1 per target | Prevents overlapping chaos runs on the same workload. |
| Min TaskManagers | 1 remaining | Ensures at least 1 TM pod survives injection. |
| Shared cluster guard | Blocked by default | Session-mode clusters rejected unless explicitly allowed. |
| Dry-run mode | Available | Preview impact without any destructive action. |

## Dry-Run Mode

Preview the impact without deleting pods or creating network rules:

```bash
kubectl fchaos run tm-kill \
  -n streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --count 1 \
  --dry-run
```

The run resolves the target, performs all safety checks, selects pods, and logs projected impact — but skips injection. Completes with verdict `Inconclusive`.

Via YAML:

```yaml
spec:
  safety:
    dryRun: true
```

## Minimum TaskManagers Remaining

Prevent runs that would reduce the TaskManager count below a threshold:

```yaml
spec:
  safety:
    minTaskManagersRemaining: 2
```

If the cluster has 3 TaskManagers and you request `count: 2` with this setting, the run is rejected.

## Concurrency Guard

At most one active chaos run per target by default. A second run on the same target is rejected while the first is in progress.

Override with:

```yaml
spec:
  safety:
    maxConcurrentRunsPerTarget: 2
```

## Shared Cluster Protection

When the resolver detects a session-mode cluster, the run is rejected unless explicitly permitted:

```yaml
spec:
  safety:
    allowSharedClusterImpact: true
```

## Duration Caps

Cap the maximum duration of time-bounded scenarios:

```yaml
spec:
  safety:
    maxNetworkChaosDuration: 5m       # NetworkPartition / NetworkChaos
    maxResourceExhaustionDuration: 5m  # ResourceExhaustion
```

## Checkpoint Storage Protection

Chaos targeting checkpoint storage (e.g., `TMtoCheckpoint`) is blocked by default due to data-loss risk:

```yaml
spec:
  safety:
    allowCheckpointStorageChaos: true  # explicit opt-in required
```
