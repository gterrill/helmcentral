import { ChartLine } from 'lucide-react'
import { useId, useMemo, useState } from 'react'
import { Area, CartesianGrid, ComposedChart, Label, Line, ResponsiveContainer, Scatter, ScatterChart, Tooltip, XAxis, YAxis } from 'recharts'

import { Skeleton } from '@/components/ui/skeleton'
import { useIsMobile } from '@/hooks/use-mobile'
import { useTelemetrySeries, type TelemetrySeriesBody } from '@/hooks/use-telemetry-series'
import type { TelemetryChartCard } from '@/lib/mate-cards'
import {
  DAY_MS,
  angleAxis,
  angleTickLabel,
  bandKey,
  buildTimeRows,
  dayTicks,
  drawnUnit,
  formatRange,
  axisTicks,
  pairXY,
  panelAngleKind,
  panelsOf,
  seriesKey,
  seriesValues,
  zonedDayStart,
  zonedYmd,
} from '@/lib/telemetry-chart'
import { cn } from '@/lib/utils'

// Series colours: the forecast charts' own series tokens, none of them alert
// colours. Series i (time view) or range i (X-Y view) takes slot i.
const SERIES_COLOURS = ['hsl(var(--chart-wind))', 'hsl(var(--chart-wave))', 'hsl(var(--chart-swell))', 'hsl(var(--chart-gust))']
const AXIS_TICK = { fontSize: 10, fill: 'hsl(var(--muted-foreground))' }
const MAX_RANGES = 3
const PRESETS = [
  { label: '24 h', days: 1 },
  { label: '7 d', days: 7 },
  { label: '30 d', days: 30 },
] as const

interface RangeSpec {
  label: string
  from: string
  to: string
}

/** A label with its unit in brackets; no brackets at all for a unitless series. */
function withUnit(label: string, unit: string): string {
  return unit ? `${label} (${unit})` : label
}

function topTickFormatter(ticks: number[], unit: string | null) {
  const top = ticks[ticks.length - 1]
  return (v: number) => (v === top && unit ? `${v} ${unit}` : `${v}`)
}

function timeTickFormatter(spanMs: number, timeZone: string) {
  const fmt = new Intl.DateTimeFormat(
    'en-GB',
    spanMs <= 2 * DAY_MS
      ? { timeZone, hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }
      : { timeZone, day: 'numeric', month: 'short' },
  )
  return (ms: number) => fmt.format(new Date(ms))
}

function Legend({ items }: { items: { label: string; colour: string }[] }) {
  return (
    <ul className="flex min-w-0 flex-wrap gap-x-3 gap-y-1">
      {items.map((it) => (
        <li key={it.label} className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
          <span className="h-0.5 w-3 shrink-0 rounded-full" style={{ background: it.colour }} aria-hidden="true" />
          <span className="min-w-0 truncate">{it.label}</span>
        </li>
      ))}
    </ul>
  )
}

const TOOLTIP_STYLE = { background: 'hsl(var(--card))', border: '1px solid hsl(var(--border))', fontSize: 12 }

/**
 * One stacked panel: one unit, one legend, the shared time axis. A scalar is a
 * mean line over its min-max band. An angle is points on a 0-360 (or -180-180)
 * scale, so a swing through north is a jump between dots, not a line across the
 * chart. Only the lowest panel shows the time labels.
 */
function TimePanel({
  card,
  indices,
  rows,
  bodies,
  from,
  to,
  height,
  last,
}: {
  card: TelemetryChartCard
  indices: number[]
  rows: ReturnType<typeof buildTimeRows>
  bodies: (TelemetrySeriesBody | undefined)[]
  from: number
  to: number
  height: number
  last: boolean
}) {
  const kind = panelAngleKind(bodies, indices)
  const unit = drawnUnit(card.series, bodies, indices[0])
  const angle = kind ? angleAxis(kind) : null
  const ticks = angle ? angle.ticks : axisTicks(indices.flatMap((i) => seriesValues(bodies, i, true)), unit)
  const domain: [number, number] = angle ? angle.domain : [ticks[0], ticks[ticks.length - 1]]
  const dayTickList = dayTicks(from, to, card.timezone)
  return (
    <div data-testid="chart-panel" className="min-w-0">
      <Legend items={indices.map((i) => ({ label: withUnit(card.series[i].label, drawnUnit(card.series, bodies, i)), colour: SERIES_COLOURS[i] }))} />
      <ResponsiveContainer width="100%" height={height}>
        <ComposedChart data={rows} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
          <CartesianGrid stroke="hsl(var(--border))" strokeDasharray="2 2" vertical={false} />
          <XAxis
            dataKey="t"
            type="number"
            scale="time"
            domain={[from, to]}
            {...(dayTickList ? { ticks: dayTickList } : { tickCount: 5 })}
            tick={last ? AXIS_TICK : false}
            {...(last ? {} : { height: 4 })}
            tickLine={false}
            axisLine={false}
            tickFormatter={timeTickFormatter(to - from, card.timezone)}
          />
          <YAxis
            yAxisId="y"
            domain={domain}
            ticks={ticks}
            interval={0}
            tick={AXIS_TICK}
            tickLine={false}
            axisLine={false}
            width={44}
            tickFormatter={kind ? (v: number) => angleTickLabel(kind, v) : topTickFormatter(ticks, unit)}
          />
          <Tooltip
            contentStyle={TOOLTIP_STYLE}
            labelFormatter={(ms: number) =>
              new Intl.DateTimeFormat('en-GB', { timeZone: card.timezone, day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(new Date(ms))
            }
            formatter={(v: number, _n, item) => {
              const i = Number(String(item.dataKey).slice(1))
              return [`${Math.round(v * 100) / 100} ${drawnUnit(card.series, bodies, i)}`.trim(), card.series[i].label]
            }}
          />
          {!angle &&
            indices.map((i) => (
              <Area
                key={`band-${i}`}
                yAxisId="y"
                dataKey={bandKey(i)}
                type="monotone"
                stroke="none"
                fill={SERIES_COLOURS[i]}
                fillOpacity={0.16}
                tooltipType="none"
                activeDot={false}
                connectNulls={false}
                isAnimationActive={false}
              />
            ))}
          {indices.map((i) =>
            angle ? (
              <Line
                key={i}
                yAxisId="y"
                dataKey={seriesKey(i)}
                stroke="none"
                dot={{ r: 2, fill: SERIES_COLOURS[i], stroke: 'none' }}
                activeDot={{ r: 3 }}
                connectNulls={false}
                isAnimationActive={false}
              />
            ) : (
              <Line
                key={i}
                yAxisId="y"
                dataKey={seriesKey(i)}
                type="monotone"
                stroke={SERIES_COLOURS[i]}
                strokeWidth={1.5}
                dot={false}
                connectNulls={false}
                isAnimationActive={false}
              />
            ),
          )}
        </ComposedChart>
      </ResponsiveContainer>
    </div>
  )
}

/** Stacked panels, one unit to a panel, on one time axis; one such stack per compared range. */
function TimePanels({
  card,
  ranges,
  bodies,
  height,
}: {
  card: TelemetryChartCard
  ranges: RangeSpec[]
  bodies: (TelemetrySeriesBody | undefined)[]
  height: number
}) {
  const panels = panelsOf(card.series)
  const stacked = ranges.length > 1
  const panelHeight = Math.max(120, Math.round(height * (panels.length > 1 ? 0.62 : 1) * (stacked ? 0.7 : 1)))
  return (
    <div className="flex min-w-0 flex-col gap-3">
      {ranges.map((r, ri) => {
        const body = bodies[ri]
        const rows = body ? buildTimeRows(body, card.series.length) : []
        const from = Date.parse(r.from)
        const to = Date.parse(r.to)
        return (
          <div key={ri} className="flex min-w-0 flex-col gap-1">
            {stacked && <p className="mb-0.5 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{r.label}</p>}
            {panels.map((pn, pi) => (
              <TimePanel
                key={pn.panel}
                card={card}
                indices={pn.indices}
                rows={rows}
                bodies={[body]}
                from={from}
                to={to}
                height={panelHeight}
                last={pi === panels.length - 1}
              />
            ))}
          </div>
        )
      })}
    </div>
  )
}

/** One line for each angle: how it was averaged, and the buckets that had no direction to average. */
function angleNotes(card: TelemetryChartCard, bodies: (TelemetrySeriesBody | undefined)[]): string[] {
  const notes: string[] = []
  card.series.forEach((s, i) => {
    const entries = bodies.map((b) => b?.series[i]).filter((e) => e?.angle)
    const method = entries.find((e) => e?.method)?.method
    if (!method) return
    const none = entries.reduce((n, e) => n + (e?.undefined_buckets ?? 0), 0)
    const blank = none > 0 ? ` No direction in ${none} ${none === 1 ? 'bucket' : 'buckets'} (calm, or too variable to average), left blank.` : ''
    notes.push(`${s.label}: ${method}.${blank}`)
  })
  return notes
}

function XYPlot({
  card,
  ranges,
  bodies,
  x,
  y,
  height,
}: {
  card: TelemetryChartCard
  ranges: RangeSpec[]
  bodies: (TelemetrySeriesBody | undefined)[]
  x: number
  y: number
  height: number
}) {
  const results = bodies.map((b) => (b ? pairXY(b, x, y) : { points: [], paired: 0, total: 0 }))
  const xTicks = axisTicks(results.flatMap((r) => r.points.map((p) => p.x)), drawnUnit(card.series, bodies, x))
  const yTicks = axisTicks(results.flatMap((r) => r.points.map((p) => p.y)), drawnUnit(card.series, bodies, y))
  const paired = results.reduce((n, r) => n + r.paired, 0)
  const total = results.reduce((n, r) => n + r.total, 0)
  const sx = { ...card.series[x], unit: drawnUnit(card.series, bodies, x) }
  const sy = { ...card.series[y], unit: drawnUnit(card.series, bodies, y) }
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <ResponsiveContainer width="100%" height={height}>
        <ScatterChart margin={{ top: 4, right: 8, bottom: 16, left: 0 }}>
          <CartesianGrid stroke="hsl(var(--border))" strokeDasharray="2 2" />
          <XAxis
            type="number"
            dataKey="x"
            name={sx.label}
            domain={[xTicks[0], xTicks[xTicks.length - 1]]}
            ticks={xTicks}
            tick={AXIS_TICK}
            tickLine={false}
            axisLine={false}
            tickFormatter={topTickFormatter(xTicks, sx.unit)}
          >
            <Label value={withUnit(sx.label, sx.unit)} position="bottom" offset={0} fontSize="10" fill="hsl(var(--muted-foreground))" />
          </XAxis>
          <YAxis
            type="number"
            dataKey="y"
            name={sy.label}
            domain={[yTicks[0], yTicks[yTicks.length - 1]]}
            ticks={yTicks}
            tick={AXIS_TICK}
            tickLine={false}
            axisLine={false}
            width={64}
            tickFormatter={topTickFormatter(yTicks, sy.unit)}
          >
            <Label value={withUnit(sy.label, sy.unit)} angle={-90} position="insideLeft" offset={14} style={{ textAnchor: 'middle' }} fontSize="10" fill="hsl(var(--muted-foreground))" />
          </YAxis>
          <Tooltip
            cursor={{ strokeDasharray: '2 2' }}
            contentStyle={{ background: 'hsl(var(--card))', border: '1px solid hsl(var(--border))', fontSize: 12 }}
          />
          {results.map((r, i) => (
            <Scatter key={i} name={ranges[i].label} data={r.points} fill={SERIES_COLOURS[i]} isAnimationActive={false} />
          ))}
        </ScatterChart>
      </ResponsiveContainer>
      {ranges.length > 1 && <Legend items={ranges.map((r, i) => ({ label: r.label, colour: SERIES_COLOURS[i] }))} />}
      <p className="text-xs tabular-nums text-muted-foreground">
        {paired} of {total} buckets paired
      </p>
    </div>
  )
}

/** The exclusive end for an inclusive local end date: the start of the next day, never past now. */
function inclusiveEnd(ymd: string, timeZone: string): string {
  const [y, m, d] = ymd.split('-').map(Number)
  const next = zonedDayStart(new Date(Date.UTC(y, m - 1, d + 1)).toISOString().slice(0, 10), timeZone)
  return new Date(Math.min(next, Date.now())).toISOString()
}

const chip = 'inline-flex h-7 items-center rounded px-2 text-xs font-medium leading-none'

/**
 * The telemetry chart under a Mate reply (ADR 0191): a time view with one line
 * per series on its own axis, or an X-Y view pairing two series by bucket. The
 * card holds a reference only; the points come from /api/telemetry/series and
 * refetch when the range changes. Compare ranges are fixed by the operator's
 * request, so they replace the range control with labelled chips.
 */
export function AssistantTelemetryChartCard({ card }: { card: TelemetryChartCard }) {
  const tz = card.timezone
  const isMobile = useIsMobile()
  const height = isMobile ? 180 : 220
  const fromId = useId()
  const toId = useId()

  // `toDay` is the local day the operator picked as the inclusive end; `to` is
  // then the start of the next day (or now). Null for the card's own range and the presets.
  const [range, setRange] = useState<{ from: string; to: string; toDay: string | null }>({ from: card.range.from, to: card.range.to, toDay: null })
  const defaultXY = card.xy ?? { x: 0, y: 1 }
  const [view, setView] = useState<'time' | 'xy'>(card.view)

  const shownEnd = range.toDay ? new Date(zonedDayStart(range.toDay, tz)).toISOString() : range.to
  const comparing = card.compare.length > 0
  const ranges: RangeSpec[] = useMemo(
    () => (comparing ? card.compare : [{ label: formatRange(range.from, shownEnd, tz), from: range.from, to: range.to }]).slice(0, MAX_RANGES),
    [comparing, card.compare, range, shownEnd, tz],
  )

  // A fixed three slots keep the hook count constant; unused ones stay disabled.
  const slot = (i: number) => ranges[i] ?? ranges[0]
  const q0 = useTelemetrySeries({ series: card.series, from: slot(0).from, to: slot(0).to })
  const q1 = useTelemetrySeries({ series: card.series, from: slot(1).from, to: slot(1).to, enabled: ranges.length > 1 })
  const q2 = useTelemetrySeries({ series: card.series, from: slot(2).from, to: slot(2).to, enabled: ranges.length > 2 })
  const queries = [q0, q1, q2].slice(0, ranges.length)
  const bodies = queries.map((q) => q.data)
  const errors = queries.map((q) => q.error).filter((e): e is string => e !== null)
  const loading = queries.some((q) => q.loading) || (errors.length === 0 && bodies.some((b) => b === undefined))

  const headerRange = formatRange(range.from, shownEnd, tz)
  const toMs = Date.parse(card.range.to)
  const spanDays = Math.round((Date.parse(range.to) - Date.parse(range.from)) / DAY_MS)
  const activePreset = Date.parse(range.to) === toMs ? PRESETS.find((p) => p.days === spanDays)?.label : undefined

  const missing: string[] = []
  card.series.forEach((s, i) => {
    ranges.forEach((r, ri) => {
      const entry = bodies[ri]?.series[i]
      if (entry && !entry.found) missing.push(`No ${s.label} recorded in ${comparing ? r.label : 'this range'}`)
    })
  })

  const plotted = view === 'xy' && card.series.length >= 2 ? (card.view === 'xy' ? defaultXY : { x: 0, y: 1 }) : null
  const notes = angleNotes(card, bodies)

  return (
    <section
      aria-label="Telemetry chart"
      data-testid="assistant-telemetry-chart-card"
      className="flex min-w-0 flex-col gap-2 rounded-md border border-border bg-card p-3"
    >
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
        <ChartLine className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
        <span className="shrink-0 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Chart</span>
        <span className="min-w-0 flex-1 line-clamp-2 text-sm font-medium text-foreground" title={card.title}>
          {card.title}
        </span>
        {comparing ? (
          <ul className="flex min-w-0 basis-full flex-wrap gap-1.5" aria-label="Compared ranges">
            {card.compare.map((c, i) => (
              <li key={i} className={cn(chip, 'min-w-0 gap-1.5 bg-muted text-foreground')}>
                <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: SERIES_COLOURS[i] }} aria-hidden="true" />
                <span className="min-w-0 truncate">{c.label}</span>
                <span className="shrink-0 tabular-nums text-muted-foreground">{formatRange(c.from, c.to, tz)}</span>
              </li>
            ))}
          </ul>
        ) : (
          <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{headerRange}</span>
        )}
      </div>

      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5">
        {!comparing && (
          <>
            <div className="flex gap-1" role="group" aria-label="Range">
              {PRESETS.map((p) => (
                <button
                  key={p.label}
                  type="button"
                  aria-pressed={activePreset === p.label}
                  onClick={() => setRange({ from: new Date(toMs - p.days * DAY_MS).toISOString(), to: card.range.to, toDay: null })}
                  className={cn(chip, 'border border-border', activePreset === p.label ? 'bg-primary text-primary-foreground' : 'text-foreground hover:bg-muted')}
                >
                  {p.label}
                </button>
              ))}
            </div>
            <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
              <label htmlFor={fromId} className="text-xs text-muted-foreground">
                From
              </label>
              <input
                id={fromId}
                type="date"
                value={zonedYmd(Date.parse(range.from), tz)}
                onChange={(e) => e.target.value && setRange((r) => ({ ...r, from: new Date(zonedDayStart(e.target.value, tz)).toISOString() }))}
                className="h-7 min-w-0 rounded border border-border bg-background px-1.5 text-xs text-foreground"
              />
              <label htmlFor={toId} className="text-xs text-muted-foreground">
                To
              </label>
              <input
                id={toId}
                type="date"
                value={range.toDay ?? zonedYmd(Date.parse(range.to), tz)}
                onChange={(e) => e.target.value && setRange((r) => ({ ...r, to: inclusiveEnd(e.target.value, tz), toDay: e.target.value }))}
                className="h-7 min-w-0 rounded border border-border bg-background px-1.5 text-xs text-foreground"
              />
            </div>
          </>
        )}
        {card.series.length >= 2 && (
          <div className="flex gap-1 sm:ml-auto" role="group" aria-label="Chart view">
            {(['time', 'xy'] as const).map((v) => (
              <button
                key={v}
                type="button"
                aria-pressed={view === v}
                onClick={() => setView(v)}
                className={cn(chip, 'border border-border', view === v ? 'bg-primary text-primary-foreground' : 'text-foreground hover:bg-muted')}
              >
                {v === 'time' ? 'Time' : 'X–Y'}
              </button>
            ))}
          </div>
        )}
      </div>

      {errors.length > 0 ? (
        <div
          role="alert"
          className="min-w-0 rounded-md border border-red-500/40 bg-red-50 p-3 text-xs text-red-800 dark:bg-red-950 dark:text-red-300"
        >
          {[...new Set(errors)].join(' ')}
        </div>
      ) : loading ? (
        <Skeleton data-testid="telemetry-chart-skeleton" className="w-full" style={{ height }} />
      ) : (
        <div role="img" aria-label={`${card.title}, ${comparing ? card.compare.map((c) => c.label).join(' against ') : headerRange}`} className="min-w-0">
          {plotted ? (
            <XYPlot card={card} ranges={ranges} bodies={bodies} x={plotted.x} y={plotted.y} height={height} />
          ) : (
            <TimePanels card={card} ranges={ranges} bodies={bodies} height={height} />
          )}
        </div>
      )}

      {notes.map((n) => (
        <p key={n} className="min-w-0 text-xs text-muted-foreground">
          {n}
        </p>
      ))}
      {missing.map((m) => (
        <p key={m} className="min-w-0 text-xs text-muted-foreground">
          {m}
        </p>
      ))}
    </section>
  )
}
