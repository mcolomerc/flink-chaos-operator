# Target Types

## FlinkDeployment

Target Apache Flink Kubernetes Operator workloads.

```yaml
spec:
  target:
    type: FlinkDeployment
    name: my-flink-app
```

**How it works:**
1. The resolver fetches the named `FlinkDeployment` resource.
2. It discovers TaskManager and JobManager pods using standard Flink Operator labels (`app`, `component`).
3. Only TaskManager pods are eligible for disruption.
4. Session-mode clusters are detected and marked as shared.

**Requirements:**
- The FlinkDeployment must exist in the same namespace as the ChaosRun.
- At least one TaskManager pod must be running.

---

## VervericaDeployment

Target Ververica Platform-managed Flink deployments using Kubernetes-native pod discovery (no VVP API calls).

```yaml
spec:
  target:
    type: VervericaDeployment
    deploymentId: "abc-123-def-456"
    # OR use deploymentName when deploymentId is not known
    deploymentName: orders-processor
    vvpNamespace: analytics
```

**How it works:**
1. The resolver queries pods in the namespace for Ververica metadata labels.
2. Pods are matched using well-known Ververica labels (`flink.apache.org/deployment-id`, `vvp.io/deployment-name`, etc.).
3. `deploymentId` is preferred over `deploymentName` for reliable matching.

**Requirements:**
- At least one of `deploymentId` or `deploymentName` must be specified.
- Pods must be labeled with Ververica metadata.

---

## PodSelector

Advanced escape hatch for selecting pods by arbitrary Kubernetes labels.

```yaml
spec:
  target:
    type: PodSelector
    selector:
      matchLabels:
        app: my-flink-cluster
        component: taskmanager
```

**Notes:**
- For advanced users and custom deployments.
- All standard safety guardrails still apply (denylist, concurrency, MinTaskManagers).
