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

// Package main is the entry point for the kubectl-fchaos CLI plugin.
package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/flink-chaos-operator/api/v1alpha1"
)

// global flags shared across all subcommands
var (
	namespace  string
	kubeconfig string
)

// buildClient constructs a controller-runtime client with the ChaosRun scheme registered.
func buildClient(kubeconfigPath string) (client.Client, error) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		loadingRules.ExplicitPath = kubeconfigPath
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build kubeconfig: %w", err)
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}
	return c, nil
}

// parseSelectorToLabelSelector converts a label selector string (e.g. "app=foo,env=prod")
// into a metav1.LabelSelector using only equality and set-based requirements that have
// a single value.
func parseSelectorToLabelSelector(selectorStr string) (*metav1.LabelSelector, error) {
	parsed, err := labels.Parse(selectorStr)
	if err != nil {
		return nil, fmt.Errorf("parse selector %q: %w", selectorStr, err)
	}
	requirements, selectable := parsed.Requirements()
	if !selectable {
		return nil, fmt.Errorf("selector %q is not convertible to requirements", selectorStr)
	}

	matchLabels := make(map[string]string)
	for _, req := range requirements {
		op := req.Operator()
		if op == selection.Equals || op == selection.DoubleEquals || op == selection.In {
			vals := req.Values()
			if vals.Len() == 1 {
				matchLabels[req.Key()] = vals.List()[0]
			}
		}
	}
	return &metav1.LabelSelector{MatchLabels: matchLabels}, nil
}

// humanAge returns a short human-readable duration since t.
func humanAge(t metav1.Time) string {
	if t.IsZero() {
		return "<unknown>"
	}
	d := time.Since(t.Time).Truncate(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}

// formatTime returns the RFC3339 string for a *metav1.Time or "<none>" when nil.
func formatTime(t *metav1.Time) string {
	if t == nil || t.IsZero() {
		return "<none>"
	}
	return t.UTC().Format(time.RFC3339)
}

// formatStrings joins a string slice as "[a b c]" or "[]" when empty.
func formatStrings(ss []string) string {
	if len(ss) == 0 {
		return "[]"
	}
	return "[" + strings.Join(ss, " ") + "]"
}

// ---- targetFlagSet: shared target flags across subcommands ----

// targetFlagSet holds the flag values for identifying the target Flink workload.
// It is embedded in each subcommand that needs target selection.
type targetFlagSet struct {
	targetType     string
	targetName     string
	deploymentName string
	deploymentID   string
	vvpNamespace   string
	selector       string
}

// register binds all target-related flags to cmd and marks --target-type as required.
func (tf *targetFlagSet) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&tf.targetType, "target-type", "", "Target type: flinkdeployment, ververica, or podselector (required)")
	cmd.Flags().StringVar(&tf.targetName, "target-name", "", "Deployment name (required for flinkdeployment)")
	cmd.Flags().StringVar(&tf.deploymentName, "deployment-name", "", "Ververica deployment name")
	cmd.Flags().StringVar(&tf.deploymentID, "deployment-id", "", "Ververica deployment UUID")
	cmd.Flags().StringVar(&tf.vvpNamespace, "vvp-namespace", "", "Ververica namespace filter")
	cmd.Flags().StringVar(&tf.selector, "selector", "", "Label selector for podselector target type (e.g. app=my-flink)")
	_ = cmd.MarkFlagRequired("target-type")
}

// build validates the flag values and returns a TargetSpec.
func (tf *targetFlagSet) build() (v1alpha1.TargetSpec, error) {
	target := v1alpha1.TargetSpec{}
	targetType := strings.ToLower(tf.targetType)

	switch targetType {
	case "flinkdeployment":
		if tf.targetName == "" {
			return target, fmt.Errorf("--target-name is required for target type %q", targetType)
		}
		target.Type = v1alpha1.TargetFlinkDeployment
		target.Name = tf.targetName

	case "ververica":
		if tf.deploymentID == "" && tf.deploymentName == "" {
			return target, fmt.Errorf("at least one of --deployment-id or --deployment-name is required for target type %q", targetType)
		}
		target.Type = v1alpha1.TargetVervericaDeployment
		target.DeploymentID = tf.deploymentID
		target.DeploymentName = tf.deploymentName
		target.VVPNamespace = tf.vvpNamespace

	case "podselector":
		if tf.selector == "" {
			return target, fmt.Errorf("--selector is required for target type %q", targetType)
		}
		ls, err := parseSelectorToLabelSelector(tf.selector)
		if err != nil {
			return target, err
		}
		target.Type = v1alpha1.TargetPodSelector
		target.Selector = ls

	default:
		return target, fmt.Errorf("unknown target type %q; valid values: flinkdeployment, ververica, podselector", targetType)
	}

	return target, nil
}

// newRunCmd builds the "run" command with its subcommands.
func newRunCmd() *cobra.Command {
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run a chaos scenario against a Flink workload",
	}

	runCmd.AddCommand(newTMKillCmd())
	runCmd.AddCommand(newNetworkPartitionCmd())
	runCmd.AddCommand(newNetworkChaosCmd())
	runCmd.AddCommand(newResourceExhaustionCmd())
	return runCmd
}

// newTMKillCmd builds the "run tm-kill" subcommand with locally scoped flags.
func newTMKillCmd() *cobra.Command {
	var (
		tmkCount          int32
		tmkDryRun         bool
		tmkObserveTimeout time.Duration
		tmkName           string
		tmkTarget         targetFlagSet
	)

	tmKillCmd := &cobra.Command{
		Use:   "tm-kill",
		Short: "Kill one or more TaskManager pods",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTMKillWithTarget(cmd, tmkName, tmkCount, tmkDryRun, tmkObserveTimeout, &tmkTarget)
		},
	}

	tmkTarget.register(tmKillCmd)
	tmKillCmd.Flags().Int32Var(&tmkCount, "count", 1, "Number of TaskManager pods to kill")
	tmKillCmd.Flags().BoolVar(&tmkDryRun, "dry-run", false, "Log actions without executing them")
	tmKillCmd.Flags().DurationVar(&tmkObserveTimeout, "observe-timeout", 10*time.Minute, "Observation timeout")
	tmKillCmd.Flags().StringVar(&tmkName, "name", "", "Name for the ChaosRun resource; auto-generated when empty")

	return tmKillCmd
}

// runTMKillWithTarget executes the tm-kill logic using the provided parameters.
func runTMKillWithTarget(cmd *cobra.Command, tmkName string, tmkCount int32, tmkDryRun bool, tmkObserveTimeout time.Duration, tf *targetFlagSet) error {
	target, err := tf.build()
	if err != nil {
		return err
	}

	name := tmkName
	if name == "" {
		name = fmt.Sprintf("tm-kill-%d", time.Now().Unix())
	}

	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.ChaosRunSpec{
			Target: target,
			Scenario: v1alpha1.ScenarioSpec{
				Type: v1alpha1.ScenarioTaskManagerPodKill,
				Selection: v1alpha1.SelectionSpec{
					Mode:  v1alpha1.SelectionModeRandom,
					Count: tmkCount,
				},
				Action: v1alpha1.ActionSpec{
					Type: v1alpha1.ActionDeletePod,
				},
			},
			Observe: v1alpha1.ObserveSpec{
				Enabled: true,
				Timeout: metav1.Duration{Duration: tmkObserveTimeout},
			},
			Safety: v1alpha1.SafetySpec{
				DryRun: tmkDryRun,
			},
		},
	}

	c, err := buildClient(kubeconfig)
	if err != nil {
		return err
	}

	if err := c.Create(cmd.Context(), run); err != nil {
		return fmt.Errorf("create ChaosRun: %w", err)
	}

	fmt.Printf("ChaosRun %q created in namespace %q\n", name, namespace)
	return nil
}

// newNetworkPartitionCmd builds the "run network-partition" subcommand with locally scoped flags.
func newNetworkPartitionCmd() *cobra.Command {
	var (
		npNetworkTarget  string
		npDirection      string
		npExternalCIDR   string
		npExternalPort   int32
		npObserveTimeout time.Duration
		npDryRun         bool
		npName           string
		tf               targetFlagSet
	)

	cmd := &cobra.Command{
		Use:   "network-partition",
		Short: "Block network traffic between Flink components using NetworkPolicy",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runNetworkPartition(cmd, npNetworkTarget, npDirection, npExternalCIDR, npExternalPort, npObserveTimeout, npDryRun, npName, &tf)
		},
	}

	tf.register(cmd)
	cmd.Flags().StringVar(&npNetworkTarget, "network-target", "TMtoJM", "Network target pair: TMtoTM, TMtoJM, TMtoCheckpoint, TMtoExternal")
	cmd.Flags().StringVar(&npDirection, "direction", "Both", "Traffic direction: Ingress, Egress, Both")
	cmd.Flags().StringVar(&npExternalCIDR, "external-cidr", "", "CIDR for external endpoint (required for TMtoExternal or TMtoCheckpoint)")
	cmd.Flags().Int32Var(&npExternalPort, "external-port", 0, "Port to restrict for external endpoint (optional)")
	cmd.Flags().DurationVar(&npObserveTimeout, "observe-timeout", 10*time.Minute, "Observation timeout")
	cmd.Flags().BoolVar(&npDryRun, "dry-run", false, "Log actions without executing them")
	cmd.Flags().StringVar(&npName, "name", "", "Name for the ChaosRun resource; auto-generated when empty")

	return cmd
}

// runNetworkPartition is the RunE handler for "run network-partition".
func runNetworkPartition(cmd *cobra.Command, npNetworkTarget, npDirection, npExternalCIDR string, npExternalPort int32, npObserveTimeout time.Duration, npDryRun bool, npName string, tf *targetFlagSet) error {
	target, err := tf.build()
	if err != nil {
		return err
	}

	if err := validateNetworkTarget(npNetworkTarget); err != nil {
		return err
	}
	npNetworkTarget = normaliseNetworkTarget(npNetworkTarget)

	if err := validateNetworkDirection(npDirection); err != nil {
		return err
	}
	npDirection = normaliseNetworkDirection(npDirection)

	if (npNetworkTarget == "TMtoExternal" || npNetworkTarget == "TMtoCheckpoint") && npExternalCIDR == "" {
		return fmt.Errorf("--external-cidr is required for network-target %q", npNetworkTarget)
	}

	name := npName
	if name == "" {
		name = fmt.Sprintf("np-%d", time.Now().Unix())
	}

	networkSpec := &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTarget(npNetworkTarget),
		Direction: v1alpha1.NetworkDirection(npDirection),
	}
	if npExternalCIDR != "" {
		networkSpec.ExternalEndpoint = &v1alpha1.ExternalTarget{
			CIDR: npExternalCIDR,
			Port: npExternalPort,
		}
	}

	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.ChaosRunSpec{
			Target: target,
			Scenario: v1alpha1.ScenarioSpec{
				Type:    v1alpha1.ScenarioNetworkPartition,
				Network: networkSpec,
			},
			Observe: v1alpha1.ObserveSpec{
				Enabled: true,
				Timeout: metav1.Duration{Duration: npObserveTimeout},
			},
			Safety: v1alpha1.SafetySpec{
				DryRun: npDryRun,
			},
		},
	}

	c, err := buildClient(kubeconfig)
	if err != nil {
		return err
	}

	if err := c.Create(cmd.Context(), run); err != nil {
		return fmt.Errorf("create ChaosRun: %w", err)
	}

	fmt.Printf("ChaosRun %q created in namespace %q\n", name, namespace)
	return nil
}

// newNetworkChaosCmd builds the "run network-chaos" subcommand with locally scoped flags.
func newNetworkChaosCmd() *cobra.Command {
	var (
		ncNetworkTarget  string
		ncDirection      string
		ncLatency        time.Duration
		ncJitter         time.Duration
		ncLoss           int32
		ncBandwidth      string
		ncDuration       time.Duration
		ncExternalCIDR   string
		ncExternalPort   int32
		ncObserveTimeout time.Duration
		ncDryRun         bool
		ncName           string
		tf               targetFlagSet
	)

	cmd := &cobra.Command{
		Use:   "network-chaos",
		Short: "Inject network disruption into Flink components using tc netem/tbf",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runNetworkChaos(cmd, ncNetworkTarget, ncDirection, ncLatency, ncJitter, ncLoss, ncBandwidth, ncDuration, ncExternalCIDR, ncExternalPort, ncObserveTimeout, ncDryRun, ncName, &tf)
		},
	}

	tf.register(cmd)
	cmd.Flags().StringVar(&ncNetworkTarget, "network-target", "TMtoJM", "Network target pair: TMtoTM, TMtoJM, TMtoCheckpoint, TMtoExternal")
	cmd.Flags().StringVar(&ncDirection, "direction", "Both", "Traffic direction: Ingress, Egress, Both")
	cmd.Flags().DurationVar(&ncLatency, "latency", 0, "Fixed one-way delay to inject (e.g. 100ms)")
	cmd.Flags().DurationVar(&ncJitter, "jitter", 0, "Random variation added to delay (e.g. 20ms); requires --latency")
	cmd.Flags().Int32Var(&ncLoss, "loss", 0, "Percentage of packets to drop (0–100)")
	cmd.Flags().StringVar(&ncBandwidth, "bandwidth", "", "Maximum egress rate (e.g. 10mbit, 1gbit)")
	cmd.Flags().DurationVar(&ncDuration, "duration", 60*time.Second, "How long the disruption remains active before cleanup")
	cmd.Flags().StringVar(&ncExternalCIDR, "external-cidr", "", "CIDR for external endpoint (optional)")
	cmd.Flags().Int32Var(&ncExternalPort, "external-port", 0, "Port to restrict for external endpoint (optional)")
	cmd.Flags().DurationVar(&ncObserveTimeout, "observe-timeout", 10*time.Minute, "Observation timeout")
	cmd.Flags().BoolVar(&ncDryRun, "dry-run", false, "Log actions without executing them")
	cmd.Flags().StringVar(&ncName, "name", "", "Name for the ChaosRun resource; auto-generated when empty")

	return cmd
}

// runNetworkChaos is the RunE handler for "run network-chaos".
func runNetworkChaos(cmd *cobra.Command, ncNetworkTarget, ncDirection string, ncLatency, ncJitter time.Duration, ncLoss int32, ncBandwidth string, ncDuration time.Duration, ncExternalCIDR string, ncExternalPort int32, ncObserveTimeout time.Duration, ncDryRun bool, ncName string, tf *targetFlagSet) error {
	target, err := tf.build()
	if err != nil {
		return err
	}

	if err := validateNetworkTarget(ncNetworkTarget); err != nil {
		return err
	}
	ncNetworkTarget = normaliseNetworkTarget(ncNetworkTarget)

	if err := validateNetworkDirection(ncDirection); err != nil {
		return err
	}
	ncDirection = normaliseNetworkDirection(ncDirection)

	name := ncName
	if name == "" {
		name = fmt.Sprintf("nc-%d", time.Now().Unix())
	}

	networkSpec := &v1alpha1.NetworkChaosSpec{
		Target:    v1alpha1.NetworkTarget(ncNetworkTarget),
		Direction: v1alpha1.NetworkDirection(ncDirection),
		Bandwidth: ncBandwidth,
	}

	if ncLatency > 0 {
		d := metav1.Duration{Duration: ncLatency}
		networkSpec.Latency = &d
	}
	if ncJitter > 0 {
		d := metav1.Duration{Duration: ncJitter}
		networkSpec.Jitter = &d
	}
	if ncLoss > 0 {
		lossVal := ncLoss
		networkSpec.Loss = &lossVal
	}
	if ncDuration > 0 {
		d := metav1.Duration{Duration: ncDuration}
		networkSpec.Duration = &d
	}
	if ncExternalCIDR != "" {
		networkSpec.ExternalEndpoint = &v1alpha1.ExternalTarget{
			CIDR: ncExternalCIDR,
			Port: ncExternalPort,
		}
	}

	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.ChaosRunSpec{
			Target: target,
			Scenario: v1alpha1.ScenarioSpec{
				Type:    v1alpha1.ScenarioNetworkChaos,
				Network: networkSpec,
			},
			Observe: v1alpha1.ObserveSpec{
				Enabled: true,
				Timeout: metav1.Duration{Duration: ncObserveTimeout},
			},
			Safety: v1alpha1.SafetySpec{
				DryRun: ncDryRun,
			},
		},
	}

	c, err := buildClient(kubeconfig)
	if err != nil {
		return err
	}

	if err := c.Create(cmd.Context(), run); err != nil {
		return fmt.Errorf("create ChaosRun: %w", err)
	}

	fmt.Printf("ChaosRun %q created in namespace %q\n", name, namespace)
	return nil
}

// newResourceExhaustionCmd builds the "run resource-exhaustion" subcommand with locally scoped flags.
func newResourceExhaustionCmd() *cobra.Command {
	var (
		reMode           string
		reWorkers        int32
		reMemoryPercent  int32
		reDuration       time.Duration
		reObserveTimeout time.Duration
		reDryRun         bool
		reName           string
		tf               targetFlagSet
	)

	cmd := &cobra.Command{
		Use:   "resource-exhaustion",
		Short: "Inject CPU or memory stress into Flink TaskManager pods using stress-ng",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runResourceExhaustion(cmd, reMode, reWorkers, reMemoryPercent, reDuration, reObserveTimeout, reDryRun, reName, &tf)
		},
	}

	tf.register(cmd)
	cmd.Flags().StringVar(&reMode, "mode", "", "Stress mode: CPU or Memory (required)")
	cmd.Flags().Int32Var(&reWorkers, "workers", 1, "Number of stress-ng worker goroutines")
	cmd.Flags().Int32Var(&reMemoryPercent, "memory-percent", 80, "Percentage of pod memory to consume (Memory mode only, 1–100)")
	cmd.Flags().DurationVar(&reDuration, "duration", 60*time.Second, "How long the stress injection runs before cleanup")
	cmd.Flags().DurationVar(&reObserveTimeout, "observe-timeout", 10*time.Minute, "Observation timeout")
	cmd.Flags().BoolVar(&reDryRun, "dry-run", false, "Log actions without executing them")
	cmd.Flags().StringVar(&reName, "name", "", "Name for the ChaosRun resource; auto-generated when empty")
	_ = cmd.MarkFlagRequired("mode")

	return cmd
}

// runResourceExhaustion is the RunE handler for "run resource-exhaustion".
func runResourceExhaustion(cmd *cobra.Command, reMode string, reWorkers, reMemoryPercent int32, reDuration, reObserveTimeout time.Duration, reDryRun bool, reName string, tf *targetFlagSet) error {
	target, err := tf.build()
	if err != nil {
		return err
	}

	if err := validateResourceExhaustionMode(reMode); err != nil {
		return err
	}

	name := reName
	if name == "" {
		name = fmt.Sprintf("re-%d", time.Now().Unix())
	}

	dur := metav1.Duration{Duration: reDuration}
	reSpec := &v1alpha1.ResourceExhaustionSpec{
		Mode:     v1alpha1.ResourceExhaustionMode(reMode),
		Workers:  reWorkers,
		Duration: &dur,
	}
	if v1alpha1.ResourceExhaustionMode(reMode) == v1alpha1.ResourceExhaustionModeMemory {
		reSpec.MemoryPercent = reMemoryPercent
	}

	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.ChaosRunSpec{
			Target: target,
			Scenario: v1alpha1.ScenarioSpec{
				Type:               v1alpha1.ScenarioResourceExhaustion,
				ResourceExhaustion: reSpec,
			},
			Observe: v1alpha1.ObserveSpec{
				Enabled: true,
				Timeout: metav1.Duration{Duration: reObserveTimeout},
			},
			Safety: v1alpha1.SafetySpec{
				DryRun: reDryRun,
			},
		},
	}

	c, err := buildClient(kubeconfig)
	if err != nil {
		return err
	}

	if err := c.Create(cmd.Context(), run); err != nil {
		return fmt.Errorf("create ChaosRun: %w", err)
	}

	fmt.Printf("ChaosRun %q created in namespace %q\n", name, namespace)
	return nil
}

// newStopCmd builds the "stop <name>" command.
func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <name>",
		Short: "Abort an in-flight ChaosRun by name",
		Args:  cobra.ExactArgs(1),
		RunE:  runStop,
	}
}

// runStop is the RunE handler for "stop <name>".
func runStop(cmd *cobra.Command, args []string) error {
	name := args[0]

	c, err := buildClient(kubeconfig)
	if err != nil {
		return err
	}

	run := &v1alpha1.ChaosRun{}
	key := types.NamespacedName{Name: name, Namespace: namespace}
	if err := c.Get(cmd.Context(), key, run); err != nil {
		return fmt.Errorf("get ChaosRun %q: %w", name, err)
	}

	patch := client.MergeFrom(run.DeepCopy())
	run.Spec.Control.Abort = true
	if err := c.Patch(cmd.Context(), run, patch); err != nil {
		return fmt.Errorf("patch ChaosRun %q: %w", name, err)
	}

	fmt.Printf("ChaosRun %q abort requested\n", name)
	return nil
}

// newStatusCmd builds the "status <name>" command.
func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <name>",
		Short: "Print the status of a ChaosRun",
		Args:  cobra.ExactArgs(1),
		RunE:  runStatus,
	}
}

// runStatus is the RunE handler for "status <name>".
func runStatus(cmd *cobra.Command, args []string) error {
	name := args[0]

	c, err := buildClient(kubeconfig)
	if err != nil {
		return err
	}

	run := &v1alpha1.ChaosRun{}
	key := types.NamespacedName{Name: name, Namespace: namespace}
	if err := c.Get(cmd.Context(), key, run); err != nil {
		return fmt.Errorf("get ChaosRun %q: %w", name, err)
	}

	s := run.Status
	targetStr := string(run.Spec.Target.Type)
	if s.TargetSummary != nil {
		targetStr = s.TargetSummary.Type + "/" + s.TargetSummary.Name
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Name:\t%s\n", run.Name)
	fmt.Fprintf(w, "Namespace:\t%s\n", run.Namespace)
	fmt.Fprintf(w, "Phase:\t%s\n", s.Phase)
	fmt.Fprintf(w, "Verdict:\t%s\n", s.Verdict)
	fmt.Fprintf(w, "Target:\t%s\n", targetStr)
	fmt.Fprintf(w, "Scenario:\t%s\n", run.Spec.Scenario.Type)
	fmt.Fprintf(w, "Selected:\t%s\n", formatStrings(s.SelectedPods))
	fmt.Fprintf(w, "Injected:\t%s\n", formatStrings(s.InjectedPods))
	fmt.Fprintf(w, "Recovery:\t%v\n", s.ReplacementObserved)
	fmt.Fprintf(w, "Started:\t%s\n", formatTime(s.StartedAt))
	fmt.Fprintf(w, "Ended:\t%s\n", formatTime(s.EndedAt))
	fmt.Fprintf(w, "Message:\t%s\n", s.Message)
	if len(s.NetworkPolicies) > 0 {
		fmt.Fprintf(w, "NetworkPolicies:\t%s\n", formatStrings(s.NetworkPolicies))
	}
	if len(s.EphemeralContainerInjections) > 0 {
		fmt.Fprintf(w, "EphemeralContainers:\t%d injected\n", len(s.EphemeralContainerInjections))
	}
	return w.Flush()
}

// newListCmd builds the "list" command.
func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List ChaosRuns in the namespace",
		Args:  cobra.NoArgs,
		RunE:  runList,
	}
}

// runList is the RunE handler for "list".
func runList(cmd *cobra.Command, _ []string) error {
	c, err := buildClient(kubeconfig)
	if err != nil {
		return err
	}

	runList := &v1alpha1.ChaosRunList{}
	if err := c.List(cmd.Context(), runList, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("list ChaosRuns: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tPHASE\tVERDICT\tTARGET\tAGE")

	for _, run := range runList.Items {
		s := run.Status
		targetStr := string(run.Spec.Target.Type)
		if s.TargetSummary != nil {
			targetStr = s.TargetSummary.Type + "/" + s.TargetSummary.Name
		}
		age := humanAge(run.CreationTimestamp)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			run.Name,
			s.Phase,
			s.Verdict,
			targetStr,
			age,
		)
	}

	return w.Flush()
}

// rootCmd is the top-level cobra command for kubectl-fchaos.
var rootCmd = &cobra.Command{
	Use:   "kubectl-fchaos",
	Short: "CLI plugin for managing Flink chaos experiments",
	Long: `kubectl-fchaos is a kubectl plugin for creating and managing ChaosRun
resources that introduce controlled failures into Apache Flink clusters.`,
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file; defaults to KUBECONFIG env or in-cluster config")

	rootCmd.AddCommand(newRunCmd())
	rootCmd.AddCommand(newStopCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newListCmd())
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
