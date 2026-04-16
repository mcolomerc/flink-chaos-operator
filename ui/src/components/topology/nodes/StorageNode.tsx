import { memo } from 'react'
import { Handle, Position } from '@xyflow/react'

interface StorageNodeData extends Record<string, unknown> {
  label: string
  storageType?: string
  purpose?: string
  endpoint?: string
}

function BucketIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.5}
      className="w-5 h-5 flex-shrink-0 text-purple-400"
      aria-hidden="true"
    >
      {/* Cylinder / bucket shape */}
      <ellipse cx="12" cy="6" rx="8" ry="3" />
      <path strokeLinecap="round" d="M4 6v12c0 1.657 3.582 3 8 3s8-1.343 8-3V6" />
      <path strokeLinecap="round" strokeDasharray="2 2" d="M4 12c0 1.657 3.582 3 8 3s8-1.343 8-3" />
    </svg>
  )
}

function purposeColor(purpose?: string): { border: string; badge: string } {
  if (purpose === 'checkpoint') return { border: 'border-purple-500', badge: 'bg-purple-900 text-purple-200 border-purple-700' }
  if (purpose === 'savepoint')  return { border: 'border-indigo-500',  badge: 'bg-indigo-900  text-indigo-200  border-indigo-700'  }
  return { border: 'border-slate-500', badge: 'bg-slate-700 text-slate-300 border-slate-600' }
}

function StorageNode({ data }: { data: StorageNodeData }) {
  const storageType = data.storageType ?? data.label ?? 'Storage'
  const purpose     = data.purpose ?? ''
  const endpoint    = data.endpoint ?? ''

  // Show only the bucket/path portion to keep the node compact
  const endpointShort = (() => {
    try {
      const url = new URL(endpoint)
      return url.host + (url.pathname.length > 1 ? url.pathname.slice(0, 20) + (url.pathname.length > 20 ? '…' : '') : '')
    } catch {
      return endpoint.length > 28 ? endpoint.slice(0, 28) + '…' : endpoint
    }
  })()

  const colors = purposeColor(purpose)

  return (
    <div
      className={[
        'flex flex-col gap-1',
        'bg-slate-800 rounded-lg border-2',
        colors.border,
        'w-[200px] px-3 py-2 shadow-md',
      ].join(' ')}
      style={{ boxSizing: 'border-box' }}
    >
      {/* Top handle — receives edges from TMs above */}
      <Handle type="target" position={Position.Top} className="!bg-purple-400" />

      {/* Header row */}
      <div className="flex items-center gap-2 min-w-0">
        <BucketIcon />
        <div className="flex flex-col min-w-0">
          <div className="flex items-center gap-1.5">
            <span className={`flex-shrink-0 inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-bold border ${colors.badge}`}>
              {storageType}
            </span>
            {purpose && (
              <span className="text-slate-400 text-[10px] truncate capitalize">{purpose}</span>
            )}
          </div>
          {endpointShort && (
            <span className="text-slate-500 text-[9px] truncate mt-0.5 font-mono" title={endpoint}>
              {endpointShort}
            </span>
          )}
        </div>
      </div>
    </div>
  )
}

export default memo(StorageNode)
