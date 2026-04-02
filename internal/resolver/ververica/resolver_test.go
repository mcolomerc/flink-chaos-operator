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

package ververica_test

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/resolver/ververica"
)

const (
	testNamespace      = "flink-test"
	testDeploymentID   = "dep-uuid-1234"
	testDeploymentName = "my-vvp-app"
	testVVPNamespace   = "vvp-ns-prod"
)

// buildScheme returns a Scheme that includes corev1 and the chaos v1alpha1
// types, as required by the fake client.
func buildScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = v1alpha1.AddToScheme(s)
	return s
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

// tmPodByID builds a Running TaskManager pod identified by a deployment-id
// label (flink.apache.org prefix).
func tmPodByID(namespace, name, deploymentID string) *corev1.Pod {
	return podWithLabels(namespace, name, map[string]string{
		"flink.apache.org/deployment-id": deploymentID,
		"component":                      "taskmanager",
	}, corev1.PodRunning)
}

// tmPodByIDVvp builds a Running TaskManager pod identified by a deployment-id
// label (vvp.io prefix).
func tmPodByIDVvp(namespace, name, deploymentID string) *corev1.Pod {
	return podWithLabels(namespace, name, map[string]string{
		"vvp.io/deployment-id": deploymentID,
		"component":            "taskmanager",
	}, corev1.PodRunning)
}

// tmPodByName builds a Running TaskManager pod identified by a deployment-name
// label (flink.apache.org prefix).
func tmPodByName(namespace, name, deploymentName string) *corev1.Pod {
	return podWithLabels(namespace, name, map[string]string{
		"flink.apache.org/deployment-name": deploymentName,
		"component":                        "taskmanager",
	}, corev1.PodRunning)
}

// tmPodByNameWithNS builds a Running TaskManager pod matched by name and
// carrying a VVP namespace label (flink.apache.org prefix).
func tmPodByNameWithNS(namespace, name, deploymentName, vvpNS string) *corev1.Pod {
	return podWithLabels(namespace, name, map[string]string{
		"flink.apache.org/deployment-name": deploymentName,
		"flink.apache.org/namespace":       vvpNS,
		"component":                        "taskmanager",
	}, corev1.PodRunning)
}

// jmPodByID builds a Running JobManager pod identified by a deployment-id
// label and the flink.apache.org/component label.
func jmPodByID(namespace, name, deploymentID string) *corev1.Pod {
	return podWithLabels(namespace, name, map[string]string{
		"flink.apache.org/deployment-id":        deploymentID,
		"flink.apache.org/component":            "jobmanager",
	}, corev1.PodRunning)
}

// sessionTMPodByID builds a Running TaskManager pod with a session-mode label.
func sessionTMPodByID(namespace, name, deploymentID string) *corev1.Pod {
	return podWithLabels(namespace, name, map[string]string{
		"flink.apache.org/deployment-id":   deploymentID,
		"component":                        "taskmanager",
		"flink.apache.org/cluster-mode":    "session",
	}, corev1.PodRunning)
}

// terminatingTMPod builds a TM pod with DeletionTimestamp set, simulating
// a pod in the process of termination.
func terminatingTMPod(namespace, name, deploymentID string) *corev1.Pod {
	now := metav1.NewTime(time.Now())
	p := tmPodByID(namespace, name, deploymentID)
	p.DeletionTimestamp = &now
	p.Finalizers = []string{"test/keep"}
	return p
}

// chaosRunByID builds a ChaosRun targeting a VervericaDeployment by deployment ID.
func chaosRunByID(namespace, deploymentID string) *v1alpha1.ChaosRun {
	return &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "test-chaos-run",
		},
		Spec: v1alpha1.ChaosRunSpec{
			Target: v1alpha1.TargetSpec{
				Type:         v1alpha1.TargetVervericaDeployment,
				DeploymentID: deploymentID,
			},
		},
	}
}

// chaosRunByName builds a ChaosRun targeting a VervericaDeployment by
// deployment name (no ID).
func chaosRunByName(namespace, deploymentName string) *v1alpha1.ChaosRun {
	return &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "test-chaos-run",
		},
		Spec: v1alpha1.ChaosRunSpec{
			Target: v1alpha1.TargetSpec{
				Type:           v1alpha1.TargetVervericaDeployment,
				DeploymentName: deploymentName,
			},
		},
	}
}

// chaosRunByNameAndNS builds a ChaosRun targeting a VervericaDeployment by
// deployment name scoped to a VVP namespace.
func chaosRunByNameAndNS(namespace, deploymentName, vvpNS string) *v1alpha1.ChaosRun {
	return &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "test-chaos-run",
		},
		Spec: v1alpha1.ChaosRunSpec{
			Target: v1alpha1.TargetSpec{
				Type:           v1alpha1.TargetVervericaDeployment,
				DeploymentName: deploymentName,
				VVPNamespace:   vvpNS,
			},
		},
	}
}

// chaosRunByIDAndName builds a ChaosRun with both DeploymentID and
// DeploymentName set (ID should take precedence).
func chaosRunByIDAndName(namespace, deploymentID, deploymentName string) *v1alpha1.ChaosRun {
	return &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "test-chaos-run",
		},
		Spec: v1alpha1.ChaosRunSpec{
			Target: v1alpha1.TargetSpec{
				Type:           v1alpha1.TargetVervericaDeployment,
				DeploymentID:   deploymentID,
				DeploymentName: deploymentName,
			},
		},
	}
}

// chaosRunWrongType builds a ChaosRun with a non-VervericaDeployment target type.
func chaosRunWrongType(namespace string) *v1alpha1.ChaosRun {
	return &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "test-chaos-run",
		},
		Spec: v1alpha1.ChaosRunSpec{
			Target: v1alpha1.TargetSpec{
				Type: v1alpha1.TargetFlinkDeployment,
			},
		},
	}
}

// chaosRunNoIdentifier builds a ChaosRun of the right type but with neither
// DeploymentID nor DeploymentName set.
func chaosRunNoIdentifier(namespace string) *v1alpha1.ChaosRun {
	return &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "test-chaos-run",
		},
		Spec: v1alpha1.ChaosRunSpec{
			Target: v1alpha1.TargetSpec{
				Type: v1alpha1.TargetVervericaDeployment,
			},
		},
	}
}

// TestResolve_WrongTargetType verifies that an error is returned when the
// ChaosRun target type is not VervericaDeployment.
func TestResolve_WrongTargetType(t *testing.T) {
	r := &ververica.Resolver{
		Client: fake.NewClientBuilder().WithScheme(buildScheme()).Build(),
	}

	_, err := r.Resolve(context.Background(), chaosRunWrongType(testNamespace))
	if err == nil {
		t.Fatal("expected error for wrong target type, got nil")
	}
}

// TestResolve_NilVervericaTarget verifies that an error is returned when
// neither DeploymentID nor DeploymentName is provided (equivalent to a nil
// Ververica target in a flat-field schema).
func TestResolve_NilVervericaTarget(t *testing.T) {
	r := &ververica.Resolver{
		Client: fake.NewClientBuilder().WithScheme(buildScheme()).Build(),
	}

	_, err := r.Resolve(context.Background(), chaosRunNoIdentifier(testNamespace))
	if err == nil {
		t.Fatal("expected error when no deployment identifier is set, got nil")
	}
}

// TestResolve_MatchByDeploymentID verifies that pods are matched by
// deployment-id label and classified as TaskManagers correctly.
func TestResolve_MatchByDeploymentID(t *testing.T) {
	tm0 := tmPodByID(testNamespace, "tm-0", testDeploymentID)
	tm1 := tmPodByID(testNamespace, "tm-1", testDeploymentID)

	r := &ververica.Resolver{
		Client: fake.NewClientBuilder().
			WithScheme(buildScheme()).
			WithObjects(tm0, tm1).
			Build(),
	}

	target, err := r.Resolve(context.Background(), chaosRunByID(testNamespace, testDeploymentID))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if target.Platform != "ververica" {
		t.Errorf("expected platform %q, got %q", "ververica", target.Platform)
	}
	if target.LogicalName != testDeploymentID {
		t.Errorf("expected logicalName %q, got %q", testDeploymentID, target.LogicalName)
	}
	if len(target.TMPodNames) != 2 {
		t.Errorf("expected 2 TM pods, got %d: %v", len(target.TMPodNames), target.TMPodNames)
	}
}

// TestResolve_MatchByDeploymentNameOnly verifies that pods are matched by
// deployment-name label when no DeploymentID is provided.
func TestResolve_MatchByDeploymentNameOnly(t *testing.T) {
	tm0 := tmPodByName(testNamespace, "tm-0", testDeploymentName)
	tm1 := tmPodByName(testNamespace, "tm-1", testDeploymentName)

	r := &ververica.Resolver{
		Client: fake.NewClientBuilder().
			WithScheme(buildScheme()).
			WithObjects(tm0, tm1).
			Build(),
	}

	target, err := r.Resolve(context.Background(), chaosRunByName(testNamespace, testDeploymentName))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if target.LogicalName != testDeploymentName {
		t.Errorf("expected logicalName %q, got %q", testDeploymentName, target.LogicalName)
	}
	if len(target.TMPodNames) != 2 {
		t.Errorf("expected 2 TM pods, got %d: %v", len(target.TMPodNames), target.TMPodNames)
	}
}

// TestResolve_MatchByDeploymentNameAndVVPNamespace verifies that when a
// VVPNamespace filter is set, only pods with a matching namespace label are
// included.
func TestResolve_MatchByDeploymentNameAndVVPNamespace(t *testing.T) {
	tmMatch := tmPodByNameWithNS(testNamespace, "tm-match", testDeploymentName, testVVPNamespace)
	tmOther := tmPodByNameWithNS(testNamespace, "tm-other", testDeploymentName, "vvp-ns-dev")

	r := &ververica.Resolver{
		Client: fake.NewClientBuilder().
			WithScheme(buildScheme()).
			WithObjects(tmMatch, tmOther).
			Build(),
	}

	target, err := r.Resolve(context.Background(), chaosRunByNameAndNS(testNamespace, testDeploymentName, testVVPNamespace))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(target.TMPodNames) != 1 {
		t.Errorf("expected 1 TM pod, got %d: %v", len(target.TMPodNames), target.TMPodNames)
	}
	if target.TMPodNames[0] != "tm-match" {
		t.Errorf("expected pod %q, got %q", "tm-match", target.TMPodNames[0])
	}
}

// TestResolve_VVPNamespaceFilterExcludesDifferentNamespace verifies that
// pods whose namespace label does not match the requested VVPNamespace are
// excluded even if the deployment-name label matches.
func TestResolve_VVPNamespaceFilterExcludesDifferentNamespace(t *testing.T) {
	// Pod with a different VVP namespace — must be excluded.
	tmWrong := tmPodByNameWithNS(testNamespace, "tm-wrong-ns", testDeploymentName, "vvp-ns-dev")
	// Pod without a namespace label at all — must also be excluded when filter is set.
	tmNoNS := tmPodByName(testNamespace, "tm-no-ns", testDeploymentName)

	r := &ververica.Resolver{
		Client: fake.NewClientBuilder().
			WithScheme(buildScheme()).
			WithObjects(tmWrong, tmNoNS).
			Build(),
	}

	_, err := r.Resolve(context.Background(), chaosRunByNameAndNS(testNamespace, testDeploymentName, testVVPNamespace))
	if err == nil {
		t.Fatal("expected error when no pods match the VVP namespace filter, got nil")
	}
}

// TestResolve_JMPodsDetectedAlongsideTMPods verifies that JobManager pods are
// correctly classified and returned alongside TaskManager pods.
func TestResolve_JMPodsDetectedAlongsideTMPods(t *testing.T) {
	tm0 := tmPodByID(testNamespace, "tm-0", testDeploymentID)
	jm0 := jmPodByID(testNamespace, "jm-0", testDeploymentID)

	r := &ververica.Resolver{
		Client: fake.NewClientBuilder().
			WithScheme(buildScheme()).
			WithObjects(tm0, jm0).
			Build(),
	}

	target, err := r.Resolve(context.Background(), chaosRunByID(testNamespace, testDeploymentID))
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
		t.Errorf("expected TM pod %q, got %q", "tm-0", target.TMPodNames[0])
	}
	if target.JMPodNames[0] != "jm-0" {
		t.Errorf("expected JM pod %q, got %q", "jm-0", target.JMPodNames[0])
	}
}

// TestResolve_NoTMPodsFound verifies that an error is returned when the
// matched pods contain no TaskManager pods.
func TestResolve_NoTMPodsFound(t *testing.T) {
	// Only a JM pod — no TM pods.
	jm0 := jmPodByID(testNamespace, "jm-0", testDeploymentID)

	r := &ververica.Resolver{
		Client: fake.NewClientBuilder().
			WithScheme(buildScheme()).
			WithObjects(jm0).
			Build(),
	}

	_, err := r.Resolve(context.Background(), chaosRunByID(testNamespace, testDeploymentID))
	if err == nil {
		t.Fatal("expected error when no TM pods found, got nil")
	}
}

// TestResolve_TerminatingPodsExcluded verifies that pods with a non-nil
// DeletionTimestamp are not counted as live TM pods.
func TestResolve_TerminatingPodsExcluded(t *testing.T) {
	terminating := terminatingTMPod(testNamespace, "tm-terminating", testDeploymentID)
	live := tmPodByID(testNamespace, "tm-live", testDeploymentID)

	r := &ververica.Resolver{
		Client: fake.NewClientBuilder().
			WithScheme(buildScheme()).
			WithObjects(terminating, live).
			Build(),
	}

	target, err := r.Resolve(context.Background(), chaosRunByID(testNamespace, testDeploymentID))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(target.TMPodNames) != 1 {
		t.Errorf("expected 1 TM pod (excluding terminating), got %d: %v", len(target.TMPodNames), target.TMPodNames)
	}
	if target.TMPodNames[0] != "tm-live" {
		t.Errorf("expected pod %q, got %q", "tm-live", target.TMPodNames[0])
	}
}

// TestResolve_SucceededAndFailedPodsExcluded verifies that pods in the
// Succeeded or Failed phase are excluded from the resolved pod sets.
func TestResolve_SucceededAndFailedPodsExcluded(t *testing.T) {
	tmSucceeded := podWithLabels(testNamespace, "tm-succeeded", map[string]string{
		"flink.apache.org/deployment-id": testDeploymentID,
		"component":                      "taskmanager",
	}, corev1.PodSucceeded)

	tmFailed := podWithLabels(testNamespace, "tm-failed", map[string]string{
		"flink.apache.org/deployment-id": testDeploymentID,
		"component":                      "taskmanager",
	}, corev1.PodFailed)

	tmRunning := tmPodByID(testNamespace, "tm-running", testDeploymentID)

	r := &ververica.Resolver{
		Client: fake.NewClientBuilder().
			WithScheme(buildScheme()).
			WithObjects(tmSucceeded, tmFailed, tmRunning).
			Build(),
	}

	target, err := r.Resolve(context.Background(), chaosRunByID(testNamespace, testDeploymentID))
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

// TestResolve_SessionModeLabel_SharedCluster verifies that when any matched
// pod carries a session cluster-mode label, SharedCluster is true.
func TestResolve_SessionModeLabel_SharedCluster(t *testing.T) {
	tm0 := sessionTMPodByID(testNamespace, "tm-0", testDeploymentID)

	r := &ververica.Resolver{
		Client: fake.NewClientBuilder().
			WithScheme(buildScheme()).
			WithObjects(tm0).
			Build(),
	}

	target, err := r.Resolve(context.Background(), chaosRunByID(testNamespace, testDeploymentID))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !target.SharedCluster {
		t.Error("expected SharedCluster=true when session-mode label is present")
	}
}

// TestResolve_DeploymentIDTakesPrecedenceOverName verifies that when both
// DeploymentID and DeploymentName are set, pods are matched by DeploymentID
// and the logical name is set to the DeploymentID.
func TestResolve_DeploymentIDTakesPrecedenceOverName(t *testing.T) {
	// Pod matched by ID — must be included.
	tmByID := tmPodByID(testNamespace, "tm-by-id", testDeploymentID)

	// Pod matched only by name — must NOT be included because DeploymentID is set.
	tmByName := tmPodByName(testNamespace, "tm-by-name", testDeploymentName)

	// Pod matched by vvp.io deployment-id label — must also be included.
	tmByIDVvp := tmPodByIDVvp(testNamespace, "tm-by-id-vvp", testDeploymentID)

	r := &ververica.Resolver{
		Client: fake.NewClientBuilder().
			WithScheme(buildScheme()).
			WithObjects(tmByID, tmByName, tmByIDVvp).
			Build(),
	}

	target, err := r.Resolve(context.Background(), chaosRunByIDAndName(testNamespace, testDeploymentID, testDeploymentName))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// LogicalName should be the DeploymentID when it is set.
	if target.LogicalName != testDeploymentID {
		t.Errorf("expected logicalName %q, got %q", testDeploymentID, target.LogicalName)
	}

	// Only the two ID-matched pods should be present.
	if len(target.TMPodNames) != 2 {
		t.Errorf("expected 2 TM pods (matched by ID only), got %d: %v", len(target.TMPodNames), target.TMPodNames)
	}

	for _, name := range target.TMPodNames {
		if name == "tm-by-name" {
			t.Errorf("pod %q matched by name must not be included when DeploymentID is set", name)
		}
	}
}
