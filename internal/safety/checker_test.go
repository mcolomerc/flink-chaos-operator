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

package safety_test

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/interfaces"
	"github.com/flink-chaos-operator/internal/safety"
)

// buildScheme returns a runtime.Scheme that includes the v1alpha1 types,
// which is required for the fake client to handle ChaosRun objects.
func buildScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("failed to add v1alpha1 to scheme: %v", err)
	}
	return s
}

// newRun is a convenience constructor for a minimal ChaosRun with the given
// namespace and name. Callers apply further mutations before passing it to
// the checker.
func newRun(namespace, name string) *v1alpha1.ChaosRun {
	return &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.ChaosRunSpec{
			Target: v1alpha1.TargetSpec{
				Type: v1alpha1.TargetFlinkDeployment,
				Name: "my-flink-job",
			},
			Scenario: v1alpha1.ScenarioSpec{
				Type: v1alpha1.ScenarioTaskManagerPodKill,
				Selection: v1alpha1.SelectionSpec{
					Mode:  v1alpha1.SelectionModeRandom,
					Count: 1,
				},
			},
			Safety: v1alpha1.SafetySpec{
				MaxConcurrentRunsPerTarget: 1,
				MinTaskManagersRemaining:   1,
			},
		},
	}
}

// newTarget returns a minimal ResolvedTarget pointing at the given logical
// name with the supplied TM pod names.
func newTarget(logicalName string, tmPods []string) *interfaces.ResolvedTarget {
	return &interfaces.ResolvedTarget{
		Platform:    "flink-operator",
		Namespace:   "default",
		LogicalName: logicalName,
		TMPodNames:  tmPods,
	}
}

// newChecker builds a safety.Checker backed by a fake client pre-populated
// with the supplied existing ChaosRun objects.
func newChecker(t *testing.T, existing ...*v1alpha1.ChaosRun) *safety.Checker {
	t.Helper()
	scheme := buildScheme(t)

	objs := make([]runtime.Object, len(existing))
	for i, r := range existing {
		objs[i] = r
	}

	fc := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objs...).
		Build()

	return &safety.Checker{Client: fc}
}

// ---------------------------------------------------------------------------
// Namespace denylist tests
// ---------------------------------------------------------------------------

func TestCheck_NamespaceDenylist_Rejected(t *testing.T) {
	deniedNamespaces := []string{"kube-system", "kube-public", "kube-node-lease"}

	for _, ns := range deniedNamespaces {
		ns := ns
		t.Run(ns, func(t *testing.T) {
			run := newRun(ns, "chaos-run")
			target := newTarget("my-flink-job", []string{"tm-0", "tm-1", "tm-2"})

			checker := newChecker(t)
			err := checker.Check(context.Background(), run, target)
			if err == nil {
				t.Errorf("expected an error for denied namespace %q, got nil", ns)
			}
		})
	}
}

func TestCheck_NamespaceDenylist_Allowed(t *testing.T) {
	run := newRun("default", "chaos-run")
	target := newTarget("my-flink-job", []string{"tm-0", "tm-1"})

	checker := newChecker(t)
	if err := checker.Check(context.Background(), run, target); err != nil {
		t.Errorf("expected no error for namespace %q, got: %v", run.Namespace, err)
	}
}

// ---------------------------------------------------------------------------
// Shared-cluster guard tests
// ---------------------------------------------------------------------------

func TestCheck_SharedCluster_RejectedWhenNotAllowed(t *testing.T) {
	run := newRun("default", "chaos-run")
	run.Spec.Safety.AllowSharedClusterImpact = false

	target := newTarget("my-flink-job", []string{"tm-0", "tm-1"})
	target.SharedCluster = true

	checker := newChecker(t)
	err := checker.Check(context.Background(), run, target)
	if err == nil {
		t.Error("expected an error for shared cluster without AllowSharedClusterImpact, got nil")
	}
}

func TestCheck_SharedCluster_AllowedWhenFlagSet(t *testing.T) {
	run := newRun("default", "chaos-run")
	run.Spec.Safety.AllowSharedClusterImpact = true

	target := newTarget("my-flink-job", []string{"tm-0", "tm-1"})
	target.SharedCluster = true

	checker := newChecker(t)
	if err := checker.Check(context.Background(), run, target); err != nil {
		t.Errorf("expected no error when AllowSharedClusterImpact=true, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Min TaskManagers remaining tests
// ---------------------------------------------------------------------------

func TestCheck_MinTMRemaining_RejectedWhenBelowThreshold(t *testing.T) {
	// 2 TM pods, count=1, minRemaining=2 => 2-1=1 < 2 => rejected.
	run := newRun("default", "chaos-run")
	run.Spec.Scenario.Selection.Count = 1
	run.Spec.Safety.MinTaskManagersRemaining = 2

	target := newTarget("my-flink-job", []string{"tm-0", "tm-1"})

	checker := newChecker(t)
	err := checker.Check(context.Background(), run, target)
	if err == nil {
		t.Error("expected an error when remaining TMs would fall below minimum, got nil")
	}
}

func TestCheck_MinTMRemaining_PassesWhenEnoughPodsRemain(t *testing.T) {
	// 3 TM pods, count=1, minRemaining=2 => 3-1=2 >= 2 => allowed.
	run := newRun("default", "chaos-run")
	run.Spec.Scenario.Selection.Count = 1
	run.Spec.Safety.MinTaskManagersRemaining = 2

	target := newTarget("my-flink-job", []string{"tm-0", "tm-1", "tm-2"})

	checker := newChecker(t)
	if err := checker.Check(context.Background(), run, target); err != nil {
		t.Errorf("expected no error when enough TMs remain, got: %v", err)
	}
}

func TestCheck_MinTMRemaining_DefaultsCountToOneWhenZero(t *testing.T) {
	// count=0 should be treated as 1.
	// 1 TM pod, effective count=1, minRemaining=1 => 1-1=0 < 1 => rejected.
	run := newRun("default", "chaos-run")
	run.Spec.Scenario.Selection.Count = 0
	run.Spec.Safety.MinTaskManagersRemaining = 1

	target := newTarget("my-flink-job", []string{"tm-0"})

	checker := newChecker(t)
	err := checker.Check(context.Background(), run, target)
	if err == nil {
		t.Error("expected an error when count defaults to 1 and no TMs remain, got nil")
	}
}

// ---------------------------------------------------------------------------
// Concurrency guard tests
// ---------------------------------------------------------------------------

func TestCheck_Concurrency_RejectedWhenMaxReached(t *testing.T) {
	// An existing active run already targets the same workload.
	existing := newRun("default", "existing-run")
	existing.Status.Phase = v1alpha1.PhaseObserving
	existing.Status.TargetSummary = &v1alpha1.TargetSummary{
		Type: "flink-operator",
		Name: "my-flink-job",
	}

	run := newRun("default", "chaos-run")
	run.Spec.Safety.MaxConcurrentRunsPerTarget = 1

	target := newTarget("my-flink-job", []string{"tm-0", "tm-1", "tm-2"})

	checker := newChecker(t, existing)
	err := checker.Check(context.Background(), run, target)
	if err == nil {
		t.Error("expected an error when max concurrent runs is reached, got nil")
	}
}

func TestCheck_Concurrency_PassesOnFirstRun(t *testing.T) {
	// No existing runs — the first run for this target must always be allowed.
	run := newRun("default", "chaos-run")
	run.Spec.Safety.MaxConcurrentRunsPerTarget = 1

	target := newTarget("my-flink-job", []string{"tm-0", "tm-1", "tm-2"})

	checker := newChecker(t)
	if err := checker.Check(context.Background(), run, target); err != nil {
		t.Errorf("expected no error on first run for target, got: %v", err)
	}
}

func TestCheck_Concurrency_TerminalRunsAreNotCounted(t *testing.T) {
	// A terminal run for the same target must not count against the limit.
	completed := newRun("default", "old-run")
	completed.Status.Phase = v1alpha1.PhaseCompleted
	completed.Status.TargetSummary = &v1alpha1.TargetSummary{
		Type: "flink-operator",
		Name: "my-flink-job",
	}

	run := newRun("default", "chaos-run")
	run.Spec.Safety.MaxConcurrentRunsPerTarget = 1

	target := newTarget("my-flink-job", []string{"tm-0", "tm-1", "tm-2"})

	checker := newChecker(t, completed)
	if err := checker.Check(context.Background(), run, target); err != nil {
		t.Errorf("expected no error when only terminal runs exist, got: %v", err)
	}
}

func TestCheck_Concurrency_SelfIsNotCounted(t *testing.T) {
	// The run being reconciled may already exist in the store (e.g. during a
	// re-queue). It must not count against its own concurrency limit.
	run := newRun("default", "chaos-run")
	run.Spec.Safety.MaxConcurrentRunsPerTarget = 1
	run.Status.Phase = v1alpha1.PhaseInjecting
	run.Status.TargetSummary = &v1alpha1.TargetSummary{
		Type: "flink-operator",
		Name: "my-flink-job",
	}

	target := newTarget("my-flink-job", []string{"tm-0", "tm-1", "tm-2"})

	checker := newChecker(t, run)
	if err := checker.Check(context.Background(), run, target); err != nil {
		t.Errorf("expected no error when the run only sees itself, got: %v", err)
	}
}

func TestCheck_Concurrency_DifferentTargetNotCounted(t *testing.T) {
	// An active run targeting a different workload must not affect the count
	// for the run under test.
	other := newRun("default", "other-run")
	other.Status.Phase = v1alpha1.PhaseObserving
	other.Status.TargetSummary = &v1alpha1.TargetSummary{
		Type: "flink-operator",
		Name: "different-flink-job",
	}

	run := newRun("default", "chaos-run")
	run.Spec.Safety.MaxConcurrentRunsPerTarget = 1

	target := newTarget("my-flink-job", []string{"tm-0", "tm-1", "tm-2"})

	checker := newChecker(t, other)
	if err := checker.Check(context.Background(), run, target); err != nil {
		t.Errorf("expected no error when active run targets a different workload, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Network chaos duration cap tests
// ---------------------------------------------------------------------------

func TestCheck_NetworkChaosDuration_ExceedsCap_Rejected(t *testing.T) {
	run := newRun("default", "chaos-run")
	run.Spec.Scenario.Type = v1alpha1.ScenarioNetworkChaos
	run.Spec.Scenario.Network = &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTargetTMtoJM,
		Direction: v1alpha1.NetworkDirectionBoth,
		Duration:  &metav1.Duration{Duration: 10 * time.Minute},
	}
	run.Spec.Safety.MaxNetworkChaosDuration = &metav1.Duration{Duration: 5 * time.Minute}

	target := newTarget("my-flink-job", []string{"tm-0", "tm-1", "tm-2"})

	checker := newChecker(t)
	err := checker.Check(context.Background(), run, target)
	if err == nil {
		t.Error("expected an error when network chaos duration exceeds cap, got nil")
	}
}

func TestCheck_NetworkChaosDuration_EqualsCap_Passes(t *testing.T) {
	run := newRun("default", "chaos-run")
	run.Spec.Scenario.Type = v1alpha1.ScenarioNetworkChaos
	run.Spec.Scenario.Network = &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTargetTMtoJM,
		Direction: v1alpha1.NetworkDirectionBoth,
		Duration:  &metav1.Duration{Duration: 5 * time.Minute},
	}
	run.Spec.Safety.MaxNetworkChaosDuration = &metav1.Duration{Duration: 5 * time.Minute}

	target := newTarget("my-flink-job", []string{"tm-0", "tm-1", "tm-2"})

	checker := newChecker(t)
	if err := checker.Check(context.Background(), run, target); err != nil {
		t.Errorf("expected no error when duration equals cap, got: %v", err)
	}
}

func TestCheck_NetworkChaosDuration_UnderCap_Passes(t *testing.T) {
	run := newRun("default", "chaos-run")
	run.Spec.Scenario.Type = v1alpha1.ScenarioNetworkChaos
	run.Spec.Scenario.Network = &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTargetTMtoJM,
		Direction: v1alpha1.NetworkDirectionBoth,
		Duration:  &metav1.Duration{Duration: 2 * time.Minute},
	}
	run.Spec.Safety.MaxNetworkChaosDuration = &metav1.Duration{Duration: 5 * time.Minute}

	target := newTarget("my-flink-job", []string{"tm-0", "tm-1", "tm-2"})

	checker := newChecker(t)
	if err := checker.Check(context.Background(), run, target); err != nil {
		t.Errorf("expected no error when duration is under cap, got: %v", err)
	}
}

func TestCheck_NetworkChaosDuration_NonNetworkScenario_Skipped(t *testing.T) {
	// TaskManagerPodKill is not a network scenario; the duration cap must not apply.
	run := newRun("default", "chaos-run")
	run.Spec.Scenario.Type = v1alpha1.ScenarioTaskManagerPodKill
	run.Spec.Safety.MaxNetworkChaosDuration = &metav1.Duration{Duration: 1 * time.Second}
	// No Network field — duration check would panic if it ran.

	target := newTarget("my-flink-job", []string{"tm-0", "tm-1", "tm-2"})

	checker := newChecker(t)
	if err := checker.Check(context.Background(), run, target); err != nil {
		t.Errorf("expected no error for non-network scenario, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Checkpoint storage chaos tests
// ---------------------------------------------------------------------------

func TestCheck_CheckpointStorageChaos_FlagFalse_Rejected(t *testing.T) {
	run := newRun("default", "chaos-run")
	run.Spec.Scenario.Type = v1alpha1.ScenarioNetworkPartition
	run.Spec.Scenario.Network = &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTargetTMtoCheckpoint,
		Direction: v1alpha1.NetworkDirectionBoth,
	}
	run.Spec.Safety.AllowCheckpointStorageChaos = false
	run.Spec.Safety.AllowSharedClusterImpact = true // avoid other guards firing

	target := newTarget("my-flink-job", []string{"tm-0", "tm-1", "tm-2"})

	checker := newChecker(t)
	err := checker.Check(context.Background(), run, target)
	if err == nil {
		t.Error("expected an error when targeting checkpoint storage without flag, got nil")
	}
}

func TestCheck_CheckpointStorageChaos_FlagTrue_Passes(t *testing.T) {
	run := newRun("default", "chaos-run")
	run.Spec.Scenario.Type = v1alpha1.ScenarioNetworkPartition
	run.Spec.Scenario.Network = &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTargetTMtoCheckpoint,
		Direction: v1alpha1.NetworkDirectionBoth,
	}
	run.Spec.Safety.AllowCheckpointStorageChaos = true
	run.Spec.Safety.AllowSharedClusterImpact = true

	target := newTarget("my-flink-job", []string{"tm-0", "tm-1", "tm-2"})

	checker := newChecker(t)
	if err := checker.Check(context.Background(), run, target); err != nil {
		t.Errorf("expected no error when allowCheckpointStorageChaos=true, got: %v", err)
	}
}

func TestCheck_CheckpointStorageChaos_TMtoTM_Skipped(t *testing.T) {
	// TMtoTM target must not trigger the checkpoint storage check even if
	// allowCheckpointStorageChaos is false.
	run := newRun("default", "chaos-run")
	run.Spec.Scenario.Type = v1alpha1.ScenarioNetworkPartition
	run.Spec.Scenario.Network = &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTargetTMtoTM,
		Direction: v1alpha1.NetworkDirectionEgress,
	}
	run.Spec.Safety.AllowCheckpointStorageChaos = false
	run.Spec.Safety.AllowSharedClusterImpact = true

	target := newTarget("my-flink-job", []string{"tm-0", "tm-1", "tm-2"})

	checker := newChecker(t)
	if err := checker.Check(context.Background(), run, target); err != nil {
		t.Errorf("expected no error for TMtoTM target, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Network partition blast radius tests
// ---------------------------------------------------------------------------

func TestCheck_NetworkPartitionBlastRadius_TMtoTM_Both_FlagFalse_Rejected(t *testing.T) {
	run := newRun("default", "chaos-run")
	run.Spec.Scenario.Type = v1alpha1.ScenarioNetworkPartition
	run.Spec.Scenario.Network = &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTargetTMtoTM,
		Direction: v1alpha1.NetworkDirectionBoth,
	}
	run.Spec.Safety.AllowSharedClusterImpact = false

	target := newTarget("my-flink-job", []string{"tm-0", "tm-1", "tm-2"})

	checker := newChecker(t)
	err := checker.Check(context.Background(), run, target)
	if err == nil {
		t.Error("expected an error for TMtoTM+Both without allowSharedClusterImpact, got nil")
	}
}

func TestCheck_NetworkPartitionBlastRadius_TMtoTM_Both_FlagTrue_Passes(t *testing.T) {
	run := newRun("default", "chaos-run")
	run.Spec.Scenario.Type = v1alpha1.ScenarioNetworkPartition
	run.Spec.Scenario.Network = &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTargetTMtoTM,
		Direction: v1alpha1.NetworkDirectionBoth,
	}
	run.Spec.Safety.AllowSharedClusterImpact = true

	target := newTarget("my-flink-job", []string{"tm-0", "tm-1", "tm-2"})

	checker := newChecker(t)
	if err := checker.Check(context.Background(), run, target); err != nil {
		t.Errorf("expected no error when allowSharedClusterImpact=true, got: %v", err)
	}
}

func TestCheck_NetworkPartitionBlastRadius_TMtoTM_EgressOnly_Passes(t *testing.T) {
	// Egress-only does not trigger the Both-direction blast-radius guard.
	run := newRun("default", "chaos-run")
	run.Spec.Scenario.Type = v1alpha1.ScenarioNetworkPartition
	run.Spec.Scenario.Network = &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTargetTMtoTM,
		Direction: v1alpha1.NetworkDirectionEgress,
	}
	run.Spec.Safety.AllowSharedClusterImpact = false

	target := newTarget("my-flink-job", []string{"tm-0", "tm-1", "tm-2"})

	checker := newChecker(t)
	if err := checker.Check(context.Background(), run, target); err != nil {
		t.Errorf("expected no error for Egress-only direction, got: %v", err)
	}
}

func TestCheck_NetworkPartitionBlastRadius_TMtoJM_Skipped(t *testing.T) {
	// TMtoJM target must not trigger the blast-radius guard regardless of
	// the AllowSharedClusterImpact flag.
	run := newRun("default", "chaos-run")
	run.Spec.Scenario.Type = v1alpha1.ScenarioNetworkPartition
	run.Spec.Scenario.Network = &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTargetTMtoJM,
		Direction: v1alpha1.NetworkDirectionBoth,
	}
	run.Spec.Safety.AllowSharedClusterImpact = false

	target := newTarget("my-flink-job", []string{"tm-0", "tm-1", "tm-2"})

	checker := newChecker(t)
	if err := checker.Check(context.Background(), run, target); err != nil {
		t.Errorf("expected no error for TMtoJM target, got: %v", err)
	}
}
