import { useId } from 'react'
import type { DepthTrendPoint, DepthTrendSince, TideType } from '@/hooks/use-depth-trend'

interface DepthSparklineProps {
  points: DepthTrendPoint[]
  isImperial: boolean
  since?: DepthTrendSince
  tideType?: TideType
  tideDepthM?: number
  className?: string
}

const METERS_TO_FEET = 3.28084

export function DepthSparkline({ points, isImperial, since, tideType, tideDepthM, className }: DepthSparklineProps) {
  const gradientId = useId()

  if (points.length < 2) return null

  const values = points.map(p => isImperial ? p.depth_m * METERS_TO_FEET : p.depth_m)
  const minVal = Math.min(...values)
  const maxVal = Math.max(...values)
  const range = maxVal - minVal

  const W = 320
  const H = 78
  const marginLeft = 30
  const marginRight = 8
  const marginTop = 18
  const marginBottom = 6

  const chartLeft = marginLeft
  const chartRight = W - marginRight
  const chartTop = marginTop
  const chartBottom = H - marginBottom
  const chartWidth = chartRight - chartLeft
  const chartHeight = chartBottom - chartTop

  // deeper = lower on chart (mirrors looking down through the water column,
  // shallow near the surface/top, deep toward the bottom)
  const toX = (i: number) => chartLeft + (i / (values.length - 1)) * chartWidth
  const toY = (v: number) =>
    range < 0.01
      ? chartTop + chartHeight / 2
      : chartTop + ((v - minVal) / range) * chartHeight

  const linePts = values.map((v, i) => `${toX(i).toFixed(1)},${toY(v).toFixed(1)}`).join(' ')
  const areaPath =
    `M${toX(0).toFixed(1)},${chartTop.toFixed(1)} ` +
    values.map((v, i) => `L${toX(i).toFixed(1)},${toY(v).toFixed(1)}`).join(' ') +
    ` L${toX(values.length - 1).toFixed(1)},${chartTop.toFixed(1)} Z`

  const unit = isImperial ? 'ft' : 'm'
  const fmt = (v: number) => v.toFixed(1)
  const fmtTime = (t: string) =>
    new Date(t).toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    })

  const yTicks = [maxVal, (minVal + maxVal) / 2, minVal]
  const xTickIndexes = [0, Math.floor((points.length - 1) / 2), points.length - 1]

  const first = values[0]
  const last = values[values.length - 1]
  const tideValue = tideDepthM !== undefined ? (isImperial ? tideDepthM * METERS_TO_FEET : tideDepthM) : undefined
  const delta = since === 'tide' && tideValue !== undefined ? last - tideValue : last - first
  const trendUp = delta > 0.1
  const trendDown = delta < -0.1

  const trendLabel = since === 'tide'
    ? (tideType === 'high' ? 'Since High Tide' : 'Since Low Tide')
    : '3h Trend'

  return (
    <div className={className}>
      <div className="mb-1 flex items-center justify-between">
        <span className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">{trendLabel}</span>
        {trendUp && (
          <span className="text-[10px] font-medium tabular-nums text-secondary">
            ▲ +{fmt(Math.abs(delta))}{unit}
          </span>
        )}
        {trendDown && (
          <span className="text-[10px] font-medium tabular-nums text-amber-500">
            ▼ −{fmt(Math.abs(delta))}{unit}
          </span>
        )}
        {!trendUp && !trendDown && (
          <span className="text-[10px] font-medium tabular-nums text-muted-foreground">
            ≈ {fmt(last)}{unit}
          </span>
        )}
      </div>
      <svg
        viewBox={`0 0 ${W} ${H}`}
        preserveAspectRatio="none"
        className="w-full"
        style={{ height: 78 }}
        aria-hidden="true"
      >
        <defs>
          <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="hsl(var(--secondary))" stopOpacity="0.25" />
            <stop offset="100%" stopColor="hsl(var(--secondary))" stopOpacity="0.03" />
          </linearGradient>
        </defs>

        {yTicks.map((tick, idx) => {
          const y = toY(tick)
          return (
            <g key={`y-${idx}`}>
              <line
                x1={chartLeft}
                y1={y}
                x2={chartRight}
                y2={y}
                stroke="hsl(var(--border))"
                strokeWidth="1"
                strokeOpacity="0.35"
              />
              <text
                x={chartLeft - 4}
                y={y + 3}
                fontSize="9"
                textAnchor="end"
                fill="hsl(var(--muted-foreground))"
              >
                {fmt(tick)}
              </text>
            </g>
          )
        })}

        <line
          x1={chartLeft}
          y1={chartTop}
          x2={chartLeft}
          y2={chartBottom}
          stroke="hsl(var(--border))"
          strokeWidth="1"
        />
        <line
          x1={chartLeft}
          y1={chartTop}
          x2={chartRight}
          y2={chartTop}
          stroke="hsl(var(--border))"
          strokeWidth="1"
        />

        {xTickIndexes.map((idx, tickIdx) => {
          const x = toX(idx)
          return (
            <g key={`x-${tickIdx}`}>
              <line
                x1={x}
                y1={chartTop}
                x2={x}
                y2={chartTop - 3}
                stroke="hsl(var(--border))"
                strokeWidth="1"
              />
              <text
                x={x}
                y={chartTop - 8}
                fontSize="9"
                textAnchor={tickIdx === 0 ? 'start' : tickIdx === 2 ? 'end' : 'middle'}
                fill="hsl(var(--muted-foreground))"
              >
                {fmtTime(points[idx].time)}
              </text>
            </g>
          )
        })}

        <path d={areaPath} fill={`url(#${gradientId})`} />
        <polyline
          points={linePts}
          fill="none"
          stroke="hsl(var(--secondary))"
          strokeWidth="1.5"
          strokeLinejoin="round"
          strokeLinecap="round"
        />
      </svg>
    </div>
  )
}
