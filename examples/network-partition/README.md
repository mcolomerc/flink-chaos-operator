# Example: Network Partition

Simulates a complete network partition between Flink components by creating
Kubernetes `NetworkPolicy` resources that block all traffic between selected
pod pairs. After the configured `duration`, the policies are automatically
removed and Flink can reconnect.

**No ephemeral containers required** — only standard Kubernetes NetworkPolicy.
Requires the cluster's CNI plugin to enforce NetworkPolicy (Calico, Cilium,
Weave Net, etc.; does **not** work with the default Docker Desktop bridge network).

---

## Files

| File | Description |
|------|-------------|
| [`01-tm-to-jm-partition.yaml`](01-tm-to-jm-partition.yaml) | Block all TM ↔ JM traffic for 30 seconds |
| [`02-tm-to-tm-partition.yaml`](02-tm-to-tm-partition.yaml) | Isolate TaskManagers from each other (requires ≥ 2 TMs) |
| [`03-dry-run.yaml`](03-dry-run.yaml) | Validate target resolution without creating any policies |

---

## Prerequisites

- The Flink Chaos Operator is installed. See [Quick Start](../../docs/guidelines/quick-start.md).
- Your cluster's **CNI plugin enforces NetworkPolicy** (Calico, Cilium, Weave Net,
  or **kube-router**).
  > Docker Desktop's default `kindnet` CNI does **not** enforce NetworkPolicy.
  > To enable enforcement on Docker Desktop, install `kube-router`:
  > ```bash
  > kubectl apply -f https://raw.githubusercontent.com/cloudnativelabs/kube-router/master/daemonset/kubeadm-kuberouter-all-features.yaml
  > ```
  > This adds iptables-based NetworkPolicy enforcement alongside `kindnet`
  > without replacing the existing CNI. Verified working with Docker Desktop's
  > 6-node kind cluster (kube-router v2.8+).
- Flink pods have consistent labels. Check with:
  ```bash
  kubectl get pods -n vvp-jobs -l component=taskmanager --show-labels
  ```

---

## How it works

When the partition is injected the operator creates two `NetworkPolicy` objects
on the target pods (one for ingress, one for egress) with empty rule lists,
which denies all matching traffic:

```bash
kubectl get networkpolicy -n vvp-jobs
```

```
NAME                         POD-SELECTOR          AGE
netpart-tm-jm-netpol-egress   component=jobmanager   5s
netpart-tm-jm-netpol-ingress  component=jobmanager   5s
```

After the configured `duration` the policies are removed automatically, and
a `NetworkChaosCleanedUp` condition is set on the run.

---

## Step 1 — Dry run

Verify that your target resolves before injecting a real partition:

```bash
kubectl apply -f 03-dry-run.yaml
kubectl get chaosrun netpart-dryrun -n vvp-jobs
```

Expected:

```
NAME              PHASE       VERDICT
netpart-dryrun    Completed   Inconclusive
```

Clean up:

```bash
kubectl delete chaosrun netpart-dryrun -n vvp-jobs
```

---

## Step 2 — Run the partition

Edit [`01-tm-to-jm-partition.yaml`](01-tm-to-jm-partition.yaml):
- Set `metadata.namespace` to your Flink namespace
- Adjust `spec.target.selector.matchLabels` to match your TaskManager pods
- Set `spec.safety.minTaskManagersRemaining: 0` if you only have 1 TM

```bash
kubectl apply -f 01-tm-to-jm-partition.yaml
```

---

## Step 3 — Monitor

### Watch phase transitions

```bash
kubectl get chaosrun -n vvp-jobs -w
```

Expected progression:

```
NAME             PHASE       VERDICT   TARGET
netpart-tm-jm    Pending                          0s
netpart-tm-jm    Injecting                        1s
netpart-tm-jm    Observing                        2s   ← NetworkPolicies active
netpart-tm-jm    CleaningUp                      32s   ← 30s duration expired
netpart-tm-jm    Completed   Inconclusive   pod-selector  35s  ← policies removed
```

> `Inconclusive` is expected without a Flink REST endpoint configured — the
> Kubernetes observer cannot detect job-level reconnection for a partition scenario
> (TM pods stay Running). Add `observe.flinkRest` to get a `Passed` verdict.

### Verify NetworkPolicies while active

```bash
# During Observing phase:
kubectl get networkpolicy -n vvp-jobs
kubectl describe networkpolicy netpart-tm-jm-netpol-egress -n vvp-jobs
```

### Check full status

```bash
kubectl get chaosrun netpart-tm-jm -n vvp-jobs -o jsonpath='{.status}' \
  | python3 -m json.tool
```

Example output:

```json
{
  "conditions": [
    { "type": "TargetResolved",       "status": "True" },
    { "type": "SafetyChecksPassed",   "status": "True" },
    { "type": "InjectionCompleted",   "status": "True" },
    { "type": "NetworkChaosCleanedUp","status": "True",
      "message": "All network chaos resources removed" }
  ],
  "injectedPods": ["job-xxx-taskmanager-yyy"],
  "phase": "Completed",
  "verdict": "Inconclusive"
}
```

### Check operator logs

```bash
kubectl logs -n vvp-jobs -l app.kubernetes.io/name=flink-chaos-operator --tail=50
```

---

## Step 4 — Configure Flink REST for a meaningful verdict

Without a Flink REST endpoint the operator can only check pod readiness, which
stays `Running` during a partition. To get a `Passed` verdict add:

```yaml
observe:
  enabled: true
  timeout: 3m
  pollInterval: 10s
  flinkRest:
    enabled: true
    endpoint: http://<jobmanager-service>.<namespace>.svc.cluster.local:8081
```

The operator will poll the Flink REST API and confirm the job returns to
`RUNNING` state after the partition is lifted.

Find your JobManager service:

```bash
kubectl get svc -n vvp-jobs | grep jobmanager
```

---

## Step 5 — Clean up

```bash
kubectl delete chaosrun netpart-tm-jm -n vvp-jobs
```

---

## Variations

### Egress-only partition

Block only outgoing traffic from TaskManagers (TM can receive but not send):

```yaml
network:
  target: TMtoJM
  direction: Egress
  duration: 30s
```

### Longer partition to force job restart

Use a duration long enough for Flink's heartbeat timeout to trigger a restart
(typically `akka.ask.timeout`, default 10s, plus recovery time):

```yaml
network:
  target: TMtoJM
  direction: Both
  duration: 2m
```

### Block checkpoint storage access

Requires `safety.allowCheckpointStorageChaos: true`. Use a CIDR to identify
your checkpoint storage endpoint:

```yaml
scenario:
  type: NetworkPartition
  network:
    target: TMtoCheckpoint
    direction: Both
    duration: 60s
    externalEndpoint:
      cidr: "10.200.0.0/16"   # IP range of your storage service (S3, GCS, etc.)
safety:
  allowCheckpointStorageChaos: true
  allowSharedClusterImpact: true
```

---

## Troubleshooting

### NetworkPolicies created but traffic not blocked

Your CNI plugin does not enforce NetworkPolicy. Check:

```bash
kubectl get pods -n kube-system | grep -i "calico\|cilium\|kube-router"
```

If none are present, install a NetworkPolicy-enforcing CNI. For Docker Desktop:

```bash
kubectl apply -f https://raw.githubusercontent.com/cloudnativelabs/kube-router/master/daemonset/kubeadm-kuberouter-all-features.yaml
kubectl get pods -n kube-system -l app=kube-router   # wait for Running
```

### Run stuck in CleaningUp

The operator could not delete the NetworkPolicies. Check RBAC:

```bash
kubectl auth can-i delete networkpolicies -n vvp-jobs \
  --as=system:serviceaccount:vvp-jobs:fchaos-flink-chaos-operator
```

### Safety check: allowSharedClusterImpact required

NetworkPartition always requires this flag because NetworkPolicy is namespace-wide:

```yaml
safety:
  allowSharedClusterImpact: true
```
