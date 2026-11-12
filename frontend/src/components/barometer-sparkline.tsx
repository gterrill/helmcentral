import { useId } from 'react'
import type { BarometerTrendPoint } from '@/hooks/use-barometer-trend'
import { convertFromSI } from '@/lib/quantities'

/** The barometer reads in millibars on both unit systems. */
export const BAROMETER_UNIT = 'mb'

/**
 * The least span the chart stretches to. Without it a barometer wobbling by
 * a tenth of a millibar would fill the whole chart and look like it was
 * swinging; at 2 mb a steady glass draws as a nearly flat line.
 */
const MIN_SPAN_MB = 2

/** The chart's fixed axis: the three hours up to the newest reading. */
const WINDOW_MS = 3 * 60 * 60 * 1000

/** Readings are five minutes apart; a longer silence breaks the line. */
const GAP_MS = 15 * 60 * 1000

interface BarometerSparklineProps {
  points: BarometerTrendPoint[]
  className?: string
}

const W = 160
const H = 32
const PAD = 2

/**
 * A bare line and fill for the barometer's last three hours, modelled on
 * DepthSparkline. Higher pressure is higher on the chart. It has no axes: the
 * tile prints the reading and the change beside it.
 */
export function BarometerSparkline({ points, className }: BarometerSparklineProps) {
  const gradientId = useId()
  if (points.length < 2) return null

  const values = points.map((p) => convertFromSI(p.pressurePa, 'pressure', BAROMETER_UNIT))
  const lo = Math.min(...values)
  const hi = Math.max(...values)
  const mid = (lo + hi) / 2
  const span = Math.max(hi - lo, MIN_SPAN_MB)
  const floor = mid - span / 2

  // Placed by time, so a silence shows as a silence: the line breaks across a
  // gap instead of drawing a slope the barometer never made. The axis ends at
  // the newest reading, not the device clock, which may differ from the boat's.
  const times = points.map((p) => Date.parse(p.time))
  const end = Math.max(...times)
  const toX = (ms: number) => Math.min(W, Math.max(0, ((ms - (end - WINDOW_MS)) / WINDOW_MS) * W))
  const toY = (v: number) => H - PAD - ((v - floor) / span) * (H - 2 * PAD)

  const segments: Array<Array<{ x: number; y: number }>> = []
  times.forEach((t, i) => {
    const xy = { x: toX(t), y: toY(values[i]) }
    const gap = i > 0 && t - times[i - 1] > GAP_MS
    if (i === 0 || gap) segments.push([xy])
    else segments[segments.length - 1].push(xy)
  })

  return (
    <svg
      viewBox={`0 0 ${W} ${H}`}
      preserveAspectRatio="none"
      className={className ?? 'h-8 w-full'}
      aria-hidden="true"
    >
      <defs>
        <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="hsl(var(--gauge-secondary))" stopOpacity="0.25" />
          <stop offset="100%" stopColor="hsl(var(--gauge-secondary))" stopOpacity="0.03" />
        </linearGradient>
      </defs>
      {segments.map((seg, i) => {
        // A lone reading draws as a dot (a zero-length line with round caps).
        const pts = (seg.length === 1 ? [seg[0], seg[0]] : seg).map((q) => `${q.x.toFixed(1)},${q.y.toFixed(1)}`)
        return (
          <g key={i}>
            {seg.length > 1 && (
              <path d={`M${seg[0].x.toFixed(1)},${H} L${pts.join(' L')} L${seg[seg.length - 1].x.toFixed(1)},${H} Z`} fill={`url(#${gradientId})`} />
            )}
            <polyline
              points={pts.join(' ')}
              fill="none"
              stroke="hsl(var(--gauge-secondary))"
              strokeWidth="1.5"
              strokeLinejoin="round"
              strokeLinecap="round"
              vectorEffect="non-scaling-stroke"
            />
          </g>
        )
      })}
    </svg>
  )
}
