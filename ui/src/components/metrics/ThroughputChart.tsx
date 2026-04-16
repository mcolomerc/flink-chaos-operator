import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  ReferenceArea,
  ReferenceLine,
  ResponsiveContainer,
} from 'recharts'
import type { ThroughputSample } from '../../hooks/useFlinkMetrics'
import type { ChaosRunSummary } from '../../api/client'

interface Props {
  history: ThroughputSample[]
  chaosRuns: ChaosRunSummary[]
}

function formatRate(value: number): string {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`
  return value.toFixed(0)
}

function formatTime(ms: number): string {
  const d = new Date(ms)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}:${String(d.getSeconds()).padStart(2, '0')}`
}

export default function ThroughputChart({ history, chaosRuns }: Props) {
  if (history.length < 2) {
    return (
      <div className="flex items-center justify-center h-full text-slate-600 text-xs">
        Collecting throughput samples…
      </div>
    )
  }

  const data = history.map((s) => ({
    t: s.sampledAt,
    in: Math.round(s.recordsInPerSec),
    out: Math.round(s.recordsOutPerSec),
  }))

  const windowStart = data[0].t
  const windowEnd = data[data.length - 1].t

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
      <LineChart data={data} margin={{ top: 6, right: 12, left: 0, bottom: 0 }}>
        <XAxis
          dataKey="t"
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
          tick={{ fill: '#475569', fontSize: 9 }}
          tickLine={false}
          axisLine={false}
          tickFormatter={formatRate}
          width={36}
        />
        <Tooltip
          contentStyle={{ background: '#0f172a', border: '1px solid #334155', borderRadius: 4, fontSize: 11 }}
          labelStyle={{ color: '#94a3b8' }}
          labelFormatter={(v) => formatTime(Number(v))}
          formatter={(value, name) => [
            `${formatRate(Number(value ?? 0))} rec/s`,
            name === 'in' ? 'Records In' : 'Records Out',
          ]}
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
        <Line
          type="monotone"
          dataKey="in"
          stroke="#22d3ee"
          strokeWidth={1.5}
          dot={false}
          isAnimationActive={false}
        />
        <Line
          type="monotone"
          dataKey="out"
          stroke="#a78bfa"
          strokeWidth={1.5}
          dot={false}
          isAnimationActive={false}
        />
      </LineChart>
    </ResponsiveContainer>
  )
}
