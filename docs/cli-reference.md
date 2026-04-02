# CLI Reference

The `kubectl fchaos` plugin creates and manages chaos runs from the command line.

## Commands

### `kubectl fchaos run tm-kill`

Create and start a TaskManager pod kill chaos run.

| Flag | Default | Description |
|------|---------|-------------|
| `-n, --namespace` | — | Kubernetes namespace (required). |
| `--target-type` | — | `flinkdeployment`, `ververica`, or `podselector` (required). |
| `--target-name` | — | FlinkDeployment name. |
| `--deployment-id` | — | Ververica deployment UUID. |
| `--deployment-name` | — | Ververica deployment name. |
| `--vvp-namespace` | — | Ververica namespace for narrowing lookup. |
| `--count` | 1 | Number of TaskManager pods to kill (Random selection). |
| `--pod-names` | — | Explicit pod names to kill (overrides `--count`). |
| `--grace-period` | 0 | Grace period in seconds for pod termination. |
| `--dry-run` | false | Preview impact without deleting pods. |
| `--timeout` | 10m | Observation timeout. |
| `--min-task-managers` | 1 | Minimum TaskManagers that must remain. |
| `--allow-shared-cluster` | false | Allow targeting shared session clusters. |

```bash
# Kill 1 random TaskManager
kubectl fchaos run tm-kill -n streaming --target-type flinkdeployment --target-name orders-app --count 1

# Kill specific pods
kubectl fchaos run tm-kill -n streaming --target-type ververica \
  --deployment-name orders-processor \
  --pod-names orders-processor-tm-0 orders-processor-tm-1

# Dry-run preview
kubectl fchaos run tm-kill -n streaming --target-type flinkdeployment --target-name orders-app --count 1 --dry-run
```

---

### `kubectl fchaos run network-partition`

Create and start a network partition chaos run.

| Flag | Default | Description |
|------|---------|-------------|
| `-n, --namespace` | — | Kubernetes namespace (required). |
| `--target-type` | — | Target type (required). |
| `--target-name` | — | FlinkDeployment name. |
| `--network-target` | — | `TMtoTM`, `TMtoJM`, `TMtoCheckpoint`, or `TMtoExternal` (required). |
| `--direction` | `Both` | `Ingress`, `Egress`, or `Both`. |
| `--external-cidr` | — | CIDR range for external endpoint. |
| `--external-hostname` | — | Hostname for external endpoint. |
| `--external-port` | — | Port for external endpoint. |
| `--dry-run` | false | Preview without creating NetworkPolicy. |
| `--timeout` | 5m | Observation timeout. |

```bash
# Isolate TaskManagers from JobManager
kubectl fchaos run network-partition -n streaming \
  --target-type flinkdeployment --target-name my-app \
  --network-target TMtoJM --direction Both

# Isolate from external Kafka
kubectl fchaos run network-partition -n streaming \
  --target-type flinkdeployment --target-name my-app \
  --network-target TMtoExternal \
  --external-hostname kafka.example.com --external-port 9092
```

---

### `kubectl fchaos run network-chaos`

Create and start a network chaos run (latency, jitter, packet loss, bandwidth).

| Flag | Default | Description |
|------|---------|-------------|
| `-n, --namespace` | — | Kubernetes namespace (required). |
| `--target-type` | — | Target type (required). |
| `--target-name` | — | FlinkDeployment name. |
| `--network-target` | — | Network target (required). |
| `--direction` | `Both` | Traffic direction. |
| `--latency` | — | Base latency (e.g., `100ms`). |
| `--jitter` | — | Jitter range (e.g., `20ms`). |
| `--loss` | — | Packet loss percentage (0–100). |
| `--bandwidth` | — | Bandwidth limit (e.g., `10mbit`). |
| `--duration` | — | Chaos duration (required). |
| `--external-cidr` | — | CIDR range. |
| `--external-hostname` | — | Hostname. |
| `--external-port` | — | Port. |
| `--dry-run` | false | Preview without creating ephemeral container. |
| `--timeout` | 2m | Observation timeout. |
| `--max-network-chaos-duration` | 5m | Maximum allowed chaos duration. |

```bash
# 100ms latency with 5% packet loss
kubectl fchaos run network-chaos -n streaming \
  --target-type flinkdeployment --target-name my-app \
  --network-target TMtoJM --direction Egress \
  --latency 100ms --jitter 20ms --loss 5 --duration 60s

# Bandwidth throttle
kubectl fchaos run network-chaos -n streaming \
  --target-type flinkdeployment --target-name my-app \
  --network-target TMtoExternal \
  --external-hostname kafka.example.com --external-port 9092 \
  --bandwidth 10mbit --duration 30s
```

---

### `kubectl fchaos run resource-exhaustion`

Create and start a resource exhaustion chaos run.

| Flag | Default | Description |
|------|---------|-------------|
| `-n, --namespace` | — | Kubernetes namespace (required). |
| `--target-type` | — | Target type (required). |
| `--target-name` | — | FlinkDeployment name. |
| `--mode` | — | `CPU` or `Memory` (required). |
| `--workers` | 1 | Number of stress-ng worker processes. |
| `--memory-percent` | 80 | Memory percentage to consume (Memory mode only). |
| `--duration` | 60s | Stress duration. |
| `--dry-run` | false | Preview without injecting. |
| `--timeout` | 2m | Observation timeout. |

```bash
# CPU exhaustion
kubectl fchaos run resource-exhaustion -n streaming \
  --target-type flinkdeployment --target-name my-app \
  --mode CPU --workers 2 --duration 60s

# Memory exhaustion
kubectl fchaos run resource-exhaustion -n streaming \
  --target-type flinkdeployment --target-name my-app \
  --mode Memory --workers 1 --memory-percent 80 --duration 90s
```

---

### `kubectl fchaos status <name>`

Show the status of a chaos run.

| Flag | Description |
|------|-------------|
| `-n, --namespace` | Kubernetes namespace (required). |
| `-w, --watch` | Watch for live status updates. |

```bash
kubectl fchaos status orders-tm-kill-001 -n streaming
kubectl fchaos status orders-tm-kill-001 -n streaming --watch
```

---

### `kubectl fchaos list`

List all chaos runs in a namespace.

```bash
kubectl fchaos list -n streaming
```

Output:
```
NAME                       PHASE       VERDICT      TARGET       AGE
orders-tm-kill-001         Completed   Passed       orders-app   11m
orders-tm-kill-002         Observing   -            orders-app   2m
```

---

### `kubectl fchaos stop <name>`

Stop an in-progress chaos run by setting `spec.control.abort=true`.

```bash
kubectl fchaos stop orders-tm-kill-001 -n streaming
```
