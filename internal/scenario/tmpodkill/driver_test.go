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

package tmpodkill_test

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/interfaces"
	"github.com/flink-chaos-operator/internal/scenario/tmpodkill"
)

// newScheme returns a runtime.Scheme that includes the core API group so
// the fake client can handle Pod objects.
func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("adding corev1 to scheme: %v", err)
	}
	return s
}

// buildPod creates a minimal Pod object in the given namespace.
func buildPod(namespace, name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

// buildRun returns a minimal ChaosRun with the given scenario configuration.
func buildRun(namespace string, scenarioType v1alpha1.ScenarioType, sel v1alpha1.SelectionSpec, action v1alpha1.ActionSpec) *v1alpha1.ChaosRun {
	return &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-run",
			Namespace: namespace,
		},
		Spec: v1alpha1.ChaosRunSpec{
			Scenario: v1alpha1.ScenarioSpec{
				Type:      scenarioType,
				Selection: sel,
				Action:    action,
			},
		},
	}
}

// fixedSelectFn returns a selectFn that always returns the first n pods from
// the provided slice, enabling deterministic tests.
func fixedSelectFn(pods []string, count int) []string {
	if count >= len(pods) {
		result := make([]string, len(pods))
		copy(result, pods)
		return result
	}
	result := make([]string, count)
	copy(result, pods[:count])
	return result
}

const testNamespace = "default"

// ---------------------------------------------------------------------------
// Wrong scenario type
// ---------------------------------------------------------------------------

func TestInject_WrongScenarioType_ReturnsError(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	d := tmpodkill.New(fakeClient)

	run := buildRun(testNamespace, "UnknownScenario", v1alpha1.SelectionSpec{}, v1alpha1.ActionSpec{})
	target := &interfaces.ResolvedTarget{TMPodNames: []string{"tm-0"}}

	_, err := d.Inject(context.Background(), run, target)
	if err == nil {
		t.Fatal("expected error for wrong scenario type, got nil")
	}
}

// ---------------------------------------------------------------------------
// Nil target
// ---------------------------------------------------------------------------

func TestInject_NilTarget_ReturnsError(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	d := tmpodkill.New(fakeClient)

	run := buildRun(testNamespace, v1alpha1.ScenarioTaskManagerPodKill,
		v1alpha1.SelectionSpec{}, v1alpha1.ActionSpec{})

	_, err := d.Inject(context.Background(), run, nil)
	if err == nil {
		t.Fatal("expected error for nil target, got nil")
	}
}

// ---------------------------------------------------------------------------
// Random selection
// ---------------------------------------------------------------------------

func TestInject_RandomMode_SelectsCorrectCount(t *testing.T) {
	pods := []*corev1.Pod{
		buildPod(testNamespace, "tm-0"),
		buildPod(testNamespace, "tm-1"),
		buildPod(testNamespace, "tm-2"),
	}

	objs := make([]client.Object, len(pods))
	for i, p := range pods {
		objs[i] = p
	}

	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(objs...).Build()
	d := tmpodkill.New(fakeClient)
	d.SetSelectFn(fixedSelectFn)

	run := buildRun(testNamespace, v1alpha1.ScenarioTaskManagerPodKill,
		v1alpha1.SelectionSpec{Mode: v1alpha1.SelectionModeRandom, Count: 2},
		v1alpha1.ActionSpec{Type: v1alpha1.ActionDeletePod})

	target := &interfaces.ResolvedTarget{
		TMPodNames: []string{"tm-0", "tm-1", "tm-2"},
	}

	result, err := d.Inject(context.Background(), run, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.SelectedPods) != 2 {
		t.Errorf("expected 2 selected pods, got %d", len(result.SelectedPods))
	}
	if len(result.InjectedPods) != 2 {
		t.Errorf("expected 2 injected pods, got %d", len(result.InjectedPods))
	}
}

func TestInject_RandomMode_CountZeroFloorsToOne(t *testing.T) {
	pods := []*corev1.Pod{
		buildPod(testNamespace, "tm-0"),
		buildPod(testNamespace, "tm-1"),
	}

	objs := make([]client.Object, len(pods))
	for i, p := range pods {
		objs[i] = p
	}

	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(objs...).Build()
	d := tmpodkill.New(fakeClient)
	d.SetSelectFn(fixedSelectFn)

	// Count=0 should be floored to 1.
	run := buildRun(testNamespace, v1alpha1.ScenarioTaskManagerPodKill,
		v1alpha1.SelectionSpec{Mode: v1alpha1.SelectionModeRandom, Count: 0},
		v1alpha1.ActionSpec{Type: v1alpha1.ActionDeletePod})

	target := &interfaces.ResolvedTarget{TMPodNames: []string{"tm-0", "tm-1"}}

	result, err := d.Inject(context.Background(), run, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.SelectedPods) != 1 {
		t.Errorf("expected 1 selected pod (count floored from 0), got %d", len(result.SelectedPods))
	}
}

func TestInject_RandomMode_CountExceedsAvailable_SelectsAll(t *testing.T) {
	pods := []*corev1.Pod{
		buildPod(testNamespace, "tm-0"),
		buildPod(testNamespace, "tm-1"),
	}

	objs := make([]client.Object, len(pods))
	for i, p := range pods {
		objs[i] = p
	}

	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(objs...).Build()
	d := tmpodkill.New(fakeClient)
	d.SetSelectFn(fixedSelectFn)

	run := buildRun(testNamespace, v1alpha1.ScenarioTaskManagerPodKill,
		v1alpha1.SelectionSpec{Mode: v1alpha1.SelectionModeRandom, Count: 100},
		v1alpha1.ActionSpec{Type: v1alpha1.ActionDeletePod})

	target := &interfaces.ResolvedTarget{TMPodNames: []string{"tm-0", "tm-1"}}

	result, err := d.Inject(context.Background(), run, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.SelectedPods) != 2 {
		t.Errorf("expected all 2 pods selected, got %d", len(result.SelectedPods))
	}
}

// ---------------------------------------------------------------------------
// Explicit selection
// ---------------------------------------------------------------------------

func TestInject_ExplicitMode_KnownPods_DeletesSuccessfully(t *testing.T) {
	pods := []*corev1.Pod{
		buildPod(testNamespace, "tm-0"),
		buildPod(testNamespace, "tm-1"),
		buildPod(testNamespace, "tm-2"),
	}

	objs := make([]client.Object, len(pods))
	for i, p := range pods {
		objs[i] = p
	}

	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(objs...).Build()
	d := tmpodkill.New(fakeClient)

	run := buildRun(testNamespace, v1alpha1.ScenarioTaskManagerPodKill,
		v1alpha1.SelectionSpec{
			Mode:     v1alpha1.SelectionModeExplicit,
			PodNames: []string{"tm-0", "tm-2"},
		},
		v1alpha1.ActionSpec{Type: v1alpha1.ActionDeletePod})

	target := &interfaces.ResolvedTarget{
		TMPodNames: []string{"tm-0", "tm-1", "tm-2"},
	}

	result, err := d.Inject(context.Background(), run, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.SelectedPods) != 2 {
		t.Errorf("expected 2 selected pods, got %d", len(result.SelectedPods))
	}
	if len(result.InjectedPods) != 2 {
		t.Errorf("expected 2 injected pods, got %d", len(result.InjectedPods))
	}
}

func TestInject_ExplicitMode_UnknownPod_ReturnsError(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	d := tmpodkill.New(fakeClient)

	run := buildRun(testNamespace, v1alpha1.ScenarioTaskManagerPodKill,
		v1alpha1.SelectionSpec{
			Mode:     v1alpha1.SelectionModeExplicit,
			PodNames: []string{"tm-0", "tm-unknown"},
		},
		v1alpha1.ActionSpec{Type: v1alpha1.ActionDeletePod})

	target := &interfaces.ResolvedTarget{TMPodNames: []string{"tm-0", "tm-1"}}

	_, err := d.Inject(context.Background(), run, target)
	if err == nil {
		t.Fatal("expected error for unknown pod name, got nil")
	}
}

// ---------------------------------------------------------------------------
// Idempotency: already-deleted pod
// ---------------------------------------------------------------------------

func TestInject_PodAlreadyDeleted_TreatedAsInjected(t *testing.T) {
	// Do not pre-create any pods in the fake client — they are already gone.
	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	d := tmpodkill.New(fakeClient)
	d.SetSelectFn(fixedSelectFn)

	run := buildRun(testNamespace, v1alpha1.ScenarioTaskManagerPodKill,
		v1alpha1.SelectionSpec{
			Mode:     v1alpha1.SelectionModeExplicit,
			PodNames: []string{"tm-0"},
		},
		v1alpha1.ActionSpec{Type: v1alpha1.ActionDeletePod})

	target := &interfaces.ResolvedTarget{TMPodNames: []string{"tm-0"}}

	result, err := d.Inject(context.Background(), run, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.InjectedPods) != 1 || result.InjectedPods[0] != "tm-0" {
		t.Errorf("expected tm-0 in injectedPods (idempotent), got %v", result.InjectedPods)
	}
}

// ---------------------------------------------------------------------------
// GracePeriodSeconds is forwarded
// ---------------------------------------------------------------------------

func TestInject_GracePeriodZero_InjectionSucceeds(t *testing.T) {
	pods := []*corev1.Pod{
		buildPod(testNamespace, "tm-0"),
	}

	objs := make([]client.Object, 1)
	objs[0] = pods[0]

	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(objs...).Build()
	d := tmpodkill.New(fakeClient)

	grace := int64(0)
	run := buildRun(testNamespace, v1alpha1.ScenarioTaskManagerPodKill,
		v1alpha1.SelectionSpec{
			Mode:     v1alpha1.SelectionModeExplicit,
			PodNames: []string{"tm-0"},
		},
		v1alpha1.ActionSpec{
			Type:               v1alpha1.ActionDeletePod,
			GracePeriodSeconds: &grace,
		})

	target := &interfaces.ResolvedTarget{TMPodNames: []string{"tm-0"}}

	result, err := d.Inject(context.Background(), run, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.InjectedPods) != 1 {
		t.Errorf("expected 1 injected pod, got %d", len(result.InjectedPods))
	}
}

// ---------------------------------------------------------------------------
// Partial failure
// ---------------------------------------------------------------------------

// failingClient wraps a fake client and injects an error on the second Delete.
type failingClient struct {
	client.Client
	deleteCount int
	failAfter   int
}

func (f *failingClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	f.deleteCount++
	if f.deleteCount > f.failAfter {
		return fmt.Errorf("simulated delete failure")
	}
	return f.Client.Delete(ctx, obj, opts...)
}

func TestInject_PartialDeleteFailure_ReturnsPartialResultAndError(t *testing.T) {
	pods := []*corev1.Pod{
		buildPod(testNamespace, "tm-0"),
		buildPod(testNamespace, "tm-1"),
	}

	objs := make([]client.Object, len(pods))
	for i, p := range pods {
		objs[i] = p
	}

	base := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(objs...).Build()
	fc := &failingClient{Client: base, failAfter: 1}

	d := tmpodkill.New(fc)

	run := buildRun(testNamespace, v1alpha1.ScenarioTaskManagerPodKill,
		v1alpha1.SelectionSpec{
			Mode:     v1alpha1.SelectionModeExplicit,
			PodNames: []string{"tm-0", "tm-1"},
		},
		v1alpha1.ActionSpec{Type: v1alpha1.ActionDeletePod})

	target := &interfaces.ResolvedTarget{TMPodNames: []string{"tm-0", "tm-1"}}

	result, err := d.Inject(context.Background(), run, target)
	if err == nil {
		t.Fatal("expected error from partial failure, got nil")
	}

	if result == nil {
		t.Fatal("expected non-nil result even on partial failure")
	}

	// First pod should have been successfully deleted.
	if len(result.InjectedPods) != 1 || result.InjectedPods[0] != "tm-0" {
		t.Errorf("expected [tm-0] in InjectedPods, got %v", result.InjectedPods)
	}
}
