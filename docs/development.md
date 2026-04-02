# Development

## Prerequisites

- Go 1.25+
- Make
- Docker
- A Kubernetes cluster (minikube or kind)

## Building

```bash
# Generate deepcopy and CRD manifests
make generate

# Format and lint
make fmt
make lint

# Build controller and CLI binaries
make build

# Build Docker image
make docker-build
```

## Testing

```bash
# Unit tests with race detection
make test

# Integration tests (requires envtest)
make integration-test

# All tests
make test-all

# End-to-end tests (requires a live cluster)
make e2e-test
```

## Project Structure

```
.
├── api/
│   └── v1alpha1/               # ChaosRun CRD types and validation
├── cmd/
│   ├── controller/             # Controller entry point
│   └── kubectl-fchaos/         # CLI entry point
├── internal/
│   ├── controller/             # Reconciliation logic
│   ├── resolver/
│   │   ├── flinkdeployment/    # FlinkDeployment resolver
│   │   ├── ververica/          # VervericaDeployment resolver
│   │   └── podselector/        # PodSelector resolver
│   ├── scenario/
│   │   ├── tmpodkill/          # TaskManagerPodKill driver
│   │   ├── networkpartition/   # NetworkPartition driver
│   │   ├── networkchaos/       # NetworkChaos driver (tc netem/tbf)
│   │   └── resourceexhaustion/ # ResourceExhaustion driver (stress-ng)
│   ├── observer/
│   │   ├── composite/          # Composite observer (aggregates children)
│   │   ├── kubernetes/         # Kubernetes pod observation
│   │   └── flinkrest/          # Flink REST API observation
│   ├── safety/                 # Safety checker
│   └── interfaces/             # Pluggable interfaces
├── config/
│   ├── crd/                    # CRD YAML manifests
│   ├── rbac/                   # RBAC manifests
│   └── samples/                # Example ChaosRun resources
├── charts/
│   └── flink-chaos-operator/   # Helm chart
└── docs/                       # Documentation
```

## Code Style

- Follow [Effective Go](https://golang.org/doc/effective_go) conventions.
- Use `gofmt` for formatting (enforced by `make fmt`).
- Use `golangci-lint` for linting (`make lint`).
- Write unit tests for all new logic.

## Contributing

1. Fork or create a branch.
2. Make changes and write tests.
3. Run `make fmt lint test` to validate.
4. Open a pull request with a clear description.
