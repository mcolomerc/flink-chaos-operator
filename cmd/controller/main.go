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

// Package main is the entry point for the flink-chaos-operator controller manager.
package main

import (
	"flag"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"k8s.io/client-go/kubernetes"

	v1alpha1 "github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/controller"
	"github.com/flink-chaos-operator/internal/interfaces"
	compositeobserver "github.com/flink-chaos-operator/internal/observer/composite"
	flinkrestobserver "github.com/flink-chaos-operator/internal/observer/flinkrest"
	k8sobserver "github.com/flink-chaos-operator/internal/observer/kubernetes"
	"github.com/flink-chaos-operator/internal/resolver/flinkdeployment"
	"github.com/flink-chaos-operator/internal/resolver/podselector"
	"github.com/flink-chaos-operator/internal/resolver/ververica"
	"github.com/flink-chaos-operator/internal/safety"
	networkchaosdriver "github.com/flink-chaos-operator/internal/scenario/networkchaos"
	networkpartitiondriver "github.com/flink-chaos-operator/internal/scenario/networkpartition"
	resourceexhaustiondriver "github.com/flink-chaos-operator/internal/scenario/resourceexhaustion"
	"github.com/flink-chaos-operator/internal/scenario/tmpodkill"
)

var scheme = runtime.NewScheme()

func init() {
	// Register core Kubernetes types (Pods, Events, etc.).
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	// Register Chaos Operator CRD types.
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	// Ensure corev1 types are present even if clientgoscheme missed them.
	utilruntime.Must(corev1.AddToScheme(scheme))
}

func main() {
	var (
		metricsAddr  string
		probeAddr    string
		enableLeader bool
		namespace    string
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080",
		"The address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081",
		"The address the health probe endpoint binds to.")
	flag.BoolVar(&enableLeader, "leader-elect", false,
		"Enable leader election for the controller manager. "+
			"Enabling this will ensure only one active controller manager.")
	flag.StringVar(&namespace, "namespace", "",
		"The namespace the controller watches. Defaults to all namespaces when empty.")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	logger := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(logger)

	setupLog := ctrl.Log.WithName("setup")

	// Build the manager options. When --namespace is set, restrict the cache
	// to that single namespace for a reduced RBAC footprint.
	mgrOpts := ctrl.Options{
		Scheme:                 scheme,
		LeaderElection:         enableLeader,
		LeaderElectionID:       "flink-chaos-operator.chaos.flink.io",
		HealthProbeBindAddress: probeAddr,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
	}

	if namespace != "" {
		mgrOpts.Cache = cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				namespace: {},
			},
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgrOpts)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	kubeClient, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to create kubernetes client")
		os.Exit(1)
	}

	tcImage := os.Getenv("TC_TOOLS_IMAGE")
	if tcImage == "" {
		tcImage = "ghcr.io/flink-chaos-operator/tc-tools:latest"
	}

	stressImage := os.Getenv("STRESS_IMAGE")
	if stressImage == "" {
		stressImage = "ghcr.io/flink-chaos-operator/stress-tools:latest"
	}

	resolvers := map[v1alpha1.TargetType]interfaces.TargetResolver{
		v1alpha1.TargetFlinkDeployment:    &flinkdeployment.Resolver{Client: mgr.GetClient()},
		v1alpha1.TargetVervericaDeployment: &ververica.Resolver{Client: mgr.GetClient()},
		v1alpha1.TargetPodSelector:         &podselector.Resolver{Client: mgr.GetClient()},
	}

	scenarioDrivers := map[v1alpha1.ScenarioType]interfaces.ScenarioDriver{
		v1alpha1.ScenarioTaskManagerPodKill: tmpodkill.New(mgr.GetClient()),
		v1alpha1.ScenarioNetworkPartition:   networkpartitiondriver.New(mgr.GetClient()),
		v1alpha1.ScenarioNetworkChaos:       networkchaosdriver.New(mgr.GetClient(), kubeClient, tcImage),
		v1alpha1.ScenarioResourceExhaustion: resourceexhaustiondriver.New(mgr.GetClient(), kubeClient, stressImage),
	}

	obs := &compositeobserver.Observer{
		Children: []interfaces.Observer{
			&k8sobserver.Observer{Client: mgr.GetClient()},
			&flinkrestobserver.Observer{NewClient: flinkrestobserver.NewHTTPClient},
		},
	}

	if err := (&controller.ChaosRunReconciler{
		Client:             mgr.GetClient(),
		Scheme:             mgr.GetScheme(),
		Log:                ctrl.Log.WithName("controllers").WithName("ChaosRun"),
		Recorder:           mgr.GetEventRecorderFor("flink-chaos-operator"),
		SafetyChecker:      &safety.Checker{Client: mgr.GetClient()},
		Resolvers:          resolvers,
		ScenarioDrivers:    scenarioDrivers,
		Observer:           obs,
		FlinkClientFactory: flinkrestobserver.NewHTTPClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ChaosRun")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager",
		"metricsAddr", metricsAddr,
		"probeAddr", probeAddr,
		"leaderElect", enableLeader,
		"namespace", namespace,
	)

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
