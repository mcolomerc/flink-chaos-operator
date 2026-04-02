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

// Package podutil provides shared helpers for pod eligibility and component
// role classification used by the resolver implementations.
package podutil

import (
	corev1 "k8s.io/api/core/v1"
)

// IsPodEligible reports whether a pod should be considered by a resolver.
// Terminating pods (DeletionTimestamp set) and pods in phases other than
// Running or Pending are excluded.
func IsPodEligible(pod *corev1.Pod) bool {
	if pod.DeletionTimestamp != nil {
		return false
	}
	return pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending
}

// ComponentRole extracts the component role from a pod's labels, checking
// all known label key conventions used by the Flink Kubernetes Operator,
// Ververica Platform, and the standard Kubernetes app label convention.
// Returns an empty string when no recognised label is set.
func ComponentRole(labels map[string]string) string {
	for _, key := range []string{
		"component",
		"app.kubernetes.io/component",
		"flink.apache.org/component",
		"vvp.io/component",
	} {
		if v, ok := labels[key]; ok {
			return v
		}
	}
	return ""
}
