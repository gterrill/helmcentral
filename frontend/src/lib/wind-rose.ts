import type { WindRoseCard } from '@/lib/mate-cards'
import { niceTicks } from '@/lib/telemetry-chart'

const POINTS = ['N', 'NNE', 'NE', 'ENE', 'E', 'ESE', 'SE', 'SSE', 'S', 'SSW', 'SW', 'WSW', 'W', 'WNW', 'NW', 'NNW'] as const

/** The nearest of the sixteen compass points to a bearing. */
export function compassPoint(deg: number): string {
  return POINTS[Math.round((((deg % 360) + 360) % 360) / 22.5) % 16]
}

export interface RoseBand {
  /** Share of every sample in this band of this sector, in percent. */
  pct: number
  /** Where the band starts and ends along the sector's petal, as a running percent. */
  from: number
  to: number
}

export interface RoseSector {
  index: number
  /** Bearing of the sector's centre, degrees clockwise from north. */
  centre: number
  compass: string
  pct: number
  bands: RoseBand[]
}

export interface RoseModel {
  sectors: RoseSector[]
  /** The longest petal, in percent. 0 when nothing was counted into a sector. */
  maxPct: number
  calmPct: number
  variablePct: number
  /** The sector most of the wind came from; null when none did. */
  prevailing: { compass: string; pct: number } | null
  bandLabels: string[]
}

const share = (n: number, total: number) => (total > 0 ? (100 * n) / total : 0)

const fmt = (v: number) => String(Number(v.toFixed(1)))

/**
 * Percentages for drawing. Every share is of all samples, calm and variable
 * included, so a petal's length is the fraction of the time the wind blew from
 * there at that strength.
 */
export function windRoseModel(card: WindRoseCard): RoseModel {
  const width = 360 / card.sectors
  const sectors = card.counts.map((row, index): RoseSector => {
    let run = 0
    const bands = row.map((n): RoseBand => {
      const pct = share(n, card.samples)
      const band = { pct, from: run, to: run + pct }
      run += pct
      return band
    })
    const centre = index * width
    return { index, centre, compass: compassPoint(centre), pct: run, bands }
  })
  const maxPct = sectors.reduce((m, s) => Math.max(m, s.pct), 0)
  const top = sectors.reduce<RoseSector | null>((best, s) => (s.pct > (best?.pct ?? 0) ? s : best), null)
  return {
    sectors,
    maxPct,
    calmPct: share(card.calm, card.samples),
    variablePct: share(card.variable, card.samples),
    prevailing: top ? { compass: top.compass, pct: top.pct } : null,
    bandLabels: card.bins.map((b) => (b.to === null ? `${fmt(b.from)}+ ${card.speedUnit}` : `${fmt(b.from)}–${fmt(b.to)} ${card.speedUnit}`)),
  }
}

/** Rings at round percentages, the last at or above the longest petal. */
export function ringTicks(maxPct: number): number[] {
  if (maxPct <= 0) return []
  return niceTicks(0, maxPct, 3).filter((t) => t > 0)
}

/** A point at `r` from (cx, cy) on a bearing measured clockwise from north. */
export function polar(cx: number, cy: number, r: number, bearing: number): { x: number; y: number } {
  const a = (bearing * Math.PI) / 180
  return { x: cx + r * Math.sin(a), y: cy - r * Math.cos(a) }
}

const n2 = (v: number) => v.toFixed(2)

/** A ring segment between radii r0 and r1 and bearings a0 to a1. From the centre (r0 of 0) it is a pie slice. */
export function wedgePath(cx: number, cy: number, r0: number, r1: number, a0: number, a1: number): string {
  const p = (r: number, a: number) => {
    const { x, y } = polar(cx, cy, r, a)
    return `${n2(x)} ${n2(y)}`
  }
  const large = a1 - a0 > 180 ? 1 : 0
  if (r0 <= 0) return `M${cx} ${cy} L${p(r1, a0)} A${n2(r1)} ${n2(r1)} 0 ${large} 1 ${p(r1, a1)} Z`
  return `M${p(r0, a0)} L${p(r1, a0)} A${n2(r1)} ${n2(r1)} 0 ${large} 1 ${p(r1, a1)} L${p(r0, a1)} A${n2(r0)} ${n2(r0)} 0 ${large} 0 ${p(r0, a0)} Z`
}
