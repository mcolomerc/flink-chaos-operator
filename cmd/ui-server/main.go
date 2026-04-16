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

// Package main is the entry point for the flink-chaos-operator UI API server.
// It bridges the web frontend to Kubernetes and the Flink REST API.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/apiserver"
)

func main() {
	var (
		port          int
		namespace     string
		flinkEndpoint string
		kubeContext   string
	)

	flag.IntVar(&port, "port", 8090, "Port to listen on")
	flag.StringVar(&namespace, "namespace", namespaceFromEnv(), "Kubernetes namespace to watch")
	flag.StringVar(&flinkEndpoint, "flink-endpoint", "", "Override Flink REST API base URL (e.g. http://flink-jm:8081)")
	flag.StringVar(&kubeContext, "context", "", "Kubernetes context to use (defaults to current context in kubeconfig)")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))

	restCfg, err := buildKubeConfig(kubeContext)
	if err != nil {
		slog.Error("failed to build kubeconfig", "error", err)
		os.Exit(1)
	}

	k8sClient, err := client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		slog.Error("failed to create Kubernetes client", "error", err)
		os.Exit(1)
	}

	srv := apiserver.NewServer(apiserver.Config{
		Client:        k8sClient,
		Namespace:     namespace,
		FlinkEndpoint: flinkEndpoint,
		Port:          port,
		StaticFS:      uiFS(),
	})

	// Honour OS signals for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("starting UI API server", "port", port, "namespace", namespace)

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			slog.Info("server stopped", "error", err)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down UI API server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
		os.Exit(1)
	}
}

// buildKubeConfig returns an in-cluster config, falling back to the kubeconfig
// file. When kubeContext is non-empty it overrides the current context so the
// server can target a specific cluster without changing the global kubectl context.
func buildKubeConfig(kubeContext string) (*rest.Config, error) {
	cfg, err := rest.InClusterConfig()
	if err == nil {
		return cfg, nil
	}
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kc := os.Getenv("KUBECONFIG"); kc != "" {
		loadingRules.ExplicitPath = kc
	}
	overrides := &clientcmd.ConfigOverrides{}
	if kubeContext != "" {
		overrides.CurrentContext = kubeContext
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
}

// namespaceFromEnv reads the pod's namespace from the downward-API env var and
// falls back to "default" when running outside a cluster.
func namespaceFromEnv() string {
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		return ns
	}
	return "default"
}
