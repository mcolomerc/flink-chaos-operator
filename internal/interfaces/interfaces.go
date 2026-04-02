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

// Package interfaces defines the pluggable contracts used across the
// Flink Chaos Operator. Keeping them in a dedicated package breaks import
// cycles: the controller, safety checker, and each driver/resolver can all
// import this package without importing each other.
package interfaces

import (
	"context"

	"github.com/flink-chaos-operator/api/v1alpha1"
)

// ResolvedTarget is the normalised output of any TargetResolver. It carries
// platform-specific pod lists and metadata needed by the safety checker,
// scenario drivers, and observers without those components needing to know
// which Flink platform was targeted.
type ResolvedTarget struct {
	// Platform identifies the Flink deployment platform:
	// "flink-operator" | "ververica" | "generic".
	Platform string

	// Namespace is the Kubernetes namespace that owns the target workload.
	Namespace string

	// LogicalName is the human-readable identifier of the resolved workload
	// (e.g. the FlinkDeployment resource name or Ververica deployment name).
	LogicalName string

	// TMPodNames lists the names of all running TaskManager pods discovered
	// during resolution. Scenario drivers and safety checks operate on this
	// slice.
	TMPodNames []string

	// JMPodNames lists the names of all running JobManager pods discovered
	// during resolution.
	JMPodNames []string

	// SharedCluster is true when the resolved target shares its Kubernetes
	// cluster with workloads outside of the chaos experiment scope.
	SharedCluster bool
}

// TargetResolver translates a ChaosRun target spec into a ResolvedTarget.
// Implementations exist for each supported Flink platform (Apache Flink
// Kubernetes Operator, Ververica Platform, and generic pod-selector).
type TargetResolver interface {
	Resolve(ctx context.Context, run *v1alpha1.ChaosRun) (*ResolvedTarget, error)
}

// InjectionResult carries the outcome of one injection pass performed by a
// ScenarioDriver.
type InjectionResult struct {
	// SelectedPods is the set of pod names chosen for injection (may be a
	// subset of all discovered pods depending on the selection spec).
	SelectedPods []string

	// InjectedPods is the set of pod names on which the chaos action was
	// successfully applied. It may differ from SelectedPods when an action
	// partially succeeds.
	InjectedPods []string
}

// ScenarioDriver executes a single chaos scenario injection against a
// resolved target. Implementations are scenario-specific (e.g.
// TaskManagerPodKill).
type ScenarioDriver interface {
	Inject(ctx context.Context, run *v1alpha1.ChaosRun, target *ResolvedTarget) (*InjectionResult, error)
}

// CleanableScenarioDriver is implemented by scenario drivers that create
// external resources (NetworkPolicies, ephemeral containers) requiring
// cleanup after the run completes or is aborted.
type CleanableScenarioDriver interface {
	ScenarioDriver
	Cleanup(ctx context.Context, run *v1alpha1.ChaosRun) error
}

// ObservationResult carries recovery signals collected during one poll cycle
// of the Observing phase.
type ObservationResult struct {
	// ReplacementObserved is true when at least one replacement pod for the
	// injected pods has been seen in a Running state.
	ReplacementObserved bool

	// AllReplacementsReady is true when all expected replacement pods are in a
	// Ready state, indicating full recovery.
	AllReplacementsReady bool

	// TMCountBefore is the number of TaskManager pods counted before the
	// chaos injection (snapshot from the injection phase).
	TMCountBefore int

	// TMCountAfter is the number of TaskManager pods currently running at the
	// time of this observation poll.
	TMCountAfter int

	// FlinkJobState is the Flink job state returned by the REST API (e.g. "RUNNING", "FAILING").
	// Empty when REST observation is not enabled.
	FlinkJobState string

	// CheckpointStatus is the completion status of the most recent checkpoint
	// ("COMPLETED", "FAILED", or "" when not available).
	CheckpointStatus string
}

// Observer polls for recovery signals during the Observing phase. Each call
// to Observe returns a point-in-time snapshot; the controller is responsible
// for scheduling subsequent polls and enforcing the observation timeout.
type Observer interface {
	Observe(ctx context.Context, run *v1alpha1.ChaosRun, target *ResolvedTarget) (*ObservationResult, error)
}
