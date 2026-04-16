# Code Review Tasks

Generated from the Go and React code review of the `feature/ui` branch.

---

## Go Backend

### Critical

- [x] **Add `http.MaxBytesReader` to `createChaosRunHandler`** — unbounded request body allows memory exhaustion attacks.  
  `internal/apiserver/handlers_chaos_write.go:74`

- [x] **Replace time-based name with `GenerateName`** — two requests within the same millisecond produce the same `ChaosRun` name and the second create fails.  
  `internal/apiserver/handlers_chaos_write.go:87`

- [x] **Validate `ScenarioType` and `SelectionMode` before casting from user string** — unknown values are passed silently to the controller.  
  `internal/apiserver/handlers_chaos_write.go:115`

### Major

- [x] **Validate `r.PathValue("name")` against RFC 1123** before using in Kubernetes API calls.  
  `internal/apiserver/handlers_chaos.go:108`, `handlers_chaos_write.go:165`

- [x] **Parallelize `fetchVertexRates` HTTP calls** — sequential calls per vertex mean worst-case latency of `N × 5s`.  
  `internal/observer/flinkrest/client.go:551`

- [x] **Fix slice filter mutation** — `filtered := podList.Items[:0]` mutates the backing array of the Kubernetes API response; use `make()` instead.  
  `internal/apiserver/handlers_flink.go:169`

- [x] **Add CORS headers to `server.go`** — frontend running on a different origin (e.g. Vite dev server) cannot reach the API without them.  
  `internal/apiserver/server.go`

- [x] **Add concurrent connection limit to SSE handler** — each connection creates two tickers and polls Kubernetes every 3 s; no upper bound.  
  `internal/apiserver/handlers_sse.go`

### Minor

- [x] **Replace `indexOf` with `strings.IndexByte`** — reimplements stdlib.  
  `internal/observer/flinkrest/client.go:285`

- [x] **Use `rfc3339()` helper consistently** — `startedAt` in `listActiveChaosRuns` uses an inline format string instead of the shared helper.  
  `internal/apiserver/handlers_topology.go:422`

- [ ] **Add tests for `internal/apiserver`** — entire package has zero test files. Priority areas: `createChaosRunHandler` validation, `resolveFlinkClient` service discovery, `buildNetworkChaosSpec` / `buildResourceExhaustionSpec`, `toChaosRunSummary` / `toChaosRunDetail`, `deduplicateConnections`, `storageType` URI detection.

---

## React Frontend

### Must Fix

- [x] **Move `setSelectedDeployment` auto-select into `useEffect`** — currently called unconditionally during render (side-effect in render body).  
  `ui/src/App.tsx:49`

- [x] **Add focus trap and focus restoration to `ChaosRunDetailModal`** — keyboard users tab into background elements; focus is not returned to the trigger on close.  
  `ui/src/components/chaos/ChaosRunDetailModal.tsx`

- [x] **Add `role="dialog"` / `aria-modal="true"` / `aria-labelledby` to `ChaosRunDetailModal`** — screen readers do not announce the modal boundary.  
  `ui/src/components/chaos/ChaosRunDetailModal.tsx:443`

- [x] **Fix `FieldGroup` label association** — `<label>` has no `htmlFor` and does not wrap the input; clicking the label does nothing.  
  `ui/src/components/chaos/ChaosRunWizard.tsx:229`

### Should Fix

- [x] **Remove duplicate `useNodesState`/`useEdgesState` in `FlinkGraph`** — `useTopology` and `FlinkGraph` both maintain separate node/edge state, causing double renders on every topology poll.  
  `ui/src/components/topology/FlinkGraph.tsx:53`

- [x] **Wrap `useTMMetrics` `Map` construction in `useMemo`** — a new `Map` is created on every render; called by every TM and JM node.  
  `ui/src/hooks/useTMMetrics.ts:21`

- [x] **Remove duplicate `topology` query in `App.tsx`** — same query key as `useTopology` but with a conflicting `refetchInterval` (30 s vs 5 s); effective interval is undefined.  
  `ui/src/App.tsx:29`

- [x] **Scope SSE query invalidation to active `deploymentName`** — currently invalidates all `['topology']` and `['chaosRuns']` queries on every event regardless of deployment.  
  `ui/src/hooks/useSSE.ts:21`

- [x] **Replace fragile 503 string-match with a structured `ApiError` class** — `isUnavailable` breaks if the backend changes its error message format.  
  `ui/src/hooks/useFlinkMetrics.ts:149`

### Nice to Fix

- [x] **Escape user input in `buildYAML`** — values containing `:`, `#`, or newlines produce structurally invalid YAML in the preview.  
  `ui/src/components/chaos/ChaosRunWizard.tsx:71`

- [x] **Replace array index key with `v.name` in vertices list** — index-as-key causes React to reuse DOM nodes when the list length changes.  
  `ui/src/components/metrics/JobMetricsPanel.tsx:269`

- [x] **Use exported `ChaosRunDetail` type instead of `ReturnType<typeof fetchChaosRun>` extraction** in `RecoveryResultBanner`.  
  `ui/src/components/chaos/ChaosRunDetailModal.tsx:78`

- [x] **Clear history refs on unmount in `useFlinkMetrics`** — stale history from a previous mount appends to the next mount's charts.  
  `ui/src/hooks/useFlinkMetrics.ts:50`
