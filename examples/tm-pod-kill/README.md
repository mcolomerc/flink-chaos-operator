# Example: TaskManager Pod Kill

This example shows how to kill one or more TaskManager pods using the
`TaskManagerPodKill` scenario and observe whether Flink recovers.

---

## Files

| File | Description |
|------|-------------|
| [`01-random-kill-podselector.yaml`](01-random-kill-podselector.yaml) | Kill one TM selected by pod labels (Ververica Platform / raw Kubernetes) |
| [`02-random-kill-flinkdeployment.yaml`](02-random-kill-flinkdeployment.yaml) | Kill one TM from a named `FlinkDeployment` (Apache Flink Kubernetes Operator) |
| [`03-dry-run.yaml`](03-dry-run.yaml) | Validate target resolution without killing any pods |

---

## Prerequisites

- The Flink Chaos Operator is installed and watching your namespace.
  See [Quick Start](../../docs/guidelines/quick-start.md) for installation steps.
- At least one Flink job is running with TaskManager pods in the target namespace.
- `kubectl` is configured against your cluster.

---

## Step 1 — Identify your TaskManager pods

Before running the experiment, confirm which pods will be targeted:

```bash
kubectl get pods -n vvp-jobs -l component=taskmanager
```

Expected output:

```
NAME                                                              READY   STATUS    RESTARTS   AGE
job-6a3aaaed-41bb-44c0-a956-754f84729037-taskmanager-69965cg4zw   1/1     Running   0          2m
```

Check what labels are on your TaskManager pods to know which selector to use:

```bash
kubectl get pods -n vvp-jobs -l component=taskmanager --show-labels
```

---

## Step 2 — Run a dry run first

Always validate your selector and safety checks before injecting a real failure:

```bash
kubectl apply -f 03-dry-run.yaml
```

Check the result:

```bash
kubectl get chaosrun tm-kill-dryrun -n vvp-jobs
```

Expected output:

```
NAME              PHASE       VERDICT        AGE
tm-kill-dryrun    Completed   Inconclusive   3s
```

`Inconclusive` is the correct verdict for dry runs — it means target resolution
and safety checks passed, but no pod was deleted.

Inspect which pods would have been selected:

```bash
kubectl get chaosrun tm-kill-dryrun -n vvp-jobs \
  -o jsonpath='{.status.dryRunPreview}' | python3 -m json.tool
```

Clean up when done:

```bash
kubectl delete chaosrun tm-kill-dryrun -n vvp-jobs
```

---

## Step 3 — Run the experiment

### Option A: PodSelector (Ververica Platform / raw Kubernetes)

Edit [`01-random-kill-podselector.yaml`](01-random-kill-podselector.yaml) and adjust:
- `metadata.namespace` — namespace where your Flink pods run
- `spec.target.selector.matchLabels` — labels that identify your TaskManager pods
- `spec.safety.minTaskManagersRemaining` — set to `0` if you only have 1 TaskManager

```bash
kubectl apply -f 01-random-kill-podselector.yaml
```

### Option B: FlinkDeployment (Apache Flink Kubernetes Operator)

Edit [`02-random-kill-flinkdeployment.yaml`](02-random-kill-flinkdeployment.yaml) and adjust:
- `metadata.namespace` — namespace where your FlinkDeployment lives
- `spec.target.name` — name of your FlinkDeployment resource

```bash
kubectl apply -f 02-random-kill-flinkdeployment.yaml
```

---

## Step 4 — Monitor the run

### Watch phase transitions in real time

```bash
kubectl get chaosrun -n vvp-jobs -w
```

You will see the run progress through phases:

```
NAME             PHASE       VERDICT   TARGET         AGE
tm-kill-random   Pending                              0s
tm-kill-random   Injecting                            1s
tm-kill-random   Observing                            2s
tm-kill-random   Completed   Passed    pod-selector   12s
```

### Inspect the full status

```bash
kubectl get chaosrun tm-kill-random -n vvp-jobs -o jsonpath='{.status}' \
  | python3 -m json.tool
```

Example output after a successful run:

```json
{
  "conditions": [
    { "type": "TargetResolved",      "status": "True", "message": "Target successfully resolved" },
    { "type": "SafetyChecksPassed",  "status": "True", "message": "All pre-injection safety checks passed" },
    { "type": "InjectionStarted",    "status": "True", "message": "Chaos injection started" },
    { "type": "InjectionCompleted",  "status": "True", "message": "Chaos injection completed" },
    { "type": "RecoveryObserved",    "status": "True", "message": "All replacement pods are ready" }
  ],
  "injectedPods": [
    "job-6a3aaaed-41bb-44c0-a956-754f84729037-taskmanager-69965cg4zw"
  ],
  "observation": {
    "recoveryObservedAt": "2026-04-02T11:21:47Z",
    "taskManagerCountAfter": 1,
    "taskManagerCountBefore": 2
  },
  "phase": "Completed",
  "replacementObserved": true,
  "verdict": "Passed"
}
```

### Check Kubernetes events

```bash
kubectl get events -n vvp-jobs \
  --field-selector involvedObject.name=tm-kill-random \
  --sort-by='.lastTimestamp'
```

### Tail the operator logs

```bash
kubectl logs -n vvp-jobs -l app.kubernetes.io/name=flink-chaos-operator --tail=50 -f
```

---

## Step 5 — Understand the verdict

| Verdict | Meaning | What to do |
|---------|---------|------------|
| `Passed` | Recovery observed — replacement TM pods became Ready within the timeout | Flink is resilient to single TM failures |
| `Failed` | No recovery observed within the timeout | Check Flink job logs, TaskManager deployment events |
| `Inconclusive` | Injection occurred but recovery could not be confirmed (e.g. Flink REST unreachable) | Check Flink REST endpoint configuration |

---

## Step 6 — Clean up

```bash
kubectl delete chaosrun tm-kill-random -n vvp-jobs
```

Delete all completed ChaosRuns in the namespace:

```bash
kubectl delete chaosrun -n vvp-jobs \
  --field-selector status.phase=Completed
```

---

## Variations

### Kill multiple TaskManagers at once

Change `spec.scenario.selection.count` to `2` or more. The safety check
`minTaskManagersRemaining` will block the run if too many would be killed:

```yaml
scenario:
  selection:
    mode: Random
    count: 2
safety:
  minTaskManagersRemaining: 1   # need at least 3 TMs total for this to pass
```

### Kill a specific pod by name

```yaml
scenario:
  selection:
    mode: Explicit
    podNames:
      - job-6a3aaaed-41bb-44c0-a956-754f84729037-taskmanager-69965cg4zw
```

### Graceful vs. abrupt kill

```yaml
scenario:
  action:
    type: DeletePod
    gracePeriodSeconds: 0   # immediate SIGKILL, simulates a node failure
```

The default omits `gracePeriodSeconds`, which uses the pod's own
`terminationGracePeriodSeconds`.

### Extend the observation window

Useful for jobs with slow checkpoint intervals or large state:

```yaml
observe:
  enabled: true
  timeout: 15m
  pollInterval: 30s
```

---

## Troubleshooting

### Safety check rejected: minTaskManagersRemaining

```
safety checks failed: injecting 1 pod(s) from 1 available TaskManagers
would leave 0 remaining, below the required minimum of 1
```

You have only 1 TaskManager and the default minimum is 1. In single-TM
test environments, override the minimum explicitly:

```yaml
safety:
  minTaskManagersRemaining: 0
```

### No pods found / target resolution failed

```bash
kubectl get chaosrun <name> -n <ns> -o jsonpath='{.status.message}'
```

If the message is `target resolution failed`, check that your label selector
matches actual running pods:

```bash
kubectl get pods -n vvp-jobs -l component=taskmanager,system=ververica-platform
```

### Run stuck in Pending

The controller may not be watching the namespace. Verify:

```bash
kubectl logs -n vvp-jobs -l app.kubernetes.io/name=flink-chaos-operator | tail -20
kubectl get deployment fchaos-flink-chaos-operator -n vvp-jobs \
  -o jsonpath='{.spec.template.spec.containers[0].env}'
```

The `WATCH_NAMESPACE` env var must match the namespace where you applied the ChaosRun.
