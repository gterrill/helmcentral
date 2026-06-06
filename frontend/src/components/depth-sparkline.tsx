import type { DepthTrendPoint } from '@/hooks/use-depth-trend'

interface DepthSparklineProps {
  points: DepthTrendPoint[]
  isImperial: boolean
  className?: string
}

const METERS_TO_FEET = 3.28084

export function DepthSparkline({ points, isImperial, className }: DepthSparklineProps) {
  if (points.length < 2) return null

  const values = points.map(p => isImperial ? p.depth_m * METERS_TO_FEET : p.depth_m)
  const minVal = Math.min(...values)
  const maxVal = Math.max(...values)
  const range = maxVal - minVal

  const W = 300
  const H = 48
  const padX = 2
  const padY = 4

  // deeper = higher on chart (standard y-axis convention)
  const toX = (i: number) => padX + (i / (values.length - 1)) * (W - padX * 2)
  const toY = (v: number) =>
    range < 0.01
      ? H / 2
      : padY + (1 - (v - minVal) / range) * (H - padY * 2)

  const linePts = values.map((v, i) => `${toX(i).toFixed(1)},${toY(v).toFixed(1)}`).join(' ')
  const areaPath =
    `M${toX(0).toFixed(1)},${H} ` +
    values.map((v, i) => `L${toX(i).toFixed(1)},${toY(v).toFixed(1)}`).join(' ') +
    ` L${toX(values.length - 1).toFixed(1)},${H} Z`

  const unit = isImperial ? 'ft' : 'm'
  const fmt = (v: number) => v.toFixed(1)

  const first = values[0]
  const last = values[values.length - 1]
  const delta = last - first
  const trendUp = delta > 0.1
  const trendDown = delta < -0.1

  return (
    <div className={className}>
      <div className="mb-1 flex items-center justify-between">
        <span className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">2h Trend</span>
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
        style={{ height: 48 }}
        aria-hidden="true"
      >
        <defs>
          <linearGradient id="depth-fill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="hsl(var(--secondary))" stopOpacity="0.25" />
            <stop offset="100%" stopColor="hsl(var(--secondary))" stopOpacity="0.03" />
          </linearGradient>
        </defs>
        <path d={areaPath} fill="url(#depth-fill)" />
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
