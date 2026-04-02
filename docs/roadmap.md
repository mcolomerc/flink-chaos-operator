# Roadmap

## Completed

### Phase 1 — Core (MVP)
- `TaskManagerPodKill` scenario
- `FlinkDeployment` and `VervericaDeployment` target types
- `kubectl fchaos` CLI plugin
- Safety guardrails (denylist, concurrency, MinTaskManagers, dry-run)
- Kubernetes + optional Flink REST observation

### Phase 2 — Network Chaos
- `NetworkPartition` scenario (Kubernetes NetworkPolicy)
- `NetworkChaos` scenario (ephemeral containers + `tc netem/tbf`)
- `CleaningUp` lifecycle phase with crash-safe cleanup
- Checkpoint-aware injection trigger
- `PodSelector` target type

### Phase 3 — Resource Exhaustion
- `ResourceExhaustion` scenario (CPU/Memory via `stress-ng`)
- Composite observer pattern
- Flink REST checkpoint observation

---

## Planned

### Phase 4
- **Pod eviction action** — Evict pods instead of direct deletion.
- **Serial deletion** — Sequential pod kills instead of burst.
- **Richer Flink REST signals** — Checkpoint failure detection, backpressure monitoring.
- **Schedules** — Recurring chaos runs on a cron schedule.

### Phase 5
- **Scenario templates** — Reusable experiment definitions.
- **Web UI** — Dashboard for run management and history.
- **Multi-namespace/cluster mode** — Operator coordination across namespaces.
- **Direct Ververica Platform API resolver**.
