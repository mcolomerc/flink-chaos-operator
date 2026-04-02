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

package flinkrest

import (
	"context"
	"fmt"
	"sync"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/interfaces"
)

// Observer implements interfaces.Observer using the Flink REST API.
// It reports AllReplacementsReady=true only when at least one job is in
// RUNNING state and the TaskManager count is >= the count before injection.
//
// When FlinkRest observation is not enabled on the run, the observer
// returns AllReplacementsReady=true immediately so it does not block
// recovery signalling from other observers.
//
// The Observer caches HTTP clients by endpoint URL to avoid creating a new
// TCP connection pool on every 5-second poll cycle.
type Observer struct {
	// NewClient is a factory that creates a Client for the given base URL.
	// Use NewHTTPClient in production; inject a stub in tests.
	NewClient func(baseURL string) Client

	mu    sync.Mutex
	cache map[string]Client
}

// cachedClient returns the cached Client for endpoint, creating and caching
// one via NewClient when no entry exists yet.
func (o *Observer) cachedClient(endpoint string) Client {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.cache == nil {
		o.cache = make(map[string]Client)
	}
	if c, ok := o.cache[endpoint]; ok {
		return c
	}
	c := o.NewClient(endpoint)
	o.cache[endpoint] = c
	return c
}

// Observe polls the Flink REST API for job state and TaskManager count.
func (o *Observer) Observe(ctx context.Context, run *v1alpha1.ChaosRun, target *interfaces.ResolvedTarget) (*interfaces.ObservationResult, error) {
	result := &interfaces.ObservationResult{}

	// Skip if REST observation not enabled for this run.
	if !run.Spec.Observe.FlinkRest.Enabled || run.Spec.Observe.FlinkRest.Endpoint == "" {
		result.AllReplacementsReady = true // don't block recovery signal
		return result, nil
	}

	client := o.cachedClient(run.Spec.Observe.FlinkRest.Endpoint)

	jobs, err := client.Jobs(ctx)
	if err != nil {
		return result, fmt.Errorf("flink REST jobs query: %w", err)
	}

	// Find any job in RUNNING state. For simplicity at this stage, one
	// running job is sufficient — multi-job sessions are a future concern.
	for _, j := range jobs {
		if j.State == "RUNNING" {
			result.FlinkJobState = j.State
			break
		}
	}
	if result.FlinkJobState == "" && len(jobs) > 0 {
		result.FlinkJobState = jobs[0].State
	}

	tmCount, err := client.TaskManagers(ctx)
	if err != nil {
		return result, fmt.Errorf("flink REST taskmanagers query: %w", err)
	}

	result.TMCountAfter = tmCount

	// Baseline comes from the status recorded before injection.
	baseline := 0
	if run.Status.Observation != nil {
		baseline = run.Status.Observation.TaskManagerCountBefore
	}

	result.ReplacementObserved = tmCount > 0
	result.AllReplacementsReady = result.FlinkJobState == "RUNNING" && tmCount >= baseline

	return result, nil
}
