import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useChaosRuns } from '../../hooks/useChaosRuns'
import type { ChaosRunSummary } from '../../api/client'
import { abortChaosRun, deleteChaosRun } from '../../api/client'
import { useAppStore } from '../../store'

function phaseBadgeClass(phase: string): string {
  switch (phase) {
    case 'Pending':    return 'bg-slate-700 text-slate-300 border-slate-600'
    case 'Injecting':  return 'bg-red-900 text-red-300 border-red-700'
    case 'Observing':  return 'bg-amber-900 text-amber-300 border-amber-700'
    case 'CleaningUp': return 'bg-blue-900 text-blue-300 border-blue-700'
    case 'Completed':  return 'bg-green-900 text-green-300 border-green-700'
    case 'Aborted':    return 'bg-rose-950 text-rose-400 border-rose-800'
    case 'Failed':     return 'bg-rose-950 text-rose-400 border-rose-800'
    default:           return 'bg-slate-700 text-slate-300 border-slate-600'
  }
}

function scenarioBadgeClass(scenario: string): string {
  switch (scenario) {
    case 'TaskManagerPodKill': return 'bg-red-900/60 text-red-300 border-red-800'
    case 'NetworkPartition':   return 'bg-orange-900/60 text-orange-300 border-orange-800'
    case 'NetworkChaos':       return 'bg-orange-900/60 text-orange-300 border-orange-800'
    case 'ResourceExhaustion': return 'bg-yellow-900/60 text-yellow-300 border-yellow-800'
    default:                   return 'bg-slate-700/60 text-slate-300 border-slate-600'
  }
}

function formatAge(startedAt?: string): string {
  if (!startedAt) return '—'
  const diffMs = Date.now() - new Date(startedAt).getTime()
  const diffSec = Math.floor(diffMs / 1000)
  if (diffSec < 60) return `${diffSec}s ago`
  const diffMin = Math.floor(diffSec / 60)
  if (diffMin < 60) return `${diffMin}m ago`
  const diffHr = Math.floor(diffMin / 60)
  return `${diffHr}h ago`
}

function RunCard({ run, isSelected, onSelect }: {
  run: ChaosRunSummary
  isSelected: boolean
  onSelect: (name: string | null) => void
}) {
  const truncatedName = run.name.length > 26 ? `${run.name.slice(0, 26)}…` : run.name
  const isTerminal = ['Completed', 'Aborted', 'Failed'].includes(run.phase)

  const openDetail = useAppStore((s) => s.openDetail)

  const queryClient = useQueryClient()
  const abortMutation = useMutation({
    mutationFn: () => abortChaosRun(run.name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['chaosRuns'] })
      queryClient.invalidateQueries({ queryKey: ['topology'] })
    },
  })
  const deleteMutation = useMutation({
    mutationFn: () => deleteChaosRun(run.name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['chaosRuns'] })
    },
  })

  function handleClick() {
    if (isTerminal) {
      openDetail(run.name)
    } else {
      onSelect(isSelected ? null : run.name)
    }
  }

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={handleClick}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') handleClick() }}
      className={[
        'rounded-lg border px-3 py-2.5 space-y-1.5 cursor-pointer transition-colors',
        isSelected && !isTerminal
          ? 'border-sky-500 bg-sky-950/40'
          : 'border-slate-700 bg-slate-800/70 hover:border-slate-500 hover:bg-slate-800',
      ].join(' ')}
    >
      {/* Name */}
      <div className="flex items-start justify-between gap-2">
        <span
          className="text-slate-100 text-xs font-semibold leading-tight truncate"
          title={run.name}
        >
          {truncatedName}
        </span>
        <span
          className={`flex-shrink-0 inline-flex items-center px-1.5 py-0.5 rounded text-[9px] font-bold border ${phaseBadgeClass(run.phase)}`}
        >
          {run.phase}
        </span>
      </div>

      {/* Scenario + target */}
      <div className="flex items-center gap-2 flex-wrap">
        <span
          className={`inline-flex items-center px-1.5 py-0.5 rounded text-[9px] font-semibold border ${scenarioBadgeClass(run.scenario)}`}
        >
          {run.scenario}
        </span>
        <span className="text-slate-500 text-[10px] truncate" title={run.target}>
          {run.target}
        </span>
      </div>

      {/* Age */}
      <div className="text-slate-600 text-[10px]">{formatAge(run.startedAt)}</div>

      {/* Abort button — active runs only */}
      {!isTerminal && (
        <button
          onClick={(e) => { e.stopPropagation(); abortMutation.mutate() }}
          disabled={abortMutation.isPending}
          className="w-full mt-1 text-[10px] font-semibold text-red-400 border border-red-800 rounded py-0.5 hover:bg-red-950/40 transition-colors disabled:opacity-50"
        >
          {abortMutation.isPending ? 'Aborting\u2026' : 'Abort'}
        </button>
      )}

      {/* View results + delete buttons — terminal runs */}
      {isTerminal && (
        <div className="flex gap-1.5 mt-1">
          <button
            onClick={(e) => { e.stopPropagation(); openDetail(run.name) }}
            className="flex-1 text-[10px] font-semibold text-sky-400 border border-sky-800 rounded py-0.5 hover:bg-sky-950/40 transition-colors"
          >
            View Results
          </button>
          <button
            onClick={(e) => { e.stopPropagation(); deleteMutation.mutate() }}
            disabled={deleteMutation.isPending}
            className="flex-1 text-[10px] font-semibold text-slate-500 border border-slate-700 rounded py-0.5 hover:bg-slate-700/50 hover:text-slate-300 transition-colors disabled:opacity-50"
          >
            {deleteMutation.isPending ? 'Removing\u2026' : 'Remove'}
          </button>
        </div>
      )}
    </div>
  )
}

export default function ChaosRunList() {
  const { active, terminal, isLoading, error } = useChaosRuns()
  const selectedRunName = useAppStore((s) => s.selectedRunName)
  const setSelectedRun = useAppStore((s) => s.setSelectedRun)

  return (
    <aside className="flex flex-col w-[280px] flex-shrink-0 border-r border-slate-700 bg-slate-900 overflow-hidden">
      {/* Sidebar header */}
      <div className="flex items-center gap-2 px-4 py-3 border-b border-slate-700">
        <span className="text-slate-100 text-sm font-semibold">Chaos Runs</span>
        {active.length > 0 && (
          <span className="ml-auto inline-flex items-center justify-center w-5 h-5 rounded-full bg-red-600 text-white text-[10px] font-bold">
            {active.length}
          </span>
        )}
      </div>

      <div className="flex-1 overflow-y-auto px-3 py-3 space-y-2">
        {isLoading && (
          <div className="flex items-center justify-center py-8">
            <div className="w-5 h-5 border-2 border-blue-500 border-t-transparent rounded-full animate-spin" />
          </div>
        )}

        {error && !isLoading && (
          <div className="rounded border border-red-800 bg-red-950/40 px-3 py-2 text-red-400 text-xs">
            Failed to load chaos runs
          </div>
        )}

        {!isLoading && !error && active.length === 0 && terminal.length === 0 && (
          <div className="flex flex-col items-center justify-center py-10 text-center">
            <span className="text-slate-600 text-sm">No chaos runs</span>
            <span className="text-slate-700 text-xs mt-1">Active experiments will appear here</span>
          </div>
        )}

        {/* Active / in-progress runs */}
        {active.length > 0 && (
          <>
            <p className="text-slate-500 text-[10px] uppercase tracking-wider font-semibold px-0.5">
              Active
            </p>
            {active.map((run) => (
              <RunCard
                key={run.name}
                run={run}
                isSelected={selectedRunName === run.name}
                onSelect={setSelectedRun}
              />
            ))}
          </>
        )}

        {/* Divider between active and terminal */}
        {active.length > 0 && terminal.length > 0 && (
          <hr className="border-slate-700 my-2" />
        )}

        {/* Completed / terminal runs */}
        {terminal.length > 0 && (
          <>
            <p className="text-slate-500 text-[10px] uppercase tracking-wider font-semibold px-0.5">
              Completed
            </p>
            {terminal.map((run) => (
              <RunCard
                key={run.name}
                run={run}
                isSelected={false}
                onSelect={setSelectedRun}
              />
            ))}
          </>
        )}
      </div>
    </aside>
  )
}
