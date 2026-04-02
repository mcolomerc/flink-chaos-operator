// Package policy builds NetworkPolicy objects for network chaos scenarios.
// It constructs the objects only — no Kubernetes API calls are made here.
//
// NetworkPolicy semantics: Kubernetes NetworkPolicy rules are ALLOW rules.
// To block traffic to a specific peer, this package uses a "deny all" approach:
// select the destination pods and configure an empty ingress/egress rules list.
// An empty rules list with a policyType set means "deny all" for that direction.
// For CIDR-based targets (external endpoints), TM pods are selected as the source
// and an empty egress rules list denies all egress, plus an allow-all-except rule
// using IPBlock with the blocked CIDR listed in "Except".
package policy

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Spec describes what a NetworkPolicy should block.
type Spec struct {
	// Name is the NetworkPolicy resource name.
	Name string
	// Namespace is the Kubernetes namespace.
	Namespace string
	// ChaosRunName is written as label chaos.flink.io/run-name for cleanup lookup.
	ChaosRunName string
	// SourceLabels are the pod selector labels for the source of the blocked traffic.
	// For label-based targets (TMtoJM, TMtoTM): these are the TM pod labels when
	// generating an egress-deny policy on TM pods.
	// For ingress-deny policies, SourceLabels selects the destination pods
	// (e.g. JM pods) and the policy denies all ingress to them.
	SourceLabels map[string]string
	// DestLabels are the peer pod selector labels. Nil when DestCIDR is set.
	// For ingress-deny policies this field is unused (deny-all has no peer).
	DestLabels map[string]string
	// DestCIDR is the destination CIDR block for external endpoint partition.
	// Non-empty when DestLabels is nil.
	DestCIDR string
	// Direction is "Ingress", "Egress", or "Both".
	Direction string
	// Port optionally restricts the policy to a single port. Zero means all ports.
	Port int32
}

// Build constructs and returns a NetworkPolicy for the given Spec.
//
// The resulting policy uses a deny-all approach rather than a permit-only approach:
//
//   - For label-based peers (DestLabels set): the policy selects the destination
//     pods (SourceLabels should be set to the destination pod labels) with
//     empty ingress/egress rules — this denies ALL traffic to/from those pods.
//
//   - For CIDR-based peers (DestCIDR set): the policy selects TM pods
//     (SourceLabels) and uses an egress rule that allows all IPs EXCEPT the
//     blocked CIDR, effectively denying egress to that CIDR only.
func Build(s Spec) *networkingv1.NetworkPolicy {
	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.Name,
			Namespace: s.Namespace,
			Labels: map[string]string{
				"chaos.flink.io/run-name":   s.ChaosRunName,
				"chaos.flink.io/managed-by": "flink-chaos-operator",
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: s.SourceLabels,
			},
		},
	}

	if s.DestCIDR != "" {
		// CIDR-based deny: select TM pods (SourceLabels) and allow egress to
		// all IPs except the blocked CIDR. The "Except" field in IPBlock creates
		// an effective deny for that specific CIDR while allowing all other egress.
		ports := buildPorts(s.Port)
		allowAllExcept := networkingv1.NetworkPolicyPeer{
			IPBlock: &networkingv1.IPBlock{
				CIDR:   "0.0.0.0/0",
				Except: []string{s.DestCIDR},
			},
		}
		switch s.Direction {
		case "Ingress":
			policy.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
			// Deny all ingress to source pods (empty rules = deny all).
			policy.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{}
		case "Both":
			policy.Spec.PolicyTypes = []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			}
			policy.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{}
			policy.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{
				{Ports: ports, To: []networkingv1.NetworkPolicyPeer{allowAllExcept}},
			}
		default: // "Egress"
			policy.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}
			policy.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{
				{Ports: ports, To: []networkingv1.NetworkPolicyPeer{allowAllExcept}},
			}
		}
		return policy
	}

	// Label-based deny: select the destination pods (SourceLabels must be the
	// destination pod labels) and use empty rules to deny all ingress/egress.
	switch s.Direction {
	case "Egress":
		policy.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}
		policy.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{}
	case "Ingress":
		policy.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
		policy.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{}
	default: // "Both"
		policy.Spec.PolicyTypes = []networkingv1.PolicyType{
			networkingv1.PolicyTypeIngress,
			networkingv1.PolicyTypeEgress,
		}
		policy.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{}
		policy.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{}
	}

	return policy
}

// Names returns the conventional names for the two policies a chaos run might create.
func Names(chaosRunName string) (ingressName, egressName string) {
	return chaosRunName + "-netpol-ingress", chaosRunName + "-netpol-egress"
}

// buildPorts constructs the ports slice. Returns nil when port is zero (all ports).
func buildPorts(port int32) []networkingv1.NetworkPolicyPort {
	if port == 0 {
		return nil
	}
	tcp := corev1.ProtocolTCP
	portVal := intstr.FromInt32(port)
	return []networkingv1.NetworkPolicyPort{
		{Protocol: &tcp, Port: &portVal},
	}
}
