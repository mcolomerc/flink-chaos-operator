# Example: Network Chaos

Injects network degradation (latency, jitter, packet loss, bandwidth limits)
directly into a TaskManager pod's network interface using Linux `tc(8)` netem
and tbf running in an **ephemeral container**. Unlike NetworkPartition, the
connection is not severed — traffic is degraded to test Flink's tolerance of
slow or unreliable networks.

---

## Files

| File | Description |
|------|-------------|
| [`01-latency-tm-jm.yaml`](01-latency-tm-jm.yaml) | 200ms latency + 50ms jitter on TM ↔ JM traffic |
| [`02-packet-loss.yaml`](02-packet-loss.yaml) | 20% random packet loss on TM ↔ JM traffic |
| [`03-bandwidth-limit.yaml`](03-bandwidth-limit.yaml) | Throttle TM → external sink to 1 Mbit/s |
| [`04-dry-run.yaml`](04-dry-run.yaml) | Validate target resolution without injecting tc rules |

---

## Prerequisites

- The Flink Chaos Operator is installed. See [Quick Start](../../docs/guidelines/quick-start.md).
- Kubernetes **1.23+** with ephemeral containers enabled.
- The operator is configured with a `tcImage` that has `iproute2` (`tc`) installed.

### tc tool image

The default image `mcolomervv/flink-chaos-tc-tools:latest` is built from
`hack/Dockerfile.tc-tools` (alpine + iproute2) and published automatically
with every release. No manual setup required.

To override with a custom image:

```bash
helm upgrade fchaos ./charts/flink-chaos-operator \
  --set networkchaos.tcImage=<your-registry>/tc-tools:latest \
  -n <operator-namespace>
```

---

## How it works

When the run enters the **Injecting** phase, the operator adds an ephemeral
container to each selected TaskManager pod. The container runs a self-contained
`sh` script that:

1. Applies `tc qdisc add dev eth0 root netem ...` (latency/loss) or
   `tc qdisc add dev eth0 root tbf ...` (bandwidth)
2. Sleeps for the configured `duration`
3. Runs `tc qdisc del dev eth0 root` to clean up

The container then exits. The operator confirms cleanup via the `CleaningUp`
phase before finalising the verdict.

The injected container is recorded in `.status.ephemeralContainerInjections`.

---

## Step 1 — Verify the tc image is reachable

```bash
kubectl run tc-check --rm -it --image=<your-tc-image> --restart=Never -- tc -V
```

Expected: `tc utility, iproute2-X.XX.X`

---

## Step 2 — Dry run

```bash
kubectl apply -f 04-dry-run.yaml
kubectl get chaosrun netchaos-dryrun -n vvp-jobs
```

Expected: `Completed / Inconclusive`

```bash
kubectl delete chaosrun netchaos-dryrun -n vvp-jobs
```

---

## Step 3 — Run the experiment

Edit [`01-latency-tm-jm.yaml`](01-latency-tm-jm.yaml):
- Set `metadata.namespace`
- Adjust `spec.target.selector.matchLabels`
- Set `spec.safety.minTaskManagersRemaining: 0` for single-TM environments

```bash
kubectl apply -f 01-latency-tm-jm.yaml
```

---

## Step 4 — Monitor

### Watch phases

```bash
kubectl get chaosrun -n vvp-jobs -w
```

Expected progression:

```
NAME               PHASE       VERDICT   TARGET
netchaos-latency   Pending                           0s
netchaos-latency   Injecting                         1s
netchaos-latency   Observing                         2s   ← tc netem running
netchaos-latency   CleaningUp                       32s   ← 30s duration elapsed
netchaos-latency   Completed   Failed   pod-selector  35s
```

### Check ephemeral container status while active

```bash
TM_POD=$(kubectl get pods -n vvp-jobs -l component=taskmanager -o name | head -1)
kubectl get $TM_POD -n vvp-jobs \
  -o jsonpath='{.status.ephemeralContainerStatuses}' | python3 -m json.tool
```

While the tc rules are active the container state is `running`.
After `duration` it transitions to `terminated` (exit code 0).

### Check injections in the ChaosRun status

```bash
kubectl get chaosrun netchaos-latency -n vvp-jobs \
  -o jsonpath='{.status.ephemeralContainerInjections}' | python3 -m json.tool
```

```json
[
  {
    "containerName": "fchaos-tc-netchaos-latency-0",
    "injectedAt": "2026-04-02T11:52:36Z",
    "podName": "job-xxx-taskmanager-yyy"
  }
]
```

### Verify tc rules are active (during Observing phase)

```bash
TM_POD=job-xxx-taskmanager-yyy
kubectl exec -n vvp-jobs $TM_POD -c fchaos-tc-netchaos-latency-0 -- tc qdisc show dev eth0
```

Expected for latency:

```
qdisc netem 1: root refcnt 2 limit 1000 delay 200ms  50ms
```

### Check full status

```bash
kubectl get chaosrun netchaos-latency -n vvp-jobs \
  -o jsonpath='{.status}' | python3 -m json.tool
```

---

## Step 5 — Understand the verdict

| Verdict | Meaning |
|---------|---------|
| `Passed` | Job remained in RUNNING state throughout (requires Flink REST) |
| `Failed` | Recovery not confirmed within `observe.timeout` |
| `Inconclusive` | Injection succeeded but observation signals were insufficient |

> Without `observe.flinkRest.enabled=true`, the Kubernetes observer only checks
> pod readiness. Since the TM pod stays Running during network degradation (it is
> not killed), the verdict will almost always be `Failed` due to timeout.
> Configure Flink REST to get meaningful verdicts for network scenarios.

Add Flink REST observation:

```yaml
observe:
  enabled: true
  timeout: 3m
  pollInterval: 10s
  flinkRest:
    enabled: true
    endpoint: http://<jobmanager-service>.<namespace>.svc.cluster.local:8081
```

---

## Step 6 — Clean up

```bash
kubectl delete chaosrun netchaos-latency -n vvp-jobs
```

---

## Variations

### Combine latency and packet loss

```yaml
network:
  target: TMtoJM
  direction: Both
  latency: 100ms
  jitter: 30ms
  loss: 5         # 5% packet loss on top of latency
  duration: 60s
```

### Test TM-to-TM degradation

```yaml
network:
  target: TMtoTM
  direction: Both
  latency: 500ms
  duration: 60s
```

### Slow external sink simulation

```yaml
network:
  target: TMtoExternal
  direction: Egress
  bandwidth: 512kbit
  duration: 60s
  externalEndpoint:
    cidr: "10.200.0.0/16"
```

### Long-running latency (watch Flink timeout behaviour)

```yaml
network:
  target: TMtoJM
  direction: Both
  latency: 2000ms   # above Flink's default akka.ask.timeout=10s RPC timeout
  duration: 2m
safety:
  maxNetworkChaosDuration: 5m
```

---

## Troubleshooting

### Ephemeral container stuck in `ImagePullBackOff`

The `tcImage` cannot be pulled. Check:

```bash
kubectl describe pod <tm-pod> -n vvp-jobs | grep -A5 "fchaos-tc"
```

Ensure the image is accessible from within the cluster. For local Docker Desktop,
build the image locally — it is available to all pods without a registry push.

### Ephemeral container already exists (duplicate injection)

Kubernetes forbids modifying an ephemeral container once injected. If a previous
run left a stuck container with a different image, you must **restart the TM pod**:

```bash
kubectl delete pod <tm-pod> -n vvp-jobs
# The Deployment/Job controller will create a new pod
```

### tc rules not visible / no effect

Verify the tc container ran successfully:

```bash
kubectl get pod <tm-pod> -n vvp-jobs \
  -o jsonpath='{.status.ephemeralContainerStatuses[0].state}'
```

If it shows `terminated.exitCode: 1`, check the container logs:

```bash
kubectl logs <tm-pod> -n vvp-jobs -c fchaos-tc-<run-name>-0
```

Common cause: `eth0` is not the correct interface name. Check with:

```bash
kubectl exec -n vvp-jobs <tm-pod> -- ip link show
```
