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

// Package networkpartition implements the NetworkPartition chaos scenario
// driver. It creates Kubernetes NetworkPolicy resources to binary-block
// traffic between Flink components and removes them during cleanup.
package networkpartition

import (
	"context"
	"fmt"

	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/interfaces"
	"github.com/flink-chaos-operator/internal/netchaos/policy"
)

// tmLabels is the pod selector that matches TaskManager pods under both the
// Apache Flink Kubernetes Operator ("component": "taskmanager") and Ververica
// Platform conventions. The "component" label is the lowest-common-denominator
// key used by both platforms.
var tmLabels = map[string]string{"component": "taskmanager"}

// jmLabels is the pod selector that matches JobManager pods under both
// Flink Kubernetes Operator and Ververica Platform conventions.
var jmLabels = map[string]string{"component": "jobmanager"}

// Driver implements interfaces.ScenarioDriver and interfaces.CleanableScenarioDriver
// for the NetworkPartition scenario. It creates NetworkPolicy resources that
// block selected traffic paths between Flink components, and deletes them when
// the run completes or is aborted.
type Driver struct {
	// Client is the controller-runtime client used to create and delete
	// NetworkPolicy resources.
	Client client.Client
}

// New constructs a Driver backed by the given controller-runtime client.
func New(c client.Client) *Driver {
	return &Driver{Client: c}
}

// Inject implements interfaces.ScenarioDriver. It creates NetworkPolicy
// resources in run.Namespace according to the network partition specification,
// then populates run.Status with the names of the created policies.
//
// It returns an error when:
//   - the run's scenario type is not NetworkPartition,
//   - run.Spec.Scenario.Network is nil,
//   - TMtoCheckpoint or TMtoExternal is requested but ExternalEndpoint.CIDR is empty, or
//   - a Kubernetes API call fails for a reason other than AlreadyExists.
func (d *Driver) Inject(ctx context.Context, run *v1alpha1.ChaosRun, target *interfaces.ResolvedTarget) (*interfaces.InjectionResult, error) {
	if run.Spec.Scenario.Type != v1alpha1.ScenarioNetworkPartition {
		return nil, fmt.Errorf("driver supports %q scenario only, got %q",
			v1alpha1.ScenarioNetworkPartition, run.Spec.Scenario.Type)
	}

	netSpec := run.Spec.Scenario.Network
	if netSpec == nil {
		return nil, fmt.Errorf("network spec is required for NetworkPartition scenario")
	}

	srcLabels, destLabels, destCIDR, err := resolvePartitionPeers(netSpec)
	if err != nil {
		return nil, err
	}

	var port int32
	if netSpec.ExternalEndpoint != nil {
		port = netSpec.ExternalEndpoint.Port
	}

	ingressName, egressName := policy.Names(run.Name)

	// The policy builder uses a deny-all model. For label-based targets
	// (TMtoJM, TMtoTM), the policy selects the destination pods and uses empty
	// ingress/egress rules to deny all traffic to those pods. For CIDR-based
	// targets (TMtoCheckpoint, TMtoExternal), the policy selects the source
	// (TM) pods and allows all egress except the blocked CIDR, using
	// IPBlock.Except to deny egress to that specific CIDR while permitting all
	// other outbound traffic.
	//
	// For label-based targets (destLabels != nil): swap srcLabels → destLabels
	// so the NetworkPolicy selects the destination pods. The deny-all effect
	// is achieved through empty ingress/egress rule lists on those pods.
	policySourceLabels := srcLabels
	if destLabels != nil {
		policySourceLabels = destLabels
	}

	created, err := d.createPolicies(ctx, run, netSpec.Direction, ingressName, egressName, policy.Spec{
		Namespace:    run.Namespace,
		ChaosRunName: run.Name,
		SourceLabels: policySourceLabels,
		DestLabels:   destLabels,
		DestCIDR:     destCIDR,
		Direction:    string(netSpec.Direction),
		Port:         port,
	})
	if err != nil {
		return nil, err
	}

	run.Status.NetworkPolicies = append(run.Status.NetworkPolicies, created...)
	run.Status.SelectedPods = target.TMPodNames

	return &interfaces.InjectionResult{
		SelectedPods: target.TMPodNames,
		InjectedPods: target.TMPodNames,
	}, nil
}

// Cleanup implements interfaces.CleanableScenarioDriver. It lists all
// NetworkPolicy resources in run.Namespace labelled with
// chaos.flink.io/run-name=<run.Name> and deletes each one. Resources that are
// already gone are silently skipped. On success, run.Status.NetworkPolicies is
// set to nil; the caller is responsible for patching the status sub-resource.
func (d *Driver) Cleanup(ctx context.Context, run *v1alpha1.ChaosRun) error {
	var list networkingv1.NetworkPolicyList
	if err := d.Client.List(ctx, &list,
		client.InNamespace(run.Namespace),
		client.MatchingLabels{"chaos.flink.io/run-name": run.Name},
	); err != nil {
		return fmt.Errorf("listing NetworkPolicies for run %q: %w", run.Name, err)
	}

	for i := range list.Items {
		np := &list.Items[i]
		if err := d.Client.Delete(ctx, np); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("deleting NetworkPolicy %q: %w", np.Name, err)
		}
	}

	run.Status.NetworkPolicies = nil

	return nil
}

// resolvePartitionPeers derives the source labels, destination labels, and
// destination CIDR for the partition based on the NetworkChaosSpec.Target.
func resolvePartitionPeers(netSpec *v1alpha1.NetworkChaosSpec) (srcLabels, destLabels map[string]string, destCIDR string, err error) {
	switch netSpec.Target {
	case v1alpha1.NetworkTargetTMtoTM:
		// Both source and destination are TaskManagers.
		return tmLabels, tmLabels, "", nil

	case v1alpha1.NetworkTargetTMtoJM:
		return tmLabels, jmLabels, "", nil

	case v1alpha1.NetworkTargetTMtoCheckpoint, v1alpha1.NetworkTargetTMtoExternal:
		if netSpec.ExternalEndpoint == nil || netSpec.ExternalEndpoint.CIDR == "" {
			return nil, nil, "", fmt.Errorf(
				"network target %q requires ExternalEndpoint.CIDR to be set",
				netSpec.Target,
			)
		}
		return tmLabels, nil, netSpec.ExternalEndpoint.CIDR, nil

	default:
		return nil, nil, "", fmt.Errorf("unsupported network target %q", netSpec.Target)
	}
}

// createPolicies creates the NetworkPolicy resources for the requested
// direction and returns the names of all policies that were created (or
// already existed). The caller decides how to record them in status.
func (d *Driver) createPolicies(
	ctx context.Context,
	run *v1alpha1.ChaosRun,
	direction v1alpha1.NetworkDirection,
	ingressName, egressName string,
	baseSpec policy.Spec,
) ([]string, error) {
	type entry struct {
		name      string
		direction string
	}

	var toCreate []entry

	switch direction {
	case v1alpha1.NetworkDirectionIngress:
		toCreate = []entry{{ingressName, "Ingress"}}
	case v1alpha1.NetworkDirectionEgress:
		toCreate = []entry{{egressName, "Egress"}}
	default:
		// Both (default)
		toCreate = []entry{
			{ingressName, "Ingress"},
			{egressName, "Egress"},
		}
	}

	created := make([]string, 0, len(toCreate))

	for _, e := range toCreate {
		spec := baseSpec
		spec.Name = e.name
		spec.Direction = e.direction

		np := policy.Build(spec)

		if err := d.Client.Create(ctx, np); err != nil {
			if apierrors.IsAlreadyExists(err) {
				// Idempotent: the policy already exists from a previous attempt.
				created = append(created, e.name)
				continue
			}
			return created, fmt.Errorf("creating NetworkPolicy %q: %w", e.name, err)
		}

		created = append(created, e.name)
	}

	return created, nil
}
