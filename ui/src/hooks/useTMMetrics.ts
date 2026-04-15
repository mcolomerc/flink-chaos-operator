import { useQuery } from '@tanstack/react-query'
import { fetchTMMetrics, type TMMetrics } from '../api/client'
import { useAppStore } from '../store'

/**
 * Polls Flink REST TM metrics every 5 s and returns a Map keyed by pod IP so
 * topology nodes can look up metrics without iterating the array every render.
 */
export function useTMMetrics(): Map<string, TMMetrics> {
  const deploymentName = useAppStore((s) => s.selectedDeploymentName) ?? undefined

  const { data } = useQuery({
    queryKey: ['tmmetrics', deploymentName],
    queryFn: () => fetchTMMetrics(deploymentName),
    refetchInterval: 5_000,
    retry: false,
    // Return an empty array on error so downstream code doesn't need null checks.
    placeholderData: [],
  })

  const map = new Map<string, TMMetrics>()
  for (const m of data ?? []) {
    if (m.podIP) map.set(m.podIP, m)
  }
  return map
}
