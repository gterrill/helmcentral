import type { DistanceUnits } from '@/config/app-config'
import { depthValueText, feetToMeters } from '@/lib/units'
import { formatDayKey } from '@/lib/vessel-time'

/**
 * The depth of a saved place (backend/saved_places.go): metres at chart datum,
 * kept with where it came from and the day it was taken. The operator types it
 * in the units they read; the server stores metres.
 */
export interface SavedPlaceDepth {
  depth_m: number | null
  depth_source: 'charted' | 'sounded' | ''
  depth_date: string
}

/** The same bounds the server holds: a drying height is negative. */
export const MIN_DEPTH_M = -10
export const MAX_DEPTH_M = 1000

export function depthUnitLabel(units: DistanceUnits): string {
  return units === 'imperial' ? 'ft' : 'm'
}

/** A depth as the box shows it, to a tenth: "4.0", or "13.1" in feet. */
export function depthToText(depthM: number | null, units: DistanceUnits): string {
  return depthM === null ? '' : depthValueText(depthM, units === 'imperial')
}

export type ParsedDepth = { ok: true; depthM: number | null } | { ok: false }

/** Reads the box: empty is no depth, otherwise a number in the operator's units within the server's bounds. */
export function parseDepthText(text: string, units: DistanceUnits): ParsedDepth {
  const t = text.trim().replace(',', '.')
  if (t === '') return { ok: true, depthM: null }
  if (!/^-?\d+(\.\d+)?$/.test(t)) return { ok: false }
  const typed = Number(t)
  const metres = units === 'imperial' ? feetToMeters(typed) : typed
  if (metres < MIN_DEPTH_M || metres > MAX_DEPTH_M) return { ok: false }
  return { ok: true, depthM: metres }
}

/**
 * Today's date at the boat, YYYY-MM-DD (ADR 0230: the boat's zone, not the
 * device's clock). Null until the boat has reported its zone, so a depth is
 * never dated by a guess.
 */
export function todayDate(timeZone: string | undefined, now: Date = new Date()): string | null {
  return timeZone ? formatDayKey(now, timeZone) : null
}

/** "4.0 m (charted 2026-11-01)", or "" when the place has no depth. */
export function depthSummary(d: SavedPlaceDepth, units: DistanceUnits): string {
  if (d.depth_m === null) return ''
  return `${depthToText(d.depth_m, units)} ${depthUnitLabel(units)} (${d.depth_source} ${d.depth_date})`
}
