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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client is the interface for querying the Flink REST API.
// It is separated from the observer to allow test stubbing.
type Client interface {
	// Jobs returns the list of jobs and their states from GET /jobs/overview.
	Jobs(ctx context.Context) ([]JobSummary, error)
	// TaskManagers returns the count of registered TaskManagers from GET /taskmanagers.
	TaskManagers(ctx context.Context) (int, error)
	// Checkpoints returns checkpoint summary for the first running job.
	// Returns nil, nil when no jobs exist.
	Checkpoints(ctx context.Context) (*CheckpointSummary, error)
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
