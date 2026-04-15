import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNodesState, useEdgesState } from '@xyflow/react'
import type { Node, Edge } from '@xyflow/react'
import { fetchTopology } from '../api/client'
import type { TopologyResponse, ActiveChaosRunSummary } from '../api/client'
import { useAppStore } from '../store'

export type FlinkNodeType = 'jobManager' | 'taskManager' | 'storage'

export interface FlinkNodeData extends Record<string, unknown> {
  label: string
  podPhase?: string
  podIP?: string         // Kubernetes pod IP, used to correlate Flink TM metrics
  chaosPhase?: string    // phase of active chaos run targeting this pod, if any
  chaosScenario?: string // scenario type, if targeted
  isHighlighted?: boolean // true when the pod belongs to the sidebar-selected run
}

// Layout constants
const COLS       = 4    // max TMs per row
const X_SPACING  = 250  // horizontal gap between nodes
const Y_SPACING  = 170  // vertical gap between TM rows
const LEVEL_GAP  = 120  // extra vertical gap between levels (JM→TM, TM→storage)
const JM_Y       = 60

/**
 * Transforms a TopologyResponse into React Flow nodes + edges.
 *
 * Three-level vertical layout:
 *   Level 1 — JobManagers:   horizontally centered
 *   Level 2 — TaskManagers:  grid (up to 4 per row), horizontally centered
 *   Level 3 — Storage:       horizontally centered below TMs
 *
 * All three levels share the same center line.
 */
export function buildGraph(
  data: TopologyResponse,
  highlightedPods: Set<string> = new Set(),
): {
  nodes: Node<FlinkNodeData>[]
  edges: Edge[]
} {
  const nodes: Node<FlinkNodeData>[] = []
  const edges: Edge[] = []

  const jmCount      = data.jobManagers.length
  const tmCount      = data.taskManagers.length
  const storageConns = data.externalConnections ?? []
  const storageCount = storageConns.length

  // TM grid dimensions
  const tmActualCols = Math.min(Math.max(tmCount, 1), COLS)
  const tmRows       = Math.ceil(tmCount / COLS)

  // Canvas width: widest level determines the shared center line.
  // Use column-count of each level to compute its span, then take the max.
  const canvasWidth = Math.max(jmCount, tmActualCols, storageCount, 1) * X_SPACING

  // Helper: compute left-edge x so a row of `count` items is centered on canvasWidth.
  function centeredStartX(count: number): number {
    return (canvasWidth - (count - 1) * X_SPACING) / 2
  }

  // ── Level 1: JobManagers ─────────────────────────────────────────────────

  const podChaosMap = new Map<string, ActiveChaosRunSummary>()
  for (const run of data.activeChaosRuns ?? []) {
    for (const podName of run.targetPods ?? []) {
      if (!podChaosMap.has(podName)) podChaosMap.set(podName, run)
    }
  }

  const networkChaosScenarios = new Set(['NetworkChaos', 'NetworkPartition'])
  const tmNetworkScenario = new Map<string, string>()
  for (const run of data.activeChaosRuns ?? []) {
    if (networkChaosScenarios.has(run.scenario)) {
      for (const podName of run.targetPods ?? []) {
        if (!tmNetworkScenario.has(podName)) tmNetworkScenario.set(podName, run.scenario)
      }
    }
  }

  const jmStartX = centeredStartX(Math.max(jmCount, 1))
  for (let i = 0; i < jmCount; i++) {
    const jm    = data.jobManagers[i]
    const chaos = podChaosMap.get(jm.name)
    nodes.push({
      id:       `jm-${i}`,
      type:     'jobManager',
      position: { x: jmStartX + i * X_SPACING, y: JM_Y },
      data: {
        label:        jm.name,
        podPhase:     jm.phase,
        podIP:        jm.podIP,
        chaosPhase:   chaos?.phase,
        chaosScenario: chaos?.scenario,
        isHighlighted: highlightedPods.has(jm.name),
      },
    })
  }

  // ── Level 2: TaskManagers ────────────────────────────────────────────────

  const TM_Y = JM_Y + Y_SPACING + LEVEL_GAP

  for (let i = 0; i < tmCount; i++) {
    const tm    = data.taskManagers[i]
    const col   = i % COLS
    const row   = Math.floor(i / COLS)
    const chaos = podChaosMap.get(tm.name)

    // Center the last (possibly shorter) row independently.
    const rowCount   = row < tmRows - 1 ? COLS : tmCount - row * COLS
    const rowStartX  = centeredStartX(rowCount)

    nodes.push({
      id:       `tm-${i}`,
      type:     'taskManager',
      position: { x: rowStartX + col * X_SPACING, y: TM_Y + row * Y_SPACING },
      data: {
        label:         tm.name,
        podPhase:      tm.phase,
        podIP:         tm.podIP,
        chaosPhase:    chaos?.phase,
        chaosScenario: chaos?.scenario,
        isHighlighted: highlightedPods.has(tm.name),
      },
    })
  }

  // ── Level 3: Storage nodes ───────────────────────────────────────────────

  const STORAGE_Y  = TM_Y + (tmRows - 1) * Y_SPACING + Y_SPACING + LEVEL_GAP
  const storageStartX = centeredStartX(Math.max(storageCount, 1))

  for (let i = 0; i < storageCount; i++) {
    const conn = storageConns[i]
    nodes.push({
      id:       `storage-${i}`,
      type:     'storage',
      position: { x: storageStartX + i * X_SPACING, y: STORAGE_Y },
      data: {
        label:         conn.type,
        podPhase:      undefined,
        chaosPhase:    undefined,
        chaosScenario: undefined,
        storageType:   conn.type,
        purpose:       conn.purpose,
        endpoint:      conn.endpoint,
      },
    })
  }

  // ── Edges: JM → TM ──────────────────────────────────────────────────────

  for (let jmIdx = 0; jmIdx < jmCount; jmIdx++) {
    for (let tmIdx = 0; tmIdx < tmCount; tmIdx++) {
      const tmName   = data.taskManagers[tmIdx].name
      const scenario = tmNetworkScenario.get(tmName)
      edges.push({
        id:     `jm-${jmIdx}-tm-${tmIdx}`,
        source: `jm-${jmIdx}`,
        target: `tm-${tmIdx}`,
        type:   scenario ? 'chaos' : 'default',
        ...(scenario ? { data: { scenario } } : {}),
      })
    }
  }

  // ── Edges: TM → storage (dashed purple, bottom→top) ─────────────────────

  for (let tmIdx = 0; tmIdx < tmCount; tmIdx++) {
    for (let sIdx = 0; sIdx < storageCount; sIdx++) {
      const tmName   = data.taskManagers[tmIdx].name
      const scenario = tmNetworkScenario.get(tmName)
      edges.push({
        id:     `tm-${tmIdx}-s-${sIdx}`,
        source: `tm-${tmIdx}`,
        target: `storage-${sIdx}`,
        type:   scenario ? 'chaos' : 'default',
        ...(scenario
          ? { data: { scenario } }
          : {
              style: {
                stroke:          '#a855f7',
                strokeWidth:     1.5,
                strokeDasharray: '5 4',
                opacity:         0.55,
              },
            }),
      })
    }
  }

  return { nodes, edges }
}

export function useTopology() {
  const selectedDeploymentName = useAppStore((s) => s.selectedDeploymentName)

  const { data, isLoading, error } = useQuery({
    queryKey: ['topology', selectedDeploymentName],
    queryFn: () => fetchTopology(selectedDeploymentName ?? undefined),
    refetchInterval: 5000,
    retry: 2,
  })

  const selectedRunName = useAppStore((s) => s.selectedRunName)

  const [nodes, setNodes, onNodesChange] = useNodesState<Node<FlinkNodeData>>([])
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([])

  useEffect(() => {
    if (!data) return
    // Build the set of pod names belonging to the selected run.
    const highlightedPods = new Set<string>()
    if (selectedRunName) {
      const run = data.activeChaosRuns?.find((r) => r.name === selectedRunName)
      for (const pod of run?.targetPods ?? []) {
        highlightedPods.add(pod)
      }
    }
    const { nodes: newNodes, edges: newEdges } = buildGraph(data, highlightedPods)
    setNodes(newNodes)
    setEdges(newEdges)
  }, [data, selectedRunName, setNodes, setEdges])

  return {
    nodes,
    edges,
    onNodesChange,
    onEdgesChange,
    activeChaosRuns: data?.activeChaosRuns ?? [],
    deploymentName: data?.deploymentName ?? '',
    targetType: data?.targetType ?? 'FlinkDeployment',
    deploymentId: data?.deploymentId,
    vvpNamespace: data?.vvpNamespace,
    hasDeployment: data != null && (data.jobManagers?.length ?? 0) > 0,
    isLoading,
    error,
  }
}
