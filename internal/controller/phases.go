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
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/flink-chaos-operator/api/v1alpha1"
)

// IsTerminal reports whether the given phase is a terminal lifecycle phase.
// Terminal phases are Completed, Aborted, and Failed. Once a ChaosRun enters
// a terminal phase the controller stops requeuing it.
func IsTerminal(phase v1alpha1.RunPhase) bool {
	switch phase {
	case v1alpha1.PhaseCompleted, v1alpha1.PhaseAborted, v1alpha1.PhaseFailed:
		return true
	default:
		return false
	}
}

// SetCondition sets or updates a single condition on the ChaosRun status.
// If a condition of the same type already exists it is replaced in-place,
// preserving the ListType=map contract declared on the Conditions field.
func SetCondition(run *v1alpha1.ChaosRun, condType string, status metav1.ConditionStatus, reason, message string) {
	cond := metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}
	meta.SetStatusCondition(&run.Status.Conditions, cond)
}

// TransitionPhase updates the run's phase and message. It does not persist the
// change — the caller is responsible for patching status after this call.
func TransitionPhase(run *v1alpha1.ChaosRun, phase v1alpha1.RunPhase, message string) {
	run.Status.Phase = phase
	run.Status.Message = message
}

// FinalizeRun transitions the run to PhaseCompleted, records the verdict,
// sets EndedAt to now, and writes a human-readable message. It does not
// persist the change — the caller must patch status afterward.
func FinalizeRun(run *v1alpha1.ChaosRun, verdict v1alpha1.RunVerdict, message string) {
	now := metav1.Now()
	run.Status.Phase = v1alpha1.PhaseCompleted
	run.Status.Verdict = verdict
	run.Status.EndedAt = &now
	run.Status.Message = message
}

// FailRun transitions the run to PhaseFailed with a VerdictFailed verdict,
// sets EndedAt to now, and records the failure message. It does not persist
// the change — the caller must patch status afterward.
func FailRun(run *v1alpha1.ChaosRun, message string) {
	now := metav1.Now()
	run.Status.Phase = v1alpha1.PhaseFailed
	run.Status.Verdict = v1alpha1.VerdictFailed
	run.Status.EndedAt = &now
	run.Status.Message = message
}

// AbortRun transitions the run to PhaseAborted, sets VerdictInconclusive,
// records EndedAt, and writes the message. It does not persist the change —
// the caller must patch status afterward.
func AbortRun(run *v1alpha1.ChaosRun, message string) {
	now := metav1.Now()
	run.Status.Phase = v1alpha1.PhaseAborted
	run.Status.Verdict = v1alpha1.VerdictInconclusive
	run.Status.EndedAt = &now
	run.Status.Message = message
}
