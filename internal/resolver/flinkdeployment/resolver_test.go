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

package flinkdeployment_test

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/resolver/flinkdeployment"
)

const (
	testNamespace      = "flink-test"
	testDeploymentName = "my-flink-app"
)

// buildScheme returns a Scheme that includes corev1 and the chaos v1alpha1
// types. The fake client uses this to serialise and deserialise objects; no
// FlinkDeployment CRD scheme registration is needed because we register
// FlinkDeployment objects as unstructured.
func buildScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = v1alpha1.AddToScheme(s)
	return s
}

// flinkDeployment builds an unstructured FlinkDeployment object. Pass a
// non-empty executionTarget (e.g. "session") to populate the
// spec.flinkConfiguration["execution.target"] field.
func flinkDeployment(namespace, name, executionTarget string) *unstructured.Unstructured {
	fd := &unstructured.Unstructured{}
	fd.SetGroupVersionKind(flinkDeploymentGVK())
	fd.SetNamespace(namespace)
	fd.SetName(name)

	if executionTarget != "" {
		_ = unstructured.SetNestedField(fd.Object,
			map[string]interface{}{
				"execution.target": executionTarget,
			},
			"spec", "flinkConfiguration",
		)
	}

	return fd
}

// flinkDeploymentGVK returns the GVK for FlinkDeployment resources.
// This mirrors the constant in the resolver package.
func flinkDeploymentGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "flink.apache.org",
		Version: "v1beta1",
		Kind:    "FlinkDeployment",
	}
}

// tmPod builds a Running TaskManager pod for the given FlinkDeployment using
// the "app" and "component" label conventions.
func tmPod(namespace, name, deploymentName string) *corev1.Pod {
	return podWithLabels(namespace, name, map[string]string{
		"app":       deploymentName,
		"component": "taskmanager",
	}, corev1.PodRunning)
}

// jmPod builds a Running JobManager pod for the given FlinkDeployment using
// the "app" and "component" label conventions.
func jmPod(namespace, name, deploymentName string) *corev1.Pod {
	return podWithLabels(namespace, name, map[string]string{
		"app":       deploymentName,
		"component": "jobmanager",
	}, corev1.PodRunning)
}

// tmPodKubernetesLabels builds a Running TaskManager pod using the
// "app.kubernetes.io/name" and "app.kubernetes.io/component" label conventions.
func tmPodKubernetesLabels(namespace, name, deploymentName string) *corev1.Pod {
	return podWithLabels(namespace, name, map[string]string{
		"app.kubernetes.io/name":      deploymentName,
		"app.kubernetes.io/component": "taskmanager",
	}, corev1.PodRunning)
}

// jmPodKubernetesLabels builds a Running JobManager pod using the
// "app.kubernetes.io/name" and "app.kubernetes.io/component" label conventions.
func jmPodKubernetesLabels(namespace, name, deploymentName string) *corev1.Pod {
	return podWithLabels(namespace, name, map[string]string{
		"app.kubernetes.io/name":      deploymentName,
		"app.kubernetes.io/component": "jobmanager",
	}, corev1.PodRunning)
}

// podWithLabels builds a pod with the given labels and phase.
func podWithLabels(namespace, name string, labels map[string]string, phase corev1.PodPhase) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    labels,
		},
		Status: corev1.PodStatus{Phase: phase},
	}
}

// terminatingPod builds a Running TM pod that has its DeletionTimestamp set,
// simulating a pod in the process of termination.
func terminatingPod(namespace, name, deploymentName string) *corev1.Pod {
	now := metav1.NewTime(time.Now())
	p := tmPod(namespace, name, deploymentName)
	p.DeletionTimestamp = &now
	// The fake client requires a non-zero Finalizer for objects with a
	// DeletionTimestamp to be stored correctly.
	p.Finalizers = []string{"test/keep"}
	return p
}

// chaosRun builds a minimal ChaosRun targeting the given FlinkDeployment.
func chaosRun(namespace, deploymentName string) *v1alpha1.ChaosRun {
	return &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "test-chaos-run",
		},
		Spec: v1alpha1.ChaosRunSpec{
			Target: v1alpha1.TargetSpec{
				Type: v1alpha1.TargetFlinkDeployment,
				Name: deploymentName,
			},
		},
	}
}

// chaosRunWrongType builds a ChaosRun with a non-FlinkDeployment target type.
func chaosRunWrongType(namespace string) *v1alpha1.ChaosRun {
	return &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "test-chaos-run",
		},
		Spec: v1alpha1.ChaosRunSpec{
			Target: v1alpha1.TargetSpec{
				Type: v1alpha1.TargetPodSelector,
			},
		},
	}
}

// TestResolve_WrongTargetType verifies that an error is returned when the
// ChaosRun target type is not FlinkDeployment.
func TestResolve_WrongTargetType(t *testing.T) {
	r := &flinkdeployment.Resolver{
		Client: fake.NewClientBuilder().WithScheme(buildScheme()).Build(),
	}

	_, err := r.Resolve(context.Background(), chaosRunWrongType(testNamespace))
	if err == nil {
		t.Fatal("expected error for wrong target type, got nil")
	}
}

// TestResolve_FlinkDeploymentNotFound verifies that a descriptive error is
// returned when the named FlinkDeployment does not exist.
func TestResolve_FlinkDeploymentNotFound(t *testing.T) {
	r := &flinkdeployment.Resolver{
		Client: fake.NewClientBuilder().WithScheme(buildScheme()).Build(),
	}

	_, err := r.Resolve(context.Background(), chaosRun(testNamespace, testDeploymentName))
	if err == nil {
		t.Fatal("expected error when FlinkDeployment is not found, got nil")
	}
}

// TestResolve_TMPodsFound verifies that a FlinkDeployment with running TM pods
// is resolved to the correct platform, logical name, and TM pod name list.
func TestResolve_TMPodsFound(t *testing.T) {
	fd := flinkDeployment(testNamespace, testDeploymentName, "")
	tm0 := tmPod(testNamespace, "tm-0", testDeploymentName)
	tm1 := tmPod(testNamespace, "tm-1", testDeploymentName)

	r := &flinkdeployment.Resolver{
		Client: fake.NewClientBuilder().
			WithScheme(buildScheme()).
			WithObjects(fd, tm0, tm1).
			Build(),
	}

	target, err := r.Resolve(context.Background(), chaosRun(testNamespace, testDeploymentName))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if target.Platform != "flink-operator" {
		t.Errorf("expected platform %q, got %q", "flink-operator", target.Platform)
	}
	if target.LogicalName != testDeploymentName {
		t.Errorf("expected logicalName %q, got %q", testDeploymentName, target.LogicalName)
	}
	if len(target.TMPodNames) != 2 {
		t.Errorf("expected 2 TM pods, got %d", len(target.TMPodNames))
	}
}

// TestResolve_TMAndJMPodsFound verifies that both TM and JM pod sets are
// populated correctly when both kinds of pods are present.
func TestResolve_TMAndJMPodsFound(t *testing.T) {
	fd := flinkDeployment(testNamespace, testDeploymentName, "")
	tm0 := tmPod(testNamespace, "tm-0", testDeploymentName)
	jm0 := jmPod(testNamespace, "jm-0", testDeploymentName)

	r := &flinkdeployment.Resolver{
		Client: fake.NewClientBuilder().
			WithScheme(buildScheme()).
			WithObjects(fd, tm0, jm0).
			Build(),
	}

	target, err := r.Resolve(context.Background(), chaosRun(testNamespace, testDeploymentName))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(target.TMPodNames) != 1 {
		t.Errorf("expected 1 TM pod, got %d: %v", len(target.TMPodNames), target.TMPodNames)
	}
	if len(target.JMPodNames) != 1 {
		t.Errorf("expected 1 JM pod, got %d: %v", len(target.JMPodNames), target.JMPodNames)
	}
	if target.TMPodNames[0] != "tm-0" {
		t.Errorf("expected TM pod name %q, got %q", "tm-0", target.TMPodNames[0])
	}
	if target.JMPodNames[0] != "jm-0" {
		t.Errorf("expected JM pod name %q, got %q", "jm-0", target.JMPodNames[0])
	}
}

// TestResolve_NoTMPodsFound verifies that an error is returned when no
// TaskManager pods are found for the given FlinkDeployment.
func TestResolve_NoTMPodsFound(t *testing.T) {
	fd := flinkDeployment(testNamespace, testDeploymentName, "")
	// Only a JM pod — no TM pods.
	jm0 := jmPod(testNamespace, "jm-0", testDeploymentName)

	r := &flinkdeployment.Resolver{
		Client: fake.NewClientBuilder().
			WithScheme(buildScheme()).
			WithObjects(fd, jm0).
			Build(),
	}

	_, err := r.Resolve(context.Background(), chaosRun(testNamespace, testDeploymentName))
	if err == nil {
		t.Fatal("expected error when no TM pods found, got nil")
	}
}

// TestResolve_SessionMode_SharedCluster verifies that a FlinkDeployment whose
// execution.target is "session" yields SharedCluster: true.
func TestResolve_SessionMode_SharedCluster(t *testing.T) {
	fd := flinkDeployment(testNamespace, testDeploymentName, "session")
	tm0 := tmPod(testNamespace, "tm-0", testDeploymentName)

	r := &flinkdeployment.Resolver{
		Client: fake.NewClientBuilder().
			WithScheme(buildScheme()).
			WithObjects(fd, tm0).
			Build(),
	}

	target, err := r.Resolve(context.Background(), chaosRun(testNamespace, testDeploymentName))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !target.SharedCluster {
		t.Error("expected SharedCluster=true for session-mode FlinkDeployment")
	}
}

// TestResolve_ApplicationMode_NotSharedCluster verifies that a FlinkDeployment
// without an execution.target field yields SharedCluster: false.
func TestResolve_ApplicationMode_NotSharedCluster(t *testing.T) {
	fd := flinkDeployment(testNamespace, testDeploymentName, "") // no execution.target
	tm0 := tmPod(testNamespace, "tm-0", testDeploymentName)

	r := &flinkdeployment.Resolver{
		Client: fake.NewClientBuilder().
			WithScheme(buildScheme()).
			WithObjects(fd, tm0).
			Build(),
	}

	target, err := r.Resolve(context.Background(), chaosRun(testNamespace, testDeploymentName))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if target.SharedCluster {
		t.Error("expected SharedCluster=false for application-mode FlinkDeployment")
	}
}

// TestResolve_TerminatingPodsExcluded verifies that pods with a non-nil
// DeletionTimestamp are not counted as live TM pods.
func TestResolve_TerminatingPodsExcluded(t *testing.T) {
	fd := flinkDeployment(testNamespace, testDeploymentName, "")
	// tm-0 is terminating; tm-1 is live.
	terminating := terminatingPod(testNamespace, "tm-0", testDeploymentName)
	live := tmPod(testNamespace, "tm-1", testDeploymentName)

	r := &flinkdeployment.Resolver{
		Client: fake.NewClientBuilder().
			WithScheme(buildScheme()).
			WithObjects(fd, terminating, live).
			Build(),
	}

	target, err := r.Resolve(context.Background(), chaosRun(testNamespace, testDeploymentName))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(target.TMPodNames) != 1 {
		t.Errorf("expected 1 TM pod (excluding terminating), got %d: %v", len(target.TMPodNames), target.TMPodNames)
	}
	if target.TMPodNames[0] != "tm-1" {
		t.Errorf("expected live pod %q, got %q", "tm-1", target.TMPodNames[0])
	}
}

// TestResolve_NonRunningPendingPodsExcluded verifies that pods in phases other
// than Running or Pending (e.g. Failed, Succeeded) are excluded from the pod
// sets. Only Running pods are included in this test.
func TestResolve_NonRunningPendingPodsExcluded(t *testing.T) {
	fd := flinkDeployment(testNamespace, testDeploymentName, "")

	// tm-failed is in the Failed phase and must be excluded.
	tmFailed := podWithLabels(testNamespace, "tm-failed", map[string]string{
		"app":       testDeploymentName,
		"component": "taskmanager",
	}, corev1.PodFailed)

	// tm-succeeded is in the Succeeded phase and must be excluded.
	tmSucceeded := podWithLabels(testNamespace, "tm-succeeded", map[string]string{
		"app":       testDeploymentName,
		"component": "taskmanager",
	}, corev1.PodSucceeded)

	// tm-running is Running and must be included.
	tmRunning := tmPod(testNamespace, "tm-running", testDeploymentName)

	r := &flinkdeployment.Resolver{
		Client: fake.NewClientBuilder().
			WithScheme(buildScheme()).
			WithObjects(fd, tmFailed, tmSucceeded, tmRunning).
			Build(),
	}

	target, err := r.Resolve(context.Background(), chaosRun(testNamespace, testDeploymentName))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(target.TMPodNames) != 1 {
		t.Errorf("expected 1 TM pod (only running), got %d: %v", len(target.TMPodNames), target.TMPodNames)
	}
	if target.TMPodNames[0] != "tm-running" {
		t.Errorf("expected pod %q, got %q", "tm-running", target.TMPodNames[0])
	}
}

// TestResolve_KubernetesLabels verifies that pods using the
// "app.kubernetes.io/name" and "app.kubernetes.io/component" labels are
// correctly discovered.
func TestResolve_KubernetesLabels(t *testing.T) {
	fd := flinkDeployment(testNamespace, testDeploymentName, "")
	tm0 := tmPodKubernetesLabels(testNamespace, "tm-0", testDeploymentName)
	jm0 := jmPodKubernetesLabels(testNamespace, "jm-0", testDeploymentName)

	r := &flinkdeployment.Resolver{
		Client: fake.NewClientBuilder().
			WithScheme(buildScheme()).
			WithObjects(fd, tm0, jm0).
			Build(),
	}

	target, err := r.Resolve(context.Background(), chaosRun(testNamespace, testDeploymentName))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(target.TMPodNames) != 1 {
		t.Errorf("expected 1 TM pod, got %d", len(target.TMPodNames))
	}
	if len(target.JMPodNames) != 1 {
		t.Errorf("expected 1 JM pod, got %d", len(target.JMPodNames))
	}
}
