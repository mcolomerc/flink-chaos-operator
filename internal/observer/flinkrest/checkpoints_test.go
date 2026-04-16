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

package flinkrest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flink-chaos-operator/internal/observer/flinkrest"
)

// checkpointStub is a minimal Client stub that only exercises the Checkpoints
// method. Jobs and TaskManagers return zero values so the struct satisfies the
// full interface without noise in checkpoint-focused tests.
type checkpointStub struct {
	summary *flinkrest.CheckpointSummary
	err     error
}

func (s *checkpointStub) Jobs(_ context.Context) ([]flinkrest.JobSummary, error) {
	return nil, nil
}

func (s *checkpointStub) TaskManagers(_ context.Context) (int, error) {
	return 0, nil
}

func (s *checkpointStub) Checkpoints(_ context.Context) (*flinkrest.CheckpointSummary, error) {
	return s.summary, s.err
}

func (s *checkpointStub) TaskManagerMetrics(_ context.Context) ([]flinkrest.TMMetrics, error) {
	return nil, nil
}

func (s *checkpointStub) JobMetrics(_ context.Context) (*flinkrest.JobMetricsSummary, error) {
	return nil, nil
}

func msAgo(d time.Duration) int64 {
	return time.Now().Add(-d).UnixMilli()
}

func TestIsCheckpointStable_RecentCheckpoint(t *testing.T) {
	stub := &checkpointStub{
		summary: &flinkrest.CheckpointSummary{
			LatestCompleted: &flinkrest.CheckpointDetail{
				TriggerTimestamp: msAgo(10 * time.Second),
				Status:           "COMPLETED",
			},
		},
	}

	stable, err := flinkrest.IsCheckpointStable(context.Background(), stub, 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stable {
		t.Error("expected stable=true for a checkpoint completed 10s ago with window=60s")
	}
}

func TestIsCheckpointStable_StaleCheckpoint(t *testing.T) {
	stub := &checkpointStub{
		summary: &flinkrest.CheckpointSummary{
			LatestCompleted: &flinkrest.CheckpointDetail{
				TriggerTimestamp: msAgo(120 * time.Second),
				Status:           "COMPLETED",
			},
		},
	}

	stable, err := flinkrest.IsCheckpointStable(context.Background(), stub, 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stable {
		t.Error("expected stable=false for a checkpoint completed 120s ago with window=60s")
	}
}

func TestIsCheckpointStable_NoJobs(t *testing.T) {
	stub := &checkpointStub{summary: nil}

	stable, err := flinkrest.IsCheckpointStable(context.Background(), stub, 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stable {
		t.Error("expected stable=false when no summary is returned")
	}
}

func TestIsCheckpointStable_FailedCheckpoint(t *testing.T) {
	stub := &checkpointStub{
		summary: &flinkrest.CheckpointSummary{
			LatestCompleted: &flinkrest.CheckpointDetail{
				TriggerTimestamp: msAgo(5 * time.Second),
				Status:           "FAILED",
			},
		},
	}

	stable, err := flinkrest.IsCheckpointStable(context.Background(), stub, 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stable {
		t.Error("expected stable=false when latest checkpoint status is FAILED")
	}
}

func TestIsCheckpointStable_ClientError(t *testing.T) {
	wantErr := errors.New("flink unavailable")
	stub := &checkpointStub{err: wantErr}

	stable, err := flinkrest.IsCheckpointStable(context.Background(), stub, 60)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected error to wrap %v, got %v", wantErr, err)
	}
	if stable {
		t.Error("expected stable=false on client error")
	}
}
