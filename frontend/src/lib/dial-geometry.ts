import type { GaugeZone } from '@/lib/dashboard-widgets'
import { severityFill } from '@/lib/severity'

/**
 * Geometry and zone logic shared by every ticked dial (the gauge's `instrument`
 * ring and the engine cluster's tachometer). Extracted from dial-ring.tsx so a
 * second dial with a different face builds on the same rules instead of a copy.
 */

const D2R = Math.PI / 180

/** Alarm severities (ADR 0038). Deliberately literal rather than tokenized: a skin that could recolour these could hide an alarm. */
export function zoneColor(state: GaugeZone['state']): string {
  return severityFill(state)
}

/**
 * The band a position on the scale falls in, ignoring the normal one.
 *
 * A redline is a marking on the scale itself. Normal is deliberately not
 * marked, because a scale painted green everywhere it is fine leaves the
 * redline nothing to mark. With no non-normal zone there is no marking at all.
 */
export function markingAt(v: number, zones: GaugeZone[] | undefined): GaugeZone['state'] | null {
  const hit = (zones ?? []).find(
    (z) => z.state !== 'normal' && v >= Math.min(z.from, z.to) && v <= Math.max(z.from, z.to),
  )
  return hit ? hit.state : null
}

/**
 * The first zone a value is inside, or -1. One rule for "the band the reading is
 * in", so a bullet graph, an arc and a text colour never disagree where bands
 * overlap.
 */
export function zoneIndexAt(v: number | null, zones: readonly GaugeZone[] | undefined): number {
  if (v === null || !zones) return -1
  return zones.findIndex((z) => v >= Math.min(z.from, z.to) && v <= Math.max(z.from, z.to))
}

/** Point at `angleDeg` (clockwise from three o'clock, SVG's convention) and radius `r` round (cx, cy). */
export function polar(cx: number, cy: number, angleDeg: number, r: number): [number, number] {
  const a = angleDeg * D2R
  return [cx + r * Math.cos(a), cy + r * Math.sin(a)]
}

/** Where a sweep begins, in the same convention: the gap is centred at the bottom. */
export function sweepStart(sweep: number): number {
  return 90 + (360 - sweep) / 2
}

/** The scale mapped onto a sweep. */
export function sweepMapper(min: number, max: number, sweep: number) {
  const span = max > min ? max - min : 1
  const start = sweepStart(sweep)
  return {
    span,
    start,
    angleFor: (v: number) => start + sweep * ((v - min) / span),
    fractionFor: (v: number) => Math.max(0, Math.min(1, (v - min) / span)),
  }
}

/** The major tick values from min to max, and the interior minors between them (plus any stretch above the last major). */
export function scaleTicks(min: number, max: number, majorStep: number, minorPerMajor: number) {
  const span = max > min ? max - min : 1
  const majors: number[] = []
  for (let v = min; v <= max + span * 1e-9; v += majorStep) majors.push(v)

  const minors: number[] = []
  if (minorPerMajor > 1) {
    const minorStep = majorStep / minorPerMajor
    for (let i = 0; i < majors.length - 1; i += 1) {
      for (let k = 1; k < minorPerMajor; k += 1) minors.push(majors[i] + minorStep * k)
    }
    // The stretch above the last whole major step, so a range whose top is not
    // a round number of them keeps ticking through the part a redline marks.
    for (let v = majors[majors.length - 1] + minorStep; v <= max + span * 1e-9; v += minorStep) {
      minors.push(v)
    }
  }
  return { majors, minors }
}
