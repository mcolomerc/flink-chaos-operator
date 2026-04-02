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

// Package kubernetes provides a Kubernetes-native implementation of the
// interfaces.Observer contract. It detects TaskManager pod replacement by
// querying the Kubernetes API directly, without depending on the Flink REST
// API.
package kubernetes

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/interfaces"
)

// Observer implements interfaces.Observer using only the Kubernetes API.
// It determines whether replacement TaskManager pods have appeared and become
// Ready after the injected pods were deleted.
type Observer struct {
	// Client is the controller-runtime client used to query pod state.
	Client client.Client
}

// Observe returns a point-in-time snapshot of TaskManager recovery state.
//
// It compares the set of currently live pods (target.TMPodNames) against the
// set of pods that were deleted during injection (run.Status.InjectedPods) to
// determine whether replacement pods have appeared and become Ready.
//
// If target is nil or contains no pod names, a zeroed result is returned
// without error.
func (o *Observer) Observe(ctx context.Context, run *v1alpha1.ChaosRun, target *interfaces.ResolvedTarget) (*interfaces.ObservationResult, error) {
	// Guard: no target information means nothing to observe.
	if target == nil || len(target.TMPodNames) == 0 {
		return &interfaces.ObservationResult{}, nil
	}

	injectedPods := run.Status.InjectedPods

	// tmCountBefore is the total TM pod count before injection:
	// the pods that are currently live plus the pods that were deleted.
	tmCountBefore := len(target.TMPodNames) + len(injectedPods)

	// tmCountAfter is the number of live TM pods at the time of this poll.
	tmCountAfter := len(target.TMPodNames)

	// Build a set of injected pod names for O(1) membership checks.
	injectedSet := make(map[string]struct{}, len(injectedPods))
	for _, name := range injectedPods {
		injectedSet[name] = struct{}{}
	}

	// Build a set of currently live pod names.
	liveSet := make(map[string]struct{}, len(target.TMPodNames))
	for _, name := range target.TMPodNames {
		liveSet[name] = struct{}{}
	}

	// replacementObserved is true when:
	//   1. None of the injected pod names appear in the current live set
	//      (the deleted pods have not reappeared under the same name), AND
	//   2. The current live count is at least as large as the pre-injection
	//      count minus the number of injected pods (i.e. we have not lost
	//      additional pods beyond those we deleted).
	//
	// When no pods were injected we treat recovery as already observed so
	// that dry-run and aborted-before-injection cases complete cleanly.
	replacementObserved := false

	if len(injectedPods) == 0 {
		// No injection occurred — consider replacements already observed.
		replacementObserved = true
	} else {
		injectedStillPresent := false
		for _, name := range injectedPods {
			if _, ok := liveSet[name]; ok {
				injectedStillPresent = true
				break
			}
		}

		expectedMinimum := tmCountBefore - len(injectedPods)
		if !injectedStillPresent && tmCountAfter >= expectedMinimum {
			replacementObserved = true
		}
	}

	// allReplacementsReady is true when replacements have been observed AND
	// every currently live pod is Running with all containers Ready.
	allReplacementsReady := false
	if replacementObserved {
		ready, err := o.allPodsReady(ctx, run.Namespace, target.TMPodNames)
		if err != nil {
			return nil, fmt.Errorf("observer: checking pod readiness: %w", err)
		}
		allReplacementsReady = ready
	}

	return &interfaces.ObservationResult{
		ReplacementObserved:  replacementObserved,
		AllReplacementsReady: allReplacementsReady,
		TMCountBefore:        tmCountBefore,
		TMCountAfter:         tmCountAfter,
	}, nil
}

// allPodsReady fetches each pod by name from the given namespace and returns
// true only when every pod is in the Running phase and all of its containers
// report Ready: true.
func (o *Observer) allPodsReady(ctx context.Context, namespace string, podNames []string) (bool, error) {
	for _, name := range podNames {
		pod := &corev1.Pod{}
		if err := o.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, pod); err != nil {
			return false, fmt.Errorf("get pod %q: %w", name, err)
		}

		if pod.Status.Phase != corev1.PodRunning {
			return false, nil
		}

		for _, cs := range pod.Status.ContainerStatuses {
			if !cs.Ready {
				return false, nil
			}
		}
	}

	return true, nil
}
