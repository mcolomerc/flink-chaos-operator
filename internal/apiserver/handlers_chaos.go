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

// Package apiserver implements the HTTP API server that bridges the web UI
// to Kubernetes and Flink REST APIs.
package apiserver

import (
	"log/slog"
	"net/http"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/flink-chaos-operator/api/v1alpha1"
)

// ChaosRunSummary is the list-view representation of a ChaosRun.
type ChaosRunSummary struct {
	Name      string `json:"name"`
	Phase     string `json:"phase"`
	Verdict   string `json:"verdict,omitempty"`
	Scenario  string `json:"scenario"`
	Target    string `json:"target"`
	StartedAt string `json:"startedAt,omitempty"`
	EndedAt   string `json:"endedAt,omitempty"`
}

// ChaosRunDetail extends ChaosRunSummary with per-pod and message fields.
type ChaosRunDetail struct {
	ChaosRunSummary
	SelectedPods        []string `json:"selectedPods,omitempty"`
	InjectedPods        []string `json:"injectedPods,omitempty"`
	Message             string   `json:"message,omitempty"`
	NetworkPolicies     []string `json:"networkPolicies,omitempty"`
	// Recovery observation fields — set for TaskManagerPodKill when an
	// observation window was configured.
	RecoveryTimeSeconds     *int64  `json:"recoveryTimeSeconds,omitempty"`
	RecoveryObservedAt      string  `json:"recoveryObservedAt,omitempty"`      // RFC3339
	TMCountBefore           int     `json:"tmCountBefore,omitempty"`
	TMCountAfter            int     `json:"tmCountAfter,omitempty"`
	ObservationTimeoutSecs  int64   `json:"observationTimeoutSecs,omitempty"`
}

// listChaosRunsHandler handles GET /api/chaosruns.
// Accepts an optional ?deploymentName= query parameter to return only runs
// whose target matches the given deployment name.
func listChaosRunsHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filterName := r.URL.Query().Get("deploymentName")

		runList := &v1alpha1.ChaosRunList{}
		if err := cfg.Client.List(r.Context(), runList, client.InNamespace(cfg.Namespace)); err != nil {
			slog.Error("list chaosruns", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list ChaosRuns")
			return
		}

		summaries := make([]ChaosRunSummary, 0, len(runList.Items))
		for i := range runList.Items {
			run := &runList.Items[i]
			if filterName != "" && !runMatchesDeployment(run, filterName) {
				continue
			}
			summaries = append(summaries, toChaosRunSummary(run))
		}
		writeJSON(w, http.StatusOK, summaries)
	}
}

// runMatchesDeployment reports whether a ChaosRun targets the given deployment name.
// It checks the human-readable name fields used by both FlinkDeployment (Spec.Target.Name)
// and VervericaDeployment (Spec.Target.DeploymentName) targets, as well as the
// resolved TargetSummary written to status by the operator.
func runMatchesDeployment(run *v1alpha1.ChaosRun, deploymentName string) bool {
	// Check the spec target fields first (always present).
	if run.Spec.Target.Name == deploymentName {
		return true
	}
	if run.Spec.Target.DeploymentName == deploymentName {
		return true
	}
	// Fall back to the resolved TargetSummary written by the operator.
	if run.Status.TargetSummary != nil && run.Status.TargetSummary.Name == deploymentName {
		return true
	}
	return false
}

// getChaosRunHandler handles GET /api/chaosruns/{name}.
func getChaosRunHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		if !validK8sName.MatchString(name) {
			writeError(w, http.StatusBadRequest, "invalid resource name")
			return
		}

		run := &v1alpha1.ChaosRun{}
		key := types.NamespacedName{Name: name, Namespace: cfg.Namespace}
		if err := cfg.Client.Get(r.Context(), key, run); err != nil {
			if client.IgnoreNotFound(err) == nil {
				writeError(w, http.StatusNotFound, "ChaosRun not found")
				return
			}
			slog.Error("get chaosrun", "name", name, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to get ChaosRun")
			return
		}

		writeJSON(w, http.StatusOK, toChaosRunDetail(run))
	}
}

// toChaosRunSummary converts a ChaosRun to its summary representation.
func toChaosRunSummary(run *v1alpha1.ChaosRun) ChaosRunSummary {
	target := string(run.Spec.Target.Type)
	if run.Status.TargetSummary != nil {
		target = run.Status.TargetSummary.Type + "/" + run.Status.TargetSummary.Name
	}

	return ChaosRunSummary{
		Name:      run.Name,
		Phase:     string(run.Status.Phase),
		Verdict:   string(run.Status.Verdict),
		Scenario:  string(run.Spec.Scenario.Type),
		Target:    target,
		StartedAt: rfc3339(run.Status.StartedAt),
		EndedAt:   rfc3339(run.Status.EndedAt),
	}
}

// toChaosRunDetail converts a ChaosRun to its detailed representation.
func toChaosRunDetail(run *v1alpha1.ChaosRun) ChaosRunDetail {
	d := ChaosRunDetail{
		ChaosRunSummary: toChaosRunSummary(run),
		SelectedPods:    run.Status.SelectedPods,
		InjectedPods:    run.Status.InjectedPods,
		Message:         run.Status.Message,
		NetworkPolicies: run.Status.NetworkPolicies,
	}
	if obs := run.Status.Observation; obs != nil {
		d.TMCountBefore = obs.TaskManagerCountBefore
		d.TMCountAfter = obs.TaskManagerCountAfter
		d.RecoveryTimeSeconds = obs.RecoveryTimeSeconds
		d.RecoveryObservedAt = rfc3339(obs.RecoveryObservedAt)
	}
	if run.Spec.Observe.Enabled && run.Spec.Observe.Timeout.Duration > 0 {
		d.ObservationTimeoutSecs = int64(run.Spec.Observe.Timeout.Duration.Seconds())
	}
	return d
}

// rfc3339 formats a *metav1.Time as an RFC3339 string, returning "" for nil/zero.
func rfc3339(t *metav1.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
