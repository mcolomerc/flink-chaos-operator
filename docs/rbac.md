# RBAC and Security

## Required Permissions

The operator requires the following permissions within its namespace:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: chaos-operator-role
rules:
- apiGroups: ["chaos.flink.io"]
  resources: ["chaosruns"]
  verbs: ["get", "list", "watch", "patch", "update"]

- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "watch", "delete"]

- apiGroups: [""]
  resources: ["pods/ephemeralcontainers"]
  verbs: ["get", "patch", "update"]

- apiGroups: [""]
  resources: ["pods/exec"]
  verbs: ["create"]

- apiGroups: ["networking.k8s.io"]
  resources: ["networkpolicies"]
  verbs: ["get", "list", "watch", "create", "delete"]

- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]

- apiGroups: ["flink.apache.org"]
  resources: ["flinkdeployments"]
  verbs: ["get", "list", "watch"]
```

## Security Context

The operator runs with minimal privileges:

```yaml
podSecurityContext:
  runAsNonRoot: true
  runAsUser: 65532

securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
      - ALL
  readOnlyRootFilesystem: true
```

The NET_ADMIN capability is granted **only to ephemeral containers** (NetworkChaos), not to the operator pod itself.

## Notes

- The operator is **single-namespace scoped**. Install one instance per namespace.
- The operator does **not** require privileged container capabilities.
- The operator does **not** use node-level agents or DaemonSets.
- All destructive operations (pod deletion, NetworkPolicy creation) are explicitly authorized via RBAC.
