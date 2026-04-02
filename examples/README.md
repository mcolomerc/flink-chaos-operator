# Examples

Ready-to-run chaos experiments. Each example includes YAML manifests and a
step-by-step README.

| Example | Scenario | What it tests |
|---------|----------|---------------|
| [tm-pod-kill](tm-pod-kill/) | `TaskManagerPodKill` | Kill one or more TM pods and observe recovery |
| [network-partition](network-partition/) | `NetworkPartition` | Block all traffic between Flink components via NetworkPolicy |
| [network-chaos](network-chaos/) | `NetworkChaos` | Inject latency, packet loss, or bandwidth limits via tc netem |
| [resource-exhaustion](resource-exhaustion/) | `ResourceExhaustion` | Saturate CPU or memory on TM pods via stress-ng |

---

## How to use

1. Install the operator — see [Quick Start](../docs/guidelines/quick-start.md)
2. Browse to the example folder
3. Edit the YAML to match your namespace and labels
4. Follow the README in that folder

## Choosing the right scenario

| I want to test… | Use |
|-----------------|-----|
| Flink restarts a TM and resumes the job | `TaskManagerPodKill` |
| Flink reconnects after a complete network outage | `NetworkPartition` |
| Flink tolerates high latency or flaky networks | `NetworkChaos` — latency / packet loss |
| Flink handles slow external sinks | `NetworkChaos` — bandwidth limit |
| Flink continues under CPU pressure | `ResourceExhaustion` — CPU |
| Flink survives memory pressure / GC pauses | `ResourceExhaustion` — Memory |

## Tool image requirements

`NetworkChaos` and `ResourceExhaustion` require ephemeral container images with
specific tools. The defaults are placeholder images — build your own for local use:

```bash
# tc-tools (for NetworkChaos)
cat > Dockerfile.tc-tools <<'EOF'
FROM alpine:3.19
RUN apk add --no-cache iproute2
EOF
docker build -t <your-registry>/tc-tools:latest -f Dockerfile.tc-tools .

# stress-tools (for ResourceExhaustion)
cat > Dockerfile.stress-tools <<'EOF'
FROM alpine:3.19
RUN apk add --no-cache stress-ng
EOF
docker build -t <your-registry>/stress-tools:latest -f Dockerfile.stress-tools .
```

Then configure the operator:

```bash
helm upgrade fchaos ./charts/flink-chaos-operator \
  --set networkchaos.tcImage=<your-registry>/tc-tools:latest \
  --set resourceExhaustion.stressImage=<your-registry>/stress-tools:latest \
  -n <operator-namespace>
```
