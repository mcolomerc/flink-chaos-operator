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

package apiserver_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/flink-chaos-operator/api/v1alpha1"
	"github.com/flink-chaos-operator/internal/apiserver"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func buildScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add v1alpha1 to scheme: %v", err)
	}
	return s
}

func newConfig(t *testing.T, objs ...runtime.Object) apiserver.Config {
	t.Helper()
	scheme := buildScheme(t)
	clientObjs := make([]runtime.Object, len(objs))
	copy(clientObjs, objs)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(clientObjs...).
		Build()
	return apiserver.Config{
		Client:    c,
		Namespace: "test-ns",
	}
}

func newServer(t *testing.T, objs ...runtime.Object) *httptest.Server {
	t.Helper()
	cfg := newConfig(t, objs...)
	srv := apiserver.NewServer(cfg)
	return httptest.NewServer(srv.Handler)
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// ── createChaosRunHandler ─────────────────────────────────────────────────────

func TestCreateChaosRun_MissingTargetName(t *testing.T) {
	g := NewWithT(t)
	ts := newServer(t)
	defer ts.Close()

	resp := postJSON(t, ts.URL+"/api/chaosruns", map[string]any{
		"scenarioType": "TaskManagerPodKill",
	})
	g.Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
}

func TestCreateChaosRun_MissingScenarioType(t *testing.T) {
	g := NewWithT(t)
	ts := newServer(t)
	defer ts.Close()

	resp := postJSON(t, ts.URL+"/api/chaosruns", map[string]any{
		"targetName": "my-deployment",
	})
	g.Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
}

func TestCreateChaosRun_UnknownScenarioType(t *testing.T) {
	g := NewWithT(t)
	ts := newServer(t)
	defer ts.Close()

	resp := postJSON(t, ts.URL+"/api/chaosruns", map[string]any{
		"targetName":   "my-deployment",
		"scenarioType": "BogusScenario",
	})
	g.Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
}

func TestCreateChaosRun_UnknownSelectionMode(t *testing.T) {
	g := NewWithT(t)
	ts := newServer(t)
	defer ts.Close()

	resp := postJSON(t, ts.URL+"/api/chaosruns", map[string]any{
		"targetName":    "my-deployment",
		"scenarioType":  "TaskManagerPodKill",
		"selectionMode": "NotARealMode",
	})
	g.Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
}

func TestCreateChaosRun_ValidRequest_Returns201(t *testing.T) {
	g := NewWithT(t)
	ts := newServer(t)
	defer ts.Close()

	resp := postJSON(t, ts.URL+"/api/chaosruns", map[string]any{
		"targetName":   "my-deployment",
		"scenarioType": "TaskManagerPodKill",
	})
	g.Expect(resp.StatusCode).To(Equal(http.StatusCreated))

	var result map[string]string
	decodeJSON(t, resp, &result)
	g.Expect(result["name"]).NotTo(BeEmpty())
	g.Expect(result["namespace"]).To(Equal("test-ns"))
}

func TestCreateChaosRun_BodyTooLarge_Returns400(t *testing.T) {
	g := NewWithT(t)
	ts := newServer(t)
	defer ts.Close()

	// Build a body larger than 1 MiB.
	large := strings.Repeat("x", 1<<20+1)
	payload := `{"targetName":"` + large + `","scenarioType":"TaskManagerPodKill"}`
	resp, err := http.Post(ts.URL+"/api/chaosruns", "application/json", strings.NewReader(payload))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
}

// ── buildNetworkChaosSpec ─────────────────────────────────────────────────────

func TestBuildNetworkChaosSpec_Defaults(t *testing.T) {
	g := NewWithT(t)
	req := apiserver.CreateChaosRunRequest{
		TargetName:   "my-deployment",
		ScenarioType: "NetworkChaos",
	}
	spec := apiserver.BuildNetworkChaosSpec(req)
	g.Expect(string(spec.Target)).To(Equal("TMtoJM"))
	g.Expect(string(spec.Direction)).To(Equal("Both"))
	g.Expect(spec.ExternalEndpoint).To(BeNil())
	g.Expect(spec.Latency).To(BeNil())
}

func TestBuildNetworkChaosSpec_WithLatencyAndLoss(t *testing.T) {
	g := NewWithT(t)
	loss := int32(10)
	req := apiserver.CreateChaosRunRequest{
		TargetName:   "my-deployment",
		ScenarioType: "NetworkChaos",
		LatencyMs:    100,
		JitterMs:     20,
		LossPercent:  &loss,
		Bandwidth:    "10mbit",
	}
	spec := apiserver.BuildNetworkChaosSpec(req)
	g.Expect(spec.Latency.Duration).To(Equal(100 * time.Millisecond))
	g.Expect(spec.Jitter.Duration).To(Equal(20 * time.Millisecond))
	g.Expect(*spec.Loss).To(Equal(int32(10)))
	g.Expect(spec.Bandwidth).To(Equal("10mbit"))
}

func TestBuildNetworkChaosSpec_ExternalEndpoint(t *testing.T) {
	g := NewWithT(t)
	req := apiserver.CreateChaosRunRequest{
		ExternalHostname: "s3.amazonaws.com",
		ExternalCIDR:     "52.216.0.0/15",
		ExternalPort:     443,
	}
	spec := apiserver.BuildNetworkChaosSpec(req)
	g.Expect(spec.ExternalEndpoint).NotTo(BeNil())
	g.Expect(spec.ExternalEndpoint.Hostname).To(Equal("s3.amazonaws.com"))
	g.Expect(spec.ExternalEndpoint.CIDR).To(Equal("52.216.0.0/15"))
	g.Expect(spec.ExternalEndpoint.Port).To(Equal(int32(443)))
}

// ── buildResourceExhaustionSpec ───────────────────────────────────────────────

func TestBuildResourceExhaustionSpec_Defaults(t *testing.T) {
	g := NewWithT(t)
	req := apiserver.CreateChaosRunRequest{}
	spec := apiserver.BuildResourceExhaustionSpec(req)
	g.Expect(string(spec.Mode)).To(Equal("CPU"))
	g.Expect(spec.Workers).To(Equal(int32(1)))
	g.Expect(spec.MemoryPercent).To(Equal(int32(80)))
	g.Expect(spec.Duration).To(BeNil())
}

func TestBuildResourceExhaustionSpec_WithDuration(t *testing.T) {
	g := NewWithT(t)
	req := apiserver.CreateChaosRunRequest{
		ResourceMode:    "Memory",
		Workers:         4,
		MemoryPercent:   90,
		DurationSeconds: 120,
	}
	spec := apiserver.BuildResourceExhaustionSpec(req)
	g.Expect(string(spec.Mode)).To(Equal("Memory"))
	g.Expect(spec.Workers).To(Equal(int32(4)))
	g.Expect(spec.MemoryPercent).To(Equal(int32(90)))
	g.Expect(spec.Duration.Duration).To(Equal(120 * time.Second))
}

// ── toChaosRunSummary / toChaosRunDetail ──────────────────────────────────────

func TestToChaosRunSummary_BasicFields(t *testing.T) {
	g := NewWithT(t)
	now := metav1.NewTime(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{Name: "run-1", Namespace: "test-ns"},
		Spec: v1alpha1.ChaosRunSpec{
			Target:   v1alpha1.TargetSpec{Type: v1alpha1.TargetFlinkDeployment, Name: "my-app"},
			Scenario: v1alpha1.ScenarioSpec{Type: v1alpha1.ScenarioTaskManagerPodKill},
		},
		Status: v1alpha1.ChaosRunStatus{
			Phase:     v1alpha1.PhaseCompleted,
			Verdict:   v1alpha1.VerdictPassed,
			StartedAt: &now,
		},
	}
	summary := apiserver.ToChaosRunSummary(run)
	g.Expect(summary.Name).To(Equal("run-1"))
	g.Expect(summary.Phase).To(Equal("Completed"))
	g.Expect(summary.Verdict).To(Equal("Passed"))
	g.Expect(summary.Scenario).To(Equal("TaskManagerPodKill"))
	g.Expect(summary.StartedAt).To(Equal("2026-01-15T12:00:00Z"))
	g.Expect(summary.EndedAt).To(BeEmpty())
}

func TestToChaosRunSummary_TargetSummaryOverridesType(t *testing.T) {
	g := NewWithT(t)
	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{Name: "run-2"},
		Spec: v1alpha1.ChaosRunSpec{
			Target:   v1alpha1.TargetSpec{Type: v1alpha1.TargetVervericaDeployment},
			Scenario: v1alpha1.ScenarioSpec{Type: v1alpha1.ScenarioNetworkChaos},
		},
		Status: v1alpha1.ChaosRunStatus{
			TargetSummary: &v1alpha1.TargetSummary{Type: "VervericaDeployment", Name: "my-vvp-app"},
		},
	}
	summary := apiserver.ToChaosRunSummary(run)
	g.Expect(summary.Target).To(Equal("VervericaDeployment/my-vvp-app"))
}

func TestToChaosRunDetail_ObservationFields(t *testing.T) {
	g := NewWithT(t)
	recoveryTime := int64(42)
	observedAt := metav1.NewTime(time.Date(2026, 1, 15, 12, 5, 0, 0, time.UTC))
	timeout := metav1.Duration{Duration: 5 * time.Minute}
	run := &v1alpha1.ChaosRun{
		ObjectMeta: metav1.ObjectMeta{Name: "run-3"},
		Spec: v1alpha1.ChaosRunSpec{
			Scenario: v1alpha1.ScenarioSpec{Type: v1alpha1.ScenarioTaskManagerPodKill},
			Observe:  v1alpha1.ObserveSpec{Enabled: true, Timeout: timeout},
		},
		Status: v1alpha1.ChaosRunStatus{
			InjectedPods:    []string{"pod-a", "pod-b"},
			NetworkPolicies: []string{"np-1"},
			Message:         "recovered",
			Observation: &v1alpha1.ObservationStatus{
				TaskManagerCountBefore: 3,
				TaskManagerCountAfter:  3,
				RecoveryTimeSeconds:    &recoveryTime,
				RecoveryObservedAt:     &observedAt,
			},
		},
	}
	detail := apiserver.ToChaosRunDetail(run)
	g.Expect(detail.InjectedPods).To(ConsistOf("pod-a", "pod-b"))
	g.Expect(detail.NetworkPolicies).To(ConsistOf("np-1"))
	g.Expect(detail.Message).To(Equal("recovered"))
	g.Expect(detail.TMCountBefore).To(Equal(3))
	g.Expect(detail.TMCountAfter).To(Equal(3))
	g.Expect(*detail.RecoveryTimeSeconds).To(Equal(int64(42)))
	g.Expect(detail.RecoveryObservedAt).To(Equal("2026-01-15T12:05:00Z"))
	g.Expect(detail.ObservationTimeoutSecs).To(Equal(int64(300)))
}

// ── deduplicateConnections ────────────────────────────────────────────────────

func TestDeduplicateConnections_RemovesDuplicates(t *testing.T) {
	g := NewWithT(t)
	input := []apiserver.ExternalConnection{
		{Type: "S3", Endpoint: "s3://bucket/checkpoints", Purpose: "checkpoint"},
		{Type: "S3", Endpoint: "s3://bucket/checkpoints", Purpose: "checkpoint"},
		{Type: "S3", Endpoint: "s3://bucket/savepoints", Purpose: "savepoint"},
	}
	out := apiserver.DeduplicateConnections(input)
	g.Expect(out).To(HaveLen(2))
}

func TestDeduplicateConnections_PreservesOrder(t *testing.T) {
	g := NewWithT(t)
	input := []apiserver.ExternalConnection{
		{Type: "S3", Endpoint: "s3://a", Purpose: "checkpoint"},
		{Type: "GCS", Endpoint: "gs://b", Purpose: "savepoint"},
	}
	out := apiserver.DeduplicateConnections(input)
	g.Expect(out[0].Endpoint).To(Equal("s3://a"))
	g.Expect(out[1].Endpoint).To(Equal("gs://b"))
}

// ── storageType ───────────────────────────────────────────────────────────────

func TestStorageType_S3Variants(t *testing.T) {
	g := NewWithT(t)
	g.Expect(apiserver.StorageType("s3://my-bucket/path")).To(Equal("S3"))
	g.Expect(apiserver.StorageType("s3a://my-bucket/path")).To(Equal("S3"))
	g.Expect(apiserver.StorageType("s3n://my-bucket/path")).To(Equal("S3"))
}

func TestStorageType_GCS(t *testing.T) {
	g := NewWithT(t)
	g.Expect(apiserver.StorageType("gs://my-bucket/path")).To(Equal("GCS"))
}

func TestStorageType_HDFS(t *testing.T) {
	g := NewWithT(t)
	g.Expect(apiserver.StorageType("hdfs://namenode/path")).To(Equal("HDFS"))
}

func TestStorageType_AzureBlob(t *testing.T) {
	g := NewWithT(t)
	g.Expect(apiserver.StorageType("wasbs://container@account.blob.core.windows.net/path")).To(Equal("AzureBlob"))
	g.Expect(apiserver.StorageType("abfs://container@account.dfs.core.windows.net/path")).To(Equal("AzureBlob"))
}

func TestStorageType_Unknown(t *testing.T) {
	g := NewWithT(t)
	g.Expect(apiserver.StorageType("file:///local/path")).To(Equal("Unknown"))
	g.Expect(apiserver.StorageType("")).To(Equal("Unknown"))
}

// ── resolveFlinkClient ────────────────────────────────────────────────────────

func TestResolveFlinkClient_ExplicitEndpointSkipsDiscovery(t *testing.T) {
	g := NewWithT(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jobs":[]}`))
	}))
	defer ts.Close()

	cfg := newConfig(t)
	cfg.FlinkEndpoint = ts.URL
	srv := apiserver.NewServer(cfg)
	httpSrv := httptest.NewServer(srv.Handler)
	defer httpSrv.Close()

	resp, err := http.Get(httpSrv.URL + "/api/flink/jobs")
	g.Expect(err).NotTo(HaveOccurred())
	// The handler proxies to FlinkEndpoint — as long as it doesn't 500 from a
	// "no jobmanager found" error, the explicit-endpoint path works.
	g.Expect(resp.StatusCode).To(Or(Equal(http.StatusOK), Equal(http.StatusServiceUnavailable)))
}

// ── path value validation (RFC 1123) ─────────────────────────────────────────

func TestGetChaosRun_InvalidName_Returns400(t *testing.T) {
	g := NewWithT(t)
	ts := newServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/chaosruns/INVALID_NAME!")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
}

func TestAbortChaosRun_InvalidName_Returns400(t *testing.T) {
	g := NewWithT(t)
	ts := newServer(t)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/chaosruns/INVALID!/abort", nil)
	resp, err := http.DefaultClient.Do(req)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
}

func TestDeleteChaosRun_InvalidName_Returns400(t *testing.T) {
	g := NewWithT(t)
	ts := newServer(t)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/chaosruns/INVALID!", nil)
	resp, err := http.DefaultClient.Do(req)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
}

func TestGetChaosRun_NotFound_Returns404(t *testing.T) {
	g := NewWithT(t)
	ts := newServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/chaosruns/does-not-exist")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
}

// ── CORS middleware ───────────────────────────────────────────────────────────

func TestCORSHeaders_PresentOnAllResponses(t *testing.T) {
	g := NewWithT(t)
	ts := newServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(resp.Header.Get("Access-Control-Allow-Origin")).To(Equal("*"))
}

func TestCORSHeaders_PreflightReturns204(t *testing.T) {
	g := NewWithT(t)
	ts := newServer(t)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/api/chaosruns", nil)
	resp, err := http.DefaultClient.Do(req)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
}
