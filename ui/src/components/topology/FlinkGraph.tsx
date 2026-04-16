import { useEffect, useRef } from 'react'
import {
  ReactFlow,
  Background,
  BackgroundVariant,
  Controls,
  MiniMap,
  type NodeTypes,
  type EdgeTypes,
  type Node,
} from '@xyflow/react'
import { useTopology } from '../../hooks/useTopology'
import { useAppStore } from '../../store'
import JobManagerNode from './nodes/JobManagerNode'
import TaskManagerNode from './nodes/TaskManagerNode'
import StorageNode from './nodes/StorageNode'
import ChaosEdge from './edges/ChaosEdge'

// React Flow's NodeTypes/EdgeTypes require specific callable signatures;
// casting via unknown is the standard pattern when using per-component data generics.
const nodeTypes: NodeTypes = {
  jobManager:  JobManagerNode  as unknown as NodeTypes[string],
  taskManager: TaskManagerNode as unknown as NodeTypes[string],
  storage:     StorageNode     as unknown as NodeTypes[string],
}

const edgeTypes: EdgeTypes = {
  chaos: ChaosEdge as unknown as EdgeTypes[string],
}

function miniMapNodeColor(node: Node): string {
  switch (node.type) {
    case 'jobManager':  return '#3b82f6'  // blue-500
    case 'taskManager': return '#22c55e'  // green-500
    case 'storage':     return '#a855f7'  // purple-500
    default:            return '#64748b'  // slate-500
  }
}

export default function FlinkGraph() {
  const {
    nodes: topologyNodes,
    edges: topologyEdges,
    onNodesChange,
    onEdgesChange,
    hasDeployment,
    isLoading,
    error,
  } = useTopology()

  const rfInstance = useRef<{ fitView: (opts?: { padding?: number; duration?: number }) => void } | null>(null)
  const metricsExpanded = useAppStore((s) => s.metricsExpanded)

  // Re-fit whenever the metrics panel is toggled (container height changes).
  useEffect(() => {
    if (!rfInstance.current) return
    const id = setTimeout(() => rfInstance.current?.fitView({ padding: 0.2, duration: 200 }), 50)
    return () => clearTimeout(id)
  }, [metricsExpanded])

  return (
    <div className="relative w-full h-full">
      <ReactFlow
        nodes={topologyNodes}
        edges={topologyEdges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onInit={(instance) => { rfInstance.current = instance }}
        fitView
        fitViewOptions={{ padding: 0.2 }}
        minZoom={0.3}
        maxZoom={2}
        className="bg-slate-900"
      >
        <Background
          variant={BackgroundVariant.Dots}
          color="#334155"
          gap={20}
          size={1}
        />
        <Controls className="!bottom-4 !left-4 !top-auto" />
        <MiniMap
          nodeColor={miniMapNodeColor}
          maskColor="rgba(15,17,23,0.7)"
          className="!bottom-4 !right-4 !top-auto border border-slate-700 rounded"
        />
      </ReactFlow>

      {/* Loading overlay */}
      {isLoading && (
        <div className="absolute inset-0 flex items-center justify-center bg-slate-900/60 pointer-events-none">
          <div className="flex flex-col items-center gap-3">
            <div className="w-8 h-8 border-2 border-blue-500 border-t-transparent rounded-full animate-spin" />
            <span className="text-slate-400 text-sm">Loading topology…</span>
          </div>
        </div>
      )}

      {/* Error overlay */}
      {error && !isLoading && (
        <div className="absolute inset-0 flex items-center justify-center bg-slate-900/80 pointer-events-none">
          <div className="text-center px-6 py-4 rounded-lg border border-red-800 bg-red-950/50">
            <p className="text-red-400 font-semibold mb-1">Failed to load topology</p>
            <p className="text-red-300/70 text-sm">{(error as Error).message}</p>
          </div>
        </div>
      )}

      {/* Empty state (connected but no JMs detected) */}
      {!isLoading && !error && !hasDeployment && topologyNodes.length === 0 && (
        <div className="absolute inset-0 flex items-center justify-center pointer-events-none">
          <div className="text-center px-6 py-4 rounded-lg border border-slate-700 bg-slate-800/80">
            <p className="text-slate-300 font-semibold mb-1">No Flink deployment detected</p>
            <p className="text-slate-500 text-sm">Waiting for JobManagers to appear in the cluster…</p>
          </div>
        </div>
      )}
    </div>
  )
}
