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

// Package resourceexhaustion implements the ResourceExhaustion scenario driver.
// It injects stress-ng into TaskManager pods using Kubernetes ephemeral
// containers to exhaust CPU or memory for a configurable duration.
package resourceexhaustion

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/ephemeral"
	"github.com/flink-chaos-operator/internal/interfaces"
	"github.com/flink-chaos-operator/internal/scenario/resourceexhaustion/stress"
)

// defaultDuration is used when the resource exhaustion spec duration is not set.
const defaultDuration = 60 * time.Second

// Driver implements chaos injection via ephemeral containers running stress-ng.
// It requires a Kubernetes client that can call the /ephemeralcontainers subresource.
type Driver struct {
	// Client is the controller-runtime client for status updates and pod reads.
	Client client.Client
	// KubeClient is the typed client-go clientset for ephemeral container injection.
	// Using the typed client because controller-runtime does not support the
	// /ephemeralcontainers subresource directly.
	KubeClient kubernetes.Interface
	// StressImage is the container image that provides the stress-ng binary.
	// Example: "ghcr.io/flink-chaos-operator/stress-tools:latest"
	StressImage string
}

// New constructs a Driver with the provided clients and stress-ng tool image.
func New(c client.Client, kube kubernetes.Interface, image string) *Driver {
	return &Driver{
		Client:      c,
		KubeClient:  kube,
		StressImage: image,
	}
}

// Inject implements interfaces.ScenarioDriver. It injects ephemeral stress-ng
// containers into each selected TaskManager pod. The method is idempotent: if
// ResourceExhaustionInjections is already populated the existing result is
// returned without re-injecting.
func (d *Driver) Inject(ctx context.Context, run *v1alpha1.ChaosRun,
	target *interfaces.ResolvedTarget) (*interfaces.InjectionResult, error) {

	if run.Spec.Scenario.Type != v1alpha1.ScenarioResourceExhaustion {
		return nil, fmt.Errorf("driver supports %q scenario only, got %q",
			v1alpha1.ScenarioResourceExhaustion, run.Spec.Scenario.Type)
	}

	spec := run.Spec.Scenario.ResourceExhaustion
	if spec == nil {
		return nil, fmt.Errorf("resourceExhaustion spec is required for %q scenario",
			v1alpha1.ScenarioResourceExhaustion)
	}

	// Idempotency: if injections have already been recorded, return the
	// result derived from the existing status without re-injecting.
	if len(run.Status.ResourceExhaustionInjections) > 0 {
		injected := make([]string, 0, len(run.Status.ResourceExhaustionInjections))
		for _, rec := range run.Status.ResourceExhaustionInjections {
			injected = append(injected, rec.PodName)
		}
		return &interfaces.InjectionResult{
			SelectedPods: run.Status.SelectedPods,
			InjectedPods: injected,
		}, nil
	}

	podNames := run.Status.SelectedPods
	if len(podNames) == 0 {
		podNames = target.TMPodNames
	}

	cmd := stress.Args(spec)
	injectedPods := make([]string, 0, len(podNames))

	for i, podName := range podNames {
		containerName := ephemeral.ContainerName("fchaos-stress-", run.Name, i)

		ephCont := corev1.EphemeralContainer{
			EphemeralContainerCommon: corev1.EphemeralContainerCommon{
				Name:    containerName,
				Image:   d.StressImage,
				Command: cmd,
			},
		}

		pod := &corev1.Pod{}
		if err := d.Client.Get(ctx, types.NamespacedName{Name: podName, Namespace: run.Namespace}, pod); err != nil {
			return &interfaces.InjectionResult{
				SelectedPods: podNames,
				InjectedPods: injectedPods,
			}, fmt.Errorf("fetching pod %s: %w", podName, err)
		}

		pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, ephCont)
		if _, err := d.KubeClient.CoreV1().Pods(run.Namespace).UpdateEphemeralContainers(
			ctx, podName, pod, metav1.UpdateOptions{}); err != nil {
			return &interfaces.InjectionResult{
				SelectedPods: podNames,
				InjectedPods: injectedPods,
			}, fmt.Errorf("injecting ephemeral container into %s: %w", podName, err)
		}

		now := metav1.Now()
		run.Status.ResourceExhaustionInjections = append(run.Status.ResourceExhaustionInjections,
			v1alpha1.EphemeralContainerRecord{
				PodName:       podName,
				ContainerName: containerName,
				InjectedAt:    &now,
			})

		injectedPods = append(injectedPods, podName)
	}

	return &interfaces.InjectionResult{
		SelectedPods: podNames,
		InjectedPods: injectedPods,
	}, nil
}

// Cleanup implements the cleanup half of interfaces.CleanableScenarioDriver.
// For each unclean record it checks whether the injection duration has elapsed
// or the container has already terminated. If so, the record is marked cleaned
// up. If the pod is not found the record is also cleaned up because the pod
// deletion removes the ephemeral container along with it.
func (d *Driver) Cleanup(ctx context.Context, run *v1alpha1.ChaosRun) error {
	spec := run.Spec.Scenario.ResourceExhaustion
	duration := resolveDuration(spec)

	for i := range run.Status.ResourceExhaustionInjections {
		rec := &run.Status.ResourceExhaustionInjections[i]
		if rec.CleanedUp {
			continue
		}

		pod := &corev1.Pod{}
		err := d.Client.Get(ctx, types.NamespacedName{Name: rec.PodName, Namespace: run.Namespace}, pod)
		if err != nil {
			if apierrors.IsNotFound(err) {
				// Pod was deleted; stress process is gone with it.
				rec.CleanedUp = true
				continue
			}
			return fmt.Errorf("fetching pod %s during cleanup: %w", rec.PodName, err)
		}

		// Check whether the ephemeral container has already terminated naturally
		// (stress-ng exits after its --timeout).
		containerState := ephemeral.FindContainerState(pod, rec.ContainerName)
		if containerState != nil && containerState.Terminated != nil {
			rec.CleanedUp = true
			continue
		}

		// Pragmatic approach: check if the injection duration has elapsed.
		// stress-ng exits automatically after --timeout, so once the configured
		// duration has passed the container should have stopped on its own.
		if rec.InjectedAt != nil {
			elapsed := time.Since(rec.InjectedAt.Time)
			if elapsed >= duration {
				rec.CleanedUp = true
				continue
			}
		}

		// Duration has not elapsed yet; leave CleanedUp false and let the
		// controller requeue for a future check.
	}

	return nil
}

// resolveDuration returns the chaos duration from the spec, falling back to
// defaultDuration when the field is nil or zero.
func resolveDuration(spec *v1alpha1.ResourceExhaustionSpec) time.Duration {
	if spec != nil && spec.Duration != nil && spec.Duration.Duration > 0 {
		return spec.Duration.Duration
	}
	return defaultDuration
}

