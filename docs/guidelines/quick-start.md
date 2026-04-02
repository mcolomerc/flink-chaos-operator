# Quick Start Guide

This guide walks you through setting up the Flink Chaos Operator from scratch and running your first end-to-end chaos experiment against a Flink workload.

---

## Prerequisites

| Requirement | Version | Notes |
|-------------|---------|-------|
| Kubernetes cluster | 1.25+ | minikube, kind, GKE, EKS, AKS, or on-prem |
| kubectl | any recent | configured to access your cluster |
| Helm | 3.x | for operator installation |
| Apache Flink Kubernetes Operator | 1.6+ | managing your Flink workloads |

---

## Step 1 — Install the Flink Chaos Operator

### 1a. Choose a target namespace

All chaos runs are namespace-scoped. Install the operator into the same namespace as your Flink workloads, or into a dedicated operations namespace.

```bash
export CHAOS_NS=streaming
kubectl create namespace $CHAOS_NS --dry-run=client -o yaml | kubectl apply -f -
```

### 1b. Install with Helm

```bash
helm install fchaos ./charts/flink-chaos-operator \
  --namespace $CHAOS_NS \
  --set watchNamespace=$CHAOS_NS
```

> **Tip**: Set `watchNamespace` to the namespace that contains your Flink workloads. Leave it empty only if you need the operator to watch all namespaces (requires ClusterRole).

### 1c. Verify the operator is running

```bash
kubectl get pods -n $CHAOS_NS -l app.kubernetes.io/name=flink-chaos-operator
```

Expected output:

```
NAME                                    READY   STATUS    RESTARTS   AGE
fchaos-flink-chaos-operator-xxx-yyy     1/1     Running   0          30s
```

### 1d. Verify the CRD is installed

```bash
kubectl get crd chaosruns.chaos.flink.io
```

---

## Step 2 — Install the kubectl Plugin

The `kubectl fchaos` plugin provides a human-friendly interface for creating, monitoring, and stopping chaos runs.

```bash
# Build from source
make build-cli

# Install to PATH
sudo cp bin/kubectl-fchaos /usr/local/bin/

# Verify
kubectl fchaos --help
```

Expected output:

```
kubectl plugin for the Flink Chaos Operator.

Usage:
  kubectl fchaos [command]

Available Commands:
  run         Run a chaos scenario against a Flink workload
  status      Show the status of a ChaosRun
  list        List ChaosRuns in a namespace
  stop        Stop an in-progress ChaosRun
```

---

## Step 3 — Prepare a Flink Workload

You need a running Flink workload to target. If you already have one, skip to [Step 4](#step-4--run-a-dry-run-first).

### Example: minimal FlinkDeployment

```yaml
# flink-app.yaml
apiVersion: flink.apache.org/v1beta1
kind: FlinkDeployment
metadata:
  name: orders-app
  namespace: streaming
spec:
  image: flink:1.17
  flinkVersion: v1_17
  serviceAccount: flink
  jobManager:
    replicas: 1
    resource:
      memory: "1024m"
      cpu: 0.5
  taskManager:
    replicas: 3
    resource:
      memory: "1024m"
      cpu: 0.5
  job:
    jarURI: local:///opt/flink/examples/streaming/StateMachineExample.jar
    parallelism: 3
    upgradeMode: stateless
```

```bash
kubectl apply -f flink-app.yaml
```

Wait for all TaskManagers to be ready:

```bash
kubectl wait pod \
  -l app=orders-app,component=taskmanager \
  --for=condition=Ready \
  --timeout=120s \
  -n streaming
```

---

## Step 4 — Run a Dry Run First

Always do a dry run before injecting real failures. A dry run resolves the target, runs all safety checks, and shows what would happen — without deleting any pods.

```bash
kubectl fchaos run tm-kill \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --count 1 \
  --dry-run
```

Check the result:

```bash
kubectl fchaos list -n streaming
```

Expected output:

```
NAME              PHASE       VERDICT        TARGET       AGE
tm-kill-xxx       Completed   Inconclusive   orders-app   5s
```

`Inconclusive` is the expected verdict for dry runs — it means no injection occurred, but all checks passed.

Inspect the dry-run preview:

```bash
kubectl get chaosrun tm-kill-xxx -n streaming -o jsonpath='{.status.dryRunPreview}'
```

---

## Step 5 — Run Your First Real Experiment

Kill one random TaskManager and observe recovery:

```bash
kubectl fchaos run tm-kill \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --count 1 \
  --observe-timeout 10m
```

The command prints the created ChaosRun name:

```
ChaosRun "tm-kill-1711234567" created in namespace "streaming"
```

---

## Step 6 — Monitor Progress

### Watch live status

```bash
kubectl fchaos status tm-kill-1711234567 -n streaming --watch
```

You will see the phase progress through:

```
Phase: Injecting   → pods being deleted
Phase: Observing   → waiting for replacement pods
Phase: Completed   Verdict: Passed
```

### Check Kubernetes events

```bash
kubectl get events -n streaming --field-selector involvedObject.name=tm-kill-1711234567
```

### Check operator logs

```bash
kubectl logs -n streaming -l app.kubernetes.io/name=flink-chaos-operator --tail=50
```

---

## Step 7 — Understand the Verdict

| Verdict | Meaning |
|---------|---------|
| `Passed` | Flink recovered within the observation timeout. Replacement pods came up and the job returned to RUNNING. |
| `Failed` | Recovery was not observed before the timeout expired. Check Flink logs. |
| `Inconclusive` | Injection succeeded but observation signals were insufficient to confirm recovery (e.g. Flink REST was unavailable). |

---

## Step 8 — List and Clean Up

List all runs:

```bash
kubectl fchaos list -n streaming
```

Delete completed runs you no longer need:

```bash
kubectl delete chaosrun tm-kill-1711234567 -n streaming
```

Delete all completed runs in the namespace:

```bash
kubectl delete chaosrun -n streaming --field-selector status.phase=Completed
```

---

## Common Setup Issues

### Operator pod is not starting

```bash
kubectl describe pod -n $CHAOS_NS -l app.kubernetes.io/name=flink-chaos-operator
kubectl logs -n $CHAOS_NS -l app.kubernetes.io/name=flink-chaos-operator
```

Check that the `watchNamespace` value matches a namespace that exists in your cluster.

### ChaosRun stuck in Pending

The controller has not yet processed the run. Verify the operator is watching the correct namespace:

```bash
kubectl get chaosrun <name> -n streaming -o jsonpath='{.status.message}'
```

### Safety check rejection

```bash
kubectl describe chaosrun <name> -n streaming
```

Look for conditions like `SafetyChecksPassed=False`. Common causes:
- Another run is already active on the same target (`maxConcurrentRunsPerTarget=1`).
- Killing the requested number of pods would drop below `minTaskManagersRemaining`.

---

## Next Steps

| Guide | Description |
|-------|-------------|
| [TaskManager Pod Kill](task-manager-pod-kill.md) | Detailed scenarios: random kill, explicit pod selection, grace periods |
| [Network Chaos](network-chaos.md) | Latency, packet loss, bandwidth limits, and network partitions |
| [Resource Exhaustion](resource-exhaustion.md) | CPU and memory stress testing |
| [Safety Configuration](safety.md) | Tune guardrails for your environment |
| [CLI Reference](../cli-reference.md) | Full flag reference for all commands |
