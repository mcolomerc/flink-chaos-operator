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

// Package stress builds stress-ng command arguments for ephemeral container injection.
// It has zero Kubernetes dependencies and operates purely on Go values.
package stress

import (
	"fmt"

	v1alpha1 "github.com/flink-chaos-operator/api/v1alpha1"
)

// Args returns the stress-ng command arguments for the given ResourceExhaustionSpec.
// The returned slice is suitable for use as an ephemeral container Command field.
func Args(spec *v1alpha1.ResourceExhaustionSpec) []string {
	switch spec.Mode {
	case v1alpha1.ResourceExhaustionModeCPU:
		return []string{
			"stress-ng",
			"--cpu", fmt.Sprintf("%d", spec.Workers),
			"--timeout", durationArg(spec),
		}
	case v1alpha1.ResourceExhaustionModeMemory:
		return []string{
			"stress-ng",
			"--vm", fmt.Sprintf("%d", spec.Workers),
			"--vm-bytes", fmt.Sprintf("%d%%", spec.MemoryPercent),
			"--timeout", durationArg(spec),
		}
	default:
		// Fallback: single CPU worker with the configured (or default) timeout.
		return []string{
			"stress-ng",
			"--cpu", "1",
			"--timeout", durationArg(spec),
		}
	}
}

// durationArg converts the spec Duration to a stress-ng timeout string in
// whole seconds (e.g. "60s"). Falls back to "60s" when Duration is nil.
func durationArg(spec *v1alpha1.ResourceExhaustionSpec) string {
	if spec.Duration == nil {
		return "60s"
	}
	return fmt.Sprintf("%.0fs", spec.Duration.Seconds())
}
