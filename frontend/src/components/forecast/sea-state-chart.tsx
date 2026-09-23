import { useId, useMemo } from 'react'
import { Area, ComposedChart, Customized, Line, ReferenceLine, XAxis, YAxis } from 'recharts'

import { WindBarb, WaveDirectionArrow } from '@/components/forecast/direction-glyphs'
import type { SeaStatePoint } from '@/lib/sea-state-series'
import type { WaveSteepnessBand } from '@/hooks/use-wave-forecast'
import { metersToFeet } from '@/lib/units'
import {
  HOURS_PER_DAY,
  SEA_STATE_CHART_MARGIN,
  SEA_STATE_DAY_COUNT,
  SEA_STATE_GLYPH_SCALE,
  SEA_STATE_PLOT_INSET,
  plotWidthFor,
  xForIndex,
} from '@/lib/sea-state-geometry'

export interface SeaStateChartProps {
  series: SeaStatePoint[]
  width: number
  height: number
  waveUnit: 'm' | 'ft'
  /**
   * Total day columns the chart divides its width into, regardless of how
   * many real days `series` actually covers (ADR 0125) - a series shorter
   * than `dayCount * 24` points (a forecast with fewer real days than the
   * tile shows card slots for) still gets `dayCount` equal-width columns,
   * the trailing ones simply empty, rather than stretching what data exists
   * across the full plot. Defaults to SEA_STATE_DAY_COUNT (5), today's only
   * caller (forecast-conditions-tile.tsx).
   */
  dayCount?: number
}

// HOURS_PER_DAY, SEA_STATE_DAY_COUNT, SEA_STATE_GLYPH_SCALE,
// SEA_STATE_CHART_MARGIN, SEA_STATE_PLOT_INSET and plotWidthFor/xForIndex/
// xForDayBoundary all moved to lib/sea-state-geometry.ts - they are pure
// pixel arithmetic with no recharts dependency, and forecast-conditions-
// tile.tsx (and this chart's own tests) need them without pulling recharts
// into the entry bundle. See that module's doc comment.
// Glyphs closer together than this read as a solid smear rather than
// individual barbs/arrows once the tile narrows below its widest columns.
const MIN_GLYPH_SPACING_PX = 22
const AXIS_LABEL_FONT_SIZE = 10
const AXIS_LABEL_COLOR = 'hsl(var(--muted-foreground))'

// Duplicated from forecast-drawer.tsx's WAVE_STEEPNESS_STROKE rather than
// imported: that map is a private module constant there, and coupling this
// wall-display chart to the drawer's internals for four colour values is a
// worse trade than the duplication. Same four tokens, same meaning.
const WAVE_STEEPNESS_STROKE: Record<string, string> = {
  rolling: 'hsl(var(--chart-wave) / 0.9)',
  building: 'hsl(var(--chart-wave) / 0.9)',
  steep: 'hsl(var(--chart-gust))',
  breaking: 'hsl(var(--destructive))',
}

function niceMax(...values: (number | null | undefined)[]): number {
  const usable = values.filter((v): v is number => typeof v === 'number' && Number.isFinite(v))
  const max = usable.length > 0 ? Math.max(...usable) : 10
  return Math.max(5, Math.ceil((max + 2) / 5) * 5)
}

/** "2026-06-14" -> "Sun", read as UTC so the label never depends on the reader's own timezone rolling the date. */
function weekdayLabel(dayKey: string): string {
  const date = new Date(`${dayKey}T00:00:00Z`)
  if (Number.isNaN(date.getTime())) return dayKey
  return new Intl.DateTimeFormat('en-US', { weekday: 'short', timeZone: 'UTC' }).format(date)
}

/**
 * The wall display's five-day sea-state chart (ADR 0092): wind (with gust)
 * on the left axis, wave height on the right, coloured by steepness band the
 * same way the forecast drawer's own wave chart is, plus a wind-barb and
 * wave-direction-arrow row every three hours. Presentational only - the
 * join, the sentinel-to-null mapping and the day boundaries all live in
 * lib/sea-state-series.ts; this component only draws what it's given.
 *
 * Fixed-margin arithmetic rather than reading Recharts' internal x-scale
 * (the technique forecast-drawer.tsx already uses for its own barb/arrow
 * rows): the <Customized> layer needs pixel coordinates, and matching the
 * same margin object passed to <ComposedChart> keeps the two in agreement
 * without depending on Recharts' internal APIs across versions.
 */
export function SeaStateChart({ series, width, height, waveUnit, dayCount = SEA_STATE_DAY_COUNT }: SeaStateChartProps) {
  const windGradientId = useId()
  const waveGradientId = useId()
  const waveSteepnessGradientId = useId()

  const margin = SEA_STATE_CHART_MARGIN
  // The actual plotted x-range's pixel bounds - see SEA_STATE_PLOT_INSET's
  // own comment for why this isn't simply margin.left/margin.right.
  const plotLeft = SEA_STATE_PLOT_INSET.left
  const plotRight = width - SEA_STATE_PLOT_INSET.right
  const plotWidth = plotWidthFor(width)
  // Fixed at dayCount columns, not series.length: see SeaStateChartProps.dayCount.
  const totalPoints = dayCount * HOURS_PER_DAY
  const barbY = margin.top / 2
  const arrowY = height - margin.bottom / 2 - 4

  // Glyph step in hours: always a multiple of 3 (the finest resolution the
  // data supports), widened just enough that adjacent glyphs stay at least
  // MIN_GLYPH_SPACING_PX apart once the tile is narrower than its widest
  // grid span.
  const glyphStepHours = 3 * Math.ceil((MIN_GLYPH_SPACING_PX * totalPoints) / plotWidth / 3)

  const middayTicks = Array.from({ length: dayCount }, (_, d) => d * HOURS_PER_DAY + HOURS_PER_DAY / 2 - 0.5)
  const dayBoundaries = Array.from({ length: dayCount - 1 }, (_, d) => (d + 1) * HOURS_PER_DAY - 0.5)
  const tickLabelByIndex = new Map(
    Array.from({ length: dayCount }, (_, d) => [d * HOURS_PER_DAY + HOURS_PER_DAY / 2 - 0.5, weekdayLabel(series[d * HOURS_PER_DAY]?.dayKey ?? '')]),
  )

  const chartData = useMemo(
    () =>
      series.map((p) => ({
        index: p.index,
        windKts: p.windKts,
        gustKts: p.gustKts,
        waveDisplay: p.waveM === null ? null : waveUnit === 'ft' ? metersToFeet(p.waveM) : p.waveM,
      })),
    [series, waveUnit],
  )

  const windMax = niceMax(...series.map((p) => p.gustKts), ...series.map((p) => p.windKts))
  const waveValues = series.map((p) => (p.waveM === null ? null : waveUnit === 'ft' ? metersToFeet(p.waveM) : p.waveM))
  const waveMax = niceMax(...waveValues)

  // Hard-edged gradient stops, one band per hour, mirroring
  // forecast-drawer.tsx's waveSteepnessStops: a wave either breaks or it
  // does not, so the colour changes as a step at the hour it describes
  // rather than blending toward it.
  const waveSteepnessStops = useMemo(() => {
    const banded = series
      .map((p, i) => ({ i, band: p.steepnessBand }))
      .filter((p): p is { i: number; band: WaveSteepnessBand } => p.band !== null)
    return banded.flatMap(({ i, band }, idx) => {
      const colour = WAVE_STEEPNESS_STROKE[band as string] ?? WAVE_STEEPNESS_STROKE.rolling
      // Same (index + 0.5) / totalPoints mapping as xForIndex above, since
      // this gradient is painted in the identical plotLeft..plotRight pixel
      // span (userSpaceOnUse) the Line/Area series and the Customized glyph
      // layer are drawn in.
      const start = Math.min(1, Math.max(0, (i + 0.5) / totalPoints))
      const next = banded[idx + 1]
      const end = next === undefined ? 1 : Math.min(1, Math.max(0, (next.i + 0.5) / totalPoints))
      return [
        { key: `${idx}-a`, offset: start, colour },
        { key: `${idx}-b`, offset: end, colour },
      ]
    })
  }, [series, totalPoints])

  const glyphIndices: number[] = []
  for (let i = 0; i < series.length; i += glyphStepHours) glyphIndices.push(i)

  return (
    <ComposedChart width={width} height={height} data={chartData} margin={margin}>
      <XAxis
        dataKey="index"
        type="number"
        domain={[-0.5, totalPoints - 0.5]}
        ticks={middayTicks}
        tickFormatter={(value: number) => tickLabelByIndex.get(value) ?? ''}
        axisLine={false}
        tickLine={false}
        tick={{ fontSize: AXIS_LABEL_FONT_SIZE, fill: AXIS_LABEL_COLOR }}
        height={20}
      />
      <YAxis
        yAxisId="wind"
        domain={[0, windMax]}
        tick={{ fontSize: AXIS_LABEL_FONT_SIZE, fill: AXIS_LABEL_COLOR }}
        axisLine={false}
        tickLine={false}
        width={margin.left}
        // Unit folded into the top tick's own text (e.g. "50 kn") rather
        // than a separate corner label, which used to collide with the
        // barb row above the plot.
        tickFormatter={(value: number) => (value === windMax ? `${value} kn` : `${value}`)}
      />
      <YAxis
        yAxisId="wave"
        orientation="right"
        domain={[0, waveMax]}
        tick={{ fontSize: AXIS_LABEL_FONT_SIZE, fill: AXIS_LABEL_COLOR }}
        axisLine={false}
        tickLine={false}
        width={margin.right}
        tickFormatter={(value: number) => (value === waveMax ? `${value} ${waveUnit}` : `${value}`)}
      />

      <defs>
        <linearGradient id={windGradientId} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="hsl(var(--chart-gust))" stopOpacity="0.25" />
          <stop offset="100%" stopColor="hsl(var(--chart-gust))" stopOpacity="0.02" />
        </linearGradient>
        <linearGradient id={waveGradientId} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="hsl(var(--chart-wave))" stopOpacity="0.25" />
          <stop offset="100%" stopColor="hsl(var(--chart-wave))" stopOpacity="0.02" />
        </linearGradient>
        <linearGradient id={waveSteepnessGradientId} gradientUnits="userSpaceOnUse" x1={plotLeft} y1="0" x2={plotRight} y2="0">
          {waveSteepnessStops.map((stop) => (
            <stop key={stop.key} offset={`${stop.offset * 100}%`} stopColor={stop.colour} />
          ))}
        </linearGradient>
      </defs>

      {dayBoundaries.map((x) => (
        <ReferenceLine
          key={x}
          x={x}
          yAxisId="wind"
          stroke="hsl(var(--border))"
          strokeDasharray="2 2"
          data-testid="sea-state-day-boundary"
        />
      ))}

      <Area
        yAxisId="wind"
        dataKey="gustKts"
        type="monotone"
        isAnimationActive={false}
        dot={false}
        connectNulls={false}
        stroke="none"
        fill={`url(#${windGradientId})`}
      />
      <Line
        yAxisId="wind"
        dataKey="windKts"
        type="monotone"
        isAnimationActive={false}
        dot={false}
        connectNulls={false}
        stroke="hsl(var(--chart-wind) / 0.95)"
        strokeWidth={2}
      />

      <Area
        yAxisId="wave"
        dataKey="waveDisplay"
        type="monotone"
        isAnimationActive={false}
        dot={false}
        connectNulls={false}
        stroke="none"
        fill={`url(#${waveGradientId})`}
      />
      <Line
        yAxisId="wave"
        dataKey="waveDisplay"
        type="monotone"
        isAnimationActive={false}
        dot={false}
        connectNulls={false}
        stroke={waveSteepnessStops.length > 0 ? `url(#${waveSteepnessGradientId})` : 'hsl(var(--chart-wave) / 0.9)'}
        strokeWidth={2}
      />

      <Customized
        component={() => (
          <>
            {glyphIndices.map((i) => {
              const point = series[i]
              const x = xForIndex(i, width, dayCount)
              return (
                <g key={i}>
                  <WindBarb cx={x} cy={barbY} speedKts={point.windKts ?? -1} directionDeg={point.windDirDeg ?? -1} scale={SEA_STATE_GLYPH_SCALE} />
                  <WaveDirectionArrow cx={x} cy={arrowY} directionDeg={point.waveDirDeg ?? -1} scale={SEA_STATE_GLYPH_SCALE} />
                </g>
              )
            })}
          </>
        )}
      />
    </ComposedChart>
  )
}
