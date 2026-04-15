import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import FlinkGraph from './components/topology/FlinkGraph'
import ChaosRunList from './components/chaos/ChaosRunList'
import ChaosRunWizard from './components/chaos/ChaosRunWizard'
import ChaosRunDetailModal from './components/chaos/ChaosRunDetailModal'
import MetricsPanel from './components/metrics/MetricsPanel'
import { useTopology } from './hooks/useTopology'
import { useSSE } from './hooks/useSSE'
import { useFlinkMetrics } from './hooks/useFlinkMetrics'
import { useAppStore } from './store'
import { fetchDeployments } from './api/client'
import './index.css'

const queryClient = new QueryClient({
  defaultOptions: { queries: { refetchInterval: 5000, retry: 2 } },
})

function AppLayout() {
  useTopology()
  useSSE()

  const flinkMetrics = useFlinkMetrics()

  const selectedDeploymentName = useAppStore((s) => s.selectedDeploymentName)
  const setSelectedDeployment = useAppStore((s) => s.setSelectedDeployment)
  const openWizard = useAppStore((s) => s.openWizard)

  const { data: topology } = useQuery({
    queryKey: ['topology', selectedDeploymentName],
    queryFn: () => import('./api/client').then(({ fetchTopology }) => fetchTopology(selectedDeploymentName ?? undefined)),
    refetchInterval: 30_000,
    retry: 1,
  })

  const checkpointEndpoint = topology?.externalConnections?.find((c) => c.purpose === 'checkpoint')?.endpoint

  const { data: deployments = [] } = useQuery({
    queryKey: ['deployments'],
    queryFn: fetchDeployments,
    refetchInterval: 10_000,
    retry: 1,
  })

  // Auto-select the first deployment when none is explicitly chosen yet.
  const firstDeployment = deployments.length > 0 ? deployments[0].deploymentName : null
  const effectiveSelection = selectedDeploymentName ?? firstDeployment

  // Write the auto-selection back to the store so topology/metrics pick it up.
  if (selectedDeploymentName === null && firstDeployment !== null) {
    setSelectedDeployment(firstDeployment)
  }

  return (
    <div className="flex flex-col w-full h-full overflow-hidden bg-slate-900">
      {/* Top header bar */}
      <header className="flex-shrink-0 flex items-center gap-3 px-5 py-2.5 border-b border-slate-700 bg-slate-900/95 backdrop-blur">
        {/* Logo */}
        <img src="/logo.png" alt="Flink Chaos Operator" className="h-7 w-auto flex-shrink-0" />

        <h1 className="text-slate-100 text-sm font-semibold tracking-tight">
          Flink Chaos Operator
        </h1>

        {/* Job-switcher badges */}
        <div className="flex items-center gap-2 flex-wrap">
          <span className="text-slate-500 text-[10px] uppercase tracking-wider font-semibold flex-shrink-0">
            Jobs detected
          </span>
          {deployments.map((d) => {
            const isActive = d.deploymentName === effectiveSelection
            return (
              <button
                key={d.deploymentName}
                type="button"
                onClick={() => setSelectedDeployment(d.deploymentName)}
                className={[
                  'inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-xs font-medium border transition-colors',
                  isActive
                    ? 'bg-blue-600 text-white border-blue-500'
                    : 'bg-slate-800 text-slate-400 border-slate-600 hover:border-blue-500 hover:text-blue-300',
                ].join(' ')}
              >
                {d.k8sNamespace && (
                  <span className={`text-[9px] font-normal opacity-70`}>
                    {d.k8sNamespace}/
                  </span>
                )}
                {d.deploymentName}
              </button>
            )
          })}
        </div>

        <div className="ml-auto flex items-center gap-3">
          {/* Run Experiment button — opens wizard for the currently selected deployment */}
          {effectiveSelection && (() => {
            const dep = deployments.find((d) => d.deploymentName === effectiveSelection)
            return (
              <button
                type="button"
                onClick={() => openWizard({
                  podName: effectiveSelection,
                  nodeType: 'taskManager',
                  deploymentName: effectiveSelection,
                  targetType: dep?.targetType ?? 'FlinkDeployment',
                  deploymentId: dep?.deploymentId,
                  vvpNamespace: dep?.vvpNamespace,
                  checkpointEndpoint,
                })}
                className="inline-flex items-center gap-1.5 px-3 py-1 rounded text-xs font-semibold bg-red-700 hover:bg-red-600 text-white border border-red-600 transition-colors"
              >
                <svg viewBox="0 0 16 16" fill="currentColor" className="w-3 h-3" aria-hidden="true">
                  <path d="M8 1a7 7 0 1 0 0 14A7 7 0 0 0 8 1zm-.75 3.5a.75.75 0 0 1 1.5 0v3.25l2.1 1.213a.75.75 0 1 1-.75 1.3l-2.35-1.357A.75.75 0 0 1 7.25 8.25V4.5z"/>
                </svg>
                Run Experiment
              </button>
            )
          })()}
          <span className="w-2 h-2 rounded-full bg-green-500 animate-pulse" />
          <span className="text-slate-500 text-xs">Live</span>
        </div>
      </header>

      {/* Body: sidebar + canvas */}
      <div className="flex flex-1 overflow-hidden">
        <ChaosRunList />
        <main className="flex-1 overflow-hidden flex flex-col min-h-0">
          <div className="flex-1 min-h-0">
            <FlinkGraph />
          </div>
          <MetricsPanel metrics={flinkMetrics} />
        </main>
      </div>

      {/* Wizard portal — renders nothing when wizardTarget is null */}
      <ChaosRunWizard />

      {/* Chaos run detail modal — renders nothing when detailRunName is null */}
      <ChaosRunDetailModal
        checkpointHistory={flinkMetrics.checkpointHistory}
        throughputHistory={flinkMetrics.throughputHistory}
      />
    </div>
  )
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <AppLayout />
    </QueryClientProvider>
  )
}
