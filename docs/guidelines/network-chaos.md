# Guideline: Network Chaos

End-to-end usage guide for `NetworkPartition` and `NetworkChaos` scenarios. Covers common network failure patterns and how to validate Flink's resilience to them.

**Prerequisites**: Complete the [Quick Start](quick-start.md) guide. Network scenarios require the `CleaningUp` phase to complete before a run finalises.

---

## Scenario Overview

| Scenario | Mechanism | Use Case |
|----------|-----------|----------|
| `NetworkPartition` | Kubernetes NetworkPolicy | Binary traffic block — test complete isolation between Flink components |
| `NetworkChaos` | Ephemeral container + `tc netem/tbf` | Gradual degradation — latency, jitter, packet loss, bandwidth limits |

Both scenarios follow the extended lifecycle:
```
Pending → Injecting → Observing → CleaningUp → Completed/Failed
```

The operator automatically removes all created resources (NetworkPolicies, ephemeral containers) during `CleaningUp`.

---

## Network Targets Reference

| Target | Blocks traffic between... |
|--------|--------------------------|
| `TMtoTM` | TaskManager ↔ TaskManager |
| `TMtoJM` | TaskManager ↔ JobManager |
| `TMtoCheckpoint` | TaskManager ↔ checkpoint storage (requires `allowCheckpointStorageChaos=true`) |
| `TMtoExternal` | TaskManager ↔ external sink/source (Kafka, S3, etc.) |

---

## Part 1: NetworkPartition

### Scenario 1: Isolate TaskManagers from the JobManager

Tests whether Flink detects the loss of the JobManager heartbeat and attempts a failover.

```bash
kubectl fchaos run network-partition \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --network-target TMtoJM \
  --direction Both
```

**What happens:**
1. A NetworkPolicy is created that denies all ingress to JobManager pods.
2. TaskManagers lose heartbeat with the JobManager.
3. Flink triggers a failover / leader election (if HA is configured).
4. After the observation window, the NetworkPolicy is removed.
5. Flink reconnects and the job resumes.

**Expected verdict**: `Passed` if Flink HA is configured. `Failed` or `Inconclusive` if the job does not recover within the timeout.

---

### Scenario 2: Isolate TaskManagers from Each Other

Tests resilience to inter-TM communication loss (e.g. network split between nodes).

```bash
kubectl fchaos run network-partition \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --network-target TMtoTM \
  --direction Both \
  --observe-timeout 5m
```

---

### Scenario 3: Block Access to External Sink (Kafka)

Simulates a network partition between TaskManagers and an external Kafka cluster.

```bash
kubectl fchaos run network-partition \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --network-target TMtoExternal \
  --external-cidr 10.10.0.0/16 \
  --external-port 9092 \
  --direction Egress
```

> **Note**: `--external-cidr` is required for NetworkPartition when using `TMtoExternal` or `TMtoCheckpoint`. Hostnames cannot be used in Kubernetes NetworkPolicy selectors.

---

### Scenario 4: Block Access to Checkpoint Storage

Tests whether your job handles checkpoint storage failures without data loss.

> **Warning**: This can cause data loss if Flink is mid-checkpoint. Requires explicit opt-in.

```yaml
apiVersion: chaos.flink.io/v1alpha1
kind: ChaosRun
metadata:
  name: orders-checkpoint-partition
  namespace: streaming
spec:
  target:
    type: FlinkDeployment
    name: orders-app
  scenario:
    type: NetworkPartition
    network:
      target: TMtoCheckpoint
      direction: Both
      externalEndpoint:
        cidr: 10.20.0.0/16   # S3 endpoint or NFS server CIDR
  safety:
    allowCheckpointStorageChaos: true   # explicit opt-in required
  observe:
    enabled: true
    timeout: 5m
```

---

## Part 2: NetworkChaos

### Scenario 5: Add Latency Between TaskManagers and JobManager

Tests Flink's behaviour under network degradation — does the job slow down gracefully or fail?

```bash
kubectl fchaos run network-chaos \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --network-target TMtoJM \
  --direction Both \
  --latency 200ms \
  --jitter 50ms \
  --duration 60s \
  --observe-timeout 3m
```

**What happens:**
1. An ephemeral container with `NET_ADMIN` is injected into each TM pod.
2. `tc netem delay 200ms 50ms` is applied to the TM's network interface.
3. After 60 seconds, the tc rule is removed and the ephemeral container exits.
4. The operator monitors recovery.

---

### Scenario 6: Introduce Packet Loss Between TaskManagers

Simulates unreliable network conditions between TaskManagers.

```bash
kubectl fchaos run network-chaos \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --network-target TMtoTM \
  --direction Both \
  --loss 10 \
  --duration 45s
```

Combine latency and packet loss to simulate severe degradation:

```bash
kubectl fchaos run network-chaos \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --network-target TMtoTM \
  --direction Both \
  --latency 100ms \
  --jitter 30ms \
  --loss 5 \
  --duration 60s
```

---

### Scenario 7: Throttle Bandwidth to External Kafka

Tests how Flink handles a saturated external sink — does backpressure propagate correctly?

```bash
kubectl fchaos run network-chaos \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --network-target TMtoExternal \
  --direction Egress \
  --bandwidth 1mbit \
  --duration 90s \
  --external-hostname kafka.example.com \
  --external-port 9092
```

> **Note**: `--bandwidth` is only valid with `TMtoExternal`. It uses `tc tbf` (token bucket filter) to enforce the rate limit.

---

### Scenario 8: Progressive Degradation Test

Run multiple NetworkChaos experiments at increasing severity to find your resilience threshold.

```bash
# Step 1: light latency — should not impact Flink
kubectl fchaos run network-chaos -n streaming \
  --target-type flinkdeployment --target-name orders-app \
  --network-target TMtoJM --direction Both \
  --latency 50ms --duration 60s

# Wait for completion
kubectl fchaos list -n streaming

# Step 2: moderate latency — may trigger heartbeat warnings
kubectl fchaos run network-chaos -n streaming \
  --target-type flinkdeployment --target-name orders-app \
  --network-target TMtoJM --direction Both \
  --latency 500ms --duration 60s

# Step 3: severe latency + loss — likely to trigger failover
kubectl fchaos run network-chaos -n streaming \
  --target-type flinkdeployment --target-name orders-app \
  --network-target TMtoJM --direction Both \
  --latency 1s --loss 20 --duration 60s
```

---

## Monitoring During Network Chaos

### Watch the run

```bash
kubectl fchaos status <name> -n streaming --watch
```

### Monitor Flink job metrics

Port-forward the Flink REST API and check job health:

```bash
kubectl port-forward svc/orders-app-rest 8081:8081 -n streaming &
curl http://localhost:8081/v1/jobs
curl http://localhost:8081/v1/overview
```

### Check TaskManager heartbeat timeout

If the TM loses contact with the JM for longer than `heartbeat.timeout` (default 50s in Flink), the JM will consider the TM lost and trigger a failover. You should see this in the Flink logs:

```bash
kubectl logs -n streaming -l app=orders-app,component=jobmanager --tail=100 | grep -i "heartbeat\|lost\|failover"
```

---

## Stopping a Run Early

If you need to stop an in-progress network chaos run before it completes:

```bash
kubectl fchaos stop <name> -n streaming
```

The operator routes the abort through the `CleaningUp` phase to ensure all NetworkPolicies and ephemeral containers are removed before the run terminates.

---

## Cleanup Verification

After a run completes, verify no orphaned resources remain:

```bash
# No NetworkPolicies left by this run
kubectl get networkpolicies -n streaming \
  -l chaos.flink.io/managed-by=flink-chaos-operator

# No ephemeral containers in TM pods (check pod spec)
kubectl get pods -n streaming -l app=orders-app,component=taskmanager \
  -o jsonpath='{range .items[*]}{.metadata.name}: {range .spec.ephemeralContainers[*]}{.name}{"\n"}{end}{end}'
```

---

## Common Issues

### Run stuck in CleaningUp

The operator is waiting for cleanup to confirm. Check the operator logs:

```bash
kubectl logs -n streaming -l app.kubernetes.io/name=flink-chaos-operator | grep -i "cleanup\|cleaningup"
```

If a TM pod was deleted during cleanup (e.g. by an autoscaler), the operator marks the injection as cleaned up and continues.

### NET_ADMIN capability denied

NetworkChaos requires the ephemeral container to have `NET_ADMIN`. If your cluster has a restrictive PodSecurityPolicy or OPA/Gatekeeper policy blocking this capability, the injection will fail.

Check:
```bash
kubectl describe chaosrun <name> -n streaming | grep -A3 "Injection"
```

---

## Next Steps

- [Resource Exhaustion](resource-exhaustion.md) — test CPU and memory pressure on TaskManagers
- [Safety Configuration](../safety.md) — configure `maxNetworkChaosDuration` and checkpoint storage guards
- [Metrics](../metrics.md) — monitor chaos run outcomes with Prometheus
