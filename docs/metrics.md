# Metrics

The operator exposes Prometheus metrics on port `8080` at `/metrics`.

## Available Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `fchaos_runs_total` | Counter | `scenario`, `phase`, `verdict` | Total chaos runs by scenario, final phase, and verdict. |
| `fchaos_run_duration_seconds` | Histogram | `scenario` | Duration of completed chaos runs. |
| `fchaos_injections_total` | Counter | `scenario` | Total successful chaos injections. |
| `fchaos_abort_requests_total` | Counter | — | Total abort requests received. |
| `fchaos_recovery_observed_total` | Counter | `scenario` | Total runs where recovery was observed. |

## Example Queries

```promql
# Runs that passed in the last hour
increase(fchaos_runs_total{verdict="Passed"}[1h])

# Average run duration by scenario
rate(fchaos_run_duration_seconds_sum[5m]) / rate(fchaos_run_duration_seconds_count[5m])

# Success rate
increase(fchaos_runs_total{verdict="Passed"}[1h]) / increase(fchaos_runs_total{phase="Completed"}[1h])
```

## Helm Configuration

```bash
helm install fchaos ./charts/flink-chaos-operator \
  --set metrics.enabled=true
```
