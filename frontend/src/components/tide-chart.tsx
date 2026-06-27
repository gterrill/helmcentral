import { useEffect, useRef, useState } from 'react'
import type { PointerEvent as ReactPointerEvent } from 'react'

import type { TideChart as TideChartData } from '@/hooks/use-tide-chart'

const AXIS_LABEL_FONT_SIZE = '10'
const AXIS_LABEL_COLOR = 'rgba(71,85,105,0.95)'
const METERS_TO_FEET = 3.28084

const CHART_LEFT = 36
const CHART_RIGHT_MARGIN = 20
const CHART_TOP = 16
const CHART_BOTTOM = 125
const WINDOW_HOURS = 96
const CURVE_STEPS = 12
// Fallback viewBox width used only before the container's actual pixel width
// has been measured (ResizeObserver hasn't fired yet, or in tests where it's
// stubbed as a no-op) - chosen to match the chart's previous fixed size.
const DEFAULT_VIEWPORT_WIDTH = 1000

// Measures the actual rendered width of a container element, so the SVG's
// viewBox can be set 1:1 with real pixels instead of a fixed coordinate
// space. With a fixed-height SVG and the default preserveAspectRatio
// ("xMidYMid meet"), a fixed viewBox width narrower than the container
// would otherwise get letterboxed (centered, with empty space on each
// side) instead of actually filling the available width.
function useMeasuredWidth() {
  const ref = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    setWidth(el.getBoundingClientRect().width)
    const observer = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width))
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  return [ref, width] as const
}


// Shows a tooltip for the nearest hourly entry on mouse hover (desktop/
// tablet) or touch (mobile) - both go through the same pointer events, since
// `pointermove` only fires for a mouse on hover (no button needed) and only
// fires for touch while a finger is actually down and moving. Positioned at
// the pointer's own X so it never requires looking elsewhere.
//
// Clearing is pointer-type-aware: a mouse hides the tooltip as soon as it
// leaves the chart (normal hover behavior), but touch does NOT hide on
// pointerup/pointerleave - those fire the instant a finger lifts, which
// would otherwise make a quick tap flash the tooltip for a single frame
// instead of actually showing it. A touch tooltip stays until the next tap
// (anywhere) replaces or clears it.
function useChartTooltip(count: number, resetKey: string, chartLeft: number, chartRight: number) {
  const [point, setPoint] = useState<{ index: number; pixelX: number } | null>(null)
  const svgRef = useRef<SVGSVGElement>(null)

  // Switching days/stations swaps in a different hourly array (different length,
  // different times) - drop any tooltip from the previous chart.
  useEffect(() => {
    setPoint(null)
  }, [resetKey])

  const updateFromClientX = (clientX: number) => {
    const svg = svgRef.current
    if (!svg || count <= 0) return
    const rect = svg.getBoundingClientRect()
    if (rect.width <= 0) return
    const pixelX = Math.max(0, Math.min(rect.width, clientX - rect.left))
    const viewBoxWidth = svg.viewBox.baseVal.width || chartRight
    const xInViewBox = (pixelX / rect.width) * viewBoxWidth
    const fraction = count <= 1 ? 0 : (xInViewBox - chartLeft) / (chartRight - chartLeft)
    const idx = Math.max(0, Math.min(count - 1, Math.round(fraction * (count - 1))))
    setPoint({ index: idx, pixelX })
  }

  // Only a mouse leaving the chart should hide the tooltip - for touch,
  // "leave" fires the moment a finger lifts, which isn't a meaningful signal
  // that the user is done looking at the value.
  const clearForMouse = (event: ReactPointerEvent<SVGSVGElement>) => {
    if (event.pointerType === 'mouse') setPoint(null)
  }

  return {
    svgRef,
    activeIndex: point === null ? null : Math.min(point.index, Math.max(0, count - 1)),
    tooltipPixelX: point?.pixelX ?? null,
    onPointerDown: (event: ReactPointerEvent<SVGSVGElement>) => updateFromClientX(event.clientX),
    onPointerMove: (event: ReactPointerEvent<SVGSVGElement>) => updateFromClientX(event.clientX),
    onPointerLeave: clearForMouse,
  }
}

// A small marker dot on the chart's primary series at the hovered/touched index.
function TideChartTooltipMarker({ x, y, color }: { x: number; y: number; color: string }) {
  return <circle pointerEvents="none" cx={x} cy={y} r="5" fill={color} stroke="white" strokeWidth="2" />
}

// The floating time / prominent-value / secondary-value bubble, positioned
// at the pointer's X (clamped so it can't run off either edge of the chart)
// right above the chart - never behind a touch point, never elsewhere on
// the page.
function TideChartTooltipBubble({ pixelX, time, primary, secondary }: { pixelX: number; time: string; primary: string; secondary: string }) {
  return (
    <div
      className="pointer-events-none absolute top-1 z-10 -translate-x-1/2 whitespace-nowrap rounded-md border border-border/60 bg-card px-2.5 py-1.5 shadow-md"
      style={{ left: `clamp(58px, ${pixelX}px, calc(100% - 58px))` }}
    >
      <p className="text-[9px] font-medium uppercase tracking-wide text-muted-foreground">{time}</p>
      <p className="font-display text-base leading-tight text-foreground">{primary}</p>
      <p className="text-[10px] text-muted-foreground">{secondary}</p>
    </div>
  )
}

interface TideChartProps {
  chart: TideChartData
  isImperial: boolean
}

export function TideChart({ chart, isImperial }: TideChartProps) {
  const [containerRef, measuredWidth] = useMeasuredWidth()
  const viewportWidth = measuredWidth > 0 ? measuredWidth : DEFAULT_VIEWPORT_WIDTH
  const CHART_RIGHT = viewportWidth - CHART_RIGHT_MARGIN

  const unit = isImperial ? 'ft' : 'm'
  const toDisplay = (meters: number) => (isImperial ? meters * METERS_TO_FEET : meters)

  if (chart.extremes.length === 0) {
    return (
      <p className="py-6 text-center text-xs text-muted-foreground">
        No tide data available for this station
      </p>
    )
  }

  const now = Date.now()
  const chartStartMs = now
  const chartEndMs = now + WINDOW_HOURS * 60 * 60 * 1000

  const sortedExtremes = [...chart.extremes].sort(
    (a, b) => new Date(a.time).getTime() - new Date(b.time).getTime(),
  )

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

  const curvePoints: { x: number; y: number }[] = []
  for (let i = 0; i < sortedExtremes.length - 1; i++) {
    const a = sortedExtremes[i]
    const b = sortedExtremes[i + 1]
    const tA = new Date(a.time).getTime()
    const tB = new Date(b.time).getTime()
    if (tB < chartStartMs || tA > chartEndMs) continue

    const hA = toDisplay(a.heightM)
    const hB = toDisplay(b.heightM)

    for (let s = 0; s <= CURVE_STEPS; s++) {
      const progress = s / CURVE_STEPS
      const t = tA + (tB - tA) * progress
      const h = (hA + hB) / 2 + ((hA - hB) / 2) * Math.cos(Math.PI * progress)
      curvePoints.push({ x: xFor(t), y: yFor(h) })
    }
  }

  const linePoints = curvePoints.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(' ')
  const areaPath = curvePoints.length > 0
    ? `M ${curvePoints[0].x.toFixed(1)} ${CHART_BOTTOM} L ${linePoints} L ${curvePoints[curvePoints.length - 1].x.toFixed(1)} ${CHART_BOTTOM} Z`
    : ''

  const zeroInRange = yMin < 0 && yMax > 0
  const nowX = xFor(now)
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

  // Day-boundary gridlines so the 4-day window has some date context.
  const dayTicks: { x: number; label: string }[] = []
  const firstMidnight = new Date(chartStartMs)
  firstMidnight.setHours(24, 0, 0, 0)
  for (let t = firstMidnight.getTime(); t <= chartEndMs; t += 24 * 60 * 60 * 1000) {
    dayTicks.push({ x: xFor(t), label: new Date(t).toLocaleDateString('en-US', { weekday: 'short' }) })
  }

  return (
    <div className="relative" ref={containerRef}>
      {tideTooltipEntry && tideTooltipTime && (
        <TideChartTooltipBubble
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
      <svg
        viewBox={`0 0 ${viewportWidth} 175`}
        preserveAspectRatio="none"
        className="h-[175px] w-full rounded bg-muted/15 touch-none"
        ref={tideTooltip.svgRef}
        onPointerDown={tideTooltip.onPointerDown}
        onPointerMove={tideTooltip.onPointerMove}
        onPointerLeave={tideTooltip.onPointerLeave}
      >
        <text x={6} y={CHART_TOP + 4} fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{yMax.toFixed(1)} {unit}</text>
        <text x={6} y={CHART_BOTTOM} fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{yMin.toFixed(1)} {unit}</text>

        <line x1={CHART_LEFT} y1={CHART_BOTTOM} x2={CHART_RIGHT} y2={CHART_BOTTOM} stroke="rgba(80,98,118,0.25)" strokeWidth="1" />

        {zeroInRange && (
          <line x1={CHART_LEFT} y1={yFor(0)} x2={CHART_RIGHT} y2={yFor(0)} stroke="rgba(80,98,118,0.2)" strokeWidth="1" strokeDasharray="3 3" />
        )}

        {dayTicks.map((tick) => (
          <g key={tick.label + tick.x}>
            <line x1={tick.x} y1={CHART_TOP} x2={tick.x} y2={CHART_BOTTOM} stroke="rgba(80,98,118,0.12)" strokeWidth="1" />
            <text x={tick.x} y={CHART_BOTTOM + 28} textAnchor="middle" fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{tick.label}</text>
          </g>
        ))}

        <path d={areaPath} fill="rgba(20,184,166,0.12)" />
        <polyline points={linePoints} fill="none" stroke="rgba(20,184,166,0.9)" strokeWidth="2.4" strokeLinejoin="round" strokeLinecap="round" />

        {visibleExtremes.map((extreme) => {
          const t = new Date(extreme.time).getTime()
          const x = xFor(t)
          const y = yFor(toDisplay(extreme.heightM))
          const label = new Date(extreme.time).toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' })
          const aboveLine = y > CHART_TOP + 16
          return (
            <g key={extreme.time}>
              <circle cx={x} cy={y} r="3" fill={extreme.high ? 'rgba(20,184,166,0.95)' : 'rgba(245,158,11,0.95)'} />
              <text x={x} y={aboveLine ? y - 8 : y + 14} textAnchor="middle" fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>
                {toDisplay(extreme.heightM).toFixed(1)}{unit}
              </text>
              <text x={x} y={CHART_BOTTOM + 15} textAnchor="middle" fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{label}</text>
            </g>
          )
        })}

        <line x1={nowX} y1={CHART_TOP} x2={nowX} y2={CHART_BOTTOM} stroke="rgba(199,137,0,0.7)" strokeWidth="1.5" strokeDasharray="4 3" />
        <circle cx={nowX} cy={nowY} r="3.5" fill="rgba(199,137,0,0.95)" />
        <text x={Math.min(nowX + 4, CHART_RIGHT - 24)} y={CHART_TOP + 10} fontSize={AXIS_LABEL_FONT_SIZE} fill="rgba(199,137,0,0.95)">Now</text>

        {tideTooltipEntry && (
          <TideChartTooltipMarker
            x={tideTooltipEntry.x}
            y={tideTooltipEntry.y}
            color="rgba(20,184,166,0.9)"
          />
        )}
      </svg>

      <p className="mt-1 text-xs text-muted-foreground">
        <span className="text-secondary">— Tide height ({unit})</span> · <span className="text-amber-600">●</span> low · <span className="text-secondary">●</span> high · <span style={{ color: 'rgba(199,137,0,0.95)' }}>┊</span> now
      </p>

    </div>
  )
}
