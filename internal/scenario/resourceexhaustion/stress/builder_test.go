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

package stress_test

import (
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/scenario/resourceexhaustion/stress"
)

func TestArgs(t *testing.T) {
	dur30s := metav1.Duration{Duration: 30 * time.Second}
	dur2m := metav1.Duration{Duration: 2 * time.Minute}

	tests := []struct {
		name string
		spec *v1alpha1.ResourceExhaustionSpec
		want []string
	}{
		{
			name: "CPU mode default workers",
			spec: &v1alpha1.ResourceExhaustionSpec{
				Mode:     v1alpha1.ResourceExhaustionModeCPU,
				Workers:  1,
				Duration: &dur30s,
			},
			want: []string{"stress-ng", "--cpu", "1", "--timeout", "30s"},
		},
		{
			name: "CPU mode multiple workers",
			spec: &v1alpha1.ResourceExhaustionSpec{
				Mode:     v1alpha1.ResourceExhaustionModeCPU,
				Workers:  4,
				Duration: &dur2m,
			},
			want: []string{"stress-ng", "--cpu", "4", "--timeout", "120s"},
		},
		{
			name: "Memory mode with percent",
			spec: &v1alpha1.ResourceExhaustionSpec{
				Mode:          v1alpha1.ResourceExhaustionModeMemory,
				Workers:       2,
				MemoryPercent: 75,
				Duration:      &dur30s,
			},
			want: []string{"stress-ng", "--vm", "2", "--vm-bytes", "75%", "--timeout", "30s"},
		},
		{
			name: "Memory mode default percent (80)",
			spec: &v1alpha1.ResourceExhaustionSpec{
				Mode:          v1alpha1.ResourceExhaustionModeMemory,
				Workers:       1,
				MemoryPercent: 80,
				Duration:      &dur30s,
			},
			want: []string{"stress-ng", "--vm", "1", "--vm-bytes", "80%", "--timeout", "30s"},
		},
		{
			name: "nil Duration falls back to 60s",
			spec: &v1alpha1.ResourceExhaustionSpec{
				Mode:    v1alpha1.ResourceExhaustionModeCPU,
				Workers: 1,
				// Duration intentionally nil
			},
			want: []string{"stress-ng", "--cpu", "1", "--timeout", "60s"},
		},
		{
			name: "unknown mode falls back to cpu 1",
			spec: &v1alpha1.ResourceExhaustionSpec{
				Mode:     "Unknown",
				Workers:  3,
				Duration: &dur30s,
			},
			want: []string{"stress-ng", "--cpu", "1", "--timeout", "30s"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stress.Args(tc.spec)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Args() = %v; want %v", got, tc.want)
			}
		})
	}
}
