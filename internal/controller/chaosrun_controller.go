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

package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/interfaces"
	flinkrestpkg "github.com/flink-chaos-operator/internal/observer/flinkrest"
)

// chaosRunFinalizer is the finalizer added to ChaosRuns that own external
// resources (NetworkPolicies, ephemeral containers). It prevents garbage
// collection until cleanup is complete.
const chaosRunFinalizer = "chaos.flink.io/cleanup"

// SafetyCheckerIface is the subset of safety.Checker used by the reconciler.
// Defined here to avoid a direct import of the safety package in controller
// tests, which would otherwise create a circular dependency.
type SafetyCheckerIface interface {
	Check(ctx context.Context, run *v1alpha1.ChaosRun, target *interfaces.ResolvedTarget) error
}

// ChaosRunReconciler reconciles ChaosRun objects.
//
// +kubebuilder:rbac:groups=chaos.flink.io,resources=chaosruns,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=chaos.flink.io,resources=chaosruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=chaos.flink.io,resources=chaosruns/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;delete;patch
// +kubebuilder:rbac:groups="",resources=pods/ephemeralcontainers,verbs=get;patch;update
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create
type ChaosRunReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	Log                logr.Logger
	Recorder           record.EventRecorder
	SafetyChecker      SafetyCheckerIface
	Resolvers          map[v1alpha1.TargetType]interfaces.TargetResolver
	ScenarioDrivers    map[v1alpha1.ScenarioType]interfaces.ScenarioDriver
	Observer           interfaces.Observer
	// FlinkClientFactory creates a Flink REST client for the given base URL.
	// Defaults to flinkrestpkg.NewHTTPClient when nil.
	// Inject a stub in tests to avoid real HTTP calls.
	FlinkClientFactory func(baseURL string) flinkrestpkg.Client
}

// SetupWithManager registers the reconciler with the provided manager and
// declares that it owns ChaosRun objects.
func (r *ChaosRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ChaosRun{}).
		Complete(r)
}

// Reconcile implements the main reconciliation loop. It is called whenever a
// ChaosRun is created, updated, or re-queued, and drives it through the
// Pending → Injecting → Observing → Completed/Aborted/Failed state machine.
func (r *ChaosRunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("chaosRun", req.NamespacedName)
	log.Info("Reconcile invoked")

	// 1. Fetch the ChaosRun. If it no longer exists it has already been
	//    deleted — nothing to do.
	run := &v1alpha1.ChaosRun{}
	if err := r.Get(ctx, req.NamespacedName, run); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("ChaosRun not found, skipping")
			return ctrl.Result{}, nil
		}
		log.Error(err, "failed to get ChaosRun")
		return ctrl.Result{}, err
	}

	// 2. Apply spec defaults so every phase sees consistent values regardless
	//    of whether the object was just created or re-fetched from the API
	//    server (defaults are not persisted to the spec, only held in memory).
	v1alpha1.SetDefaults(run)

	// 3. Skip terminal phases — completed runs are immutable from the
	//    controller's perspective.
	if IsTerminal(run.Status.Phase) {
		return ctrl.Result{}, nil
	}

	// 3. Handle deletion: if the run has been deleted and our finalizer is
	//    present, run cleanup first to remove any NetworkPolicies or ephemeral
	//    resources, then remove the finalizer.
	if !run.DeletionTimestamp.IsZero() && controllerutil.ContainsFinalizer(run, chaosRunFinalizer) {
		log.Info("ChaosRun deleted, running cleanup before removal")
		if r.ScenarioDrivers != nil {
			if driver, ok := r.ScenarioDrivers[run.Spec.Scenario.Type]; ok {
				if cleanable, ok := driver.(interfaces.CleanableScenarioDriver); ok {
					if err := cleanable.Cleanup(ctx, run); err != nil {
						log.Error(err, "cleanup on delete failed, will retry")
						return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
					}
				}
			}
		}
		runBase := run.DeepCopy()
		controllerutil.RemoveFinalizer(run, chaosRunFinalizer)
		if err := r.Patch(ctx, run, client.MergeFrom(runBase)); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// 4. Handle abort signal: if spec.control.abort is set and the run has
	//    not yet reached a terminal phase, abort it. Network scenarios that
	//    have already injected resources must first clean up — they are routed
	//    through PhaseCleaningUp before being finalised as Aborted.
	//    If the run is already in CleaningUp (because a previous reconcile
	//    already routed it here), fall through to the phase switch so that
	//    reconcileCleaningUp can finish the work.
	if run.Spec.Control.Abort && run.Status.Phase != v1alpha1.PhaseCleaningUp {
		isNetworkScenario := run.Spec.Scenario.Type == v1alpha1.ScenarioNetworkPartition ||
			run.Spec.Scenario.Type == v1alpha1.ScenarioNetworkChaos ||
			run.Spec.Scenario.Type == v1alpha1.ScenarioResourceExhaustion
		hasInjectedResources := isNetworkScenario &&
			run.Status.Phase != "" &&
			run.Status.Phase != v1alpha1.PhasePending

		runCopy := run.DeepCopy()
		SetCondition(run, v1alpha1.ConditionAbortRequested,
			metav1.ConditionTrue, "AbortRequested", "User set spec.control.abort=true")
		AbortRequestsTotal.Inc()
		r.Recorder.Event(run, corev1.EventTypeNormal, "AbortRequested",
			"ChaosRun abort requested by user")

		if hasInjectedResources {
			// Route through CleaningUp so the driver can remove NetworkPolicies
			// and ephemeral tc containers before the run is finalised.
			log.Info("aborting network run via CleaningUp", "reason", "spec.control.abort=true")
			TransitionPhase(run, v1alpha1.PhaseCleaningUp,
				"abort requested, cleaning up network chaos resources")
			if err := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}

		log.Info("aborting run", "reason", "spec.control.abort=true")
		AbortRun(run, "abort requested via spec.control.abort")
		if err := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); err != nil {
			return ctrl.Result{}, err
		}
		RunsTotal.WithLabelValues(
			string(run.Spec.Scenario.Type),
			string(v1alpha1.PhaseAborted),
			"",
		).Inc()
		return ctrl.Result{}, nil
	}

	// 5. Phase-based state machine.
	switch run.Status.Phase {
	case "", v1alpha1.PhasePending:
		return r.reconcilePending(ctx, log, run)
	case v1alpha1.PhaseInjecting:
		return r.reconcileInjecting(ctx, log, run)
	case v1alpha1.PhaseObserving:
		return r.reconcileObserving(ctx, log, run)
	case v1alpha1.PhaseCleaningUp:
		return r.reconcileCleaningUp(ctx, log, run)
	default:
		// Unknown phase — do nothing.
		log.Info("unrecognised phase, skipping", "phase", run.Status.Phase)
		return ctrl.Result{}, nil
	}
}

// reconcilePending handles the initial "" or Pending phase.
// It validates the spec, records startedAt, then transitions to Injecting
// and immediately requeues. Defaults are already applied by Reconcile.
func (r *ChaosRunReconciler) reconcilePending(ctx context.Context, log logr.Logger, run *v1alpha1.ChaosRun) (ctrl.Result, error) {
	log.Info("reconciling pending run")

	runCopy := run.DeepCopy()

	if err := v1alpha1.Validate(run); err != nil {
		FailRun(run, "spec validation failed: "+err.Error())
		SetCondition(run, v1alpha1.ConditionRunFailed,
			metav1.ConditionTrue, "ValidationFailed", err.Error())
		r.Recorder.Event(run, corev1.EventTypeWarning, "ValidationFailed",
			"ChaosRun spec validation failed: "+err.Error())
		log.Error(err, "spec validation failed")
		if patchErr := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); patchErr != nil {
			return ctrl.Result{}, patchErr
		}
		return ctrl.Result{}, nil
	}

	now := metav1.Now()
	run.Status.StartedAt = &now

	// Record startedAt before the checkpoint wait so the timeout deadline is
	// anchored to when the run first entered Pending, not to a later reconcile.
	if err := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); err != nil {
		return ctrl.Result{}, err
	}
	runCopy = run.DeepCopy()

	// If checkpoint stability is required, poll before transitioning to Injecting.
	if run.Spec.Observe.FlinkRest.RequireStableCheckpointBeforeInject {
		err := r.waitForStableCheckpoint(ctx, log, run, runCopy)
		if errors.Is(err, errCheckpointNotStable) {
			return ctrl.Result{RequeueAfter: run.Spec.Observe.PollInterval.Duration}, nil
		}
		if err != nil {
			return ctrl.Result{}, err
		}
		// If waitForStableCheckpoint transitioned to Failed, do not continue.
		if run.Status.Phase == v1alpha1.PhaseFailed {
			return ctrl.Result{}, nil
		}
		runCopy = run.DeepCopy()
	}

	TransitionPhase(run, v1alpha1.PhaseInjecting,
		"target resolution and injection starting")

	log.Info("transitioning to Injecting")
	if err := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); err != nil {
		return ctrl.Result{}, err
	}
	// Requeue immediately to continue into the Injecting phase.
	return ctrl.Result{Requeue: true}, nil
}

// errCheckpointNotStable is a sentinel returned by waitForStableCheckpoint
// when the checkpoint is not yet stable and the caller should requeue.
var errCheckpointNotStable = errors.New("checkpoint not yet stable")

// waitForStableCheckpoint polls the Flink REST API once and checks whether the
// most recently completed checkpoint falls within the configured stable window.
// It returns errCheckpointNotStable (a sentinel) when the checkpoint is not
// yet stable and the reconcile loop should requeue. It transitions the run to
// PhaseFailed when the wait timeout is exceeded, patching status before
// returning nil. Non-nil non-sentinel errors are transient and should be
// returned to the controller-runtime requeue mechanism.
func (r *ChaosRunReconciler) waitForStableCheckpoint(
	ctx context.Context,
	log logr.Logger,
	run *v1alpha1.ChaosRun,
	runCopy *v1alpha1.ChaosRun,
) error {
	obs := run.Spec.Observe.FlinkRest
	factory := r.FlinkClientFactory
	if factory == nil {
		factory = flinkrestpkg.NewHTTPClient
	}
	restClient := factory(obs.Endpoint)

	stable, err := flinkrestpkg.IsCheckpointStable(ctx, restClient, obs.CheckpointStableWindowSeconds)
	if err != nil {
		log.Error(err, "checkpoint stability check failed, will retry")
		return err
	}

	if stable {
		log.Info("checkpoint stable, proceeding to injection")
		return nil
	}

	// Guard nil dereferences before computing the wait deadline.
	if run.Status.StartedAt == nil {
		return errCheckpointNotStable
	}
	if obs.CheckpointWaitTimeout == nil {
		return fmt.Errorf("checkpointWaitTimeout is nil; defaults may not have been applied")
	}

	// Not yet stable — check whether the wait timeout has been exceeded.
	waitDeadline := run.Status.StartedAt.Time.Add(obs.CheckpointWaitTimeout.Duration)
	if time.Now().After(waitDeadline) {
		log.Info("checkpoint wait timeout exceeded, failing run")
		prePatch := run.DeepCopy()
		FailRun(run, "timed out waiting for a stable Flink checkpoint before injection")
		SetCondition(run, v1alpha1.ConditionRunFailed,
			metav1.ConditionTrue, "CheckpointWaitTimeout",
			"No stable checkpoint was observed within the configured timeout")
		r.Recorder.Event(run, corev1.EventTypeWarning, "CheckpointWaitTimeout",
			"Timed out waiting for a stable Flink checkpoint")
		if patchErr := r.Status().Patch(ctx, run, client.MergeFrom(prePatch)); patchErr != nil {
			return patchErr
		}
		return nil
	}

	// Not stable and timeout not yet reached — signal requeue.
	log.Info("checkpoint not yet stable, requeueing",
		"pollInterval", run.Spec.Observe.PollInterval.Duration)
	if patchErr := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); patchErr != nil {
		return patchErr
	}
	return errCheckpointNotStable
}

// reconcileInjecting handles the Injecting phase.
// It resolves the target, runs safety checks, and either performs or skips
// injection (dry-run), then transitions to Observing.
func (r *ChaosRunReconciler) reconcileInjecting(ctx context.Context, log logr.Logger, run *v1alpha1.ChaosRun) (ctrl.Result, error) {
	log.Info("reconciling injecting run")

	// Idempotency guard: if we already have injected pods recorded, skip the
	// injection work and proceed directly to the Observing transition.
	if len(run.Status.InjectedPods) > 0 {
		log.Info("injection already recorded, transitioning to Observing (idempotency)")
		return r.transitionToObserving(ctx, run)
	}

	runCopy := run.DeepCopy()

	// Resolve target.
	target, err := r.resolveTarget(ctx, run)
	if err != nil {
		FailRun(run, "target resolution failed: "+err.Error())
		SetCondition(run, v1alpha1.ConditionTargetResolved,
			metav1.ConditionFalse, "ResolutionFailed", err.Error())
		SetCondition(run, v1alpha1.ConditionRunFailed,
			metav1.ConditionTrue, "TargetResolutionFailed", err.Error())
		r.Recorder.Event(run, corev1.EventTypeWarning, "TargetResolutionFailed",
			"Failed to resolve target: "+err.Error())
		log.Error(err, "target resolution failed")
		if patchErr := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); patchErr != nil {
			return ctrl.Result{}, patchErr
		}
		return ctrl.Result{}, nil
	}

	SetCondition(run, v1alpha1.ConditionTargetResolved,
		metav1.ConditionTrue, "TargetResolved", "Target successfully resolved")
	r.Recorder.Event(run, corev1.EventTypeNormal, "TargetResolved",
		"Target resolved: "+target.LogicalName)
	log.Info("target resolved", "platform", target.Platform, "name", target.LogicalName,
		"tmPods", len(target.TMPodNames))

	// Safety checks.
	if err := r.runSafetyChecks(ctx, run, target); err != nil {
		FailRun(run, "safety checks failed: "+err.Error())
		SetCondition(run, v1alpha1.ConditionSafetyChecksPassed,
			metav1.ConditionFalse, "SafetyCheckFailed", err.Error())
		SetCondition(run, v1alpha1.ConditionRunFailed,
			metav1.ConditionTrue, "SafetyCheckFailed", err.Error())
		r.Recorder.Event(run, corev1.EventTypeWarning, "SafetyRejection",
			"Safety checks rejected injection: "+err.Error())
		log.Error(err, "safety checks failed")
		if patchErr := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); patchErr != nil {
			return ctrl.Result{}, patchErr
		}
		return ctrl.Result{}, nil
	}

	SetCondition(run, v1alpha1.ConditionSafetyChecksPassed,
		metav1.ConditionTrue, "SafetyChecksPassed", "All pre-injection safety checks passed")

	// Record target summary in status.
	run.Status.TargetSummary = &v1alpha1.TargetSummary{
		Type: target.Platform,
		Name: target.LogicalName,
	}

	// Dry-run path: record the projected selection but do not delete any pod.
	if run.Spec.Safety.DryRun {
		run.Status.SelectedPods = target.TMPodNames
		run.Status.DryRunPreview = buildDryRunPreview(run, target)
		FinalizeRun(run, v1alpha1.VerdictInconclusive,
			"dry-run: no pods deleted")
		log.Info("dry-run: finalising without injection")
		if patchErr := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); patchErr != nil {
			return ctrl.Result{}, patchErr
		}
		return ctrl.Result{}, nil
	}

	// Inject chaos.
	result, err := r.injectChaos(ctx, run, target)
	if err != nil {
		FailRun(run, "injection failed: "+err.Error())
		SetCondition(run, v1alpha1.ConditionInjectionStarted,
			metav1.ConditionFalse, "InjectionFailed", err.Error())
		SetCondition(run, v1alpha1.ConditionRunFailed,
			metav1.ConditionTrue, "InjectionFailed", err.Error())
		r.Recorder.Event(run, corev1.EventTypeWarning, "RunFailed",
			"Injection failed: "+err.Error())
		log.Error(err, "injection failed")
		if patchErr := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); patchErr != nil {
			return ctrl.Result{}, patchErr
		}
		return ctrl.Result{}, nil
	}

	SetCondition(run, v1alpha1.ConditionInjectionStarted,
		metav1.ConditionTrue, "InjectionStarted", "Chaos injection started")
	SetCondition(run, v1alpha1.ConditionInjectionCompleted,
		metav1.ConditionTrue, "InjectionCompleted", "Chaos injection completed")
	r.Recorder.Event(run, corev1.EventTypeNormal, "InjectionStarted",
		"Chaos injection started for target "+target.LogicalName)

	run.Status.SelectedPods = result.SelectedPods
	run.Status.InjectedPods = result.InjectedPods
	InjectionsTotal.WithLabelValues(string(run.Spec.Scenario.Type)).Inc()

	// Capture the true pre-injection TM count here, while target is fresh.
	// The observer computes tmCountBefore as live+injected, which becomes wrong
	// after recovery (the replacement pod is live again, inflating the count).
	if run.Status.Observation == nil {
		run.Status.Observation = &v1alpha1.ObservationStatus{}
	}
	run.Status.Observation.TaskManagerCountBefore = len(target.TMPodNames)

	log.Info("injection complete", "selected", result.SelectedPods,
		"injected", result.InjectedPods)

	// Persist InjectedPods/SelectedPods before transitioning to Observing.
	// transitionToObserving takes its own runCopy baseline, so any fields set
	// on run after the original runCopy would not appear in that patch.
	if patchErr := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); patchErr != nil {
		return ctrl.Result{}, patchErr
	}

	// Add a finalizer for scenarios that create external resources (NetworkPolicies,
	// ephemeral containers). This ensures cleanup runs before the object is deleted.
	isNetworkScenario := run.Spec.Scenario.Type == v1alpha1.ScenarioNetworkPartition ||
		run.Spec.Scenario.Type == v1alpha1.ScenarioNetworkChaos ||
		run.Spec.Scenario.Type == v1alpha1.ScenarioResourceExhaustion
	if isNetworkScenario && !controllerutil.ContainsFinalizer(run, chaosRunFinalizer) {
		runBase := run.DeepCopy()
		controllerutil.AddFinalizer(run, chaosRunFinalizer)
		if err := r.Patch(ctx, run, client.MergeFrom(runBase)); err != nil {
			return ctrl.Result{}, err
		}
	}

	return r.transitionToObserving(ctx, run)
}

// transitionToObserving records the Observing phase transition, stamps
// ObservingStartedAt so the observation timeout is anchored to injection
// completion rather than run start, and requeues after the poll interval.
func (r *ChaosRunReconciler) transitionToObserving(ctx context.Context, run *v1alpha1.ChaosRun) (ctrl.Result, error) {
	// Capture poll interval before the status patch — Status().Patch refreshes
	// run from the server response, overwriting in-memory defaults applied by
	// SetDefaults (PollInterval is never persisted to the spec). If we read it
	// after the patch we would get 0s, which ctrl.Result{RequeueAfter: 0} does
	// not honour (controller-runtime only schedules when RequeueAfter > 0).
	pollInterval := run.Spec.Observe.PollInterval.Duration

	runCopy := run.DeepCopy()
	now := metav1.Now()
	run.Status.ObservingStartedAt = &now
	TransitionPhase(run, v1alpha1.PhaseObserving,
		"injection complete, observing recovery")
	if err := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: pollInterval}, nil
}

// reconcileObserving handles the Observing phase.
// It polls for recovery signals and either finalises the run or requeues for
// the next poll cycle.
func (r *ChaosRunReconciler) reconcileObserving(ctx context.Context, log logr.Logger, run *v1alpha1.ChaosRun) (ctrl.Result, error) {
	log.Info("reconciling observing run")

	// Capture poll interval before any status patch — Status().Patch refreshes
	// run from the server, overwriting in-memory defaults. PollInterval is set
	// by SetDefaults but never persisted, so it reads back as 0s after a patch.
	pollInterval := run.Spec.Observe.PollInterval.Duration

	runCopy := run.DeepCopy()

	needsCleanup := run.Spec.Scenario.Type == v1alpha1.ScenarioNetworkPartition ||
		run.Spec.Scenario.Type == v1alpha1.ScenarioNetworkChaos ||
		run.Spec.Scenario.Type == v1alpha1.ScenarioResourceExhaustion

	// If observation is disabled skip straight to a Passed verdict.
	if !run.Spec.Observe.Enabled {
		if needsCleanup {
			TransitionPhase(run, v1alpha1.PhaseCleaningUp, "observation disabled, cleaning up network chaos resources")
			if err := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		FinalizeRun(run, v1alpha1.VerdictPassed, "observation disabled")
		r.finishRun(run, log)
		if err := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Check observation timeout. Prefer ObservingStartedAt so that time spent
	// waiting for a stable checkpoint before injection does not reduce the
	// observation window. Fall back to StartedAt when ObservingStartedAt is
	// not yet set (e.g. a run created before this field was added).
	observeAnchor := run.Status.StartedAt
	if run.Status.ObservingStartedAt != nil {
		observeAnchor = run.Status.ObservingStartedAt
	}
	if observeAnchor != nil {
		elapsed := time.Since(observeAnchor.Time)
		if elapsed > run.Spec.Observe.Timeout.Duration {
			log.Info("observation window elapsed", "elapsed", elapsed,
				"timeout", run.Spec.Observe.Timeout.Duration)
			thresholdSecs := int64(run.Spec.Observe.Timeout.Duration.Seconds())

			// For TaskManagerPodKill: the observation window IS the experiment
			// duration. Finalize based on whether recovery was observed during
			// the window rather than whether it is ready right now.
			var verdict v1alpha1.RunVerdict
			var msg string
			if !needsCleanup {
				if run.Status.Observation != nil && run.Status.Observation.RecoveryObservedAt != nil {
					recoverySecs := int64(0)
					if run.Status.Observation.RecoveryTimeSeconds != nil {
						recoverySecs = *run.Status.Observation.RecoveryTimeSeconds
					}
					verdict = v1alpha1.VerdictPassed
					msg = fmt.Sprintf("killed pod recovered in %ds (observation window: %ds)", recoverySecs, thresholdSecs)
				} else {
					verdict = v1alpha1.VerdictFailed
					msg = fmt.Sprintf("killed pod was not recovered within the %ds observation window", thresholdSecs)
				}
				FinalizeRun(run, verdict, msg)
				r.finishRun(run, log)
				if err := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, nil
			}

			// Build verdict and message based on scenario type.
			// ResourceExhaustion always passes — stress ran for the full window.
			// Network scenarios pass/fail based on whether Flink recovered.
			if run.Spec.Scenario.Type == v1alpha1.ScenarioResourceExhaustion {
				verdict = v1alpha1.VerdictPassed
				msg = fmt.Sprintf("resource exhaustion ran for %ds", thresholdSecs)
			} else if run.Status.Observation != nil && run.Status.Observation.RecoveryObservedAt != nil {
				verdict = v1alpha1.VerdictPassed
				msg = fmt.Sprintf("recovery confirmed within the %ds observation window", thresholdSecs)
			} else if run.Status.ReplacementObserved {
				verdict = v1alpha1.VerdictInconclusive
				msg = fmt.Sprintf("replacement pod seen but full recovery not confirmed within the %ds observation window", thresholdSecs)
			} else {
				verdict = v1alpha1.VerdictFailed
				msg = fmt.Sprintf("no Flink recovery detected within the %ds observation window", thresholdSecs)
			}
			run.Status.Verdict = verdict
			TransitionPhase(run, v1alpha1.PhaseCleaningUp, msg+", cleaning up")
			if err := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// For NetworkPartition the disruption is held by NetworkPolicies for
	// scenario.network.duration. Transition to CleaningUp when it elapses so
	// policies are removed on schedule regardless of recovery observation.
	if run.Spec.Scenario.Type == v1alpha1.ScenarioNetworkPartition {
		if netSpec := run.Spec.Scenario.Network; netSpec != nil && netSpec.Duration != nil {
			if run.Status.ObservingStartedAt != nil {
				elapsed := time.Since(run.Status.ObservingStartedAt.Time)
				if elapsed >= netSpec.Duration.Duration {
					log.Info("network partition duration elapsed, transitioning to CleaningUp",
						"duration", netSpec.Duration.Duration, "elapsed", elapsed)
					// Use Inconclusive when recovery was not confirmed.
					verdict := v1alpha1.VerdictInconclusive
					msg := "network partition duration elapsed"
					if run.Status.ReplacementObserved {
						verdict = v1alpha1.VerdictPassed
						msg = "network partition duration elapsed, recovery confirmed"
					}
					run.Status.Verdict = verdict
					TransitionPhase(run, v1alpha1.PhaseCleaningUp, msg+", removing policies")
					if err := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); err != nil {
						return ctrl.Result{}, err
					}
					return ctrl.Result{Requeue: true}, nil
				}
				// Requeue when the remaining duration elapses.
				remaining := netSpec.Duration.Duration - elapsed
				if err := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: remaining}, nil
			}
		}
	}

	// Resolve target for observation. Errors here are non-fatal — we continue
	// polling rather than failing the run, but log so the error is visible.
	target, resolveErr := r.resolveTarget(ctx, run)
	if resolveErr != nil {
		log.Error(resolveErr, "target resolution failed during observation; recovery signals may be incomplete")
	}

	// Poll recovery signals.
	obsResult, err := r.observeRecovery(ctx, run, target)
	if err != nil {
		log.Error(err, "observation poll error, will retry")
		return ctrl.Result{RequeueAfter: pollInterval}, nil
	}

	// Update observation status fields.
	if run.Status.Observation == nil {
		run.Status.Observation = &v1alpha1.ObservationStatus{}
	}
	// Preserve the injection-time TM count captured in reconcileInjecting —
	// the observer's computed tmCountBefore inflates once the replacement pod
	// is live (live_pods + injected = N+1 when N was the true before count).
	if run.Status.Observation.TaskManagerCountBefore == 0 {
		run.Status.Observation.TaskManagerCountBefore = obsResult.TMCountBefore
	}
	run.Status.Observation.TaskManagerCountAfter = obsResult.TMCountAfter
	run.Status.ReplacementObserved = obsResult.ReplacementObserved

	// When recovery is observed for the first time, record the timestamp and
	// immediately finalize the run. For scenarios that require cleanup
	// (NetworkChaos, NetworkPartition, ResourceExhaustion) transition to
	// CleaningUp so the driver can remove injected resources. For
	// TaskManagerPodKill (no cleanup needed) finalize directly with Passed.
	if obsResult.AllReplacementsReady && (run.Status.Observation == nil || run.Status.Observation.RecoveryObservedAt == nil) {
		now := metav1.Now()
		if run.Status.Observation == nil {
			run.Status.Observation = &v1alpha1.ObservationStatus{}
		}
		run.Status.Observation.RecoveryObservedAt = &now
		var recoveryMsg string
		if observeAnchor != nil {
			elapsed := time.Since(observeAnchor.Time)
			secs := int64(elapsed.Seconds())
			run.Status.Observation.RecoveryTimeSeconds = &secs
			recoveryMsg = fmt.Sprintf("recovery confirmed in %ds", secs)
		} else {
			recoveryMsg = "recovery confirmed"
		}
		log.Info("recovery observed, finalizing run", "recoveryMsg", recoveryMsg)
		SetCondition(run, v1alpha1.ConditionRecoveryObserved,
			metav1.ConditionTrue, "RecoveryObserved", recoveryMsg)
		r.Recorder.Event(run, corev1.EventTypeNormal, "RecoveryObserved", recoveryMsg)
		RecoveryObservedTotal.Inc()

		if needsCleanup {
			TransitionPhase(run, v1alpha1.PhaseCleaningUp, recoveryMsg+", cleaning up")
			if err := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		FinalizeRun(run, v1alpha1.VerdictPassed, recoveryMsg)
		r.finishRun(run, log)
		if err := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	recoveryAlreadySeen := run.Status.Observation != nil && run.Status.Observation.RecoveryObservedAt != nil
	log.Info("requeueing for next observation poll",
		"recoveryAlreadySeen", recoveryAlreadySeen,
		"replacementObserved", obsResult.ReplacementObserved,
		"pollInterval", pollInterval)
	if err := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); err != nil {
		log.Error(err, "status patch failed in reconcileObserving")
		return ctrl.Result{}, err
	}
	log.Info("status patch ok, next poll scheduled", "pollInterval", pollInterval)
	return ctrl.Result{RequeueAfter: pollInterval}, nil
}

// finishRun records terminal metrics for a run that has just reached a
// terminal phase. It must be called before the status patch.
func (r *ChaosRunReconciler) finishRun(run *v1alpha1.ChaosRun, log logr.Logger) {
	scenario := string(run.Spec.Scenario.Type)
	phase := string(run.Status.Phase)
	verdict := string(run.Status.Verdict)

	RunsTotal.WithLabelValues(scenario, phase, verdict).Inc()

	if run.Status.StartedAt != nil {
		elapsed := time.Since(run.Status.StartedAt.Time)
		RunDurationSeconds.WithLabelValues(scenario, verdict).Observe(elapsed.Seconds())
	}

	r.Recorder.Event(run, corev1.EventTypeNormal, "RunFinished",
		"ChaosRun reached terminal phase "+phase+" with verdict "+verdict)
	log.Info("run finished", "phase", phase, "verdict", verdict)
}

// ---------------------------------------------------------------------------
// Delegating helpers — these dispatch to the injected interface implementations.
// ---------------------------------------------------------------------------

// resolveTarget dispatches to the resolver registered for the run's target
// type. Returns an error when no resolver is registered for the type.
func (r *ChaosRunReconciler) resolveTarget(ctx context.Context, run *v1alpha1.ChaosRun) (*interfaces.ResolvedTarget, error) {
	if r.Resolvers == nil {
		return nil, fmt.Errorf("unsupported target type %q", run.Spec.Target.Type)
	}
	resolver, ok := r.Resolvers[run.Spec.Target.Type]
	if !ok {
		return nil, fmt.Errorf("unsupported target type %q", run.Spec.Target.Type)
	}
	return resolver.Resolve(ctx, run)
}

// cleanupGrace is the extra time allowed for cleanup after the observe timeout.
const cleanupGrace = 5 * time.Minute

// reconcileCleaningUp handles the CleaningUp phase for network chaos scenarios.
// It calls Cleanup on the scenario driver, checks whether all external resources
// have been removed, then finalises the run. The pending verdict stored in
// run.Status.Verdict (set when transitioning here from reconcileObserving) is
// preserved so that a timed-out run is not incorrectly reported as Passed.
func (r *ChaosRunReconciler) reconcileCleaningUp(ctx context.Context, log logr.Logger, run *v1alpha1.ChaosRun) (ctrl.Result, error) {
	log.Info("reconciling cleaning up run")
	runCopy := run.DeepCopy()

	// Absolute deadline: StartedAt + observeTimeout + cleanupGrace.
	// If exceeded, force the run to Failed to avoid spinning indefinitely.
	if run.Status.StartedAt != nil {
		deadline := run.Status.StartedAt.Time.
			Add(run.Spec.Observe.Timeout.Duration).
			Add(cleanupGrace)
		if time.Now().After(deadline) {
			log.Info("cleanup deadline exceeded, forcing terminal state")
			SetCondition(run, v1alpha1.ConditionNetworkChaosCleanedUp,
				metav1.ConditionFalse, "CleanupTimeout", "Cleanup did not complete within the allotted time")
			FailRun(run, "cleanup timeout: network chaos resources may not have been fully removed")
			r.finishRun(run, log)
			r.Recorder.Event(run, corev1.EventTypeWarning, "CleanupTimeout",
				"Cleanup timed out; network chaos resources may still be present")
			if patchErr := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); patchErr != nil {
				return ctrl.Result{}, patchErr
			}
			return ctrl.Result{}, nil
		}
	}

	// Call Cleanup on the driver if it supports it.
	if r.ScenarioDrivers != nil {
		if driver, ok := r.ScenarioDrivers[run.Spec.Scenario.Type]; ok {
			if cleanable, ok := driver.(interfaces.CleanableScenarioDriver); ok {
				if err := cleanable.Cleanup(ctx, run); err != nil {
					log.Error(err, "cleanup error, will retry")
					if patchErr := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); patchErr != nil {
						return ctrl.Result{}, patchErr
					}
					return ctrl.Result{RequeueAfter: run.Spec.Observe.PollInterval.Duration}, nil
				}
			}
		}
	}

	// Requeue if cleanup is not yet complete.
	if !r.isCleanupComplete(run) {
		log.Info("cleanup not yet complete, requeueing")
		if patchErr := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); patchErr != nil {
			return ctrl.Result{}, patchErr
		}
		return ctrl.Result{RequeueAfter: run.Spec.Observe.PollInterval.Duration}, nil
	}

	// Cleanup done — finalise. The outcome depends on how we arrived here:
	//   • abort requested → PhaseAborted
	//   • observation timeout → use the stored pending verdict (Failed/Inconclusive)
	//   • normal recovery observed → VerdictPassed
	SetCondition(run, v1alpha1.ConditionNetworkChaosCleanedUp,
		metav1.ConditionTrue, "CleanupComplete", "All network chaos resources removed")

	if hasConditionTrue(run, v1alpha1.ConditionAbortRequested) {
		AbortRun(run, "abort requested, network chaos resources cleaned up")
	} else {
		verdict := run.Status.Verdict
		if verdict == "" {
			verdict = v1alpha1.VerdictPassed
		}
		// Use the scenario-specific message stored during the observation
		// timeout transition, stripping the transitional ", cleaning up" suffix
		// and replacing it with the final ", cleaned up".
		finalMsg := "chaos experiment completed and cleaned up"
		if run.Status.Message != "" {
			finalMsg = strings.TrimSuffix(run.Status.Message, ", cleaning up") + ", cleaned up"
		}
		FinalizeRun(run, verdict, finalMsg)
	}

	r.finishRun(run, log)
	r.Recorder.Event(run, corev1.EventTypeNormal, "CleanupComplete",
		"Network chaos resources cleaned up, run finalized")
	if patchErr := r.Status().Patch(ctx, run, client.MergeFrom(runCopy)); patchErr != nil {
		return ctrl.Result{}, patchErr
	}

	// Remove the finalizer now that cleanup is complete and status is persisted.
	// Re-fetch to avoid resource version conflicts with the status patch above.
	if controllerutil.ContainsFinalizer(run, chaosRunFinalizer) {
		fresh := &v1alpha1.ChaosRun{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(run), fresh); err != nil {
			return ctrl.Result{}, err
		}
		if controllerutil.ContainsFinalizer(fresh, chaosRunFinalizer) {
			freshBase := fresh.DeepCopy()
			controllerutil.RemoveFinalizer(fresh, chaosRunFinalizer)
			if err := r.Patch(ctx, fresh, client.MergeFrom(freshBase)); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

// isCleanupComplete returns true when all injected resources have been removed.
func (r *ChaosRunReconciler) isCleanupComplete(run *v1alpha1.ChaosRun) bool {
	if len(run.Status.NetworkPolicies) > 0 {
		return false
	}
	for _, rec := range run.Status.EphemeralContainerInjections {
		if !rec.CleanedUp {
			return false
		}
	}
	for _, rec := range run.Status.ResourceExhaustionInjections {
		if !rec.CleanedUp {
			return false
		}
	}
	return true
}

// runSafetyChecks delegates to the injected SafetyChecker. When no checker is
// configured all checks are skipped (safe for test environments).
func (r *ChaosRunReconciler) runSafetyChecks(ctx context.Context, run *v1alpha1.ChaosRun, target *interfaces.ResolvedTarget) error {
	if r.SafetyChecker == nil {
		return nil
	}
	return r.SafetyChecker.Check(ctx, run, target)
}

// hasConditionTrue returns true when the named condition is present and True.
func hasConditionTrue(run *v1alpha1.ChaosRun, condType string) bool {
	for _, c := range run.Status.Conditions {
		if c.Type == condType {
			return c.Status == metav1.ConditionTrue
		}
	}
	return false
}

// injectChaos dispatches to the ScenarioDriver registered for the run's
// scenario type. When no drivers map is configured it falls back to a no-op
// that treats all target pods as both selected and injected.
func (r *ChaosRunReconciler) injectChaos(ctx context.Context, run *v1alpha1.ChaosRun, target *interfaces.ResolvedTarget) (*interfaces.InjectionResult, error) {
	if r.ScenarioDrivers == nil {
		return &interfaces.InjectionResult{
			SelectedPods: target.TMPodNames,
			InjectedPods: target.TMPodNames,
		}, nil
	}
	driver, ok := r.ScenarioDrivers[run.Spec.Scenario.Type]
	if !ok {
		return nil, fmt.Errorf("unsupported scenario type %q", run.Spec.Scenario.Type)
	}
	return driver.Inject(ctx, run, target)
}

// buildDryRunPreview returns a human-readable description of what would be
// injected if the run were not in dry-run mode. It is written to
// run.Status.DryRunPreview so operators can review the projected effect
// without any destructive action being taken.
func buildDryRunPreview(run *v1alpha1.ChaosRun, target *interfaces.ResolvedTarget) string {
	switch run.Spec.Scenario.Type {
	case v1alpha1.ScenarioTaskManagerPodKill:
		return fmt.Sprintf("would delete %d TaskManager pod(s): %v",
			len(target.TMPodNames), target.TMPodNames)

	case v1alpha1.ScenarioNetworkPartition:
		n := run.Spec.Scenario.Network
		if n == nil {
			return "would create NetworkPolicy for network partition (network spec missing)"
		}
		return fmt.Sprintf("would create NetworkPolicy: target=%s direction=%s",
			n.Target, n.Direction)

	case v1alpha1.ScenarioNetworkChaos:
		n := run.Spec.Scenario.Network
		if n == nil {
			return "would inject tc netem/tbf rules (network spec missing)"
		}
		return fmt.Sprintf("would inject tc netem/tbf on %d pod(s): target=%s direction=%s latency=%v jitter=%v loss=%v bandwidth=%q",
			len(target.TMPodNames), n.Target, n.Direction, n.Latency, n.Jitter, n.Loss, n.Bandwidth)

	case v1alpha1.ScenarioResourceExhaustion:
		re := run.Spec.Scenario.ResourceExhaustion
		if re == nil {
			return "would inject stress-ng (resource exhaustion spec missing)"
		}
		return fmt.Sprintf("would inject stress-ng on %d pod(s): mode=%s workers=%d duration=%v",
			len(target.TMPodNames), re.Mode, re.Workers, re.Duration)

	default:
		return fmt.Sprintf("would execute scenario %q on %d pod(s)", run.Spec.Scenario.Type, len(target.TMPodNames))
	}
}

// observeRecovery delegates to the injected Observer.
func (r *ChaosRunReconciler) observeRecovery(ctx context.Context, run *v1alpha1.ChaosRun, target *interfaces.ResolvedTarget) (*interfaces.ObservationResult, error) {
	if r.Observer == nil {
		return &interfaces.ObservationResult{}, nil
	}
	return r.Observer.Observe(ctx, run, target)
}
