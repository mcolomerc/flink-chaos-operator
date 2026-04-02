# Example: Resource Exhaustion

Injects CPU or memory stress into TaskManager pods using `stress-ng` running in
an **ephemeral container**. Tests how Flink behaves when TaskManagers are
resource-starved: delayed heartbeats, GC pressure, checkpoint latency, or
OOM risk.

The stress container runs for the configured `duration`, then exits cleanly.
The operator waits for cleanup before finalising the verdict.

---

## Files

| File | Description |
|------|-------------|
| [`01-cpu-stress.yaml`](01-cpu-stress.yaml) | Saturate 1 CPU core on each TM for 30 seconds |
| [`02-memory-stress.yaml`](02-memory-stress.yaml) | Consume 80% of each TM's memory limit for 30 seconds |
| [`03-dry-run.yaml`](03-dry-run.yaml) | Validate target resolution without stressing |

---

## Prerequisites

- The Flink Chaos Operator is installed. See [Quick Start](../../docs/guidelines/quick-start.md).
- Kubernetes **1.23+** with ephemeral containers enabled.
- The operator is configured with a `stressImage` that has `stress-ng` installed.

### stress-ng tool image

The default `ghcr.io/flink-chaos-operator/stress-tools:latest` requires the
image to be published. For local testing, build your own:

```bash
cat > Dockerfile.stress-tools <<'EOF'
FROM alpine:3.19
RUN apk add --no-cache stress-ng
EOF

docker build -t <your-registry>/stress-tools:latest -f Dockerfile.stress-tools .
docker push <your-registry>/stress-tools:latest
```

Configure the operator to use it:

```bash
helm upgrade fchaos ./charts/flink-chaos-operator \
  --set resourceExhaustion.stressImage=<your-registry>/stress-tools:latest \
  -n <operator-namespace>
```

Or in `values.yaml`:

```yaml
resourceExhaustion:
  stressImage: <your-registry>/stress-tools:latest
```

---

## How it works

When the run enters the **Injecting** phase, the operator adds an ephemeral
container to each selected TaskManager pod running:

- **CPU mode:** `stress-ng --cpu <workers> --timeout <duration>s`
- **Memory mode:** `stress-ng --vm <workers> --vm-bytes <percent>% --timeout <duration>s`

The container exits after `duration` seconds. The operator confirms cleanup
before finalising the verdict.

Injections are recorded in `.status.resourceExhaustionInjections`.

---

## Step 1 — Verify the stress image is reachable

```bash
kubectl run stress-check --rm -it \
  --image=<your-stress-image> --restart=Never -- stress-ng --version
```

Expected: `stress-ng, version X.XX.XX`

---

## Step 2 — Dry run

```bash
kubectl apply -f 03-dry-run.yaml
kubectl get chaosrun resexhaust-dryrun -n vvp-jobs
```

Expected: `Completed / Inconclusive`

```bash
kubectl delete chaosrun resexhaust-dryrun -n vvp-jobs
```

---

## Step 3 — Run CPU stress

Edit [`01-cpu-stress.yaml`](01-cpu-stress.yaml):
- Set `metadata.namespace`
- Adjust `spec.target.selector.matchLabels`
- Set `spec.safety.minTaskManagersRemaining: 0` for single-TM environments

```bash
kubectl apply -f 01-cpu-stress.yaml
```

---

## Step 4 — Monitor

### Watch phases

```bash
kubectl get chaosrun -n vvp-jobs -w
```

Expected progression:

```
NAME              PHASE       VERDICT   TARGET
resexhaust-cpu    Pending                           0s
resexhaust-cpu    Injecting                         1s
resexhaust-cpu    Observing                         2s   ← stress-ng running
resexhaust-cpu    CleaningUp                       32s   ← 30s duration elapsed
resexhaust-cpu    Completed   Failed   pod-selector  35s
```

### Check ephemeral container while stress is running

```bash
TM_POD=$(kubectl get pods -n vvp-jobs -l component=taskmanager -o name | head -1 | cut -d/ -f2)
kubectl get pod $TM_POD -n vvp-jobs \
  -o jsonpath='{.status.ephemeralContainerStatuses}' | python3 -m json.tool
```

While stress-ng runs the state is `running`. After `duration` it is `terminated`.

### Observe CPU usage while stress is running

```bash
kubectl top pod $TM_POD -n vvp-jobs
```

Expected: CPU usage spikes to the pod's CPU limit.

### Check resource exhaustion injections in status

```bash
kubectl get chaosrun resexhaust-cpu -n vvp-jobs \
  -o jsonpath='{.status.resourceExhaustionInjections}' | python3 -m json.tool
```

```json
[
  {
    "containerName": "fchaos-stress-resexhaust-cpu-0",
    "injectedAt": "2026-04-02T11:55:35Z",
    "podName": "job-xxx-taskmanager-yyy"
  }
]
```

### Check full final status

```bash
kubectl get chaosrun resexhaust-cpu -n vvp-jobs \
  -o jsonpath='{.status}' | python3 -m json.tool
```

---

## Step 5 — Understand the verdict

| Verdict | Meaning |
|---------|---------|
| `Passed` | TM pod stayed Running and Flink job returned to RUNNING (requires Flink REST) |
| `Failed` | Recovery not confirmed within `observe.timeout` |
| `Inconclusive` | Injection succeeded but observation was insufficient |

> Without Flink REST configured the Kubernetes observer only checks pod
> readiness. Since the TM stays Running under CPU/memory stress (unless it OOMKills),
> the verdict defaults to `Failed` due to timeout.

To get a meaningful verdict, add Flink REST:

```yaml
observe:
  enabled: true
  timeout: 3m
  flinkRest:
    enabled: true
    endpoint: http://<jobmanager-service>.<namespace>.svc.cluster.local:8081
```

---

## Step 6 — Clean up

```bash
kubectl delete chaosrun resexhaust-cpu -n vvp-jobs
```

---

## Variations

### Saturate all CPU cores

Set `workers` equal to the TM pod's CPU limit (e.g. `500m` → use `workers: 1`,
`2000m` → `workers: 2`):

```yaml
resourceExhaustion:
  mode: CPU
  workers: 2
  duration: 60s
```

### Memory pressure at a lower percentage (safer)

Start low to understand the effect before increasing:

```yaml
resourceExhaustion:
  mode: Memory
  workers: 1
  memoryPercent: 50   # 50% of pod's memory limit
  duration: 30s
```

### Longer stress to trigger GC pressure

Useful to observe checkpoint latency or backpressure:

```yaml
resourceExhaustion:
  mode: Memory
  workers: 1
  memoryPercent: 75
  duration: 3m
safety:
  maxResourceExhaustionDuration: 5m
```

### Combined with checkpoint stability gate

Wait for a stable checkpoint before injecting:

```yaml
observe:
  enabled: true
  timeout: 5m
  flinkRest:
    enabled: true
    endpoint: http://<jobmanager>:8081
    requireStableCheckpointBeforeInject: true
    checkpointStableWindowSeconds: 60
    checkpointWaitTimeout: 5m
```

---

## Troubleshooting

### Ephemeral container stuck in `ImagePullBackOff`

```bash
kubectl describe pod <tm-pod> -n vvp-jobs | grep -A5 "fchaos-stress"
```

Ensure `stressImage` is accessible from within the cluster. For local Docker
Desktop, build the image locally — it is available without a registry push.

### Ephemeral container already exists from a previous run

Kubernetes forbids changing an injected ephemeral container. Restart the TM pod:

```bash
kubectl delete pod <tm-pod> -n vvp-jobs
```

### stress-ng exits immediately (exit code ≠ 0)

Check the container logs:

```bash
kubectl logs <tm-pod> -n vvp-jobs -c fchaos-stress-<run-name>-0
```

Common cause: `--vm-bytes` is larger than the pod's cgroup memory limit.
Lower `memoryPercent` or increase the pod's memory limit.

### TM pod OOMKilled

`memoryPercent` is too high relative to the actual JVM heap usage. Lower it:

```yaml
resourceExhaustion:
  mode: Memory
  memoryPercent: 50   # reduce until stable
```

Monitor memory headroom before the test:

```bash
kubectl top pod -n vvp-jobs -l component=taskmanager
```
