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
	"context"
	"fmt"
	"log/slog"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	flinkrest "github.com/flink-chaos-operator/internal/observer/flinkrest"
)

// flinkJobsHandler handles GET /api/flink/jobs.
func flinkJobsHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fc, err := resolveFlinkClient(r.Context(), cfg, r.URL.Query().Get("deploymentName"))
		if err != nil {
			slog.Warn("flink jobs: endpoint not available", "error", err)
			writeError(w, http.StatusServiceUnavailable, "flink endpoint not available")
			return
		}

		jobs, err := fc.Jobs(r.Context())
		if err != nil {
			slog.Error("flink jobs: query failed", "error", err)
			writeError(w, http.StatusBadGateway, "failed to query Flink jobs")
			return
		}

		writeJSON(w, http.StatusOK, jobs)
	}
}

// flinkTaskManagersHandler handles GET /api/flink/taskmanagers.
func flinkTaskManagersHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fc, err := resolveFlinkClient(r.Context(), cfg, r.URL.Query().Get("deploymentName"))
		if err != nil {
			slog.Warn("flink taskmanagers: endpoint not available", "error", err)
			writeError(w, http.StatusServiceUnavailable, "flink endpoint not available")
			return
		}

		count, err := fc.TaskManagers(r.Context())
		if err != nil {
			slog.Error("flink taskmanagers: query failed", "error", err)
			writeError(w, http.StatusBadGateway, "failed to query Flink TaskManagers")
			return
		}

		writeJSON(w, http.StatusOK, map[string]int{"count": count})
	}
}

// flinkCheckpointsHandler handles GET /api/flink/checkpoints.
func flinkCheckpointsHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fc, err := resolveFlinkClient(r.Context(), cfg, r.URL.Query().Get("deploymentName"))
		if err != nil {
			slog.Warn("flink checkpoints: endpoint not available", "error", err)
			writeError(w, http.StatusServiceUnavailable, "flink endpoint not available")
			return
		}

		summary, err := fc.Checkpoints(r.Context())
		if err != nil {
			slog.Error("flink checkpoints: query failed", "error", err)
			writeError(w, http.StatusBadGateway, "failed to query Flink checkpoints")
			return
		}

		// summary is nil when no running jobs exist — return JSON null.
		writeJSON(w, http.StatusOK, summary)
	}
}

// flinkJobMetricsHandler handles GET /api/flink/jobmetrics.
// Returns aggregated checkpoint counters and cumulative throughput for the
// first running Flink job. The frontend uses consecutive samples to derive
// per-second record/byte rates.
func flinkJobMetricsHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fc, err := resolveFlinkClient(r.Context(), cfg, r.URL.Query().Get("deploymentName"))
		if err != nil {
			slog.Warn("flink jobmetrics: endpoint not available", "error", err)
			writeError(w, http.StatusServiceUnavailable, "flink endpoint not available")
			return
		}

		metrics, err := fc.JobMetrics(r.Context())
		if err != nil {
			slog.Error("flink jobmetrics: query failed", "error", err)
			writeError(w, http.StatusBadGateway, "failed to query Flink job metrics")
			return
		}

		writeJSON(w, http.StatusOK, metrics)
	}
}

// flinkTMMetricsHandler handles GET /api/flink/tmmetrics.
// Returns per-TM CPU load and JVM heap metrics from the Flink REST API.
func flinkTMMetricsHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fc, err := resolveFlinkClient(r.Context(), cfg, r.URL.Query().Get("deploymentName"))
		if err != nil {
			slog.Warn("flink tmmetrics: endpoint not available", "error", err)
			writeError(w, http.StatusServiceUnavailable, "flink endpoint not available")
			return
		}

		metrics, err := fc.TaskManagerMetrics(r.Context())
		if err != nil {
			slog.Error("flink tmmetrics: query failed", "error", err)
			writeError(w, http.StatusBadGateway, "failed to query Flink TM metrics")
			return
		}

		writeJSON(w, http.StatusOK, metrics)
	}
}

// resolveFlinkClient returns a flinkrest.Client aimed at the JobManager for
// the given deploymentName. When deploymentName is empty it picks the first
// running JobManager found in the namespace. When an explicit FlinkEndpoint is
// configured in cfg it is used unconditionally.
func resolveFlinkClient(ctx context.Context, cfg Config, deploymentName string) (flinkrest.Client, error) {
	if cfg.FlinkEndpoint != "" {
		return flinkrest.NewHTTPClient(cfg.FlinkEndpoint), nil
	}

	// List all jobmanager pods in the namespace. We use only the
	// component=jobmanager selector at the API-server level because VVP pods
	// carry camelCase label keys (e.g. "deploymentName") that some environments
	// do not apply as server-side label filters reliably. The deploymentName
	// filter is applied in-memory below.
	podList := &corev1.PodList{}
	if err := cfg.Client.List(ctx, podList,
		client.InNamespace(cfg.Namespace),
		client.MatchingLabels{"component": "jobmanager"},
	); err != nil {
		return nil, fmt.Errorf("list jobmanager pods: %w", err)
	}

	// Post-filter by deploymentName when provided. VVP pods carry the plain
	// "deploymentName" label; the "app" label is checked as a fallback for
	// standard Flink Kubernetes Operator deployments.
	if deploymentName != "" {
		filtered := podList.Items[:0]
		for i := range podList.Items {
			podDN := podList.Items[i].Labels["deploymentName"]
			if podDN == "" {
				podDN = podList.Items[i].Labels["app"]
			}
			if podDN == deploymentName {
				filtered = append(filtered, podList.Items[i])
			}
		}
		podList.Items = filtered
	}

	// Collect jobIds for running pods.
	runningJobIDs := map[string]bool{}
	for i := range podList.Items {
		if podList.Items[i].Status.Phase == corev1.PodRunning {
			if jobID, ok := podList.Items[i].Labels["jobId"]; ok && jobID != "" {
				runningJobIDs[jobID] = true
			}
		}
	}

	// List services and keep only those backed by a running pod.
	svcList := &corev1.ServiceList{}
	if err := cfg.Client.List(ctx, svcList,
		client.InNamespace(cfg.Namespace),
		client.MatchingLabels{"component": "jobmanager"},
	); err != nil {
		return nil, fmt.Errorf("list jobmanager services: %w", err)
	}

	var candidates []corev1.Service
	for i := range svcList.Items {
		jobID, ok := svcList.Items[i].Labels["jobId"]
		if ok && runningJobIDs[jobID] {
			candidates = append(candidates, svcList.Items[i])
		}
	}

	// Fall back to annotation-based discovery.
	if len(candidates) == 0 {
		all := &corev1.ServiceList{}
		if err := cfg.Client.List(ctx, all, client.InNamespace(cfg.Namespace)); err != nil {
			return nil, fmt.Errorf("list services: %w", err)
		}
		for i := range all.Items {
			if all.Items[i].Annotations["flink-chaos/flink-rest"] == "true" {
				candidates = append(candidates, all.Items[i])
				break
			}
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no running Flink REST service found in namespace %q (deploymentName=%q)",
			cfg.Namespace, deploymentName)
	}

	svc := candidates[0]
	port := int32(8081)
	for _, p := range svc.Spec.Ports {
		if p.Name == "rest" {
			port = p.Port
			break
		}
	}

	endpoint := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", svc.Name, svc.Namespace, port)
	return flinkrest.NewHTTPClient(endpoint), nil
}
