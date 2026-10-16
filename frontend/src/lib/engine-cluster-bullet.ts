import type { GaugeWidgetConfig, GaugeZone } from '@/lib/dashboard-widgets'
import { reading } from '@/lib/cluster-readings'

/**
 * A bullet graph for one cluster slot, derived entirely from that slot's
 * zones: nothing about where a band starts or what colour it is lives in the
 * component. Zones are also the alarm source, so the graph and the alarm cannot
 * disagree about where the line is.
 *
 * Colour is state only. The fill and marker stay neutral through the normal
 * band and turn amber or red only while the reading is in that zone; the bands
 * behind them dim when idle and brighten when the reading is inside them.
 *
 * A bundled profile ships no warn or alarm thresholds, so a slot usually has a
 * normal band and nothing else. Then only that band draws, no setpoint ticks,
 * and a reading outside it is amber (never red: the configuration cannot know
 * it is past a limit, only that it has left the usual range).
 */

export type BulletTone = 'neutral' | 'alert' | 'warn' | 'alarm'

export interface BulletBand {
  /** Percent along the scale. */
  left: number
  width: number
  state: GaugeZone['state']
  /** The reading sits inside this band. */
  active: boolean
}

export interface BulletMark {
  left: number
  state: GaugeZone['state']
  label: string
  /** Which side of the tick its number is written. */
  labelSide: 'before' | 'after'
}

export interface BulletModel {
  text: string | null
  unit: string
  converted: number | null
  zone: ReturnType<typeof reading>['zone']
  stale: boolean
  /** Percent along the scale, or null with no reading. */
  position: number | null
  tone: BulletTone
  bands: BulletBand[]
  marks: BulletMark[]
}

/** The tone a zone state paints the fill and the value in. */
export function toneFor(zone: BulletModel['zone']): BulletTone {
  switch (zone) {
    case 'alert':
      return 'alert'
    case 'warn':
    case 'outside':
      return 'warn'
    case 'alarm':
    case 'emergency':
      return 'alarm'
    default:
      return 'neutral'
  }
}

function labelOf(value: number): string {
  return String(Number(value.toFixed(1)))
}

export function bulletFor(
  slot: GaugeWidgetConfig,
  values: Record<string, number | null>,
  ages?: Record<string, number | null>,
): BulletModel {
  const r = reading(slot, values, ages)
  const min = slot.min ?? 0
  const max = slot.max ?? 100
  const span = max - min
  const pct = (v: number) => (span > 0 ? Math.max(0, Math.min(100, ((v - min) / span) * 100)) : 0)

  const zones = slot.zones ?? []
  const epsilon = span * 0.001
  const hit = r.converted === null
    ? -1
    : zones.findIndex((z) => r.converted! >= Math.min(z.from, z.to) && r.converted! <= Math.max(z.from, z.to))

  const bands: BulletBand[] = zones
    .map((z, index) => {
      const from = Math.max(min, Math.min(z.from, z.to))
      const to = Math.min(max, Math.max(z.from, z.to))
      return { from, to, state: z.state, active: index === hit }
    })
    .filter((b) => b.to > b.from)
    .map((b) => ({ left: pct(b.from), width: pct(b.to) - pct(b.from), state: b.state, active: b.active }))

  // A tick only where a threshold really is: the inner edge of a non-normal
  // band that runs to either end of the scale. A band floating mid-scale is
  // drawn but has no single line to mark.
  const marks: BulletMark[] = []
  for (const z of zones) {
    if (z.state === 'normal') continue
    const lo = Math.min(z.from, z.to)
    const hi = Math.max(z.from, z.to)
    const severe = z.state === 'alarm' || z.state === 'emergency'
    if (lo <= min + epsilon && hi < max - epsilon) {
      marks.push({ left: pct(hi), state: z.state, label: labelOf(hi), labelSide: severe ? 'before' : 'after' })
    } else if (hi >= max - epsilon && lo > min + epsilon) {
      marks.push({ left: pct(lo), state: z.state, label: labelOf(lo), labelSide: severe ? 'after' : 'before' })
    }
  }

  return {
    text: r.text,
    unit: r.unit,
    converted: r.converted,
    zone: r.zone,
    stale: r.stale,
    position: r.converted === null ? null : pct(r.converted),
    tone: toneFor(r.zone),
    bands,
    marks,
  }
}

/** Victron / Venus charge states, as the alternator regulator reports them. */
const CHARGE_STAGES: Record<number, string> = {
  0: 'OFF',
  1: 'LOW POWER',
  2: 'FAULT',
  3: 'BULK',
  4: 'ABSORPTION',
  5: 'FLOAT',
  6: 'STORAGE',
  7: 'EQUALIZE',
  8: 'PASSTHRU',
  9: 'INVERTING',
  10: 'ASSISTING',
  11: 'SUPPLY',
  252: 'EXTERNAL',
}

/**
 * The word for a charge stage number. A number the table does not know shows as
 * itself rather than as a guess at a stage.
 */
export function chargeStageLabel(value: number | null): string | null {
  if (value === null) return null
  const n = Math.round(value)
  return CHARGE_STAGES[n] ?? `STATE ${n}`
}
