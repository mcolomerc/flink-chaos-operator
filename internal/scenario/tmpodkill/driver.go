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

// Package tmpodkill implements the TaskManagerPodKill chaos scenario driver.
// It selects TaskManager pods from a resolved target and deletes them via the
// Kubernetes API according to the selection and action specifications.
package tmpodkill

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/interfaces"
)

// Driver implements interfaces.ScenarioDriver for the TaskManagerPodKill
// scenario. It selects a subset of TaskManager pods and deletes them.
type Driver struct {
	// Client is the controller-runtime client used to delete pods.
	Client client.Client

	// selectFn is the pod selection function. Defaults to randomSelect.
	// Tests override this to get deterministic output.
	selectFn func(pods []string, count int) []string
}

// New constructs a Driver with the default random selection function.
func New(c client.Client) *Driver {
	return &Driver{
		Client:   c,
		selectFn: randomSelect,
	}
}

// SetSelectFn replaces the pod selection function. Intended for use in tests
// that need deterministic pod selection without relying on math/rand.
func (d *Driver) SetSelectFn(fn func(pods []string, count int) []string) {
	d.selectFn = fn
}

// Inject implements interfaces.ScenarioDriver. It selects pods from
// target.TMPodNames according to run.Spec.Scenario.Selection, then deletes
// each selected pod. It is idempotent: pods that are already gone are counted
// as successfully injected.
func (d *Driver) Inject(ctx context.Context, run *v1alpha1.ChaosRun, target *interfaces.ResolvedTarget) (*interfaces.InjectionResult, error) {
	if target == nil {
		return nil, fmt.Errorf("target is nil")
	}

	if run.Spec.Scenario.Type != v1alpha1.ScenarioTaskManagerPodKill {
		return nil, fmt.Errorf("driver supports %q scenario only, got %q",
			v1alpha1.ScenarioTaskManagerPodKill, run.Spec.Scenario.Type)
	}

	selectedPods, err := d.selectPods(run, target)
	if err != nil {
		return nil, err
	}

	injectedPods, err := d.deletePods(ctx, run, selectedPods)
	if err != nil {
		// Return a partial result alongside the error so the caller can
		// record which pods were successfully deleted before the failure.
		return &interfaces.InjectionResult{
			SelectedPods: selectedPods,
			InjectedPods: injectedPods,
		}, err
	}

	return &interfaces.InjectionResult{
		SelectedPods: selectedPods,
		InjectedPods: injectedPods,
	}, nil
}

// selectPods returns the pod names to inject based on the selection spec.
func (d *Driver) selectPods(run *v1alpha1.ChaosRun, target *interfaces.ResolvedTarget) ([]string, error) {
	sel := run.Spec.Scenario.Selection

	switch sel.Mode {
	case v1alpha1.SelectionModeExplicit:
		return d.explicitSelect(sel.PodNames, target.TMPodNames)

	default:
		// SelectionModeRandom and empty/default all use random selection.
		count := sel.Count
		if count < 1 {
			count = 1
		}
		return d.selectFn(target.TMPodNames, int(count)), nil
	}
}

// explicitSelect validates that every requested name is present in available,
// returning an error that lists any unknown names.
func explicitSelect(requested, available []string) ([]string, error) {
	avail := make(map[string]struct{}, len(available))
	for _, p := range available {
		avail[p] = struct{}{}
	}

	var unknown []string
	for _, p := range requested {
		if _, ok := avail[p]; !ok {
			unknown = append(unknown, p)
		}
	}

	if len(unknown) > 0 {
		return nil, fmt.Errorf("pod names not found in target: %s",
			strings.Join(unknown, ", "))
	}

	return requested, nil
}

// explicitSelect is a method shim so Driver can call the package-level
// function while keeping the logic pure and easily testable.
func (d *Driver) explicitSelect(requested, available []string) ([]string, error) {
	return explicitSelect(requested, available)
}

// randomSelect picks count pods at random from pods. If count >= len(pods),
// the full slice is returned. A new seeded source is created on each call so
// that results are non-deterministic in production.
func randomSelect(pods []string, count int) []string {
	if count >= len(pods) {
		// Return a copy to avoid mutating the caller's slice.
		result := make([]string, len(pods))
		copy(result, pods)
		return result
	}

	//nolint:gosec // math/rand is acceptable for non-security pod selection.
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Shuffle a copy so the original slice is not modified.
	shuffled := make([]string, len(pods))
	copy(shuffled, pods)
	r.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	return shuffled[:count]
}

// deletePods deletes each pod in selected and returns the names of those
// successfully deleted (or already gone). On the first non-NotFound error,
// it aborts and returns the partial list together with the error.
func (d *Driver) deletePods(ctx context.Context, run *v1alpha1.ChaosRun, selected []string) ([]string, error) {
	injected := make([]string, 0, len(selected))

	for _, podName := range selected {
		pod := &corev1.Pod{}
		pod.Name = podName
		pod.Namespace = run.Namespace

		opts := &client.DeleteOptions{}
		if run.Spec.Scenario.Action.GracePeriodSeconds != nil {
			grace := *run.Spec.Scenario.Action.GracePeriodSeconds
			opts = &client.DeleteOptions{GracePeriodSeconds: &grace}
		}

		if err := d.Client.Delete(ctx, pod, opts); err != nil {
			if apierrors.IsNotFound(err) {
				// Pod is already gone — treat as successfully injected.
				injected = append(injected, podName)
				continue
			}
			return injected, fmt.Errorf("deleting pod %q: %w", podName, err)
		}

		injected = append(injected, podName)
	}

	return injected, nil
}
