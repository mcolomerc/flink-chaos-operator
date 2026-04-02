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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/observer/flinkrest"
)

// stubClient implements flinkrest.Client for testing.
type stubClient struct {
	jobs        []flinkrest.JobSummary
	jobsErr     error
	tmCount     int
	tmErr       error
	checkpoints *flinkrest.CheckpointSummary
	checkErr    error
}

func (s *stubClient) Jobs(_ context.Context) ([]flinkrest.JobSummary, error) {
	return s.jobs, s.jobsErr
}

func (s *stubClient) TaskManagers(_ context.Context) (int, error) {
	return s.tmCount, s.tmErr
}

func (s *stubClient) Checkpoints(_ context.Context) (*flinkrest.CheckpointSummary, error) {
	return s.checkpoints, s.checkErr
}

// newObserver builds an Observer that always returns the given stub client,
// regardless of base URL.
func newObserver(stub *stubClient) *flinkrest.Observer {
	return &flinkrest.Observer{
		NewClient: func(_ string) flinkrest.Client {
			return stub
		},
	}
}

// runWithFlinkRest builds a minimal ChaosRun with Flink REST enabled.
func runWithFlinkRest(baseline int) *v1alpha1.ChaosRun {
	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{Name: "test-run", Namespace: "default"},
		Spec: v1alpha1.ChaosRunSpec{
			Observe: v1alpha1.ObserveSpec{
				FlinkRest: v1alpha1.FlinkRestObserve{
					Enabled:  true,
					Endpoint: "http://flink-jobmanager:8081",
				},
			},
		},
	}
	if baseline > 0 {
		run.Status.Observation = &v1alpha1.ObservationStatus{
			TaskManagerCountBefore: baseline,
		}
	}
	return run
}

func TestObserver_JobRunning_TMCountRecovered(t *testing.T) {
	stub := &stubClient{
		jobs:    []flinkrest.JobSummary{{ID: "abc", Name: "myJob", State: "RUNNING"}},
		tmCount: 3,
	}
	obs := newObserver(stub)
	run := runWithFlinkRest(3)

	result, err := obs.Observe(context.Background(), run, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.AllReplacementsReady {
		t.Error("expected AllReplacementsReady=true, got false")
	}
	if result.FlinkJobState != "RUNNING" {
		t.Errorf("expected FlinkJobState=RUNNING, got %q", result.FlinkJobState)
	}
	if result.TMCountAfter != 3 {
		t.Errorf("expected TMCountAfter=3, got %d", result.TMCountAfter)
	}
	if !result.ReplacementObserved {
		t.Error("expected ReplacementObserved=true, got false")
	}
}

func TestObserver_NoRunningJob(t *testing.T) {
	stub := &stubClient{
		jobs:    []flinkrest.JobSummary{{ID: "abc", Name: "myJob", State: "FAILING"}},
		tmCount: 3,
	}
	obs := newObserver(stub)
	run := runWithFlinkRest(3)

	result, err := obs.Observe(context.Background(), run, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AllReplacementsReady {
		t.Error("expected AllReplacementsReady=false, got true")
	}
	if result.FlinkJobState != "FAILING" {
		t.Errorf("expected FlinkJobState=FAILING, got %q", result.FlinkJobState)
	}
}

func TestObserver_JobsError(t *testing.T) {
	stub := &stubClient{
		jobsErr: errors.New("connection refused"),
	}
	obs := newObserver(stub)
	run := runWithFlinkRest(0)

	result, err := obs.Observe(context.Background(), run, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result.AllReplacementsReady {
		t.Error("expected AllReplacementsReady=false when error occurs")
	}
}

func TestObserver_TMCountBelowBaseline(t *testing.T) {
	stub := &stubClient{
		jobs:    []flinkrest.JobSummary{{ID: "abc", Name: "myJob", State: "RUNNING"}},
		tmCount: 2,
	}
	obs := newObserver(stub)
	// baseline=3, tmCount=2 — not yet recovered
	run := runWithFlinkRest(3)

	result, err := obs.Observe(context.Background(), run, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AllReplacementsReady {
		t.Error("expected AllReplacementsReady=false when TM count below baseline")
	}
	if result.FlinkJobState != "RUNNING" {
		t.Errorf("expected FlinkJobState=RUNNING, got %q", result.FlinkJobState)
	}
	if !result.ReplacementObserved {
		t.Error("expected ReplacementObserved=true (tmCount > 0)")
	}
}

func TestObserver_FlinkRestDisabled(t *testing.T) {
	// Stub that would fail if called — ensures it is never invoked.
	stub := &stubClient{
		jobsErr: errors.New("should not be called"),
	}
	obs := newObserver(stub)

	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{Name: "test-run", Namespace: "default"},
		Spec: v1alpha1.ChaosRunSpec{
			Observe: v1alpha1.ObserveSpec{
				FlinkRest: v1alpha1.FlinkRestObserve{
					Enabled: false,
				},
			},
		},
	}

	result, err := obs.Observe(context.Background(), run, nil)
	if err != nil {
		t.Fatalf("unexpected error when disabled: %v", err)
	}
	if !result.AllReplacementsReady {
		t.Error("expected AllReplacementsReady=true when REST observation is disabled")
	}
}
