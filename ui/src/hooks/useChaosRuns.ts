import { useQuery } from '@tanstack/react-query'
import { fetchChaosRuns } from '../api/client'
import type { ChaosRunSummary } from '../api/client'
import { useAppStore } from '../store'

export function useChaosRuns() {
  const selectedDeploymentName = useAppStore((s) => s.selectedDeploymentName)
  const dep = selectedDeploymentName ?? undefined

  const { data, isLoading, error } = useQuery<ChaosRunSummary[]>({
    queryKey: ['chaosRuns', dep],
    queryFn: () => fetchChaosRuns(dep),
    refetchInterval: 5000,
    retry: 2,
  })

  const runs = data ?? []

  const terminalPhases = new Set(['Completed', 'Aborted', 'Failed'])
  const active = runs.filter((r) => !terminalPhases.has(r.phase))
  const terminal = runs.filter((r) => terminalPhases.has(r.phase))

  return { active, terminal, isLoading, error }
}
