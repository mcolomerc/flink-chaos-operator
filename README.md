# Flink Chaos Operator

<img src="logo.png" alt="Flink Chaos Operator" width="25%"/>

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue)](https://golang.org)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-1.25%2B-blue)](https://kubernetes.io)

A Kubernetes-native chaos engineering tool for Apache Flink. Inject controlled failures into Flink workloads and validate recovery behavior — safely, without privileged node agents or DaemonSets.

## Scenarios

| Scenario | Mechanism | Description |
|----------|-----------|-------------|
| `TaskManagerPodKill` | Kubernetes pod delete | Kill one or more TaskManager pods and monitor recovery. |
| `NetworkPartition` | Kubernetes NetworkPolicy | Binary traffic isolation between Flink components. |
| `NetworkChaos` | Ephemeral container + `tc` | Latency, jitter, packet loss, and bandwidth limits. |
| `ResourceExhaustion` | Ephemeral container + `stress-ng` | CPU or memory exhaustion on TaskManager pods. |

## Target Types

- **FlinkDeployment** — Apache Flink Kubernetes Operator workloads
- **VervericaDeployment** — Ververica Platform-managed deployments (Kubernetes-native discovery)
- **PodSelector** — Custom label-based pod selection

## Quick Start

```bash
# Deploy the operator
make deploy

# Install the CLI
sudo cp bin/kubectl-fchaos /usr/local/bin/

# Kill 1 random TaskManager
kubectl fchaos run tm-kill \
  -n <namespace> \
  --target-type flinkdeployment \
  --target-name <deployment-name> \
  --count 1

# Watch progress
kubectl fchaos status tm-kill -n <namespace> --watch

# List all runs
kubectl fchaos list -n <namespace>

# Stop a run
kubectl fchaos stop tm-kill -n <namespace>
```

## Architecture

```
┌─────────────┐    ┌─────────────────┐
│  kubectl    │    │   Web UI        │
│   fchaos    │    │  (ui-server)    │
└──────┬──────┘    └────────┬────────┘
       │ creates             │ reads/writes
       v                     v
┌──────────────────────────────────────┐
│  ChaosRun (CRD)                      │
└──────┬───────────────────────────────┘
       v
┌──────────────────────────────────────┐
│  Reconciliation Controller           │
│  Pending → Validating → Injecting    │
│  → Observing → CleaningUp → Done     │
└──────┬───────────────────────────────┘
       │
       ├─────────────┬──────────────┬─────────────┐
       v             v              v             v
  Target         Safety         Scenario      Observer
  Resolvers      Checker        Driver        (K8s + Flink REST)
```

## Prerequisites

- Go 1.21+ (for building from source)
- Kubernetes 1.25+
- kubectl
- Docker (for building container images)

## Installation

### Helm (recommended)

```bash
# Operator only
helm install fchaos ./charts/flink-chaos-operator -n streaming --create-namespace

# Operator + Web UI
helm install fchaos ./charts/flink-chaos-operator -n streaming --create-namespace \
  --set ui.enabled=true \
  --set ui.ingress.enabled=true \
  --set ui.ingress.host=flink-chaos.mycompany.com
```

Key values:

| Value | Default | Description |
|-------|---------|-------------|
| `watchNamespace` | `""` | Namespace to watch (empty = release namespace). |
| `installCRD` | `true` | Install the CRD automatically. |
| `networkchaos.tcImage` | `mcolomervv/flink-chaos-tc-tools:latest` | Image for `tc` tools (NetworkChaos). |
| `resourceExhaustion.stressImage` | `mcolomervv/flink-chaos-stress-tools:latest` | Image for `stress-ng` (ResourceExhaustion). |
| `ui.enabled` | `false` | Deploy the Web UI alongside the operator. |
| `ui.image.repository` | `mcolomervv/flink-chaos-ui-server` | UI server image. |
| `ui.flinkEndpoint` | `""` | Override Flink REST URL; auto-discovers when empty. |
| `ui.ingress.enabled` | `false` | Create an Ingress for the UI. |
| `ui.ingress.host` | `flink-chaos-ui.localhost` | Ingress hostname. |
| `defaults.safety.maxConcurrentRunsPerTarget` | `1` | Maximum concurrent runs per target. |
| `defaults.safety.minTaskManagersRemaining` | `1` | Minimum TaskManagers after injection. |

### Build from Source

```bash
make build              # controller + CLI binaries
make build-ui-server    # React app + ui-server binary
make docker-build       # operator Docker image
make docker-build-ui    # ui-server Docker image
make deploy             # apply CRD + operator manifests (no Helm)
make deploy-ui          # apply UI manifests (no Helm)
sudo cp bin/kubectl-fchaos /usr/local/bin/
```

### Accessing the UI

```bash
kubectl port-forward -n <namespace> deployment/<release>-flink-chaos-operator-ui 8090:8090
# Open http://localhost:8090
```

### UI Features

![Flink Chaos Operator Dashboard](docs/screenshots/dashboard.png)

The web interface includes:
- **Topology diagram** — Real-time 3-level view: JobManagers → TaskManagers → Storage buckets
- **Live metrics** — CPU and memory per pod displayed on the diagram nodes
- **Chaos wizard** — Step-by-step experiment creation with form validation
- **Experiment history** — Active and completed runs in a sidebar; click to view detailed results
- **Metrics panel** — Checkpoint age, throughput, job stats, and checkpoint counters
- **Result comparison** — Before/during/after metrics for each experiment

## Configuration

### ChaosRun Resource

The `ChaosRun` CRD defines a chaos experiment. See `config/crd/bases/` for the complete schema.

Key fields:
- `spec.target` — Target FlinkDeployment, VervericaDeployment, or pods by label
- `spec.scenario` — Chaos scenario (TaskManagerPodKill, NetworkPartition, NetworkChaos, ResourceExhaustion)
- `spec.duration` — How long to run the chaos (e.g., `5m`)
- `spec.count` — Number of pods to target
- `status` — Current state (Pending, Validating, Injecting, Observing, CleaningUp, Done)

Example manifests are in `examples/` directory.

## Usage Examples

Run chaos via the CLI:

```bash
# Kill TaskManagers
kubectl fchaos run kill-tm \
  -n streaming \
  --target-type flinkdeployment \
  --target-name my-app \
  --scenario task-manager-pod-kill \
  --count 2 \
  --duration 2m

# Apply network latency
kubectl fchaos run add-latency \
  -n streaming \
  --target-type flinkdeployment \
  --target-name my-app \
  --scenario network-chaos \
  --network-latency 100ms \
  --network-jitter 10ms \
  --duration 3m

# Exhaust CPU
kubectl fchaos run cpu-exhaust \
  -n streaming \
  --target-type flinkdeployment \
  --target-name my-app \
  --scenario resource-exhaustion \
  --resource-type cpu \
  --duration 1m
```

Or define experiments as ChaosRun manifests and apply them:

```bash
kubectl apply -f examples/task-manager-pod-kill.yaml
kubectl apply -f examples/network-chaos.yaml
kubectl apply -f examples/resource-exhaustion.yaml
```

## Testing

```bash
# Unit tests
make test

# Integration tests (uses envtest)
make integration-test

# End-to-end tests (requires a live cluster)
make e2e-test

# All tests
make test-all
```

## RBAC and Security

The operator requires a ClusterRole with these permissions:

- `chaosruns` (chaos.flink.io) — get, list, watch, create, update, patch, delete
- `pods` (core) — get, list, watch, delete
- `services` (core) — get, list, watch
- `networkpolicies` (networking.k8s.io) — create, delete
- `events` (core) — create, patch

The UI server requires similar permissions plus read access to FlinkDeployments.

See `config/ui/role.yaml` for the UI server role and `docs/rbac.md` for operator RBAC details.

## Development

### Project Structure

```
.
├── api/                          # CRD API definitions
├── cmd/
│   ├── controller/              # Operator controller entry point
│   ├── kubectl-fchaos/          # CLI plugin
│   └── ui-server/               # Web UI backend
├── config/
│   ├── crd/bases/               # Generated CRD manifests
│   ├── rbac.yaml                # Operator RBAC
│   └── ui/                       # UI deployment manifests
├── internal/
│   ├── controller/              # Reconciliation logic
│   ├── target/                  # Target resolver implementations
│   ├── scenario/                # Chaos scenario drivers
│   └── observer/                # Flink metrics collection
├── ui/                          # React frontend
├── examples/                     # ChaosRun example manifests
├── Makefile                      # Build and deploy targets
└── CLAUDE.md                     # Claude Code guidance
```

### Code Style

- Go: Follow [Effective Go](https://golang.org/doc/effective_go)
- Formatting: `make fmt`
- Linting: `make lint`
- Tests: Unit and integration tests are required for new features

## Further Documentation

| Topic | Link |
|-------|------|
| Quick Start Guide | [docs/guidelines/quick-start.md](docs/guidelines/quick-start.md) |
| ChaosRun API Reference | [docs/api-reference.md](docs/api-reference.md) |
| Target Types | [docs/target-types.md](docs/target-types.md) |
| Scenario: TaskManagerPodKill | [docs/scenarios/task-manager-pod-kill.md](docs/scenarios/task-manager-pod-kill.md) |
| Scenario: NetworkPartition / NetworkChaos | [docs/scenarios/network-chaos.md](docs/scenarios/network-chaos.md) |
| Scenario: ResourceExhaustion | [docs/scenarios/resource-exhaustion.md](docs/scenarios/resource-exhaustion.md) |
| Web UI | [docs/ui.md](docs/ui.md) |
| CLI Reference | [docs/cli-reference.md](docs/cli-reference.md) |
| Safety Guardrails | [docs/safety.md](docs/safety.md) |
| Metrics and Observability | [docs/metrics.md](docs/metrics.md) |
| RBAC and Security | [docs/rbac.md](docs/rbac.md) |
| Troubleshooting | [docs/troubleshooting.md](docs/troubleshooting.md) |
| Development | [docs/development.md](docs/development.md) |
| Roadmap | [docs/roadmap.md](docs/roadmap.md) |

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.
