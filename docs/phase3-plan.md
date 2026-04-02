# Phase 3 Implementation Plan — Flink Chaos Operator

**Status:** In Progress
**Follows:** Phase 2 (NetworkPartition, NetworkChaos, PhaseCleaningUp)
**Design principle:** Highest Flink-specific value first, observability before new scenarios, no new cluster-scoped components.

---

## Executive Summary

Phase 3 delivers five things, in priority order:

1. **Flink REST API observer** — the `FlinkRestObserve` struct already exists in the CRD but has no client implementation. Implementing it closes the biggest functional gap and unlocks checkpoint-aware triggers.
2. **Checkpoint-aware injection triggers** — only inject when checkpointing is stable, using the REST observer as the data source.
3. **Controller-level tests for CleaningUp phase** — `reconcileCleaningUp` has zero test coverage.
4. **CLI pre-flight validation for network enum values** — `NetworkTarget` and `NetworkDirection` are not validated before resource submission.
5. **`ResourceExhaustion` scenario** — stress memory or CPU on a TaskManager via ephemeral containers running `stress-ng`.

---

## Dependency Graph

```
Step 3 (CleaningUp tests)    ─────────────────────────────────┐
Step 4 (CLI enum validation)  ────────────────────────────────┤
Step 1 (REST observer)   ──────► Step 2 (checkpoint triggers) │
                                                               ▼
                                               Step 5 (ResourceExhaustion)
                                                               │
                                                               ▼
                                               Step 6 (Docs + Helm)
```

Steps 1, 3, and 4 have no dependencies on each other and can execute in parallel.

---

## Step 1 — Flink REST API Observer

**Complexity:** M | **Agent:** go-dev-agent

**Scope (in):**
- New package `internal/observer/flinkrest` implementing `interfaces.Observer`
- Polls `/jobs` for job state and `/taskmanagers` for running TM count
- Populates `ObservationStatus.JobStateBefore`, `JobStateAfter`, `TaskManagerCountBefore`, `TaskManagerCountAfter`
- Activated when `spec.observe.flinkRest.enabled: true` and `spec.observe.flinkRest.endpoint` is non-empty
- Composite observer (`internal/observer/composite`) fans out to k8s + REST; `AllReplacementsReady` requires both
- `FlinkRestClient` interface injected for unit test stubbing
- Extend `ObservationResult` in `interfaces.go` with `FlinkJobState string` and `CheckpointStatus string`

**Scope (out):** No checkpoint metadata, no Ververica REST API.

**Files created:**
- `internal/observer/flinkrest/observer.go`
- `internal/observer/flinkrest/client.go`
- `internal/observer/flinkrest/observer_test.go`
- `internal/observer/composite/observer.go`

**Files modified:**
- `internal/interfaces/interfaces.go`
- `cmd/controller/main.go`

---

## Step 2 — Checkpoint-Aware Injection Trigger

**Complexity:** M | **Agent:** go-dev-agent | **Depends on:** Step 1

**Scope (in):**
- New CRD fields: `FlinkRestObserve.RequireStableCheckpointBeforeInject bool`, `CheckpointStableWindowSeconds int32` (default 60), `CheckpointWaitTimeout metav1.Duration` (default 5m)
- `reconcilePending` polls `/jobs/{jobId}/checkpoints` before transitioning to Injecting
- If not stable within `CheckpointWaitTimeout` → PhaseFailed with clear message
- Validation: `RequireStableCheckpointBeforeInject=true` requires `FlinkRest.Enabled=true`

**Scope (out):** No backpressure-aware triggers, no checkpoint data/savepoint parsing.

**Files created:**
- `internal/observer/flinkrest/checkpoints.go`
- `internal/observer/flinkrest/checkpoints_test.go`

**Files modified:**
- `api/v1alpha1/chaosrun_types.go`
- `api/v1alpha1/chaosrun_validation.go`
- `internal/controller/chaosrun_controller.go`
- `api/v1alpha1/zz_generated.deepcopy.go`

---

## Step 3 — Controller Tests for CleaningUp Phase

**Complexity:** S | **Agent:** go-dev-agent

**Scope (in):**
- `stubCleanableDriver` in `suite_test.go` implementing `interfaces.CleanableScenarioDriver`
- Five new test functions in `chaosrun_controller_test.go`:
  - `TestReconcile_NetworkChaos_CleaningUp_HappyPath`
  - `TestReconcile_NetworkChaos_CleaningUp_AbortPath`
  - `TestReconcile_NetworkChaos_CleaningUp_TimeoutPreservesVerdict`
  - `TestReconcile_CleaningUp_DeadlineExceeded`
  - `TestReconcile_CleaningUp_CleanupErrorRequeues`

**Files modified:**
- `internal/controller/chaosrun_controller_test.go`
- `internal/controller/suite_test.go`

---

## Step 4 — CLI Pre-flight Enum Validation

**Complexity:** S | **Agent:** go-dev-agent

**Scope (in):**
- `validateNetworkTarget(s string) error` and `validateNetworkDirection(s string) error` functions
- Called from `runNetworkPartition` and `runNetworkChaos` before `c.Create()`
- Table-driven tests for valid/invalid combos

**Files created:**
- `cmd/kubectl-fchaos/validate.go`
- `cmd/kubectl-fchaos/validate_test.go`

**Files modified:**
- `cmd/kubectl-fchaos/main.go`

---

## Step 5 — `ResourceExhaustion` Scenario

**Complexity:** L | **Agent:** go-dev-agent | **Depends on:** Steps 3 and 4

**Scope (in):**
- New `ScenarioType`: `ScenarioResourceExhaustion = "ResourceExhaustion"`
- New `ResourceExhaustionSpec`: `Mode` (Memory|CPU), `MemoryBytes int64`, `CPUWorkers int32`, `Duration *metav1.Duration`
- Ephemeral container running `stress-ng` (no extra Linux capabilities needed)
- Implements `CleanableScenarioDriver`
- New `internal/netchaos/stress/builder.go` (pure stress-ng argv builder, analogous to `netchaos/tc/builder.go`)
- New safety field: `safety.MaxResourceExhaustionDuration` (default 5m)
- CLI: `run resource-exhaustion` subcommand
- Helm: `resourceexhaustion.stressImage` value

**Files created:**
- `internal/scenario/resourceexhaustion/driver.go`
- `internal/scenario/resourceexhaustion/driver_test.go`
- `internal/netchaos/stress/builder.go`
- `internal/netchaos/stress/builder_test.go`

**Files modified:**
- `api/v1alpha1/chaosrun_types.go`
- `api/v1alpha1/chaosrun_validation.go`
- `api/v1alpha1/zz_generated.deepcopy.go`
- `internal/safety/checker.go`
- `internal/controller/chaosrun_controller.go`
- `cmd/controller/main.go`
- `cmd/kubectl-fchaos/main.go`
- `charts/flink-chaos-operator/values.yaml`

---

## Step 6 — Documentation and Helm Chart Update

**Complexity:** S | **Agent:** readme-doc-maintainer | **Depends on:** Steps 1–5

**Files modified/created:**
- `README.md`
- `charts/flink-chaos-operator/Chart.yaml`
- `charts/flink-chaos-operator/values.yaml`
- `charts/flink-chaos-operator/templates/deployment.yaml`
- `docs/phase3-plan.md` (this file, status updated)

---

## Complexity and Agent Summary

| Step | Description | Complexity | Agent | Depends on |
|------|-------------|------------|-------|------------|
| 1 | Flink REST API observer + composite observer | M | go-dev-agent | — |
| 2 | Checkpoint-aware injection trigger | M | go-dev-agent | Step 1 |
| 3 | Controller tests for CleaningUp phase | S | go-dev-agent | — |
| 4 | CLI pre-flight enum validation | S | go-dev-agent | — |
| 5 | ResourceExhaustion scenario (stress-ng) | L | go-dev-agent | Steps 3, 4 |
| 6 | Docs + Helm chart update | S | readme-doc-maintainer | Steps 1–5 |

---

## Architectural Decisions

- **Composite observer** — k8s pod readiness AND Flink job state must agree before `AllReplacementsReady=true`; a stale REST endpoint cannot mask a pod that hasn't recovered.
- **`ResourceExhaustionSpec` as separate field on `ScenarioSpec`** — follows the `Network *NetworkChaosSpec` precedent; avoids nullable fields with conflicting semantics.
- **stress-ng** — no extra Linux capabilities required for CPU/memory; configurable image (same pattern as `tcImage`).
- **`internal/netchaos/stress`** — pure argv builder with no k8s imports; kept alongside `netchaos/tc` to signal both are injection-tool builders.
