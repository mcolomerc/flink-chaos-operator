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

package kubernetes_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/interfaces"
	k8sobserver "github.com/flink-chaos-operator/internal/observer/kubernetes"
)

const testNamespace = "flink-test"

// buildScheme returns a runtime.Scheme that includes the core Kubernetes types
// required by the fake client.
func buildScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = v1alpha1.AddToScheme(s)
	return s
}

// runningReadyPod builds a Pod object in the Running phase with all containers
// reporting Ready: true.
func runningReadyPod(namespace, name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "taskmanager", Ready: true},
			},
		},
	}
}

// pendingPod builds a Pod object in the Pending phase (not yet Ready).
func pendingPod(namespace, name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "taskmanager", Ready: false},
			},
		},
	}
}

// chaosRun returns a minimal ChaosRun with the given injected pod names
// recorded in its status.
func chaosRun(injectedPods []string) *v1alpha1.ChaosRun {
	return &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "test-run",
		},
		Status: v1alpha1.ChaosRunStatus{
			InjectedPods: injectedPods,
		},
	}
}

// TestObserve_NilTarget verifies that a nil target returns a zeroed result
// without error.
func TestObserve_NilTarget(t *testing.T) {
	obs := &k8sobserver.Observer{
		Client: fake.NewClientBuilder().WithScheme(buildScheme()).Build(),
	}

	run := chaosRun(nil)
	result, err := obs.Observe(context.Background(), run, nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result.ReplacementObserved {
		t.Error("expected ReplacementObserved=false for nil target")
	}
	if result.AllReplacementsReady {
		t.Error("expected AllReplacementsReady=false for nil target")
	}
	if result.TMCountBefore != 0 {
		t.Errorf("expected TMCountBefore=0, got %d", result.TMCountBefore)
	}
	if result.TMCountAfter != 0 {
		t.Errorf("expected TMCountAfter=0, got %d", result.TMCountAfter)
	}
}

// TestObserve_EmptyTMPodNames verifies that a target with no pod names returns
// a zeroed result without error.
func TestObserve_EmptyTMPodNames(t *testing.T) {
	obs := &k8sobserver.Observer{
		Client: fake.NewClientBuilder().WithScheme(buildScheme()).Build(),
	}

	run := chaosRun([]string{"tm-0"})
	target := &interfaces.ResolvedTarget{
		Namespace:   testNamespace,
		TMPodNames:  []string{},
	}

	result, err := obs.Observe(context.Background(), run, target)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result.ReplacementObserved {
		t.Error("expected ReplacementObserved=false for empty TMPodNames")
	}
	if result.AllReplacementsReady {
		t.Error("expected AllReplacementsReady=false for empty TMPodNames")
	}
}

// TestObserve_InjectedPodStillPresent verifies that when the injected pod name
// still appears in the live set (not yet deleted), replacement is not observed.
func TestObserve_InjectedPodStillPresent(t *testing.T) {
	// "tm-0" was injected and is still live — it has not been deleted yet.
	fakeClient := fake.NewClientBuilder().
		WithScheme(buildScheme()).
		WithObjects(runningReadyPod(testNamespace, "tm-0")).
		Build()

	obs := &k8sobserver.Observer{Client: fakeClient}

	run := chaosRun([]string{"tm-0"})
	target := &interfaces.ResolvedTarget{
		Namespace:  testNamespace,
		TMPodNames: []string{"tm-0"},
	}

	result, err := obs.Observe(context.Background(), run, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ReplacementObserved {
		t.Error("expected ReplacementObserved=false while injected pod is still present")
	}
	if result.AllReplacementsReady {
		t.Error("expected AllReplacementsReady=false while injected pod is still present")
	}
	if result.TMCountBefore != 2 {
		// 1 live (tm-0) + 1 injected (tm-0) = 2
		t.Errorf("expected TMCountBefore=2, got %d", result.TMCountBefore)
	}
	if result.TMCountAfter != 1 {
		t.Errorf("expected TMCountAfter=1, got %d", result.TMCountAfter)
	}
}

// TestObserve_ReplacementPresentButNotReady verifies that when the injected
// pod is gone and a new pod exists but is not yet Ready, ReplacementObserved
// is true but AllReplacementsReady is false.
func TestObserve_ReplacementPresentButNotReady(t *testing.T) {
	// "tm-0" was injected and deleted; "tm-1" is the replacement but still
	// in Pending phase.
	fakeClient := fake.NewClientBuilder().
		WithScheme(buildScheme()).
		WithObjects(pendingPod(testNamespace, "tm-1")).
		Build()

	obs := &k8sobserver.Observer{Client: fakeClient}

	run := chaosRun([]string{"tm-0"})
	target := &interfaces.ResolvedTarget{
		Namespace:  testNamespace,
		TMPodNames: []string{"tm-1"},
	}

	result, err := obs.Observe(context.Background(), run, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.ReplacementObserved {
		t.Error("expected ReplacementObserved=true: injected pod gone, replacement pod present")
	}
	if result.AllReplacementsReady {
		t.Error("expected AllReplacementsReady=false: replacement pod is not yet Ready")
	}
	// TMCountBefore = 1 (tm-1 live) + 1 (tm-0 injected) = 2
	if result.TMCountBefore != 2 {
		t.Errorf("expected TMCountBefore=2, got %d", result.TMCountBefore)
	}
	if result.TMCountAfter != 1 {
		t.Errorf("expected TMCountAfter=1, got %d", result.TMCountAfter)
	}
}

// TestObserve_ReplacementPresentAndReady verifies that when the injected pod
// is gone and a Ready replacement exists, both ReplacementObserved and
// AllReplacementsReady are true.
func TestObserve_ReplacementPresentAndReady(t *testing.T) {
	// "tm-0" was injected and deleted; "tm-1" is the replacement and Running/Ready.
	fakeClient := fake.NewClientBuilder().
		WithScheme(buildScheme()).
		WithObjects(runningReadyPod(testNamespace, "tm-1")).
		Build()

	obs := &k8sobserver.Observer{Client: fakeClient}

	run := chaosRun([]string{"tm-0"})
	target := &interfaces.ResolvedTarget{
		Namespace:  testNamespace,
		TMPodNames: []string{"tm-1"},
	}

	result, err := obs.Observe(context.Background(), run, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.ReplacementObserved {
		t.Error("expected ReplacementObserved=true")
	}
	if !result.AllReplacementsReady {
		t.Error("expected AllReplacementsReady=true: replacement pod is Running and Ready")
	}
}

// TestObserve_NoInjectedPods_AllPodsReady verifies that when no pods were
// injected (e.g. dry-run or aborted before injection) and all current pods
// are Ready, both ReplacementObserved and AllReplacementsReady are true.
func TestObserve_NoInjectedPods_AllPodsReady(t *testing.T) {
	fakeClient := fake.NewClientBuilder().
		WithScheme(buildScheme()).
		WithObjects(
			runningReadyPod(testNamespace, "tm-0"),
			runningReadyPod(testNamespace, "tm-1"),
		).
		Build()

	obs := &k8sobserver.Observer{Client: fakeClient}

	// No injected pods recorded in status.
	run := chaosRun(nil)
	target := &interfaces.ResolvedTarget{
		Namespace:  testNamespace,
		TMPodNames: []string{"tm-0", "tm-1"},
	}

	result, err := obs.Observe(context.Background(), run, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.ReplacementObserved {
		t.Error("expected ReplacementObserved=true when no pods were injected")
	}
	if !result.AllReplacementsReady {
		t.Error("expected AllReplacementsReady=true when no pods injected and all current pods Ready")
	}
	// TMCountBefore = 2 (live) + 0 (injected) = 2
	if result.TMCountBefore != 2 {
		t.Errorf("expected TMCountBefore=2, got %d", result.TMCountBefore)
	}
	if result.TMCountAfter != 2 {
		t.Errorf("expected TMCountAfter=2, got %d", result.TMCountAfter)
	}
}

// TestObserve_MultipleInjectedPods_AllReplacementsReady verifies correct
// behaviour when multiple pods were injected and all replacements are Ready.
func TestObserve_MultipleInjectedPods_AllReplacementsReady(t *testing.T) {
	// "tm-0" and "tm-1" were injected; "tm-2" and "tm-3" are replacements.
	fakeClient := fake.NewClientBuilder().
		WithScheme(buildScheme()).
		WithObjects(
			runningReadyPod(testNamespace, "tm-2"),
			runningReadyPod(testNamespace, "tm-3"),
		).
		Build()

	obs := &k8sobserver.Observer{Client: fakeClient}

	run := chaosRun([]string{"tm-0", "tm-1"})
	target := &interfaces.ResolvedTarget{
		Namespace:  testNamespace,
		TMPodNames: []string{"tm-2", "tm-3"},
	}

	result, err := obs.Observe(context.Background(), run, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.ReplacementObserved {
		t.Error("expected ReplacementObserved=true")
	}
	if !result.AllReplacementsReady {
		t.Error("expected AllReplacementsReady=true")
	}
	// TMCountBefore = 2 (live) + 2 (injected) = 4
	if result.TMCountBefore != 4 {
		t.Errorf("expected TMCountBefore=4, got %d", result.TMCountBefore)
	}
	if result.TMCountAfter != 2 {
		t.Errorf("expected TMCountAfter=2, got %d", result.TMCountAfter)
	}
}

// TestObserve_MultipleInjectedPods_OneReplacementNotReady verifies that
// AllReplacementsReady is false when even one current pod is not yet Ready.
func TestObserve_MultipleInjectedPods_OneReplacementNotReady(t *testing.T) {
	fakeClient := fake.NewClientBuilder().
		WithScheme(buildScheme()).
		WithObjects(
			runningReadyPod(testNamespace, "tm-2"),
			pendingPod(testNamespace, "tm-3"),
		).
		Build()

	obs := &k8sobserver.Observer{Client: fakeClient}

	run := chaosRun([]string{"tm-0", "tm-1"})
	target := &interfaces.ResolvedTarget{
		Namespace:  testNamespace,
		TMPodNames: []string{"tm-2", "tm-3"},
	}

	result, err := obs.Observe(context.Background(), run, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.ReplacementObserved {
		t.Error("expected ReplacementObserved=true")
	}
	if result.AllReplacementsReady {
		t.Error("expected AllReplacementsReady=false: one replacement pod is still Pending")
	}
}
