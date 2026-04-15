import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ReferenceArea,
  ReferenceLine,
  ResponsiveContainer,
} from 'recharts'
import type { CheckpointSample } from '../../hooks/useFlinkMetrics'
import type { ChaosRunSummary } from '../../api/client'

interface Props {
  history: CheckpointSample[]
  chaosRuns: ChaosRunSummary[]
}

function formatTime(ms: number): string {
  const d = new Date(ms)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}:${String(d.getSeconds()).padStart(2, '0')}`
}

interface TooltipPayload {
  value: number
  payload: CheckpointSample
}

function CustomTooltip({ active, payload }: { active?: boolean; payload?: TooltipPayload[] }) {
  if (!active || !payload?.length) return null
  const sample = payload[0]
  return (
    <div className="bg-slate-800 border border-slate-600 rounded px-2 py-1.5 text-xs shadow-lg">
      <p className="text-slate-300">{formatTime(sample.payload.sampledAt)}</p>
      <p className="text-cyan-400 font-semibold">{sample.value}s checkpoint age</p>
    </div>
  )
}

export default function CheckpointChart({ history, chaosRuns }: Props) {
  if (history.length === 0) {
    return (
      <div className="flex items-center justify-center h-full text-slate-600 text-xs">
        Waiting for checkpoint data…
      </div>
    )
  }

  // Build the chart data with a human-readable time label.
  const chartData = history.map((s) => ({
    ...s,
    timeLabel: formatTime(s.sampledAt),
  }))

  // Build chaos bands: each run gets a colored area from startedAt to endedAt
  // (or now for active runs). Only show bands that overlap the visible window.
  const windowStart = history[0]?.sampledAt ?? 0
  const windowEnd = history[history.length - 1]?.sampledAt ?? Date.now()
  const chaosBands = chaosRuns
    .filter((r) => r.startedAt)
    .map((r) => ({
      name: r.name,
      scenario: r.scenario,
      x1: new Date(r.startedAt!).getTime(),
      x2: r.endedAt ? new Date(r.endedAt).getTime() : Date.now(),
    }))
    .filter((b) => b.x2 >= windowStart && b.x1 <= windowEnd)

  return (
    <ResponsiveContainer width="100%" height="100%">
      <LineChart data={chartData} margin={{ top: 6, right: 12, bottom: 0, left: 0 }}>
        <CartesianGrid strokeDasharray="3 3" stroke="#1e293b" />
        <XAxis
          dataKey="sampledAt"
          type="number"
          domain={['dataMin', 'dataMax']}
          scale="time"
          tickFormatter={formatTime}
          tick={{ fill: '#64748b', fontSize: 9 }}
          tickLine={false}
          axisLine={{ stroke: '#334155' }}
          minTickGap={40}
        />
        <YAxis
          dataKey="ageSeconds"
          tick={{ fill: '#64748b', fontSize: 9 }}
          tickLine={false}
          axisLine={false}
          width={36}
          tickFormatter={(v: number) => `${v}s`}
        />
        <Tooltip content={<CustomTooltip />} />
        <Line
          type="monotone"
          dataKey="ageSeconds"
          stroke="#22d3ee"
          strokeWidth={1.5}
          dot={false}
          activeDot={{ r: 3, fill: '#22d3ee' }}
          isAnimationActive={false}
        />
        {chaosBands.map((b) => (
          <ReferenceArea
            key={b.name}
            x1={b.x1}
            x2={b.x2}
            fill="#ef4444"
            fillOpacity={0.12}
            stroke="#ef4444"
            strokeOpacity={0.4}
            strokeWidth={1}
          />
        ))}
        {chaosBands.map((b) => (
          <ReferenceLine
            key={`start-${b.name}`}
            x={b.x1}
            stroke="#ef4444"
            strokeDasharray="4 3"
            strokeWidth={1.5}
            label={{
              value: b.scenario,
              position: 'insideTopRight',
              fill: '#ef4444',
              fontSize: 9,
            }}
          />
        ))}
      </LineChart>
    </ResponsiveContainer>
  )
}
