# Flink Chaos Tool - MVP Specification

Status: Draft v0.1  
Audience: platform engineers, operators, contributors  
Scope: first production-usable MVP

---

## 1. Summary

Build a Kubernetes-native, Flink-aware chaos tool that is easy to install, easy to operate, and safe by default.

The first MVP will ship as:

- one Kubernetes controller deployment
- one namespaced Custom Resource Definition: `ChaosRun`
- one CLI (`kubectl fchaos`) that creates, stops, and inspects runs
- one supported scenario: `TaskManagerPodKill`
- one deployment package: Helm chart
- no UI
- no database
- no daemonset / privileged agent
- no admission webhook

The MVP must support two target families:

1. Apache Flink Kubernetes Operator deployments
2. Ververica-managed Flink deployments resolved from Kubernetes workload metadata

The MVP must optimize for operational simplicity over breadth.

---

## 2. Problem Statement

Platform teams running Flink on Kubernetes want a simple way to validate failure handling and recovery behavior.

Existing chaos tooling is powerful but often too generic, too heavy, or too broad for day-one adoption in Flink environments. What is missing is a Flink-first experience with:

- simple deployment
- a small operational footprint
- Flink-aware targeting
- Flink-relevant run status
- straightforward start / stop semantics

The first user story is:

> As a platform engineer, I want to intentionally kill one or more TaskManager pods for a specific Flink workload and observe whether the workload recovers as expected.

---

## 3. Product Goals

### 3.1 Primary goals

- Make the tool easy to install with Helm into a single namespace.
- Make the tool easy to run with a single CLI command or a single YAML resource.
- Support Flink Operator workloads and Ververica-managed workloads with one user-facing API.
- Provide safe defaults and explicit blast-radius controls.
- Report useful recovery information instead of only reporting that a pod was deleted.

### 3.2 Success criteria

A user can:

- install the operator in less than 10 minutes
- run a TM kill experiment in one command
- stop an in-progress run in one command
- understand from `kubectl` whether the run injected the fault and whether recovery was observed

---

## 4. Non-Goals for the MVP

The MVP will not include:

- network chaos
- traffic shaping
- packet loss / latency injection
- OOM or memory stress injection
- checkpoint-aware triggers
- backpressure-aware triggers
- recurring schedules
- web UI
- workflow engine
- multi-cluster coordination
- Ververica Platform API integration
- direct support for session-mode chaos by default
- rollback of irreversible actions

These are phase-2+ features.

---

## 5. Design Principles

1. **Flink-first, not generic first**  
   The user should target logical Flink workloads, not raw pods, whenever possible.

2. **Small footprint**  
   Avoid node agents, webhooks, extra state stores, and privileged components in v1.

3. **Safe by default**  
   Limit concurrency, restrict scope, and make shared-cluster impact opt-in.

4. **One-shot runs first**  
   The MVP should execute discrete experiments, not full chaos workflows.

5. **Observable outcomes**  
   The tool should report injection and recovery signals, not only execution status.

6. **Extensible architecture**  
   Scenario drivers and target resolvers must be replaceable so the product can grow later.

---

## 6. MVP Scope

### 6.1 In scope

- `ChaosRun` CRD
- namespaced controller
- `kubectl fchaos` CLI
- one scenario: `TaskManagerPodKill`
- target resolution for:
  - `FlinkDeployment`
  - `VervericaDeployment`
  - `PodSelector` (advanced / escape hatch)
- Kubernetes-native observation
- optional Flink REST observation when available
- Helm packaging
- operator metrics endpoint

### 6.2 Out of scope

Everything listed under non-goals.

---

## 7. Deployment Model

### 7.1 Runtime topology

The MVP runtime consists of:

- one controller Deployment
- one ServiceAccount
- one Role / RoleBinding
- one CRD
- one metrics Service
- optional ServiceMonitor

### 7.2 Namespace model

The MVP is **single-workload-namespace scoped**.

That means:

- one operator instance watches one Kubernetes namespace
- the operator only injects chaos into that namespace
- users install additional releases if they need coverage in more namespaces

This keeps RBAC, support, and failure boundaries simple.

### 7.3 Packaging

Ship a single Helm chart:

- chart name: `flink-chaos-operator`
- install target: workload namespace
- default mode: namespaced

Example:

```bash
helm install fchaos ./charts/flink-chaos-operator -n streaming
```

---

## 8. Architecture

### 8.1 Components

#### Controller
Reconciles `ChaosRun` resources and drives scenario execution.

#### Target Resolvers
Translate a user-facing target into the current set of TaskManager pods.

#### Scenario Driver
Implements the injection behavior for `TaskManagerPodKill`.

#### Observer
Collects recovery signals from Kubernetes and, optionally, Flink REST.

#### CLI
Creates and patches `ChaosRun` resources and renders status in a human-friendly format.

### 8.2 Explicitly omitted in MVP

- daemonset / node agent
- tc / iptables integration
- cgroup manipulation
- sidecar injection
- persistent state store
- UI server
- admission webhook

### 8.3 Internal interfaces

```text
CLI -> ChaosRun CR -> Controller -> Target Resolver -> Scenario Driver -> Observer -> Status
```

---

## 9. Supported Target Types

The API will expose a logical `target.type`, not a raw Kubernetes `kind`, so the tool can support multiple platforms behind a stable user interface.

### 9.1 `FlinkDeployment`

Used for Apache Flink Kubernetes Operator application deployments.

Example:

```yaml
spec:
  target:
    type: FlinkDeployment
    name: orders-app
```

Resolution strategy:

- read the named `FlinkDeployment` in the same namespace
- resolve the current JobManager / TaskManager pod set for that deployment
- select TaskManager pods only

### 9.2 `VervericaDeployment`

Used for Ververica-managed workloads running in the same workload namespace.

Example:

```yaml
spec:
  target:
    type: VervericaDeployment
    deploymentName: orders-app
    vvpNamespace: analytics
```

Resolution strategy:

- find candidate pods in the namespace using Ververica workload metadata
- identify TaskManager pods only
- prefer matching by `deploymentId` when provided
- otherwise match by `deploymentName` and optional `vvpNamespace`

Notes:

- MVP support is Kubernetes-only and does not call the Ververica Platform API.
- If the Ververica Platform Kubernetes Operator is present, direct `VvpDeployment` resolution is a future enhancement, not part of the MVP.

### 9.3 `PodSelector`

Advanced escape hatch for unsupported cases.

Example:

```yaml
spec:
  target:
    type: PodSelector
    selector:
      matchLabels:
        app: my-flink-cluster
```

Rules:

- hidden from most examples and docs
- intended for advanced users and debugging
- still subject to safety checks

### 9.4 Unsupported target modes in MVP

- `FlinkSessionJob`
- Ververica session-mode workloads by default
- cross-namespace target resolution
- VVP API-driven target resolution

---

## 10. Session Mode Policy

Session mode is **not enabled by default** in the MVP.

Reason:

- a session cluster can host multiple applications
- killing a TaskManager can impact more than one logical deployment
- this breaks the default expectation that one chaos run targets one workload

MVP behavior:

- if the resolver detects a likely shared session target, the run is rejected by default
- the API includes an explicit override: `allowSharedClusterImpact: true`
- even with the override, support is marked experimental in MVP

---

## 11. Custom Resource API

### 11.1 Resource name

`ChaosRun`

### 11.2 Group / version

`chaos.flink.io/v1alpha1`

### 11.3 Example resource

```yaml
apiVersion: chaos.flink.io/v1alpha1
kind: ChaosRun
metadata:
  name: orders-tm-kill-001
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
```

### 11.4 Spec fields

#### `spec.target`

```yaml
spec:
  target:
    type: FlinkDeployment | VervericaDeployment | PodSelector
```

Per-type fields:

##### `FlinkDeployment`

```yaml
spec:
  target:
    type: FlinkDeployment
    name: string
```

##### `VervericaDeployment`

```yaml
spec:
  target:
    type: VervericaDeployment
    deploymentId: string        # optional, preferred when known
    deploymentName: string      # optional if deploymentId is set
    vvpNamespace: string        # optional, narrows matching
```

Validation:

- at least one of `deploymentId` or `deploymentName` must be set

##### `PodSelector`

```yaml
spec:
  target:
    type: PodSelector
    selector:
      matchLabels: {}
      matchExpressions: []
```

#### `spec.scenario`

MVP supports one scenario only:

```yaml
spec:
  scenario:
    type: TaskManagerPodKill
```

##### Selection fields

```yaml
selection:
  mode: Random | Explicit
  count: 1
  podNames: []
```

Rules:

- `Random` requires `count`
- `Explicit` requires `podNames`
- `count` must be >= 1
- `count` must not exceed the number of resolvable TM pods

##### Action fields

```yaml
action:
  type: DeletePod
  gracePeriodSeconds: 0
```

MVP supports only `DeletePod`.

`Evict` is a future enhancement.

#### `spec.observe`

```yaml
observe:
  enabled: true
  timeout: 10m
  pollInterval: 5s
  flinkRest:
    enabled: true
    endpoint: auto | http://...
```

Rules:

- `timeout` must be >= `pollInterval`
- `flinkRest.endpoint: auto` means attempt automatic discovery
- if Flink REST cannot be reached, the run continues with Kubernetes-only observation

#### `spec.safety`

```yaml
safety:
  dryRun: false
  maxConcurrentRunsPerTarget: 1
  minTaskManagersRemaining: 1
  allowSharedClusterImpact: false
```

Rules:

- only one active run per target by default
- the run is rejected if the resulting blast radius would violate `minTaskManagersRemaining`
- shared-cluster impact is blocked unless explicitly allowed

#### `spec.control`

```yaml
control:
  abort: false
```

This field is patched by the CLI to stop an in-progress run.

### 11.5 Status fields

```yaml
status:
  phase: Pending | Validating | Injecting | Observing | Completed | Aborted | Failed
  verdict: Passed | Failed | Inconclusive
  message: string
  startedAt: timestamp
  endedAt: timestamp
  selectedPods: []
  injectedPods: []
  replacementObserved: true | false
  targetSummary:
    type: FlinkDeployment
    name: orders-app
  observation:
    jobStateBefore: string
    jobStateAfter: string
    taskManagerCountBefore: 0
    taskManagerCountAfter: 0
    recoveryObservedAt: timestamp
  conditions: []
```

Condition examples:

- `TargetResolved`
- `SafetyChecksPassed`
- `InjectionStarted`
- `InjectionCompleted`
- `RecoveryObserved`
- `AbortRequested`
- `RunFailed`

---

## 12. Scenario Specification: `TaskManagerPodKill`

### 12.1 Goal

Delete one or more TaskManager pods belonging to a specific Flink workload and observe whether the system recovers within a configured window.

### 12.2 Why this is the first scenario

- high value
- low implementation complexity
- no privileged runtime requirements
- directly exercises Flink task failure and restart behavior

### 12.3 Injection semantics

The controller will:

1. resolve target pods
2. filter to TaskManager pods
3. run safety checks
4. choose pod(s) according to selection rules
5. delete selected pod(s)
6. observe replacement and recovery
7. write final status and verdict

### 12.4 Concurrency model

MVP behavior:

- default: one-shot burst delete of selected pods
- no long-running repeated kill loops
- no schedule support

### 12.5 Stop semantics

This scenario is **partially irreversible**.

That means:

- if aborted before deletion, no pods are deleted
- if aborted after some pods are deleted, the tool will stop further injection and stop observation early
- the tool does not recreate deleted pods manually
- recovery remains the responsibility of the target platform

### 12.6 Verdict rules

#### `Passed`

- injection succeeded, and
- replacement TaskManager pod(s) were observed, and
- if Flink REST was available, the job returned to an acceptable healthy state within the observation window

#### `Failed`

- target resolution failed, or
- safety checks failed, or
- deletion failed, or
- recovery was not observed before timeout

#### `Inconclusive`

- injection succeeded, but
- available observation signals were insufficient to confidently determine workload recovery

---

## 13. Observation Model

### 13.1 Required signals

The MVP must always collect Kubernetes-native signals:

- selected pod names
- pod deletion success
- replacement pod appearance
- ready status of replacement pods
- taskmanager pod count before and after

### 13.2 Optional signals

If available, also collect Flink REST signals:

- job state before injection
- job state after injection
- timestamp when job returned to a healthy state

### 13.3 Optional metrics integration

Prometheus is optional in the MVP.

If configured, the controller may store references or summaries for:

- selected recovery metrics
- checkpoint failure count during the window
- basic lag / throughput indicators

Prometheus is not required for MVP success.

### 13.4 Observation window

Observation ends when either:

- recovery is observed, or
- timeout is reached, or
- the run is aborted

---

## 14. Target Resolution Details

### 14.1 FlinkDeployment resolver

Input:

- namespace (implicit from `ChaosRun`)
- deployment name

Output:

- normalized target summary
- JobManager pod set
- TaskManager pod set

Rules:

- only TaskManager pods are eligible for this scenario
- if zero TaskManager pods are found, fail the run
- if multiple active clusters are detected for one logical target, fail unless explicitly supported in a future release

### 14.2 VervericaDeployment resolver

Input:

- namespace (implicit from `ChaosRun`)
- `deploymentId` and/or `deploymentName`
- optional `vvpNamespace`

Output:

- normalized target summary
- TaskManager pod set

Rules:

- match candidate pods using Ververica workload metadata
- identify TaskManager pods using workload characteristics available in Kubernetes metadata and pod specs
- prefer `deploymentId` over `deploymentName` because it is less ambiguous
- if the resolver suspects session-mode sharing, reject unless `allowSharedClusterImpact: true`

### 14.3 PodSelector resolver

Input:

- namespace
- Kubernetes label selector

Rules:

- only pods in the same namespace are allowed
- selected pods must pass TaskManager classification
- intended for advanced or fallback use only

### 14.4 Normalized target result

All resolvers must return the same internal structure:

```yaml
resolvedTarget:
  platform: flink-operator | ververica | generic
  namespace: streaming
  logicalName: orders-app
  taskManagerPods:
    - pod-a
    - pod-b
  jobManagerPods:
    - pod-c
  sharedCluster: false
```

This keeps scenario code platform-agnostic.

---

## 15. Safety Model

### 15.1 Default protections

The MVP must enforce:

- single active run per logical target by default
- single namespace execution
- no shared-cluster impact by default
- no all-pods deletion through examples or CLI defaults
- dry-run support
- rejection when there are too few TM pods remaining after selection

### 15.2 Protected namespaces

Even though the operator is namespace-scoped, the controller should still reject well-known system namespaces when misconfigured.

Default denylist:

- `kube-system`
- `kube-public`
- `kube-node-lease`

### 15.3 Dry-run mode

When `dryRun: true`:

- resolve the target
- perform safety checks
- select the pods
- write the selection and projected impact into status
- do not delete any pod

### 15.4 Abort behavior

Abort sets final phase to `Aborted` when possible.

No rollback is performed.

---

## 16. CLI Specification

CLI name:

```bash
kubectl fchaos
```

### 16.1 Commands

#### Run a TM kill

```bash
kubectl fchaos run tm-kill \
  --namespace streaming \
  --target-type flinkdeployment \
  --target-name orders-app \
  --count 1
```

Ververica form:

```bash
kubectl fchaos run tm-kill \
  --namespace streaming \
  --target-type ververica \
  --deployment-name orders-app \
  --vvp-namespace analytics \
  --count 1
```

#### Stop a run

```bash
kubectl fchaos stop orders-tm-kill-001 -n streaming
```

#### Show status

```bash
kubectl fchaos status orders-tm-kill-001 -n streaming
```

#### List runs

```bash
kubectl fchaos list -n streaming
```

### 16.2 CLI behavior

- `run` creates a `ChaosRun`
- `stop` patches `spec.control.abort=true`
- `status` reads and renders `status`
- `list` shows recent runs and phases

### 16.3 CLI output requirements

Status output should render:

- target
- scenario
- selected pods
- phase
- verdict
- recovery summary
- reason/message on failure

---

## 17. Controller Behavior

### 17.1 Reconciliation flow

1. ignore completed terminal resources unless cleanup is needed
2. validate spec
3. resolve target
4. check concurrency guard
5. run safety checks
6. select pods
7. inject deletion
8. observe recovery
9. update terminal phase and verdict

### 17.2 Idempotency

The controller must tolerate retries and restarts.

Requirements:

- status-driven reconciliation
- no duplicate deletion for the same selected pod if the controller restarts mid-run
- terminal phases are immutable except for metadata updates

### 17.3 Finalizers

MVP should avoid finalizers unless required for cleanup.

Preferred approach:

- no finalizer in normal execution
- rely on status and TTL cleanup of completed runs in a future phase

---

## 18. RBAC Requirements

The operator needs only namespace-scoped privileges.

Required verbs include:

- `get`, `list`, `watch` on pods
- `delete` on pods
- `get`, `list`, `watch` on events
- `get`, `list`, `watch`, `patch`, `update` on `chaosruns`
- `get`, `list`, `watch` on `flinkdeployments` when Flink Operator targets are enabled
- create / patch Kubernetes events for auditability

Not required in MVP:

- privileged pod execution
- host networking
- daemonset permissions
- cluster-admin access

---

## 19. Helm Configuration

### 19.1 Minimal values

```yaml
image:
  repository: example/flink-chaos-operator
  tag: 0.1.0

metrics:
  enabled: true
  serviceMonitor:
    enabled: false

defaults:
  observe:
    timeout: 10m
    pollInterval: 5s
  safety:
    maxConcurrentRunsPerTarget: 1
    minTaskManagersRemaining: 1
    allowSharedClusterImpact: false

features:
  ververicaResolver: true
  podSelectorTarget: true
```

### 19.2 Optional values

```yaml
flinkRest:
  autoDiscovery: true

logging:
  level: info
```

### 19.3 Deliberately absent values

No values for:

- daemonset agent
- web UI
- schedule engine
- external database

---

## 20. Operator Metrics

The operator should expose basic metrics for itself.

Suggested metrics:

- `fchaos_runs_total{scenario,phase,verdict}`
- `fchaos_run_duration_seconds`
- `fchaos_injections_total{scenario}`
- `fchaos_abort_requests_total`
- `fchaos_recovery_observed_total`

These metrics are for operating the chaos tool itself, not replacing workload observability.

---

## 21. Logging and Events

### 21.1 Structured logs

Each reconcile loop should log:

- `chaosRun`
- namespace
- target type
- target identity
- scenario
- selected pods
- phase
- verdict

### 21.2 Kubernetes events

Emit events for:

- target resolved
- safety rejection
- injection started
- injection completed
- recovery observed
- run failed
- abort requested

---

## 22. Repository Layout

```text
cmd/
  controller/
  kubectl-fchaos/
api/
  v1alpha1/
internal/
  controller/
  resolver/
    flinkdeployment/
    ververica/
    podselector/
  scenario/
    tmpodkill/
  observer/
    kubernetes/
    flinkrest/
  safety/
charts/
  flink-chaos-operator/
docs/
  mvp-spec.md
```

---

## 23. Testing Strategy

### 23.1 Unit tests

- API validation
- target resolver behavior
- selection logic
- safety rule evaluation
- verdict calculation

### 23.2 Integration tests

- controller reconciliation against a fake Kubernetes API
- status transitions
- abort behavior

### 23.3 End-to-end tests

Run on `kind` with:

- sample Flink deployment through Apache Flink Operator
- sample Ververica-like labeled workload fixture
- one TM kill run
- assertion that replacement is observed

MVP can use simulated Ververica metadata fixtures rather than requiring a full VVP installation in CI.

---

## 24. Acceptance Criteria

The MVP is done when all of the following are true:

1. Helm installs one namespaced operator successfully.
2. A user can submit a `ChaosRun` targeting a `FlinkDeployment`.
3. A user can submit a `ChaosRun` targeting a Ververica-managed workload via Kubernetes metadata.
4. The controller deletes one selected TaskManager pod.
5. The controller records selected pods, injected pods, and terminal status.
6. The controller can observe replacement pods using Kubernetes.
7. The CLI can create, stop, list, and show runs.
8. Safety checks reject shared-cluster impact unless explicitly allowed.
9. Dry-run mode shows projected impact without deleting pods.
10. The chart installs without any privileged daemonset, webhook, or external database.

---

## 25. Deferred Roadmap

### Phase 2

- `DeletePod` plus `Evict`
- repeated / serial kill modes
- direct `VvpDeployment` resolver
- Prometheus-backed recovery summaries
- richer Flink REST health signals

### Phase 3

- memory pressure / stress scenarios
- optional node agent
- network delay / loss / partition scenarios
- checkpoint-aware triggers
- backpressure-aware triggers

### Phase 4

- schedules
- reusable templates
- UI
- multi-namespace or cluster-wide mode

---

## 26. Open Questions

1. Should `PodSelector` be public in v1, or hidden behind an advanced feature flag?
2. Should `DeletePod` remain the only action in v1, or should `Evict` be added before GA?
3. What is the minimum healthy-state definition when Flink REST is not reachable?
4. Should the first release support serial deletion of multiple TaskManagers, or only burst deletion?
5. Do we want a TTL cleanup controller for completed `ChaosRun` resources in v1, or defer it?

---

## 27. Recommended Implementation Order

1. API types and validation
2. `ChaosRun` controller skeleton
3. Kubernetes observer
4. `FlinkDeployment` resolver
5. `VervericaDeployment` resolver
6. `TaskManagerPodKill` scenario driver
7. CLI commands
8. Helm packaging
9. E2E tests
10. Documentation and examples

---

## 28. Final Recommendation

Keep the first release intentionally small.

The MVP should prove three things well:

- the tool is easy to install
- the tool is easy to run and stop
- the tool can inject a meaningful Flink failure and report a useful outcome

Everything else should be postponed until that core loop is stable.
