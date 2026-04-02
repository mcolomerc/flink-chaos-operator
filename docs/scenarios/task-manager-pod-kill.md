# Scenario: TaskManagerPodKill

Deletes one or more TaskManager pods and monitors whether the Flink job recovers.

## What It Does

1. Selects TaskManager pods (randomly or by explicit name).
2. Deletes them via the Kubernetes API.
3. Monitors for replacement pods and Flink job recovery.

## Selection Modes

### Random

```yaml
scenario:
  type: TaskManagerPodKill
  selection:
    mode: Random
    count: 2
```

### Explicit

```yaml
scenario:
  type: TaskManagerPodKill
  selection:
    mode: Explicit
    podNames:
      - my-flink-app-taskmanager-0
      - my-flink-app-taskmanager-2
```

Both pods must exist in the resolved target. If a pod doesn't exist, the run fails.

## Action

```yaml
scenario:
  action:
    type: DeletePod
    gracePeriodSeconds: 0  # 0 = immediate termination (default)
```

## Verdict Rules

| Verdict | Condition |
|---------|-----------|
| `Passed` | Pod injection succeeded and at least one replacement TaskManager was observed running. If Flink REST was enabled, the job returned to a healthy state within the timeout. |
| `Failed` | Target resolution failed, safety checks rejected, pod deletion failed, or recovery was not observed before timeout. |
| `Inconclusive` | Pods were injected but available observation signals are insufficient to confirm recovery (e.g., Flink REST unavailable). |

## Example

```yaml
apiVersion: chaos.flink.io/v1alpha1
kind: ChaosRun
metadata:
  name: orders-tm-kill
  namespace: streaming
spec:
  target:
    type: FlinkDeployment
    name: orders-app
  scenario:
    type: TaskManagerPodKill
    selection:
      mode: Random
      count: 1
    action:
      type: DeletePod
      gracePeriodSeconds: 0
  observe:
    enabled: true
    timeout: 10m
    pollInterval: 5s
  safety:
    minTaskManagersRemaining: 1
```
