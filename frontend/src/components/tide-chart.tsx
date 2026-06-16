import type { TideChart as TideChartData } from '@/hooks/use-tide-chart'

const AXIS_LABEL_FONT_SIZE = '10'
const AXIS_LABEL_COLOR = 'rgba(71,85,105,0.95)'
const METERS_TO_FEET = 3.28084

const CHART_LEFT = 36
const CHART_RIGHT = 980
const CHART_TOP = 16
const CHART_BOTTOM = 125
const WINDOW_HOURS = 96
const CURVE_STEPS = 12

interface TideChartProps {
  chart: TideChartData
  isImperial: boolean
}

export function TideChart({ chart, isImperial }: TideChartProps) {
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

  // Day-boundary gridlines so the 4-day window has some date context.
  const dayTicks: { x: number; label: string }[] = []
  const firstMidnight = new Date(chartStartMs)
  firstMidnight.setHours(24, 0, 0, 0)
  for (let t = firstMidnight.getTime(); t <= chartEndMs; t += 24 * 60 * 60 * 1000) {
    dayTicks.push({ x: xFor(t), label: new Date(t).toLocaleDateString('en-US', { weekday: 'short' }) })
  }

  return (
    <div>
      <svg viewBox="0 0 1000 175" className="h-[175px] w-full rounded bg-muted/15">
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
      </svg>

      <p className="mt-1 text-[10px] text-muted-foreground">
        <span className="text-secondary">— Tide height ({unit})</span> · <span className="text-amber-600">●</span> low · <span className="text-secondary">●</span> high · <span style={{ color: 'rgba(199,137,0,0.95)' }}>┊</span> now
      </p>

    </div>
  )
}
