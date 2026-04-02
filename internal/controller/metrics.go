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

// Package controller implements the ChaosRun reconciler and supporting types.
package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Operator-level Prometheus metrics. These describe the chaos tool's own
// behaviour, not the health of the Flink workloads it targets.
var (
	// RunsTotal counts ChaosRun reconciliations partitioned by scenario,
	// lifecycle phase, and verdict. It is incremented once per terminal
	// transition (Completed, Aborted, Failed).
	RunsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "fchaos_runs_total",
		Help: "Total number of ChaosRun reconciliations by scenario, phase, and verdict.",
	}, []string{"scenario", "phase", "verdict"})

	// RunDurationSeconds measures the wall-clock duration from the moment a run
	// leaves Pending until it enters a terminal phase.
	RunDurationSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "fchaos_run_duration_seconds",
		Help:    "Duration of ChaosRun executions from start to terminal phase.",
		Buckets: prometheus.DefBuckets,
	}, []string{"scenario", "verdict"})

	// InjectionsTotal counts the number of chaos injections that were
	// performed, partitioned by scenario type.
	InjectionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "fchaos_injections_total",
		Help: "Total number of chaos injections performed by scenario.",
	}, []string{"scenario"})

	// AbortRequestsTotal counts the total number of abort requests received
	// across all runs.
	AbortRequestsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "fchaos_abort_requests_total",
		Help: "Total number of abort requests received.",
	})

	// RecoveryObservedTotal counts the number of times workload recovery was
	// confirmed by the observer after injection.
	RecoveryObservedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "fchaos_recovery_observed_total",
		Help: "Total number of times workload recovery was observed after injection.",
	})
)

func init() {
	metrics.Registry.MustRegister(
		RunsTotal,
		RunDurationSeconds,
		InjectionsTotal,
		AbortRequestsTotal,
		RecoveryObservedTotal,
	)
}
