import type { SavedPlace } from '@/hooks/use-saved-places'
import type { DistanceUnits } from '@/config/app-config'
import { haversineMeters } from '@/lib/geo'
import { depthToText, depthUnitLabel } from '@/lib/saved-place-depth'
import { formatDayKey } from '@/lib/vessel-time'

/** 0.1 nm: how near a saved place must be to the anchor to take its depth (backend anchorage_details uses the same). */
export const SAVE_DEPTH_RADIUS_M = 185.2

/**
 * The saved place the depth goes to. The anchorage reader (backend
 * anchorage_depth.go) takes the nearest place within 0.1 nm that has a depth,
 * so a save goes there; else the nearest place within range; else null, and
 * the depth is offered as a new saved place.
 */
export function saveTargetPlace(places: SavedPlace[], lat: number, lon: number): SavedPlace | null {
  let nearest: SavedPlace | null = null
  let nearestM = Infinity
  let nearestWithDepth: SavedPlace | null = null
  let nearestWithDepthM = Infinity
  for (const p of places) {
    const d = haversineMeters(lat, lon, p.lat, p.lon)
    if (d > SAVE_DEPTH_RADIUS_M) continue
    if (d < nearestM) {
      nearest = p
      nearestM = d
    }
    if (p.depth_m !== null && d < nearestWithDepthM) {
      nearestWithDepth = p
      nearestWithDepthM = d
    }
  }
  return nearestWithDepth ?? nearest
}

/** The figure sent and shown: metres to a centimetre, the server's precision for a saved depth. */
export function saveFigureM(datumDepthM: number): number {
  return Math.round(datumDepthM * 100) / 100
}

/** Where the planning depth came from, for the panel. */
export function depthSourceText(source: 'sounder' | 'operator'): string {
  return source === 'sounder' ? 'from the sounder' : 'the depth you entered'
}

/** The calendar day the anchor went down, at the boat (ADR 0230). Null without a zone or a valid time. */
export function dropDay(setAt: string | null, timeZone: string | null | undefined): string | null {
  if (!setAt || !timeZone) return null
  const at = new Date(setAt)
  return Number.isNaN(at.getTime()) ? null : formatDayKey(at, timeZone)
}

/** "Save 4.2 m at datum". */
export function saveDepthLabel(datumDepthM: number, units: DistanceUnits): string {
  return `Save ${depthToText(datumDepthM, units)} ${depthUnitLabel(units)} at datum`
}
