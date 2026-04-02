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

package networkchaos_test

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/interfaces"
	"github.com/flink-chaos-operator/internal/scenario/networkchaos"
)

const (
	testNamespace = "default"
	testImage     = "ghcr.io/flink-chaos-operator/tc-tools:latest"
)

// newScheme returns a runtime.Scheme with corev1 registered so the fake
// controller-runtime client can handle Pod objects.
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
		Spec: corev1.PodSpec{},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}
}

// buildRun creates a ChaosRun with a NetworkChaos scenario.
func buildRun(name, namespace string, netSpec *v1alpha1.NetworkChaosSpec) *v1alpha1.ChaosRun {
	return &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.ChaosRunSpec{
			Scenario: v1alpha1.ScenarioSpec{
				Type:    v1alpha1.ScenarioNetworkChaos,
				Network: netSpec,
			},
		},
	}
}

// defaultNetSpec returns a minimal NetworkChaosSpec with a 100ms latency rule.
func defaultNetSpec() *v1alpha1.NetworkChaosSpec {
	latency := metav1.Duration{Duration: 100 * time.Millisecond}
	dur := metav1.Duration{Duration: 30 * time.Second}
	return &v1alpha1.NetworkChaosSpec{
		Target:   v1alpha1.NetworkTargetTMtoJM,
		Latency:  &latency,
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
	d := networkchaos.New(fakeClient, kubeClient, testImage)

	run := buildRun("test-run", testNamespace, defaultNetSpec())
	run.Spec.Scenario.Type = v1alpha1.ScenarioTaskManagerPodKill

	target := &interfaces.ResolvedTarget{TMPodNames: []string{"tm-0"}}

	_, err := d.Inject(context.Background(), run, target)
	if err == nil {
		t.Fatal("expected error for wrong scenario type, got nil")
	}
}

// ---------------------------------------------------------------------------
// Nil network spec
// ---------------------------------------------------------------------------

func TestInject_NilNetworkSpec_ReturnsError(t *testing.T) {
	scheme := newScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	kubeClient := kubefake.NewSimpleClientset()
	d := networkchaos.New(fakeClient, kubeClient, testImage)

	run := buildRun("test-run", testNamespace, nil)
	target := &interfaces.ResolvedTarget{TMPodNames: []string{"tm-0"}}

	_, err := d.Inject(context.Background(), run, target)
	if err == nil {
		t.Fatal("expected error for nil network spec, got nil")
	}
}

// ---------------------------------------------------------------------------
// Inject with latency rule
// ---------------------------------------------------------------------------

func TestInject_LatencyRule_EphemeralContainerCreated(t *testing.T) {
	pod := buildPod(testNamespace, "tm-0")
	scheme := newScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	// Pre-register the pod in the typed fake client as well.
	kubeClient := kubefake.NewSimpleClientset(pod)
	d := networkchaos.New(fakeClient, kubeClient, testImage)

	latency := metav1.Duration{Duration: 100 * time.Millisecond}
	dur := metav1.Duration{Duration: 30 * time.Second}
	run := buildRun("test-run", testNamespace, &v1alpha1.NetworkChaosSpec{
		Target:   v1alpha1.NetworkTargetTMtoJM,
		Latency:  &latency,
		Duration: &dur,
	})

	target := &interfaces.ResolvedTarget{TMPodNames: []string{"tm-0"}}

	result, err := d.Inject(context.Background(), run, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.InjectedPods) != 1 || result.InjectedPods[0] != "tm-0" {
		t.Errorf("expected [tm-0] in InjectedPods, got %v", result.InjectedPods)
	}

	if len(run.Status.EphemeralContainerInjections) != 1 {
		t.Fatalf("expected 1 injection record, got %d", len(run.Status.EphemeralContainerInjections))
	}

	rec := run.Status.EphemeralContainerInjections[0]
	if rec.PodName != "tm-0" {
		t.Errorf("expected PodName tm-0, got %q", rec.PodName)
	}
	if rec.ContainerName == "" {
		t.Error("expected non-empty container name")
	}
	if rec.InjectedAt == nil {
		t.Error("expected InjectedAt to be set")
	}

	// Verify the ephemeral container was actually submitted to the Kubernetes API.
	updatedPod, err := kubeClient.CoreV1().Pods(testNamespace).Get(context.Background(), "tm-0", metav1.GetOptions{})
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

	// Verify the command contains the latency value.
	cmd := strings.Join(ephContainer.Command, " ")
	if !strings.Contains(cmd, "delay") {
		t.Errorf("expected tc command to contain 'delay', got: %q", cmd)
	}

	// Verify NET_ADMIN capability.
	if ephContainer.SecurityContext == nil || ephContainer.SecurityContext.Capabilities == nil {
		t.Fatal("expected security context with capabilities")
	}
	hasNetAdmin := false
	for _, cap := range ephContainer.SecurityContext.Capabilities.Add {
		if cap == "NET_ADMIN" {
			hasNetAdmin = true
		}
	}
	if !hasNetAdmin {
		t.Error("expected NET_ADMIN capability")
	}
}

// ---------------------------------------------------------------------------
// Inject with bandwidth rule
// ---------------------------------------------------------------------------

func TestInject_BandwidthRule_CommandContainsTBF(t *testing.T) {
	pod := buildPod(testNamespace, "tm-0")
	scheme := newScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	kubeClient := kubefake.NewSimpleClientset(pod)
	d := networkchaos.New(fakeClient, kubeClient, testImage)

	dur := metav1.Duration{Duration: 30 * time.Second}
	run := buildRun("test-run", testNamespace, &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTargetTMtoExternal,
		Bandwidth: "10mbit",
		Duration:  &dur,
	})

	target := &interfaces.ResolvedTarget{TMPodNames: []string{"tm-0"}}

	result, err := d.Inject(context.Background(), run, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.InjectedPods) != 1 {
		t.Fatalf("expected 1 injected pod, got %d", len(result.InjectedPods))
	}

	updatedPod, err := kubeClient.CoreV1().Pods(testNamespace).Get(context.Background(), "tm-0", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("fetching pod: %v", err)
	}
	if len(updatedPod.Spec.EphemeralContainers) == 0 {
		t.Fatal("expected ephemeral containers on pod")
	}

	cmd := strings.Join(updatedPod.Spec.EphemeralContainers[0].Command, " ")
	if !strings.Contains(cmd, "tbf") {
		t.Errorf("expected tc tbf syntax in command, got: %q", cmd)
	}
	if !strings.Contains(cmd, "10mbit") {
		t.Errorf("expected bandwidth '10mbit' in command, got: %q", cmd)
	}
}

// ---------------------------------------------------------------------------
// Idempotency
// ---------------------------------------------------------------------------

func TestInject_Idempotent_NoReinjectionWhenRecordsExist(t *testing.T) {
	pod := buildPod(testNamespace, "tm-0")
	scheme := newScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	kubeClient := kubefake.NewSimpleClientset(pod)
	d := networkchaos.New(fakeClient, kubeClient, testImage)

	injectedAt := metav1.Now()
	run := buildRun("test-run", testNamespace, defaultNetSpec())
	// Pre-populate the injection records to simulate a previous reconcile.
	run.Status.EphemeralContainerInjections = []v1alpha1.EphemeralContainerRecord{
		{
			PodName:       "tm-0",
			ContainerName: "fchaos-tc-test-run-0",
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
	if len(run.Status.EphemeralContainerInjections) != 1 {
		t.Errorf("expected 1 injection record after idempotent call, got %d",
			len(run.Status.EphemeralContainerInjections))
	}

	// The typed kube client should have received no UpdateEphemeralContainers call.
	actions := kubeClient.Actions()
	for _, a := range actions {
		if a.GetVerb() == "update" && strings.Contains(a.GetResource().Resource, "pod") {
			t.Errorf("unexpected update action on pods: %v", a)
		}
	}
}

// ---------------------------------------------------------------------------
// Cleanup: duration elapsed → CleanedUp = true
// ---------------------------------------------------------------------------

func TestCleanup_DurationElapsed_MarksCleanedUp(t *testing.T) {
	pod := buildPod(testNamespace, "tm-0")
	scheme := newScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	kubeClient := kubefake.NewSimpleClientset(pod)
	d := networkchaos.New(fakeClient, kubeClient, testImage)

	// Set InjectedAt far in the past so the duration has definitely elapsed.
	pastTime := metav1.NewTime(time.Now().Add(-5 * time.Minute))
	dur := metav1.Duration{Duration: 30 * time.Second}
	run := buildRun("test-run", testNamespace, &v1alpha1.NetworkChaosSpec{
		Target:   v1alpha1.NetworkTargetTMtoJM,
		Duration: &dur,
	})
	run.Status.EphemeralContainerInjections = []v1alpha1.EphemeralContainerRecord{
		{
			PodName:       "tm-0",
			ContainerName: "fchaos-tc-test-run-0",
			InjectedAt:    &pastTime,
			CleanedUp:     false,
		},
	}

	if err := d.Cleanup(context.Background(), run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !run.Status.EphemeralContainerInjections[0].CleanedUp {
		t.Error("expected CleanedUp = true when duration has elapsed")
	}
}

// ---------------------------------------------------------------------------
// Cleanup: duration not elapsed → CleanedUp remains false
// ---------------------------------------------------------------------------

func TestCleanup_DurationNotElapsed_CleanedUpRemainsFlase(t *testing.T) {
	pod := buildPod(testNamespace, "tm-0")
	scheme := newScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	kubeClient := kubefake.NewSimpleClientset(pod)
	d := networkchaos.New(fakeClient, kubeClient, testImage)

	// Set InjectedAt very recently so the duration has not elapsed.
	recentTime := metav1.NewTime(time.Now().Add(-1 * time.Second))
	dur := metav1.Duration{Duration: 5 * time.Minute}
	run := buildRun("test-run", testNamespace, &v1alpha1.NetworkChaosSpec{
		Target:   v1alpha1.NetworkTargetTMtoJM,
		Duration: &dur,
	})
	run.Status.EphemeralContainerInjections = []v1alpha1.EphemeralContainerRecord{
		{
			PodName:       "tm-0",
			ContainerName: "fchaos-tc-test-run-0",
			InjectedAt:    &recentTime,
			CleanedUp:     false,
		},
	}

	if err := d.Cleanup(context.Background(), run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if run.Status.EphemeralContainerInjections[0].CleanedUp {
		t.Error("expected CleanedUp = false when duration has not yet elapsed")
	}
}

// ---------------------------------------------------------------------------
// Cleanup: pod not found → CleanedUp = true
// ---------------------------------------------------------------------------

func TestCleanup_PodNotFound_MarksCleanedUp(t *testing.T) {
	// Do not add any pod to the fake client — simulate pod already deleted.
	scheme := newScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	kubeClient := kubefake.NewSimpleClientset()
	d := networkchaos.New(fakeClient, kubeClient, testImage)

	injectedAt := metav1.NewTime(time.Now().Add(-1 * time.Second))
	dur := metav1.Duration{Duration: 5 * time.Minute}
	run := buildRun("test-run", testNamespace, &v1alpha1.NetworkChaosSpec{
		Target:   v1alpha1.NetworkTargetTMtoJM,
		Duration: &dur,
	})
	run.Status.EphemeralContainerInjections = []v1alpha1.EphemeralContainerRecord{
		{
			PodName:       "tm-0",
			ContainerName: "fchaos-tc-test-run-0",
			InjectedAt:    &injectedAt,
			CleanedUp:     false,
		},
	}

	if err := d.Cleanup(context.Background(), run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !run.Status.EphemeralContainerInjections[0].CleanedUp {
		t.Error("expected CleanedUp = true when pod is not found")
	}
}

// ---------------------------------------------------------------------------
// Cleanup: already terminated container → CleanedUp = true
// ---------------------------------------------------------------------------

func TestCleanup_ContainerTerminated_MarksCleanedUp(t *testing.T) {
	containerName := "fchaos-tc-test-run-0"
	pod := buildPod(testNamespace, "tm-0")
	// Add an ephemeral container status showing it has terminated.
	pod.Status.EphemeralContainerStatuses = []corev1.ContainerStatus{
		{
			Name: containerName,
			State: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{
					ExitCode: 0,
				},
			},
		},
	}

	scheme := newScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	kubeClient := kubefake.NewSimpleClientset(pod)
	d := networkchaos.New(fakeClient, kubeClient, testImage)

	// Use a long duration to confirm it's the container state — not elapsed time —
	// that triggers the cleanup.
	recentTime := metav1.NewTime(time.Now().Add(-1 * time.Second))
	dur := metav1.Duration{Duration: 1 * time.Hour}
	run := buildRun("test-run", testNamespace, &v1alpha1.NetworkChaosSpec{
		Target:   v1alpha1.NetworkTargetTMtoJM,
		Duration: &dur,
	})
	run.Status.EphemeralContainerInjections = []v1alpha1.EphemeralContainerRecord{
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

	if !run.Status.EphemeralContainerInjections[0].CleanedUp {
		t.Error("expected CleanedUp = true when container is terminated")
	}
}
