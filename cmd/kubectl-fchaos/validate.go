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

package main

import (
	"fmt"
	"strings"

	v1alpha1 "github.com/flink-chaos-operator/api/v1alpha1"
)

// validNetworkTargets lists all accepted values for --network-target.
var validNetworkTargets = []string{"TMtoTM", "TMtoJM", "TMtoCheckpoint", "TMtoExternal"}

// validNetworkDirections lists all accepted values for --direction.
var validNetworkDirections = []string{"Ingress", "Egress", "Both"}

// validateNetworkTarget returns an error when s is not a known NetworkTarget value.
// Matching is case-insensitive so that "tmtojm" and "TMtoJM" are both accepted.
func validateNetworkTarget(s string) error {
	for _, v := range validNetworkTargets {
		if strings.EqualFold(s, v) {
			return nil
		}
	}
	return fmt.Errorf("invalid --network-target %q; valid values: %s",
		s, strings.Join(validNetworkTargets, ", "))
}

// validateNetworkDirection returns an error when s is not a known NetworkDirection value.
// Matching is case-insensitive so that "both" and "Both" are both accepted.
func validateNetworkDirection(s string) error {
	for _, v := range validNetworkDirections {
		if strings.EqualFold(s, v) {
			return nil
		}
	}
	return fmt.Errorf("invalid --direction %q; valid values: %s",
		s, strings.Join(validNetworkDirections, ", "))
}

// normaliseNetworkTarget returns the canonical casing for a network target value.
// The caller must have already validated s with validateNetworkTarget.
func normaliseNetworkTarget(s string) string {
	for _, v := range validNetworkTargets {
		if strings.EqualFold(s, v) {
			return v
		}
	}
	return s
}

// normaliseNetworkDirection returns the canonical casing for a network direction value.
// The caller must have already validated s with validateNetworkDirection.
func normaliseNetworkDirection(s string) string {
	for _, v := range validNetworkDirections {
		if strings.EqualFold(s, v) {
			return v
		}
	}
	return s
}

// validateResourceExhaustionMode returns an error if mode is not CPU or Memory.
// Matching is case-sensitive to match the enum values in the API type.
func validateResourceExhaustionMode(mode string) error {
	switch v1alpha1.ResourceExhaustionMode(mode) {
	case v1alpha1.ResourceExhaustionModeCPU, v1alpha1.ResourceExhaustionModeMemory:
		return nil
	default:
		return fmt.Errorf("resourceExhaustion mode %q is not supported; must be CPU or Memory", mode)
	}
}
