import { memo } from 'react'
import { Handle, Position } from '@xyflow/react'
import type { FlinkNodeData } from '../../../hooks/useTopology'
import { useTMMetrics } from '../../../hooks/useTMMetrics'

function podPhaseClass(phase?: string): string {
  switch (phase) {
    case 'Running':  return 'bg-green-500'
    case 'Pending':  return 'bg-yellow-400'
    case 'Failed':   return 'bg-red-500'
    default:         return 'bg-slate-500'
  }
}

function metricBarColor(pct: number): string {
  if (pct >= 85) return 'bg-red-500'
  if (pct >= 65) return 'bg-amber-400'
  return 'bg-blue-500'
}

function cpuBarColor(load: number): string {
  if (load >= 0.85) return 'bg-red-500'
  if (load >= 0.65) return 'bg-amber-400'
  return 'bg-blue-500'
}

function JobManagerNode({ data }: { data: FlinkNodeData }) {
  const { label, podPhase, podIP, chaosPhase, chaosScenario, isHighlighted } = data
  const hasChaos = Boolean(chaosPhase)

  const metricsMap = useTMMetrics()
  const metrics = podIP ? metricsMap.get(podIP) : undefined
  const hasCpu  = metrics && metrics.cpuLoad >= 0
  const hasHeap = metrics && metrics.heapMaxMB > 0
  const cpuPct  = hasCpu ? Math.round(metrics!.cpuLoad * 100) : 0
  const heapPct = hasHeap ? metrics!.heapPercent : 0

  return (
    <div
      className={[
        'relative flex flex-col gap-1.5',
        'bg-slate-800 rounded-lg border-l-4 border-l-blue-500',
        'border border-slate-600',
        'w-[220px] px-3 py-2.5 shadow-md',
        hasChaos ? 'ring-2 ring-red-500 ring-offset-1 ring-offset-slate-900 animate-pulse' : '',
        isHighlighted && !hasChaos ? 'ring-2 ring-sky-400 ring-offset-1 ring-offset-slate-900' : '',
      ].join(' ')}
      style={{ boxSizing: 'border-box' }}
    >
      <Handle type="target" position={Position.Top} className="!bg-blue-400" />

      {/* Header row */}
      <div className="flex items-center gap-2 min-w-0">
        <span className="flex-shrink-0 inline-flex items-center justify-center w-6 h-6 rounded bg-blue-600 text-white text-[10px] font-bold">
          JM
        </span>
        <span className="text-slate-100 text-xs font-medium truncate" title={label}>
          {label}
        </span>
      </div>

      {/* Status row */}
      <div className="flex items-center gap-2">
        <span className={`flex-shrink-0 w-2 h-2 rounded-full ${podPhaseClass(podPhase)}`} />
        <span className="text-slate-400 text-[10px]">{podPhase ?? 'Unknown'}</span>
        {hasChaos && (
          <span className="ml-auto flex-shrink-0 inline-flex items-center px-1.5 py-0.5 rounded text-[9px] font-semibold bg-red-900 text-red-300 border border-red-700">
            {chaosScenario ?? chaosPhase}
          </span>
        )}
      </div>

      {/* CPU metric row */}
      {hasCpu && (
        <div className="flex items-center gap-2">
          <span className="text-slate-500 text-[10px] font-medium w-8 flex-shrink-0">CPU</span>
          <div className="flex-1 h-2 rounded-full bg-slate-700 overflow-hidden">
            <div
              className={`h-full rounded-full transition-all duration-500 ${cpuBarColor(metrics!.cpuLoad)}`}
              style={{ width: `${cpuPct}%` }}
            />
          </div>
          <span className="text-slate-300 text-[10px] w-8 text-right flex-shrink-0">{cpuPct}%</span>
        </div>
      )}

      {/* Memory metric row */}
      {hasHeap && (
        <div className="flex items-center gap-2">
          <span className="text-slate-500 text-[10px] font-medium w-8 flex-shrink-0">Mem</span>
          <div className="flex-1 h-2 rounded-full bg-slate-700 overflow-hidden">
            <div
              className={`h-full rounded-full transition-all duration-500 ${metricBarColor(heapPct)}`}
              style={{ width: `${heapPct}%` }}
            />
          </div>
          <span className="text-slate-300 text-[10px] w-8 text-right flex-shrink-0">{metrics!.heapUsedMB}M</span>
        </div>
      )}

      <Handle type="source" position={Position.Bottom} className="!bg-blue-400" />
    </div>
  )
}

export default memo(JobManagerNode)
