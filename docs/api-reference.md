# ChaosRun API Reference

## Full Example

```yaml
apiVersion: chaos.flink.io/v1alpha1
kind: ChaosRun
metadata:
  name: orders-app-tm-kill-001
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
    action:
      type: DeletePod
      gracePeriodSeconds: 0

  observe:
    enabled: true
    timeout: 10m
    pollInterval: 5s
    flinkRest:
      enabled: true
      endpoint: auto

  safety:
    dryRun: false
    maxConcurrentRunsPerTarget: 1
    minTaskManagersRemaining: 1
    allowSharedClusterImpact: false

  control:
    abort: false

status:
  phase: Observing
  verdict: ""
  message: Waiting for recovery...
  startedAt: "2026-03-27T14:20:00Z"
  selectedPods:
    - orders-app-taskmanager-0
  injectedPods:
    - orders-app-taskmanager-0
  replacementObserved: true
  observation:
    jobStateBefore: RUNNING
    jobStateAfter: RUNNING
    taskManagerCountBefore: 3
    taskManagerCountAfter: 3
    recoveryObservedAt: "2026-03-27T14:35:22Z"
  conditions:
    - type: TargetResolved
      status: "True"
    - type: SafetyChecksPassed
      status: "True"
    - type: InjectionCompleted
      status: "True"
    - type: RecoveryObserved
      status: "True"
```

## Spec Fields

### `spec.target`

Identifies the Flink workload to target.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | `FlinkDeployment`, `VervericaDeployment`, or `PodSelector`. |
| `name` | string | For FlinkDeployment | Kubernetes resource name of the FlinkDeployment. |
| `deploymentId` | string | For VervericaDeployment | Ververica Platform deployment UUID. |
| `deploymentName` | string | For VervericaDeployment | Human-readable Ververica deployment name. |
| `vvpNamespace` | string | Optional | VVP namespace for narrowing Ververica lookups. |
| `selector` | LabelSelector | For PodSelector | Kubernetes label selector for pod matching. |

### `spec.scenario`

Describes the chaos scenario to execute.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `type` | string | — | `TaskManagerPodKill`, `NetworkPartition`, `NetworkChaos`, or `ResourceExhaustion`. |
| `selection.mode` | string | `Random` | Pod selection: `Random` or `Explicit` (TaskManagerPodKill only). |
| `selection.count` | int | 1 | Number of pods to select when mode is `Random`. |
| `selection.podNames` | []string | — | Pod names to target when mode is `Explicit`. |
| `action.type` | string | `DeletePod` | Action to perform (TaskManagerPodKill only). |
| `action.gracePeriodSeconds` | int64 | 0 | Grace period for pod termination (0 = immediate). |
| `network.target` | string | — | `TMtoTM`, `TMtoJM`, `TMtoCheckpoint`, or `TMtoExternal`. |
| `network.direction` | string | `Both` | Traffic direction: `Ingress`, `Egress`, or `Both`. |
| `network.latency` | Duration | — | Base latency to inject (NetworkChaos only; e.g., `100ms`). |
| `network.jitter` | Duration | — | Jitter range around latency (NetworkChaos only). |
| `network.loss` | int | — | Packet loss percentage 0–100 (NetworkChaos only). |
| `network.bandwidth` | string | — | Bandwidth limit (NetworkChaos only; e.g., `10mbit`). |
| `network.duration` | Duration | 60s | How long to apply network impairment. |
| `network.externalEndpoint.cidr` | string | — | CIDR range for external endpoint. |
| `network.externalEndpoint.hostname` | string | — | Hostname for external endpoint. |
| `network.externalEndpoint.port` | int | — | Port for external endpoint. |
| `resourceExhaustion.mode` | string | — | `CPU` or `Memory` (ResourceExhaustion only). |
| `resourceExhaustion.workers` | int | 1 | Number of stress-ng worker processes. |
| `resourceExhaustion.memoryPercent` | int | 80 | Percentage of pod memory limit to consume (Memory mode only, 1–100). |
| `resourceExhaustion.duration` | Duration | 60s | How long to apply resource exhaustion. |

### `spec.observe`

Configures the post-injection observation phase.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | true | Activate observation after injection. |
| `timeout` | Duration | 10m | Maximum time to wait for recovery. |
| `pollInterval` | Duration | 5s | Frequency of recovery checks. Must be ≤ timeout. |
| `flinkRest.enabled` | bool | false | Enable Flink REST API polling. |
| `flinkRest.endpoint` | string | — | Flink REST API base URL. |
| `flinkRest.requireStableCheckpointBeforeInject` | bool | false | Hold injection until a stable checkpoint exists. |
| `flinkRest.checkpointStableWindowSeconds` | int | 60 | Max age (seconds) for a checkpoint to be considered stable. |
| `flinkRest.checkpointWaitTimeout` | Duration | 5m | How long to wait for a stable checkpoint before failing. |

### `spec.safety`

Defines guardrails that prevent unsafe chaos executions.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `dryRun` | bool | false | Preview impact without executing any destructive action. |
| `maxConcurrentRunsPerTarget` | int | 1 | Maximum simultaneous runs targeting the same workload. |
| `minTaskManagersRemaining` | int | 1 | Minimum TaskManagers that must remain after injection. |
| `maxNetworkChaosDuration` | Duration | 5m | Maximum allowed duration for NetworkChaos scenarios. |
| `maxResourceExhaustionDuration` | Duration | 5m | Maximum allowed duration for ResourceExhaustion scenarios. |
| `allowCheckpointStorageChaos` | bool | false | Allow chaos targeting checkpoint storage. Requires explicit opt-in. |
| `allowSharedClusterImpact` | bool | false | Permit runs that impact shared session clusters. |

### `spec.control`

Provides runtime control signals for an in-flight run.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `abort` | bool | false | Request immediate stop of an in-flight run. |

## Status Fields

### `status.phase`

Lifecycle phase of the run:

| Value | Description |
|-------|-------------|
| `Pending` | Run accepted, not yet processed. |
| `Validating` | Target resolution and safety checks in progress. |
| `Injecting` | Chaos action being applied. |
| `Observing` | Monitoring recovery. |
| `CleaningUp` | Removing chaos resources (NetworkPolicy, ephemeral containers). |
| `Completed` | Run finished normally. |
| `Aborted` | Run stopped by user or operator. |
| `Failed` | Run encountered unrecoverable error. |

### `status.verdict`

Outcome of a completed run:

| Value | Description |
|-------|-------------|
| `Passed` | Injection succeeded and recovery was observed. |
| `Failed` | Injection or recovery detection failed. |
| `Inconclusive` | Insufficient signals to determine outcome. |

### Other Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `message` | string | Human-readable status description. |
| `startedAt` | Time | When the run transitioned out of Pending. |
| `endedAt` | Time | When the run reached a terminal phase. |
| `selectedPods` | []string | Pod names chosen for injection. |
| `injectedPods` | []string | Pod names where chaos was successfully applied. |
| `replacementObserved` | bool | Whether replacement pods were detected. |
| `targetSummary.type` | string | Resolved target type. |
| `targetSummary.name` | string | Resolved target name. |
| `observation.jobStateBefore` | string | Flink job state before injection. |
| `observation.jobStateAfter` | string | Flink job state after injection. |
| `observation.taskManagerCountBefore` | int | TaskManager pod count before injection. |
| `observation.taskManagerCountAfter` | int | TaskManager pod count at last observation. |
| `observation.recoveryObservedAt` | Time | Timestamp when recovery was confirmed. |
| `networkPolicies` | []string | Names of NetworkPolicy resources created by this run. |
| `ephemeralContainerInjections` | []EphemeralContainerRecord | Per-pod tc container state (NetworkChaos). |
| `resourceExhaustionInjections` | []EphemeralContainerRecord | Per-pod stress container state (ResourceExhaustion). |
| `conditions` | []Condition | Standard Kubernetes status conditions. |
