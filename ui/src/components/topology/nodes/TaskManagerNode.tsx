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
  return 'bg-green-500'
}

function cpuBarColor(load: number): string {
  if (load >= 0.85) return 'bg-red-500'
  if (load >= 0.65) return 'bg-amber-400'
  return 'bg-green-500'
}

function TaskManagerNode({ data }: { data: FlinkNodeData }) {
  const { label, podPhase, podIP, chaosPhase, chaosScenario, isHighlighted } = data

  const isInjecting        = chaosPhase === 'Injecting'
  const isObserving        = chaosPhase === 'Observing'
  const isPodKill          = chaosScenario === 'TaskManagerPodKill'
  const isBeingKilled      = isPodKill && isInjecting
  const isRecovering       = isPodKill && isObserving
  const isResourceExhaustion = chaosScenario === 'ResourceExhaustion'
  const isStressing = isResourceExhaustion && (isInjecting || isObserving || chaosPhase === 'CleaningUp')

  const showFlame = isResourceExhaustion && isInjecting
  const showSkull = isBeingKilled

  let borderColour = '#22c55e'
  if (isBeingKilled)     borderColour = '#ef4444'
  else if (isRecovering) borderColour = '#f59e0b'
  else if (isStressing)  borderColour = '#f97316'

  const metricsMap = useTMMetrics()
  const metrics = podIP ? metricsMap.get(podIP) : undefined
  const hasCpu  = metrics && metrics.cpuLoad >= 0
  const hasHeap = metrics && metrics.heapMaxMB > 0
  const cpuPct  = hasCpu ? Math.round(metrics!.cpuLoad * 100) : 0
  const heapPct = hasHeap ? metrics!.heapPercent : 0

  return (
    <div
      className={[
        'relative flex flex-col gap-1.5 overflow-hidden',
        'bg-slate-800 rounded-lg border-l-4',
        'border border-slate-600',
        'w-[220px] px-3 py-2.5 shadow-md',
        isBeingKilled ? 'animate-pulse' : '',
        isHighlighted && !chaosPhase ? 'ring-2 ring-sky-400 ring-offset-1 ring-offset-slate-900' : '',
      ].join(' ')}
      style={{
        boxSizing: 'border-box',
        borderLeftColor: borderColour,
        boxShadow: isBeingKilled
          ? '0 0 0 1px rgba(239,68,68,0.4), 0 0 12px 2px rgba(239,68,68,0.25)'
          : isStressing
            ? '0 0 0 1px rgba(249,115,22,0.4), 0 0 12px 2px rgba(249,115,22,0.2)'
            : undefined,
        opacity: isBeingKilled ? 0.55 : 1,
        transition: 'opacity 0.4s ease, box-shadow 0.4s ease',
      }}
    >
      {/* Diagonal stripe overlay while being killed */}
      {isBeingKilled && (
        <div
          aria-hidden="true"
          style={{
            position: 'absolute', inset: 0, borderRadius: 'inherit',
            backgroundImage: 'repeating-linear-gradient(135deg, rgba(239,68,68,0.12) 0px, rgba(239,68,68,0.12) 4px, transparent 4px, transparent 10px)',
            pointerEvents: 'none',
          }}
        />
      )}

      {/* Amber pulse ring while recovering */}
      {isRecovering && (
        <div
          aria-hidden="true"
          style={{
            position: 'absolute', inset: -2, borderRadius: 'inherit',
            border: '2px solid rgba(245,158,11,0.5)',
            animation: 'pulse 2s cubic-bezier(0.4,0,0.6,1) infinite',
            pointerEvents: 'none',
          }}
        />
      )}

      <Handle type="target" position={Position.Top} className="!bg-green-400" />

      {/* Header row */}
      <div className="relative flex items-center gap-2 min-w-0">
        <span className="flex-shrink-0 inline-flex items-center justify-center w-6 h-6 rounded bg-green-700 text-white text-[10px] font-bold">
          TM
        </span>
        <span className="text-slate-100 text-xs font-medium truncate" title={label}>
          {label}
        </span>
        {showFlame && (
          <span className="ml-auto text-sm flex-shrink-0" title="ResourceExhaustion">🔥</span>
        )}
        {showSkull && (
          <span className="ml-auto text-sm animate-bounce flex-shrink-0" title="Pod kill injecting">💀</span>
        )}
      </div>

      {/* Status row */}
      <div className="relative flex items-center gap-2">
        <span className={`flex-shrink-0 w-2 h-2 rounded-full ${podPhaseClass(podPhase)}`} />
        <span className="text-slate-400 text-[10px]">{podPhase ?? 'Unknown'}</span>
        {chaosPhase && (
          <span
            className={[
              'ml-auto flex-shrink-0 inline-flex items-center px-1.5 py-0.5 rounded text-[9px] font-semibold border',
              isBeingKilled  ? 'bg-red-900 text-red-300 border-red-700'
              : isRecovering ? 'bg-amber-900 text-amber-300 border-amber-700'
                             : 'bg-slate-700 text-slate-300 border-slate-600',
            ].join(' ')}
          >
            {chaosPhase}
          </span>
        )}
      </div>

      {/* CPU metric row */}
      {hasCpu && (
        <div className="relative flex items-center gap-2">
          <span className={`text-[10px] font-medium w-8 flex-shrink-0 ${isStressing ? 'text-orange-400' : 'text-slate-500'}`}>CPU</span>
          <div className="flex-1 h-2 rounded-full bg-slate-700 overflow-hidden">
            <div
              className={`h-full rounded-full transition-all duration-500 ${isStressing ? 'bg-orange-500' : cpuBarColor(metrics!.cpuLoad)}`}
              style={{ width: `${cpuPct}%` }}
            />
          </div>
          <span className={`text-[10px] w-8 text-right flex-shrink-0 ${isStressing ? 'text-orange-300 font-semibold' : 'text-slate-300'}`}>{cpuPct}%</span>
        </div>
      )}

      {/* Memory metric row */}
      {hasHeap && (
        <div className="relative flex items-center gap-2">
          <span className={`text-[10px] font-medium w-8 flex-shrink-0 ${isStressing ? 'text-orange-400' : 'text-slate-500'}`}>Mem</span>
          <div className="flex-1 h-2 rounded-full bg-slate-700 overflow-hidden">
            <div
              className={`h-full rounded-full transition-all duration-500 ${isStressing ? 'bg-orange-500' : metricBarColor(heapPct)}`}
              style={{ width: `${heapPct}%` }}
            />
          </div>
          <span className={`text-[10px] w-8 text-right flex-shrink-0 ${isStressing ? 'text-orange-300 font-semibold' : 'text-slate-300'}`}>{metrics!.heapUsedMB}M</span>
        </div>
      )}

      <Handle type="source" position={Position.Bottom} className="!bg-green-400" />
    </div>
  )
}

export default memo(TaskManagerNode)
