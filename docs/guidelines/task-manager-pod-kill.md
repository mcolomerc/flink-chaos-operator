# Guideline: TaskManager Pod Kill

End-to-end usage guide for the `TaskManagerPodKill` scenario. Covers the most common testing patterns and how to interpret results.

**Prerequisites**: Complete the [Quick Start](quick-start.md) guide before following these scenarios.

---

## Scenario Overview

`TaskManagerPodKill` deletes one or more TaskManager pods and measures whether:
1. Kubernetes schedules replacement pods.
2. The Flink job returns to a healthy state.
3. Recovery completes within the configured timeout.

No network rules or ephemeral containers are created — cleanup is instantaneous.

---

## Scenario 1: Kill One Random TaskManager

The baseline test. Validates that your Flink job can tolerate a single TaskManager failure.

```bash
kubectl fchaos run tm-kill \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --count 1
```

**What to expect:**
- The operator selects 1 TaskManager pod at random.
- The pod is deleted immediately (grace period = 0).
- Kubernetes schedules a replacement pod.
- The operator polls for the replacement to reach Ready state.
- If Flink REST is reachable and the job returns to RUNNING, the run completes with `Passed`.

**Passing criteria:**
```
Phase: Completed   Verdict: Passed
```

---

## Scenario 2: Kill Multiple TaskManagers Simultaneously

Tests burst failure: can Flink recover when several TaskManagers fail at the same time?

```bash
kubectl fchaos run tm-kill \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --count 2 \
  --observe-timeout 15m
```

> **Safety guard**: The operator will reject this run if `count >= total TM pods - minTaskManagersRemaining`. Default `minTaskManagersRemaining=1`, so a cluster with 3 TMs can safely kill at most 2.

Adjust the safety minimum if you need a stricter limit:

```bash
kubectl fchaos run tm-kill \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --count 2 \
  --min-task-managers 2
```

This rejects the run when the cluster would drop below 2 running TMs.

---

## Scenario 3: Kill a Specific TaskManager Pod

Use explicit pod selection when you want to test recovery from a specific node or pod failure (e.g. the pod on a particular node, or the one holding a specific task slot).

```bash
# Find pod names first
kubectl get pods -n streaming -l app=orders-app,component=taskmanager

# Kill a specific pod
kubectl fchaos run tm-kill \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --pod-names orders-app-taskmanager-2
```

> If the named pod does not exist, the run fails immediately with `TargetResolutionFailed`.

---

## Scenario 4: Graceful Termination vs Immediate Kill

Test the difference in recovery time when the pod has time to shut down cleanly.

**Immediate kill** (default, simulates node failure):

```bash
kubectl fchaos run tm-kill \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --count 1
# grace-period defaults to 0
```

**Graceful shutdown** (simulates rolling restart):

```bash
kubectl fchaos run tm-kill \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --count 1 \
  --grace-period 30
```

Compare `recoveryObservedAt - startedAt` across both runs to measure the impact of graceful vs immediate termination on recovery time.

---

## Scenario 5: Kill with Flink REST Observation

Enable Flink REST polling to get richer recovery signals: job state before/after injection and precise recovery timestamps.

```yaml
# chaosrun-tm-kill-with-rest.yaml
apiVersion: chaos.flink.io/v1alpha1
kind: ChaosRun
metadata:
  name: orders-kill-with-rest
  namespace: streaming
spec:
  target:
    type: FlinkDeployment
    name: orders-app
  scenario:
    type: TaskManagerPodKill
    selection:
      mode: Random
      count: 1
  observe:
    enabled: true
    timeout: 10m
    pollInterval: 5s
    flinkRest:
      enabled: true
      endpoint: "http://orders-app-rest.streaming.svc.cluster.local:8081"
  safety:
    minTaskManagersRemaining: 1
```

```bash
kubectl apply -f chaosrun-tm-kill-with-rest.yaml
kubectl fchaos status orders-kill-with-rest -n streaming --watch
```

After completion, inspect the observation data:

```bash
kubectl get chaosrun orders-kill-with-rest -n streaming \
  -o jsonpath='{.status.observation}' | jq .
```

Expected output:

```json
{
  "jobStateBefore": "RUNNING",
  "jobStateAfter": "RUNNING",
  "taskManagerCountBefore": 3,
  "taskManagerCountAfter": 3,
  "recoveryObservedAt": "2026-04-02T10:35:22Z"
}
```

---

## Scenario 6: Kill with Checkpoint Stability Gate

Ensure the Flink job has a recent checkpoint before injecting a failure. This tests meaningful failure points rather than hitting a job mid-recovery.

```yaml
apiVersion: chaos.flink.io/v1alpha1
kind: ChaosRun
metadata:
  name: orders-checkpoint-aware-kill
  namespace: streaming
spec:
  target:
    type: FlinkDeployment
    name: orders-app
  scenario:
    type: TaskManagerPodKill
    selection:
      mode: Random
      count: 1
  observe:
    enabled: true
    timeout: 10m
    flinkRest:
      enabled: true
      endpoint: "http://orders-app-rest.streaming.svc.cluster.local:8081"
      requireStableCheckpointBeforeInject: true
      checkpointStableWindowSeconds: 60    # checkpoint must be < 60s old
      checkpointWaitTimeout: 5m            # fail if no stable checkpoint within 5m
```

The operator waits up to 5 minutes for a checkpoint younger than 60 seconds before injecting. If no such checkpoint exists within the timeout, the run fails with `CheckpointWaitTimeout`.

---

## Scenario 7: Ververica Deployment Target

Target a Flink job managed by Ververica Platform.

```bash
# Using deployment ID (preferred)
kubectl fchaos run tm-kill \
  --namespace streaming \
  --target-type ververica \
  --deployment-id abc-123-def-456 \
  --count 1

# Using deployment name
kubectl fchaos run tm-kill \
  --namespace streaming \
  --target-type ververica \
  --deployment-name orders-processor \
  --vvp-namespace analytics \
  --count 1
```

---

## Interpreting Results

### Run took longer than expected

Check how long each phase took:

```bash
kubectl get chaosrun <name> -n streaming \
  -o jsonpath='{.status.startedAt}{"\n"}{.status.observation.recoveryObservedAt}{"\n"}{.status.endedAt}'
```

- Long gap between `startedAt` and `observation.recoveryObservedAt` → slow Flink job restart.
- `endedAt` without `recoveryObservedAt` → timeout expired before recovery was observed.

### Verdict is `Inconclusive`

Flink REST was not reachable or returned insufficient data. Either:
1. Set `observe.flinkRest.enabled=false` to rely only on Kubernetes-level pod signals.
2. Fix the Flink REST endpoint configuration.

### Run failed with `SafetyCheckFailed`

```bash
kubectl describe chaosrun <name> -n streaming | grep -A5 "Conditions"
```

Adjust safety limits or wait for other runs to complete.

---

## Next Steps

- [Network Chaos](network-chaos.md) — test failure modes beyond pod loss
- [Resource Exhaustion](resource-exhaustion.md) — test CPU and memory pressure
- [Safety Configuration](../safety.md) — configure guardrails for production use
