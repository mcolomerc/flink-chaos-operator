import { useChaosRuns } from '../../hooks/useChaosRuns'
import { useAppStore } from '../../store'
import type { FlinkMetrics } from '../../hooks/useFlinkMetrics'
import JobMetricsPanel from './JobMetricsPanel'
import CheckpointChart from './CheckpointChart'
import ThroughputChart from './ThroughputChart'

interface Props {
  metrics: FlinkMetrics
}

export default function MetricsPanel({ metrics }: Props) {
  const { jobs, taskManagerCount, checkpointHistory, throughputHistory, jobMetrics, isLoading, isUnavailable, error } = metrics

  const expanded    = useAppStore((s) => s.metricsExpanded)
  const setExpanded = useAppStore((s) => s.setMetricsExpanded)

  const { active, terminal } = useChaosRuns()
  const allRuns = [...active, ...terminal]

  return (
    <div className="flex-shrink-0 border-t border-slate-700 bg-slate-900">
      {/* Toggle bar — always visible */}
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex w-full items-center gap-2 px-4 py-1.5 text-left hover:bg-slate-800 transition-colors group"
        aria-expanded={expanded}
      >
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth={1.5}
          className="w-3.5 h-3.5 text-slate-500 group-hover:text-slate-300"
          aria-hidden="true"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            d="M3 13.5h18M3 8.25h18M3 18.75h18"
          />
        </svg>
        <span className="text-slate-400 text-[10px] uppercase tracking-wider font-semibold group-hover:text-slate-200">
          Flink Metrics
        </span>
        {error && (
          <span className="ml-auto text-red-400 text-[10px]">error</span>
        )}
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth={2}
          className={`ml-auto w-3 h-3 text-slate-600 transition-transform ${expanded ? 'rotate-180' : ''}`}
          aria-hidden="true"
        >
          <path strokeLinecap="round" strokeLinejoin="round" d="M19.5 8.25l-7.5 7.5-7.5-7.5" />
        </svg>
      </button>

      {/* Expanded panel */}
      {expanded && (
        <div className="flex h-[420px] border-t border-slate-800">
          {/* Left: job stats */}
          <div className="w-[600px] flex-shrink-0 border-r border-slate-800 overflow-hidden">
            <JobMetricsPanel
              jobs={jobs}
              taskManagerCount={taskManagerCount}
              jobMetrics={jobMetrics}
              isLoading={isLoading}
              isUnavailable={isUnavailable}
            />
          </div>

          {/* Middle: checkpoint age chart */}
          <div className="flex-1 min-w-0 flex flex-col px-3 py-2 border-r border-slate-800">
            <div className="flex items-center gap-2 mb-1">
              <span className="text-slate-500 text-[10px] uppercase tracking-wider font-semibold">
                Checkpoint Age
              </span>
              <span className="text-slate-700 text-[10px]">(seconds since last)</span>
              {active.length > 0 && (
                <span className="ml-auto text-red-400 text-[10px] flex items-center gap-1">
                  <span className="w-1.5 h-1.5 rounded-full bg-red-500 animate-pulse inline-block" />
                  chaos active
                </span>
              )}
            </div>
            <div className="flex-1 min-h-0">
              <CheckpointChart history={checkpointHistory} chaosRuns={allRuns} />
            </div>
          </div>

          {/* Right: throughput chart */}
          <div className="flex-1 min-w-0 flex flex-col px-3 py-2">
            <span className="text-slate-500 text-[10px] uppercase tracking-wider font-semibold mb-1">
              Throughput (rec/s)
            </span>
            <div className="flex-1 min-h-0">
              <ThroughputChart history={throughputHistory} chaosRuns={allRuns} />
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
