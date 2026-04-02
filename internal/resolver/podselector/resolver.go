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

// Package podselector provides a TargetResolver implementation that targets
// pods matched by an arbitrary Kubernetes label selector. It is a raw escape
// hatch for workloads not managed by a supported Flink platform.
package podselector

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/interfaces"
	"github.com/flink-chaos-operator/internal/podutil"
)

// Resolver implements interfaces.TargetResolver for PodSelector targets.
// It discovers pods by evaluating the label selector from the run's target
// spec directly against the Kubernetes API, without requiring a specific Flink
// platform CRD.
type Resolver struct {
	// Client is the controller-runtime client used to query Kubernetes resources.
	Client client.Client
}

// Resolve translates a ChaosRun targeting a PodSelector into a ResolvedTarget
// containing live TM and JM pod names classified by well-known component
// labels.
//
// It returns an error when:
//   - the run's target type is not PodSelector,
//   - spec.target.selector is nil, or
//   - no running/pending pods match the selector in the run's namespace.
func (r *Resolver) Resolve(ctx context.Context, run *v1alpha1.ChaosRun) (*interfaces.ResolvedTarget, error) {
	if run.Spec.Target.Type != v1alpha1.TargetPodSelector {
		return nil, fmt.Errorf("resolver: expected target type %q, got %q",
			v1alpha1.TargetPodSelector, run.Spec.Target.Type)
	}

	if run.Spec.Target.Selector == nil {
		return nil, fmt.Errorf("PodSelector target requires spec.target.selector")
	}

	sel, err := metav1.LabelSelectorAsSelector(run.Spec.Target.Selector)
	if err != nil {
		return nil, fmt.Errorf("resolver: converting label selector: %w", err)
	}

	tmPods, jmPods, err := r.discoverPods(ctx, run.Namespace, sel)
	if err != nil {
		return nil, err
	}

	if len(tmPods) == 0 && len(jmPods) == 0 {
		return nil, fmt.Errorf("no pods matched selector in namespace %q", run.Namespace)
	}

	return &interfaces.ResolvedTarget{
		Platform:      "generic",
		Namespace:     run.Namespace,
		LogicalName:   "pod-selector",
		TMPodNames:    tmPods,
		JMPodNames:    jmPods,
		SharedCluster: false,
	}, nil
}

// discoverPods lists pods in the given namespace, filters to eligible pods
// matching the selector, and classifies them into TM and JM sets using the
// well-known component labels from the Flink and Ververica ecosystems.
//
// Pods that carry no recognised component label are treated as TaskManagers,
// since PodSelector is a raw escape hatch and the caller has explicitly
// selected the pods they want to target.
func (r *Resolver) discoverPods(ctx context.Context, namespace string, sel labels.Selector) (tmPods, jmPods []string, err error) {
	var podList corev1.PodList
	if err := r.Client.List(ctx, &podList,
		client.InNamespace(namespace),
		client.MatchingLabelsSelector{Selector: sel},
	); err != nil {
		return nil, nil, fmt.Errorf("resolver: listing pods in namespace %q: %w", namespace, err)
	}

	for i := range podList.Items {
		pod := &podList.Items[i]

		if !podutil.IsPodEligible(pod) {
			continue
		}

		switch podutil.ComponentRole(pod.Labels) {
		case "jobmanager":
			jmPods = append(jmPods, pod.Name)
		default:
			// "taskmanager" or any unrecognised value → treat as TM.
			tmPods = append(tmPods, pod.Name)
		}
	}

	return tmPods, jmPods, nil
}

