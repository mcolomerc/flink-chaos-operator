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

package v1alpha1

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// bandwidthRe validates tc tbf rate strings: a positive integer followed by
// kbit, mbit, gbit, or tbit (case-insensitive).
var bandwidthRe = regexp.MustCompile(`(?i)^[1-9][0-9]*(kbit|mbit|gbit|tbit)$`)

// Validate performs semantic validation on a ChaosRun. It returns a combined
// error listing all validation failures found, so callers receive the full
// picture in a single pass. It is intentionally a plain function rather than
// a webhook so the controller can call it during reconciliation.
func Validate(run *ChaosRun) error {
	var errs []error

	if err := validateTargetSpec(run.Spec.Target); err != nil {
		errs = append(errs, err)
	}

	if err := validateScenarioSpec(run.Spec.Scenario); err != nil {
		errs = append(errs, err)
	}

	if err := validateObserveSpec(run.Spec.Observe); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// validateTargetSpec checks that the TargetSpec fields are consistent with the
// chosen TargetType.
func validateTargetSpec(t TargetSpec) error {
	var errs []error

	switch t.Type {
	case TargetFlinkDeployment:
		if t.Name == "" {
			errs = append(errs, fmt.Errorf("target.name is required when target.type is %s", TargetFlinkDeployment))
		}

	case TargetVervericaDeployment:
		if t.DeploymentID == "" && t.DeploymentName == "" {
			errs = append(errs, fmt.Errorf(
				"at least one of target.deploymentId or target.deploymentName is required when target.type is %s",
				TargetVervericaDeployment,
			))
		}

	case TargetPodSelector:
		if t.Selector == nil || (len(t.Selector.MatchLabels) == 0 && len(t.Selector.MatchExpressions) == 0) {
			errs = append(errs, fmt.Errorf(
				"target.selector with at least one matchLabel or matchExpression is required when target.type is %s",
				TargetPodSelector,
			))
		}

	default:
		errs = append(errs, fmt.Errorf("target.type %q is not supported", t.Type))
	}

	return errors.Join(errs...)
}

// validateScenarioSpec checks that the scenario fields are consistent with the
// chosen ScenarioType.
func validateScenarioSpec(s ScenarioSpec) error {
	var errs []error

	switch s.Type {
	case ScenarioTaskManagerPodKill, "":
		// An empty type will be defaulted to TaskManagerPodKill; validate selection and action.
		sel := s.Selection

		switch sel.Mode {
		case SelectionModeRandom, "":
			// An empty mode will be defaulted to Random; validate as Random.
			if sel.Count < 1 {
				errs = append(errs, fmt.Errorf(
					"scenario.selection.count must be >= 1 when scenario.selection.mode is %s (got %d)",
					SelectionModeRandom, sel.Count,
				))
			}

		case SelectionModeExplicit:
			if len(sel.PodNames) == 0 {
				errs = append(errs, fmt.Errorf(
					"scenario.selection.podNames must be non-empty when scenario.selection.mode is %s",
					SelectionModeExplicit,
				))
			}

		default:
			errs = append(errs, fmt.Errorf("scenario.selection.mode %q is not supported", sel.Mode))
		}

	case ScenarioNetworkPartition, ScenarioNetworkChaos:
		if s.Network == nil {
			errs = append(errs, fmt.Errorf("scenario.network is required when scenario.type is %s", s.Type))
			break
		}

		n := s.Network

		if n.Target == "" {
			errs = append(errs, fmt.Errorf("scenario.network.target must be non-empty"))
		}

		if s.Type == ScenarioNetworkChaos {
			if n.Latency == nil && n.Loss == nil && n.Bandwidth == "" {
				errs = append(errs, fmt.Errorf(
					"at least one of scenario.network.latency, scenario.network.loss, or scenario.network.bandwidth must be set for %s",
					ScenarioNetworkChaos,
				))
			}

			if n.Jitter != nil && n.Latency == nil {
				errs = append(errs, fmt.Errorf(
					"scenario.network.latency must be set when scenario.network.jitter is set",
				))
			}

			if n.Bandwidth != "" && n.Target != NetworkTargetTMtoExternal {
				errs = append(errs, fmt.Errorf(
					"scenario.network.bandwidth may only be set when scenario.network.target is %s",
					NetworkTargetTMtoExternal,
				))
			}
		}

		if n.Bandwidth != "" && !bandwidthRe.MatchString(n.Bandwidth) {
			errs = append(errs, fmt.Errorf(
				"scenario.network.bandwidth %q is not a valid rate; expected a positive integer followed by kbit, mbit, gbit, or tbit (e.g. \"10mbit\")",
				n.Bandwidth,
			))
		}

		if n.Target == NetworkTargetTMtoCheckpoint || n.Target == NetworkTargetTMtoExternal {
			if n.ExternalEndpoint == nil {
				errs = append(errs, fmt.Errorf(
					"scenario.network.externalEndpoint is required when scenario.network.target is %s",
					n.Target,
				))
			} else {
				if s.Type == ScenarioNetworkPartition && n.ExternalEndpoint.CIDR == "" {
					errs = append(errs, fmt.Errorf(
						"scenario.network.externalEndpoint.cidr is required for %s when scenario.network.target is %s (hostname is not usable for NetworkPolicy)",
						ScenarioNetworkPartition, n.Target,
					))
				}
				if n.ExternalEndpoint.CIDR != "" {
					if _, _, err := net.ParseCIDR(n.ExternalEndpoint.CIDR); err != nil {
						errs = append(errs, fmt.Errorf(
							"scenario.network.externalEndpoint.cidr %q is not a valid CIDR: %w",
							n.ExternalEndpoint.CIDR, err,
						))
					}
				}
			}
		}

	case ScenarioResourceExhaustion:
		if s.ResourceExhaustion == nil {
			errs = append(errs, fmt.Errorf("scenario.resourceExhaustion is required when scenario.type is %s", ScenarioResourceExhaustion))
			break
		}
		re := s.ResourceExhaustion
		if re.Mode != ResourceExhaustionModeCPU && re.Mode != ResourceExhaustionModeMemory {
			errs = append(errs, fmt.Errorf("scenario.resourceExhaustion.mode %q is not supported; must be CPU or Memory", re.Mode))
		}
		if re.Mode == ResourceExhaustionModeMemory && re.MemoryPercent != 0 && (re.MemoryPercent < 1 || re.MemoryPercent > 100) {
			errs = append(errs, fmt.Errorf("scenario.resourceExhaustion.memoryPercent must be between 1 and 100 (got %d)", re.MemoryPercent))
		}

	default:
		errs = append(errs, fmt.Errorf("scenario.type %q is not supported", s.Type))
	}

	return errors.Join(errs...)
}

// validateObserveSpec verifies that the observation timing constraints are
// internally consistent: if both Timeout and PollInterval are non-zero, the
// Timeout must be greater than or equal to the PollInterval. It also validates
// the checkpoint trigger configuration when enabled.
func validateObserveSpec(o ObserveSpec) error {
	var errs []error

	if o.FlinkRest.RequireStableCheckpointBeforeInject {
		if !o.FlinkRest.Enabled {
			errs = append(errs, fmt.Errorf(
				"observe.flinkRest.requireStableCheckpointBeforeInject=true requires observe.flinkRest.enabled=true",
			))
		}
		if o.FlinkRest.Endpoint == "" {
			errs = append(errs, fmt.Errorf(
				"observe.flinkRest.requireStableCheckpointBeforeInject=true requires observe.flinkRest.endpoint to be non-empty",
			))
		}
	}

	timeout := o.Timeout.Duration
	poll := o.PollInterval.Duration

	if timeout > 0 && poll > 0 && timeout < poll {
		errs = append(errs, fmt.Errorf(
			"observe.timeout (%s) must be >= observe.pollInterval (%s)",
			timeout, poll,
		))
	}

	return errors.Join(errs...)
}

// SetDefaults applies safe default values to any unset fields on a ChaosRun.
// It is called by the controller before validation to ensure a minimal valid
// configuration even when the user omits optional fields.
func SetDefaults(run *ChaosRun) {
	setScenarioDefaults(&run.Spec.Scenario)
	setObserveDefaults(&run.Spec.Observe)
	setSafetyDefaults(&run.Spec.Safety)
}

func setScenarioDefaults(s *ScenarioSpec) {
	switch s.Type {
	case ScenarioTaskManagerPodKill, "":
		if s.Selection.Mode == "" {
			s.Selection.Mode = SelectionModeRandom
		}

		if s.Selection.Mode == SelectionModeRandom && s.Selection.Count == 0 {
			s.Selection.Count = 1
		}

		if s.Action.Type == "" {
			s.Action.Type = ActionDeletePod
		}

		if s.Action.GracePeriodSeconds == nil {
			zero := int64(0)
			s.Action.GracePeriodSeconds = &zero
		}
	}

	if s.Network != nil {
		if s.Network.Direction == "" {
			s.Network.Direction = NetworkDirectionBoth
		}

		if s.Network.Duration == nil {
			d := metav1.Duration{Duration: 60 * time.Second}
			s.Network.Duration = &d
		}
	}

	switch s.Type {
	case ScenarioResourceExhaustion:
		if s.ResourceExhaustion != nil {
			if s.ResourceExhaustion.Workers == 0 {
				s.ResourceExhaustion.Workers = 1
			}
			if s.ResourceExhaustion.Mode == ResourceExhaustionModeMemory && s.ResourceExhaustion.MemoryPercent == 0 {
				s.ResourceExhaustion.MemoryPercent = 80
			}
			if s.ResourceExhaustion.Duration == nil {
				d := metav1.Duration{Duration: 60 * time.Second}
				s.ResourceExhaustion.Duration = &d
			}
		}
	}
}

func setObserveDefaults(o *ObserveSpec) {
	// Enable observation by default. Because the zero value of bool is false,
	// we cannot distinguish "user explicitly set false" from "omitted" at this
	// layer; the defaulting contract is that an entirely zero ObserveSpec gets
	// observation enabled. If the user explicitly sets enabled: false, the
	// controller will respect that at runtime.
	if !o.Enabled && o.Timeout.Duration == 0 && o.PollInterval.Duration == 0 {
		o.Enabled = true
	}

	if o.Timeout == (metav1.Duration{}) {
		o.Timeout = metav1.Duration{Duration: 10 * time.Minute}
	}

	if o.PollInterval == (metav1.Duration{}) {
		o.PollInterval = metav1.Duration{Duration: 5 * time.Second}
	}

	// Default checkpoint wait timeout when checkpoint trigger is enabled.
	if o.FlinkRest.RequireStableCheckpointBeforeInject {
		if o.FlinkRest.CheckpointStableWindowSeconds == 0 {
			o.FlinkRest.CheckpointStableWindowSeconds = 60
		}
		if o.FlinkRest.CheckpointWaitTimeout == nil {
			d := metav1.Duration{Duration: 5 * time.Minute}
			o.FlinkRest.CheckpointWaitTimeout = &d
		}
	}
}

func setSafetyDefaults(s *SafetySpec) {
	if s.MaxConcurrentRunsPerTarget == 0 {
		s.MaxConcurrentRunsPerTarget = 1
	}

	if s.MinTaskManagersRemaining == 0 {
		s.MinTaskManagersRemaining = 1
	}

	if s.MaxNetworkChaosDuration == nil {
		d := metav1.Duration{Duration: 5 * time.Minute}
		s.MaxNetworkChaosDuration = &d
	}

	if s.MaxResourceExhaustionDuration == nil {
		d := metav1.Duration{Duration: 5 * time.Minute}
		s.MaxResourceExhaustionDuration = &d
	}
}
