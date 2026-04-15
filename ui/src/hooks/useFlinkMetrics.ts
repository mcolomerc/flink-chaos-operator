import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchFlinkJobs, fetchFlinkTaskManagers, fetchFlinkCheckpoints, fetchFlinkJobMetrics } from '../api/client'
import type { FlinkJob, FlinkJobMetrics } from '../api/client'
import { useAppStore } from '../store'

export interface CheckpointSample {
  /** Wall-clock time when this sample was taken (Unix ms). */
  sampledAt: number
  /** Age of the latest completed checkpoint at sample time (seconds). */
  ageSeconds: number
}

export interface ThroughputSample {
  sampledAt: number
  recordsInPerSec: number
  recordsOutPerSec: number
  bytesInPerSec: number
  bytesOutPerSec: number
}

export interface FlinkMetrics {
  jobs: FlinkJob[]
  taskManagerCount: number | null
  /** Rolling history of checkpoint age samples, newest last. Max 50 entries. */
  checkpointHistory: CheckpointSample[]
  /** Rolling history of throughput samples, newest last. Max 50 entries. */
  throughputHistory: ThroughputSample[]
  /** Latest raw job metrics snapshot (checkpoint counters etc.) */
  jobMetrics: FlinkJobMetrics | null
  isLoading: boolean
  isUnavailable: boolean
  error: Error | null
}

const MAX_HISTORY = 50

export function useFlinkMetrics(): FlinkMetrics {
  const selectedDeploymentName = useAppStore((s) => s.selectedDeploymentName)
  const dep = selectedDeploymentName ?? undefined

  const checkpointHistoryRef = useRef<CheckpointSample[]>([])
  const throughputHistoryRef = useRef<ThroughputSample[]>([])
  const lastTriggerRef = useRef<number | null>(null)
  const lastJobMetricsRef = useRef<FlinkJobMetrics | null>(null)
  const [, setTick] = useState(0)

  // Reset histories when the selected deployment changes so stale data
  // from a previous job doesn't bleed into the new one's charts.
  useEffect(() => {
    checkpointHistoryRef.current = []
    throughputHistoryRef.current = []
    lastTriggerRef.current = null
    lastJobMetricsRef.current = null
    setTick((t) => t + 1)
  }, [selectedDeploymentName])

  const jobsQuery = useQuery({
    queryKey: ['flinkJobs', dep],
    queryFn: () => fetchFlinkJobs(dep),
    refetchInterval: 10_000,
    retry: 1,
  })

  const tmQuery = useQuery({
    queryKey: ['flinkTMs', dep],
    queryFn: () => fetchFlinkTaskManagers(dep),
    refetchInterval: 10_000,
    retry: 1,
  })

  const checkpointQuery = useQuery({
    queryKey: ['flinkCheckpoints', dep],
    queryFn: () => fetchFlinkCheckpoints(dep),
    refetchInterval: 10_000,
    retry: 1,
  })

  const jobMetricsQuery = useQuery({
    queryKey: ['flinkJobMetrics', dep],
    queryFn: () => fetchFlinkJobMetrics(dep),
    refetchInterval: 5_000,
    retry: 1,
  })

  // Accumulate a checkpoint age sample each time new checkpoint data arrives.
  useEffect(() => {
    const summary = checkpointQuery.data
    if (!summary?.latest) return
    const { trigger_timestamp } = summary.latest
    if (trigger_timestamp === lastTriggerRef.current) return
    lastTriggerRef.current = trigger_timestamp
    const sampledAt = Date.now()
    const ageSeconds = Math.round((sampledAt - trigger_timestamp) / 1000)
    checkpointHistoryRef.current = [
      ...checkpointHistoryRef.current.slice(-(MAX_HISTORY - 1)),
      { sampledAt, ageSeconds },
    ]
    setTick((t) => t + 1)
  }, [checkpointQuery.data])

  // Compute per-second throughput rates. Prefer direct rate metrics returned
  // by the backend (/jobs/{id}/metrics?agg=sum) — these are non-zero even in
  // VVP where cumulative vertex counters are often absent. Fall back to diffing
  // consecutive cumulative samples when the direct rates are all zero.
  useEffect(() => {
    const cur = jobMetricsQuery.data
    if (!cur) return
    const prev = lastJobMetricsRef.current
    lastJobMetricsRef.current = cur

    // Prefer direct per-second rates from the backend.
    const hasDirectRates =
      (cur.recordsInPerSec ?? 0) > 0 ||
      (cur.recordsOutPerSec ?? 0) > 0 ||
      (cur.bytesInPerSec ?? 0) > 0 ||
      (cur.bytesOutPerSec ?? 0) > 0

    let sample: ThroughputSample
    if (hasDirectRates) {
      sample = {
        sampledAt: cur.sampledAt,
        recordsInPerSec: cur.recordsInPerSec ?? 0,
        recordsOutPerSec: cur.recordsOutPerSec ?? 0,
        bytesInPerSec: cur.bytesInPerSec ?? 0,
        bytesOutPerSec: cur.bytesOutPerSec ?? 0,
      }
    } else {
      // Diff-based fallback for standard Flink clusters.
      if (!prev) return
      const dtMs = cur.sampledAt - prev.sampledAt
      if (dtMs <= 0) return
      const dtSec = dtMs / 1000
      sample = {
        sampledAt: cur.sampledAt,
        recordsInPerSec: Math.max(0, (cur.readRecords - prev.readRecords) / dtSec),
        recordsOutPerSec: Math.max(0, (cur.writeRecords - prev.writeRecords) / dtSec),
        bytesInPerSec: Math.max(0, (cur.readBytes - prev.readBytes) / dtSec),
        bytesOutPerSec: Math.max(0, (cur.writeBytes - prev.writeBytes) / dtSec),
      }
    }
    throughputHistoryRef.current = [
      ...throughputHistoryRef.current.slice(-(MAX_HISTORY - 1)),
      sample,
    ]
    setTick((t) => t + 1)
  }, [jobMetricsQuery.data])

  const isUnavailable =
    (jobsQuery.error as { message?: string } | null)?.message?.includes('503') ?? false

  const isLoading =
    jobsQuery.isLoading || tmQuery.isLoading || checkpointQuery.isLoading

  const firstError =
    (jobsQuery.error as Error | null) ??
    (tmQuery.error as Error | null) ??
    (checkpointQuery.error as Error | null)

  return {
    jobs: jobsQuery.data ?? [],
    taskManagerCount: tmQuery.data?.count ?? null,
    checkpointHistory: checkpointHistoryRef.current,
    throughputHistory: throughputHistoryRef.current,
    jobMetrics: jobMetricsQuery.data ?? null,
    isLoading,
    isUnavailable,
    error: isUnavailable ? null : firstError,
  }
}
