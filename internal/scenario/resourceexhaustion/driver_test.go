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

package resourceexhaustion_test

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/interfaces"
	"github.com/flink-chaos-operator/internal/scenario/resourceexhaustion"
)

const (
	testNamespace = "default"
	testImage     = "ghcr.io/flink-chaos-operator/stress-tools:latest"
)

// newScheme returns a runtime.Scheme with corev1 registered.
func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("adding corev1 to scheme: %v", err)
	}
	return s
}

// buildPod creates a minimal running Pod in the given namespace.
func buildPod(namespace, name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec:   corev1.PodSpec{},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

// buildRun creates a ChaosRun with a ResourceExhaustion scenario.
func buildRun(name, namespace string, spec *v1alpha1.ResourceExhaustionSpec) *v1alpha1.ChaosRun {
	return &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.ChaosRunSpec{
			Scenario: v1alpha1.ScenarioSpec{
				Type:               v1alpha1.ScenarioResourceExhaustion,
				ResourceExhaustion: spec,
			},
		},
	}
}

// defaultSpec returns a minimal ResourceExhaustionSpec for CPU mode.
func defaultSpec() *v1alpha1.ResourceExhaustionSpec {
	dur := metav1.Duration{Duration: 30 * time.Second}
	return &v1alpha1.ResourceExhaustionSpec{
		Mode:     v1alpha1.ResourceExhaustionModeCPU,
		Workers:  1,
		Duration: &dur,
	}
}

// ---------------------------------------------------------------------------
// Wrong scenario type
// ---------------------------------------------------------------------------

func TestInject_WrongScenarioType_ReturnsError(t *testing.T) {
	scheme := newScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	kubeClient := kubefake.NewSimpleClientset()
	d := resourceexhaustion.New(fakeClient, kubeClient, testImage)

	run := buildRun("test-run", testNamespace, defaultSpec())
	run.Spec.Scenario.Type = v1alpha1.ScenarioTaskManagerPodKill

	target := &interfaces.ResolvedTarget{TMPodNames: []string{"tm-0"}}

	_, err := d.Inject(context.Background(), run, target)
	if err == nil {
		t.Fatal("expected error for wrong scenario type, got nil")
	}
}

// ---------------------------------------------------------------------------
// Nil resource exhaustion spec
// ---------------------------------------------------------------------------

func TestInject_NilSpec_ReturnsError(t *testing.T) {
	scheme := newScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	kubeClient := kubefake.NewSimpleClientset()
	d := resourceexhaustion.New(fakeClient, kubeClient, testImage)

	run := buildRun("test-run", testNamespace, nil)
	target := &interfaces.ResolvedTarget{TMPodNames: []string{"tm-0"}}

	_, err := d.Inject(context.Background(), run, target)
	if err == nil {
		t.Fatal("expected error for nil resource exhaustion spec, got nil")
	}
}

// ---------------------------------------------------------------------------
// Inject creates ephemeral container and records injection
// ---------------------------------------------------------------------------

func TestInject_CPUMode_EphemeralContainerCreated(t *testing.T) {
	pod := buildPod(testNamespace, "tm-0")
	scheme := newScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	kubeClient := kubefake.NewSimpleClientset(pod)
	d := resourceexhaustion.New(fakeClient, kubeClient, testImage)

	run := buildRun("test-run", testNamespace, defaultSpec())
	target := &interfaces.ResolvedTarget{TMPodNames: []string{"tm-0"}}

	result, err := d.Inject(context.Background(), run, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.InjectedPods) != 1 || result.InjectedPods[0] != "tm-0" {
		t.Errorf("expected [tm-0] in InjectedPods, got %v", result.InjectedPods)
	}

	if len(run.Status.ResourceExhaustionInjections) != 1 {
		t.Fatalf("expected 1 injection record, got %d", len(run.Status.ResourceExhaustionInjections))
	}

	rec := run.Status.ResourceExhaustionInjections[0]
	if rec.PodName != "tm-0" {
		t.Errorf("expected PodName tm-0, got %q", rec.PodName)
	}
	if rec.ContainerName == "" {
		t.Error("expected non-empty container name")
	}
	if rec.InjectedAt == nil {
		t.Error("expected InjectedAt to be set")
	}

	// Verify the ephemeral container was submitted to the Kubernetes API.
	updatedPod, err := kubeClient.CoreV1().Pods(testNamespace).Get(
		context.Background(), "tm-0", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("fetching pod from fake kube client: %v", err)
	}
	if len(updatedPod.Spec.EphemeralContainers) == 0 {
		t.Fatal("expected ephemeral containers to be set on pod spec")
	}

	ephContainer := updatedPod.Spec.EphemeralContainers[0]
	if ephContainer.Image != testImage {
		t.Errorf("expected image %q, got %q", testImage, ephContainer.Image)
	}
}

// ---------------------------------------------------------------------------
// Idempotency: no re-injection when records already exist
// ---------------------------------------------------------------------------

func TestInject_Idempotent_NoReinjectionWhenRecordsExist(t *testing.T) {
	pod := buildPod(testNamespace, "tm-0")
	scheme := newScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	kubeClient := kubefake.NewSimpleClientset(pod)
	d := resourceexhaustion.New(fakeClient, kubeClient, testImage)

	injectedAt := metav1.Now()
	run := buildRun("test-run", testNamespace, defaultSpec())
	run.Status.SelectedPods = []string{"tm-0"}
	run.Status.ResourceExhaustionInjections = []v1alpha1.EphemeralContainerRecord{
		{
			PodName:       "tm-0",
			ContainerName: "fchaos-stress-test-run-0",
			InjectedAt:    &injectedAt,
		},
	}

	target := &interfaces.ResolvedTarget{TMPodNames: []string{"tm-0"}}

	result, err := d.Inject(context.Background(), run, target)
	if err != nil {
		t.Fatalf("unexpected error on idempotent call: %v", err)
	}

	if len(result.InjectedPods) != 1 || result.InjectedPods[0] != "tm-0" {
		t.Errorf("expected [tm-0] as already injected, got %v", result.InjectedPods)
	}

	// The injection records should still be 1 (not doubled).
	if len(run.Status.ResourceExhaustionInjections) != 1 {
		t.Errorf("expected 1 injection record after idempotent call, got %d",
			len(run.Status.ResourceExhaustionInjections))
	}
}

// ---------------------------------------------------------------------------
// Cleanup: duration elapsed marks record as cleaned up
// ---------------------------------------------------------------------------

func TestCleanup_DurationElapsed_MarksCleanedUp(t *testing.T) {
	pod := buildPod(testNamespace, "tm-0")
	scheme := newScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	kubeClient := kubefake.NewSimpleClientset(pod)
	d := resourceexhaustion.New(fakeClient, kubeClient, testImage)

	pastTime := metav1.NewTime(time.Now().Add(-5 * time.Minute))
	dur := metav1.Duration{Duration: 30 * time.Second}
	run := buildRun("test-run", testNamespace, &v1alpha1.ResourceExhaustionSpec{
		Mode:     v1alpha1.ResourceExhaustionModeCPU,
		Workers:  1,
		Duration: &dur,
	})
	run.Status.ResourceExhaustionInjections = []v1alpha1.EphemeralContainerRecord{
		{
			PodName:       "tm-0",
			ContainerName: "fchaos-stress-test-run-0",
			InjectedAt:    &pastTime,
			CleanedUp:     false,
		},
	}

	if err := d.Cleanup(context.Background(), run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !run.Status.ResourceExhaustionInjections[0].CleanedUp {
		t.Error("expected CleanedUp = true when duration has elapsed")
	}
}

// ---------------------------------------------------------------------------
// Cleanup: duration not elapsed leaves CleanedUp false
// ---------------------------------------------------------------------------

func TestCleanup_DurationNotElapsed_CleanedUpRemainsFalse(t *testing.T) {
	pod := buildPod(testNamespace, "tm-0")
	scheme := newScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	kubeClient := kubefake.NewSimpleClientset(pod)
	d := resourceexhaustion.New(fakeClient, kubeClient, testImage)

	recentTime := metav1.NewTime(time.Now().Add(-1 * time.Second))
	dur := metav1.Duration{Duration: 5 * time.Minute}
	run := buildRun("test-run", testNamespace, &v1alpha1.ResourceExhaustionSpec{
		Mode:     v1alpha1.ResourceExhaustionModeCPU,
		Workers:  1,
		Duration: &dur,
	})
	run.Status.ResourceExhaustionInjections = []v1alpha1.EphemeralContainerRecord{
		{
			PodName:       "tm-0",
			ContainerName: "fchaos-stress-test-run-0",
			InjectedAt:    &recentTime,
			CleanedUp:     false,
		},
	}

	if err := d.Cleanup(context.Background(), run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if run.Status.ResourceExhaustionInjections[0].CleanedUp {
		t.Error("expected CleanedUp = false when duration has not yet elapsed")
	}
}

// ---------------------------------------------------------------------------
// Cleanup: pod not found marks record as cleaned up
// ---------------------------------------------------------------------------

func TestCleanup_PodNotFound_MarksCleanedUp(t *testing.T) {
	scheme := newScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	kubeClient := kubefake.NewSimpleClientset()
	d := resourceexhaustion.New(fakeClient, kubeClient, testImage)

	injectedAt := metav1.NewTime(time.Now().Add(-1 * time.Second))
	dur := metav1.Duration{Duration: 5 * time.Minute}
	run := buildRun("test-run", testNamespace, &v1alpha1.ResourceExhaustionSpec{
		Mode:     v1alpha1.ResourceExhaustionModeCPU,
		Workers:  1,
		Duration: &dur,
	})
	run.Status.ResourceExhaustionInjections = []v1alpha1.EphemeralContainerRecord{
		{
			PodName:       "tm-0",
			ContainerName: "fchaos-stress-test-run-0",
			InjectedAt:    &injectedAt,
			CleanedUp:     false,
		},
	}

	if err := d.Cleanup(context.Background(), run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !run.Status.ResourceExhaustionInjections[0].CleanedUp {
		t.Error("expected CleanedUp = true when pod is not found")
	}
}

// ---------------------------------------------------------------------------
// Cleanup: already terminated container marks record as cleaned up
// ---------------------------------------------------------------------------

func TestCleanup_ContainerTerminated_MarksCleanedUp(t *testing.T) {
	containerName := "fchaos-stress-test-run-0"
	pod := buildPod(testNamespace, "tm-0")
	pod.Status.EphemeralContainerStatuses = []corev1.ContainerStatus{
		{
			Name: containerName,
			State: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{ExitCode: 0},
			},
		},
	}

	scheme := newScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	kubeClient := kubefake.NewSimpleClientset(pod)
	d := resourceexhaustion.New(fakeClient, kubeClient, testImage)

	// Long duration so it is the container state — not elapsed time — that triggers cleanup.
	recentTime := metav1.NewTime(time.Now().Add(-1 * time.Second))
	dur := metav1.Duration{Duration: 1 * time.Hour}
	run := buildRun("test-run", testNamespace, &v1alpha1.ResourceExhaustionSpec{
		Mode:     v1alpha1.ResourceExhaustionModeCPU,
		Workers:  1,
		Duration: &dur,
	})
	run.Status.ResourceExhaustionInjections = []v1alpha1.EphemeralContainerRecord{
		{
			PodName:       "tm-0",
			ContainerName: containerName,
			InjectedAt:    &recentTime,
			CleanedUp:     false,
		},
	}

	if err := d.Cleanup(context.Background(), run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !run.Status.ResourceExhaustionInjections[0].CleanedUp {
		t.Error("expected CleanedUp = true when container is terminated")
	}
}
