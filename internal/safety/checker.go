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

// Package safety implements the pre-injection safety checks for the Flink
// Chaos Operator. It enforces namespace denylists, shared-cluster guards,
// minimum TaskManager thresholds, and per-target concurrency limits before
// any destructive action is permitted.
package safety

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/interfaces"
)

// deniedNamespaces lists Kubernetes system namespaces that must never be
// targeted by a chaos injection. These namespaces host cluster-critical
// components whose disruption could render the entire cluster unstable.
var deniedNamespaces = []string{
	"kube-system",
	"kube-public",
	"kube-node-lease",
}

// Checker runs all pre-injection safety checks for a ChaosRun. It requires a
// controller-runtime Client to query existing ChaosRun resources when
// enforcing the per-target concurrency limit.
type Checker struct {
	// Client is the controller-runtime client used to list ChaosRun resources
	// for the concurrency guard check.
	Client client.Client
}

// Check validates that it is safe to proceed with chaos injection for the
// given run and its resolved target. It returns a descriptive error if any
// check fails, or nil when all checks pass.
//
// The checks are performed in the following order:
//  1. Namespace denylist — the run's namespace must not be a system namespace.
//  2. Shared-cluster guard — if the target spans a shared cluster, the run
//     must explicitly permit shared-cluster impact.
//  3. Minimum TaskManagers remaining — injecting the requested number of pods
//     must not bring the live TM count below the configured minimum.
//  4. Concurrency guard — no more than MaxConcurrentRunsPerTarget active runs
//     may already be targeting the same logical workload.
//  5. Network chaos duration cap — duration must not exceed
//     safety.maxNetworkChaosDuration (network scenarios only).
//  6. Checkpoint storage chaos — TMtoCheckpoint target requires explicit
//     safety.allowCheckpointStorageChaos opt-in (network scenarios only).
//  7. Network partition blast radius — TMtoTM with direction=Both requires
//     safety.allowSharedClusterImpact opt-in (NetworkPartition only).
func (c *Checker) Check(ctx context.Context, run *v1alpha1.ChaosRun, target *interfaces.ResolvedTarget) error {
	if err := c.checkNamespaceDenylist(run); err != nil {
		return err
	}

	if err := c.checkSharedCluster(run, target); err != nil {
		return err
	}

	// Network scenarios disrupt traffic but do not kill pods, so the
	// "minimum TMs remaining" guard is not meaningful for them.
	if run.Spec.Scenario.Type == v1alpha1.ScenarioTaskManagerPodKill {
		if err := c.checkMinTaskManagersRemaining(run, target); err != nil {
			return err
		}
	}

	if err := c.checkConcurrency(ctx, run, target); err != nil {
		return err
	}

	// Network-chaos-specific checks (only run for network scenarios).
	netType := run.Spec.Scenario.Type
	if netType == v1alpha1.ScenarioNetworkPartition || netType == v1alpha1.ScenarioNetworkChaos {
		if err := c.checkNetworkChaosDuration(run); err != nil {
			return err
		}
		if run.Spec.Scenario.Network != nil {
			if err := c.checkCheckpointStorageChaos(run); err != nil {
				return err
			}
			if netType == v1alpha1.ScenarioNetworkPartition {
				if err := c.checkNetworkPartitionBlastRadius(run, target); err != nil {
					return err
				}
			}
		}
	}

	// Resource-exhaustion-specific checks.
	if run.Spec.Scenario.Type == v1alpha1.ScenarioResourceExhaustion {
		if err := c.checkResourceExhaustionDuration(run); err != nil {
			return err
		}
	}

	return nil
}

// checkNamespaceDenylist rejects runs whose namespace is a protected
// Kubernetes system namespace.
func (c *Checker) checkNamespaceDenylist(run *v1alpha1.ChaosRun) error {
	for _, ns := range deniedNamespaces {
		if run.Namespace == ns {
			return fmt.Errorf("namespace %q is in the denylist and may not be targeted for chaos injection", run.Namespace)
		}
	}
	return nil
}

// checkSharedCluster rejects runs that would impact a shared cluster unless
// the run spec explicitly opts in via AllowSharedClusterImpact.
func (c *Checker) checkSharedCluster(run *v1alpha1.ChaosRun, target *interfaces.ResolvedTarget) error {
	if target.SharedCluster && !run.Spec.Safety.AllowSharedClusterImpact {
		return fmt.Errorf(
			"target %q is on a shared cluster; set spec.safety.allowSharedClusterImpact=true to permit injection",
			target.LogicalName,
		)
	}
	return nil
}

// checkMinTaskManagersRemaining ensures that performing the requested
// injection would leave at least MinTaskManagersRemaining live TM pods.
//
// The effective injection count defaults to 1 when the selection count is
// zero (which should not occur after defaults are applied, but is guarded
// here for safety).
func (c *Checker) checkMinTaskManagersRemaining(run *v1alpha1.ChaosRun, target *interfaces.ResolvedTarget) error {
	count := int(run.Spec.Scenario.Selection.Count)
	if count == 0 {
		count = 1
	}

	remaining := len(target.TMPodNames) - count
	minRequired := run.Spec.Safety.MinTaskManagersRemaining

	if remaining < minRequired {
		return fmt.Errorf(
			"injecting %d pod(s) from %d available TaskManagers would leave %d remaining, "+
				"below the required minimum of %d (spec.safety.minTaskManagersRemaining)",
			count, len(target.TMPodNames), remaining, minRequired,
		)
	}

	return nil
}

// checkConcurrency lists all non-terminal ChaosRun resources in the same
// namespace that are already targeting the same logical workload and rejects
// the run if the count meets or exceeds MaxConcurrentRunsPerTarget.
//
// The run being reconciled is excluded from the count by name/namespace so
// that an in-flight run does not block its own re-queue.
func (c *Checker) checkConcurrency(ctx context.Context, run *v1alpha1.ChaosRun, target *interfaces.ResolvedTarget) error {
	var list v1alpha1.ChaosRunList
	if err := c.Client.List(ctx, &list, client.InNamespace(run.Namespace)); err != nil {
		return fmt.Errorf("concurrency guard: failed to list ChaosRuns in namespace %q: %w", run.Namespace, err)
	}

	active := 0
	for i := range list.Items {
		peer := &list.Items[i]

		// Skip the run being reconciled itself.
		if peer.Name == run.Name && peer.Namespace == run.Namespace {
			continue
		}

		// Skip runs in terminal phases.
		if isTerminal(peer.Status.Phase) {
			continue
		}

		// Count only runs that target the same logical workload.
		if peer.Status.TargetSummary != nil && peer.Status.TargetSummary.Name == target.LogicalName {
			active++
		}
	}

	max := run.Spec.Safety.MaxConcurrentRunsPerTarget
	if active >= max {
		return fmt.Errorf(
			"concurrency guard: %d active run(s) already target %q, "+
				"which meets or exceeds the limit of %d (spec.safety.maxConcurrentRunsPerTarget)",
			active, target.LogicalName, max,
		)
	}

	return nil
}

// checkNetworkChaosDuration rejects network chaos runs whose requested
// duration exceeds the safety cap defined in MaxNetworkChaosDuration.
// The check is skipped when either duration pointer is nil.
func (c *Checker) checkNetworkChaosDuration(run *v1alpha1.ChaosRun) error {
	net := run.Spec.Scenario.Network
	if net == nil || net.Duration == nil || run.Spec.Safety.MaxNetworkChaosDuration == nil {
		return nil
	}

	if net.Duration.Duration > run.Spec.Safety.MaxNetworkChaosDuration.Duration {
		return fmt.Errorf(
			"network chaos duration %s exceeds safety cap %s; "+
				"increase safety.maxNetworkChaosDuration or reduce network.duration",
			net.Duration.Duration,
			run.Spec.Safety.MaxNetworkChaosDuration.Duration,
		)
	}

	return nil
}

// checkCheckpointStorageChaos rejects runs that target checkpoint storage
// unless the run spec explicitly opts in via AllowCheckpointStorageChaos.
// Targeting checkpoint storage carries a data-loss risk that requires a
// deliberate operator acknowledgement.
func (c *Checker) checkCheckpointStorageChaos(run *v1alpha1.ChaosRun) error {
	if run.Spec.Scenario.Network == nil {
		return nil
	}

	if run.Spec.Scenario.Network.Target != v1alpha1.NetworkTargetTMtoCheckpoint {
		return nil
	}

	if !run.Spec.Safety.AllowCheckpointStorageChaos {
		return fmt.Errorf(
			"targeting checkpoint storage has high blast-radius risk (data loss possible); " +
				"set safety.allowCheckpointStorageChaos=true to confirm",
		)
	}

	return nil
}

// checkNetworkPartitionBlastRadius rejects NetworkPartition runs that target
// all TM-to-TM traffic in both directions unless the run explicitly opts in
// via AllowSharedClusterImpact. A bidirectional full-mesh partition fully
// isolates every TaskManager from its peers, which typically causes a total
// job failure regardless of cluster health.
func (c *Checker) checkNetworkPartitionBlastRadius(run *v1alpha1.ChaosRun, _ *interfaces.ResolvedTarget) error {
	net := run.Spec.Scenario.Network
	if net == nil {
		return nil
	}

	if net.Target == v1alpha1.NetworkTargetTMtoTM && net.Direction == v1alpha1.NetworkDirectionBoth {
		if !run.Spec.Safety.AllowSharedClusterImpact {
			return fmt.Errorf(
				"NetworkPartition with target=TMtoTM and direction=Both will fully isolate all " +
					"TaskManagers from each other; set safety.allowSharedClusterImpact=true to confirm",
			)
		}
	}

	return nil
}

// checkResourceExhaustionDuration rejects resource exhaustion runs whose
// requested duration exceeds the safety cap defined in
// MaxResourceExhaustionDuration. The check is skipped when either duration
// pointer is nil.
func (c *Checker) checkResourceExhaustionDuration(run *v1alpha1.ChaosRun) error {
	re := run.Spec.Scenario.ResourceExhaustion
	if re == nil || re.Duration == nil || run.Spec.Safety.MaxResourceExhaustionDuration == nil {
		return nil
	}

	if re.Duration.Duration > run.Spec.Safety.MaxResourceExhaustionDuration.Duration {
		return fmt.Errorf(
			"resource exhaustion duration %s exceeds safety cap %s; "+
				"increase safety.maxResourceExhaustionDuration or reduce resourceExhaustion.duration",
			re.Duration.Duration,
			run.Spec.Safety.MaxResourceExhaustionDuration.Duration,
		)
	}

	return nil
}

// isTerminal reports whether a RunPhase is a terminal lifecycle phase.
// This mirrors the controller's IsTerminal helper but is kept local to this
// package to avoid a circular import on the controller package.
func isTerminal(phase v1alpha1.RunPhase) bool {
	switch phase {
	case v1alpha1.PhaseCompleted, v1alpha1.PhaseAborted, v1alpha1.PhaseFailed:
		return true
	default:
		return false
	}
}
