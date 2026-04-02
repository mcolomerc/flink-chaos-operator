# Code Review — Go Developer Task List

Generated from full codebase review. Ordered by priority. Each task is self-contained and actionable.

---

## Critical

### CR-01 — Fix NetworkPolicy semantics (inverted partition logic)

**File**: `internal/scenario/networkpartition/` (policy builder)
**Severity**: Critical — functional correctness bug; the NetworkPartition scenario blocks all traffic *except* to the target peer instead of *only* blocking traffic to the target peer.

**Problem**: Kubernetes NetworkPolicy rules are *allow* rules. A policy with `PolicyTypes: [Egress]` and one egress rule matching JM pods means TMs can *only* egress to JMs — the inverse of the intended partition. NetworkPolicy has no native "deny specific peer" primitive.

**Required fix**: Restructure the NetworkPolicy generation to achieve a true partition. The standard pattern is:
1. Create a default-deny egress policy for TM pods (empty `egress: []` with `PolicyTypes: [Egress]`).
2. Create explicit allow policies for all traffic that should still be permitted (inter-TM, DNS, etc.).

Or consider switching to a two-policy approach:
- Policy A: default deny all egress on TM pods.
- Policy B: allow egress to everything *except* the blocked peer by using CIDR negation if supported by the CNI.

At minimum, add an integration test that verifies traffic between TMs and JMs is actually blocked (not only allowed) when a TMtoJM partition is in effect. Document the CNI requirements (standard NetworkPolicy does not support deny-specific-peer; a CNI like Calico with `NetworkPolicy` v2 or `GlobalNetworkPolicy` is required for true deny rules).

---

### CR-02 — Eliminate shell injection risk in tc builder

**File**: `internal/scenario/networkchaos/tc/builder.go`
**Severity**: Critical — any user-controlled field interpolated into `/bin/sh -c` is a shell injection vector.

**Problem**: `EntrypointScript()` interpolates `r.PeerCIDR` and `r.Bandwidth` into a shell script passed to `/bin/sh -c`. Even though both fields are validated, the validated value is stored in the spec but the original string from the API object is what flows through. Future callers (or a validation bypass) could pass metacharacters.

**Required fix**: Remove the shell script entirely. Pass `tc` as a direct `command` + `args` array in the ephemeral container spec, bypassing `/bin/sh` entirely. Each `tc` invocation should be a separate container command or use `&&`-chained args without shell interpolation. Example:

```go
// Instead of: /bin/sh -c "tc qdisc add dev eth0 root netem delay 100ms"
// Use:
Command: []string{"tc"},
Args:    []string{"qdisc", "add", "dev", "eth0", "root", "netem", "delay", "100ms"},
```

For multi-step tc setup (add + filter), use an init container or a purpose-built entrypoint binary that accepts structured arguments, not a shell script.

---

### CR-03 — Guard nil dereference in `waitForStableCheckpoint`

**File**: `internal/controller/chaosrun_controller.go`
**Severity**: Critical — nil pointer panic under realistic conditions.

**Problem**: `run.Status.StartedAt.Time.Add(obs.CheckpointWaitTimeout.Duration)` dereferences both `run.Status.StartedAt` and `obs.CheckpointWaitTimeout` without nil checks. If a status patch fails after `StartedAt` was set or if defaults were not applied (e.g., the object was created with a partial spec), this panics.

**Required fix**:
```go
if run.Status.StartedAt == nil {
    return errCheckpointNotStable // not ready yet; retry
}
if obs.CheckpointWaitTimeout == nil {
    return fmt.Errorf("checkpointWaitTimeout is nil; defaults may not have been applied")
}
```

---

### CR-04 — Handle `PhaseValidating` in the controller state machine

**File**: `internal/controller/chaosrun_controller.go`, `api/v1alpha1/chaosrun_types.go`
**Severity**: Critical — a ChaosRun in `Validating` phase is stuck forever with no progress and no error.

**Problem**: `PhaseValidating` is declared in the enum and appears in kubebuilder validation, but the controller's phase switch has no case for it. A manual status patch or a future migration could leave a run stuck in this phase indefinitely.

**Options** (choose one):
1. Remove `PhaseValidating` from the enum if it is not a real phase in the current state machine.
2. Add a case in the switch that transitions it back to `PhasePending` or directly handles it like `PhasePending`.

---

## Major

### CR-05 — Preserve real errors in `flinkdeployment` resolver

**File**: `internal/resolver/flinkdeployment/resolver.go`
**Severity**: Major — transient API errors (network timeout, RBAC) are masked as "not found".

**Required fix**:
```go
if err := r.Client.Get(ctx, key, &fd); err != nil {
    if apierrors.IsNotFound(err) {
        return nil, fmt.Errorf("FlinkDeployment %q not found in namespace %q", name, namespace)
    }
    return nil, fmt.Errorf("fetching FlinkDeployment %q: %w", name, namespace, err)
}
```
Apply the same pattern to the Ververica resolver if it has the same issue.

---

### CR-06 — Reuse HTTP client in Flink REST observer

**File**: `internal/observer/flinkrest/observer.go`, `internal/controller/chaosrun_controller.go`
**Severity**: Major — a new `http.Client` (and TCP connection pool) is created on every 5-second observation poll and on every reconcile during checkpoint wait.

**Required fix**: Cache the HTTP client per endpoint. The simplest approach is to create the client once in `main.go` and inject it into the observer/controller, or to add a `sync.Map`-based cache keyed by endpoint URL inside the observer.

For `waitForStableCheckpoint` in the controller: the factory function `flinkrestpkg.NewHTTPClient` is called directly. Make the factory injectable (it already is on the observer struct — use the same pattern in the controller).

---

### CR-07 — Log resolver errors during observation phase

**File**: `internal/controller/chaosrun_controller.go`, `reconcileObserving`
**Severity**: Major — silent failure; target resolution errors during observation are completely invisible.

**Required fix**:
```go
target, resolveErr := r.resolveTarget(ctx, run)
if resolveErr != nil {
    log.Error(resolveErr, "target resolution failed during observation; recovery signals may be incomplete")
}
```

---

### CR-08 — Anchor observation timeout to injection time, not start time

**File**: `internal/controller/chaosrun_controller.go`
**Severity**: Major — checkpoint wait time (up to 5 min) silently reduces the actual observation window.

**Required fix**: Record an `ObservingStartedAt` timestamp in `ChaosRunStatus` when the run transitions from `Injecting` to `Observing`. Use this timestamp for the observation timeout calculation instead of `StartedAt`. Update `zz_generated.deepcopy.go` and the API reference doc accordingly.

---

### CR-09 — Use label selectors in pod list calls

**Files**: `internal/resolver/flinkdeployment/resolver.go`, `internal/resolver/ververica/resolver.go`
**Severity**: Major — listing all pods in a namespace is a performance and scalability problem.

**Required fix** (flinkdeployment example):
```go
r.Client.List(ctx, &podList,
    client.InNamespace(namespace),
    client.MatchingLabels{"app": deploymentName},
)
```
For Ververica, add `client.MatchingLabels{ververica.DeploymentIDLabel: deploymentID}` when `deploymentId` is set.

---

## Minor

### CR-10 — Set verdict on aborted runs

**File**: `internal/controller/` (wherever `AbortRun` / terminal phase transitions happen)
**Severity**: Minor — aborted runs have no verdict, creating empty metric label values.

**Required fix**: When transitioning to `PhaseAborted`, set `run.Status.Verdict = v1alpha1.VerdictInconclusive` (or add a dedicated `VerdictAborted` constant to the enum). Ensure this is reflected in the kubebuilder validation marker.

---

### CR-11 — Extract shared pod utility functions

**Files**: `internal/resolver/flinkdeployment/resolver.go`, `internal/resolver/ververica/resolver.go`, `internal/resolver/podselector/resolver.go`
**Severity**: Minor — `isPodEligible` is identically copied across three packages.

**Required fix**: Create `internal/podutil/podutil.go` with `IsPodEligible(pod corev1.Pod) bool` and `ComponentRole(pod corev1.Pod) string`. Update all three resolvers to import and use these shared functions.

---

### CR-12 — Extract shared ephemeral container utilities

**Files**: `internal/scenario/networkchaos/driver.go`, `internal/scenario/resourceexhaustion/driver.go`
**Severity**: Minor — `ephemeralContainerName` and `findEphemeralContainerState` are duplicated.

**Required fix**: Create `internal/ephemeral/ephemeral.go` (or add to `internal/podutil/`) with shared `EphemeralContainerName(prefix, podName string, ts time.Time) string` and `FindEphemeralContainerState(pod *corev1.Pod, containerName string) *corev1.ContainerState`.

---

### CR-13 — Use `cmd.Context()` instead of `context.Background()` in CLI

**File**: `cmd/kubectl-fchaos/main.go`
**Severity**: Minor — Ctrl+C does not cancel in-flight Kubernetes API calls.

**Required fix**: Replace all `context.Background()` in `RunE` handlers with `cmd.Context()`. Cobra v1.2+ wires `cmd.Context()` to OS signal handling automatically when `cobra.EnableCommandSorting` is set and `PersistentPreRunE` registers a signal context.

---

### CR-14 — Make CLI flags local (not package-level vars)

**File**: `cmd/kubectl-fchaos/main.go`
**Severity**: Minor — package-level flag vars make parallel testing impossible and pollute the global namespace.

**Required fix**: Move flag variables inside each subcommand constructor function. Bind them using `cmd.Flags().StringVarP(&localVar, ...)` on locally declared variables, following the `targetFlagSet` pattern already used in the file.

---

### CR-15 — Make `waitForStableCheckpoint` HTTP client injectable

**File**: `internal/controller/chaosrun_controller.go`
**Severity**: Minor — directly calls `flinkrestpkg.NewHTTPClient`, making checkpoint wait untestable without real HTTP.

**Required fix**: Add a `FlinkClientFactory func(string) flinkrestpkg.Client` field to `ChaosRunReconciler`. Use it in `waitForStableCheckpoint`. Wire `flinkrestpkg.NewHTTPClient` in `main.go`. In tests, inject a stub factory.

---

### CR-16 — Populate `DryRunPreview` for all scenario types

**File**: `internal/controller/chaosrun_controller.go`
**Severity**: Minor — `status.dryRunPreview` is defined in the API but never written.

**Required fix**: In the dry-run branch of `reconcileInjecting`, populate `run.Status.DryRunPreview` with a human-readable description of what would be injected. For TaskManagerPodKill: list selected pods. For NetworkPartition: describe the NetworkPolicy that would be created. For NetworkChaos: describe the tc rule. For ResourceExhaustion: describe the stress-ng invocation.

---

### CR-17 — Change `SelectionSpec.Count` from `int` to `int32`

**File**: `api/v1alpha1/chaosrun_types.go`
**Severity**: Minor — `int` is platform-dependent; `int32` is conventional for CRD numeric fields.

**Required fix**: Change the field type to `int32`. Update all code that reads or writes this field (validation, controller, CLI). Update `zz_generated.deepcopy.go` if needed (scalar fields do not need deep copy but the struct's DeepCopyInto may reference it).
