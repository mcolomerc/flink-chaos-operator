# Usage Guidelines

Step-by-step end-to-end guides for common chaos engineering workflows with the Flink Chaos Operator.

## Guides

| Guide | Description |
|-------|-------------|
| [Quick Start](quick-start.md) | Install the operator, install the CLI plugin, and run your first experiment |
| [TaskManager Pod Kill](task-manager-pod-kill.md) | Random kill, explicit pod selection, grace periods, checkpoint-aware injection |
| [Network Chaos](network-chaos.md) | Network partition, latency, packet loss, bandwidth throttling |
| [Resource Exhaustion](resource-exhaustion.md) | CPU saturation and memory pressure on TaskManager pods |

## How to Use These Guides

Each guide follows the same structure:

1. **Scenario overview** — what the test validates and how the operator implements it
2. **Numbered scenarios** — concrete `kubectl fchaos` commands or YAML manifests, ordered from simple to advanced
3. **Monitoring tips** — how to observe the Flink job during injection
4. **Interpreting results** — what each verdict means and how to diagnose failures
5. **Common issues** — solutions to frequently encountered problems

Start with the [Quick Start](quick-start.md) if this is your first time using the operator.
