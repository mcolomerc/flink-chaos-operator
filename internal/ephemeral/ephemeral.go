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

// Package ephemeral provides shared helpers for naming and inspecting
// Kubernetes ephemeral containers injected by chaos scenario drivers.
package ephemeral

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// ContainerName generates a DNS-label-safe ephemeral container name from the
// given prefix, ChaosRun name, and pod index. The total length is capped at
// 63 characters per Kubernetes naming constraints.
func ContainerName(prefix, runName string, index int) string {
	const maxLen = 63

	suffix := fmt.Sprintf("-%d", index)

	maxName := maxLen - len(prefix) - len(suffix)
	if maxName < 0 {
		maxName = 0
	}

	truncated := runName
	if len(truncated) > 20 {
		truncated = truncated[:20]
	}
	if len(truncated) > maxName {
		truncated = truncated[:maxName]
	}

	name := prefix + truncated + suffix
	if len(name) > maxLen {
		name = name[:maxLen]
	}
	return name
}

// FindContainerState returns the ContainerState for the named ephemeral
// container in the pod's status, or nil when the container is not found.
func FindContainerState(pod *corev1.Pod, containerName string) *corev1.ContainerState {
	for i := range pod.Status.EphemeralContainerStatuses {
		s := &pod.Status.EphemeralContainerStatuses[i]
		if s.Name == containerName {
			return &s.State
		}
	}
	return nil
}
