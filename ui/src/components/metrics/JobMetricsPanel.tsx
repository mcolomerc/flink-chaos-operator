import type { FlinkJob, FlinkJobMetrics, VertexDetail } from '../../api/client'

function jobStateDot(state: string): string {
  switch (state) {
    case 'RUNNING':    return 'bg-green-400'
    case 'FAILING':
    case 'FAILED':     return 'bg-red-400 animate-pulse'
    case 'RESTARTING': return 'bg-amber-400 animate-pulse'
    default:           return 'bg-slate-500'
  }
}

function jobStateColor(state: string): string {
  switch (state) {
    case 'RUNNING':    return 'text-green-400'
    case 'FAILING':
    case 'FAILED':     return 'text-red-400'
    case 'RESTARTING': return 'text-amber-400'
    case 'FINISHED':   return 'text-blue-400'
    case 'CANCELED':   return 'text-slate-400'
    default:           return 'text-slate-400'
  }
}

function JobRow({ job }: { job: FlinkJob }) {
  const truncated = job.name.length > 28 ? `${job.name.slice(0, 28)}…` : job.name
  return (
    <div className="flex items-center gap-2 min-w-0">
      <span className={`flex-shrink-0 w-2 h-2 rounded-full ${jobStateDot(job.state)}`} />
      <span className="text-slate-200 text-xs font-medium truncate" title={job.name}>
        {truncated}
      </span>
      <span className={`flex-shrink-0 text-[10px] font-semibold ${jobStateColor(job.state)}`}>
        {job.state}
      </span>
    </div>
  )
}

function formatBytes(bytes: number | undefined): string {
  if (bytes === undefined || bytes === null) return '—'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(2)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(2)} MB`
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`
}

function formatUnixMs(ms: number | undefined): string {
  if (ms === undefined || ms === null) return '—'
  const d = new Date(ms)
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  const ss = String(d.getSeconds()).padStart(2, '0')
  return `${hh}:${mm}:${ss}`
}

function formatDurationMs(ms: number | undefined): string {
  if (ms === undefined || ms === null) return '—'
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  return `${(ms / 60_000).toFixed(1)}m`
}

function formatUptime(startMs: number | undefined): string {
  if (!startMs) return '—'
  const diffMs = Date.now() - startMs
  if (diffMs < 0) return '—'
  const s = Math.floor(diffMs / 1000)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m ${s % 60}s`
  return `${s}s`
}


function formatRate(val: number | undefined, unit: string): string {
  if (val === undefined || val === null) return '—'
  if (val >= 1_000_000) return `${(val / 1_000_000).toFixed(1)}M ${unit}`
  if (val >= 1_000) return `${(val / 1_000).toFixed(1)}K ${unit}`
  return `${val.toFixed(1)} ${unit}`
}

function vertexStatusClass(status: string): string {
  switch (status) {
    case 'RUNNING':    return 'text-green-400'
    case 'FINISHED':   return 'text-blue-400'
    case 'FAILED':     return 'text-red-400'
    case 'CANCELING':
    case 'CANCELED':   return 'text-slate-400'
    default:           return 'text-slate-500'
  }
}

interface MRow { label: string; value: React.ReactNode }
function MRow({ label, value }: MRow) {
  return (
    <>
      <span className="text-slate-500 text-[10px]">{label}</span>
      <span className="text-slate-100 text-[11px] font-semibold tabular-nums">{value}</span>
    </>
  )
}

function SectionLabel({ label }: { label: string }) {
  return (
    <div className="text-slate-500 text-[9px] uppercase tracking-wider font-semibold mb-1 col-span-2">
      {label}
    </div>
  )
}

interface Props {
  jobs: FlinkJob[]
  taskManagerCount: number | null
  jobMetrics: FlinkJobMetrics | null
  isLoading: boolean
  isUnavailable: boolean
}

export default function JobMetricsPanel({ jobs, taskManagerCount, jobMetrics, isLoading, isUnavailable }: Props) {
  if (isUnavailable) {
    return (
      <div className="flex items-center gap-2 px-4 text-slate-600 text-xs">
        <span className="w-2 h-2 rounded-full bg-slate-600" />
        Flink REST API unavailable
      </div>
    )
  }

  if (isLoading && jobs.length === 0) {
    return (
      <div className="flex items-center gap-2 px-4">
        <div className="w-3 h-3 border border-blue-500 border-t-transparent rounded-full animate-spin" />
        <span className="text-slate-500 text-xs">Loading Flink metrics…</span>
      </div>
    )
  }

  const runningCount = jobs.filter((j) => j.state === 'RUNNING').length
  const cpDurationMs = jobMetrics?.latestCheckpointDurationMs ?? jobMetrics?.lastCheckpointDurationMs

  return (
    // Three-column grid: status | checkpoints | job config
    <div className="grid grid-cols-3 h-full divide-x divide-slate-800 overflow-hidden">

      {/* ── Col 1: Job Status + Throughput ───────────────────────────── */}
      <div className="flex flex-col gap-2 px-3 py-2 overflow-hidden">
        {/* TM count + running jobs */}
        <div className="flex items-center gap-3">
          <div className="flex flex-col gap-0">
            <span className="text-slate-500 text-[9px] uppercase tracking-wider">TaskManagers</span>
            <span className="text-slate-100 text-sm font-semibold tabular-nums">{taskManagerCount ?? '—'}</span>
          </div>
          <div className="flex flex-col gap-0">
            <span className="text-slate-500 text-[9px] uppercase tracking-wider">Jobs Running</span>
            <div className="flex items-baseline gap-1">
              <span className="text-green-400 text-sm font-semibold tabular-nums">{runningCount}</span>
              {jobs.length > runningCount && (
                <span className="text-slate-500 text-[10px]">/{jobs.length}</span>
              )}
            </div>
          </div>
        </div>

        {/* Job rows */}
        <div className="flex flex-col gap-0.5 min-w-0">
          {jobs.length === 0
            ? <span className="text-slate-600 text-xs">No jobs</span>
            : jobs.map((job) => <JobRow key={job.jid} job={job} />)
          }
        </div>

        {/* Divider */}
        <div className="border-t border-slate-800" />

        {/* Throughput */}
        <div className="grid grid-cols-2 gap-x-2 gap-y-0.5 items-baseline">
          <SectionLabel label="Throughput" />
          <MRow label="Rec In/s" value={formatRate(jobMetrics?.recordsInPerSec, 'r/s')} />
          <MRow label="Rec Out/s" value={formatRate(jobMetrics?.recordsOutPerSec, 'r/s')} />
          <MRow label="Bytes In/s" value={formatRate(jobMetrics?.bytesInPerSec, 'B/s')} />
          <MRow label="Bytes Out/s" value={formatRate(jobMetrics?.bytesOutPerSec, 'B/s')} />
        </div>

      </div>

      {/* ── Col 2: Checkpoint counts + latest detail ─────────────────── */}
      <div className="flex flex-col gap-2 px-3 py-2 overflow-hidden">
        <div className="grid grid-cols-2 gap-x-2 gap-y-0.5 items-baseline">
          <SectionLabel label="Checkpoints" />
          <MRow label="Triggered" value={jobMetrics ? jobMetrics.totalCheckpoints : '—'} />
          <MRow
            label="Completed"
            value={jobMetrics
              ? <span className="text-green-400">{jobMetrics.completedCheckpoints}</span>
              : '—'}
          />
          <MRow label="In Progress" value={jobMetrics ? jobMetrics.inProgressCheckpoints : '—'} />
          <MRow
            label="Failed"
            value={jobMetrics
              ? (jobMetrics.failedCheckpoints > 0
                  ? <span className="text-red-400">{jobMetrics.failedCheckpoints}</span>
                  : <span>{jobMetrics.failedCheckpoints}</span>)
              : '—'}
          />
          <MRow label="Restored" value={jobMetrics ? jobMetrics.restoredCheckpoints : '—'} />
        </div>

        <div className="border-t border-slate-800" />

        <div className="grid grid-cols-2 gap-x-2 gap-y-0.5 items-baseline">
          <SectionLabel label="Latest Checkpoint" />
          <MRow label="ID" value={jobMetrics?.latestCheckpointId ?? '—'} />
          <MRow label="Time" value={formatUnixMs(jobMetrics?.latestCheckpointCompletionTime)} />
          <MRow label="Duration" value={formatDurationMs(cpDurationMs)} />
          <MRow label="Data" value={formatBytes(jobMetrics?.latestCheckpointStateSizeBytes)} />
          <MRow label="Full" value={formatBytes(jobMetrics?.latestCheckpointFullSizeBytes)} />
        </div>
      </div>

      {/* ── Col 3: Job configuration + vertices ──────────────────────── */}
      <div className="flex flex-col gap-2 px-3 py-2 overflow-hidden">
        {!jobMetrics?.jobName ? (
          <span className="text-slate-600 text-xs">No running job</span>
        ) : (
          <>
            <div className="grid grid-cols-2 gap-x-2 gap-y-0.5 items-baseline">
              <SectionLabel label="Job Config" />
              <MRow
                label="Name"
                value={
                  <span className="text-slate-100 text-[10px] truncate block max-w-[100px]" title={jobMetrics.jobName}>
                    {jobMetrics.jobName}
                  </span>
                }
              />
              {jobMetrics.jobId && (
                <MRow
                  label="Job ID"
                  value={
                    <span className="font-mono text-slate-300 text-[10px]" title={jobMetrics.jobId}>
                      {jobMetrics.jobId.slice(0, 8)}…
                    </span>
                  }
                />
              )}
              <MRow label="Started" value={formatUnixMs(jobMetrics.jobStartTime)} />
              <MRow label="Uptime" value={formatUptime(jobMetrics.jobStartTime)} />
              {jobMetrics.maxParallelism != null && jobMetrics.maxParallelism > 0 && (
                <MRow label="Max Parallelism" value={jobMetrics.maxParallelism} />
              )}
              {jobMetrics.numRestarts > 0 && (
                <MRow
                  label="Restarts"
                  value={<span className="text-amber-400">{jobMetrics.numRestarts}</span>}
                />
              )}
            </div>

            {jobMetrics.vertices && jobMetrics.vertices.length > 0 && (
              <>
                <div className="border-t border-slate-800" />
                <div className="text-slate-500 text-[9px] uppercase tracking-wider font-semibold">
                  Vertices ({jobMetrics.vertices.length})
                </div>
                <div className="flex flex-col gap-1 overflow-y-auto flex-1 min-h-0">
                  {jobMetrics.vertices.map((v: VertexDetail, i: number) => (
                    <div key={i} className="flex items-start gap-1 min-w-0">
                      <span className={`flex-shrink-0 text-[9px] font-bold leading-tight pt-px ${vertexStatusClass(v.status)}`}>
                        ●
                      </span>
                      <div className="flex-1 min-w-0">
                        <div className="text-slate-300 text-[10px] leading-tight break-words">{v.name}</div>
                        <div className="flex items-center gap-1 mt-0.5">
                          <span className={`text-[9px] ${vertexStatusClass(v.status)}`}>{v.status}</span>
                          <span className="text-slate-600 text-[9px]">×{v.parallelism}</span>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </>
            )}
          </>
        )}
      </div>

    </div>
  )
}
