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

package composite_test

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/interfaces"
	"github.com/flink-chaos-operator/internal/observer/composite"
)

// stubObserver implements interfaces.Observer with a fixed result.
type stubObserver struct {
	result *interfaces.ObservationResult
	err    error
}

func (s *stubObserver) Observe(_ context.Context, _ *v1alpha1.ChaosRun, _ *interfaces.ResolvedTarget) (*interfaces.ObservationResult, error) {
	return s.result, s.err
}

func emptyRun() *v1alpha1.ChaosRun {
	return &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{Name: "test-run", Namespace: "default"},
	}
}

func TestComposite_AllReady(t *testing.T) {
	obs := &composite.Observer{
		Children: []interfaces.Observer{
			&stubObserver{result: &interfaces.ObservationResult{AllReplacementsReady: true, ReplacementObserved: true, TMCountBefore: 3, TMCountAfter: 3}},
			&stubObserver{result: &interfaces.ObservationResult{AllReplacementsReady: true, ReplacementObserved: true}},
		},
	}

	result, err := obs.Observe(context.Background(), emptyRun(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.AllReplacementsReady {
		t.Error("expected AllReplacementsReady=true when all children agree")
	}
	if !result.ReplacementObserved {
		t.Error("expected ReplacementObserved=true")
	}
	if result.TMCountBefore != 3 {
		t.Errorf("expected TMCountBefore=3, got %d", result.TMCountBefore)
	}
}

func TestComposite_OneNotReady(t *testing.T) {
	obs := &composite.Observer{
		Children: []interfaces.Observer{
			&stubObserver{result: &interfaces.ObservationResult{AllReplacementsReady: true}},
			&stubObserver{result: &interfaces.ObservationResult{AllReplacementsReady: false}},
		},
	}

	result, err := obs.Observe(context.Background(), emptyRun(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AllReplacementsReady {
		t.Error("expected AllReplacementsReady=false when one child disagrees")
	}
}

func TestComposite_ChildError(t *testing.T) {
	obs := &composite.Observer{
		Children: []interfaces.Observer{
			&stubObserver{result: &interfaces.ObservationResult{AllReplacementsReady: true}},
			&stubObserver{err: errors.New("simulated error")},
		},
	}

	_, err := obs.Observe(context.Background(), emptyRun(), nil)
	if err == nil {
		t.Fatal("expected error from child observer, got nil")
	}
}

func TestComposite_EmptyChildren(t *testing.T) {
	obs := &composite.Observer{Children: []interfaces.Observer{}}

	result, err := obs.Observe(context.Background(), emptyRun(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.AllReplacementsReady {
		t.Error("expected AllReplacementsReady=true vacuously for empty children")
	}
}

func TestComposite_FlinkJobStateLastChildWins(t *testing.T) {
	obs := &composite.Observer{
		Children: []interfaces.Observer{
			&stubObserver{result: &interfaces.ObservationResult{AllReplacementsReady: true, FlinkJobState: "FAILING"}},
			&stubObserver{result: &interfaces.ObservationResult{AllReplacementsReady: true, FlinkJobState: "RUNNING"}},
		},
	}

	result, err := obs.Observe(context.Background(), emptyRun(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FlinkJobState != "RUNNING" {
		t.Errorf("expected FlinkJobState=RUNNING (last child wins), got %q", result.FlinkJobState)
	}
}
