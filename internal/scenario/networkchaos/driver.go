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

// Package networkchaos implements the NetworkChaos scenario driver.
// It injects tc(8) network disruption rules into TaskManager pods using
// Kubernetes ephemeral containers, allowing controlled introduction of
// latency, jitter, packet loss, and bandwidth limits without modifying
// the original container images.
package networkchaos

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
	"github.com/flink-chaos-operator/internal/netchaos/tc"
)

// defaultDuration is used when the network spec duration is not set.
const defaultDuration = 60 * time.Second

// Driver implements chaos injection via ephemeral containers running tc commands.
// It requires a Kubernetes client that can call the /ephemeralcontainers subresource.
type Driver struct {
	// Client is the controller-runtime client for status updates and pod reads.
	Client client.Client
	// KubeClient is the typed client-go clientset for ephemeral container injection.
	// Using the typed client because controller-runtime does not support the
	// /ephemeralcontainers subresource directly.
	KubeClient kubernetes.Interface
	// TCImage is the container image that provides the tc binary (iproute2).
	// Example: "ghcr.io/flink-chaos-operator/tc-tools:latest"
	TCImage string
}

// New constructs a Driver with the provided clients and tc tool image.
func New(c client.Client, kube kubernetes.Interface, image string) *Driver {
	return &Driver{
		Client:     c,
		KubeClient: kube,
		TCImage:    image,
	}
}

// Inject implements interfaces.ScenarioDriver. It injects ephemeral tc containers
// into each TaskManager pod in the resolved target. The method is idempotent:
// if EphemeralContainerInjections is already populated, the existing result is
// returned without re-injecting.
func (d *Driver) Inject(ctx context.Context, run *v1alpha1.ChaosRun,
	target *interfaces.ResolvedTarget) (*interfaces.InjectionResult, error) {

	if run.Spec.Scenario.Type != v1alpha1.ScenarioNetworkChaos {
		return nil, fmt.Errorf("driver supports %q scenario only, got %q",
			v1alpha1.ScenarioNetworkChaos, run.Spec.Scenario.Type)
	}

	n := run.Spec.Scenario.Network
	if n == nil {
		return nil, fmt.Errorf("network spec is required for %q scenario", v1alpha1.ScenarioNetworkChaos)
	}

	// Idempotency: if injections have already been recorded, return the
	// result derived from the existing status without re-injecting.
	if len(run.Status.EphemeralContainerInjections) > 0 {
		injected := make([]string, 0, len(run.Status.EphemeralContainerInjections))
		for _, rec := range run.Status.EphemeralContainerInjections {
			injected = append(injected, rec.PodName)
		}
		return &interfaces.InjectionResult{
			SelectedPods: target.TMPodNames,
			InjectedPods: injected,
		}, nil
	}

	rule := buildRule(n)
	duration := resolveDuration(n)

	injectedPods := make([]string, 0, len(target.TMPodNames))
	var injectionResult *interfaces.InjectionResult

	for i, podName := range target.TMPodNames {
		containerName := ephemeral.ContainerName("fchaos-tc-", run.Name, i)

		ephCont := corev1.EphemeralContainer{
			EphemeralContainerCommon: corev1.EphemeralContainerCommon{
				Name:    containerName,
				Image:   d.TCImage,
				Command: []string{"/bin/sh", "-c", tc.EntrypointScript(rule, duration)},
				SecurityContext: &corev1.SecurityContext{
					Capabilities: &corev1.Capabilities{
						Add: []corev1.Capability{"NET_ADMIN"},
					},
				},
			},
		}

		pod := &corev1.Pod{}
		if err := d.Client.Get(ctx, types.NamespacedName{Name: podName, Namespace: run.Namespace}, pod); err != nil {
			injectionResult = &interfaces.InjectionResult{
				SelectedPods: target.TMPodNames,
				InjectedPods: injectedPods,
			}
			return injectionResult, fmt.Errorf("fetching pod %s: %w", podName, err)
		}

		pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, ephCont)
		if _, err := d.KubeClient.CoreV1().Pods(run.Namespace).UpdateEphemeralContainers(
			ctx, podName, pod, metav1.UpdateOptions{}); err != nil {
			injectionResult = &interfaces.InjectionResult{
				SelectedPods: target.TMPodNames,
				InjectedPods: injectedPods,
			}
			return injectionResult, fmt.Errorf("injecting ephemeral container into %s: %w", podName, err)
		}

		now := metav1.Now()
		run.Status.EphemeralContainerInjections = append(run.Status.EphemeralContainerInjections,
			v1alpha1.EphemeralContainerRecord{
				PodName:       podName,
				ContainerName: containerName,
				InjectedAt:    &now,
			})

		injectedPods = append(injectedPods, podName)
	}

	return &interfaces.InjectionResult{
		SelectedPods: target.TMPodNames,
		InjectedPods: injectedPods,
	}, nil
}

// Cleanup implements the cleanup half of interfaces.CleanableScenarioDriver.
// For each unclean ephemeral container record it checks whether the injection
// duration has elapsed. If so, the container's embedded sleep has expired and
// the tc cleanup shell script has already run; the record is marked cleaned up.
// If the duration has not yet elapsed, the method returns nil so the controller
// will requeue and check again on the next reconciliation.
// If the pod is not found, the record is also marked cleaned up because the
// pod deletion removes any tc qdiscs along with it.
func (d *Driver) Cleanup(ctx context.Context, run *v1alpha1.ChaosRun) error {
	n := run.Spec.Scenario.Network
	duration := resolveDuration(n)

	for i := range run.Status.EphemeralContainerInjections {
		rec := &run.Status.EphemeralContainerInjections[i]
		if rec.CleanedUp {
			continue
		}

		pod := &corev1.Pod{}
		err := d.Client.Get(ctx, types.NamespacedName{Name: rec.PodName, Namespace: run.Namespace}, pod)
		if err != nil {
			if apierrors.IsNotFound(err) {
				// Pod was deleted; tc rules are gone with it.
				rec.CleanedUp = true
				continue
			}
			return fmt.Errorf("fetching pod %s during cleanup: %w", rec.PodName, err)
		}

		// Check the container's status in the pod.
		containerState := ephemeral.FindContainerState(pod, rec.ContainerName)
		if containerState != nil && containerState.Terminated != nil {
			rec.CleanedUp = true
			continue
		}

		// Pragmatic MVP approach: check if the injection duration has elapsed.
		// If so, the container's sleep has expired naturally and the cleanup
		// shell has already run the tc qdisc del command.
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

// buildRule constructs a tc.Rule from the NetworkChaosSpec.
func buildRule(n *v1alpha1.NetworkChaosSpec) tc.Rule {
	rule := tc.Rule{Interface: "eth0"}

	if n.Latency != nil {
		rule.Latency = n.Latency.Duration
	}
	if n.Jitter != nil {
		rule.Jitter = n.Jitter.Duration
	}
	if n.Loss != nil {
		rule.LossPct = *n.Loss
	}
	if n.Bandwidth != "" {
		rule.Bandwidth = n.Bandwidth
	}
	if n.ExternalEndpoint != nil && n.ExternalEndpoint.CIDR != "" {
		rule.PeerCIDR = n.ExternalEndpoint.CIDR
	}

	return rule
}

// resolveDuration returns the chaos duration from the spec, falling back to
// defaultDuration when the field is nil or zero.
func resolveDuration(n *v1alpha1.NetworkChaosSpec) time.Duration {
	if n != nil && n.Duration != nil && n.Duration.Duration > 0 {
		return n.Duration.Duration
	}
	return defaultDuration
}

