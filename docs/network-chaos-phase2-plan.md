# Phase 2 — Network Chaos: Design & Implementation Plan

Status: Draft
Scope: Flink-specific network disruption scenarios
Follows: [MVP Specification](../flink-chaos-mvp-spec.md)

---

## 1. Architecture Decision

**Chosen approach: Hybrid — NetworkPolicy for partition + ephemeral containers with `tc` for latency / loss / bandwidth**

| Sub-scenario | Mechanism | Rationale |
|---|---|---|
| TM ↔ TM partition | NetworkPolicy | Zero privilege, pure Kubernetes API, binary block is the correct partition semantic |
| TM ↔ JM partition | NetworkPolicy | Same |
| TM ↔ checkpoint storage partition | NetworkPolicy (egress CIDR block) | Binary block to a CIDR is valid; CIDR must be provided |
| TM ↔ TM latency / jitter / loss | Ephemeral container + `tc netem` | NetworkPolicy has no rate/delay semantics |
| TM ↔ JM latency / jitter / loss | Ephemeral container + `tc netem` | Same |
| TM ↔ checkpoint storage latency / loss | Ephemeral container + `tc netem` | Checkpoint storage is outside the cluster; NetworkPolicy can only block, not delay |
| TM ↔ external sink/source bandwidth | Ephemeral container + `tc tbf` | Token Bucket Filter is the correct discipline for rate limiting |

**Rejected options:**

- **DaemonSet node-agent** — violates the project's core principle of no privileged node components.
- **NetworkPolicy-only** — covers partition but has no rate/delay/loss semantics.
- **iptables via ephemeral container** — equivalent trade-offs to `tc` but less precise for delay/jitter.
- **eBPF** — requires kernel-level tooling and deep cluster access.

**Key constraints of the ephemeral container approach:**

- Ephemeral containers are stable since Kubernetes 1.23. The module already uses `k8s.io/api v0.35.0`, so this is satisfied.
- The ephemeral container needs `NET_ADMIN` capability and an image with `iproute2`. The image is configurable in the Helm chart.
- Ephemeral containers cannot be removed from a pod's spec once added. Cleanup means running `tc qdisc del dev eth0 root` inside the container and letting its process exit.
- Abort before natural expiry is handled by sending `SIGTERM` via the exec API. The entrypoint script traps `SIGTERM` and cleans up before exiting.

---

## 2. New CRD API Fields

### 2.1 New `ScenarioType` constants

```go
// +kubebuilder:validation:Enum=TaskManagerPodKill;NetworkPartition;NetworkChaos
type ScenarioType string

const (
    ScenarioTaskManagerPodKill ScenarioType = "TaskManagerPodKill"
    ScenarioNetworkPartition   ScenarioType = "NetworkPartition"
    ScenarioNetworkChaos       ScenarioType = "NetworkChaos"
)
```

### 2.2 New enums

```go
// NetworkTarget identifies the Flink component pair whose traffic is disrupted.
// +kubebuilder:validation:Enum=TMtoTM;TMtoJM;TMtoCheckpoint;TMtoExternal
type NetworkTarget string

const (
    NetworkTargetTMtoTM         NetworkTarget = "TMtoTM"
    NetworkTargetTMtoJM         NetworkTarget = "TMtoJM"
    NetworkTargetTMtoCheckpoint NetworkTarget = "TMtoCheckpoint"
    NetworkTargetTMtoExternal   NetworkTarget = "TMtoExternal"
)

// NetworkDirection controls which directions of traffic are affected.
// +kubebuilder:validation:Enum=Ingress;Egress;Both
type NetworkDirection string

const (
    NetworkDirectionIngress NetworkDirection = "Ingress"
    NetworkDirectionEgress  NetworkDirection = "Egress"
    NetworkDirectionBoth    NetworkDirection = "Both"
)
```

### 2.3 `ExternalTarget` sub-struct

```go
// ExternalTarget describes a network endpoint outside the Flink pod mesh.
// Either CIDR or Hostname must be set; CIDR is preferred.
type ExternalTarget struct {
    // CIDR is the IP block of the external endpoint (e.g. "10.200.0.0/16").
    // Required for NetworkPartition (NetworkPolicy cannot use hostnames).
    // +optional
    CIDR string `json:"cidr,omitempty"`

    // Hostname is a DNS name for the external endpoint. Usable by NetworkChaos
    // only; NetworkPartition requires CIDR.
    // +optional
    Hostname string `json:"hostname,omitempty"`

    // Port restricts the chaos effect to one destination port.
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=65535
    // +optional
    Port int32 `json:"port,omitempty"`
}
```

### 2.4 `NetworkChaosSpec` sub-struct

```go
// NetworkChaosSpec describes the network disruption to apply.
// Required when ScenarioSpec.Type is NetworkPartition or NetworkChaos.
type NetworkChaosSpec struct {
    // Target identifies the Flink component pair to disrupt.
    // +kubebuilder:default=TMtoJM
    Target NetworkTarget `json:"target"`

    // Direction controls which traffic directions are affected.
    // +kubebuilder:default=Both
    // +optional
    Direction NetworkDirection `json:"direction,omitempty"`

    // ExternalEndpoint describes the remote endpoint for TMtoCheckpoint
    // and TMtoExternal targets.
    // +optional
    ExternalEndpoint *ExternalTarget `json:"externalEndpoint,omitempty"`

    // Latency is the fixed one-way delay to inject (e.g. "100ms").
    // Only valid for ScenarioNetworkChaos. Maps to tc netem delay.
    // +optional
    Latency *metav1.Duration `json:"latency,omitempty"`

    // Jitter is the random variation added to the delay (e.g. "20ms").
    // Requires Latency to be set. Maps to tc netem delay X jitter Y.
    // +optional
    Jitter *metav1.Duration `json:"jitter,omitempty"`

    // Loss is the percentage of packets to drop (0–100).
    // Only valid for ScenarioNetworkChaos. Maps to tc netem loss N%.
    // +kubebuilder:validation:Minimum=0
    // +kubebuilder:validation:Maximum=100
    // +optional
    Loss *int32 `json:"loss,omitempty"`

    // Bandwidth is the maximum egress rate (e.g. "10mbit", "1gbit").
    // Maps to tc tbf rate X. Only valid for TMtoExternal.
    // +optional
    Bandwidth string `json:"bandwidth,omitempty"`

    // Duration is how long the disruption remains active.
    // After this duration the driver cleans up before finalizing.
    // Defaults to 60s. Capped by safety.maxNetworkChaosDuration.
    // +optional
    Duration *metav1.Duration `json:"duration,omitempty"`
}
```

### 2.5 Extended `ScenarioSpec`

```go
type ScenarioSpec struct {
    Type      ScenarioType       `json:"type"`
    Selection SelectionSpec      `json:"selection,omitempty"`
    Action    ActionSpec         `json:"action,omitempty"`

    // Network describes network disruption parameters.
    // Required when Type is NetworkPartition or NetworkChaos.
    // +optional
    Network *NetworkChaosSpec `json:"network,omitempty"`
}
```

### 2.6 New `PhaseCleaningUp`

```go
const (
    // PhaseCleaningUp means the operator is removing injected network rules
    // before finalizing. Skipped for TaskManagerPodKill.
    PhaseCleaningUp RunPhase = "CleaningUp"
)
```

Updated state machine:
```
Pending → Injecting → Observing → CleaningUp → Completed / Failed / Aborted
```
`TaskManagerPodKill` skips `CleaningUp` and transitions directly from `Observing` to terminal.

### 2.7 Extended `ChaosRunStatus`

```go
// NetworkPolicies lists NetworkPolicy names created by this run.
// Used for label-based cleanup.
// +optional
NetworkPolicies []string `json:"networkPolicies,omitempty"`

// EphemeralContainerInjections records per-pod ephemeral tc container state.
// +optional
EphemeralContainerInjections []EphemeralContainerRecord `json:"ephemeralContainerInjections,omitempty"`

// EphemeralContainerRecord tracks a single ephemeral tc container injection.
type EphemeralContainerRecord struct {
    PodName       string           `json:"podName"`
    ContainerName string           `json:"containerName"`
    InjectedAt    *metav1.Time     `json:"injectedAt,omitempty"`
    CleanedUp     bool             `json:"cleanedUp,omitempty"`
}
```

### 2.8 Extended `SafetySpec`

```go
// MaxNetworkChaosDuration caps spec.scenario.network.duration.
// Defaults to 5m. Runs requesting a longer duration are rejected.
// +optional
MaxNetworkChaosDuration *metav1.Duration `json:"maxNetworkChaosDuration,omitempty"`

// AllowCheckpointStorageChaos permits targeting checkpoint storage.
// Defaults to false. Requires explicit opt-in due to data-loss risk.
// +optional
AllowCheckpointStorageChaos bool `json:"allowCheckpointStorageChaos,omitempty"`
```

### 2.9 New condition constants

```go
const (
    ConditionNetworkChaosInjected  = "NetworkChaosInjected"
    ConditionNetworkChaosCleanedUp = "NetworkChaosCleanedUp"
)
```

---

## 3. New Packages

```
internal/
  netchaos/
    tc/            — pure Go tc command builder (no k8s dependency)
    policy/        — pure Go NetworkPolicy builder (no k8s API calls)
  scenario/
    networkpartition/   — ScenarioDriver for NetworkPartition
    networkchaos/       — ScenarioDriver for NetworkChaos (ephemeral containers)
```

### 3.1 `internal/netchaos/tc/builder.go`

```go
type Rule struct {
    Interface string
    Latency   time.Duration
    Jitter    time.Duration
    LossPct   int32    // 0–100
    Bandwidth string   // "10mbit" for tbf rate
    PeerCIDR  string   // optional: restrict rule to traffic for this CIDR
}

func AddArgs(r Rule) []string          // argv for: tc qdisc add dev eth0 root netem ...
func DelArgs(iface string) []string    // argv for: tc qdisc del dev eth0 root
func EntrypointScript(r Rule, duration time.Duration) string  // shell script with SIGTERM trap
```

The entrypoint script traps `SIGTERM` to run `tc qdisc del` before exiting, ensuring cleanup on abort:

```sh
#!/bin/sh
set -e
trap 'tc qdisc del dev eth0 root 2>/dev/null; exit 0' TERM
tc qdisc add dev eth0 root netem delay 100ms 20ms
sleep 60 &
wait $!
tc qdisc del dev eth0 root 2>/dev/null || true
```

### 3.2 `internal/netchaos/policy/builder.go`

```go
type Spec struct {
    Name         string
    Namespace    string
    ChaosRunName string   // written as label chaos.flink.io/run-name
    SourceLabels map[string]string
    DestLabels   map[string]string   // nil when destination is a CIDR
    DestCIDR     string
    Direction    v1alpha1.NetworkDirection
}

func Build(s Spec) *networkingv1.NetworkPolicy
```

### 3.3 `internal/scenario/networkpartition/driver.go`

Implements `interfaces.ScenarioDriver` + `interfaces.CleanableScenarioDriver`.

- `Inject`: creates NetworkPolicy resources, labels each with `chaos.flink.io/run-name=<name>`, writes names to `status.networkPolicies`.
- `Cleanup`: lists policies by label, deletes each, treats `NotFound` as already cleaned up.

### 3.4 `internal/scenario/networkchaos/driver.go`

Implements `interfaces.ScenarioDriver` + `interfaces.CleanableScenarioDriver`.

- `Inject`: injects an ephemeral container per target TM pod via the `/ephemeralcontainers` subresource, records `EphemeralContainerRecord` entries in status.
- `Cleanup`: polls container state; sends `SIGTERM` via exec API for early abort; marks `CleanedUp: true` when container reaches `Terminated`.
- Re-attach after crash: uses `status.EphemeralContainerInjections` + `InjectedAt` + `Duration` to determine remaining time and re-attach.

### 3.5 Extended `interfaces.go`

```go
// CleanableScenarioDriver is implemented by drivers that create external
// resources (NetworkPolicies, ephemeral containers) requiring cleanup
// after the run completes or is aborted.
type CleanableScenarioDriver interface {
    ScenarioDriver
    Cleanup(ctx context.Context, run *v1alpha1.ChaosRun) error
}
```

### 3.6 Controller: `ScenarioDriver` → `ScenarioDrivers` map

```go
type ChaosRunReconciler struct {
    // ...existing fields...

    // ScenarioDrivers maps scenario type to its driver implementation.
    // Replaces the single ScenarioDriver field from Phase 1.
    ScenarioDrivers map[v1alpha1.ScenarioType]interfaces.ScenarioDriver
}
```

The `injectChaos` helper dispatches by `run.Spec.Scenario.Type`. The reconciler gains a `reconcileCleaningUp` phase handler that type-asserts `CleanableScenarioDriver` and calls `Cleanup`.

---

## 4. Cleanup / Restore Logic

### 4.1 NetworkPolicy cleanup

Every NetworkPolicy is labeled:
```
chaos.flink.io/run-name: <ChaosRun.Name>
chaos.flink.io/managed-by: flink-chaos-operator
```

Cleanup uses label-based lookup (not just the status list) so it works even when the status write failed after the policy was created. Owner references are not used — the controller owns the timing.

### 4.2 Ephemeral container cleanup

1. The container's entrypoint applies tc rules, sleeps for `Duration`, then deletes the qdisc.
2. `SIGTERM` triggers early cleanup: the trap deletes the qdisc before the process exits.
3. The controller monitors container state by polling `pod.Status.EphemeralContainerStatuses`.
4. When the container reaches `Terminated`, the controller sets `CleanedUp: true` in status.

### 4.3 Crash-recovery re-attach

On reconciler restart, when `status.EphemeralContainerInjections` is non-empty:

1. Fetch each pod and inspect the ephemeral container state.
2. `Running` → re-compute remaining duration; if elapsed, send `SIGTERM`.
3. `Terminated` → mark `CleanedUp: true`.
4. `Waiting` / absent → treat as failed injection; transition to `Failed`.

This makes the `status` the single source of truth for what was injected, tolerating operator crashes at any point.

---

## 5. Safety Additions

All new checks added to `internal/safety/checker.go`:

| Check | Rejects when |
|---|---|
| `checkNetworkPartitionBlastRadius` | All TMs would be isolated simultaneously (Direction=Both targeting all TMs) |
| `checkCheckpointStorageChaos` | Target=TMtoCheckpoint and `allowCheckpointStorageChaos != true` |
| `checkNetworkChaosDuration` | `network.duration > safety.maxNetworkChaosDuration` |
| `checkEphemeralContainerSupport` | Cluster version < 1.23 (runtime gate with clear error message) |

**Dry-run extension:** for `dryRun: true`, write to a `status.dryRunPreview` string field:
- `NetworkPartition`: the NetworkPolicy names and pod selectors that would be created.
- `NetworkChaos`: per-pod ephemeral container entrypoint script (exact `tc` commands) and image.

---

## 6. New RBAC Requirements

```go
// NetworkPolicy management
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;delete;patch

// Ephemeral container injection
// +kubebuilder:rbac:groups="",resources=pods/ephemeralcontainers,verbs=get;patch;update

// SIGTERM delivery for early abort
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create
```

> **Note on `pods/exec`**: This is a sensitive RBAC permission. It is only required for abort-before-expiry. Clusters that cannot grant it can set `safety.maxNetworkChaosDuration` to an acceptable wait time and rely on natural expiry instead. This trade-off should be prominently documented.

---

## 7. Implementation Order

| Step | Scope | Key deliverables |
|---|---|---|
| 1 | API types | New structs, enums, `PhaseCleaningUp`, extended status; updated validation and defaults; regenerated deepcopy |
| 2 | `internal/netchaos/tc/` | `Rule`, `AddArgs`, `DelArgs`, `EntrypointScript`; full unit tests |
| 3 | `internal/netchaos/policy/` | `Build`; unit tests comparing generated NetworkPolicy objects |
| 4 | `internal/scenario/networkpartition/` | `Inject` + `Cleanup`; tests with fake client |
| 5 | `internal/scenario/networkchaos/` | `Inject` + `Cleanup` + crash-recovery; tests with fake client (exec stubbed) |
| 6 | `internal/safety/` | Four new checks + dry-run preview; unit tests for each rejection path |
| 7 | Controller | `ScenarioDrivers` map, `reconcileCleaningUp`, `CleanableScenarioDriver` dispatch, updated `main.go` |
| 8 | CLI | `run network-partition`, `run network-chaos` sub-commands; `status` shows `CleaningUp` phase |
| 9 | Helm | New RBAC rules; `networkchaos.tcImage` value; feature flags; `safety.maxNetworkChaosDuration` |
| 10 | E2E tests | kind-based: partition verifiable via TCP timeout; latency verifiable via ping; crash-recovery test |

---

## 8. Open Questions

**OQ-1: `pods/exec` acceptability**
Granting `pods/exec` to the operator is sensitive in multi-tenant clusters. Should the abort-before-expiry path be gated behind a feature flag (`features.networkChaosAbort: true`), so operators who cannot grant `pods/exec` simply wait for natural expiry?

**OQ-2: tc image provenance**
The ephemeral container needs `iproute2`. Options: (a) publish a purpose-built project image, (b) use a well-known public image (`alpine`, `nicolaka/netshoot`), (c) require users to supply their own. Option (a) is most controlled but adds a build pipeline. What is the project's image provenance policy?

**OQ-3: `NET_ADMIN` under Pod Security Standards**
`NET_ADMIN` on an ephemeral container may be blocked by a `Restricted` Pod Security Standards profile or by OPA/Gatekeeper. Should the operator require a documented minimum PSS profile (`Baseline`) on the target namespace, and should the safety checker pre-flight this?

**OQ-4: NetworkPolicy CNI compatibility**
Clusters using Flannel without an enforcement overlay silently ignore NetworkPolicy. Should the operator emit a warning event when a NetworkPolicy is created but enforcement cannot be verified? Or is this purely a documentation concern?

**OQ-5: Controller struct migration**
Changing `ScenarioDriver` (single field) to `ScenarioDrivers` (map) is a breaking change to the reconciler struct and all test setups. Should Phase 2 introduce both fields temporarily (`ScenarioDriver` as fallback, `ScenarioDrivers` as override) for a cleaner migration, or is a clean break preferred?
