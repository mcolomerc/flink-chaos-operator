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

// Package controller_test contains unit tests for the ChaosRun reconciler.
// Tests use controller-runtime's fake client so they run without an etcd or
// Kubernetes API server binary.
package controller_test

import (
	"context"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/interfaces"
)

// ---------------------------------------------------------------------------
// Stub implementations for the four controller dependency interfaces.
// ---------------------------------------------------------------------------

// noopSafetyChecker always passes safety checks (or returns a configured error).
type noopSafetyChecker struct{ err error }

func (n *noopSafetyChecker) Check(_ context.Context, _ *v1alpha1.ChaosRun, _ *interfaces.ResolvedTarget) error {
	return n.err
}

// stubResolver returns a pre-configured ResolvedTarget or error.
type stubResolver struct {
	result *interfaces.ResolvedTarget
	err    error
}

func (s *stubResolver) Resolve(_ context.Context, _ *v1alpha1.ChaosRun) (*interfaces.ResolvedTarget, error) {
	return s.result, s.err
}

// stubDriver returns a pre-configured InjectionResult or error.
type stubDriver struct {
	result *interfaces.InjectionResult
	err    error
}

func (s *stubDriver) Inject(_ context.Context, _ *v1alpha1.ChaosRun, _ *interfaces.ResolvedTarget) (*interfaces.InjectionResult, error) {
	return s.result, s.err
}

// stubCleanableDriver implements both ScenarioDriver and CleanableScenarioDriver.
type stubCleanableDriver struct {
	injectResult  *interfaces.InjectionResult
	injectErr     error
	cleanupErr    error
	cleanupCalled bool
	// When true, Cleanup marks all EphemeralContainerInjections as CleanedUp=true.
	markCleanedUp bool
}

func (s *stubCleanableDriver) Inject(_ context.Context, run *v1alpha1.ChaosRun, _ *interfaces.ResolvedTarget) (*interfaces.InjectionResult, error) {
	return s.injectResult, s.injectErr
}

func (s *stubCleanableDriver) Cleanup(_ context.Context, run *v1alpha1.ChaosRun) error {
	s.cleanupCalled = true
	if s.cleanupErr != nil {
		return s.cleanupErr
	}
	if s.markCleanedUp {
		for i := range run.Status.EphemeralContainerInjections {
			run.Status.EphemeralContainerInjections[i].CleanedUp = true
		}
	}
	return nil
}

// stubObserver returns a pre-configured ObservationResult or error.
type stubObserver struct {
	result *interfaces.ObservationResult
	err    error
}

func (s *stubObserver) Observe(_ context.Context, _ *v1alpha1.ChaosRun, _ *interfaces.ResolvedTarget) (*interfaces.ObservationResult, error) {
	return s.result, s.err
}

// defaultResolvedTarget returns a minimal ResolvedTarget suitable for tests
// that do not require a specific target state.
func defaultResolvedTarget() *interfaces.ResolvedTarget {
	return &interfaces.ResolvedTarget{
		Platform:      "flink-operator",
		Namespace:     "default",
		LogicalName:   "orders-app",
		TMPodNames:    []string{"tm-0"},
		SharedCluster: false,
	}
}

// defaultResolvers returns a resolver map that uses a stub resolver for all
// supported target types, returning a default resolved target.
func defaultResolvers() map[v1alpha1.TargetType]interfaces.TargetResolver {
	stub := &stubResolver{result: defaultResolvedTarget()}
	return map[v1alpha1.TargetType]interfaces.TargetResolver{
		v1alpha1.TargetFlinkDeployment:     stub,
		v1alpha1.TargetVervericaDeployment: stub,
		v1alpha1.TargetPodSelector:         stub,
	}
}
