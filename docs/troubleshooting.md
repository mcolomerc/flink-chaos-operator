# Troubleshooting

## Run Stuck in Validating Phase

**Symptom:** A ChaosRun remains in `Validating` indefinitely.

**Diagnosis:**
```bash
kubectl logs -n streaming -l app=flink-chaos-operator
kubectl get flinkdeployments -n streaming
kubectl describe chaosrun <name> -n streaming
```

**Resolution:**
- Ensure the target exists in the same namespace as the ChaosRun.
- Verify the operator has RBAC permissions to read the target resource.
- Check that `minTaskManagersRemaining` is not higher than the actual TM count.

---

## Run Stuck in Injecting Phase

**Symptom:** Pod deletion or ephemeral container creation appears stuck.

**Diagnosis:**
```bash
kubectl get pods -n streaming --field-selector=status.phase=Failed
kubectl describe pod <pod-name> -n streaming
```

**Resolution:**
- Pods may take time to shut down. Check `gracePeriodSeconds`.
- Verify no admission controllers or webhooks are blocking pod deletion.
- For NetworkChaos/ResourceExhaustion: check that the cluster supports ephemeral containers (Kubernetes 1.23+).

---

## Recovery Not Observed

**Symptom:** Run completes with `Failed` or `Inconclusive` despite pods being killed.

**Possible causes:**
1. Replacement pods not starting — check ResourceQuotas, PVC availability, or node capacity.
2. Flink REST unreachable — network policy issue or endpoint misconfiguration.
3. Job in unhealthy state — check Flink JobManager logs.
4. Observation timeout too short — increase `spec.observe.timeout`.

**Diagnosis:**
```bash
kubectl get pods -n streaming -l app=<target-name>
kubectl fchaos status <name> -n streaming
kubectl port-forward svc/<flink-jobmanager> 8081:8081
# then: curl http://localhost:8081/api/v1/jobs
```

---

## Safety Check Rejection

**Symptom:** Run fails with concurrency or minTaskManagers error.

**Diagnosis:**
```bash
kubectl fchaos list -n streaming
kubectl get pods -n streaming -l app=<target-name>,component=taskmanager
```

**Resolution:**
- Wait for in-progress runs to complete.
- Increase TaskManager count before running chaos.
- Adjust safety limits: `spec.safety.maxConcurrentRunsPerTarget` or `minTaskManagersRemaining`.

---

## Shared Cluster Detection

**Symptom:** Run rejected with "shared cluster impact" error.

**Diagnosis:**
```bash
kubectl get flinkdeployment <name> -o jsonpath='{.spec.flinkConfiguration.execution\.target}'
```

**Resolution:**
- Override: `spec.safety.allowSharedClusterImpact: true`
- Or switch to application mode.

---

## NetworkChaos: tc Command Fails

**Symptom:** NetworkChaos run fails during Injecting phase with tc error.

**Diagnosis:**
```bash
kubectl describe chaosrun <name> -n streaming
kubectl logs -n streaming -l app=flink-chaos-operator
```

**Resolution:**
- Verify the node kernel supports `netem` and `tbf` modules (`lsmod | grep sch_netem`).
- Ensure the `TC_TOOLS_IMAGE` contains the correct tc version.
- Check that `bandwidth` is only used with `TMtoExternal` target.

---

## ResourceExhaustion: stress-ng Fails

**Symptom:** ResourceExhaustion run fails during Injecting phase.

**Resolution:**
- Verify `STRESS_IMAGE` contains `stress-ng`.
- Check pod memory limits are set (required for Memory mode percentage calculation).
- Review ephemeral container events: `kubectl describe pod <pod-name> -n streaming`.
