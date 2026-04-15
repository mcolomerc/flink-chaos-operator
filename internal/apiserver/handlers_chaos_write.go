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
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "github.com/flink-chaos-operator/api/v1alpha1"
)

// CreateChaosRunRequest is the JSON body accepted by POST /api/chaosruns.
type CreateChaosRunRequest struct {
	// TargetName is the FlinkDeployment resource name (FlinkDeployment targets)
	// or the human-readable VVP deployment name (VervericaDeployment targets).
	TargetName         string   `json:"targetName"`
	// TargetType distinguishes "FlinkDeployment" from "VervericaDeployment".
	// Defaults to "FlinkDeployment" when absent.
	TargetType         string   `json:"targetType,omitempty"`
	// DeploymentID is the Ververica Platform deployment UUID (VVP only).
	DeploymentID       string   `json:"deploymentId,omitempty"`
	// VVPNamespace is the Ververica namespace (VVP only).
	VVPNamespace       string   `json:"vvpNamespace,omitempty"`
	ScenarioType       string   `json:"scenarioType"`
	SelectionMode      string   `json:"selectionMode"`
	SelectionCount     int32    `json:"selectionCount,omitempty"`
	SelectionPods      []string `json:"selectionPods,omitempty"`
	GracePeriodSeconds *int64   `json:"gracePeriodSeconds,omitempty"`
	NetworkTarget      string   `json:"networkTarget,omitempty"`
	NetworkDirection   string   `json:"networkDirection,omitempty"`
	ExternalHostname   string   `json:"externalHostname,omitempty"`
	ExternalCIDR       string   `json:"externalCIDR,omitempty"`
	ExternalPort       int32    `json:"externalPort,omitempty"`
	LatencyMs          int64    `json:"latencyMs,omitempty"`
	JitterMs           int64    `json:"jitterMs,omitempty"`
	LossPercent        *int32   `json:"lossPercent,omitempty"`
	Bandwidth          string   `json:"bandwidth,omitempty"`
	DurationSeconds    int64    `json:"durationSeconds,omitempty"`
	ResourceMode       string   `json:"resourceMode,omitempty"`
	Workers            int32    `json:"workers,omitempty"`
	MemoryPercent      int32    `json:"memoryPercent,omitempty"`
	MinTaskManagers    int32    `json:"minTaskManagers,omitempty"`
	DryRun             bool     `json:"dryRun,omitempty"`
}

// createChaosRunHandler handles POST /api/chaosruns.
// It decodes the request body, builds a ChaosRun object, and creates it in Kubernetes.
func createChaosRunHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateChaosRunRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}

		if req.TargetName == "" {
			writeError(w, http.StatusBadRequest, "targetName is required")
			return
		}
		if req.ScenarioType == "" {
			writeError(w, http.StatusBadRequest, "scenarioType is required")
			return
		}
		name := fmt.Sprintf("chaos-run-%x", time.Now().UnixMilli())

		run := v1alpha1.ChaosRun{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "chaos.flink.io/v1alpha1",
				Kind:       "ChaosRun",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: cfg.Namespace,
			},
			Spec: v1alpha1.ChaosRunSpec{
				Target: buildTargetSpec(req),
				Scenario: v1alpha1.ScenarioSpec{
					Type: v1alpha1.ScenarioType(req.ScenarioType),
					Selection: v1alpha1.SelectionSpec{
						Mode:     v1alpha1.SelectionMode(req.SelectionMode),
						Count:    req.SelectionCount,
						PodNames: req.SelectionPods,
					},
				},
				Safety: v1alpha1.SafetySpec{
					MinTaskManagersRemaining: minTaskManagersPtr(req.MinTaskManagers),
					DryRun:                   req.DryRun,
				},
			},
		}

		switch v1alpha1.ScenarioType(req.ScenarioType) {
		case v1alpha1.ScenarioTaskManagerPodKill:
			run.Spec.Scenario.Action = v1alpha1.ActionSpec{
				Type:               v1alpha1.ActionDeletePod,
				GracePeriodSeconds: req.GracePeriodSeconds,
			}
			if req.DurationSeconds > 0 {
				run.Spec.Observe.Enabled = true
				run.Spec.Observe.Timeout = metav1.Duration{
					Duration: time.Duration(req.DurationSeconds) * time.Second,
				}
			}

		case v1alpha1.ScenarioNetworkPartition, v1alpha1.ScenarioNetworkChaos:
			run.Spec.Scenario.Network = buildNetworkChaosSpec(req)
			if req.DurationSeconds > 0 {
				run.Spec.Observe.Enabled = true
				run.Spec.Observe.Timeout = metav1.Duration{
					Duration: time.Duration(req.DurationSeconds) * time.Second,
				}
			}

		case v1alpha1.ScenarioResourceExhaustion:
			run.Spec.Scenario.ResourceExhaustion = buildResourceExhaustionSpec(req)
			if req.DurationSeconds > 0 {
				run.Spec.Observe.Enabled = true
				run.Spec.Observe.Timeout = metav1.Duration{
					Duration: time.Duration(req.DurationSeconds) * time.Second,
				}
			}
		}

		ctx := r.Context()
		if err := cfg.Client.Create(ctx, &run); err != nil {
			slog.Error("create chaosrun", "name", name, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create ChaosRun")
			return
		}

		writeJSON(w, http.StatusCreated, map[string]string{
			"name":      run.Name,
			"namespace": run.Namespace,
		})
	}
}

// abortChaosRunHandler handles PATCH /api/chaosruns/{name}/abort.
// It sets Spec.Control.Abort = true on the named ChaosRun to request an immediate stop.
func abortChaosRunHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}

		ctx := r.Context()
		run := &v1alpha1.ChaosRun{}
		key := types.NamespacedName{Namespace: cfg.Namespace, Name: name}
		if err := cfg.Client.Get(ctx, key, run); err != nil {
			if k8serrors.IsNotFound(err) {
				writeError(w, http.StatusNotFound, "ChaosRun not found")
				return
			}
			slog.Error("get chaosrun for abort", "name", name, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to get ChaosRun")
			return
		}

		run.Spec.Control.Abort = true
		if err := cfg.Client.Update(ctx, run); err != nil {
			slog.Error("abort chaosrun", "name", name, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to abort ChaosRun")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// deleteChaosRunHandler handles DELETE /api/chaosruns/{name}.
// It permanently removes the named ChaosRun from Kubernetes.
// Only terminal runs (Completed/Aborted/Failed) should be deleted via the UI,
// but the handler does not enforce that — the operator's finalizer logic handles safety.
func deleteChaosRunHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}

		ctx := r.Context()
		run := &v1alpha1.ChaosRun{}
		key := types.NamespacedName{Namespace: cfg.Namespace, Name: name}
		if err := cfg.Client.Get(ctx, key, run); err != nil {
			if k8serrors.IsNotFound(err) {
				writeError(w, http.StatusNotFound, "ChaosRun not found")
				return
			}
			slog.Error("get chaosrun for delete", "name", name, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to get ChaosRun")
			return
		}

		if err := cfg.Client.Delete(ctx, run); err != nil {
			slog.Error("delete chaosrun", "name", name, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to delete ChaosRun")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// buildTargetSpec constructs the correct TargetSpec based on the request's
// TargetType field. Defaults to FlinkDeployment when TargetType is absent.
func buildTargetSpec(req CreateChaosRunRequest) v1alpha1.TargetSpec {
	if req.TargetType == string(v1alpha1.TargetVervericaDeployment) {
		return v1alpha1.TargetSpec{
			Type:           v1alpha1.TargetVervericaDeployment,
			DeploymentName: req.TargetName,
			DeploymentID:   req.DeploymentID,
			VVPNamespace:   req.VVPNamespace,
		}
	}
	return v1alpha1.TargetSpec{
		Type: v1alpha1.TargetFlinkDeployment,
		Name: req.TargetName,
	}
}

// buildNetworkChaosSpec constructs a NetworkChaosSpec from the request fields,
// applying defaults for Target and Direction when not provided.
func buildNetworkChaosSpec(req CreateChaosRunRequest) *v1alpha1.NetworkChaosSpec {
	target := v1alpha1.NetworkTarget(req.NetworkTarget)
	if target == "" {
		target = v1alpha1.NetworkTargetTMtoJM
	}

	direction := v1alpha1.NetworkDirection(req.NetworkDirection)
	if direction == "" {
		direction = v1alpha1.NetworkDirectionBoth
	}

	spec := &v1alpha1.NetworkChaosSpec{
		Target:    target,
		Direction: direction,
	}

	if req.ExternalHostname != "" || req.ExternalCIDR != "" {
		spec.ExternalEndpoint = &v1alpha1.ExternalTarget{
			Hostname: req.ExternalHostname,
			CIDR:     req.ExternalCIDR,
		}
		if req.ExternalPort > 0 {
			spec.ExternalEndpoint.Port = req.ExternalPort
		}
	}

	if req.LatencyMs > 0 {
		spec.Latency = &metav1.Duration{Duration: time.Duration(req.LatencyMs) * time.Millisecond}
	}
	if req.JitterMs > 0 {
		spec.Jitter = &metav1.Duration{Duration: time.Duration(req.JitterMs) * time.Millisecond}
	}
	if req.LossPercent != nil {
		spec.Loss = req.LossPercent
	}
	if req.Bandwidth != "" {
		spec.Bandwidth = req.Bandwidth
	}
	if req.DurationSeconds > 0 {
		spec.Duration = &metav1.Duration{Duration: time.Duration(req.DurationSeconds) * time.Second}
	}

	return spec
}

// buildResourceExhaustionSpec constructs a ResourceExhaustionSpec from the request
// fields, applying defaults for Mode, Workers, and MemoryPercent when not provided.
func buildResourceExhaustionSpec(req CreateChaosRunRequest) *v1alpha1.ResourceExhaustionSpec {
	mode := v1alpha1.ResourceExhaustionMode(req.ResourceMode)
	if mode == "" {
		mode = v1alpha1.ResourceExhaustionModeCPU
	}

	workers := req.Workers
	if workers == 0 {
		workers = 1
	}

	memoryPercent := req.MemoryPercent
	if memoryPercent == 0 {
		memoryPercent = 80
	}

	spec := &v1alpha1.ResourceExhaustionSpec{
		Mode:          mode,
		Workers:       workers,
		MemoryPercent: memoryPercent,
	}

	if req.DurationSeconds > 0 {
		spec.Duration = &metav1.Duration{Duration: time.Duration(req.DurationSeconds) * time.Second}
	}

	return spec
}

// minTaskManagersPtr converts the int32 value from the request into the *int32
// pointer expected by SafetySpec.MinTaskManagersRemaining.
// A zero value is passed through as-is because the CRD allows 0 to mean
// "allow killing all TaskManagers".
func minTaskManagersPtr(v int32) *int32 {
	return &v
}
