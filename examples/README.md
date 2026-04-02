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

## Tool images

`NetworkChaos` and `ResourceExhaustion` use ephemeral containers with specific
tools. Both images are built from sources in `hack/` and published to Docker Hub
automatically with every release — no manual setup required:

| Helm value | Default image | Used by |
|------------|---------------|---------|
| `networkchaos.tcImage` | `mcolomervv/flink-chaos-tc-tools:latest` | NetworkChaos |
| `resourceExhaustion.stressImage` | `mcolomervv/flink-chaos-stress-tools:latest` | ResourceExhaustion |

To override either image, pass it via Helm:

```bash
helm upgrade fchaos ./charts/flink-chaos-operator \
  --set networkchaos.tcImage=<custom>/tc-tools:tag \
  --set resourceExhaustion.stressImage=<custom>/stress-tools:tag \
  -n <operator-namespace>
```
