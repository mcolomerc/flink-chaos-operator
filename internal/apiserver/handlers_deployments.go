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

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeploymentInfo describes a running Flink deployment discovered from pod labels.
type DeploymentInfo struct {
	DeploymentName string `json:"deploymentName"`
	DeploymentID   string `json:"deploymentId,omitempty"`
	VVPNamespace   string `json:"vvpNamespace,omitempty"`
	JobID          string `json:"jobId,omitempty"`
	// K8sNamespace is the Kubernetes namespace the JobManager pods reside in.
	K8sNamespace string `json:"k8sNamespace"`
	// TargetType is "FlinkDeployment" or "VervericaDeployment".
	TargetType string `json:"targetType"`
}

// listDeploymentsHandler handles GET /api/deployments.
// It returns one entry per unique running Flink deployment, derived from
// JobManager pod labels. The list drives the job-switcher badges in the header.
func listDeploymentsHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		podList := &corev1.PodList{}
		if err := cfg.Client.List(ctx, podList,
			client.InNamespace(cfg.Namespace),
			client.MatchingLabels{"component": "jobmanager"},
		); err != nil {
			slog.Error("list deployments: list pods", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list pods")
			return
		}

		// Deduplicate by deploymentName; prefer VVP labels, fall back to
		// "app" label used by the Apache Flink Kubernetes Operator.
		seen := map[string]bool{}
		var result []DeploymentInfo

		for i := range podList.Items {
			pod := &podList.Items[i]
			if pod.Status.Phase != corev1.PodRunning {
				continue
			}
			labels := pod.Labels

			// VVP deployment
			if name, ok := labels["deploymentName"]; ok && name != "" {
				if seen[name] {
					continue
				}
				seen[name] = true
				result = append(result, DeploymentInfo{
					DeploymentName: name,
					DeploymentID:   labels["deploymentId"],
					VVPNamespace:   labels["vvpNamespace"],
					JobID:          labels["jobId"],
					K8sNamespace:   pod.Namespace,
					TargetType:     "VervericaDeployment",
				})
				continue
			}

			// Apache Flink Kubernetes Operator deployment (uses "app" label)
			if name, ok := labels["app"]; ok && name != "" {
				if seen[name] {
					continue
				}
				seen[name] = true
				result = append(result, DeploymentInfo{
					DeploymentName: name,
					K8sNamespace:   pod.Namespace,
					TargetType:     "FlinkDeployment",
				})
			}
		}

		if result == nil {
			result = []DeploymentInfo{}
		}
		writeJSON(w, http.StatusOK, result)
	}
}
