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

package controller_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/controller"
	"github.com/flink-chaos-operator/internal/interfaces"
)

// newTestScheme builds a Scheme that includes all types used by the tests.
func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(v1alpha1.AddToScheme(s))
	utilruntime.Must(corev1.AddToScheme(s))
	return s
}

// newReconciler constructs a ChaosRunReconciler wired with the supplied stubs
// and backed by a fake client seeded with the given objects.
func newReconciler(scheme *runtime.Scheme, run *v1alpha1.ChaosRun, resolvers map[v1alpha1.TargetType]interfaces.TargetResolver, driver *stubDriver, observer *stubObserver) (*controller.ChaosRunReconciler, fake.ClientBuilder) {
	fc := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(run).
		WithStatusSubresource(&v1alpha1.ChaosRun{})
	fakeClient := fc.Build()

	r := &controller.ChaosRunReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Log:           logr.Discard(),
		Recorder:      record.NewFakeRecorder(10),
		SafetyChecker: &noopSafetyChecker{},
		Resolvers:     resolvers,
		ScenarioDrivers: map[v1alpha1.ScenarioType]interfaces.ScenarioDriver{
			v1alpha1.ScenarioTaskManagerPodKill: driver,
		},
		Observer: observer,
	}
	return r, *fc
}

// reqFor returns a ctrl.Request for the given ChaosRun.
func reqFor(run *v1alpha1.ChaosRun) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: run.Name, Namespace: run.Namespace}}
}

// TestReconcile_NewRun_ReachesCompleted verifies the happy-path state machine:
// a valid ChaosRun with all stubs returning success drives through
// Pending → Injecting → Observing → Completed with VerdictPassed.
func TestReconcile_NewRun_ReachesCompleted(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := newTestScheme()

	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "happy-path",
			Namespace: "default",
		},
		Spec: v1alpha1.ChaosRunSpec{
			Target: v1alpha1.TargetSpec{
				Type: v1alpha1.TargetFlinkDeployment,
				Name: "orders-app",
			},
			Scenario: v1alpha1.ScenarioSpec{
				Type: v1alpha1.ScenarioTaskManagerPodKill,
				Selection: v1alpha1.SelectionSpec{
					Mode:  v1alpha1.SelectionModeRandom,
					Count: 1,
				},
				Action: v1alpha1.ActionSpec{
					Type: v1alpha1.ActionDeletePod,
				},
			},
		},
	}

	driver := &stubDriver{
		result: &interfaces.InjectionResult{
			SelectedPods: []string{"tm-0"},
			InjectedPods: []string{"tm-0"},
		},
	}
	observer := &stubObserver{
		result: &interfaces.ObservationResult{
			ReplacementObserved:  true,
			AllReplacementsReady: true,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(run).
		WithStatusSubresource(&v1alpha1.ChaosRun{}).
		Build()

	r := &controller.ChaosRunReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Log:           logr.Discard(),
		Recorder:      record.NewFakeRecorder(10),
		SafetyChecker: &noopSafetyChecker{},
		Resolvers:     defaultResolvers(),
		ScenarioDrivers: map[v1alpha1.ScenarioType]interfaces.ScenarioDriver{
			v1alpha1.ScenarioTaskManagerPodKill: driver,
		},
		Observer: observer,
	}

	req := reqFor(run)

	// Call 1: empty phase → Injecting (sets StartedAt, requeues).
	_, err := r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	fetched := &v1alpha1.ChaosRun{}
	g.Expect(fakeClient.Get(ctx, req.NamespacedName, fetched)).To(Succeed())
	g.Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseInjecting))

	// Call 2: Injecting → Observing (resolves target, injects, transitions).
	_, err = r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(fakeClient.Get(ctx, req.NamespacedName, fetched)).To(Succeed())
	g.Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseObserving))

	// Call 3: Observing → Completed (stubObserver returns AllReplacementsReady=true).
	_, err = r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(fakeClient.Get(ctx, req.NamespacedName, fetched)).To(Succeed())
	g.Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseCompleted))
	g.Expect(fetched.Status.Verdict).To(Equal(v1alpha1.VerdictPassed))
}

// TestReconcile_AbortBeforeInjection verifies that a ChaosRun with
// spec.control.abort=true reaches PhaseAborted on the first Reconcile call
// without performing any injection.
func TestReconcile_AbortBeforeInjection(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := newTestScheme()

	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "abort-run",
			Namespace: "default",
		},
		Spec: v1alpha1.ChaosRunSpec{
			Target: v1alpha1.TargetSpec{
				Type: v1alpha1.TargetFlinkDeployment,
				Name: "orders-app",
			},
			Scenario: v1alpha1.ScenarioSpec{
				Type: v1alpha1.ScenarioTaskManagerPodKill,
				Selection: v1alpha1.SelectionSpec{
					Mode:  v1alpha1.SelectionModeRandom,
					Count: 1,
				},
				Action: v1alpha1.ActionSpec{
					Type: v1alpha1.ActionDeletePod,
				},
			},
			Control: v1alpha1.ControlSpec{
				Abort: true,
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(run).
		WithStatusSubresource(&v1alpha1.ChaosRun{}).
		Build()

	r := &controller.ChaosRunReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Log:           logr.Discard(),
		Recorder:      record.NewFakeRecorder(10),
		SafetyChecker: &noopSafetyChecker{},
		Resolvers:     defaultResolvers(),
		ScenarioDrivers: map[v1alpha1.ScenarioType]interfaces.ScenarioDriver{
			v1alpha1.ScenarioTaskManagerPodKill: &stubDriver{
				result: &interfaces.InjectionResult{
					SelectedPods: []string{"tm-0"},
					InjectedPods: []string{"tm-0"},
				},
			},
		},
		Observer: &stubObserver{
			result: &interfaces.ObservationResult{AllReplacementsReady: true},
		},
	}

	req := reqFor(run)

	// Single call: abort is handled before any phase transition.
	_, err := r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	fetched := &v1alpha1.ChaosRun{}
	g.Expect(fakeClient.Get(ctx, req.NamespacedName, fetched)).To(Succeed())
	g.Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseAborted))
	g.Expect(fetched.Status.InjectedPods).To(BeEmpty())

	// ConditionAbortRequested must be set to True.
	found := false
	for _, cond := range fetched.Status.Conditions {
		if cond.Type == v1alpha1.ConditionAbortRequested && cond.Status == metav1.ConditionTrue {
			found = true
			break
		}
	}
	g.Expect(found).To(BeTrue(), "expected ConditionAbortRequested=True on aborted run")
}

// TestReconcile_InvalidSpec_FailsValidation verifies that a ChaosRun with a
// FlinkDeployment target that has an empty Name is transitioned to PhaseFailed
// with ConditionRunFailed=True and VerdictFailed on the first Reconcile call.
func TestReconcile_InvalidSpec_FailsValidation(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := newTestScheme()

	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "invalid-run",
			Namespace: "default",
		},
		Spec: v1alpha1.ChaosRunSpec{
			Target: v1alpha1.TargetSpec{
				Type: v1alpha1.TargetFlinkDeployment,
				// Name intentionally omitted — this is the invalid case.
			},
			Scenario: v1alpha1.ScenarioSpec{
				Type: v1alpha1.ScenarioTaskManagerPodKill,
				Selection: v1alpha1.SelectionSpec{
					Mode:  v1alpha1.SelectionModeRandom,
					Count: 1,
				},
				Action: v1alpha1.ActionSpec{
					Type: v1alpha1.ActionDeletePod,
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(run).
		WithStatusSubresource(&v1alpha1.ChaosRun{}).
		Build()

	r := &controller.ChaosRunReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Log:           logr.Discard(),
		Recorder:      record.NewFakeRecorder(10),
		SafetyChecker: &noopSafetyChecker{},
		Resolvers:     defaultResolvers(),
		ScenarioDrivers: map[v1alpha1.ScenarioType]interfaces.ScenarioDriver{
			v1alpha1.ScenarioTaskManagerPodKill: &stubDriver{result: &interfaces.InjectionResult{}},
		},
		Observer: &stubObserver{
			result: &interfaces.ObservationResult{},
		},
	}

	req := reqFor(run)

	// Single call: validation runs in reconcilePending and fails immediately.
	_, err := r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	fetched := &v1alpha1.ChaosRun{}
	g.Expect(fakeClient.Get(ctx, req.NamespacedName, fetched)).To(Succeed())
	g.Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseFailed))
	g.Expect(fetched.Status.Verdict).To(Equal(v1alpha1.VerdictFailed))

	found := false
	for _, cond := range fetched.Status.Conditions {
		if cond.Type == v1alpha1.ConditionRunFailed && cond.Status == metav1.ConditionTrue {
			found = true
			break
		}
	}
	g.Expect(found).To(BeTrue(), "expected ConditionRunFailed=True on failed run")
}

// TestReconcile_UnsupportedTargetType verifies that a ChaosRun whose target
// type has no registered resolver transitions to PhaseFailed with
// ConditionRunFailed=True. The resolvers map here only covers
// FlinkDeployment, but the run specifies VervericaDeployment (with a valid
// DeploymentName so it passes validation).
func TestReconcile_UnsupportedTargetType(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := newTestScheme()

	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unsupported-target",
			Namespace: "default",
		},
		Spec: v1alpha1.ChaosRunSpec{
			Target: v1alpha1.TargetSpec{
				Type:           v1alpha1.TargetVervericaDeployment,
				DeploymentName: "orders-app-vvp", // passes validation
			},
			Scenario: v1alpha1.ScenarioSpec{
				Type: v1alpha1.ScenarioTaskManagerPodKill,
				Selection: v1alpha1.SelectionSpec{
					Mode:  v1alpha1.SelectionModeRandom,
					Count: 1,
				},
				Action: v1alpha1.ActionSpec{
					Type: v1alpha1.ActionDeletePod,
				},
			},
		},
	}

	// Resolver map intentionally omits VervericaDeployment.
	limitedResolvers := map[v1alpha1.TargetType]interfaces.TargetResolver{
		v1alpha1.TargetFlinkDeployment: &stubResolver{result: defaultResolvedTarget()},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(run).
		WithStatusSubresource(&v1alpha1.ChaosRun{}).
		Build()

	r := &controller.ChaosRunReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Log:           logr.Discard(),
		Recorder:      record.NewFakeRecorder(10),
		SafetyChecker: &noopSafetyChecker{},
		Resolvers:     limitedResolvers,
		ScenarioDrivers: map[v1alpha1.ScenarioType]interfaces.ScenarioDriver{
			v1alpha1.ScenarioTaskManagerPodKill: &stubDriver{result: &interfaces.InjectionResult{}},
		},
		Observer: &stubObserver{
			result: &interfaces.ObservationResult{},
		},
	}

	req := reqFor(run)

	// Call 1: empty phase → Injecting (validation passes, StartedAt set).
	_, err := r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	fetched := &v1alpha1.ChaosRun{}
	g.Expect(fakeClient.Get(ctx, req.NamespacedName, fetched)).To(Succeed())
	g.Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseInjecting),
		fmt.Sprintf("expected PhaseInjecting after first reconcile, got %s (msg: %s)",
			fetched.Status.Phase, fetched.Status.Message))

	// Call 2: Injecting → Failed (no resolver registered for VervericaDeployment).
	_, err = r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(fakeClient.Get(ctx, req.NamespacedName, fetched)).To(Succeed())
	g.Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseFailed))

	found := false
	for _, cond := range fetched.Status.Conditions {
		if cond.Type == v1alpha1.ConditionRunFailed && cond.Status == metav1.ConditionTrue {
			found = true
			break
		}
	}
	g.Expect(found).To(BeTrue(), "expected ConditionRunFailed=True")
	g.Expect(fetched.Status.Verdict).To(Equal(v1alpha1.VerdictFailed))
}

// ---------------------------------------------------------------------------
// NetworkChaos / CleaningUp phase tests
// ---------------------------------------------------------------------------

// networkChaosRun returns a minimal NetworkChaos ChaosRun for use in CleaningUp tests.
func networkChaosRun(name string) *v1alpha1.ChaosRun {
	return &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: v1alpha1.ChaosRunSpec{
			Target: v1alpha1.TargetSpec{
				Type: v1alpha1.TargetFlinkDeployment,
				Name: "orders-app",
			},
			Scenario: v1alpha1.ScenarioSpec{
				Type: v1alpha1.ScenarioNetworkChaos,
				Network: &v1alpha1.NetworkChaosSpec{
					Target:    v1alpha1.NetworkTargetTMtoJM,
					Direction: v1alpha1.NetworkDirectionBoth,
					Latency:   &metav1.Duration{Duration: 100 * time.Millisecond},
				},
			},
		},
	}
}

// TestReconcile_NetworkChaos_CleaningUp_HappyPath drives a NetworkChaos run
// through the full state machine: Pending → Injecting → Observing →
// CleaningUp → Completed with VerdictPassed.
func TestReconcile_NetworkChaos_CleaningUp_HappyPath(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := newTestScheme()

	run := networkChaosRun("nc-happy")

	cleanable := &stubCleanableDriver{
		injectResult: &interfaces.InjectionResult{
			SelectedPods: []string{"tm-0"},
			InjectedPods: []string{"tm-0"},
		},
		markCleanedUp: true,
	}
	observer := &stubObserver{
		result: &interfaces.ObservationResult{
			ReplacementObserved:  true,
			AllReplacementsReady: true,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(run).
		WithStatusSubresource(&v1alpha1.ChaosRun{}).
		Build()

	r := &controller.ChaosRunReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Log:           logr.Discard(),
		Recorder:      record.NewFakeRecorder(10),
		SafetyChecker: &noopSafetyChecker{},
		Resolvers:     defaultResolvers(),
		ScenarioDrivers: map[v1alpha1.ScenarioType]interfaces.ScenarioDriver{
			v1alpha1.ScenarioNetworkChaos: cleanable,
		},
		Observer: observer,
	}

	req := reqFor(run)

	// Call 1: Pending → Injecting.
	_, err := r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	fetched := &v1alpha1.ChaosRun{}
	g.Expect(fakeClient.Get(ctx, req.NamespacedName, fetched)).To(Succeed())
	g.Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseInjecting))

	// Call 2: Injecting → Observing.
	_, err = r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(fakeClient.Get(ctx, req.NamespacedName, fetched)).To(Succeed())
	g.Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseObserving))

	// Call 3: Observing → CleaningUp (AllReplacementsReady=true, needsCleanup=true).
	_, err = r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(fakeClient.Get(ctx, req.NamespacedName, fetched)).To(Succeed())
	g.Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseCleaningUp))

	// Call 4: CleaningUp → Completed.
	_, err = r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(fakeClient.Get(ctx, req.NamespacedName, fetched)).To(Succeed())
	g.Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseCompleted))
	g.Expect(fetched.Status.Verdict).To(Equal(v1alpha1.VerdictPassed))

	found := false
	for _, cond := range fetched.Status.Conditions {
		if cond.Type == v1alpha1.ConditionNetworkChaosCleanedUp && cond.Status == metav1.ConditionTrue {
			found = true
			break
		}
	}
	g.Expect(found).To(BeTrue(), "expected ConditionNetworkChaosCleanedUp=True")
}

// TestReconcile_NetworkChaos_CleaningUp_AbortPath verifies that a NetworkChaos
// run with spec.control.abort=true is routed through CleaningUp and finalised
// as PhaseAborted (not PhaseCompleted).
func TestReconcile_NetworkChaos_CleaningUp_AbortPath(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := newTestScheme()

	run := networkChaosRun("nc-abort")

	cleanable := &stubCleanableDriver{
		injectResult: &interfaces.InjectionResult{
			SelectedPods: []string{"tm-0"},
			InjectedPods: []string{"tm-0"},
		},
		markCleanedUp: true,
	}
	// Observer returns not-ready so we stay in Observing until abort fires.
	observer := &stubObserver{
		result: &interfaces.ObservationResult{
			AllReplacementsReady: false,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(run).
		WithStatusSubresource(&v1alpha1.ChaosRun{}).
		Build()

	r := &controller.ChaosRunReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Log:           logr.Discard(),
		Recorder:      record.NewFakeRecorder(10),
		SafetyChecker: &noopSafetyChecker{},
		Resolvers:     defaultResolvers(),
		ScenarioDrivers: map[v1alpha1.ScenarioType]interfaces.ScenarioDriver{
			v1alpha1.ScenarioNetworkChaos: cleanable,
		},
		Observer: observer,
	}

	req := reqFor(run)

	// Call 1: Pending → Injecting.
	_, err := r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	// Call 2: Injecting → Observing.
	_, err = r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	fetched := &v1alpha1.ChaosRun{}
	g.Expect(fakeClient.Get(ctx, req.NamespacedName, fetched)).To(Succeed())
	g.Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseObserving))

	// Set abort flag on the live object.
	fetchedCopy := fetched.DeepCopy()
	fetched.Spec.Control.Abort = true
	g.Expect(fakeClient.Update(ctx, fetched)).To(Succeed())

	_ = fetchedCopy // suppress unused warning

	// Call 3: abort handler fires (phase=Observing, isNetworkScenario=true,
	// hasInjectedResources=true) → transitions to CleaningUp.
	_, err = r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(fakeClient.Get(ctx, req.NamespacedName, fetched)).To(Succeed())
	g.Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseCleaningUp))

	// Call 4: CleaningUp → Aborted.
	_, err = r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(fakeClient.Get(ctx, req.NamespacedName, fetched)).To(Succeed())
	g.Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseAborted))
	g.Expect(fetched.Status.Verdict).To(Equal(v1alpha1.VerdictInconclusive))

	var foundAbort, foundCleaned bool
	for _, cond := range fetched.Status.Conditions {
		if cond.Type == v1alpha1.ConditionAbortRequested && cond.Status == metav1.ConditionTrue {
			foundAbort = true
		}
		if cond.Type == v1alpha1.ConditionNetworkChaosCleanedUp && cond.Status == metav1.ConditionTrue {
			foundCleaned = true
		}
	}
	g.Expect(foundAbort).To(BeTrue(), "expected ConditionAbortRequested=True")
	g.Expect(foundCleaned).To(BeTrue(), "expected ConditionNetworkChaosCleanedUp=True")
}

// TestReconcile_NetworkChaos_CleaningUp_TimeoutPreservesVerdict verifies that
// when a run times out during Observing, the VerdictFailed set at timeout is
// preserved through CleaningUp so the final phase is Completed(Failed), not
// Completed(Passed).
func TestReconcile_NetworkChaos_CleaningUp_TimeoutPreservesVerdict(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := newTestScheme()

	run := networkChaosRun("nc-timeout")
	// Enable observation and set a short timeout so the timeout check triggers
	// once we back-date StartedAt.
	run.Spec.Observe.Enabled = true
	run.Spec.Observe.Timeout = metav1.Duration{Duration: 10 * time.Minute}
	run.Spec.Observe.PollInterval = metav1.Duration{Duration: 5 * time.Second}

	cleanable := &stubCleanableDriver{
		injectResult: &interfaces.InjectionResult{
			SelectedPods: []string{"tm-0"},
			InjectedPods: []string{"tm-0"},
		},
		markCleanedUp: true,
	}
	observer := &stubObserver{
		result: &interfaces.ObservationResult{
			AllReplacementsReady: false,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(run).
		WithStatusSubresource(&v1alpha1.ChaosRun{}).
		Build()

	r := &controller.ChaosRunReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Log:           logr.Discard(),
		Recorder:      record.NewFakeRecorder(10),
		SafetyChecker: &noopSafetyChecker{},
		Resolvers:     defaultResolvers(),
		ScenarioDrivers: map[v1alpha1.ScenarioType]interfaces.ScenarioDriver{
			v1alpha1.ScenarioNetworkChaos: cleanable,
		},
		Observer: observer,
	}

	req := reqFor(run)

	// Call 1: Pending → Injecting.
	_, err := r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	// Call 2: Injecting → Observing.
	_, err = r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	fetched := &v1alpha1.ChaosRun{}
	g.Expect(fakeClient.Get(ctx, req.NamespacedName, fetched)).To(Succeed())
	g.Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseObserving))

	// Back-date both StartedAt and ObservingStartedAt so the observe timeout
	// (10min) is exceeded on the next reconcile, but the absolute cleanup
	// deadline (10min + 5min grace) is NOT yet exceeded so reconcileCleaningUp
	// can still finalise normally.
	fetched.Status.StartedAt = &metav1.Time{Time: time.Now().Add(-12 * time.Minute)}
	fetched.Status.ObservingStartedAt = &metav1.Time{Time: time.Now().Add(-12 * time.Minute)}
	g.Expect(fakeClient.Status().Update(ctx, fetched)).To(Succeed())

	// Call 3: Observing — timeout fires, needsCleanup=true → sets Verdict=Failed,
	// transitions to CleaningUp.
	_, err = r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(fakeClient.Get(ctx, req.NamespacedName, fetched)).To(Succeed())
	g.Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseCleaningUp))
	g.Expect(fetched.Status.Verdict).To(Equal(v1alpha1.VerdictFailed))

	// Call 4: CleaningUp → Completed, verdict must remain Failed.
	_, err = r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(fakeClient.Get(ctx, req.NamespacedName, fetched)).To(Succeed())
	g.Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseCompleted))
	g.Expect(fetched.Status.Verdict).To(Equal(v1alpha1.VerdictFailed),
		"verdict set at timeout must be preserved through CleaningUp")
}

// TestReconcile_CleaningUp_DeadlineExceeded verifies that a run in CleaningUp
// that has exceeded the absolute cleanup deadline is transitioned to PhaseFailed
// with ConditionNetworkChaosCleanedUp=False and Reason="CleanupTimeout".
func TestReconcile_CleaningUp_DeadlineExceeded(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := newTestScheme()

	// Pre-seed the run in PhaseCleaningUp with a StartedAt far in the past so
	// the absolute deadline (StartedAt + ObserveTimeout + 5min) is already exceeded.
	run := networkChaosRun("nc-deadline")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(run).
		WithStatusSubresource(&v1alpha1.ChaosRun{}).
		Build()

	// Seed status via the status subresource so the phase and startedAt are visible to the reconciler.
	startedAt := metav1.NewTime(time.Now().Add(-60 * time.Minute))
	seeded := &v1alpha1.ChaosRun{}
	g.Expect(fakeClient.Get(ctx, types.NamespacedName{Name: run.Name, Namespace: run.Namespace}, seeded)).To(Succeed())
	seeded.Status.Phase = v1alpha1.PhaseCleaningUp
	seeded.Status.StartedAt = &startedAt
	g.Expect(fakeClient.Status().Update(ctx, seeded)).To(Succeed())

	r := &controller.ChaosRunReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Log:           logr.Discard(),
		Recorder:      record.NewFakeRecorder(10),
		SafetyChecker: &noopSafetyChecker{},
		Resolvers:     defaultResolvers(),
		ScenarioDrivers: map[v1alpha1.ScenarioType]interfaces.ScenarioDriver{
			v1alpha1.ScenarioNetworkChaos: &stubCleanableDriver{markCleanedUp: true},
		},
		Observer: &stubObserver{result: &interfaces.ObservationResult{}},
	}

	req := reqFor(run)

	// Single reconcile: deadline already exceeded → FailRun.
	_, err := r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	fetched := &v1alpha1.ChaosRun{}
	g.Expect(fakeClient.Get(ctx, req.NamespacedName, fetched)).To(Succeed())
	g.Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseFailed))
	g.Expect(fetched.Status.Verdict).To(Equal(v1alpha1.VerdictFailed))

	found := false
	for _, cond := range fetched.Status.Conditions {
		if cond.Type == v1alpha1.ConditionNetworkChaosCleanedUp &&
			cond.Status == metav1.ConditionFalse &&
			cond.Reason == "CleanupTimeout" {
			found = true
			break
		}
	}
	g.Expect(found).To(BeTrue(), "expected ConditionNetworkChaosCleanedUp=False with Reason=CleanupTimeout")
}

// TestReconcile_CleaningUp_CleanupErrorRequeues verifies that a transient
// cleanup error does not move the run to a terminal state — it requeues with
// a non-zero RequeueAfter and leaves the phase as CleaningUp.
func TestReconcile_CleaningUp_CleanupErrorRequeues(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := newTestScheme()

	run := networkChaosRun("nc-cleanup-err")
	// Set an explicit PollInterval so that the RequeueAfter returned on cleanup
	// error is non-zero (SetDefaults only applies in memory during reconcilePending
	// and does not persist the spec).
	run.Spec.Observe.Enabled = true
	run.Spec.Observe.PollInterval = metav1.Duration{Duration: 5 * time.Second}
	run.Spec.Observe.Timeout = metav1.Duration{Duration: 10 * time.Minute}

	cleanable := &stubCleanableDriver{
		injectResult: &interfaces.InjectionResult{
			SelectedPods: []string{"tm-0"},
			InjectedPods: []string{"tm-0"},
		},
		cleanupErr: errors.New("transient error"),
	}
	observer := &stubObserver{
		result: &interfaces.ObservationResult{
			AllReplacementsReady: true,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(run).
		WithStatusSubresource(&v1alpha1.ChaosRun{}).
		Build()

	r := &controller.ChaosRunReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Log:           logr.Discard(),
		Recorder:      record.NewFakeRecorder(10),
		SafetyChecker: &noopSafetyChecker{},
		Resolvers:     defaultResolvers(),
		ScenarioDrivers: map[v1alpha1.ScenarioType]interfaces.ScenarioDriver{
			v1alpha1.ScenarioNetworkChaos: cleanable,
		},
		Observer: observer,
	}

	req := reqFor(run)

	// Call 1: Pending → Injecting.
	_, err := r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	// Call 2: Injecting → Observing.
	_, err = r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	// Call 3: Observing → CleaningUp (AllReplacementsReady=true).
	_, err = r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())

	fetched := &v1alpha1.ChaosRun{}
	g.Expect(fakeClient.Get(ctx, req.NamespacedName, fetched)).To(Succeed())
	g.Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseCleaningUp))

	// Call 4: CleaningUp — cleanup returns a transient error → requeue.
	result, err := r.Reconcile(ctx, req)
	g.Expect(err).NotTo(HaveOccurred(), "transient cleanup errors must not be returned as reconciler errors")
	g.Expect(result.RequeueAfter).NotTo(BeZero(), "expected non-zero RequeueAfter on cleanup error")

	g.Expect(fakeClient.Get(ctx, req.NamespacedName, fetched)).To(Succeed())
	g.Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseCleaningUp), "phase must remain CleaningUp on transient error")
	g.Expect(fetched.Status.Verdict).To(BeEmpty(), "verdict must not be set on a transient cleanup error")
}
