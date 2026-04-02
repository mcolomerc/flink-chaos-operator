# Scenario: ResourceExhaustion

Exhausts CPU or memory on TaskManager pods using `stress-ng` in ephemeral containers. Tests how Flink workloads respond to resource contention.

## What It Does

1. Creates an ephemeral container in each selected TaskManager pod.
2. Runs `stress-ng` for the configured duration.
3. Monitors job recovery during the stress period.
4. Automatically terminates after `duration` expires.

## Modes

### CPU Mode

```yaml
scenario:
  type: ResourceExhaustion
  resourceExhaustion:
    mode: CPU
    workers: 2
    duration: 60s
```

Runs `stress-ng --cpu N --timeout Xs` where `N` is the number of workers.

### Memory Mode

```yaml
scenario:
  type: ResourceExhaustion
  resourceExhaustion:
    mode: Memory
    workers: 1
    memoryPercent: 80
    duration: 90s
```

Runs `stress-ng --vm N --vm-bytes P% --timeout Xs` where `P` is `memoryPercent`.

## Parameters

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `mode` | string | — | Required. `CPU` or `Memory`. |
| `workers` | int | 1 | Number of stress-ng worker processes. Min 1. |
| `memoryPercent` | int | 80 | Percentage of pod memory limit to consume. Memory mode only. Range 1–100. |
| `duration` | Duration | 60s | How long to apply stress. Capped by `safety.maxResourceExhaustionDuration`. |

## Safety

```yaml
safety:
  maxResourceExhaustionDuration: 5m  # default cap
```

Runs requesting a longer duration are rejected before injection.

## Example

```yaml
apiVersion: chaos.flink.io/v1alpha1
kind: ChaosRun
metadata:
  name: flink-cpu-exhaustion
  namespace: streaming
spec:
  target:
    type: FlinkDeployment
    name: my-flink-app
  scenario:
    type: ResourceExhaustion
    resourceExhaustion:
      mode: CPU
      workers: 2
      duration: 60s
  observe:
    enabled: true
    timeout: 2m
    pollInterval: 5s
  safety:
    maxResourceExhaustionDuration: 5m
```

## CLI

```bash
kubectl fchaos run resource-exhaustion \
  -n streaming \
  --target-type flinkdeployment \
  --target-name my-app \
  --mode CPU \
  --workers 2 \
  --duration 60s

# Memory mode
kubectl fchaos run resource-exhaustion \
  -n streaming \
  --target-type flinkdeployment \
  --target-name my-app \
  --mode Memory \
  --workers 1 \
  --memory-percent 80 \
  --duration 90s
```

## Container Image

Configure via Helm or environment variable:

```bash
# Helm
helm install fchaos ./charts/flink-chaos-operator \
  --set resourceExhaustion.stressImage=ghcr.io/flink-chaos-operator/stress-tools:v0.1.0

# Environment variable
export STRESS_IMAGE=ghcr.io/flink-chaos-operator/stress-tools:latest
```

## Verdict Rules

| Verdict | Condition |
|---------|-----------|
| `Passed` | Stress applied and removed; job returned to healthy state within timeout. |
| `Failed` | Ephemeral container creation failed, or recovery not observed within timeout. |
| `Inconclusive` | Stress applied but observation signals insufficient to confirm recovery. |
