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
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Config holds the dependencies and configuration for the API server.
type Config struct {
	// Client is the controller-runtime Kubernetes client.
	Client client.Client
	// Namespace is the Kubernetes namespace to scope all queries to.
	Namespace string
	// FlinkEndpoint is an optional explicit Flink REST API base URL.
	// When empty the server auto-discovers the JobManager service.
	FlinkEndpoint string
	// Port is the TCP port the server will listen on.
	Port int
	// StaticFS is the embedded frontend filesystem served at /.
	// When nil, no static files are served (Vite dev-server handles it locally).
	StaticFS fs.FS
}

// NewServer constructs an *http.Server with all routes registered.
func NewServer(cfg Config) *http.Server {
	mux := http.NewServeMux()

	// Health check — used by Kubernetes liveness/readiness probes.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Deployments list (drives header job-switcher badges)
	mux.HandleFunc("GET /api/deployments", listDeploymentsHandler(cfg))

	// Topology
	mux.HandleFunc("GET /api/topology", topologyHandler(cfg))

	// ChaosRun resources
	mux.HandleFunc("GET /api/chaosruns", listChaosRunsHandler(cfg))
	mux.HandleFunc("GET /api/chaosruns/{name}", getChaosRunHandler(cfg))
	mux.HandleFunc("POST /api/chaosruns", createChaosRunHandler(cfg))
	mux.HandleFunc("PATCH /api/chaosruns/{name}/abort", abortChaosRunHandler(cfg))
	mux.HandleFunc("DELETE /api/chaosruns/{name}", deleteChaosRunHandler(cfg))

	// Flink REST proxy
	mux.HandleFunc("GET /api/flink/jobs", flinkJobsHandler(cfg))
	mux.HandleFunc("GET /api/flink/taskmanagers", flinkTaskManagersHandler(cfg))
	mux.HandleFunc("GET /api/flink/checkpoints", flinkCheckpointsHandler(cfg))
	mux.HandleFunc("GET /api/flink/jobmetrics", flinkJobMetricsHandler(cfg))
	mux.HandleFunc("GET /api/flink/tmmetrics", flinkTMMetricsHandler(cfg))

	// Server-Sent Events stream
	mux.HandleFunc("GET /api/events", sseHandler(cfg))

	// Embedded React frontend — registered last so /api/* routes take priority.
	if cfg.StaticFS != nil {
		mux.Handle("/", http.FileServerFS(cfg.StaticFS))
	}

	return &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      corsMiddleware(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // disabled: SSE connections are long-lived and would be killed by a finite write timeout
		IdleTimeout:  60 * time.Second,
	}
}

// corsMiddleware adds CORS headers to every response and handles OPTIONS
// preflight requests with 204 No Content.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
