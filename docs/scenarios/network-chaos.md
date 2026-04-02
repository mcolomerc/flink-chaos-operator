# Network Chaos Scenarios

Two network-level chaos scenarios are supported: **NetworkPartition** and **NetworkChaos**.

## NetworkPartition

Introduces binary network isolation between Flink components using Kubernetes NetworkPolicy resources. No special privileges required.

### What It Does

- Creates a NetworkPolicy that blocks traffic between specified component pairs.
- Automatically removes the NetworkPolicy during the CleaningUp phase.

### Network Targets

| Target | Description |
|--------|-------------|
| `TMtoTM` | TaskManager-to-TaskManager isolation. |
| `TMtoJM` | TaskManager-to-JobManager isolation. |
| `TMtoCheckpoint` | Blocks TaskManager access to checkpoint storage. |
| `TMtoExternal` | Blocks TaskManager access to external sinks/sources. |

### Traffic Directions

| Direction | Behavior |
|-----------|----------|
| `Ingress` | Block incoming traffic to target component. |
| `Egress` | Block outgoing traffic from source component. |
| `Both` | Block both directions (default). |

### External Endpoint

For `TMtoCheckpoint` and `TMtoExternal` targets:

```yaml
scenario:
  network:
    target: TMtoExternal
    direction: Both
    externalEndpoint:
      cidr: 10.0.0.0/8
      hostname: kafka.example.com
      port: 9092
```

> **Note:** `cidr` is required for NetworkPartition (hostname cannot be used in NetworkPolicy selectors).

### Example

```yaml
apiVersion: chaos.flink.io/v1alpha1
kind: ChaosRun
metadata:
  name: flink-network-partition-tmjm
  namespace: streaming
spec:
  target:
    type: FlinkDeployment
    name: my-flink-app
  scenario:
    type: NetworkPartition
    network:
      target: TMtoJM
      direction: Both
  observe:
    enabled: true
    timeout: 5m
```

### CLI

```bash
kubectl fchaos run network-partition \
  -n streaming \
  --target-type flinkdeployment \
  --target-name my-app \
  --network-target TMtoJM \
  --direction Both
```

---

## NetworkChaos

Injects latency, jitter, packet loss, or bandwidth limits using ephemeral containers with Linux `tc` (traffic control). Requires NET_ADMIN capability (granted only to the ephemeral container).

### What It Does

- Creates an ephemeral container in each selected TaskManager pod with NET_ADMIN.
- Applies `tc netem` (latency/jitter/loss) or `tc tbf` (bandwidth) rules.
- Automatically cleans up during the CleaningUp phase.

### Parameters

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `target` | string | Yes | Network target (see table above). |
| `direction` | string | Yes | `Ingress`, `Egress`, or `Both`. |
| `latency` | Duration | No | Base latency (e.g., `100ms`). |
| `jitter` | Duration | No | Jitter range around latency (e.g., `20ms`). Requires `latency`. |
| `loss` | int | No | Packet loss percentage (0–100). |
| `bandwidth` | string | No | Bandwidth limit (e.g., `10mbit`). Only valid with `TMtoExternal`. |
| `duration` | Duration | Yes | How long to apply impairment. Capped by `safety.maxNetworkChaosDuration`. |
| `externalEndpoint` | object | For TMtoCheckpoint/TMtoExternal | Endpoint specification. |

### Examples

**Add latency and packet loss:**

```yaml
apiVersion: chaos.flink.io/v1alpha1
kind: ChaosRun
metadata:
  name: flink-network-chaos-latency
  namespace: streaming
spec:
  target:
    type: FlinkDeployment
    name: my-flink-app
  scenario:
    type: NetworkChaos
    network:
      target: TMtoJM
      direction: Egress
      latency: 100ms
      jitter: 20ms
      loss: 5
      duration: 60s
  observe:
    enabled: true
    timeout: 2m
  safety:
    maxNetworkChaosDuration: 5m
```

**Bandwidth throttle to external sink:**

```yaml
scenario:
  type: NetworkChaos
  network:
    target: TMtoExternal
    direction: Egress
    bandwidth: 10mbit
    duration: 30s
    externalEndpoint:
      hostname: kafka.example.com
      port: 9092
```

### CLI

```bash
# Latency
kubectl fchaos run network-chaos \
  -n streaming \
  --target-type flinkdeployment \
  --target-name my-app \
  --network-target TMtoJM \
  --direction Egress \
  --latency 100ms \
  --jitter 20ms \
  --duration 60s

# Packet loss
kubectl fchaos run network-chaos \
  -n streaming \
  --target-type flinkdeployment \
  --target-name my-app \
  --network-target TMtoTM \
  --loss 5 \
  --duration 45s

# Bandwidth limit
kubectl fchaos run network-chaos \
  -n streaming \
  --target-type flinkdeployment \
  --target-name my-app \
  --network-target TMtoExternal \
  --external-hostname kafka.example.com \
  --external-port 9092 \
  --bandwidth 10mbit \
  --duration 30s
```

### Container Image

NetworkChaos requires a container image with `tc` tools. Configure via Helm or environment variable:

```bash
# Helm
helm install fchaos ./charts/flink-chaos-operator \
  --set networkchaos.tcImage=ghcr.io/flink-chaos-operator/tc-tools:v0.1.0

# Environment variable
export TC_TOOLS_IMAGE=ghcr.io/flink-chaos-operator/tc-tools:latest
```

### Kernel Requirements

- Linux kernel 4.10+ with `CONFIG_NET_SCH_NETEM` and `CONFIG_NET_SCH_TBF` enabled.
- Most Kubernetes nodes on major cloud providers meet this requirement.

---

## State Machine

Both network scenarios follow the extended lifecycle:

```
Pending → Validating → Injecting → Observing → CleaningUp → Completed/Aborted/Failed
```

The **CleaningUp** phase removes NetworkPolicy resources or ephemeral containers before finalizing.

## Verdict Rules

| Verdict | Condition |
|---------|-----------|
| `Passed` | Isolation/impairment applied and removed; recovery observed within timeout. |
| `Failed` | Resource creation failed, or recovery not observed within timeout. |
| `Inconclusive` | Isolation applied but observation signals are insufficient. |
