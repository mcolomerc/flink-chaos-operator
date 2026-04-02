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

package podselector_test

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/resolver/podselector"
)

func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = v1alpha1.AddToScheme(s)
	return s
}

// newPod is a helper that builds a minimal pod for use in tests.
func newPod(name, namespace string, phase corev1.PodPhase, lbls map[string]string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    lbls,
		},
		Status: corev1.PodStatus{Phase: phase},
	}
}

// TestResolve_WrongTargetType verifies that Resolve returns an error when the
// run's target type is not PodSelector.
func TestResolve_WrongTargetType(t *testing.T) {
	g := NewWithT(t)

	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	r := &podselector.Resolver{Client: fakeClient}

	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{Name: "run1", Namespace: "default"},
		Spec: v1alpha1.ChaosRunSpec{
			Target: v1alpha1.TargetSpec{
				Type: v1alpha1.TargetFlinkDeployment,
				Name: "my-app",
			},
		},
	}

	_, err := r.Resolve(context.Background(), run)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("expected target type"))
}

// TestResolve_NilSelector verifies that Resolve returns an error when
// spec.target.selector is nil.
func TestResolve_NilSelector(t *testing.T) {
	g := NewWithT(t)

	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	r := &podselector.Resolver{Client: fakeClient}

	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{Name: "run1", Namespace: "default"},
		Spec: v1alpha1.ChaosRunSpec{
			Target: v1alpha1.TargetSpec{
				Type:     v1alpha1.TargetPodSelector,
				Selector: nil,
			},
		},
	}

	_, err := r.Resolve(context.Background(), run)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("spec.target.selector"))
}

// TestResolve_MatchingPodsClassifiedCorrectly verifies that matching pods are
// partitioned into TM and JM sets according to component labels, and that pods
// without a recognised component label are treated as TaskManagers.
func TestResolve_MatchingPodsClassifiedCorrectly(t *testing.T) {
	g := NewWithT(t)

	ns := "test-ns"
	pods := []corev1.Pod{
		newPod("tm-1", ns, corev1.PodRunning, map[string]string{
			"app":       "flink",
			"component": "taskmanager",
		}),
		newPod("tm-2", ns, corev1.PodRunning, map[string]string{
			"app":                      "flink",
			"flink.apache.org/component": "taskmanager",
		}),
		newPod("jm-1", ns, corev1.PodRunning, map[string]string{
			"app":       "flink",
			"component": "jobmanager",
		}),
		// No component label — should be treated as TM.
		newPod("unknown-1", ns, corev1.PodRunning, map[string]string{
			"app": "flink",
		}),
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(testScheme()).
		WithObjects(podsToObjects(pods)...).
		Build()

	r := &podselector.Resolver{Client: fakeClient}

	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{Name: "run1", Namespace: ns},
		Spec: v1alpha1.ChaosRunSpec{
			Target: v1alpha1.TargetSpec{
				Type: v1alpha1.TargetPodSelector,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "flink"},
				},
			},
		},
	}

	target, err := r.Resolve(context.Background(), run)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(target.Platform).To(Equal("generic"))
	g.Expect(target.LogicalName).To(Equal("pod-selector"))
	g.Expect(target.Namespace).To(Equal(ns))
	g.Expect(target.SharedCluster).To(BeFalse())
	g.Expect(target.TMPodNames).To(ConsistOf("tm-1", "tm-2", "unknown-1"))
	g.Expect(target.JMPodNames).To(ConsistOf("jm-1"))
}

// TestResolve_NoPodsFound verifies that Resolve returns an error when no pods
// match the selector in the given namespace.
func TestResolve_NoPodsFound(t *testing.T) {
	g := NewWithT(t)

	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	r := &podselector.Resolver{Client: fakeClient}

	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{Name: "run1", Namespace: "default"},
		Spec: v1alpha1.ChaosRunSpec{
			Target: v1alpha1.TargetSpec{
				Type: v1alpha1.TargetPodSelector,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "nonexistent"},
				},
			},
		},
	}

	_, err := r.Resolve(context.Background(), run)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("no pods matched selector"))
	g.Expect(err.Error()).To(ContainSubstring("default"))
}

// TestResolve_NonRunningPodsExcluded verifies that pods not in Running or
// Pending phase are excluded from the resolved target.
func TestResolve_NonRunningPodsExcluded(t *testing.T) {
	g := NewWithT(t)

	ns := "test-ns"
	pods := []corev1.Pod{
		newPod("running-tm", ns, corev1.PodRunning, map[string]string{"app": "flink"}),
		newPod("pending-tm", ns, corev1.PodPending, map[string]string{"app": "flink"}),
		newPod("failed-tm", ns, corev1.PodFailed, map[string]string{"app": "flink"}),
		newPod("succeeded-tm", ns, corev1.PodSucceeded, map[string]string{"app": "flink"}),
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(testScheme()).
		WithObjects(podsToObjects(pods)...).
		Build()

	r := &podselector.Resolver{Client: fakeClient}

	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{Name: "run1", Namespace: ns},
		Spec: v1alpha1.ChaosRunSpec{
			Target: v1alpha1.TargetSpec{
				Type: v1alpha1.TargetPodSelector,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "flink"},
				},
			},
		},
	}

	target, err := r.Resolve(context.Background(), run)
	g.Expect(err).NotTo(HaveOccurred())
	// Only Running and Pending pods should be included.
	g.Expect(target.TMPodNames).To(ConsistOf("running-tm", "pending-tm"))
}

// TestResolve_TerminatingPodsExcluded verifies that pods with a non-nil
// DeletionTimestamp are excluded even if their phase is Running.
func TestResolve_TerminatingPodsExcluded(t *testing.T) {
	g := NewWithT(t)

	ns := "test-ns"
	now := metav1.Now()
	terminatingPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "terminating-tm",
			Namespace:         ns,
			Labels:            map[string]string{"app": "flink"},
			DeletionTimestamp: &now,
			Finalizers:        []string{"test-finalizer"}, // required for fake client to accept DeletionTimestamp
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	runningPod := newPod("running-tm", ns, corev1.PodRunning, map[string]string{"app": "flink"})

	fakeClient := fake.NewClientBuilder().
		WithScheme(testScheme()).
		WithObjects(&terminatingPod, &runningPod).
		Build()

	r := &podselector.Resolver{Client: fakeClient}

	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{Name: "run1", Namespace: ns},
		Spec: v1alpha1.ChaosRunSpec{
			Target: v1alpha1.TargetSpec{
				Type: v1alpha1.TargetPodSelector,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "flink"},
				},
			},
		},
	}

	target, err := r.Resolve(context.Background(), run)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(target.TMPodNames).To(ConsistOf("running-tm"))
}

// podsToObjects converts a slice of pods to a slice of client.Object for
// use with the fake client builder.
func podsToObjects(pods []corev1.Pod) []client.Object {
	objs := make([]client.Object, len(pods))
	for i := range pods {
		objs[i] = &pods[i]
	}
	return objs
}
