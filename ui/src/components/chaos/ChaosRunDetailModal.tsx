import { useEffect, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchChaosRun } from '../../api/client'
import type { ChaosRunDetail } from '../../api/client'
import { useAppStore } from '../../store'
import type { CheckpointSample, ThroughputSample } from '../../hooks/useFlinkMetrics'

// ── Helpers ──────────────────────────────────────────────────────────────────

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

function verdictBadgeClass(verdict?: string): string {
  if (!verdict) return 'bg-slate-700 text-slate-300 border-slate-600'
  switch (verdict.toLowerCase()) {
    case 'passed':        return 'bg-green-900 text-green-300 border-green-700'
    case 'failed':        return 'bg-red-900 text-red-300 border-red-700'
    case 'inconclusive':  return 'bg-amber-900 text-amber-300 border-amber-700'
    default:              return 'bg-slate-700 text-slate-300 border-slate-600'
  }
}

function formatDatetime(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return d.toLocaleDateString('en-GB', { day: '2-digit', month: 'short' }) +
    ' ' + d.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

function formatDuration(startedAt?: string, endedAt?: string): string {
  if (!startedAt || !endedAt) return '—'
  const diffSec = Math.round((new Date(endedAt).getTime() - new Date(startedAt).getTime()) / 1000)
  if (diffSec < 0) return '—'
  if (diffSec < 60) return `${diffSec}s`
  const hours = Math.floor(diffSec / 3600)
  const mins = Math.floor((diffSec % 3600) / 60)
  const secs = diffSec % 60
  if (hours > 0) return secs > 0 ? `${hours}h ${mins}m ${secs}s` : `${hours}h ${mins}m`
  return secs > 0 ? `${mins}m ${secs}s` : `${mins}m`
}

function fmtThroughput(val: number): string {
  if (val >= 1_000_000) return `${(val / 1_000_000).toFixed(1)}M`
  if (val >= 1_000)     return `${(val / 1_000).toFixed(1)}k`
  return `${Math.round(val)}`
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <p className="text-slate-500 text-[10px] uppercase tracking-wider font-semibold mb-1">{children}</p>
  )
}

function PodList({ pods }: { pods: string[] }) {
  return (
    <ul className="space-y-0.5">
      {pods.map((p) => (
        <li key={p} className="text-slate-300 text-xs flex items-start gap-1">
          <span className="text-slate-600 select-none">•</span>
          <span className="break-all font-mono">{p}</span>
        </li>
      ))}
    </ul>
  )
}

// ── Recovery banner ───────────────────────────────────────────────────────────

function RecoveryResultBanner({ data }: { data: ChaosRunDetail }) {
  const isPodKill = data.scenario === 'TaskManagerPodKill'
  if (!isPodKill || data.observationTimeoutSecs == null || data.observationTimeoutSecs === 0) return null

  const recovered = data.recoveryTimeSeconds != null
  const timeout = data.observationTimeoutSecs

  if (recovered) {
    const secs = data.recoveryTimeSeconds!
    return (
      <div className="rounded-lg border border-green-700 bg-green-950/50 px-3 py-2.5 flex items-start gap-2">
        <span className="text-green-400 text-base leading-none mt-0.5">✓</span>
        <div>
          <p className="text-green-300 text-xs font-semibold">Pod recovered in {secs}s</p>
          <p className="text-green-500 text-[10px] mt-0.5">
            Recovery threshold was {timeout}s — system recovered {timeout - secs}s to spare
          </p>
        </div>
      </div>
    )
  }

  const aborted = data.phase === 'Aborted'
  if (aborted) {
    return (
      <div className="rounded-lg border border-rose-800 bg-rose-950/50 px-3 py-2.5 flex items-start gap-2">
        <span className="text-rose-400 text-base leading-none mt-0.5">⊘</span>
        <div>
          <p className="text-rose-300 text-xs font-semibold">Experiment aborted</p>
          <p className="text-rose-500 text-[10px] mt-0.5">Recovery was not confirmed before abort</p>
        </div>
      </div>
    )
  }

  return (
    <div className="rounded-lg border border-red-700 bg-red-950/50 px-3 py-2.5 flex items-start gap-2">
      <span className="text-red-400 text-base leading-none mt-0.5">✗</span>
      <div>
        <p className="text-red-300 text-xs font-semibold">Pod not recovered within {timeout}s threshold</p>
        <p className="text-red-500 text-[10px] mt-0.5">Recovery was not observed during the observation window</p>
      </div>
    </div>
  )
}

// ── Before / during / after comparison ───────────────────────────────────────

interface WindowStats {
  avgCheckpointAge: number | null
  avgThroughput: number | null
  sampleCount: number
}

function computeWindowStats(
  checkpointHistory: CheckpointSample[],
  throughputHistory: ThroughputSample[],
  fromMs: number,
  toMs: number,
): WindowStats {
  const cpSamples = checkpointHistory.filter((s) => s.sampledAt >= fromMs && s.sampledAt <= toMs)
  const tpSamples = throughputHistory.filter((s) => s.sampledAt >= fromMs && s.sampledAt <= toMs)
  const avgCheckpointAge =
    cpSamples.length > 0
      ? Math.round(cpSamples.reduce((a, b) => a + b.ageSeconds, 0) / cpSamples.length)
      : null
  const avgThroughput =
    tpSamples.length > 0
      ? Math.round(tpSamples.reduce((a, b) => a + b.recordsInPerSec + b.recordsOutPerSec, 0) / tpSamples.length)
      : null
  return { avgCheckpointAge, avgThroughput, sampleCount: Math.max(cpSamples.length, tpSamples.length) }
}

function StatCard({
  label,
  stats,
  highlight,
}: {
  label: string
  stats: WindowStats
  highlight?: 'good' | 'bad' | 'neutral'
}) {
  const borderClass =
    highlight === 'bad'     ? 'border-red-800 bg-red-950/30'
    : highlight === 'good'  ? 'border-green-800 bg-green-950/30'
    : 'border-slate-700 bg-slate-800/50'

  const hasData = stats.sampleCount > 0

  return (
    <div className={`flex-1 min-w-0 rounded-lg border px-3 py-2.5 ${borderClass}`}>
      <p className="text-slate-400 text-[10px] uppercase tracking-wider font-semibold mb-2">{label}</p>
      {!hasData ? (
        <p className="text-slate-600 text-xs">No samples</p>
      ) : (
        <div className="space-y-1.5">
          <div>
            <p className="text-slate-500 text-[9px] uppercase tracking-wider">Checkpoint Age</p>
            <p className="text-slate-100 text-sm font-bold tabular-nums">
              {stats.avgCheckpointAge != null ? `${stats.avgCheckpointAge}s` : '—'}
            </p>
          </div>
          <div>
            <p className="text-slate-500 text-[9px] uppercase tracking-wider">Throughput (rec/s)</p>
            <p className="text-slate-100 text-sm font-bold tabular-nums">
              {stats.avgThroughput != null ? fmtThroughput(stats.avgThroughput) : '—'}
            </p>
          </div>
          <p className="text-slate-600 text-[9px]">{stats.sampleCount} sample{stats.sampleCount !== 1 ? 's' : ''}</p>
        </div>
      )}
    </div>
  )
}

function MetricComparison({
  checkpointHistory,
  throughputHistory,
  startedAt,
  endedAt,
}: {
  checkpointHistory: CheckpointSample[]
  throughputHistory: ThroughputSample[]
  startedAt?: string
  endedAt?: string
}) {
  if (!startedAt) return null

  const startMs = new Date(startedAt).getTime()
  const endMs = endedAt ? new Date(endedAt).getTime() : Date.now()
  const nowMs = Date.now()

  // Use 5 minutes before start as the "before" window, bounded by oldest sample
  const beforeWindowMs = 5 * 60 * 1000
  const beforeStats  = computeWindowStats(checkpointHistory, throughputHistory, startMs - beforeWindowMs, startMs)
  const duringStats  = computeWindowStats(checkpointHistory, throughputHistory, startMs, endMs)
  const afterStats   = computeWindowStats(checkpointHistory, throughputHistory, endMs, nowMs)

  const hasSomeData =
    beforeStats.sampleCount > 0 || duringStats.sampleCount > 0 || afterStats.sampleCount > 0

  if (!hasSomeData) return null

  // For checkpoint age: during > before means chaos worsened latency → bad
  // For throughput: during < before means chaos hurt throughput → bad
  let duringHighlight: 'good' | 'bad' | 'neutral' = 'neutral'
  if (duringStats.avgCheckpointAge != null && beforeStats.avgCheckpointAge != null) {
    duringHighlight = duringStats.avgCheckpointAge > beforeStats.avgCheckpointAge * 1.5 ? 'bad' : 'neutral'
  }
  if (duringStats.avgThroughput != null && beforeStats.avgThroughput != null && beforeStats.avgThroughput > 0) {
    if (duringStats.avgThroughput < beforeStats.avgThroughput * 0.7) duringHighlight = 'bad'
  }

  let afterHighlight: 'good' | 'bad' | 'neutral' = 'neutral'
  if (afterStats.sampleCount > 0 && beforeStats.sampleCount > 0) {
    const cpOk =
      afterStats.avgCheckpointAge == null ||
      beforeStats.avgCheckpointAge == null ||
      afterStats.avgCheckpointAge <= beforeStats.avgCheckpointAge * 1.2
    const tpOk =
      afterStats.avgThroughput == null ||
      beforeStats.avgThroughput == null ||
      afterStats.avgThroughput >= beforeStats.avgThroughput * 0.8
    afterHighlight = cpOk && tpOk ? 'good' : 'bad'
  }

  return (
    <div>
      <SectionLabel>Impact Comparison (avg per window)</SectionLabel>
      <div className="flex gap-2">
        <StatCard label="Before" stats={beforeStats} highlight="neutral" />
        <StatCard label="During" stats={duringStats} highlight={duringHighlight} />
        {endedAt && <StatCard label="After" stats={afterStats} highlight={afterHighlight} />}
      </div>
      <p className="text-slate-600 text-[9px] mt-1">
        Based on in-session samples. Checkpoint age lower = better. Throughput higher = better.
      </p>
    </div>
  )
}

// ── Modal content ─────────────────────────────────────────────────────────────

function ModalContent({
  name,
  checkpointHistory,
  throughputHistory,
  closeButtonRef,
}: {
  name: string
  checkpointHistory: CheckpointSample[]
  throughputHistory: ThroughputSample[]
  closeButtonRef: React.RefObject<HTMLButtonElement | null>
}) {
  const closeDetail = useAppStore((s) => s.closeDetail)

  const { data, isLoading, isError } = useQuery({
    queryKey: ['chaosRunDetail', name],
    queryFn: () => fetchChaosRun(name),
    staleTime: 10_000,
    refetchInterval: (query) => {
      const phase = query.state.data?.phase
      return phase && ['Completed', 'Aborted', 'Failed'].includes(phase) ? false : 3000
    },
  })

  const injectedSet = new Set(data?.injectedPods ?? [])
  const selectedExtra = (data?.selectedPods ?? []).filter((p) => !injectedSet.has(p))
  const showSelectedSection = selectedExtra.length > 0

  return (
    <>
      {/* Modal header */}
      <div className="sticky top-0 z-10 bg-slate-900 border-b border-slate-700 px-5 py-4 flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-slate-400 text-[10px] uppercase tracking-wider font-semibold mb-1">
            Experiment Results
          </p>
          <p id="detail-modal-title" className="text-slate-100 text-sm font-semibold truncate" title={name}>
            {name}
          </p>
          {data && (
            <div className="flex items-center gap-1.5 mt-1.5 flex-wrap">
              <span className={`inline-flex items-center px-1.5 py-0.5 rounded text-[9px] font-bold border ${phaseBadgeClass(data.phase)}`}>
                {data.phase}
              </span>
              {data.verdict && (
                <span className={`inline-flex items-center px-1.5 py-0.5 rounded text-[9px] font-bold border ${verdictBadgeClass(data.verdict)}`}>
                  {data.verdict}
                </span>
              )}
              {data.scenario && (
                <span className="text-slate-500 text-[10px]">{data.scenario}</span>
              )}
            </div>
          )}
        </div>
        <button
          ref={closeButtonRef}
          onClick={closeDetail}
          className="flex-shrink-0 p-1.5 rounded hover:bg-slate-800 text-slate-500 hover:text-slate-200 transition-colors"
          aria-label="Close"
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} className="w-4 h-4">
            <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
      </div>

      {/* Modal body */}
      <div className="px-5 py-4 space-y-4">
        {isLoading && (
          <div className="flex items-center justify-center py-8">
            <div className="w-5 h-5 border-2 border-blue-500 border-t-transparent rounded-full animate-spin" />
          </div>
        )}

        {isError && (
          <div className="rounded border border-red-800 bg-red-950/40 px-3 py-2 text-red-400 text-sm">
            Failed to load experiment details
          </div>
        )}

        {data && (
          <>
            <RecoveryResultBanner data={data} />

            {/* Impact comparison — before / during / after */}
            <MetricComparison
              checkpointHistory={checkpointHistory}
              throughputHistory={throughputHistory}
              startedAt={data.startedAt}
              endedAt={data.endedAt}
            />

            {/* TM count for pod kill */}
            {data.scenario === 'TaskManagerPodKill' && (data.tmCountBefore != null && data.tmCountBefore > 0) && (
              <div>
                <SectionLabel>TaskManager Count</SectionLabel>
                <div className="grid grid-cols-2 gap-2">
                  <div className="rounded border border-slate-700 bg-slate-800/60 px-3 py-2 text-center">
                    <p className="text-slate-500 text-[9px] uppercase tracking-wider">Before</p>
                    <p className="text-slate-100 text-lg font-bold tabular-nums">{data.tmCountBefore}</p>
                  </div>
                  <div className="rounded border border-slate-700 bg-slate-800/60 px-3 py-2 text-center">
                    <p className="text-slate-500 text-[9px] uppercase tracking-wider">After</p>
                    <p className={`text-lg font-bold tabular-nums ${data.tmCountAfter != null && data.tmCountAfter < data.tmCountBefore! ? 'text-red-400' : 'text-green-400'}`}>
                      {data.tmCountAfter ?? '—'}
                    </p>
                  </div>
                </div>
              </div>
            )}

            {/* Timing */}
            <div>
              <SectionLabel>Timing</SectionLabel>
              <div className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
                <span className="text-slate-500">Started</span>
                <span className="text-slate-300">{formatDatetime(data.startedAt)}</span>
                <span className="text-slate-500">Ended</span>
                <span className="text-slate-300">{formatDatetime(data.endedAt)}</span>
                <span className="text-slate-500">Total</span>
                <span className="text-slate-300">{formatDuration(data.startedAt, data.endedAt)}</span>
                {data.observationTimeoutSecs != null && data.observationTimeoutSecs > 0 && (
                  <>
                    <span className="text-slate-500">Threshold</span>
                    <span className="text-slate-300">{data.observationTimeoutSecs}s</span>
                  </>
                )}
              </div>
            </div>

            {/* Pods */}
            {data.injectedPods && data.injectedPods.length > 0 && (
              <div>
                <SectionLabel>Injected Pods ({data.injectedPods.length})</SectionLabel>
                <PodList pods={data.injectedPods} />
              </div>
            )}

            {showSelectedSection && (
              <div>
                <SectionLabel>Selected Pods ({selectedExtra.length})</SectionLabel>
                <PodList pods={selectedExtra} />
              </div>
            )}

            {data.networkPolicies && data.networkPolicies.length > 0 && (
              <div>
                <SectionLabel>Network Policies ({data.networkPolicies.length})</SectionLabel>
                <PodList pods={data.networkPolicies} />
              </div>
            )}

            {data.message && (
              <div>
                <SectionLabel>Message</SectionLabel>
                <p className="text-slate-300 text-xs break-words whitespace-pre-wrap leading-relaxed">{data.message}</p>
              </div>
            )}
          </>
        )}
      </div>
    </>
  )
}

// ── Modal shell ───────────────────────────────────────────────────────────────

const FOCUSABLE_SELECTORS =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

interface ChaosRunDetailModalProps {
  checkpointHistory: CheckpointSample[]
  throughputHistory: ThroughputSample[]
}

export default function ChaosRunDetailModal({ checkpointHistory, throughputHistory }: ChaosRunDetailModalProps) {
  const detailRunName = useAppStore((s) => s.detailRunName)
  const closeDetail   = useAppStore((s) => s.closeDetail)

  // Capture the element that had focus before the modal opened so we can restore it on close.
  const previousFocusRef = useRef<Element | null>(null)
  // Ref forwarded into ModalContent so we can focus the close button on open.
  const closeButtonRef = useRef<HTMLButtonElement | null>(null)
  const panelRef = useRef<HTMLDivElement | null>(null)

  // Capture active element when the modal opens.
  useEffect(() => {
    if (detailRunName) {
      previousFocusRef.current = document.activeElement
    } else {
      // Modal just closed — restore focus.
      if (previousFocusRef.current && previousFocusRef.current instanceof HTMLElement) {
        previousFocusRef.current.focus()
      }
      previousFocusRef.current = null
    }
  }, [detailRunName])

  // Move focus to the close button on first render of the panel.
  useEffect(() => {
    if (detailRunName && closeButtonRef.current) {
      closeButtonRef.current.focus()
    }
  }, [detailRunName])

  // Escape key closes the modal.
  useEffect(() => {
    if (!detailRunName) return
    function onKey(e: KeyboardEvent) { if (e.key === 'Escape') closeDetail() }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [detailRunName, closeDetail])

  // Tab / Shift-Tab focus trap within the modal panel.
  useEffect(() => {
    if (!detailRunName) return
    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== 'Tab' || !panelRef.current) return
      const focusable = Array.from(
        panelRef.current.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTORS),
      ).filter((el) => el.offsetParent !== null) // exclude hidden elements
      if (focusable.length === 0) return
      const first = focusable[0]
      const last  = focusable[focusable.length - 1]
      if (e.shiftKey) {
        if (document.activeElement === first) {
          e.preventDefault()
          last.focus()
        }
      } else {
        if (document.activeElement === last) {
          e.preventDefault()
          first.focus()
        }
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [detailRunName])

  if (!detailRunName) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
      onClick={closeDetail}
    >
      {/* Backdrop */}
      <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" aria-hidden="true" />

      {/* Modal panel */}
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="detail-modal-title"
        className="relative bg-slate-900 border border-slate-700 rounded-xl shadow-2xl w-full max-w-2xl max-h-[85vh] overflow-y-auto"
        onClick={(e) => e.stopPropagation()}
      >
        <ModalContent
          name={detailRunName}
          checkpointHistory={checkpointHistory}
          throughputHistory={throughputHistory}
          closeButtonRef={closeButtonRef}
        />
      </div>
    </div>
  )
}
