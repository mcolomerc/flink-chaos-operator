import { create } from 'zustand'

interface WizardTarget {
  podName: string
  nodeType: 'jobManager' | 'taskManager'
  deploymentName: string
  targetType: string
  deploymentId?: string
  vvpNamespace?: string
  /** Raw checkpoint storage URL from topology (e.g. s3://bucket/path), used to pre-fill externalEndpoint */
  checkpointEndpoint?: string
}

interface AppStore {
  /** Name of the ChaosRun currently selected in the sidebar, or null. */
  selectedRunName: string | null
  setSelectedRun: (name: string | null) => void

  /** Name of the Flink deployment currently shown in graph + metrics. null = first/default. */
  selectedDeploymentName: string | null
  setSelectedDeployment: (name: string | null) => void

  /** Whether the Flink Metrics panel at the bottom is expanded. */
  metricsExpanded: boolean
  setMetricsExpanded: (v: boolean) => void

  /** Name of the ChaosRun whose detail modal is open, or null. */
  detailRunName: string | null
  openDetail: (name: string) => void
  closeDetail: () => void

  wizardTarget: WizardTarget | null
  openWizard: (target: WizardTarget) => void
  closeWizard: () => void
}

export const useAppStore = create<AppStore>((set) => ({
  selectedRunName: null,
  setSelectedRun: (name) => set({ selectedRunName: name }),

  selectedDeploymentName: null,
  setSelectedDeployment: (name) => set({ selectedDeploymentName: name }),

  metricsExpanded: true,
  setMetricsExpanded: (v) => set({ metricsExpanded: v }),

  detailRunName: null,
  openDetail: (name) => set({ detailRunName: name }),
  closeDetail: () => set({ detailRunName: null }),

  wizardTarget: null,
  openWizard: (target) => set({ wizardTarget: target }),
  closeWizard: () => set({ wizardTarget: null }),
}))
