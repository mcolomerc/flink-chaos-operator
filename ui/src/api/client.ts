export interface PodInfo {
  name: string
  phase: string // "Running" | "Pending" | "Failed" | ...
  labels: Record<string, string>
  podIP?: string
}

export interface TMMetrics {
  tmId: string
  podIP: string
  cpuLoad: number   // 0–1, or -1 when unavailable
  heapUsedMB: number
  heapMaxMB: number
  heapPercent: number  // 0–100
}

export interface ExternalConnection {
  type: string     // "S3" | "GCS" | "HDFS" | "Unknown"
  endpoint: string
  purpose: string  // "checkpoint" | "savepoint"
}

export interface ActiveChaosRunSummary {
  name: string
  phase: string    // "Pending" | "Injecting" | "Observing" | "CleaningUp" | "Completed" | "Aborted" | "Failed"
  scenario: string // "TaskManagerPodKill" | "NetworkPartition" | "NetworkChaos" | "ResourceExhaustion"
  targetPods: string[]
  startedAt?: string
}

export interface DeploymentInfo {
  deploymentName: string
  deploymentId?: string
  vvpNamespace?: string
  jobId?: string
  k8sNamespace: string
  targetType: string
}

export interface TopologyResponse {
  deploymentName: string
  /** "FlinkDeployment" | "VervericaDeployment" */
  targetType: string
  deploymentId?: string
  vvpNamespace?: string
  jobManagers: PodInfo[]
  taskManagers: PodInfo[]
  externalConnections: ExternalConnection[]
  activeChaosRuns: ActiveChaosRunSummary[]
}

export interface ChaosRunSummary {
  name: string
  phase: string
  verdict?: string
  scenario: string
  target: string
  startedAt?: string
  endedAt?: string
}

export interface ChaosRunDetail extends ChaosRunSummary {
  selectedPods?: string[]
  injectedPods?: string[]
  message?: string
  networkPolicies?: string[]
  recoveryTimeSeconds?: number | null
  recoveryObservedAt?: string
  tmCountBefore?: number
  tmCountAfter?: number
  observationTimeoutSecs?: number
}

// ── Flink REST proxy types ──────────────────────────────────────────────────

export interface FlinkJob {
  jid: string
  name: string
  state: string // "RUNNING" | "FAILING" | "RESTARTING" | "FINISHED" | "CANCELED" | "FAILED"
}

export interface FlinkCheckpointDetail {
  trigger_timestamp: number // Unix milliseconds
  status: string            // "COMPLETED" | "FAILED"
}

export interface FlinkCheckpointSummary {
  latest: FlinkCheckpointDetail | null
}

export interface VertexDetail {
  name: string
  parallelism: number
  status: string
}

export interface FlinkJobMetrics {
  // Job configuration (from /jobs/{id})
  jobId?: string
  jobName?: string
  jobStartTime?: number       // Unix ms
  jobDurationMs?: number      // running duration in ms
  maxParallelism?: number
  vertices?: VertexDetail[]
  lastCheckpointDurationMs: number
  lastCheckpointSizeBytes: number
  completedCheckpoints: number
  failedCheckpoints: number
  numRestarts: number
  // Direct per-second throughput rates (from /jobs/{id}/metrics?agg=sum)
  recordsInPerSec: number
  recordsOutPerSec: number
  bytesInPerSec: number
  bytesOutPerSec: number
  /** Cumulative totals summed across all vertices (used as chart fallback) */
  readRecords: number
  writeRecords: number
  readBytes: number
  writeBytes: number
  /** Unix ms when sampled — used to compute per-second rates */
  sampledAt: number
  // From /jobs/{id}/checkpoints counts
  inProgressCheckpoints: number
  restoredCheckpoints: number
  totalCheckpoints: number
  // Latest completed checkpoint detail
  latestCheckpointId?: number
  latestCheckpointCompletionTime?: number  // Unix ms
  latestCheckpointDurationMs?: number      // end-to-end duration ms (richer than lastCheckpointDurationMs)
  latestCheckpointStateSizeBytes?: number
  latestCheckpointFullSizeBytes?: number
}

// ── fetch helpers ──────────────────────────────────────────────────────────

async function apiFetch<T>(path: string): Promise<T> {
  const res = await fetch(path)
  if (!res.ok) {
    throw new Error(`API error ${res.status} on ${path}: ${await res.text()}`)
  }
  return res.json() as Promise<T>
}

export async function fetchDeployments(): Promise<DeploymentInfo[]> {
  return apiFetch<DeploymentInfo[]>('/api/deployments')
}

export async function fetchTopology(deploymentName?: string): Promise<TopologyResponse> {
  const qs = deploymentName ? `?deploymentName=${encodeURIComponent(deploymentName)}` : ''
  return apiFetch<TopologyResponse>(`/api/topology${qs}`)
}

export async function fetchChaosRuns(deploymentName?: string): Promise<ChaosRunSummary[]> {
  const qs = deploymentName ? `?deploymentName=${encodeURIComponent(deploymentName)}` : ''
  return apiFetch<ChaosRunSummary[]>(`/api/chaosruns${qs}`)
}

export async function fetchChaosRun(name: string): Promise<ChaosRunDetail> {
  return apiFetch<ChaosRunDetail>(`/api/chaosruns/${name}`)
}

export async function fetchFlinkJobs(deploymentName?: string): Promise<FlinkJob[]> {
  const qs = deploymentName ? `?deploymentName=${encodeURIComponent(deploymentName)}` : ''
  return apiFetch<FlinkJob[]>(`/api/flink/jobs${qs}`)
}

export async function fetchFlinkTaskManagers(deploymentName?: string): Promise<{ count: number }> {
  const qs = deploymentName ? `?deploymentName=${encodeURIComponent(deploymentName)}` : ''
  return apiFetch<{ count: number }>(`/api/flink/taskmanagers${qs}`)
}

export async function fetchFlinkCheckpoints(deploymentName?: string): Promise<FlinkCheckpointSummary | null> {
  const qs = deploymentName ? `?deploymentName=${encodeURIComponent(deploymentName)}` : ''
  return apiFetch<FlinkCheckpointSummary | null>(`/api/flink/checkpoints${qs}`)
}

// ── Chaos run mutations ────────────────────────────────────────────────────

export interface CreateChaosRunRequest {
  targetName: string
  /** "FlinkDeployment" | "VervericaDeployment" — defaults to FlinkDeployment */
  targetType?: string
  deploymentId?: string
  vvpNamespace?: string
  scenarioType: string
  selectionMode: string
  selectionCount?: number
  selectionPods?: string[]
  gracePeriodSeconds?: number | null
  networkTarget?: string
  networkDirection?: string
  externalHostname?: string
  externalCIDR?: string
  externalPort?: number
  latencyMs?: number
  jitterMs?: number
  lossPercent?: number | null
  bandwidth?: string
  durationSeconds?: number
  resourceMode?: string
  workers?: number
  memoryPercent?: number
  minTaskManagers?: number
  dryRun?: boolean
}

export interface CreateChaosRunResponse {
  name: string
  namespace: string
}

export async function createChaosRun(req: CreateChaosRunRequest): Promise<CreateChaosRunResponse> {
  const res = await fetch('/api/chaosruns', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  })
  if (!res.ok) throw new Error(`Create failed: ${await res.text()}`)
  return res.json() as Promise<CreateChaosRunResponse>
}

export async function abortChaosRun(name: string): Promise<void> {
  const res = await fetch(`/api/chaosruns/${encodeURIComponent(name)}/abort`, {
    method: 'PATCH',
  })
  if (!res.ok && res.status !== 204) throw new Error(`Abort failed: ${await res.text()}`)
}

export async function deleteChaosRun(name: string): Promise<void> {
  const res = await fetch(`/api/chaosruns/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  })
  if (!res.ok && res.status !== 204) throw new Error(`Delete failed: ${await res.text()}`)
}

export async function fetchFlinkJobMetrics(deploymentName?: string): Promise<FlinkJobMetrics | null> {
  const qs = deploymentName ? `?deploymentName=${encodeURIComponent(deploymentName)}` : ''
  return apiFetch<FlinkJobMetrics | null>(`/api/flink/jobmetrics${qs}`)
}

export async function fetchTMMetrics(deploymentName?: string): Promise<TMMetrics[]> {
  const qs = deploymentName ? `?deploymentName=${encodeURIComponent(deploymentName)}` : ''
  return apiFetch<TMMetrics[]>(`/api/flink/tmmetrics${qs}`)
}
