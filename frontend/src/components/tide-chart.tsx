import { useMemo } from 'react'

import { Area, CartesianGrid, ComposedChart, Line, ReferenceDot, ReferenceLine, XAxis, YAxis } from 'recharts'

import { ChartTooltipBubble, ChartTooltipMarker, ChartUnavailableMessage } from '@/components/chart-tooltip'
import { useChartTooltip } from '@/hooks/use-chart-tooltip'
import type { TideChart as TideChartData } from '@/hooks/use-tide-chart'
import { useMeasuredWidth } from '@/hooks/use-measured-width'
import { classifyTidePhase } from '@/lib/tide-phase'
import { metersToFeet } from '@/lib/units'
import { cn } from '@/lib/utils'

const AXIS_LABEL_FONT_SIZE = '10'
const AXIS_LABEL_COLOR = 'hsl(var(--muted-foreground))'

const CHART_LEFT = 36
const CHART_RIGHT_MARGIN = 20
const CHART_TOP = 16
const CHART_BOTTOM = 125
const CURVE_STEPS = 12
// Fallback viewBox width used only before the container's actual pixel width
// has been measured (ResizeObserver hasn't fired yet, or in tests where it's
// stubbed as a no-op) - chosen to match the chart's previous fixed size.
const DEFAULT_VIEWPORT_WIDTH = 1000

// Maps the backend's richer tidal_phase string ("springs", "springs+2",
// "neaps-1", ...) down to the badge's coarser spring/neap/null shape. Prefer
// this over the local classifyTidePhase() heuristic whenever the backend
// field is present - see tidePhaseFor below - falling back to the heuristic
// only when it's absent, so behavior is unchanged for any response that
// never went through the shared backend classifier.
function derivePhaseFromTidalPhase(tidalPhase: string | undefined): 'spring' | 'neap' | null {
  if (!tidalPhase) return null
  if (tidalPhase.startsWith('springs')) return 'spring'
  if (tidalPhase.startsWith('neaps')) return 'neap'
  return null
}

// A small inline swatch used by the legend below, drawing an actual stroked
// <line> in the series' real color/width/dasharray - replacing the old fake
// swatches that stood a plain em-dash character in for the solid tide curve
// and a "┊" character in for the dashed "now" marker line, neither of which
// tracked the real stroke or dasharray. `kind="vertical"` mirrors the
// vertical dashed ReferenceLine drawn for the "now" marker; `kind="line"`
// (the default) mirrors every other horizontal series line.
function LegendSwatch({
  color,
  strokeWidth = 2,
  dasharray,
  kind = 'line',
}: {
  color: string
  strokeWidth?: number
  dasharray?: string
  kind?: 'line' | 'vertical'
}) {
  return (
    <svg width="14" height="8" viewBox="0 0 14 8" className="inline-block align-middle" aria-hidden="true">
      {kind === 'vertical' ? (
        <line x1="7" y1="0" x2="7" y2="8" stroke={color} strokeWidth={strokeWidth} strokeDasharray={dasharray} strokeLinecap="round" />
      ) : (
        <line x1="0" y1="4" x2="14" y2="4" stroke={color} strokeWidth={strokeWidth} strokeDasharray={dasharray} strokeLinecap="round" />
      )}
    </svg>
  )
}

// Amber for spring (bigger swings), teal for neap (calmer) - matching the
// app's existing primary/secondary accent tokens. Renders nothing outside a
// clear spring/neap window (see classifyTidePhase).
export function TidePhaseBadge({ phase, className }: { phase: 'spring' | 'neap' | null; className?: string }) {
  if (!phase) return null
  return (
    <span
      className={cn(
        'text-[10px] font-semibold uppercase tracking-[0.16em]',
        phase === 'spring' ? 'text-gauge-primary' : 'text-gauge-secondary',
        className,
      )}
    >
      {phase === 'spring' ? 'Spring Tide' : 'Neap Tide'}
    </span>
  )
}

interface TideChartProps {
  chart: TideChartData
  isImperial: boolean
  windowStart: Date // start of the day to display
  windowEnd: Date // windowStart + 24h
}

export function TideChart({ chart, isImperial, windowStart, windowEnd }: TideChartProps) {
  const [containerRef, measuredWidth] = useMeasuredWidth()
  const viewportWidth = measuredWidth > 0 ? measuredWidth : DEFAULT_VIEWPORT_WIDTH
  const CHART_RIGHT = viewportWidth - CHART_RIGHT_MARGIN

  const unit = isImperial ? 'ft' : 'm'
  const toDisplay = (meters: number) => (isImperial ? metersToFeet(meters) : meters)

  const chartStartMs = windowStart.getTime()
  const chartEndMs = windowEnd.getTime()

  const sortedExtremes = useMemo(
    () => [...chart.extremes].sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime()),
    [chart.extremes],
  )

  const hasExtremesInWindow = sortedExtremes.some((extreme) => {
    const t = new Date(extreme.time).getTime()
    return t >= chartStartMs && t < chartEndMs
  })

  const nowMs = Date.now()
  const showNowMarker = nowMs >= chartStartMs && nowMs < chartEndMs
  const tidePhaseReferenceMs = showNowMarker ? nowMs : (chartStartMs + chartEndMs) / 2
  // Prefer the backend's tidal_phase field (richer - can express a
  // day-offset) when present; fall back to the local heuristic only when
  // it's absent, so this stays fully backward compatible.
  const tidePhase =
    chart.tidalPhase !== undefined
      ? derivePhaseFromTidalPhase(chart.tidalPhase)
      : classifyTidePhase(chart.extremes, new Date(tidePhaseReferenceMs))

  const visibleExtremes = sortedExtremes.filter((extreme) => {
    const t = new Date(extreme.time).getTime()
    return t >= chartStartMs && t <= chartEndMs
  })

const displayHeights = sortedExtremes.map((extreme) => toDisplay(extreme.heightM))
  const currentDisplayHeight = toDisplay(chart.currentHeightM)
  const minHeight = Math.min(...displayHeights, currentDisplayHeight)
  const maxHeight = Math.max(...displayHeights, currentDisplayHeight)
  const padding = Math.max((maxHeight - minHeight) * 0.1, 0.1)
  const yMin = minHeight - padding
  const yMax = maxHeight + padding
  const yRange = yMax - yMin || 1

  const xFor = (timeMs: number) =>
    CHART_LEFT + ((timeMs - chartStartMs) / (chartEndMs - chartStartMs)) * (CHART_RIGHT - CHART_LEFT)
  const yFor = (value: number) => CHART_TOP + (1 - (value - yMin) / yRange) * (CHART_BOTTOM - CHART_TOP)

  // curvePoints stores real epoch-ms time and real display-unit height (not
  // pixel values) - recharts computes the data-to-pixel scaling itself now
  // (see tideChartMargin/XAxis/YAxis below), rather than being handed an
  // identity pixel-space mapping. Memoized on the primitive values it
  // actually reads (sortedExtremes, isImperial, chartStartMs/chartEndMs)
  // rather than by calling the xFor/yFor/toDisplay closures below - those
  // closures are recreated every render, so depending on them would defeat
  // the memoization (it would "change" every render even when nothing it
  // actually reads changed). The formulas are inlined here instead, matching
  // xFor/yFor/toDisplay exactly.
  const curvePoints = useMemo(() => {
    const points: { t: number; h: number }[] = []
    for (let i = 0; i < sortedExtremes.length - 1; i++) {
      const a = sortedExtremes[i]
      const b = sortedExtremes[i + 1]
      const tA = new Date(a.time).getTime()
      const tB = new Date(b.time).getTime()
      if (tB < chartStartMs || tA > chartEndMs) continue

      const hA = isImperial ? metersToFeet(a.heightM) : a.heightM
      const hB = isImperial ? metersToFeet(b.heightM) : b.heightM

      for (let s = 0; s <= CURVE_STEPS; s++) {
        const progress = s / CURVE_STEPS
        const t = tA + (tB - tA) * progress
        const h = (hA + hB) / 2 + ((hA - hB) / 2) * Math.cos(Math.PI * progress)
        points.push({ t, h })
      }
    }
    return points
  }, [sortedExtremes, isImperial, chartStartMs, chartEndMs])

  // A real margin, unlike the old identity-pixel-space trick: this gives
  // recharts' own data-to-pixel scaling a plot rectangle spanning
  // (CHART_LEFT, CHART_TOP)-(viewportWidth - CHART_RIGHT_MARGIN, CHART_BOTTOM),
  // matching xFor/yFor's own pixel bounds exactly. No RECHARTS_XAXIS_HEIGHT-
  // style compensation is needed here (unlike the 4 hourly charts in
  // forecast-drawer.tsx) because Tide's day-ticks stay a manual overlay (the
  // XAxis below stays `hide`), and a hidden axis reserves no extra height in
  // recharts' offset calculation.
  const tideChartMargin = { left: CHART_LEFT, right: CHART_RIGHT_MARGIN, top: CHART_TOP, bottom: 175 - CHART_BOTTOM }

  const zeroInRange = yMin < 0 && yMax > 0
  // Only nowX (not nowY) is still needed - the manual overlay's "Now" <text>
  // below is positioned at a fixed CHART_TOP + 10, not by height; the
  // recharts-rendered ReferenceDot's y is now the real currentDisplayHeight
  // value directly (see the ReferenceDot in ComposedChart above), not this
  // closure.
  const nowX = xFor(nowMs)

  const tideTooltip = useChartTooltip(
    curvePoints.length,
    chart.station.stationId,
    CHART_LEFT,
    CHART_RIGHT,
  )

  const tideTooltipEntry =
    tideTooltip.activeIndex === null ? null : curvePoints[tideTooltip.activeIndex] ?? null
  const tideTooltipTime = tideTooltipEntry ? new Date(tideTooltipEntry.t) : null
  const tideTooltipHeight = tideTooltipEntry ? tideTooltipEntry.h : 0

  // Hour ticks at 0/6/12/18/24h offsets from the window start, matching the
  // "12AM/6AM/12PM/6PM" convention used by the sibling Wind/Wave/Precip/Cloud
  // charts in forecast-drawer.tsx now that this chart sits directly among them.
  // Memoized on the primitives xFor actually reads (chartStartMs, chartEndMs,
  // CHART_RIGHT) rather than by calling xFor itself, for the same reason as
  // curvePoints above — xFor is a new closure every render.
  const dayTicks = useMemo(
    () =>
      [0, 6, 12, 18, 24].map((hourOffset) => {
        const t = chartStartMs + hourOffset * 60 * 60 * 1000
        const x = CHART_LEFT + ((t - chartStartMs) / (chartEndMs - chartStartMs)) * (CHART_RIGHT - CHART_LEFT)
        return {
          x,
          label: new Date(t).toLocaleTimeString('en-US', { hour: 'numeric', hour12: true }).replace(' ', ''),
        }
      }),
    [chartStartMs, chartEndMs, CHART_RIGHT],
  )

  if (!hasExtremesInWindow) {
    return <ChartUnavailableMessage testId="forecast-tide-unavailable" message="Tide forecast unavailable for this day" />
  }

  return (
    <div className="relative" ref={containerRef}>
      {tidePhase && (
        <TidePhaseBadge phase={tidePhase} className="absolute right-2 top-2 z-10" />
      )}
      {tideTooltipEntry && tideTooltipTime && (
        <ChartTooltipBubble
          pixelX={tideTooltip.tooltipPixelX ?? 0}
          time={tideTooltipTime.toLocaleDateString('en-US', {
            month: 'short',
            day: 'numeric',
          })}
          primary={`${tideTooltipHeight.toFixed(1)} ${unit}`}
          secondary={tideTooltipTime.toLocaleTimeString('en-US', {
            hour: 'numeric',
            minute: '2-digit',
          })}
        />
      )}
      <div className="relative h-[175px] w-full touch-none overflow-hidden rounded bg-muted/15">
        <ComposedChart width={viewportWidth} height={175} margin={tideChartMargin} data={curvePoints}>
          {/* allowDataOverflow is required here: curvePoints intentionally
              includes samples with t (and, for boundary-straddling segments,
              h) slightly outside [chartStartMs, chartEndMs]/[yMin, yMax] - the
              segment-skip guard above (`if (tB < chartStartMs || tA >
              chartEndMs) continue`) only skips a whole segment when BOTH its
              endpoints are outside the window on the same side; it does not
              stop a segment straddling the boundary (one extreme just outside
              the window, one just inside) from producing individual
              interpolated samples that land outside the domain, for a smooth
              day-boundary curve - exactly like the original hand-rolled <svg>
              relied on its own viewBox clipping to hide. Without
              allowDataOverflow, recharts silently *widens* the domain to fit
              that overflow (see parseSpecifiedDomain in recharts'
              ChartUtils.js) while the pixel range stays fixed at
              [CHART_LEFT, CHART_RIGHT] - compressing the scale for every
              point, not just the overflowing ones. Confirmed empirically: the
              'keeps an in-window ReferenceDot pixel-accurate...' test below
              fails without this (expected cx ~232.7, actual ~272/710/508 -
              off by 30-500px) and passes once this is added. */}
          <XAxis dataKey="t" type="number" domain={[chartStartMs, chartEndMs]} allowDataOverflow hide />
          <YAxis dataKey="h" type="number" domain={[yMin, yMax]} hide />
          <CartesianGrid horizontal vertical={false} stroke="hsl(var(--chart-grid) / 0.12)" />
          <Area
            dataKey="h"
            type="linear"
            isAnimationActive={false}
            dot={false}
            stroke="none"
            fill="hsl(var(--chart-wave) / 0.12)"
          />
          <Line
            dataKey="h"
            type="linear"
            isAnimationActive={false}
            dot={false}
            stroke="hsl(var(--chart-wave) / 0.9)"
            strokeWidth={2.4}
          />

          {zeroInRange && (
            <ReferenceLine
              segment={[{ x: chartStartMs, y: 0 }, { x: chartEndMs, y: 0 }]}
              stroke="hsl(var(--chart-grid) / 0.2)"
              strokeWidth={1}
              strokeDasharray="3 3"
            />
          )}

          {visibleExtremes.map((extreme) => {
            const t = new Date(extreme.time).getTime()
            return (
              <ReferenceDot
                key={extreme.time}
                x={t}
                y={toDisplay(extreme.heightM)}
                r={3}
                fill={extreme.high ? 'hsl(var(--chart-wave) / 0.95)' : 'hsl(var(--chart-gust) / 0.95)'}
                stroke="none"
                isFront
              />
            )
          })}

          {showNowMarker && (
            <>
              <ReferenceLine
                segment={[{ x: nowMs, y: yMin }, { x: nowMs, y: yMax }]}
                stroke="hsl(var(--gauge-primary) / 0.7)"
                strokeWidth={1.5}
                strokeDasharray="4 3"
              />
              <ReferenceDot x={nowMs} y={currentDisplayHeight} r={3.5} fill="hsl(var(--gauge-primary))" stroke="none" isFront />
            </>
          )}
        </ComposedChart>

        <svg
          viewBox={`0 0 ${viewportWidth} 175`}
          preserveAspectRatio="none"
          className="pointer-events-auto absolute inset-0 h-full w-full touch-none"
          ref={tideTooltip.svgRef}
          onPointerDown={tideTooltip.onPointerDown}
          onPointerMove={tideTooltip.onPointerMove}
          onPointerLeave={tideTooltip.onPointerLeave}
        >
          <text x={6} y={CHART_TOP + 4} fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{yMax.toFixed(1)} {unit}</text>
          <text x={6} y={CHART_BOTTOM} fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{yMin.toFixed(1)} {unit}</text>

          <line x1={CHART_LEFT} y1={CHART_BOTTOM} x2={CHART_RIGHT} y2={CHART_BOTTOM} stroke="hsl(var(--chart-grid) / 0.25)" strokeWidth="1" />

          {dayTicks.map((tick) => (
            <g key={tick.label + tick.x}>
              <line x1={tick.x} y1={CHART_TOP} x2={tick.x} y2={CHART_BOTTOM} stroke="hsl(var(--chart-grid) / 0.12)" strokeWidth="1" />
              <text x={tick.x} y={CHART_BOTTOM + 28} textAnchor="middle" fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{tick.label}</text>
            </g>
          ))}

          {visibleExtremes.map((extreme) => {
            const t = new Date(extreme.time).getTime()
            const x = xFor(t)
            const y = yFor(toDisplay(extreme.heightM))
            const label = new Date(extreme.time).toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' })
            const aboveLine = y > CHART_TOP + 16
            return (
              <g key={extreme.time}>
                <text x={x} y={aboveLine ? y - 8 : y + 14} textAnchor="middle" fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>
                  {toDisplay(extreme.heightM).toFixed(1)}{unit}
                </text>
                <text x={x} y={CHART_BOTTOM + 15} textAnchor="middle" fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{label}</text>
              </g>
            )
          })}

          {showNowMarker && (
            <text x={Math.min(nowX + 4, CHART_RIGHT - 24)} y={CHART_TOP + 10} fontSize={AXIS_LABEL_FONT_SIZE} fill="hsl(var(--gauge-primary))">Now</text>
          )}

          {tideTooltipEntry && (
            <ChartTooltipMarker
              x={xFor(tideTooltipEntry.t)}
              y={yFor(tideTooltipEntry.h)}
              color="hsl(var(--chart-wave) / 0.9)"
            />
          )}
        </svg>
      </div>

      <p data-testid="forecast-tide-legend" className="mt-1 text-xs text-muted-foreground">
        <span className="inline-flex items-center gap-1 align-middle"><LegendSwatch color="hsl(var(--chart-wave) / 0.9)" strokeWidth={2.4} /> Tide height ({unit})</span> · <span className="text-amber-600">●</span> low · <span className="text-gauge-secondary">●</span> high · <span className="inline-flex items-center gap-1 align-middle"><LegendSwatch kind="vertical" color="hsl(var(--gauge-primary) / 0.7)" strokeWidth={1.5} dasharray="4 3" /> now</span>
      </p>

    </div>
  )
}
