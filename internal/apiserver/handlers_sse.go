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
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/flink-chaos-operator/api/v1alpha1"
)

const (
	ssePollInterval      = 3 * time.Second
	sseHeartbeatInterval = 15 * time.Second
)

// sseEvent is the envelope written to the SSE stream.
type sseEvent struct {
	Type     string `json:"type"`
	Name     string `json:"name,omitempty"`
	Phase    string `json:"phase,omitempty"`
	Scenario string `json:"scenario,omitempty"`
}

// sseHandler handles GET /api/events as a Server-Sent Events stream.
//
// The handler streams real-time ChaosRun phase changes to connected clients.
// It polls the Kubernetes API every 3 seconds and emits heartbeats every 15s.
// The stream terminates when the client disconnects.
func sseHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming not supported")
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		// Send the initial connected event immediately.
		if err := writeSSEEvent(w, sseEvent{Type: "connected"}); err != nil {
			return
		}
		flusher.Flush()

		ctx := r.Context()

		// snapshot maps run name → phase for change detection.
		snapshot := make(map[string]v1alpha1.RunPhase)

		pollTicker := time.NewTicker(ssePollInterval)
		heartbeatTicker := time.NewTicker(sseHeartbeatInterval)
		defer pollTicker.Stop()
		defer heartbeatTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case <-heartbeatTicker.C:
				if err := writeSSEEvent(w, sseEvent{Type: "heartbeat"}); err != nil {
					return
				}
				flusher.Flush()

			case <-pollTicker.C:
				runList := &v1alpha1.ChaosRunList{}
				if err := cfg.Client.List(ctx, runList, client.InNamespace(cfg.Namespace)); err != nil {
					slog.Error("sse: list chaosruns", "error", err)
					continue
				}

				// Detect new runs and phase changes.
				seen := make(map[string]struct{}, len(runList.Items))
				for i := range runList.Items {
					run := &runList.Items[i]
					seen[run.Name] = struct{}{}
					prev, exists := snapshot[run.Name]
					if !exists || prev != run.Status.Phase {
						snapshot[run.Name] = run.Status.Phase
						event := sseEvent{
							Type:     "chaosrun_update",
							Name:     run.Name,
							Phase:    string(run.Status.Phase),
							Scenario: string(run.Spec.Scenario.Type),
						}
						if err := writeSSEEvent(w, event); err != nil {
							return
						}
						flusher.Flush()
					}
				}

				// Remove stale entries from the snapshot.
				for name := range snapshot {
					if _, ok := seen[name]; !ok {
						delete(snapshot, name)
					}
				}
			}
		}
	}
}

// writeSSEEvent serialises ev as JSON and writes it as an SSE data line.
func writeSSEEvent(w http.ResponseWriter, ev sseEvent) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal sse event: %w", err)
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}
