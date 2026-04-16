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
	"log/slog"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/flink-chaos-operator/api/v1alpha1"
)

// TopologyResponse is the JSON response for GET /api/topology.
type TopologyResponse struct {
	DeploymentName      string                  `json:"deploymentName"`
	// TargetType is "FlinkDeployment" or "VervericaDeployment" — tells the UI
	// which ChaosRun target spec to build when launching an experiment.
	TargetType          string                  `json:"targetType"`
	// DeploymentID is the Ververica Platform deployment UUID (VVP only).
	DeploymentID        string                  `json:"deploymentId,omitempty"`
	// VVPNamespace is the Ververica namespace the deployment lives in (VVP only).
	VVPNamespace        string                  `json:"vvpNamespace,omitempty"`
	JobManagers         []PodInfo               `json:"jobManagers"`
	TaskManagers        []PodInfo               `json:"taskManagers"`
	ExternalConnections []ExternalConnection    `json:"externalConnections"`
	ActiveChaosRuns     []ActiveChaosRunSummary `json:"activeChaosRuns"`
}

// PodInfo is a concise representation of a Kubernetes Pod.
type PodInfo struct {
	Name   string            `json:"name"`
	Phase  string            `json:"phase"`
	Labels map[string]string `json:"labels"`
	PodIP  string            `json:"podIP,omitempty"`
}

// ExternalConnection describes an external storage endpoint derived from
// Flink configuration (checkpoint/savepoint directories).
type ExternalConnection struct {
	Type     string `json:"type"`     // "S3", "GCS", "HDFS", "Unknown"
	Endpoint string `json:"endpoint"`
	Purpose  string `json:"purpose"`  // "checkpoint" or "savepoint"
}

// ActiveChaosRunSummary is a brief summary of a non-terminal ChaosRun.
type ActiveChaosRunSummary struct {
	Name       string   `json:"name"`
	Phase      string   `json:"phase"`
	Scenario   string   `json:"scenario"`
	TargetPods []string `json:"targetPods"`
	StartedAt  string   `json:"startedAt,omitempty"` // RFC3339
}

// flinkDeploymentGVK is the GroupVersionKind for the Apache Flink Kubernetes
// Operator's FlinkDeployment CRD.
var flinkDeploymentGVK = schema.GroupVersionKind{
	Group:   "flink.apache.org",
	Version: "v1beta1",
	Kind:    "FlinkDeployment",
}

// topologyHandler handles GET /api/topology.
// Accepts an optional ?deploymentName= query parameter to scope the response
// to a single Flink deployment; when absent the first running deployment is used.
func topologyHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		filterName := r.URL.Query().Get("deploymentName")

		jms, err := listPodsByDeployment(ctx, cfg, "jobmanager", filterName)
		if err != nil {
			slog.Error("topology: list jobmanagers", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list JobManager pods")
			return
		}

		tms, err := listPodsByDeployment(ctx, cfg, "taskmanager", filterName)
		if err != nil {
			slog.Error("topology: list taskmanagers", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list TaskManager pods")
			return
		}

		resp := TopologyResponse{
			JobManagers: jms,
			TaskManagers: tms,
		}

		// Try Apache Flink Operator (FlinkDeployment CRD) first.
		fdName, extConns := resolveFlinkDeployment(ctx, cfg, filterName)
		if fdName != "" {
			resp.DeploymentName = fdName
			resp.TargetType = "FlinkDeployment"
			resp.ExternalConnections = extConns
		} else {
			// Fall back to Ververica Platform: read info from pod labels.
			vvpName, vvpID, vvpNS, vvpConns := resolveVVPDeployment(ctx, cfg, jms)
			resp.DeploymentName = vvpName
			resp.TargetType = "VervericaDeployment"
			resp.DeploymentID = vvpID
			resp.VVPNamespace = vvpNS
			resp.ExternalConnections = vvpConns
		}

		activeChaosRuns, err := listActiveChaosRuns(ctx, cfg)
		if err != nil {
			slog.Error("topology: list active chaos runs", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list active ChaosRuns")
			return
		}
		resp.ActiveChaosRuns = activeChaosRuns

		writeJSON(w, http.StatusOK, resp)
	}
}

// listPodsByDeployment returns PodInfo slices for pods with component=<role>.
// When deploymentName is non-empty it filters by the "deploymentName" label
// (VVP) or the "app" label (Apache Flink Operator). A secondary in-memory
// filter is applied as a safeguard in case the label selector is not applied
// server-side (e.g. camelCase label keys in some controller-runtime builds).
func listPodsByDeployment(ctx context.Context, cfg Config, role, deploymentName string) ([]PodInfo, error) {
	podList := &corev1.PodList{}
	if err := cfg.Client.List(ctx, podList,
		client.InNamespace(cfg.Namespace),
		client.MatchingLabels{"component": role},
	); err != nil {
		return nil, err
	}

	result := make([]PodInfo, 0, len(podList.Items))
	for _, pod := range podList.Items {
		if deploymentName != "" {
			// VVP pods carry a "deploymentName" label; Apache Flink Operator
			// pods use "app".  Accept either.
			podName := pod.Labels["deploymentName"]
			if podName == "" {
				podName = pod.Labels["app"]
			}
			if podName != deploymentName {
				continue
			}
		}
		result = append(result, PodInfo{
			Name:   pod.Name,
			Phase:  string(pod.Status.Phase),
			Labels: pod.Labels,
			PodIP:  pod.Status.PodIP,
		})
	}
	return result, nil
}

// resolveFlinkDeployment attempts to read FlinkDeployment CRs via the dynamic
// client and extract storage endpoint configuration. If the CRD is not
// installed the error is swallowed and empty values are returned.
func resolveFlinkDeployment(ctx context.Context, cfg Config, filterName string) (string, []ExternalConnection) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   flinkDeploymentGVK.Group,
		Version: flinkDeploymentGVK.Version,
		Kind:    flinkDeploymentGVK.Kind + "List",
	})

	if err := cfg.Client.List(ctx, list, client.InNamespace(cfg.Namespace)); err != nil {
		// Swallow "no kind registered" and similar CRD-not-installed errors.
		slog.Debug("topology: FlinkDeployment CRD not available", "error", err)
		return "", nil
	}

	if len(list.Items) == 0 {
		return "", nil
	}

	// Pick the item matching filterName when specified; otherwise use the first.
	var selected *unstructured.Unstructured
	for i := range list.Items {
		if filterName == "" || list.Items[i].GetName() == filterName {
			selected = &list.Items[i]
			break
		}
	}
	if selected == nil {
		return "", nil
	}

	first := selected
	deploymentName := first.GetName()

	flinkConfig, _, _ := unstructured.NestedStringMap(first.Object, "spec", "flinkConfiguration")

	var extConns []ExternalConnection
	if dir, ok := flinkConfig["state.checkpoints.dir"]; ok && dir != "" {
		extConns = append(extConns, ExternalConnection{
			Type:     storageType(dir),
			Endpoint: dir,
			Purpose:  "checkpoint",
		})
	}
	if dir, ok := flinkConfig["state.savepoints.dir"]; ok && dir != "" {
		extConns = append(extConns, ExternalConnection{
			Type:     storageType(dir),
			Endpoint: dir,
			Purpose:  "savepoint",
		})
	}

	return deploymentName, extConns
}

// resolveVVPDeployment reads Ververica Platform deployment info from running
// JobManager pod labels and looks up checkpoint/savepoint paths from the
// Flink ConfigMap that VVP creates per job (named "job-{jobId}-config").
// Returns deploymentName, deploymentID, vvpNamespace, and storage connections.
func resolveVVPDeployment(ctx context.Context, cfg Config, jms []PodInfo) (name, deploymentID, vvpNS string, conns []ExternalConnection) {
	// Extract identity fields from the first running JM pod's labels.
	for _, jm := range jms {
		if jm.Phase != "Running" {
			continue
		}
		if n, ok := jm.Labels["deploymentName"]; ok && n != "" {
			name = n
		}
		if id, ok := jm.Labels["deploymentId"]; ok && id != "" {
			deploymentID = id
		}
		if ns, ok := jm.Labels["vvpNamespace"]; ok && ns != "" {
			vvpNS = ns
		}
		break
	}

	// Use jobId label to locate the Flink ConfigMap that VVP creates per job.
	// The ConfigMap is named "job-{jobId}-config" in the same namespace.
	var jobID string
	for _, jm := range jms {
		if id, ok := jm.Labels["jobId"]; ok && id != "" {
			jobID = id
			break
		}
	}

	if jobID != "" {
		cmName := "job-" + jobID + "-config"
		cm := &corev1.ConfigMap{}
		if err := cfg.Client.Get(ctx, client.ObjectKey{Namespace: cfg.Namespace, Name: cmName}, cm); err == nil {
			// Try flat keys first (some operators write them directly).
			if dir, ok := cm.Data["state.checkpoints.dir"]; ok && dir != "" {
				conns = append(conns, ExternalConnection{Type: storageType(dir), Endpoint: dir, Purpose: "checkpoint"})
			}
			if dir, ok := cm.Data["state.savepoints.dir"]; ok && dir != "" {
				conns = append(conns, ExternalConnection{Type: storageType(dir), Endpoint: dir, Purpose: "savepoint"})
			}
			// VVP stores config inside a "flink-conf.yaml" key as a multi-line
			// "key: value" YAML document. Parse it line by line.
			if len(conns) == 0 {
				if raw, ok := cm.Data["flink-conf.yaml"]; ok && raw != "" {
					conns = append(conns, parseFlinkConfYAML(raw)...)
				}
			}
		} else {
			slog.Debug("topology: VVP config map not found", "name", cmName, "error", err)
		}
	}

	// Fallback: read checkpoint/savepoint dirs from JM pod environment variables.
	// VVP injects Flink configuration via FLINK_PROPERTIES (a multiline
	// "key: value" block) and/or the individual env vars CHECKPOINT_DIR /
	// SAVEPOINT_DIR.  We only probe the first running JM pod.
	if len(conns) == 0 {
		for _, jm := range jms {
			if jm.Phase != "Running" {
				continue
			}
			pod := &corev1.Pod{}
			if err := cfg.Client.Get(ctx, client.ObjectKey{Namespace: cfg.Namespace, Name: jm.Name}, pod); err != nil {
				slog.Debug("topology: failed to fetch JM pod for env fallback", "pod", jm.Name, "error", err)
				break
			}
			for _, container := range pod.Spec.Containers {
				for _, env := range container.Env {
					switch env.Name {
					case "FLINK_PROPERTIES":
						if env.Value == "" {
							continue
						}
						for _, line := range strings.Split(env.Value, "\n") {
							parts := strings.SplitN(strings.TrimSpace(line), ": ", 2)
							if len(parts) != 2 {
								continue
							}
							key, val := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
							if val == "" {
								continue
							}
							switch key {
							case "state.checkpoints.dir":
								conns = append(conns, ExternalConnection{Type: storageType(val), Endpoint: val, Purpose: "checkpoint"})
							case "state.savepoints.dir":
								conns = append(conns, ExternalConnection{Type: storageType(val), Endpoint: val, Purpose: "savepoint"})
							}
						}
					case "CHECKPOINT_DIR":
						if env.Value != "" {
							conns = append(conns, ExternalConnection{Type: storageType(env.Value), Endpoint: env.Value, Purpose: "checkpoint"})
						}
					case "SAVEPOINT_DIR":
						if env.Value != "" {
							conns = append(conns, ExternalConnection{Type: storageType(env.Value), Endpoint: env.Value, Purpose: "savepoint"})
						}
					}
				}
			}
			break // only the first running JM is needed
		}
	}

	// Deduplicate connections: keep the first occurrence of each (endpoint, purpose) pair.
	conns = deduplicateConnections(conns)

	return name, deploymentID, vvpNS, conns
}

// parseFlinkConfYAML parses a flink-conf.yaml string (key: value lines) and
// extracts checkpoint/savepoint directory entries as ExternalConnections.
func parseFlinkConfYAML(raw string) []ExternalConnection {
	var conns []ExternalConnection
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), ": ", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if val == "" {
			continue
		}
		switch key {
		case "state.checkpoints.dir":
			conns = append(conns, ExternalConnection{Type: storageType(val), Endpoint: val, Purpose: "checkpoint"})
		case "state.savepoints.dir":
			conns = append(conns, ExternalConnection{Type: storageType(val), Endpoint: val, Purpose: "savepoint"})
		}
	}
	return conns
}

// deduplicateConnections removes duplicate ExternalConnections, retaining the
// first occurrence of each (Endpoint, Purpose) pair.
func deduplicateConnections(conns []ExternalConnection) []ExternalConnection {
	seen := make(map[string]struct{}, len(conns))
	out := conns[:0]
	for _, c := range conns {
		key := c.Endpoint + "|" + c.Purpose
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, c)
	}
	return out
}

// storageType detects the storage backend from a URI prefix.
func storageType(uri string) string {
	switch {
	case strings.HasPrefix(uri, "s3://"), strings.HasPrefix(uri, "s3a://"), strings.HasPrefix(uri, "s3n://"):
		return "S3"
	case strings.HasPrefix(uri, "gs://"):
		return "GCS"
	case strings.HasPrefix(uri, "hdfs://"):
		return "HDFS"
	case strings.HasPrefix(uri, "wasbs://"), strings.HasPrefix(uri, "abfs://"), strings.HasPrefix(uri, "abfss://"):
		return "AzureBlob"
	default:
		return "Unknown"
	}
}

// listActiveChaosRuns returns summaries for all non-terminal ChaosRuns.
func listActiveChaosRuns(ctx context.Context, cfg Config) ([]ActiveChaosRunSummary, error) {
	runList := &v1alpha1.ChaosRunList{}
	if err := cfg.Client.List(ctx, runList, client.InNamespace(cfg.Namespace)); err != nil {
		return nil, err
	}

	var result []ActiveChaosRunSummary
	for _, run := range runList.Items {
		phase := run.Status.Phase
		if isTerminalPhase(phase) {
			continue
		}

		targetPods := run.Status.InjectedPods
		if len(targetPods) == 0 {
			targetPods = run.Status.SelectedPods
		}

		startedAt := rfc3339(run.Status.StartedAt)

		result = append(result, ActiveChaosRunSummary{
			Name:       run.Name,
			Phase:      string(phase),
			Scenario:   string(run.Spec.Scenario.Type),
			TargetPods: targetPods,
			StartedAt:  startedAt,
		})
	}
	return result, nil
}

// isTerminalPhase reports whether a RunPhase is a terminal state.
func isTerminalPhase(p v1alpha1.RunPhase) bool {
	return p == v1alpha1.PhaseCompleted || p == v1alpha1.PhaseAborted || p == v1alpha1.PhaseFailed
}
