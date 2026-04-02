/*
Copyright 2024 The Flink Chaos Operator Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TargetType identifies the kind of Flink workload being targeted.
// +kubebuilder:validation:Enum=FlinkDeployment;VervericaDeployment;PodSelector
type TargetType string

const (
	// TargetFlinkDeployment targets a FlinkDeployment managed by the Apache Flink Kubernetes Operator.
	TargetFlinkDeployment TargetType = "FlinkDeployment"
	// TargetVervericaDeployment targets a Flink deployment managed by Ververica Platform.
	TargetVervericaDeployment TargetType = "VervericaDeployment"
	// TargetPodSelector targets pods matching an arbitrary label selector.
	TargetPodSelector TargetType = "PodSelector"
)

// ScenarioType identifies the chaos scenario to execute.
// +kubebuilder:validation:Enum=TaskManagerPodKill;NetworkPartition;NetworkChaos;ResourceExhaustion
type ScenarioType string

const (
	// ScenarioTaskManagerPodKill kills one or more TaskManager pods.
	ScenarioTaskManagerPodKill ScenarioType = "TaskManagerPodKill"
	// ScenarioNetworkPartition uses NetworkPolicy to create a binary traffic block.
	ScenarioNetworkPartition ScenarioType = "NetworkPartition"
	// ScenarioNetworkChaos uses ephemeral containers with tc netem/tbf to inject
	// latency, jitter, packet loss, or bandwidth limits.
	ScenarioNetworkChaos ScenarioType = "NetworkChaos"
	// ScenarioResourceExhaustion uses stress-ng in ephemeral containers to exhaust
	// CPU or memory on selected TaskManager pods.
	ScenarioResourceExhaustion ScenarioType = "ResourceExhaustion"
)

// SelectionMode controls how pods are chosen within a scenario.
// +kubebuilder:validation:Enum=Random;Explicit
type SelectionMode string

const (
	// SelectionModeRandom selects pods at random up to the specified count.
	SelectionModeRandom SelectionMode = "Random"
	// SelectionModeExplicit selects the pods listed by name in PodNames.
	SelectionModeExplicit SelectionMode = "Explicit"
)

// ActionType identifies the disruptive action applied to selected pods.
// +kubebuilder:validation:Enum=DeletePod
type ActionType string

const (
	// ActionDeletePod deletes the targeted pod via the Kubernetes API.
	ActionDeletePod ActionType = "DeletePod"
)

// RunPhase represents the lifecycle phase of a ChaosRun.
// +kubebuilder:validation:Enum=Pending;Injecting;Observing;CleaningUp;Completed;Aborted;Failed
type RunPhase string

const (
	// PhasePending indicates the ChaosRun has been accepted but not yet processed.
	PhasePending RunPhase = "Pending"
	// PhaseInjecting indicates the chaos action is being applied.
	PhaseInjecting RunPhase = "Injecting"
	// PhaseObserving indicates the operator is monitoring recovery.
	PhaseObserving RunPhase = "Observing"
	// PhaseCleaningUp indicates the operator is removing injected network rules
	// (NetworkPolicies, tc qdiscs) before finalizing the run.
	// Skipped for TaskManagerPodKill which has no external resource cleanup.
	PhaseCleaningUp RunPhase = "CleaningUp"
	// PhaseCompleted indicates the run finished normally.
	PhaseCompleted RunPhase = "Completed"
	// PhaseAborted indicates the run was stopped by the operator or user.
	PhaseAborted RunPhase = "Aborted"
	// PhaseFailed indicates the run encountered an unrecoverable error.
	PhaseFailed RunPhase = "Failed"
)

// RunVerdict is the outcome verdict of a completed ChaosRun.
// +kubebuilder:validation:Enum=Passed;Failed;Inconclusive
type RunVerdict string

const (
	// VerdictPassed indicates the workload recovered as expected.
	VerdictPassed RunVerdict = "Passed"
	// VerdictFailed indicates the workload did not recover as expected.
	VerdictFailed RunVerdict = "Failed"
	// VerdictInconclusive indicates recovery could not be definitively determined.
	VerdictInconclusive RunVerdict = "Inconclusive"
)

// Condition type constants used in ChaosRunStatus.Conditions.
const (
	// ConditionTargetResolved reports whether the target workload was successfully resolved.
	ConditionTargetResolved = "TargetResolved"
	// ConditionSafetyChecksPassed reports whether all pre-injection safety checks passed.
	ConditionSafetyChecksPassed = "SafetyChecksPassed"
	// ConditionInjectionStarted reports whether the chaos injection has begun.
	ConditionInjectionStarted = "InjectionStarted"
	// ConditionInjectionCompleted reports whether all injection actions completed.
	ConditionInjectionCompleted = "InjectionCompleted"
	// ConditionRecoveryObserved reports whether workload recovery was observed.
	ConditionRecoveryObserved = "RecoveryObserved"
	// ConditionAbortRequested reports whether an abort was requested via the control field.
	ConditionAbortRequested = "AbortRequested"
	// ConditionRunFailed reports whether the run terminated with a failure.
	ConditionRunFailed = "RunFailed"
	// ConditionNetworkChaosInjected reports that network disruption rules are active.
	ConditionNetworkChaosInjected = "NetworkChaosInjected"
	// ConditionNetworkChaosCleanedUp reports that all network rules have been removed.
	ConditionNetworkChaosCleanedUp = "NetworkChaosCleanedUp"
)

// NetworkTarget identifies the pair of Flink components whose traffic is disrupted.
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

// TargetSpec describes which Flink workload to target.
type TargetSpec struct {
	// Type identifies the kind of Flink deployment being targeted.
	Type TargetType `json:"type"`

	// Name is the Kubernetes resource name of the target deployment.
	// Required when Type is FlinkDeployment.
	// +optional
	Name string `json:"name,omitempty"`

	// DeploymentID is the Ververica Platform deployment UUID.
	// Required (or DeploymentName) when Type is VervericaDeployment.
	// +optional
	DeploymentID string `json:"deploymentId,omitempty"`

	// DeploymentName is the human-readable name of the Ververica deployment.
	// Required (or DeploymentID) when Type is VervericaDeployment.
	// +optional
	DeploymentName string `json:"deploymentName,omitempty"`

	// VVPNamespace scopes the Ververica deployment lookup to a specific VVP namespace.
	// +optional
	VVPNamespace string `json:"vvpNamespace,omitempty"`

	// Selector matches pods directly by label.
	// Required when Type is PodSelector.
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

// SelectionSpec controls how pods are chosen for the chaos action.
type SelectionSpec struct {
	// Mode determines the pod selection strategy.
	// Defaults to Random.
	// +optional
	Mode SelectionMode `json:"mode,omitempty"`

	// Count is the number of pods to select when Mode is Random.
	// +kubebuilder:validation:Minimum=1
	// +optional
	Count int32 `json:"count,omitempty"`

	// PodNames lists the exact pod names to target when Mode is Explicit.
	// +optional
	PodNames []string `json:"podNames,omitempty"`
}

// ActionSpec defines the disruptive action applied to the selected pods.
type ActionSpec struct {
	// Type is the action to perform on the selected pods.
	Type ActionType `json:"type"`

	// GracePeriodSeconds is the grace period passed to the pod deletion API.
	// Set to 0 for immediate termination.
	// +kubebuilder:validation:Minimum=0
	// +optional
	GracePeriodSeconds *int64 `json:"gracePeriodSeconds,omitempty"`
}

// ExternalTarget describes a network endpoint outside the Flink pod mesh,
// such as a checkpoint storage service or external sink/source.
type ExternalTarget struct {
	// CIDR is the IP block of the external endpoint (e.g. "10.200.0.0/16").
	// Required for NetworkPartition; optional for NetworkChaos.
	// +optional
	CIDR string `json:"cidr,omitempty"`

	// Hostname is a DNS name for the external endpoint.
	// Usable by NetworkChaos only — NetworkPartition requires CIDR.
	// +optional
	Hostname string `json:"hostname,omitempty"`

	// Port restricts the chaos effect to a single destination port.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port int32 `json:"port,omitempty"`
}

// NetworkChaosSpec describes the network disruption parameters.
// Required when ScenarioSpec.Type is NetworkPartition or NetworkChaos.
type NetworkChaosSpec struct {
	// Target identifies the Flink component pair to disrupt.
	// +kubebuilder:default=TMtoJM
	Target NetworkTarget `json:"target"`

	// Direction controls which traffic directions are affected.
	// Defaults to Both.
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

	// Duration is how long the disruption remains active before cleanup.
	// Defaults to 60s. Capped by safety.maxNetworkChaosDuration.
	// +optional
	Duration *metav1.Duration `json:"duration,omitempty"`
}

// ResourceExhaustionMode identifies whether CPU or Memory is exhausted.
// +kubebuilder:validation:Enum=CPU;Memory
type ResourceExhaustionMode string

const (
	// ResourceExhaustionModeCPU runs stress-ng --cpu workers to saturate CPU.
	ResourceExhaustionModeCPU ResourceExhaustionMode = "CPU"
	// ResourceExhaustionModeMemory runs stress-ng --vm workers to exhaust memory.
	ResourceExhaustionModeMemory ResourceExhaustionMode = "Memory"
)

// ResourceExhaustionSpec configures a CPU or memory stress injection.
type ResourceExhaustionSpec struct {
	// Mode selects whether CPU or Memory is exhausted.
	// +kubebuilder:validation:Enum=CPU;Memory
	Mode ResourceExhaustionMode `json:"mode"`

	// Workers is the number of stress-ng worker goroutines.
	// Defaults to 1. For CPU mode this is --cpu N; for Memory mode --vm N.
	// +kubebuilder:validation:Minimum=1
	// +optional
	Workers int32 `json:"workers,omitempty"`

	// MemoryPercent is the percentage of each pod's memory limit to consume.
	// Only valid for Memory mode. Valid range 1–100. Defaults to 80.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// +optional
	MemoryPercent int32 `json:"memoryPercent,omitempty"`

	// Duration is how long the stress injection runs before cleanup.
	// Defaults to 60s. Capped by safety.maxResourceExhaustionDuration.
	// +optional
	Duration *metav1.Duration `json:"duration,omitempty"`
}

// ScenarioSpec describes the chaos scenario to execute against the target.
type ScenarioSpec struct {
	// Type identifies the chaos scenario.
	Type ScenarioType `json:"type"`

	// Selection controls which pods within the target are disrupted.
	// +optional
	Selection SelectionSpec `json:"selection,omitempty"`

	// Action defines the disruptive action applied to selected pods.
	// +optional
	Action ActionSpec `json:"action,omitempty"`

	// Network describes network disruption parameters.
	// Required when Type is NetworkPartition or NetworkChaos.
	// +optional
	Network *NetworkChaosSpec `json:"network,omitempty"`

	// ResourceExhaustion describes resource stress parameters.
	// Required when Type is ResourceExhaustion.
	// +optional
	ResourceExhaustion *ResourceExhaustionSpec `json:"resourceExhaustion,omitempty"`
}

// FlinkRestObserve configures Flink REST API-based observation.
type FlinkRestObserve struct {
	// Enabled activates Flink REST API polling during the observation phase.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Endpoint is the base URL of the Flink REST API (e.g., http://flink-jobmanager:8081).
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// RequireStableCheckpointBeforeInject holds injection until the most recent
	// Flink checkpoint completed within CheckpointStableWindowSeconds.
	// Requires FlinkRest.Enabled=true and FlinkRest.Endpoint to be non-empty.
	// +optional
	RequireStableCheckpointBeforeInject bool `json:"requireStableCheckpointBeforeInject,omitempty"`

	// CheckpointStableWindowSeconds is the maximum age (in seconds) of the most
	// recent completed checkpoint for it to be considered "stable". Defaults to 60.
	// +kubebuilder:validation:Minimum=1
	// +optional
	CheckpointStableWindowSeconds int32 `json:"checkpointStableWindowSeconds,omitempty"`

	// CheckpointWaitTimeout is how long to wait for a stable checkpoint before
	// failing the run. Defaults to 5 minutes.
	// +optional
	CheckpointWaitTimeout *metav1.Duration `json:"checkpointWaitTimeout,omitempty"`
}

// ObserveSpec configures the post-injection observation phase.
type ObserveSpec struct {
	// Enabled activates the observation phase after injection.
	// Defaults to true.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Timeout is the maximum duration to wait for recovery before declaring the verdict.
	// Defaults to 10m.
	// +optional
	Timeout metav1.Duration `json:"timeout,omitempty"`

	// PollInterval is the frequency at which the operator checks recovery state.
	// Defaults to 5s. Must be less than or equal to Timeout.
	// +optional
	PollInterval metav1.Duration `json:"pollInterval,omitempty"`

	// FlinkRest configures observation via the Flink REST API.
	// +optional
	FlinkRest FlinkRestObserve `json:"flinkRest,omitempty"`
}

// SafetySpec defines guardrails that prevent unsafe chaos executions.
type SafetySpec struct {
	// DryRun logs what would happen without performing any destructive actions.
	// +optional
	DryRun bool `json:"dryRun,omitempty"`

	// MaxConcurrentRunsPerTarget limits the number of simultaneous ChaosRuns
	// targeting the same workload.
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxConcurrentRunsPerTarget int `json:"maxConcurrentRunsPerTarget,omitempty"`

	// MinTaskManagersRemaining is the minimum number of TaskManager pods that
	// must remain running after injection. The run is blocked if this threshold
	// would be violated. Defaults to 1. Set explicitly to 0 to allow killing
	// all TaskManagers (useful in single-TM test environments).
	// +kubebuilder:validation:Minimum=0
	// +optional
	MinTaskManagersRemaining *int32 `json:"minTaskManagersRemaining,omitempty"`

	// AllowSharedClusterImpact permits the run even when the target shares a
	// Kubernetes cluster with other workloads that may be affected.
	// +optional
	AllowSharedClusterImpact bool `json:"allowSharedClusterImpact,omitempty"`

	// MaxNetworkChaosDuration caps the duration field in NetworkChaosSpec.
	// Defaults to 5m. Runs requesting a longer duration are rejected.
	// +optional
	MaxNetworkChaosDuration *metav1.Duration `json:"maxNetworkChaosDuration,omitempty"`

	// AllowCheckpointStorageChaos permits scenarios targeting checkpoint storage.
	// Defaults to false due to potential data-loss risk. Requires explicit opt-in.
	// +optional
	AllowCheckpointStorageChaos bool `json:"allowCheckpointStorageChaos,omitempty"`

	// MaxResourceExhaustionDuration caps the duration field in ResourceExhaustionSpec.
	// Defaults to 5m. Runs requesting a longer duration are rejected.
	// +optional
	MaxResourceExhaustionDuration *metav1.Duration `json:"maxResourceExhaustionDuration,omitempty"`
}

// ControlSpec provides runtime control signals for an in-flight ChaosRun.
type ControlSpec struct {
	// Abort requests an immediate stop of the ChaosRun.
	// Setting this to true transitions the run to the Aborted phase.
	// +optional
	Abort bool `json:"abort,omitempty"`
}

// ChaosRunSpec defines the desired chaos experiment.
type ChaosRunSpec struct {
	// Target identifies the Flink workload to disrupt.
	Target TargetSpec `json:"target"`

	// Scenario describes the chaos scenario to execute.
	Scenario ScenarioSpec `json:"scenario"`

	// Observe configures the post-injection observation phase.
	// +optional
	Observe ObserveSpec `json:"observe,omitempty"`

	// Safety defines guardrails that prevent unsafe executions.
	// +optional
	Safety SafetySpec `json:"safety,omitempty"`

	// Control provides runtime signals for an in-flight run.
	// +optional
	Control ControlSpec `json:"control,omitempty"`
}

// TargetSummary is a concise snapshot of the resolved target written to status.
type TargetSummary struct {
	// Type is the resolved target type string.
	Type string `json:"type"`
	// Name is the resolved target name.
	Name string `json:"name"`
}

// ObservationStatus records Flink state measurements taken before and after injection.
type ObservationStatus struct {
	// JobStateBefore is the Flink job state sampled before injection.
	// +optional
	JobStateBefore string `json:"jobStateBefore,omitempty"`

	// JobStateAfter is the Flink job state sampled after injection.
	// +optional
	JobStateAfter string `json:"jobStateAfter,omitempty"`

	// TaskManagerCountBefore is the number of running TaskManager pods before injection.
	// +optional
	TaskManagerCountBefore int `json:"taskManagerCountBefore,omitempty"`

	// TaskManagerCountAfter is the number of running TaskManager pods after injection.
	// +optional
	TaskManagerCountAfter int `json:"taskManagerCountAfter,omitempty"`

	// RecoveryObservedAt is the timestamp when the workload was confirmed recovered.
	// +optional
	RecoveryObservedAt *metav1.Time `json:"recoveryObservedAt,omitempty"`
}

// EphemeralContainerRecord tracks a single ephemeral tc container injection.
type EphemeralContainerRecord struct {
	// PodName is the name of the target pod.
	PodName string `json:"podName"`
	// ContainerName is the name given to the ephemeral container.
	ContainerName string `json:"containerName"`
	// InjectedAt is when the ephemeral container was started.
	// +optional
	InjectedAt *metav1.Time `json:"injectedAt,omitempty"`
	// CleanedUp is true when the tc qdisc was confirmed deleted.
	// +optional
	CleanedUp bool `json:"cleanedUp,omitempty"`
}

// ChaosRunStatus describes the observed state of a ChaosRun.
type ChaosRunStatus struct {
	// Phase is the current lifecycle phase of the run.
	// +optional
	Phase RunPhase `json:"phase,omitempty"`

	// Verdict is the outcome of a completed run.
	// +optional
	Verdict RunVerdict `json:"verdict,omitempty"`

	// Message provides a human-readable description of the current state.
	// +optional
	Message string `json:"message,omitempty"`

	// StartedAt is the time the run transitioned out of the Pending phase.
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// ObservingStartedAt is the time the run transitioned into the Observing
	// phase. It is used as the anchor for the observation timeout so that
	// time spent waiting for a stable checkpoint does not reduce the window.
	// +optional
	ObservingStartedAt *metav1.Time `json:"observingStartedAt,omitempty"`

	// EndedAt is the time the run reached a terminal phase.
	// +optional
	EndedAt *metav1.Time `json:"endedAt,omitempty"`

	// SelectedPods lists the pod names chosen for injection.
	// +optional
	SelectedPods []string `json:"selectedPods,omitempty"`

	// InjectedPods lists the pod names on which the chaos action was successfully applied.
	// +optional
	InjectedPods []string `json:"injectedPods,omitempty"`

	// ReplacementObserved is true when replacement pods for all injected pods have been seen running.
	// +optional
	ReplacementObserved bool `json:"replacementObserved,omitempty"`

	// TargetSummary is a snapshot of the resolved target.
	// +optional
	TargetSummary *TargetSummary `json:"targetSummary,omitempty"`

	// Observation holds Flink state measurements from the observation phase.
	// +optional
	Observation *ObservationStatus `json:"observation,omitempty"`

	// NetworkPolicies lists the names of NetworkPolicy resources created by this run.
	// Used for label-based cleanup on completion or abort.
	// +optional
	NetworkPolicies []string `json:"networkPolicies,omitempty"`

	// EphemeralContainerInjections records per-pod ephemeral tc container state.
	// Used for cleanup tracking and crash-recovery re-attach.
	// +optional
	EphemeralContainerInjections []EphemeralContainerRecord `json:"ephemeralContainerInjections,omitempty"`

	// ResourceExhaustionInjections records per-pod stress ephemeral container state.
	// Used for cleanup tracking.
	// +optional
	ResourceExhaustionInjections []EphemeralContainerRecord `json:"resourceExhaustionInjections,omitempty"`

	// DryRunPreview describes what would be applied in dry-run mode.
	// +optional
	DryRunPreview string `json:"dryRunPreview,omitempty"`

	// Conditions is a list of standard Kubernetes status conditions.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ChaosRun is the Schema for the chaosruns API.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=cr;chaosrun
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Verdict",type=string,JSONPath=".status.verdict"
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=".status.targetSummary.name"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
type ChaosRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ChaosRunSpec   `json:"spec,omitempty"`
	Status ChaosRunStatus `json:"status,omitempty"`
}

// ChaosRunList contains a list of ChaosRun.
//
// +kubebuilder:object:root=true
type ChaosRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ChaosRun `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ChaosRun{}, &ChaosRunList{})
}
