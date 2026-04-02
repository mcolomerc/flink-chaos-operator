package policy

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
)

// TestBuild_EgressOnly verifies that an Egress-direction label-based policy
// produces empty egress rules (deny-all egress) on the selected pods.
func TestBuild_EgressOnly(t *testing.T) {
	s := Spec{
		Name:         "test-egress",
		Namespace:    "default",
		ChaosRunName: "run-abc",
		SourceLabels: map[string]string{"app": "flink-jobmanager"},
		Direction:    "Egress",
	}

	p := Build(s)

	if p.Spec.PodSelector.MatchLabels["app"] != "flink-jobmanager" {
		t.Errorf("expected podSelector app=flink-jobmanager, got %v", p.Spec.PodSelector.MatchLabels)
	}
	if len(p.Spec.PolicyTypes) != 1 || p.Spec.PolicyTypes[0] != networkingv1.PolicyTypeEgress {
		t.Errorf("expected PolicyTypes=[Egress], got %v", p.Spec.PolicyTypes)
	}
	// Deny-all egress: empty rules list.
	if p.Spec.Egress == nil {
		t.Errorf("expected non-nil (empty) egress rules for deny-all, got nil")
	}
	if len(p.Spec.Egress) != 0 {
		t.Errorf("expected 0 egress rules for deny-all, got %d", len(p.Spec.Egress))
	}
	if len(p.Spec.Ingress) != 0 {
		t.Errorf("expected no ingress rules for Egress direction")
	}
}

// TestBuild_IngressOnly verifies that an Ingress-direction label-based policy
// produces empty ingress rules (deny-all ingress) on the selected pods.
func TestBuild_IngressOnly(t *testing.T) {
	s := Spec{
		Name:         "test-ingress",
		Namespace:    "default",
		ChaosRunName: "run-abc",
		SourceLabels: map[string]string{"app": "flink-jobmanager"},
		Direction:    "Ingress",
	}

	p := Build(s)

	if len(p.Spec.PolicyTypes) != 1 || p.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress {
		t.Errorf("expected PolicyTypes=[Ingress], got %v", p.Spec.PolicyTypes)
	}
	// Deny-all ingress: empty rules list.
	if p.Spec.Ingress == nil {
		t.Errorf("expected non-nil (empty) ingress rules for deny-all, got nil")
	}
	if len(p.Spec.Ingress) != 0 {
		t.Errorf("expected 0 ingress rules for deny-all, got %d", len(p.Spec.Ingress))
	}
	if len(p.Spec.Egress) != 0 {
		t.Errorf("expected no egress rules for Ingress direction")
	}
}

// TestBuild_BothDirection verifies that a Both-direction label-based policy
// produces empty ingress and egress rules (deny-all both directions).
func TestBuild_BothDirection(t *testing.T) {
	s := Spec{
		Name:         "test-both",
		Namespace:    "default",
		ChaosRunName: "run-xyz",
		SourceLabels: map[string]string{"app": "flink-jobmanager"},
		Direction:    "Both",
	}

	p := Build(s)

	if len(p.Spec.PolicyTypes) != 2 {
		t.Fatalf("expected 2 PolicyTypes, got %d", len(p.Spec.PolicyTypes))
	}
	hasIngress, hasEgress := false, false
	for _, pt := range p.Spec.PolicyTypes {
		if pt == networkingv1.PolicyTypeIngress {
			hasIngress = true
		}
		if pt == networkingv1.PolicyTypeEgress {
			hasEgress = true
		}
	}
	if !hasIngress || !hasEgress {
		t.Errorf("expected both Ingress and Egress in PolicyTypes, got %v", p.Spec.PolicyTypes)
	}
	// Deny-all: both rule lists are non-nil but empty.
	if p.Spec.Ingress == nil {
		t.Errorf("expected non-nil (empty) ingress rules, got nil")
	}
	if len(p.Spec.Ingress) != 0 {
		t.Errorf("expected 0 ingress rules, got %d", len(p.Spec.Ingress))
	}
	if p.Spec.Egress == nil {
		t.Errorf("expected non-nil (empty) egress rules, got nil")
	}
	if len(p.Spec.Egress) != 0 {
		t.Errorf("expected 0 egress rules, got %d", len(p.Spec.Egress))
	}
}

// TestBuild_DestCIDR verifies that a CIDR-based policy selects TM pods and
// allows all egress except the blocked CIDR (using IPBlock.Except).
func TestBuild_DestCIDR(t *testing.T) {
	s := Spec{
		Name:         "test-cidr",
		Namespace:    "default",
		ChaosRunName: "run-cidr",
		SourceLabels: map[string]string{"app": "flink-taskmanager"},
		DestCIDR:     "10.0.0.0/8",
		Direction:    "Egress",
	}

	p := Build(s)

	if len(p.Spec.Egress) != 1 {
		t.Fatalf("expected 1 egress rule, got %d", len(p.Spec.Egress))
	}
	peer := p.Spec.Egress[0].To[0]
	if peer.IPBlock == nil {
		t.Fatal("expected IPBlock to be set for CIDR deny")
	}
	if peer.IPBlock.CIDR != "0.0.0.0/0" {
		t.Errorf("expected allow-all CIDR 0.0.0.0/0, got %s", peer.IPBlock.CIDR)
	}
	if len(peer.IPBlock.Except) != 1 || peer.IPBlock.Except[0] != "10.0.0.0/8" {
		t.Errorf("expected Except=[10.0.0.0/8], got %v", peer.IPBlock.Except)
	}
	if peer.PodSelector != nil {
		t.Errorf("expected PodSelector to be nil for CIDR destination")
	}
}

// TestBuild_PortRestriction verifies that port restriction is applied for
// CIDR-based policies (the port is on the allow-all-except rule).
func TestBuild_PortRestriction(t *testing.T) {
	s := Spec{
		Name:         "test-port",
		Namespace:    "default",
		ChaosRunName: "run-port",
		SourceLabels: map[string]string{"app": "flink-taskmanager"},
		DestCIDR:     "10.0.0.0/8",
		Direction:    "Egress",
		Port:         6123,
	}

	p := Build(s)

	if len(p.Spec.Egress) != 1 {
		t.Fatalf("expected 1 egress rule, got %d", len(p.Spec.Egress))
	}
	ports := p.Spec.Egress[0].Ports
	if len(ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(ports))
	}
	if ports[0].Port == nil || ports[0].Port.IntVal != 6123 {
		t.Errorf("expected port IntVal=6123, got %v", ports[0].Port)
	}
	if ports[0].Protocol == nil || *ports[0].Protocol != corev1.ProtocolTCP {
		t.Errorf("expected protocol TCP, got %v", ports[0].Protocol)
	}
}

// TestBuild_NoPortRestriction verifies that no port restriction means all ports.
func TestBuild_NoPortRestriction(t *testing.T) {
	s := Spec{
		Name:         "test-noport",
		Namespace:    "default",
		ChaosRunName: "run-noport",
		SourceLabels: map[string]string{"app": "flink-taskmanager"},
		DestCIDR:     "10.0.0.0/8",
		Direction:    "Egress",
		Port:         0,
	}

	p := Build(s)

	if len(p.Spec.Egress) != 1 {
		t.Fatalf("expected 1 egress rule, got %d", len(p.Spec.Egress))
	}
	if p.Spec.Egress[0].Ports != nil {
		t.Errorf("expected nil Ports slice for zero port, got %v", p.Spec.Egress[0].Ports)
	}
}

// TestBuild_Labels verifies that the chaos run labels are set correctly.
func TestBuild_Labels(t *testing.T) {
	s := Spec{
		Name:         "test-labels",
		Namespace:    "staging",
		ChaosRunName: "my-chaos-run",
		SourceLabels: map[string]string{"app": "flink-jobmanager"},
		Direction:    "Both",
	}

	p := Build(s)

	if p.Labels["chaos.flink.io/run-name"] != "my-chaos-run" {
		t.Errorf("expected label chaos.flink.io/run-name=my-chaos-run, got %q", p.Labels["chaos.flink.io/run-name"])
	}
	if p.Labels["chaos.flink.io/managed-by"] != "flink-chaos-operator" {
		t.Errorf("expected label chaos.flink.io/managed-by=flink-chaos-operator, got %q", p.Labels["chaos.flink.io/managed-by"])
	}
	if p.Name != "test-labels" {
		t.Errorf("expected Name=test-labels, got %q", p.Name)
	}
	if p.Namespace != "staging" {
		t.Errorf("expected Namespace=staging, got %q", p.Namespace)
	}
}

// TestNames verifies the conventional naming scheme for the two policy names.
func TestNames(t *testing.T) {
	ingress, egress := Names("my-chaos-run")
	if ingress != "my-chaos-run-netpol-ingress" {
		t.Errorf("unexpected ingress name: %q", ingress)
	}
	if egress != "my-chaos-run-netpol-egress" {
		t.Errorf("unexpected egress name: %q", egress)
	}
}

// TestBuild_DenyAll_LabelBased_Ingress verifies the deny-all ingress model:
// selecting JM pods with empty ingress rules blocks all traffic to JM pods.
func TestBuild_DenyAll_LabelBased_Ingress(t *testing.T) {
	s := Spec{
		Name:         "test-deny-jm",
		Namespace:    "default",
		ChaosRunName: "run-tojm",
		SourceLabels: map[string]string{"component": "jobmanager"},
		Direction:    "Ingress",
	}

	p := Build(s)

	// Policy selects JM pods.
	if p.Spec.PodSelector.MatchLabels["component"] != "jobmanager" {
		t.Errorf("expected podSelector component=jobmanager, got %v", p.Spec.PodSelector.MatchLabels)
	}
	// PolicyType is Ingress.
	if len(p.Spec.PolicyTypes) != 1 || p.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress {
		t.Errorf("expected PolicyTypes=[Ingress], got %v", p.Spec.PolicyTypes)
	}
	// Empty ingress rules = deny all ingress to JM pods.
	if len(p.Spec.Ingress) != 0 {
		t.Errorf("expected 0 ingress rules (deny-all), got %d", len(p.Spec.Ingress))
	}
}

// TestBuild_DestCIDR_Both verifies that Both direction with CIDR denies
// all ingress to source pods and allows all egress except the CIDR.
func TestBuild_DestCIDR_Both(t *testing.T) {
	s := Spec{
		Name:         "test-cidr-both",
		Namespace:    "default",
		ChaosRunName: "run-cidr-both",
		SourceLabels: map[string]string{"app": "flink-taskmanager"},
		DestCIDR:     "172.16.0.0/12",
		Direction:    "Both",
	}

	p := Build(s)

	if len(p.Spec.PolicyTypes) != 2 {
		t.Fatalf("expected 2 PolicyTypes, got %d", len(p.Spec.PolicyTypes))
	}
	// Ingress: deny-all (empty).
	if len(p.Spec.Ingress) != 0 {
		t.Errorf("expected 0 ingress rules, got %d", len(p.Spec.Ingress))
	}
	// Egress: allow all except CIDR.
	if len(p.Spec.Egress) != 1 {
		t.Fatalf("expected 1 egress rule, got %d", len(p.Spec.Egress))
	}
	peer := p.Spec.Egress[0].To[0]
	if peer.IPBlock == nil {
		t.Fatal("expected IPBlock in egress rule")
	}
	if peer.IPBlock.CIDR != "0.0.0.0/0" {
		t.Errorf("expected 0.0.0.0/0 CIDR, got %s", peer.IPBlock.CIDR)
	}
	if len(peer.IPBlock.Except) != 1 || peer.IPBlock.Except[0] != "172.16.0.0/12" {
		t.Errorf("expected Except=[172.16.0.0/12], got %v", peer.IPBlock.Except)
	}
}
