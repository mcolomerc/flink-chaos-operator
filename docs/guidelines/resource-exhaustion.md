# Guideline: Resource Exhaustion

End-to-end usage guide for the `ResourceExhaustion` scenario. Tests how Flink workloads behave under CPU or memory pressure on TaskManager pods.

**Prerequisites**: Complete the [Quick Start](quick-start.md) guide.

---

## Scenario Overview

`ResourceExhaustion` uses `stress-ng` in ephemeral containers to saturate CPU or memory on selected TaskManager pods. It tests:

- Whether Flink backpressure mechanisms engage under CPU saturation.
- Whether the job fails or degrades gracefully under memory pressure.
- How long recovery takes after the stress is removed.

The ephemeral container exits after the configured `duration`. The operator then monitors recovery.

---

## Container Image Requirement

ResourceExhaustion requires a container image with `stress-ng` installed. Verify the image is configured:

```bash
kubectl get deployment -n streaming -l app.kubernetes.io/name=flink-chaos-operator \
  -o jsonpath='{.items[0].spec.template.spec.containers[0].env[?(@.name=="STRESS_IMAGE")].value}'
```

Expected: `ghcr.io/flink-chaos-operator/stress-tools:latest` (or your custom image).

To override:

```bash
helm upgrade fchaos ./charts/flink-chaos-operator \
  --namespace streaming \
  --set resourceExhaustion.stressImage=your-registry/stress-tools:v1.0.0
```

---

## Part 1: CPU Exhaustion

### Scenario 1: Single Worker CPU Stress

The baseline CPU test. Runs one `stress-ng --cpu` worker per selected TM pod.

```bash
kubectl fchaos run resource-exhaustion \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --mode CPU \
  --workers 1 \
  --duration 60s
```

**What happens:**
1. An ephemeral container is injected into each selected TM pod.
2. `stress-ng --cpu 1 --timeout 60s` runs inside the container, consuming CPU.
3. The TM pod experiences CPU contention with the stress process.
4. After 60 seconds, stress-ng exits and the ephemeral container terminates.
5. The operator monitors job recovery.

**What to observe:**
- Flink task processing rate should decrease during stress.
- Backpressure should propagate upstream.
- After cleanup, processing rate should recover.

---

### Scenario 2: Saturate All CPU Cores

Simulate a CPU-starved node by matching the number of workers to the pod's CPU limit.

```yaml
# First check your TM pod's CPU limit
kubectl get pods -n streaming -l app=orders-app,component=taskmanager \
  -o jsonpath='{.items[0].spec.containers[0].resources.limits.cpu}'
# e.g. output: "2" (2 CPU cores)
```

```bash
kubectl fchaos run resource-exhaustion \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --mode CPU \
  --workers 4 \
  --duration 60s \
  --observe-timeout 3m
```

> **Note**: `workers` in excess of the pod's CPU limit will still run but contention is bounded by the cgroup limit — useful for testing OOM-killer behaviour when combined with memory flags.

---

### Scenario 3: CPU Stress with Flink REST Monitoring

Combine CPU exhaustion with Flink REST observation to capture job state during stress.

```yaml
apiVersion: chaos.flink.io/v1alpha1
kind: ChaosRun
metadata:
  name: orders-cpu-stress
  namespace: streaming
spec:
  target:
    type: FlinkDeployment
    name: orders-app
  scenario:
    type: ResourceExhaustion
    resourceExhaustion:
      mode: CPU
      workers: 2
      duration: 90s
  observe:
    enabled: true
    timeout: 3m
    pollInterval: 5s
    flinkRest:
      enabled: true
      endpoint: "http://orders-app-rest.streaming.svc.cluster.local:8081"
  safety:
    maxResourceExhaustionDuration: 5m
```

```bash
kubectl apply -f orders-cpu-stress.yaml
kubectl fchaos status orders-cpu-stress -n streaming --watch
```

---

## Part 2: Memory Exhaustion

### Scenario 4: Moderate Memory Pressure

Consumes 50% of the pod's memory limit, leaving headroom for the JVM.

```bash
kubectl fchaos run resource-exhaustion \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --mode Memory \
  --workers 1 \
  --memory-percent 50 \
  --duration 60s
```

**What to observe:**
- JVM GC pressure may increase.
- Flink task throughput may drop.
- The job should recover after stress is removed.

---

### Scenario 5: High Memory Pressure

Tests OOM-adjacent conditions. Flink's off-heap memory management means it may survive high JVM pressure if configured correctly.

```bash
kubectl fchaos run resource-exhaustion \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --mode Memory \
  --workers 2 \
  --memory-percent 80 \
  --duration 45s \
  --observe-timeout 3m
```

> **Warning**: 80%+ memory consumption may trigger the Linux OOM killer, which will kill the TM pod. This is intentional for some tests — if you want to avoid pod loss, keep `memory-percent` below 70%.

---

### Scenario 6: Memory Exhaustion with Safety Cap

Always set `maxResourceExhaustionDuration` when running memory tests to avoid runaway stress sessions.

```yaml
apiVersion: chaos.flink.io/v1alpha1
kind: ChaosRun
metadata:
  name: orders-memory-stress
  namespace: streaming
spec:
  target:
    type: FlinkDeployment
    name: orders-app
  scenario:
    type: ResourceExhaustion
    resourceExhaustion:
      mode: Memory
      workers: 1
      memoryPercent: 75
      duration: 60s
  observe:
    enabled: true
    timeout: 5m
  safety:
    maxResourceExhaustionDuration: 2m   # operator enforces this cap
    minTaskManagersRemaining: 1
```

---

## Scenario 7: Dry Run — Preview What Would Be Injected

Before running a real stress test, use dry run to confirm the target and injection plan:

```bash
kubectl fchaos run resource-exhaustion \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --mode CPU \
  --workers 2 \
  --duration 60s \
  --dry-run
```

Inspect the preview:

```bash
kubectl get chaosrun <name> -n streaming \
  -o jsonpath='{.status.dryRunPreview}'
```

---

## Monitoring During Resource Exhaustion

### Watch pod resource usage in real time

```bash
# CPU and memory usage per TM pod
kubectl top pods -n streaming -l app=orders-app,component=taskmanager --containers
```

### Watch Flink job metrics

```bash
kubectl port-forward svc/orders-app-rest 8081:8081 -n streaming &
# Check backpressure
curl http://localhost:8081/v1/jobs/<job-id>/vertices/<vertex-id>/backpressure
# Check job status
curl http://localhost:8081/v1/jobs/overview
```

### Check ephemeral container status

```bash
kubectl get pod <tm-pod-name> -n streaming \
  -o jsonpath='{.status.ephemeralContainerStatuses}' | jq .
```

You should see the stress container in `running` state during injection and `terminated` after the duration expires.

---

## Interpreting Results

### Verdict: `Passed`

The stress injection ran for the full duration, the ephemeral container exited, and the Flink job returned to a healthy state within the observation timeout.

### Verdict: `Failed`

One of:
- Ephemeral container creation failed (check kernel and image).
- The TM pod was OOM-killed and did not recover within the timeout.
- The Flink job did not return to RUNNING within the observation timeout.

```bash
kubectl describe chaosrun <name> -n streaming
kubectl get events -n streaming --sort-by='.lastTimestamp' | tail -20
```

### Verdict: `Inconclusive`

The stress ran but observation signals were insufficient. Usually means Flink REST was not reachable. The job may actually have recovered — check manually:

```bash
curl http://localhost:8081/v1/jobs
```

---

## Common Issues

### `stress-ng: command not found`

The `STRESS_IMAGE` does not have `stress-ng` installed. Build a custom image:

```dockerfile
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y stress-ng && rm -rf /var/lib/apt/lists/*
```

```bash
docker build -t your-registry/stress-tools:latest .
docker push your-registry/stress-tools:latest

helm upgrade fchaos ./charts/flink-chaos-operator \
  --namespace streaming \
  --set resourceExhaustion.stressImage=your-registry/stress-tools:latest
```

### Ephemeral container creation failed

Kubernetes 1.23+ is required for ephemeral containers. Check your cluster version:

```bash
kubectl version --short
```

Also check for PodSecurityPolicy or OPA/Gatekeeper policies that restrict ephemeral containers.

### Run exceeds `maxResourceExhaustionDuration`

The operator rejects runs where `duration > maxResourceExhaustionDuration`. Increase the cap in `spec.safety` or the Helm chart defaults:

```bash
helm upgrade fchaos ./charts/flink-chaos-operator \
  --namespace streaming \
  --set defaults.safety.maxResourceExhaustionDuration=10m
```

---

## Next Steps

- [Network Chaos](network-chaos.md) — combine network and resource failures
- [Safety Configuration](../safety.md) — configure duration caps and guardrails
- [Metrics](../metrics.md) — track run outcomes with Prometheus
