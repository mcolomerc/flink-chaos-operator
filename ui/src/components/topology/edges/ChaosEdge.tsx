import { memo } from 'react'
import { BaseEdge, getBezierPath } from '@xyflow/react'
import type { EdgeProps } from '@xyflow/react'

interface ChaosEdgeData extends Record<string, unknown> {
  scenario?: string
}

function ChaosEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
}: EdgeProps) {
  const [edgePath] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  })

  const edgeData = data as ChaosEdgeData | undefined
  const scenario = edgeData?.scenario ?? ''
  // NetworkChaos → red (latency/loss/bandwidth injection)
  // NetworkPartition and everything else → orange (binary block)
  const strokeColor = scenario === 'NetworkChaos' ? '#ef4444' : '#f97316'

  return (
    <BaseEdge
      id={id}
      path={edgePath}
      style={{
        stroke: strokeColor,
        strokeWidth: 2,
        strokeDasharray: '6 4',
        animation: 'dash 0.8s linear infinite',
      }}
    />
  )
}

export { ChaosEdge }
export default memo(ChaosEdge)
