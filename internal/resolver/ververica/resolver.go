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

// Package ververica provides a TargetResolver implementation for Flink
// workloads managed by Ververica Platform. Resolution is Kubernetes-only:
// no Ververica Platform API calls are made. Pods are discovered by querying
// well-known labels that Ververica attaches to its managed pods.
package ververica

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/interfaces"
	"github.com/flink-chaos-operator/internal/podutil"
)

// Resolver implements interfaces.TargetResolver for VervericaDeployment
// targets. It discovers TaskManager and JobManager pods by querying the
// Kubernetes API using label conventions set by Ververica Platform.
type Resolver struct {
	// Client is the controller-runtime client used to query Kubernetes resources.
	Client client.Client
}

// Resolve translates a ChaosRun targeting a VervericaDeployment into a
// ResolvedTarget containing live TM and JM pod names, platform metadata, and
// a shared-cluster flag derived from pod labels.
//
// Resolution uses Kubernetes-only pod discovery — no Ververica Platform API
// calls are made. Pods are matched using the well-known labels that Ververica
// attaches to managed pods (both flink.apache.org/* and vvp.io/* prefixes).
//
// It returns an error when:
//   - the run's target type is not VervericaDeployment,
//   - neither DeploymentID nor DeploymentName is set on the target spec, or
//   - no running/pending TaskManager pods are found after filtering.
func (r *Resolver) Resolve(ctx context.Context, run *v1alpha1.ChaosRun) (*interfaces.ResolvedTarget, error) {
	if run.Spec.Target.Type != v1alpha1.TargetVervericaDeployment {
		return nil, fmt.Errorf("resolver: expected target type %q, got %q",
			v1alpha1.TargetVervericaDeployment, run.Spec.Target.Type)
	}

	deploymentID := run.Spec.Target.DeploymentID
	deploymentName := run.Spec.Target.DeploymentName
	vvpNamespace := run.Spec.Target.VVPNamespace

	if deploymentID == "" && deploymentName == "" {
		return nil, fmt.Errorf("resolver: VervericaDeployment target requires at least one of DeploymentID or DeploymentName")
	}

	// logicalName is used in error messages and as the ResolvedTarget name.
	// DeploymentID is the preferred identifier; fall back to DeploymentName.
	logicalName := deploymentID
	if logicalName == "" {
		logicalName = deploymentName
	}

	tmPods, jmPods, sharedCluster, err := r.discoverPods(ctx, run.Namespace, deploymentID, deploymentName, vvpNamespace)
	if err != nil {
		return nil, err
	}

	if len(tmPods) == 0 {
		return nil, fmt.Errorf("no TaskManager pods found for VervericaDeployment %q", logicalName)
	}

	return &interfaces.ResolvedTarget{
		Platform:      "ververica",
		Namespace:     run.Namespace,
		LogicalName:   logicalName,
		TMPodNames:    tmPods,
		JMPodNames:    jmPods,
		SharedCluster: sharedCluster,
	}, nil
}

// discoverPods lists all pods in the given namespace and partitions matching
// pods into TaskManager and JobManager sets. It also detects whether any
// matched pod carries a session-mode cluster label.
//
// When deploymentID is non-empty, pods are matched by deployment-id labels
// (primary path). When deploymentID is empty, pods are matched by
// deployment-name labels with an optional VVP namespace filter.
func (r *Resolver) discoverPods(
	ctx context.Context,
	namespace, deploymentID, deploymentName, vvpNamespace string,
) (tmPods, jmPods []string, sharedCluster bool, err error) {
	// List pods in the namespace. Ververica Platform uses either
	// flink.apache.org/deployment-id or vvp.io/deployment-id labels, so an
	// API-server-level label selector would only match one convention and miss
	// the other. The in-memory matchesTarget filter below handles both.
	var podList corev1.PodList
	if err := r.Client.List(ctx, &podList, client.InNamespace(namespace)); err != nil {
		return nil, nil, false, fmt.Errorf("resolver: listing pods in namespace %q: %w", namespace, err)
	}

	for i := range podList.Items {
		pod := &podList.Items[i]

		if !podutil.IsPodEligible(pod) {
			continue
		}

		if !matchesTarget(pod.Labels, deploymentID, deploymentName, vvpNamespace) {
			continue
		}

		if isSessionMode(pod.Labels) {
			sharedCluster = true
		}

		switch podutil.ComponentRole(pod.Labels) {
		case "taskmanager":
			tmPods = append(tmPods, pod.Name)
		case "jobmanager":
			jmPods = append(jmPods, pod.Name)
		}
	}

	return tmPods, jmPods, sharedCluster, nil
}

// matchesTarget returns true when a pod's labels identify it as belonging to
// the described Ververica deployment.
//
// Primary match (when deploymentID is non-empty): the pod must carry a
// deployment-id label. Three conventions are checked in order:
//   - plain camelCase "deploymentId" (VVP ≥ 2.x default)
//   - "flink.apache.org/deployment-id"
//   - "vvp.io/deployment-id"
//
// Fallback match (when deploymentID is empty): the pod must carry a
// deployment-name label. Three conventions are checked:
//   - plain camelCase "deploymentName"
//   - "flink.apache.org/deployment-name"
//   - "vvp.io/deployment-name"
//
// If vvpNamespace is non-empty the pod must additionally carry a matching
// namespace label ("vvpNamespace", "flink.apache.org/namespace", or
// "vvp.io/namespace").
func matchesTarget(labels map[string]string, deploymentID, deploymentName, vvpNamespace string) bool {
	if deploymentID != "" {
		return labels["deploymentId"] == deploymentID ||
			labels["flink.apache.org/deployment-id"] == deploymentID ||
			labels["vvp.io/deployment-id"] == deploymentID
	}

	nameMatch := labels["deploymentName"] == deploymentName ||
		labels["flink.apache.org/deployment-name"] == deploymentName ||
		labels["vvp.io/deployment-name"] == deploymentName

	if !nameMatch {
		return false
	}

	if vvpNamespace == "" {
		return true
	}

	return labels["vvpNamespace"] == vvpNamespace ||
		labels["flink.apache.org/namespace"] == vvpNamespace ||
		labels["vvp.io/namespace"] == vvpNamespace
}

// isSessionMode reports whether a pod's labels indicate that it belongs to a
// session-mode (shared) cluster. Both the flink.apache.org and vvp.io prefixed
// cluster-mode labels are checked.
func isSessionMode(labels map[string]string) bool {
	return labels["flink.apache.org/cluster-mode"] == "session" ||
		labels["vvp.io/cluster-mode"] == "session"
}
