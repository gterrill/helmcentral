import { useId, type CSSProperties, type ReactNode } from 'react'

import { markingAt, polar, scaleTicks, sweepMapper, zoneColor } from '@/lib/dial-geometry'
import type { GaugeZone } from '@/lib/dashboard-widgets'

/**
 * The engine cluster's tachometer: a lit instrument face with a sweep band up to
 * the reading, a short glowing needle, a centre hub that carries the readout,
 * and a scale whose redline comes from the slot's own zones.
 *
 * Built on dial-geometry.ts, the same helpers DialRing uses, rather than as a
 * variant of it: the face, band, needle and hub are a different instrument, and
 * a variant prop would be the mode flag growing.
 *
 * Colour is state only (ADR 0080). Ticks and numbers are the skin's neutral
 * scale colour except inside a zone you have set, and with no threshold there is
 * no red at all. The face, rim, glow and hub are skin tokens, inert in :root.
 */

/** The face's own size in design px; the radii below are fractions of its 190 radius. */
export const CLUSTER_DIAL_SIZE = 380
const R = CLUSTER_DIAL_SIZE / 2
const K = 190 / 240

const R_BAND = 148 * K
const W_BAND = 62 * K
const R_TICK_OUT = 200 * K
const R_TICK_MAJOR = 176 * K
const R_TICK_MINOR = 189 * K
const R_LABEL = 156 * K
const HUB = 190
const SWEEP = 250

export interface ClusterDialProps {
  /** Already converted to display units, or null with no reading. */
  value: number | null
  min: number
  max: number
  majorStep: number
  minorPerMajor?: number
  /** Divides the scale numbers (100 shows 2600 as 26). */
  labelDivisor?: number
  zones?: GaugeZone[]
  /** The hub's content: the readout. */
  children?: ReactNode
}

export function ClusterDial({
  value, min, max, majorStep, minorPerMajor = 5, labelDivisor, zones, children,
}: ClusterDialProps) {
  const gradientId = useId()
  const { angleFor, fractionFor } = sweepMapper(min, max, SWEEP)
  const { majors, minors } = scaleTicks(min, max, majorStep, minorPerMajor)
  const at = (angle: number, r: number) => polar(R, R, angle, r)

  const tick = (v: number, inner: number, major: boolean) => {
    const [x1, y1] = at(angleFor(v), inner)
    const [x2, y2] = at(angleFor(v), R_TICK_OUT)
    const mark = markingAt(v, zones)
    return (
      <line key={`${major ? 'M' : 'm'}${v}`} data-tick={major ? 'major' : 'minor'} data-tick-zone={mark ?? undefined}
        x1={x1} y1={y1} x2={x2} y2={y2} strokeLinecap="butt"
        style={{
          stroke: mark ? zoneColor(mark) : 'hsl(var(--dial-tick-major))',
          strokeWidth: major ? 5.5 : 2.4,
        }} />
    )
  }

  // The band is a polyline rather than an arc command so one path serves any sweep.
  const bandPath = (() => {
    if (value === null || fractionFor(value) <= 0) return ''
    const end = angleFor(min + (max - min) * fractionFor(value))
    const from = angleFor(min)
    let d = ''
    for (let i = 0; i <= 80; i += 1) {
      const [x, y] = at(from + ((end - from) * i) / 80, R_BAND)
      d += `${i ? 'L' : 'M'}${x.toFixed(1)} ${y.toFixed(1)}`
    }
    return d
  })()

  const needle = (() => {
    if (value === null) return null
    const angle = angleFor(min + (max - min) * fractionFor(value))
    const [x1, y1] = at(angle, R_BAND - W_BAND / 2)
    const [x2, y2] = at(angle, R_BAND + W_BAND / 2 + 14 * K)
    return { x1, y1, x2, y2 }
  })()

  return (
    <div data-cluster-dial-face="" className="relative" style={{ width: CLUSTER_DIAL_SIZE, height: CLUSTER_DIAL_SIZE }}>
      <div className="absolute inset-0 rounded-full"
        style={{ background: 'var(--cluster-dial-face)', boxShadow: 'var(--cluster-dial-rim)' }} />
      <svg width={CLUSTER_DIAL_SIZE} height={CLUSTER_DIAL_SIZE} viewBox={`0 0 ${CLUSTER_DIAL_SIZE} ${CLUSTER_DIAL_SIZE}`}
        className="absolute left-0 top-0" aria-hidden="true">
        <defs>
          {/* Climbs the face from its foot, as the reference's does. */}
          <linearGradient id={gradientId} gradientUnits="userSpaceOnUse" x1={0} y1={R * 2 * 0.9} x2={0} y2={0}>
            <stop offset="0%" style={{ stopColor: 'hsl(var(--primary))', stopOpacity: 'var(--cluster-sweep-o0)' }} />
            <stop offset="50%" style={{ stopColor: 'hsl(var(--primary))', stopOpacity: 'var(--cluster-sweep-o1)' }} />
            <stop offset="100%" style={{ stopColor: 'hsl(var(--dial-needle))', stopOpacity: 'var(--cluster-sweep-o2)' }} />
          </linearGradient>
        </defs>
        {bandPath && <path data-value-arc="" d={bandPath} fill="none" strokeWidth={W_BAND} stroke={`url(#${gradientId})`} />}
        {needle && (
          <line data-needle="" x1={needle.x1} y1={needle.y1} x2={needle.x2} y2={needle.y2} strokeWidth={4} strokeLinecap="round"
            style={{ stroke: 'hsl(var(--dial-needle))', filter: 'drop-shadow(var(--cluster-needle-glow))' }} />
        )}
        {minors.map((v) => tick(v, R_TICK_MINOR, false))}
        {majors.map((v) => tick(v, R_TICK_MAJOR, true))}
        {majors.map((v) => {
          const [x, y] = at(angleFor(v), R_LABEL)
          const mark = markingAt(v, zones)
          return (
            <text key={`L${v}`} x={x} y={y} textAnchor="middle" dominantBaseline="central"
              style={{ fill: mark ? zoneColor(mark) : 'hsl(var(--dial-label))', fontSize: 24, fontWeight: 600 } as CSSProperties}>
              {Number((labelDivisor ? v / labelDivisor : v).toFixed(2))}
            </text>
          )
        })}
      </svg>
      <div data-hub="" className="absolute flex flex-col items-center justify-center gap-1.5 rounded-full"
        style={{
          left: R - HUB / 2, top: R - HUB / 2, width: HUB, height: HUB,
          background: 'var(--cluster-hub)', boxShadow: 'var(--cluster-hub-shadow)',
        }}>
        {children}
      </div>
    </div>
  )
}
