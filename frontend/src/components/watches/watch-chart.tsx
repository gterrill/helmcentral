import { ChartLine } from 'lucide-react'
import { CartesianGrid, Line, LineChart, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'

import { axisTicks } from '@/lib/telemetry-chart'
import { panelRows } from '@/lib/watch-chart'
import type { WatchChartData } from '@/lib/watches'

// A watch finding's chart (ADR 0228), drawn from the points stored with the
// run. It looks like the chart under a Mate reply: the same series tokens, axis
// text and stacked panels with one unit each. Unlike that card it holds its own
// data, so it has no range controls and never fetches.
const SERIES_COLOURS = ['hsl(var(--chart-wind))', 'hsl(var(--chart-wave))']
const AXIS_TICK = { fontSize: 10, fill: 'hsl(var(--muted-foreground))' }
const TOOLTIP_STYLE = { background: 'hsl(var(--card))', border: '1px solid hsl(var(--border))', fontSize: 12 }
const PANEL_HEIGHT = 150

/** A time on the boat's clock: the offset is added and the result read as UTC. */
function boatFormatter(offsetSeconds: number, withDay: boolean) {
  const fmt = new Intl.DateTimeFormat('en-GB', {
    timeZone: 'UTC',
    ...(withDay ? { weekday: 'short' } : {}),
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  })
  return (ms: number) => fmt.format(new Date(ms + offsetSeconds * 1000))
}

function withUnit(label: string, unit: string): string {
  return unit ? `${label} (${unit})` : label
}

export function WatchChart({ chart }: { chart: WatchChartData }) {
  const from = Date.parse(chart.window.from)
  const to = Date.parse(chart.window.to)
  const short = chart.series.find((s) => s.points.length < 2)
  if (short || chart.series.length === 0 || !(to > from)) {
    const why = short
      ? `${short.label} has ${short.points.length} ${short.points.length === 1 ? 'reading' : 'readings'}`
      : 'there is nothing to plot'
    return (
      <div role="alert" className="min-w-0 rounded-md border border-red-500/40 bg-red-50 p-3 text-xs text-red-800 dark:bg-red-950 dark:text-red-300">
        Cannot draw this chart: {why}
      </div>
    )
  }

  const span = to - from
  const dayFmt = boatFormatter(chart.utc_offset_seconds, true)
  const clockFmt = boatFormatter(chart.utc_offset_seconds, span > 36 * 3_600_000)
  const ticks = Array.from({ length: 5 }, (_, i) => from + (span * i) / 4)

  // One panel for each unit, in the order the units first appear.
  const units = [...new Set(chart.series.map((s) => s.unit))]
  const panels = units.map((unit, pi) => {
    const indices = chart.series.map((s, i) => (s.unit === unit ? i : -1)).filter((i) => i >= 0)
    const threshold = pi === 0 ? chart.threshold : undefined
    const values = indices.flatMap((i) => chart.series[i].points.map((p) => p[1]))
    if (threshold) values.push(threshold.value)
    return { unit, indices, threshold, data: panelRows(chart, indices), yTicks: axisTicks(values, unit || null) }
  })

  return (
    <section
      aria-label="Chart"
      data-testid="watch-chart"
      className="flex min-w-0 flex-col gap-2 rounded-md border border-border bg-card p-3"
    >
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
        <ChartLine className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
        <span className="shrink-0 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Chart</span>
        <span className="min-w-0 flex-1 line-clamp-2 text-sm font-medium text-foreground" title={chart.title}>
          {chart.title}
        </span>
        <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
          {dayFmt(from)} – {dayFmt(to)}
        </span>
        <span className="shrink-0 text-xs text-muted-foreground">{chart.timezone}</span>
      </div>
      <div role="img" aria-label={`${chart.title}, ${dayFmt(from)} to ${dayFmt(to)} ${chart.timezone}`} className="flex min-w-0 flex-col gap-2">
        {panels.map((p, pi) => (
          <div key={p.unit} data-testid="watch-chart-panel" className="min-w-0">
            <ul className="flex min-w-0 flex-wrap gap-x-3 gap-y-1">
              {p.indices.map((i) => (
                <li key={i} className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
                  <span className="h-0.5 w-3 shrink-0 rounded-full" style={{ background: SERIES_COLOURS[i % SERIES_COLOURS.length] }} aria-hidden="true" />
                  <span className="min-w-0 truncate">{withUnit(chart.series[i].label, chart.series[i].unit)}</span>
                </li>
              ))}
              {p.threshold && (
                <li className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
                  <span className="h-0 w-3 shrink-0 border-t border-dashed border-muted-foreground" aria-hidden="true" />
                  <span className="min-w-0 truncate">{p.threshold.label}</span>
                </li>
              )}
            </ul>
            <ResponsiveContainer width="100%" height={PANEL_HEIGHT}>
              <LineChart data={p.data} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
                <CartesianGrid stroke="hsl(var(--border))" strokeDasharray="2 2" vertical={false} />
                <XAxis
                  dataKey="t"
                  type="number"
                  scale="time"
                  domain={[from, to]}
                  ticks={ticks}
                  tick={pi === panels.length - 1 ? AXIS_TICK : false}
                  {...(pi === panels.length - 1 ? {} : { height: 4 })}
                  tickLine={false}
                  axisLine={false}
                  tickFormatter={clockFmt}
                />
                <YAxis
                  domain={[p.yTicks[0], p.yTicks[p.yTicks.length - 1]]}
                  ticks={p.yTicks}
                  interval={0}
                  tick={AXIS_TICK}
                  tickLine={false}
                  axisLine={false}
                  width={52}
                  tickFormatter={(v: number) => (v === p.yTicks[p.yTicks.length - 1] && p.unit ? `${v} ${p.unit}` : `${v}`)}
                />
                <Tooltip contentStyle={TOOLTIP_STYLE} labelFormatter={(ms: number) => `${dayFmt(ms)} ${chart.timezone}`} />
                {p.threshold && <ReferenceLine y={p.threshold.value} stroke="hsl(var(--muted-foreground))" strokeDasharray="5 4" ifOverflow="extendDomain" />}
                {p.indices.map((i) => (
                  <Line
                    key={i}
                    dataKey={`s${i}`}
                    name={chart.series[i].label}
                    type="monotone"
                    stroke={SERIES_COLOURS[i % SERIES_COLOURS.length]}
                    strokeWidth={1.5}
                    dot={false}
                    connectNulls={false}
                    isAnimationActive={false}
                  />
                ))}
              </LineChart>
            </ResponsiveContainer>
          </div>
        ))}
      </div>
    </section>
  )
}
