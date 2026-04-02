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

// Package flinkdeployment provides a TargetResolver implementation for Apache
// Flink workloads managed by the Flink Kubernetes Operator. It uses the
// unstructured Kubernetes API to avoid a hard dependency on the Flink Operator
// Go module.
package flinkdeployment

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/interfaces"
	"github.com/flink-chaos-operator/internal/podutil"
)

// flinkDeploymentGVK is the GroupVersionKind of the FlinkDeployment resource
// as defined by the Apache Flink Kubernetes Operator.
var flinkDeploymentGVK = schema.GroupVersionKind{
	Group:   "flink.apache.org",
	Version: "v1beta1",
	Kind:    "FlinkDeployment",
}

// Resolver implements interfaces.TargetResolver for FlinkDeployment targets.
// It discovers TaskManager and JobManager pods by querying the Kubernetes API
// using standard pod label conventions set by the Flink Kubernetes Operator.
type Resolver struct {
	// Client is the controller-runtime client used to query Kubernetes resources.
	Client client.Client
}

// Resolve translates a ChaosRun targeting a FlinkDeployment into a
// ResolvedTarget containing live TM and JM pod names, platform metadata, and
// a shared-cluster flag derived from the FlinkDeployment's execution mode.
//
// It returns an error when:
//   - the run's target type is not FlinkDeployment,
//   - the named FlinkDeployment does not exist in the run's namespace, or
//   - no running/pending TaskManager pods are found.
func (r *Resolver) Resolve(ctx context.Context, run *v1alpha1.ChaosRun) (*interfaces.ResolvedTarget, error) {
	if run.Spec.Target.Type != v1alpha1.TargetFlinkDeployment {
		return nil, fmt.Errorf("resolver: expected target type %q, got %q",
			v1alpha1.TargetFlinkDeployment, run.Spec.Target.Type)
	}

	name := run.Spec.Target.Name
	ns := run.Namespace

	fd, err := r.fetchFlinkDeployment(ctx, name, ns)
	if err != nil {
		return nil, err
	}

	tmPods, jmPods, err := r.discoverPods(ctx, name, ns)
	if err != nil {
		return nil, err
	}

	if len(tmPods) == 0 {
		return nil, fmt.Errorf("no TaskManager pods found for FlinkDeployment %q", name)
	}

	sharedCluster := isSessionMode(fd)

	return &interfaces.ResolvedTarget{
		Platform:      "flink-operator",
		Namespace:     ns,
		LogicalName:   name,
		TMPodNames:    tmPods,
		JMPodNames:    jmPods,
		SharedCluster: sharedCluster,
	}, nil
}

// fetchFlinkDeployment retrieves the FlinkDeployment unstructured object.
// It returns a descriptive not-found error when the resource does not exist,
// and wraps transient API errors (network timeout, RBAC denial, etc.) with %w
// so callers can distinguish and log them correctly.
func (r *Resolver) fetchFlinkDeployment(ctx context.Context, name, namespace string) (*unstructured.Unstructured, error) {
	fd := &unstructured.Unstructured{}
	fd.SetGroupVersionKind(flinkDeploymentGVK)

	if err := r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, fd); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("FlinkDeployment %q not found in namespace %q", name, namespace)
		}
		return nil, fmt.Errorf("fetching FlinkDeployment %q in namespace %q: %w", name, namespace, err)
	}

	return fd, nil
}

// discoverPods lists all pods in the given namespace and partitions them into
// TaskManager and JobManager sets according to the label conventions used by
// the Flink Kubernetes Operator.
//
// A pod is included only when:
//   - its DeletionTimestamp is nil (not terminating), and
//   - its phase is Running or Pending.
func (r *Resolver) discoverPods(ctx context.Context, deploymentName, namespace string) (tmPods, jmPods []string, err error) {
	// List pods in the namespace. The Flink Kubernetes Operator uses either the
	// "app" label or the "app.kubernetes.io/name" label, so we cannot restrict
	// at the API server level without risking missed pods. The in-memory
	// matchesDeployment filter below handles both label conventions.
	var podList corev1.PodList
	if err := r.Client.List(ctx, &podList, client.InNamespace(namespace)); err != nil {
		return nil, nil, fmt.Errorf("resolver: listing pods in namespace %q: %w", namespace, err)
	}

	for i := range podList.Items {
		pod := &podList.Items[i]

		if !podutil.IsPodEligible(pod) {
			continue
		}

		if !matchesDeployment(pod.Labels, deploymentName) {
			continue
		}

		switch podutil.ComponentRole(pod.Labels) {
		case "taskmanager":
			tmPods = append(tmPods, pod.Name)
		case "jobmanager":
			jmPods = append(jmPods, pod.Name)
		}
	}

	return tmPods, jmPods, nil
}

// matchesDeployment returns true when the pod's labels identify it as belonging
// to the named FlinkDeployment. The Flink Kubernetes Operator sets either the
// "app" label or the "app.kubernetes.io/name" label on managed pods.
func matchesDeployment(labels map[string]string, deploymentName string) bool {
	return labels["app"] == deploymentName || labels["app.kubernetes.io/name"] == deploymentName
}

// isSessionMode inspects the FlinkDeployment's flinkConfiguration to determine
// whether the job runs in session mode. If spec.flinkConfiguration["execution.target"]
// contains "session" (case-insensitive), the deployment is treated as shared.
//
// Absent or non-string fields are treated as application mode (not shared).
func isSessionMode(fd *unstructured.Unstructured) bool {
	execTarget, found, err := unstructured.NestedString(fd.Object, "spec", "flinkConfiguration", "execution.target")
	if err != nil || !found {
		return false
	}
	return strings.Contains(strings.ToLower(execTarget), "session")
}
