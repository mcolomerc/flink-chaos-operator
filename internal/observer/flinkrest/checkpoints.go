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

// Package flinkrest — checkpoint polling logic for pre-inject stability check.
package flinkrest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// CheckpointSummary holds the fields from GET /jobs/{jobId}/checkpoints that
// the stability check needs.
type CheckpointSummary struct {
	// LatestCompleted holds the most recently completed checkpoint, or nil.
	LatestCompleted *CheckpointDetail `json:"latest"`
}

// CheckpointDetail holds detail about one checkpoint.
type CheckpointDetail struct {
	// TriggerTimestamp is when the checkpoint was triggered (Unix ms).
	TriggerTimestamp int64  `json:"trigger_timestamp"`
	Status           string `json:"status"` // "COMPLETED" or "FAILED"
}

// Checkpoints implements Client by first finding the first RUNNING job via
// GET /jobs/overview, then calling GET /jobs/{jobId}/checkpoints.
// Returns nil, nil when no running jobs exist.
func (c *HTTPClient) Checkpoints(ctx context.Context) (*CheckpointSummary, error) {
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

	url := c.BaseURL + "/jobs/" + jobID + "/checkpoints"
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

	// The Flink /checkpoints response wraps the latest completed checkpoint
	// under the "latest" key as an object with a "completed" sub-key.
	// We decode only the fields we need.
	var raw struct {
		Latest struct {
			Completed *CheckpointDetail `json:"completed"`
		} `json:"latest"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode checkpoints response: %w", err)
	}

	return &CheckpointSummary{LatestCompleted: raw.Latest.Completed}, nil
}

// IsCheckpointStable returns true when the most recently completed checkpoint
// for the first running job was completed within windowSeconds.
func IsCheckpointStable(ctx context.Context, client Client, windowSeconds int32) (bool, error) {
	summary, err := client.Checkpoints(ctx)
	if err != nil {
		return false, fmt.Errorf("checkpoints query: %w", err)
	}
	if summary == nil || summary.LatestCompleted == nil {
		return false, nil
	}
	completed := summary.LatestCompleted
	if completed.Status != "COMPLETED" {
		return false, nil
	}
	age := time.Since(time.UnixMilli(completed.TriggerTimestamp))
	return age <= time.Duration(windowSeconds)*time.Second, nil
}
