import { useId, type CSSProperties, type ReactNode } from 'react'

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
// The value arc's radius and width live in --dial-band-r / --dial-band-w
// rather than here, so a skin can move the band without a second geometry.

// The pointer's width, as a fraction of the circle it is dashed onto. Its
// radius is CSS (see Needle), so this cannot be a length: 0.46 of a hundredth
// of the circumference lands at about 3px at the radii the skins use, which is
// the reference's own 5-of-480.
const NEEDLE_DASH = 0.46

const D2R = Math.PI / 180

function pt(angleDeg: number, r: number): [number, number] {
  const a = angleDeg * D2R
  return [C + r * Math.cos(a), C + r * Math.sin(a)]
}

/**
 * Alarm severities (ADR 0038), the same vocabulary the gauges already use.
 *
 * Exported so the cluster's zone bar and the fuel rail read the one palette
 * rather than each keeping a copy. Deliberately literal rather than tokenized:
 * a skin that could recolour these is a skin that could hide an alarm.
 */
export function zoneColor(state: GaugeZone['state']): string {
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
 *
 * A bar across the band, drawn as a dashed circle for the same reason the value
 * arc is: the radius and width are the skin's tokens, so a skin that moves the
 * band takes the pointer with it. The blade this replaces had its geometry
 * here, which is why the instrument skin pulling the band in to r96 left the
 * pointer out on the rim, riding its own bright end and lost against it.
 */
function Needle({ angle }: { angle: number }) {
  return (
    <circle
      data-needle=""
      cx={C} cy={C} pathLength={100} fill="none"
      strokeDasharray={`${NEEDLE_DASH} 100`}
      // Centred on the angle, not started at it: a bar hung off one side of
      // its own reading points half a bar-width high.
      strokeDashoffset={(NEEDLE_DASH / 2 - (angle / 360) * 100).toFixed(2)}
      style={{
        r: 'var(--dial-needle-r)',
        stroke: 'hsl(var(--dial-needle))',
        strokeWidth: 'var(--dial-needle-w)',
        // Butt, whatever the band is set to: a round cap on a 48px stroke
        // would add 24px of pointer at each end.
        strokeLinecap: 'butt',
        filter: 'drop-shadow(var(--dial-needle-glow))',
      }}
    />
  )
}

/**
 * The band a position on the scale falls in, ignoring the normal one.
 *
 * A redline is a marking on the scale itself, which is what the reference gets
 * right and a rim segment alone does not: it is legible from the angle a helm
 * dial is actually read at. Normal is deliberately not marked, because a scale
 * painted green everywhere it is fine leaves the redline nothing to mark.
 */
function markingAt(v: number, zones: GaugeZone[] | undefined): GaugeZone['state'] | null {
  const hit = (zones ?? []).find(
    (z) => z.state !== 'normal' && v >= Math.min(z.from, z.to) && v <= Math.max(z.from, z.to),
  )
  return hit ? hit.state : null
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

  // The swept fraction of a full circle, in pathLength-100 units.
  const sweepDash = (sweep / 360) * 100
  // Where the sweep begins, as a negative dash offset. A circle's path starts
  // at three o'clock, and the arc has to start at `start` instead. This is a
  // dash offset rather than a rotate() on the element because objectBoundingBox
  // gradient coordinates are in the element's own space: transforming the
  // circle drags its gradient round with it, which reversed the value arc's
  // ramp and changed how the flat dial looked as well as the skinned one.
  const sweepStart = -(start / 360) * 100

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
    const minorStep = majorStep / minorPerMajor
    for (let i = 0; i < majors.length - 1; i += 1) {
      for (let k = 1; k < minorPerMajor; k += 1) minors.push(majors[i] + minorStep * k)
    }
    // The stretch above the last whole major step. A range whose top is not a
    // round number of them, such as a 3300 rpm redline on a 500 rpm scale, used
    // to stop ticking at 3000 and leave bare exactly the part the red marks.
    for (let v = majors[majors.length - 1] + minorStep; v <= max + span * 1e-9; v += minorStep) {
      minors.push(v)
    }
  }

  return (
    <div className={`relative ${className ?? ''}`}>
      {/* Outside the SVG, not in it: the viewBox clips at 140 and the zone rim
          already reaches 138.5, so there is no room for a bezel inside. The
          overhang is the gap a caller's layout leaves around the rim. */}
      <div
        data-bezel=""
        className="pointer-events-none absolute rounded-full"
        style={{
          inset: 'calc(-1 * var(--dial-bezel-overhang))',
          background: 'var(--dial-bezel)',
          boxShadow: 'var(--dial-bezel-shadow)',
        }}
      />
      <svg viewBox={`0 0 ${V} ${V}`} className="relative w-full" aria-hidden="true">
        <defs>
          <linearGradient id={gradientId} x1="0" y1="0" x2="1" y2="0">
            <stop offset="0%" style={{ stopColor: 'hsl(var(--dial-band-c0))', stopOpacity: 'var(--dial-band-o0)' }} />
            <stop offset="50%" style={{ stopColor: 'hsl(var(--dial-band-c1))', stopOpacity: 'var(--dial-band-o1)' }} />
            <stop offset="100%" style={{ stopColor: 'hsl(var(--dial-band-c2))', stopOpacity: 'var(--dial-band-o2)' }} />
          </linearGradient>
        </defs>

        {/* Drawn as a dashed circle rather than an arc path so its radius and
            width are CSS, not baked into a `d`. SVG2 made `r` a property; line
            endpoints never became one, which is why the ticks stay in JS. With
            pathLength 100 the dash is a percentage of the circle whatever the
            radius, so a skin can move the band without recomputing anything. */}
        <circle
          cx={C} cy={C} pathLength={100} fill="none"
          strokeDasharray={`${sweepDash.toFixed(2)} 100`}
          strokeDashoffset={sweepStart.toFixed(2)}
          style={{
            r: 'var(--dial-track-r)',
            stroke: 'hsl(var(--dial-track))',
            strokeWidth: 'var(--dial-track-w)',
            strokeLinecap: 'var(--dial-band-cap)' as CSSProperties['strokeLinecap'],
          }}
        />

        {minors.map((v) => {
          const [x1, y1] = pt(angleFor(v), R_OUTER - 1)
          const [x2, y2] = pt(angleFor(v), R_MINOR)
          const mark = markingAt(v, zones)
          return <line key={`m${v}`} data-tick="minor" data-tick-zone={mark ?? undefined}
            x1={x1} y1={y1} x2={x2} y2={y2}
            style={{
              stroke: mark ? zoneColor(mark) : 'hsl(var(--dial-tick-minor))',
              strokeWidth: 'var(--dial-tick-minor-w)',
            }} />
        })}

        {majors.map((v) => {
          const [x1, y1] = pt(angleFor(v), R_OUTER - 1)
          const [x2, y2] = pt(angleFor(v), R_MAJOR)
          const mark = markingAt(v, zones)
          return <line key={`M${v}`} data-tick="major" data-tick-zone={mark ?? undefined}
            x1={x1} y1={y1} x2={x2} y2={y2}
            style={{
              stroke: mark ? zoneColor(mark) : 'hsl(var(--dial-tick-major))',
              strokeWidth: 'var(--dial-tick-major-w)',
            }} />
        })}

        {/* Rim segments rather than a wash across the arc: a red band has to be
            readable at a glance, which is most of what the reference gets right.
            Markings only, on the same rule as the ticks: a green arc down the
            whole normal band buried the value arc, the redline and the pointer
            under the one thing on the dial that carries no information. */}
        {(zones ?? []).map((zone, index) => {
          if (zone.state === 'normal') return null
          const from = Math.max(min, Math.min(zone.from, zone.to))
          const to = Math.min(max, Math.max(zone.from, zone.to))
          if (to <= from) return null
          return <path key={index} data-zone={zone.state} d={arc(from, to, R_OUTER + 5)}
            fill="none" stroke={zoneColor(zone.state)} strokeWidth="7" />
        })}

        {majors.map((v, index) => {
          if (index % labelEvery !== 0) return null
          const [x, y] = pt(angleFor(v), R_LABEL)
          const shown = labelDivisor ? v / labelDivisor : v
          const mark = markingAt(v, zones)
          return (
            <text key={`L${v}`} x={x} y={y} textAnchor="middle" dominantBaseline="central"
              style={{
                fill: mark ? zoneColor(mark) : 'hsl(var(--dial-label))',
                fontSize: 'var(--dial-label-size)',
                fontWeight: 'var(--dial-label-weight)' as CSSProperties['fontWeight'],
              }}>
              {Number(shown.toFixed(2))}
            </text>
          )
        })}

        {/* No arc and no needle when there is no reading: a zero-length arc at
            the minimum reads as a real measurement of the minimum. */}
        {value !== null && fractionFor(value) > 0 && (
          <circle
            data-value-arc=""
            cx={C} cy={C} pathLength={100} fill="none"
            strokeDasharray={`${(sweepDash * fractionFor(value)).toFixed(2)} 100`}
            strokeDashoffset={sweepStart.toFixed(2)}
            stroke={`url(#${gradientId})`}
            style={{
              r: 'var(--dial-band-r)',
              strokeWidth: 'var(--dial-band-w)',
              strokeLinecap: 'var(--dial-band-cap)' as CSSProperties['strokeLinecap'],
            }}
          />
        )}

        {value !== null && <Needle angle={angleFor(min + span * fractionFor(value))} />}

      </svg>

      {/* A DOM layer, not an SVG <circle>: `fill` takes a paint, so a CSS
          gradient in it is invalid and the shape falls back to solid black
          over the whole dial. Sits above the svg and below the readout. */}
      <div
        data-hub=""
        className="pointer-events-none absolute inset-0 rounded-full"
        style={{ background: 'var(--dial-hub)' }}
      />

      {children && (
        <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
          {children}
        </div>
      )}

    </div>
  )
}
