import { ALARM_STATES, type AlarmState } from '@/hooks/use-alarms'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { DistanceUnits } from '@/config/app-config'
import { METRES_PER_NM } from '@/lib/geo'
import { distanceText } from '@/lib/marina-card'

// The wording, alarm tiers and ordering for AIS contacts, shared by the Nearby
// Vessels tile and the Nearby map's Vessels group. They differ in one place:
// the map's rows give range in the marine units of its place list, while the
// Nearby Vessels tile gives metres or feet.

/**
 * A seconds count on the shared s/m/h/d ladder, without a trailing "ago".
 * Both the server-computed age_seconds and formatLastSeen (a client-side delta
 * against last_seen_at, which can be days old) share this: one ladder means a
 * staleness-cutoff change can't reintroduce the "2648m ago" bug by falling
 * through a ladder that was never extended.
 */
export function formatRelativeDuration(seconds: number): string {
  const clamped = Math.max(0, seconds)
  if (clamped < 60) return `${clamped}s`

  const minutes = Math.floor(clamped / 60)
  if (minutes < 60) return `${minutes}m`

  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h`

  return `${Math.floor(hours / 24)}d`
}

export function formatRelativeAge(seconds: number): string {
  return `${formatRelativeDuration(seconds)} ago`
}

function formatLastSeen(lastSeenAt: string, nowMs = Date.now()): string {
  const parsedMs = Date.parse(lastSeenAt)
  if (!Number.isFinite(parsedMs)) return 'unknown'
  return formatRelativeAge(Math.max(0, Math.floor((nowMs - parsedMs) / 1000)))
}

/**
 * "Seen 7x before, last 2d ago", or null when the vessel has no sighting
 * history. Zero is not reported as a "first sighting": a vessel with no MMSI
 * always reads zero, and a returning vessel reads one low until its visit is
 * confirmed.
 */
export function formatSightingSummary(vessel: NearbyVessel, nowMs = Date.now()): string | null {
  if (typeof vessel.seen_count !== 'number' || vessel.seen_count <= 0) return null
  const last = typeof vessel.last_seen_at === 'string' && vessel.last_seen_at.trim() !== '' ? `, last ${formatLastSeen(vessel.last_seen_at, nowMs)}` : ''
  return `Seen ${vessel.seen_count}x before${last}`
}

/**
 * Two-tier collapse of the live collision-alarm ladder (ADR 0140). A marker
 * has one colour to spend, but a list row has room for a text label as well,
 * so `alert`/`warn` read as a still-forming risk (amber, "Collision warning")
 * and `alarm`/`emergency` as an imminent one (red, "Collision alarm"). `normal`
 * is not a state collisionAlarmStatesByVessel hands back, but the type doesn't
 * rule it out, so it is treated as no alarm.
 */
export type CollisionTier = 'warn' | 'alarm'

export function collisionTier(state: AlarmState | undefined): CollisionTier | null {
  if (state === undefined || state === 'normal') return null
  return ALARM_STATES.indexOf(state) >= ALARM_STATES.indexOf('alarm') ? 'alarm' : 'warn'
}

export const COLLISION_TIER_STYLE: Record<CollisionTier, { row: string; text: string; label: string }> = {
  alarm: {
    row: 'border-red-500 bg-red-50 dark:bg-red-950/60',
    text: 'text-red-600 dark:text-red-400',
    label: 'Collision alarm',
  },
  warn: {
    row: 'border-amber-500 bg-amber-50 dark:bg-amber-950/60',
    text: 'text-amber-600 dark:text-amber-400',
    label: 'Collision warning',
  },
}

/**
 * Alarmed vessels first, worst state first; unalarmed vessels, and vessels
 * tied on tier, keep the order the feed gave them (nearest first, set by the
 * backend). Builds a new array so the memoised prop is never mutated, and ties
 * break on the original index so the result doesn't depend on sort stability.
 */
export function sortByCollisionAlarm(vessels: NearbyVessel[], aisCollisionAlarms: ReadonlyMap<string, AlarmState> | undefined): NearbyVessel[] {
  if (!aisCollisionAlarms || aisCollisionAlarms.size === 0) return vessels

  return vessels
    .map((vessel, index) => ({ vessel, index, rank: ALARM_STATES.indexOf(aisCollisionAlarms.get(vessel.id) ?? 'normal') }))
    .sort((a, b) => (b.rank !== a.rank ? b.rank - a.rank : a.index - b.index))
    .map((entry) => entry.vessel)
}

// A search radius, in the units used for marine ranges elsewhere on the board:
// km and nm (as the Nearby map tile does), metres under a kilometre.
export function formatSearchRange(rangeMeters: number, distanceUnits: DistanceUnits): string {
  const trim = (n: number) => String(Number(n.toFixed(1)))
  if (distanceUnits === 'imperial') return `${trim(rangeMeters / METRES_PER_NM)} nm`
  if (rangeMeters < 1000) return `${Math.round(rangeMeters)} m`
  return `${trim(rangeMeters / 1000)} km`
}

const METRES_PER_FOOT = 0.3048
// Under this, nautical miles round to "0.0 nm" and say nothing.
const FEET_BELOW_M = METRES_PER_NM * 0.1

/**
 * A contact's range in the place list's marine units: whole feet under 0.1 nm
 * in imperial, otherwise metres under a kilometre, km above, or nm in imperial.
 */
export function formatVesselRange(rangeM: number, units: DistanceUnits): string {
  if (units === 'imperial' && rangeM < FEET_BELOW_M) return `${Math.round(rangeM / METRES_PER_FOOT)} ft`
  return distanceText(rangeM, units)
}

/**
 * The contacts a list shows: those inside `limitM`, nearest first, then
 * alarmed ones lifted to the top (worst state first). A contact in a collision
 * alarm or warning is listed at any range: the list must not read as empty
 * while one is sounding.
 */
export function selectListedVessels(vessels: NearbyVessel[], limitM: number, aisCollisionAlarms: ReadonlyMap<string, AlarmState> | undefined): NearbyVessel[] {
  const listed = vessels
    .filter((v) => v.range_m <= limitM || collisionTier(aisCollisionAlarms?.get(v.id)) !== null)
    .sort((a, b) => a.range_m - b.range_m)
  return sortByCollisionAlarm(listed, aisCollisionAlarms)
}
