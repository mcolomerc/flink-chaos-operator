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

package networkpartition_test

import (
	"context"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/interfaces"
	"github.com/flink-chaos-operator/internal/scenario/networkpartition"
)

// newScheme returns a runtime.Scheme that includes the networking API group so
// the fake client can handle NetworkPolicy objects.
func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	utilruntime.Must(networkingv1.AddToScheme(s))
	return s
}

const testNamespace = "default"

// buildRun returns a minimal ChaosRun for NetworkPartition tests.
func buildRun(name string, netSpec *v1alpha1.NetworkChaosSpec) *v1alpha1.ChaosRun {
	return &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: v1alpha1.ChaosRunSpec{
			Scenario: v1alpha1.ScenarioSpec{
				Type:    v1alpha1.ScenarioNetworkPartition,
				Network: netSpec,
			},
		},
	}
}

// defaultTarget returns a ResolvedTarget with two TM pods.
func defaultTarget() *interfaces.ResolvedTarget {
	return &interfaces.ResolvedTarget{
		Platform:    "flink-operator",
		Namespace:   testNamespace,
		LogicalName: "my-flink",
		TMPodNames:  []string{"tm-0", "tm-1"},
		JMPodNames:  []string{"jm-0"},
	}
}

// listNetworkPolicies returns all NetworkPolicy resources in the fake client.
func listNetworkPolicies(t *testing.T, c client.Client) []networkingv1.NetworkPolicy {
	t.Helper()
	var list networkingv1.NetworkPolicyList
	if err := c.List(context.Background(), &list); err != nil {
		t.Fatalf("listing NetworkPolicies: %v", err)
	}
	return list.Items
}

// ---------------------------------------------------------------------------
// Wrong scenario type
// ---------------------------------------------------------------------------

func TestInject_WrongScenarioType_ReturnsError(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	d := networkpartition.New(fakeClient)

	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{Name: "test-run", Namespace: testNamespace},
		Spec: v1alpha1.ChaosRunSpec{
			Scenario: v1alpha1.ScenarioSpec{
				Type: v1alpha1.ScenarioTaskManagerPodKill,
				Network: &v1alpha1.NetworkChaosSpec{
					Target: v1alpha1.NetworkTargetTMtoJM,
				},
			},
		},
	}

	_, err := d.Inject(context.Background(), run, defaultTarget())
	if err == nil {
		t.Fatal("expected error for wrong scenario type, got nil")
	}
}

// ---------------------------------------------------------------------------
// Nil network spec
// ---------------------------------------------------------------------------

func TestInject_NilNetworkSpec_ReturnsError(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	d := networkpartition.New(fakeClient)

	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{Name: "test-run", Namespace: testNamespace},
		Spec: v1alpha1.ChaosRunSpec{
			Scenario: v1alpha1.ScenarioSpec{
				Type:    v1alpha1.ScenarioNetworkPartition,
				Network: nil,
			},
		},
	}

	_, err := d.Inject(context.Background(), run, defaultTarget())
	if err == nil {
		t.Fatal("expected error for nil network spec, got nil")
	}
}

// ---------------------------------------------------------------------------
// TMtoTM: two NetworkPolicies created (Both direction by default)
// ---------------------------------------------------------------------------

func TestInject_TMtoTM_BothDirections_CreatesTwoPolicies(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	d := networkpartition.New(fakeClient)

	run := buildRun("chaos-tmtotm", &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTargetTMtoTM,
		Direction: v1alpha1.NetworkDirectionBoth,
	})

	result, err := d.Inject(context.Background(), run, defaultTarget())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	policies := listNetworkPolicies(t, fakeClient)
	if len(policies) != 2 {
		t.Errorf("expected 2 NetworkPolicies, got %d", len(policies))
	}

	if len(run.Status.NetworkPolicies) != 2 {
		t.Errorf("expected status.NetworkPolicies length 2, got %d", len(run.Status.NetworkPolicies))
	}

	if len(result.SelectedPods) != 2 || len(result.InjectedPods) != 2 {
		t.Errorf("expected 2 selected and 2 injected pods, got selected=%d injected=%d",
			len(result.SelectedPods), len(result.InjectedPods))
	}

	// Both source and dest labels should be taskmanager for TMtoTM.
	for _, p := range policies {
		if p.Spec.PodSelector.MatchLabels["component"] != "taskmanager" {
			t.Errorf("expected source podSelector component=taskmanager, got %v", p.Spec.PodSelector.MatchLabels)
		}
	}
}

// ---------------------------------------------------------------------------
// TMtoJM: deny-all ingress and egress to JM pods (both directions)
// ---------------------------------------------------------------------------

// TestInject_TMtoJM_CorrectLabels verifies that for a TMtoJM partition with
// Both direction, the policies select JM pods (the destination) and use
// empty ingress/egress rules to deny all traffic to those pods.
func TestInject_TMtoJM_CorrectLabels(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	d := networkpartition.New(fakeClient)

	run := buildRun("chaos-tmtojm", &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTargetTMtoJM,
		Direction: v1alpha1.NetworkDirectionBoth,
	})

	_, err := d.Inject(context.Background(), run, defaultTarget())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	policies := listNetworkPolicies(t, fakeClient)
	if len(policies) != 2 {
		t.Fatalf("expected 2 NetworkPolicies, got %d", len(policies))
	}

	// Both policies should select JM pods (the deny-all destination).
	for _, p := range policies {
		srcComponent := p.Spec.PodSelector.MatchLabels["component"]
		if srcComponent != "jobmanager" {
			t.Errorf("expected podSelector component=jobmanager (deny-all on destination), got %q", srcComponent)
		}
	}

	// Both policies use deny-all rules: empty ingress and/or egress lists.
	for _, p := range policies {
		for _, pt := range p.Spec.PolicyTypes {
			switch pt {
			case networkingv1.PolicyTypeIngress:
				if len(p.Spec.Ingress) != 0 {
					t.Errorf("expected empty ingress rules for deny-all, got %d", len(p.Spec.Ingress))
				}
			case networkingv1.PolicyTypeEgress:
				if len(p.Spec.Egress) != 0 {
					t.Errorf("expected empty egress rules for deny-all, got %d", len(p.Spec.Egress))
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// TMtoCheckpoint: egress policy allows all egress except blocked CIDR
// ---------------------------------------------------------------------------

// TestInject_TMtoCheckpoint_EgressWithCIDR verifies that for a TMtoCheckpoint
// partition with Egress direction, the policy selects TM pods and allows all
// egress except the specified CIDR (using IPBlock.Except), effectively denying
// egress to the checkpoint storage endpoint while leaving other egress intact.
func TestInject_TMtoCheckpoint_EgressWithCIDR(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	d := networkpartition.New(fakeClient)

	run := buildRun("chaos-checkpoint", &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTargetTMtoCheckpoint,
		Direction: v1alpha1.NetworkDirectionEgress,
		ExternalEndpoint: &v1alpha1.ExternalTarget{
			CIDR: "10.200.0.0/16",
		},
	})

	_, err := d.Inject(context.Background(), run, defaultTarget())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	policies := listNetworkPolicies(t, fakeClient)
	if len(policies) != 1 {
		t.Fatalf("expected 1 NetworkPolicy (egress only), got %d", len(policies))
	}

	p := policies[0]

	// Policy selects TM pods.
	if p.Spec.PodSelector.MatchLabels["component"] != "taskmanager" {
		t.Errorf("expected podSelector component=taskmanager for CIDR-based deny, got %v",
			p.Spec.PodSelector.MatchLabels)
	}

	// Policy uses allow-all-except semantics: one egress rule with 0.0.0.0/0
	// and the blocked CIDR listed in Except.
	if len(p.Spec.Egress) != 1 {
		t.Fatalf("expected 1 egress rule (allow-all-except), got %d", len(p.Spec.Egress))
	}
	foundExcept := false
	for _, peer := range p.Spec.Egress[0].To {
		if peer.IPBlock != nil &&
			peer.IPBlock.CIDR == "0.0.0.0/0" &&
			len(peer.IPBlock.Except) == 1 &&
			peer.IPBlock.Except[0] == "10.200.0.0/16" {
			foundExcept = true
		}
	}
	if !foundExcept {
		t.Errorf("expected egress rule with IPBlock{CIDR:0.0.0.0/0, Except:[10.200.0.0/16]}, none found in %+v", p.Spec)
	}
}

// ---------------------------------------------------------------------------
// TMtoExternal with missing ExternalEndpoint → error
// ---------------------------------------------------------------------------

func TestInject_TMtoExternal_MissingCIDR_ReturnsError(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	d := networkpartition.New(fakeClient)

	run := buildRun("chaos-ext-nocid", &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTargetTMtoExternal,
		Direction: v1alpha1.NetworkDirectionEgress,
		// ExternalEndpoint intentionally omitted.
	})

	_, err := d.Inject(context.Background(), run, defaultTarget())
	if err == nil {
		t.Fatal("expected error for missing ExternalEndpoint.CIDR, got nil")
	}
}

func TestInject_TMtoExternal_EmptyCIDR_ReturnsError(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	d := networkpartition.New(fakeClient)

	run := buildRun("chaos-ext-empty", &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTargetTMtoExternal,
		Direction: v1alpha1.NetworkDirectionEgress,
		ExternalEndpoint: &v1alpha1.ExternalTarget{
			CIDR: "", // explicitly empty
		},
	})

	_, err := d.Inject(context.Background(), run, defaultTarget())
	if err == nil {
		t.Fatal("expected error for empty CIDR, got nil")
	}
}

// ---------------------------------------------------------------------------
// Ingress-only direction: only one policy created
// ---------------------------------------------------------------------------

func TestInject_IngressOnly_CreatesOnePolicy(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	d := networkpartition.New(fakeClient)

	run := buildRun("chaos-ingress", &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTargetTMtoJM,
		Direction: v1alpha1.NetworkDirectionIngress,
	})

	_, err := d.Inject(context.Background(), run, defaultTarget())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	policies := listNetworkPolicies(t, fakeClient)
	if len(policies) != 1 {
		t.Errorf("expected 1 NetworkPolicy (ingress only), got %d", len(policies))
	}

	if len(run.Status.NetworkPolicies) != 1 {
		t.Errorf("expected 1 entry in status.NetworkPolicies, got %d", len(run.Status.NetworkPolicies))
	}
}

// ---------------------------------------------------------------------------
// Already-exists on create → idempotent (no error, policy counted)
// ---------------------------------------------------------------------------

func TestInject_PolicyAlreadyExists_Idempotent(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	d := networkpartition.New(fakeClient)

	run := buildRun("chaos-idem", &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTargetTMtoJM,
		Direction: v1alpha1.NetworkDirectionBoth,
	})

	// First injection creates the policies.
	_, err := d.Inject(context.Background(), run, defaultTarget())
	if err != nil {
		t.Fatalf("first inject failed: %v", err)
	}

	// Reset status to simulate a retry from a clean run object.
	run.Status.NetworkPolicies = nil

	// Second injection: policies already exist, should be idempotent.
	result, err := d.Inject(context.Background(), run, defaultTarget())
	if err != nil {
		t.Fatalf("second inject (idempotent) returned error: %v", err)
	}

	if len(run.Status.NetworkPolicies) != 2 {
		t.Errorf("expected 2 entries in status.NetworkPolicies after idempotent inject, got %d",
			len(run.Status.NetworkPolicies))
	}

	if len(result.InjectedPods) != 2 {
		t.Errorf("expected 2 injected pods, got %d", len(result.InjectedPods))
	}
}

// ---------------------------------------------------------------------------
// Cleanup: policies deleted by label selector, status cleared
// ---------------------------------------------------------------------------

func TestCleanup_DeletesPoliciesByLabel(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	d := networkpartition.New(fakeClient)

	run := buildRun("chaos-cleanup", &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTargetTMtoJM,
		Direction: v1alpha1.NetworkDirectionBoth,
	})

	_, err := d.Inject(context.Background(), run, defaultTarget())
	if err != nil {
		t.Fatalf("inject failed: %v", err)
	}

	if len(listNetworkPolicies(t, fakeClient)) != 2 {
		t.Fatal("expected 2 policies before cleanup")
	}

	if err := d.Cleanup(context.Background(), run); err != nil {
		t.Fatalf("cleanup returned error: %v", err)
	}

	remaining := listNetworkPolicies(t, fakeClient)
	if len(remaining) != 0 {
		t.Errorf("expected 0 NetworkPolicies after cleanup, got %d", len(remaining))
	}

	if run.Status.NetworkPolicies != nil {
		t.Errorf("expected status.NetworkPolicies to be nil after cleanup, got %v", run.Status.NetworkPolicies)
	}
}

// ---------------------------------------------------------------------------
// Cleanup: NotFound policies skipped without error
// ---------------------------------------------------------------------------

func TestCleanup_NotFoundPolicies_SkippedWithoutError(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	d := networkpartition.New(fakeClient)

	// Build a run that references policies that were never created in the fake client.
	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{Name: "chaos-notfound", Namespace: testNamespace},
		Spec: v1alpha1.ChaosRunSpec{
			Scenario: v1alpha1.ScenarioSpec{
				Type: v1alpha1.ScenarioNetworkPartition,
			},
		},
		Status: v1alpha1.ChaosRunStatus{
			NetworkPolicies: []string{
				"chaos-notfound-netpol-ingress",
				"chaos-notfound-netpol-egress",
			},
		},
	}

	// Cleanup on an empty cluster — no policies exist, list returns empty, no deletes.
	if err := d.Cleanup(context.Background(), run); err != nil {
		t.Fatalf("cleanup returned error for already-gone policies: %v", err)
	}

	if run.Status.NetworkPolicies != nil {
		t.Errorf("expected status.NetworkPolicies nil after cleanup, got %v", run.Status.NetworkPolicies)
	}
}

// ---------------------------------------------------------------------------
// SelectedPods set on successful inject
// ---------------------------------------------------------------------------

func TestInject_SetsSelectedPodsOnStatus(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	d := networkpartition.New(fakeClient)

	run := buildRun("chaos-pods", &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTargetTMtoJM,
		Direction: v1alpha1.NetworkDirectionBoth,
	})

	target := defaultTarget()

	_, err := d.Inject(context.Background(), run, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(run.Status.SelectedPods) != len(target.TMPodNames) {
		t.Errorf("expected status.SelectedPods=%v, got %v", target.TMPodNames, run.Status.SelectedPods)
	}
}
