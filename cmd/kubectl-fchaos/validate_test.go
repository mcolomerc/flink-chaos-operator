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
	"testing"
)

func TestValidateNetworkTarget(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Valid canonical values.
		{name: "TMtoTM", input: "TMtoTM", wantErr: false},
		{name: "TMtoJM", input: "TMtoJM", wantErr: false},
		{name: "TMtoCheckpoint", input: "TMtoCheckpoint", wantErr: false},
		{name: "TMtoExternal", input: "TMtoExternal", wantErr: false},

		// Case variants — must be accepted.
		{name: "lowercase tmtotm", input: "tmtotm", wantErr: false},
		{name: "uppercase TMTOJM", input: "TMTOJM", wantErr: false},
		{name: "mixed TMTOcheckpoint", input: "TMTOcheckpoint", wantErr: false},
		{name: "mixed tmtoExternal", input: "tmtoExternal", wantErr: false},

		// Invalid values.
		{name: "empty string", input: "", wantErr: true},
		{name: "unknown Foo", input: "Foo", wantErr: true},
		{name: "TMtoMagicBean", input: "TMtoMagicBean", wantErr: true},
		{name: "partial TM", input: "TM", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateNetworkTarget(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("validateNetworkTarget(%q): expected error, got nil", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("validateNetworkTarget(%q): unexpected error: %v", tc.input, err)
			}
		})
	}
}

func TestValidateNetworkDirection(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Valid canonical values.
		{name: "Ingress", input: "Ingress", wantErr: false},
		{name: "Egress", input: "Egress", wantErr: false},
		{name: "Both", input: "Both", wantErr: false},

		// Case variants — must be accepted.
		{name: "lowercase ingress", input: "ingress", wantErr: false},
		{name: "uppercase EGRESS", input: "EGRESS", wantErr: false},
		{name: "lowercase both", input: "both", wantErr: false},

		// Invalid values.
		{name: "empty string", input: "", wantErr: true},
		{name: "both-ways", input: "both-ways", wantErr: true},
		{name: "partial in", input: "in", wantErr: true},
		{name: "unknown Out", input: "Out", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateNetworkDirection(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("validateNetworkDirection(%q): expected error, got nil", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("validateNetworkDirection(%q): unexpected error: %v", tc.input, err)
			}
		})
	}
}

func TestNormaliseNetworkTarget(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"tmtotm", "TMtoTM"},
		{"TMTOJM", "TMtoJM"},
		{"tmtocheckpoint", "TMtoCheckpoint"},
		{"TMTOEXTERNAL", "TMtoExternal"},
		// Already canonical — must be returned unchanged.
		{"TMtoTM", "TMtoTM"},
		{"TMtoJM", "TMtoJM"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := normaliseNetworkTarget(tc.input)
			if got != tc.want {
				t.Errorf("normaliseNetworkTarget(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormaliseNetworkDirection(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ingress", "Ingress"},
		{"EGRESS", "Egress"},
		{"both", "Both"},
		// Already canonical — must be returned unchanged.
		{"Ingress", "Ingress"},
		{"Both", "Both"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := normaliseNetworkDirection(tc.input)
			if got != tc.want {
				t.Errorf("normaliseNetworkDirection(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestValidateResourceExhaustionMode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Valid values.
		{name: "CPU", input: "CPU", wantErr: false},
		{name: "Memory", input: "Memory", wantErr: false},

		// Invalid values — mode is case-sensitive.
		{name: "lowercase cpu", input: "cpu", wantErr: true},
		{name: "lowercase memory", input: "memory", wantErr: true},
		{name: "uppercase MEMORY", input: "MEMORY", wantErr: true},
		{name: "empty string", input: "", wantErr: true},
		{name: "unknown Disk", input: "Disk", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateResourceExhaustionMode(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("validateResourceExhaustionMode(%q): expected error, got nil", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("validateResourceExhaustionMode(%q): unexpected error: %v", tc.input, err)
			}
		})
	}
}
