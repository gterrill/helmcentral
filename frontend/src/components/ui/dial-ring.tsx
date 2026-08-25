import { useId, type ReactNode } from 'react'

import type { GaugeZone } from '@/lib/dashboard-widgets'

/**
 * A ticked instrument dial (ADR 0054).
 *
 * Shared by the radial gauge's `instrument` ring style and the engine cluster,
 * so the tick and scale maths exist once. Follows wind-compass.tsx's idiom —
 * polar helper, major/minor tick radii, theme-token strokes, fontSize="10"
 * labels — including its 280-unit viewBox, because the radial gauge's 100x74
 * box is far too coarse to place ticks in.
 */

const V = 280
const C = V / 2

const R_OUTER = 130 // rim
const R_MAJOR = 112 // major tick inner end
const R_MINOR = 121 // minor tick inner end
const R_LABEL = 96 // scale numbers
const R_ARC = 130 // value arc centreline

// A blade riding the rim rather than a full needle pivoting at the centre: a
// centre-pivoted needle crosses the readout it is meant to accompany.
const R_NEEDLE_TIP = 122
const R_NEEDLE_BASE = 5 // half-width at the widest point
const R_NEEDLE_TAIL = 88

const D2R = Math.PI / 180

function pt(angleDeg: number, r: number): [number, number] {
  const a = angleDeg * D2R
  return [C + r * Math.cos(a), C + r * Math.sin(a)]
}

/** Alarm severities (ADR 0038), the same vocabulary the gauges already use. */
function zoneStroke(state: GaugeZone['state']): string {
  switch (state) {
    case 'emergency':
      return 'hsl(0 72% 42%)'
    case 'alarm':
      return 'hsl(0 72% 51%)'
    case 'warn':
      return 'hsl(38 92% 50%)'
    case 'alert':
      return 'hsl(43 96% 56%)'
    default:
      return 'hsl(142 71% 45%)'
  }
}

/**
 * The arc says how much, the pointer says where — the redundancy every
 * mechanical instrument uses, and the reason a dial reads faster than a bare
 * number at a glance.
 */
function Needle({ angle }: { angle: number }) {
  const mid = (R_NEEDLE_TIP + R_NEEDLE_TAIL) / 2
  const offset = (Math.atan2(R_NEEDLE_BASE, mid) * 180) / Math.PI

  const [tipX, tipY] = pt(angle, R_NEEDLE_TIP)
  const [leftX, leftY] = pt(angle + offset, mid)
  const [rightX, rightY] = pt(angle - offset, mid)
  const [tailX, tailY] = pt(angle, R_NEEDLE_TAIL)

  return (
    <g>
      <polygon
        data-needle=""
        points={[[tipX, tipY], [leftX, leftY], [tailX, tailY], [rightX, rightY]]
          .map(([x, y]) => `${x.toFixed(2)},${y.toFixed(2)}`)
          .join(' ')}
        fill="hsl(var(--gauge-primary))"
      />
    </g>
  )
}

/**
 * Where the sweep ends, as a fraction of the dial box's height.
 *
 * Exported because a layout placing things against the dial has to derive that
 * line from the same geometry the arc is drawn with. Measured to the outer
 * edge of the zone rim, which sits further out than the value arc.
 */
export function arcEndFraction(sweep: number): number {
  const start = 90 + (360 - sweep) / 2
  const outermost = R_OUTER + 5 + 3.5 // zone rim centreline plus half its width
  return (C + outermost * Math.sin(start * D2R)) / V
}

export interface DialRingProps {
  /** Already converted to display units, or null when the path is absent. */
  value: number | null
  min: number
  max: number
  /** A major tick and a scale number every this many display units. */
  majorStep: number
  /** Ticks per major interval, minors being the interior ones. Default 5. */
  minorPerMajor?: number
  /** Divides the scale numbers, e.g. 100 shows 2600 as "26" under an "x100" caption. */
  labelDivisor?: number
  /**
   * Label every Nth major tick. Every major by default; 2 halves the numbers
   * while keeping the ticks, so the scale orients without competing with the
   * reading it surrounds.
   */
  labelEvery?: number
  zones?: GaugeZone[]
  /** Degrees of arc. The marine convention is a wide sweep with a gap at the bottom. */
  sweep?: number
  className?: string
  /** Rendered centred inside the ring. */
  children?: ReactNode
}

export function DialRing({
  value, min, max, majorStep, minorPerMajor = 5, labelDivisor, labelEvery = 1,
  zones, sweep = 250, className, children,
}: DialRingProps) {
  const gradientId = useId()

  const span = max > min ? max - min : 1
  const start = 90 + (360 - sweep) / 2
  const angleFor = (v: number) => start + sweep * ((v - min) / span)
  const fractionFor = (v: number) => Math.max(0, Math.min(1, (v - min) / span))

  const arc = (fromV: number, toV: number, r: number) => {
    const [x1, y1] = pt(angleFor(fromV), r)
    const [x2, y2] = pt(angleFor(toV), r)
    const large = sweep * ((toV - fromV) / span) > 180 ? 1 : 0
    return `M ${x1.toFixed(2)} ${y1.toFixed(2)} A ${r} ${r} 0 ${large} 1 ${x2.toFixed(2)} ${y2.toFixed(2)}`
  }

  const majors: number[] = []
  for (let v = min; v <= max + span * 1e-9; v += majorStep) majors.push(v)

  const minors: number[] = []
  if (minorPerMajor > 1) {
    for (let i = 0; i < majors.length - 1; i += 1) {
      for (let k = 1; k < minorPerMajor; k += 1) {
        minors.push(majors[i] + (majorStep * k) / minorPerMajor)
      }
    }
  }

  return (
    <div className={`relative ${className ?? ''}`}>
      <svg viewBox={`0 0 ${V} ${V}`} className="w-full" aria-hidden="true">
        <defs>
          <linearGradient id={gradientId} x1="0" y1="0" x2="1" y2="0">
            <stop offset="0%" stopColor="hsl(var(--primary))" stopOpacity="0.55" />
            <stop offset="100%" stopColor="hsl(var(--primary))" />
          </linearGradient>
        </defs>

        <path d={arc(min, max, R_ARC)} fill="none" stroke="hsl(var(--muted))" strokeWidth="3" strokeLinecap="round" />

        {minors.map((v) => {
          const [x1, y1] = pt(angleFor(v), R_OUTER - 1)
          const [x2, y2] = pt(angleFor(v), R_MINOR)
          return <line key={`m${v}`} data-tick="minor" x1={x1} y1={y1} x2={x2} y2={y2}
            stroke="hsl(var(--border))" strokeWidth="1.5" />
        })}

        {majors.map((v) => {
          const [x1, y1] = pt(angleFor(v), R_OUTER - 1)
          const [x2, y2] = pt(angleFor(v), R_MAJOR)
          return <line key={`M${v}`} data-tick="major" x1={x1} y1={y1} x2={x2} y2={y2}
            stroke="hsl(var(--muted-foreground))" strokeWidth="3" />
        })}

        {/* Rim segments rather than a wash across the arc: a red band has to be
            readable at a glance, which is most of what the reference gets right. */}
        {(zones ?? []).map((zone, index) => {
          const from = Math.max(min, Math.min(zone.from, zone.to))
          const to = Math.min(max, Math.max(zone.from, zone.to))
          if (to <= from) return null
          return <path key={index} data-zone={zone.state} d={arc(from, to, R_OUTER + 5)}
            fill="none" stroke={zoneStroke(zone.state)} strokeWidth="7" />
        })}

        {majors.map((v, index) => {
          if (index % labelEvery !== 0) return null
          const [x, y] = pt(angleFor(v), R_LABEL)
          const shown = labelDivisor ? v / labelDivisor : v
          return (
            <text key={`L${v}`} x={x} y={y} textAnchor="middle" dominantBaseline="central"
              fontSize="14" fill="hsl(var(--muted-foreground))">
              {Number(shown.toFixed(2))}
            </text>
          )
        })}

        {/* No arc and no needle when there is no reading: a zero-length arc at
            the minimum reads as a real measurement of the minimum. */}
        {value !== null && fractionFor(value) > 0 && (
          <path
            data-value-arc=""
            d={arc(min, min + span * fractionFor(value), R_ARC)}
            fill="none"
            stroke={`url(#${gradientId})`}
            strokeWidth="8"
            strokeLinecap="round"
          />
        )}

        {value !== null && <Needle angle={angleFor(min + span * fractionFor(value))} />}
      </svg>

      {children && (
        <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
          {children}
        </div>
      )}

    </div>
  )
}
