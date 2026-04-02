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

// Package composite implements an Observer that delegates to multiple child
// observers and merges their results. AllReplacementsReady is true only when
// all child observers agree.
package composite

import (
	"context"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/interfaces"
)

// Observer delegates to an ordered list of child observers and combines results.
type Observer struct {
	Children []interfaces.Observer
}

// Observe calls each child observer in order and merges results.
//
// Merging rules:
//   - AllReplacementsReady requires all children to return true.
//   - ReplacementObserved is true when any child returns true.
//   - TMCountBefore and TMCountAfter are taken from the last child that sets
//     a non-zero value.
//   - FlinkJobState and CheckpointStatus come from the last child that sets them.
//   - Errors from any child are returned immediately (fail-fast).
//
// An empty Children slice returns AllReplacementsReady=true vacuously.
func (o *Observer) Observe(ctx context.Context, run *v1alpha1.ChaosRun, target *interfaces.ResolvedTarget) (*interfaces.ObservationResult, error) {
	merged := &interfaces.ObservationResult{AllReplacementsReady: true}
	for _, child := range o.Children {
		r, err := child.Observe(ctx, run, target)
		if err != nil {
			return &interfaces.ObservationResult{}, err
		}
		if !r.AllReplacementsReady {
			merged.AllReplacementsReady = false
		}
		if r.ReplacementObserved {
			merged.ReplacementObserved = true
		}
		if r.TMCountBefore > 0 {
			merged.TMCountBefore = r.TMCountBefore
		}
		if r.TMCountAfter > 0 {
			merged.TMCountAfter = r.TMCountAfter
		}
		if r.FlinkJobState != "" {
			merged.FlinkJobState = r.FlinkJobState
		}
		if r.CheckpointStatus != "" {
			merged.CheckpointStatus = r.CheckpointStatus
		}
	}
	return merged, nil
}
