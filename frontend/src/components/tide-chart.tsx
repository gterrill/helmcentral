import { useMemo } from 'react'

import { Area, ComposedChart, Line, ReferenceDot, ReferenceLine, XAxis, YAxis } from 'recharts'

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
  const tidePhase = classifyTidePhase(chart.extremes, new Date(tidePhaseReferenceMs))

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

  // curvePoints is memoized on the primitive values it actually reads
  // (sortedExtremes, isImperial, chartStartMs/chartEndMs, CHART_RIGHT, yMin,
  // yRange) rather than by calling the xFor/yFor/toDisplay closures below —
  // those closures are recreated every render, so depending on them would
  // defeat the memoization (it would "change" every render even when nothing
  // it actually reads changed). The formulas are inlined here instead,
  // matching xFor/yFor/toDisplay exactly. yMin/yRange (numbers) are safe to
  // use directly as deps: since they're recomputed fresh each render from
  // stable inputs, their *value* only changes when something real (extremes,
  // unit, current height) changes, even though the memo can't see those
  // inputs directly.
  const curvePoints = useMemo(() => {
    const points: { x: number; y: number }[] = []
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
        const x = CHART_LEFT + ((t - chartStartMs) / (chartEndMs - chartStartMs)) * (CHART_RIGHT - CHART_LEFT)
        const y = CHART_TOP + (1 - (h - yMin) / yRange) * (CHART_BOTTOM - CHART_TOP)
        points.push({ x, y })
      }
    }
    return points
  }, [sortedExtremes, isImperial, chartStartMs, chartEndMs, CHART_RIGHT, yMin, yRange])

  // curvePoints/extreme markers are already in literal SVG pixel space (not
  // a real data domain), so recharts is handed an identity mapping instead
  // of doing its own data-to-pixel scaling: zero margin on a chart sized to
  // exactly viewportWidth x 175, X domain [0, viewportWidth] (recharts' default
  // orientation already maps pixel-left-to-right, needing no flip), and Y
  // domain [0, 175] with `reversed` (SVG y grows downward - same as our
  // pixel space - but recharts' un-reversed Y axis treats domain-max as "up",
  // the opposite of what a plain top-down pixel value means). Given that,
  // a data value of (x, y) renders at the literal SVG pixel (x, y).
  const tideChartMargin = { left: 0, right: 0, top: 0, bottom: 0 }

  const zeroInRange = yMin < 0 && yMax > 0
  const nowX = xFor(nowMs)
  const nowY = yFor(currentDisplayHeight)

  const tideTooltip = useChartTooltip(
    curvePoints.length,
    chart.station.stationId,
    CHART_LEFT,
    CHART_RIGHT,
  )

  const tideTooltipEntry =
    tideTooltip.activeIndex === null ? null : curvePoints[tideTooltip.activeIndex] ?? null
  let tideTooltipTime: Date | null = null
  let tideTooltipHeight = 0

  if (tideTooltipEntry) {
    // Reverse xFor to get timeMs from pixel x
    const timeFraction =
      (tideTooltipEntry.x - CHART_LEFT) / (CHART_RIGHT - CHART_LEFT)
    tideTooltipTime = new Date(
      chartStartMs + timeFraction * (chartEndMs - chartStartMs),
    )
    tideTooltipHeight =
      yMin + (1 - (tideTooltipEntry.y - CHART_TOP) / (CHART_BOTTOM - CHART_TOP)) * yRange
  }

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
              includes samples slightly past [0, viewportWidth]/[0, 175] (a
              curve segment's raised-cosine interpolation can extend past the
              window edges for a smooth day-boundary, exactly as the original
              hand-rolled <svg> did, relying on the SVG's own viewBox clipping
              to hide the overflow). Without allowDataOverflow, recharts
              silently *expands* the domain to fit that overflow (see
              parseSpecifiedDomain in recharts' ChartUtils.js) while the pixel
              range stays fixed at [0, viewportWidth]/[0, 175] - compressing
              the whole identity mapping this chart depends on. */}
          <XAxis dataKey="x" type="number" domain={[0, viewportWidth]} allowDataOverflow hide />
          <YAxis dataKey="y" type="number" domain={[0, 175]} reversed allowDataOverflow hide />
          <Area
            dataKey="y"
            type="linear"
            isAnimationActive={false}
            dot={false}
            stroke="none"
            fill="rgba(20,184,166,0.12)"
          />
          <Line
            dataKey="y"
            type="linear"
            isAnimationActive={false}
            dot={false}
            stroke="rgba(20,184,166,0.9)"
            strokeWidth={2.4}
          />

          {zeroInRange && (
            <ReferenceLine
              segment={[{ x: CHART_LEFT, y: yFor(0) }, { x: CHART_RIGHT, y: yFor(0) }]}
              stroke="rgba(80,98,118,0.2)"
              strokeWidth={1}
              strokeDasharray="3 3"
            />
          )}

          {visibleExtremes.map((extreme) => {
            const t = new Date(extreme.time).getTime()
            const x = xFor(t)
            const y = yFor(toDisplay(extreme.heightM))
            return (
              <ReferenceDot
                key={extreme.time}
                x={x}
                y={y}
                r={3}
                fill={extreme.high ? 'rgba(20,184,166,0.95)' : 'rgba(245,158,11,0.95)'}
                stroke="none"
                isFront
              />
            )
          })}

          {showNowMarker && (
            <>
              <ReferenceLine
                segment={[{ x: nowX, y: CHART_TOP }, { x: nowX, y: CHART_BOTTOM }]}
                stroke="rgba(199,137,0,0.7)"
                strokeWidth={1.5}
                strokeDasharray="4 3"
              />
              <ReferenceDot x={nowX} y={nowY} r={3.5} fill="hsl(var(--gauge-primary))" stroke="none" isFront />
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

          <line x1={CHART_LEFT} y1={CHART_BOTTOM} x2={CHART_RIGHT} y2={CHART_BOTTOM} stroke="rgba(80,98,118,0.25)" strokeWidth="1" />

          {dayTicks.map((tick) => (
            <g key={tick.label + tick.x}>
              <line x1={tick.x} y1={CHART_TOP} x2={tick.x} y2={CHART_BOTTOM} stroke="rgba(80,98,118,0.12)" strokeWidth="1" />
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
              x={tideTooltipEntry.x}
              y={tideTooltipEntry.y}
              color="rgba(20,184,166,0.9)"
            />
          )}
        </svg>
      </div>

      <p className="mt-1 text-xs text-muted-foreground">
        <span className="text-gauge-secondary">— Tide height ({unit})</span> · <span className="text-amber-600">●</span> low · <span className="text-gauge-secondary">●</span> high · <span style={{ color: 'rgba(199,137,0,0.95)' }}>┊</span> now
      </p>

    </div>
  )
}
