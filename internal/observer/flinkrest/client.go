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

// Package flinkrest provides a Flink REST API client and observer
// that polls /jobs and /taskmanagers to determine workload recovery state.
package flinkrest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Client is the interface for querying the Flink REST API.
// It is separated from the observer to allow test stubbing.
type Client interface {
	// Jobs returns the list of jobs and their states from GET /jobs/overview.
	Jobs(ctx context.Context) ([]JobSummary, error)
	// TaskManagers returns the count of registered TaskManagers from GET /taskmanagers.
	TaskManagers(ctx context.Context) (int, error)
	// TaskManagerMetrics returns per-TM CPU load and JVM heap usage from
	// GET /taskmanagers and GET /taskmanagers/{id}/metrics.
	TaskManagerMetrics(ctx context.Context) ([]TMMetrics, error)
	// Checkpoints returns checkpoint summary for the first running job.
	// Returns nil, nil when no jobs exist.
	Checkpoints(ctx context.Context) (*CheckpointSummary, error)
	// JobMetrics returns aggregated throughput and checkpoint metrics for the
	// first running job. Returns nil, nil when no running jobs exist.
	JobMetrics(ctx context.Context) (*JobMetricsSummary, error)
}

// TMMetrics holds CPU and JVM heap metrics for a single TaskManager.
// PodIP is extracted from the TM id (format: "{ip}:{port}-{suffix}") and used
// by the UI to correlate metrics with Kubernetes pod names.
type TMMetrics struct {
	// TMID is the Flink TaskManager identifier.
	TMID string `json:"tmId"`
	// PodIP is the IP address portion of the TM id.
	PodIP string `json:"podIP"`
	// CPULoad is the JVM CPU load in the range [0, 1]. -1 means not available.
	CPULoad float64 `json:"cpuLoad"`
	// HeapUsedMB is the JVM heap memory currently in use, in MiB.
	HeapUsedMB int64 `json:"heapUsedMB"`
	// HeapMaxMB is the JVM heap memory maximum, in MiB.
	HeapMaxMB int64 `json:"heapMaxMB"`
	// HeapPercent is HeapUsedMB / HeapMaxMB * 100, clamped to [0,100].
	HeapPercent int `json:"heapPercent"`
}

// VertexDetail holds per-vertex summary info from GET /jobs/{id}.
type VertexDetail struct {
	Name        string `json:"name"`
	Parallelism int64  `json:"parallelism"`
	Status      string `json:"status"`
}

// JobMetricsSummary holds aggregated per-job metrics for the UI metrics panel.
type JobMetricsSummary struct {
	// Job identity / configuration (from /jobs/{id})
	JobID           string         `json:"jobId,omitempty"`
	JobName         string         `json:"jobName,omitempty"`
	JobStartTime    int64          `json:"jobStartTime,omitempty"` // Unix ms
	JobDurationMs   int64          `json:"jobDurationMs,omitempty"`
	MaxParallelism  int64          `json:"maxParallelism,omitempty"`
	Vertices        []VertexDetail `json:"vertices,omitempty"`
	// Checkpoint metrics (from /jobs/{id}/metrics)
	LastCheckpointDurationMs int64 `json:"lastCheckpointDurationMs"`
	LastCheckpointSizeBytes  int64 `json:"lastCheckpointSizeBytes"`
	CompletedCheckpoints     int64 `json:"completedCheckpoints"`
	FailedCheckpoints        int64 `json:"failedCheckpoints"`
	NumRestarts              int64 `json:"numRestarts"`
	// Checkpoint counts (from /jobs/{id}/checkpoints)
	InProgressCheckpoints int64 `json:"inProgressCheckpoints"`
	RestoredCheckpoints   int64 `json:"restoredCheckpoints"`
	TotalCheckpoints      int64 `json:"totalCheckpoints"`
	// Latest completed checkpoint detail (from /jobs/{id}/checkpoints)
	LatestCheckpointID             int64 `json:"latestCheckpointId,omitempty"`
	LatestCheckpointCompletionTime int64 `json:"latestCheckpointCompletionTime,omitempty"` // Unix ms
	LatestCheckpointDurationMs     int64 `json:"latestCheckpointDurationMs,omitempty"`
	LatestCheckpointStateSizeBytes int64 `json:"latestCheckpointStateSizeBytes,omitempty"`
	LatestCheckpointFullSizeBytes  int64 `json:"latestCheckpointFullSizeBytes,omitempty"`
	// Throughput — direct per-second rates from /jobs/{id}/metrics (preferred)
	RecordsInPerSec  float64 `json:"recordsInPerSec"`
	RecordsOutPerSec float64 `json:"recordsOutPerSec"`
	BytesInPerSec    float64 `json:"bytesInPerSec"`
	BytesOutPerSec   float64 `json:"bytesOutPerSec"`
	// Throughput — cumulative totals from /jobs/{id} (used as fallback for charting)
	ReadRecords  int64 `json:"readRecords"`
	WriteRecords int64 `json:"writeRecords"`
	ReadBytes    int64 `json:"readBytes"`
	WriteBytes   int64 `json:"writeBytes"`
	// SampledAt is when these values were read (Unix ms), used by the frontend
	// to compute per-second rates by comparing consecutive samples.
	SampledAt int64 `json:"sampledAt"`
}

// JobSummary holds the fields returned by GET /jobs/overview that the observer needs.
type JobSummary struct {
	ID    string `json:"jid"`
	Name  string `json:"name"`
	State string `json:"state"` // e.g. "RUNNING", "FAILING", "FINISHED"
}

// HTTPClient implements Client using the standard net/http package.
type HTTPClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewHTTPClient constructs an HTTPClient with the given base URL and a
// 5-second default HTTP timeout.
func NewHTTPClient(baseURL string) Client {
	return &HTTPClient{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// Jobs implements Client by calling GET /jobs/overview on the Flink REST API.
func (c *HTTPClient) Jobs(ctx context.Context) ([]JobSummary, error) {
	url := c.BaseURL + "/jobs/overview"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %d", url, resp.StatusCode)
	}
	var result struct {
		Jobs []JobSummary `json:"jobs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode jobs response: %w", err)
	}
	return result.Jobs, nil
}

// TaskManagers implements Client by calling GET /taskmanagers on the Flink REST API.
func (c *HTTPClient) TaskManagers(ctx context.Context) (int, error) {
	url := c.BaseURL + "/taskmanagers"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("GET %s: unexpected status %d", url, resp.StatusCode)
	}
	var result struct {
		TaskManagers []struct{} `json:"taskmanagers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode taskmanagers response: %w", err)
	}
	return len(result.TaskManagers), nil
}

// TaskManagerMetrics implements Client. It calls GET /taskmanagers to list TM
// IDs, then fetches CPU load and JVM heap metrics for each TM via
// GET /taskmanagers/{id}/metrics. Results are correlated to Kubernetes pods by
// the pod IP extracted from the TM id (format "{ip}:{port}-{suffix}").
func (c *HTTPClient) TaskManagerMetrics(ctx context.Context) ([]TMMetrics, error) {
	url := c.BaseURL + "/taskmanagers"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %d", url, resp.StatusCode)
	}

	var listResp struct {
		TaskManagers []struct {
			ID string `json:"id"`
		} `json:"taskmanagers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("decode taskmanagers response: %w", err)
	}

	result := make([]TMMetrics, 0, len(listResp.TaskManagers))
	for _, tm := range listResp.TaskManagers {
		m := fetchTMMetrics(ctx, c, tm.ID)
		result = append(result, m)
	}
	return result, nil
}

// fetchTMMetrics fetches CPU load and JVM heap metrics for a single TM.
// Errors are silently ignored — the returned TMMetrics will have CPULoad=-1
// and zero heap values to signal that data is unavailable.
func fetchTMMetrics(ctx context.Context, c *HTTPClient, tmID string) TMMetrics {
	m := TMMetrics{TMID: tmID, CPULoad: -1}

	// Extract pod IP from the TM id which has the form "{ip}:{port}-{suffix}".
	if idx := strings.IndexByte(tmID, ':'); idx > 0 {
		m.PodIP = tmID[:idx]
	}

	metricsURL := fmt.Sprintf("%s/taskmanagers/%s/metrics?get=Status.JVM.CPU.Load,Status.JVM.Memory.Heap.Used,Status.JVM.Memory.Heap.Max",
		c.BaseURL, tmID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metricsURL, nil)
	if err != nil {
		return m
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return m
	}
	defer resp.Body.Close()

	var metrics []struct {
		ID    string `json:"id"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
		return m
	}

	for _, entry := range metrics {
		switch entry.ID {
		case "Status.JVM.CPU.Load":
			var v float64
			if _, err := fmt.Sscanf(entry.Value, "%f", &v); err == nil {
				m.CPULoad = v
			}
		case "Status.JVM.Memory.Heap.Used":
			var v int64
			if _, err := fmt.Sscanf(entry.Value, "%d", &v); err == nil {
				m.HeapUsedMB = v / (1024 * 1024)
			}
		case "Status.JVM.Memory.Heap.Max":
			var v int64
			if _, err := fmt.Sscanf(entry.Value, "%d", &v); err == nil {
				m.HeapMaxMB = v / (1024 * 1024)
			}
		}
	}

	if m.HeapMaxMB > 0 {
		pct := int(m.HeapUsedMB * 100 / m.HeapMaxMB)
		if pct > 100 {
			pct = 100
		}
		m.HeapPercent = pct
	}
	return m
}

// JobMetrics implements Client by fetching checkpoint metrics from
// GET /jobs/{jobId}/metrics and cumulative throughput counters from
// GET /jobs/{jobId} (vertex-level read/write records summed across vertices).
func (c *HTTPClient) JobMetrics(ctx context.Context) (*JobMetricsSummary, error) {
	jobs, err := c.Jobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}

	var jobID string
	for _, j := range jobs {
		if j.State == "RUNNING" {
			jobID = j.ID
			break
		}
	}
	if jobID == "" {
		return nil, nil
	}

	summary := &JobMetricsSummary{SampledAt: time.Now().UnixMilli()}

	// Fetch checkpoint + restart counters and direct per-second throughput rates.
	// Rate metrics (numRecordsInPerSecond etc.) are queried with &agg=sum so
	// Flink aggregates across all subtasks in one request.
	metricsURL := c.BaseURL + "/jobs/" + jobID +
		"/metrics?get=lastCheckpointDuration,lastCheckpointSize," +
		"numberOfCompletedCheckpoints,numberOfFailedCheckpoints,numRestarts," +
		"numberOfInProgressCheckpoints,numberOfRestoredCheckpoints," +
		"numRecordsInPerSecond,numRecordsOutPerSecond," +
		"numBytesInPerSecond,numBytesOutPerSecond&agg=sum"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metricsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build metrics request: %w", err)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", metricsURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		var items []struct {
			ID    string `json:"id"`
			Value string `json:"value"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&items); err == nil {
			for _, item := range items {
				switch item.ID {
				case "lastCheckpointDuration":
					fmt.Sscanf(item.Value, "%d", &summary.LastCheckpointDurationMs)
				case "lastCheckpointSize":
					fmt.Sscanf(item.Value, "%d", &summary.LastCheckpointSizeBytes)
				case "numberOfCompletedCheckpoints":
					fmt.Sscanf(item.Value, "%d", &summary.CompletedCheckpoints)
				case "numberOfFailedCheckpoints":
					fmt.Sscanf(item.Value, "%d", &summary.FailedCheckpoints)
				case "numRestarts":
					fmt.Sscanf(item.Value, "%d", &summary.NumRestarts)
				case "numberOfInProgressCheckpoints":
					fmt.Sscanf(item.Value, "%d", &summary.InProgressCheckpoints)
				case "numberOfRestoredCheckpoints":
					fmt.Sscanf(item.Value, "%d", &summary.RestoredCheckpoints)
				case "numRecordsInPerSecond":
					fmt.Sscanf(item.Value, "%f", &summary.RecordsInPerSec)
				case "numRecordsOutPerSecond":
					fmt.Sscanf(item.Value, "%f", &summary.RecordsOutPerSec)
				case "numBytesInPerSecond":
					fmt.Sscanf(item.Value, "%f", &summary.BytesInPerSec)
				case "numBytesOutPerSecond":
					fmt.Sscanf(item.Value, "%f", &summary.BytesOutPerSec)
				}
			}
		}
	}

	// Fetch rich checkpoint detail: counts and latest completed checkpoint info.
	// This supersedes the partial data available from /metrics and provides
	// in-progress, restored, and total counts along with completion timestamps.
	c.fetchCheckpointDetail(ctx, jobID, summary)

	// Fetch job detail (name, start-time, vertices) and cumulative throughput.
	detailURL := c.BaseURL + "/jobs/" + jobID
	req2, err := http.NewRequestWithContext(ctx, http.MethodGet, detailURL, nil)
	if err != nil {
		return summary, nil
	}
	resp2, err := c.HTTPClient.Do(req2)
	if err != nil {
		return summary, nil
	}
	defer resp2.Body.Close()
	var vertexIDs []string
	if resp2.StatusCode == http.StatusOK {
		vertexIDs = c.decodeVertexThroughput(resp2.Body, summary)
	}

	// Fetch per-second rate metrics per vertex and sum across all vertices.
	// The vertex subtask endpoint (/vertices/{id}/subtasks/metrics?agg=sum) is
	// the only reliable way to get rate metrics in Ververica Platform.
	if len(vertexIDs) > 0 {
		c.fetchVertexRates(ctx, jobID, vertexIDs, summary)
	}

	return summary, nil
}

// fetchCheckpointDetail calls GET /jobs/{jobId}/checkpoints and populates the
// detailed checkpoint counts and latest-checkpoint fields on summary.
// Errors are silently swallowed — the caller already has partial data.
func (c *HTTPClient) fetchCheckpointDetail(ctx context.Context, jobID string, summary *JobMetricsSummary) {
	url := c.BaseURL + "/jobs/" + jobID + "/checkpoints"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}

	var raw struct {
		Counts struct {
			Restored   int64 `json:"restored"`
			Total      int64 `json:"total"`
			InProgress int64 `json:"in_progress"`
			Completed  int64 `json:"completed"`
			Failed     int64 `json:"failed"`
		} `json:"counts"`
		Latest struct {
			Completed *struct {
				ID                 int64 `json:"id"`
				LatestAckTimestamp int64 `json:"latest_ack_timestamp"`
				EndToEndDuration   int64 `json:"end_to_end_duration"`
				StateSize          int64 `json:"state_size"`
				FullSize           int64 `json:"full_size"`
			} `json:"completed"`
		} `json:"latest"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return
	}

	summary.RestoredCheckpoints = raw.Counts.Restored
	summary.TotalCheckpoints = raw.Counts.Total
	summary.InProgressCheckpoints = raw.Counts.InProgress
	// Overwrite CompletedCheckpoints and FailedCheckpoints with the richer
	// /checkpoints response when available; these shadow the /metrics values.
	if raw.Counts.Total > 0 {
		summary.CompletedCheckpoints = raw.Counts.Completed
		summary.FailedCheckpoints = raw.Counts.Failed
	}

	if cp := raw.Latest.Completed; cp != nil {
		summary.LatestCheckpointID = cp.ID
		summary.LatestCheckpointCompletionTime = cp.LatestAckTimestamp
		summary.LatestCheckpointDurationMs = cp.EndToEndDuration
		summary.LatestCheckpointStateSizeBytes = cp.StateSize
		summary.LatestCheckpointFullSizeBytes = cp.FullSize
	}
}

// decodeVertexThroughput reads the /jobs/{id} response body, extracts job
// configuration fields (name, start-time, vertices), sums cumulative
// read/write counters across all vertices into summary, and returns the list
// of vertex IDs so the caller can query per-second rate metrics per vertex.
func (c *HTTPClient) decodeVertexThroughput(body io.Reader, summary *JobMetricsSummary) []string {
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil
	}

	var detail struct {
		JID            string `json:"jid"`
		Name           string `json:"name"`
		StartTime      int64  `json:"start-time"`
		Duration       int64  `json:"duration"`
		MaxParallelism int64  `json:"maxParallelism"`
		Vertices       []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Parallelism int64  `json:"parallelism"`
			Status      string `json:"status"`
			ReadRecords  int64 `json:"read-records"`
			WriteRecords int64 `json:"write-records"`
			ReadBytes    int64 `json:"read-bytes"`
			WriteBytes   int64 `json:"write-bytes"`
		} `json:"vertices"`
	}
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&detail); err != nil {
		return nil
	}

	if detail.JID != "" {
		summary.JobID = detail.JID
	}
	if detail.Name != "" {
		summary.JobName = detail.Name
	}
	if detail.StartTime > 0 {
		summary.JobStartTime = detail.StartTime
	}
	if detail.Duration > 0 {
		summary.JobDurationMs = detail.Duration
	}
	if detail.MaxParallelism > 0 {
		summary.MaxParallelism = detail.MaxParallelism
	}

	var vertexIDs []string
	for _, v := range detail.Vertices {
		summary.ReadRecords += v.ReadRecords
		summary.WriteRecords += v.WriteRecords
		summary.ReadBytes += v.ReadBytes
		summary.WriteBytes += v.WriteBytes
		summary.Vertices = append(summary.Vertices, VertexDetail{
			Name:        v.Name,
			Parallelism: v.Parallelism,
			Status:      v.Status,
		})
		if v.ID != "" {
			vertexIDs = append(vertexIDs, v.ID)
		}
	}

	// VVP fallback for cumulative counters: "plan.nodes" with camelCase fields.
	if summary.ReadRecords == 0 && summary.WriteRecords == 0 {
		var vvpDetail struct {
			Plan struct {
				Nodes []struct {
					NumRecordsIn  int64 `json:"numRecordsIn"`
					NumRecordsOut int64 `json:"numRecordsOut"`
					NumBytesIn    int64 `json:"numBytesIn"`
					NumBytesOut   int64 `json:"numBytesOut"`
				} `json:"nodes"`
			} `json:"plan"`
		}
		if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&vvpDetail); err == nil {
			for _, n := range vvpDetail.Plan.Nodes {
				summary.ReadRecords += n.NumRecordsIn
				summary.WriteRecords += n.NumRecordsOut
				summary.ReadBytes += n.NumBytesIn
				summary.WriteBytes += n.NumBytesOut
			}
		}
	}

	return vertexIDs
}

// fetchVertexRates queries per-second throughput rates from the vertex subtask
// metrics endpoint for each vertex concurrently and sums the results into summary.
// This is the correct approach for Ververica Platform where job-level rate
// metrics are not aggregated but vertex subtask metrics are available.
func (c *HTTPClient) fetchVertexRates(ctx context.Context, jobID string, vertexIDs []string, summary *JobMetricsSummary) {
	const metricsParam = "numRecordsInPerSecond,numRecordsOutPerSecond,numBytesInPerSecond,numBytesOutPerSecond"

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)

	for _, vid := range vertexIDs {
		vid := vid
		wg.Add(1)
		go func() {
			defer wg.Done()

			url := fmt.Sprintf("%s/jobs/%s/vertices/%s/subtasks/metrics?get=%s&agg=sum",
				c.BaseURL, jobID, vid, metricsParam)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return
			}
			resp, err := c.HTTPClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return
			}
			var items []struct {
				ID  string  `json:"id"`
				Sum float64 `json:"sum"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
				return
			}

			mu.Lock()
			defer mu.Unlock()
			for _, item := range items {
				switch item.ID {
				case "numRecordsInPerSecond":
					summary.RecordsInPerSec += item.Sum
				case "numRecordsOutPerSecond":
					summary.RecordsOutPerSec += item.Sum
				case "numBytesInPerSecond":
					summary.BytesInPerSec += item.Sum
				case "numBytesOutPerSecond":
					summary.BytesOutPerSec += item.Sum
				}
			}
		}()
	}
	wg.Wait()
}
