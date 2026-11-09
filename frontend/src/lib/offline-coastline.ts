// The Offline coastline settings page's view of the server's coverage status
// (GET /api/offline-coastline), and the few pure helpers that turn it into
// words and map shapes. Words follow GLOSSARY.md: coverage, coverage square,
// edits since. Never "tile" (that is a dashboard tile), "cell" or "region".

import { DAY_MS, zoneKnown, formatDayKey, formatDayMonthIn, formatTimeOfDay, NO_TIME } from '@/lib/vessel-time'

export type CoastlineSquareState = 'ready' | 'failed' | 'pending'

export interface CoastlineSquare {
  lat: number
  lon: number
  state: CoastlineSquareState
  in_range: boolean
  downloaded_at?: string
  edits_since: number
  checked_at?: string
  /** Why a download of this square failed (state failed). */
  error?: string
  /** Why the weekly edit check of a held square failed; the square is still held. */
  check_error?: string
  next_attempt_at?: string
  /** A held square from before data dates were kept: usable, and downloaded again in the background. */
  awaiting_refresh?: boolean
  /** Why the last download again of an awaiting square failed; the old copy is kept. */
  refresh_error?: string
  /** Asked for by the operator and not held yet. If it failed, error says why. */
  requested?: boolean
}

export type OfflineCoastlineState =
  | 'off'
  | 'error'
  | 'paused'
  | 'waiting_for_fix'
  | 'updating'
  | 'downloading'
  | 'retrying'
  | 'idle'

/** The south-west corners of the squares the operator may ask for, as the server sets them. */
export interface SelectableBounds {
  lat_min: number
  lat_max: number
  lon_min: number
  lon_max: number
}

export interface OfflineCoastlineStatus {
  enabled: boolean
  selectable: SelectableBounds
  state: OfflineCoastlineState
  error?: string
  /** While paused: what the map server did, and when the worker tries again. */
  pause_reason?: string
  pause_detail?: string
  retry_at?: string
  /** While only the weekly edit check is paused (downloads carry on). */
  check_pause_reason?: string
  check_pause_detail?: string
  check_retry_at?: string
  has_fix: boolean
  position?: { lat: number; lon: number }
  radius_nm: number
  squares: CoastlineSquare[]
  held: number
  wanted: number
  wanted_held: number
  /** Squares in range that are all land, so are not downloaded. */
  inland_skipped: number
  /** Wanted squares whose download failed and will be retried. */
  failed: number
  /** Held squares whose weekly edit check failed. */
  check_failed?: number
  edits_since: number
  squares_with_edits: number
  updating: number
  /** Held squares waiting to be downloaded again. They still serve routing and shelter. */
  awaiting_refresh?: number
  /** Awaiting squares whose last download again failed. */
  refresh_failed?: number
  /** Requested squares not yet downloaded and not failed. */
  requested_waiting?: number
  /** Requested squares whose last download failed. */
  requested_failed?: number
  oldest_downloaded_at?: string
}

/** A square named by its south-west corner, e.g. "21°S 148°E". */
export function squareLabel(lat: number, lon: number): string {
  const ns = lat < 0 ? 'S' : 'N'
  const ew = lon < 0 ? 'W' : 'E'
  return `${Math.abs(lat)}°${ns} ${Math.abs(lon)}°${ew}`
}

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`

function shortDate(iso: string, now: Date, timeZone: string | undefined): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime()) || !timeZone || !zoneKnown(timeZone)) return NO_TIME
  const sameYear = formatDayKey(d, timeZone).slice(0, 4) === formatDayKey(now, timeZone).slice(0, 4)
  return formatDayMonthIn(d, timeZone, { year: !sameYear })
}

/** The boat's HH:MM, or null when the time is unreadable. */
export function clockTime(iso: string, timeZone: string | undefined): string | null {
  const t = new Date(iso)
  if (Number.isNaN(t.getTime())) return null
  const shown = formatTimeOfDay(t, timeZone)
  return shown === NO_TIME ? null : shown
}

function coverageText(s: OfflineCoastlineStatus, now: Date, timeZone: string | undefined): string {
  if (s.held === 0) return 'Coverage: none yet'
  const parts = [`Coverage: ${plural(s.held, 'square', 'squares')}`]
  if (s.oldest_downloaded_at) parts.push(`oldest ${shortDate(s.oldest_downloaded_at, now, timeZone)}`)
  if (s.edits_since > 0) parts.push(`${plural(s.edits_since, 'edit', 'edits')} since`)
  if ((s.awaiting_refresh ?? 0) > 0) parts.push(`${s.awaiting_refresh} awaiting refresh`)
  return parts.join(' · ')
}

/** The muted note about all-land squares left out, or null when there is nothing to say (none skipped, or the switch is off). */
export function inlandSkippedLine(s: OfflineCoastlineStatus): string | null {
  const n = s.inland_skipped ?? 0
  if (n <= 0 || s.state === 'off') return null
  return `${plural(n, 'square', 'squares')} that ${n === 1 ? 'is' : 'are'} all land ${n === 1 ? 'is' : 'are'} skipped.`
}

/** Why the weekly edit check is on hold while downloads carry on, or null. */
export function editCheckPauseLine(s: OfflineCoastlineStatus, timeZone?: string, now: Date = new Date()): string | null {
  if (s.state === 'off' || !s.check_pause_reason) return null
  const due = s.check_retry_at ? new Date(s.check_retry_at) : null
  const at = s.check_retry_at ? clockTime(s.check_retry_at, timeZone) : null
  const retry = due && due.getTime() <= now.getTime() ? 'Trying again now' : at ? `Trying again about ${at}` : 'Trying again later'
  return `Edit check on hold: ${s.check_pause_reason}. ${retry}.`
}

/** The one line under the switch. */
export function coastlineStatusLine(s: OfflineCoastlineStatus, now: Date = new Date(), timeZone?: string): string {
  switch (s.state) {
    case 'off':
      return 'Off. Nothing is downloaded or checked.'
    case 'error':
      return s.error ?? 'Offline coastline cannot run.'
    case 'paused': {
      const at = s.retry_at ? clockTime(s.retry_at, timeZone) : null
      return `${s.pause_reason ?? 'The map server is refusing requests'}. ${at ? `Trying again about ${at}` : 'Trying again later'}`
    }
    case 'waiting_for_fix':
      return s.held > 0
        ? `Waiting for a position fix · ${coverageText(s, now, timeZone)}`
        : 'Waiting for a position fix'
    case 'updating':
      return `Updating ${plural(s.updating, 'square', 'squares')}`
    case 'downloading':
      return `Downloading ${s.wanted_held} of ${s.wanted}`
    case 'retrying':
      return `${coverageText(s, now, timeZone)} · ${s.failed} failed, will retry`
    case 'idle':
      return coverageText(s, now, timeZone)
  }
}

export interface CoastlineFailure {
  label: string
  error: string
  nextAttemptAt?: string
}

/** Squares whose download failed, for the page to name. */
export function coastlineFailures(s: OfflineCoastlineStatus): CoastlineFailure[] {
  return s.squares
    .filter((q) => q.state === 'failed' && q.error)
    .map((q) => ({ label: squareLabel(q.lat, q.lon), error: q.error as string, nextAttemptAt: q.next_attempt_at }))
}

/** Held squares whose weekly edit check failed. They stay held. */
export function coastlineCheckFailures(s: OfflineCoastlineStatus): CoastlineFailure[] {
  return s.squares
    .filter((q) => q.state === 'ready' && q.check_error)
    .map((q) => ({ label: squareLabel(q.lat, q.lon), error: q.check_error as string, nextAttemptAt: q.next_attempt_at }))
}

/** Held squares awaiting a download again whose last try failed. They stay held. */
export function coastlineRefreshFailures(s: OfflineCoastlineStatus): CoastlineFailure[] {
  return s.squares
    .filter((q) => q.state === 'ready' && q.refresh_error)
    .map((q) => ({ label: squareLabel(q.lat, q.lon), error: q.refresh_error as string, nextAttemptAt: q.next_attempt_at }))
}

/** The outline of the square whose south-west corner is lat, lon. */
export function squarePolygon(lat: number, lon: number) {
  return {
    type: 'Polygon' as const,
    coordinates: [[[lon, lat], [lon + 1, lat], [lon + 1, lat + 1], [lon, lat + 1], [lon, lat]]],
  }
}


export interface SquareFeatureProps {
  state: CoastlineSquareState
  failed: boolean
  in_range: boolean
  has_edits: boolean
  requested: boolean
  age_days: number
  label: string
}

export interface SquareFeature {
  type: 'Feature'
  properties: SquareFeatureProps
  geometry: ReturnType<typeof squarePolygon>
}

/** One polygon per square, with what the map needs to shade it. */
export function coverageSquaresGeoJSON(
  s: OfflineCoastlineStatus,
  now: Date = new Date(),
): { type: 'FeatureCollection'; features: SquareFeature[] } {
  return {
    type: 'FeatureCollection',
    features: s.squares.map((q) => ({
      type: 'Feature',
      properties: {
        state: q.state,
        failed: q.state === 'failed',
        in_range: q.in_range,
        has_edits: q.edits_since > 0,
        requested: q.requested === true && q.state !== 'ready',
        age_days: q.downloaded_at ? Math.max(0, Math.round((now.getTime() - new Date(q.downloaded_at).getTime()) / DAY_MS)) : 0,
        label: squareLabel(q.lat, q.lon),
      },
      geometry: squarePolygon(q.lat, q.lon),
    })),
  }
}

const EARTH_RADIUS_NM = 3440.065

/** The coverage circle around the boat as a closed polygon ring. */
export function coverageRingGeoJSON(lat: number, lon: number, radiusNm: number, steps = 90) {
  const d = radiusNm / EARTH_RADIUS_NM
  const lat1 = (lat * Math.PI) / 180
  const lon1 = (lon * Math.PI) / 180
  const ring: number[][] = []
  for (let i = 0; i < steps; i++) {
    const brg = (2 * Math.PI * i) / steps
    const lat2 = Math.asin(Math.sin(lat1) * Math.cos(d) + Math.cos(lat1) * Math.sin(d) * Math.cos(brg))
    const lon2 = lon1 + Math.atan2(Math.sin(brg) * Math.sin(d) * Math.cos(lat1), Math.cos(d) - Math.sin(lat1) * Math.sin(lat2))
    ring.push([(lon2 * 180) / Math.PI, (lat2 * 180) / Math.PI])
  }
  ring.push(ring[0])
  return {
    type: 'Feature' as const,
    properties: {},
    geometry: { type: 'Polygon' as const, coordinates: [ring] },
  }
}

// ── selecting squares to download or remove ───────────────────────────────

/** Squares can be selected only when the map shows about this many degrees or fewer. */
export const SELECT_MAX_SPAN_DEG = 30
export const squareKey = (lat: number, lon: number) => `${lat},${lon}`

export function parseSquareKey(key: string): { lat: number; lon: number } {
  const [lat, lon] = key.split(',').map(Number)
  return { lat, lon }
}

/**
 * Squares across the 180° meridian or at the poles cannot be selected, the
 * same limit as the ring around the boat. The server sets the range.
 */
export function squareSelectable(b: SelectableBounds, lat: number, lon: number): boolean {
  return Number.isInteger(lat) && Number.isInteger(lon) && lat >= b.lat_min && lat <= b.lat_max && lon >= b.lon_min && lon <= b.lon_max
}

/**
 * The selectable squares in the rectangle between two squares, either corner
 * first. The corners are the map's unwrapped longitudes, so a drag across the
 * 180° meridian keeps its order; each square is wrapped as it is listed.
 */
export function squaresInBox(a: { lat: number; lon: number }, b: { lat: number; lon: number }, bounds: SelectableBounds): string[] {
  const out: string[] = []
  for (let lat = Math.min(a.lat, b.lat); lat <= Math.max(a.lat, b.lat); lat++) {
    for (let unwrapped = Math.min(a.lon, b.lon); unwrapped <= Math.max(a.lon, b.lon); unwrapped++) {
      const lon = Math.floor(wrapLongitude(unwrapped))
      if (squareSelectable(bounds, lat, lon)) out.push(squareKey(lat, lon))
    }
  }
  return out
}

export interface ViewBounds {
  west: number
  south: number
  east: number
  north: number
}

/** Whether squares can be selected at this view: zoomed in far enough. */
export function selectionAllowedAt(b: ViewBounds): boolean {
  return Math.max(b.north - b.south, b.east - b.west) <= SELECT_MAX_SPAN_DEG
}

/** A longitude wrapped into [-180, 180): the map keeps counting past the antimeridian. */
export function wrapLongitude(lng: number): number {
  return ((((lng + 180) % 360) + 360) % 360) - 180
}

/** The 1° square holding a point, with the longitude wrapped first. */
export function squareContaining(lat: number, lng: number): { lat: number; lon: number } {
  return { lat: Math.floor(lat), lon: Math.floor(wrapLongitude(lng)) }
}

/**
 * The not-held squares in view that can be selected, for the grid. A view
 * across the 180° meridian reports longitudes past 180, so each square's
 * longitude is wrapped; the map draws the square in every copy of the world.
 */
export function gridSquares(
  b: ViewBounds,
  held: ReadonlySet<string>,
  bounds: SelectableBounds,
): Array<{ lat: number; lon: number }> {
  const out: Array<{ lat: number; lon: number }> = []
  for (let lat = Math.floor(b.south); lat < Math.ceil(b.north); lat++) {
    for (let unwrapped = Math.floor(b.west); unwrapped < Math.ceil(b.east); unwrapped++) {
      const lon = Math.floor(wrapLongitude(unwrapped))
      if (squareSelectable(bounds, lat, lon) && !held.has(squareKey(lat, lon))) out.push({ lat, lon })
    }
  }
  return out
}

export interface SelectionSummary {
  /** Selected squares to ask for: not held and not already requested. */
  download: string[]
  /** Selected squares to remove: held ones, and requested ones still waiting (removing cancels the request). */
  remove: string[]
  /** Of the held ones, how many are within range of the boat and so download again. */
  removeInRange: number
  /** How many of remove are requests still waiting, not held squares. */
  removeRequested: number
}

export function summariseSelection(selected: ReadonlySet<string>, s: OfflineCoastlineStatus): SelectionSummary {
  const byKey = new Map(s.squares.map((q) => [squareKey(q.lat, q.lon), q]))
  const out: SelectionSummary = { download: [], remove: [], removeInRange: 0, removeRequested: 0 }
  for (const key of selected) {
    const q = byKey.get(key)
    if (q?.state === 'ready') {
      out.remove.push(key)
      if (q.in_range) out.removeInRange++
    } else if (q?.requested) {
      out.remove.push(key)
      out.removeRequested++
    } else {
      out.download.push(key)
    }
  }
  return out
}

/** What a download request would do, as the server counts it before anything is stored. */
export interface RequestPreview {
  requested: number
  already_requested: number
  held: number
  inland_skipped: number
}

/** Requested squares not yet downloaded: waiting and failed together. */
export function waitingRequests(s: OfflineCoastlineStatus): number {
  return (s.requested_waiting ?? 0) + (s.requested_failed ?? 0)
}

/** "Requested: 9 waiting, 1 failed (reason)", or null with no requested squares. */
export function requestedLine(s: OfflineCoastlineStatus): string | null {
  const waiting = s.requested_waiting ?? 0
  const failed = s.requested_failed ?? 0
  if (waitingRequests(s) === 0) return null
  const parts: string[] = []
  if (waiting > 0) parts.push(`${waiting} waiting`)
  if (failed > 0) parts.push(`${failed} failed`)
  let line = `Requested: ${parts.join(', ')}`
  const reason = s.squares.find((q) => q.requested && q.state === 'failed' && q.error)?.error
  if (failed > 0 && reason) line += ` (${reason})`
  if (!s.enabled) line += ' (offline coastline is off)'
  return line
}

/** The confirm text for a download of n squares, with the all-land ones the server will skip. */
export function downloadConfirmText(preview: RequestPreview): string {
  const skipped = preview.inland_skipped
  const base = `${plural(preview.requested, 'square', 'squares')} will download before the squares around the boat, one at a time.`
  return skipped > 0 ? `${base} ${plural(skipped, 'all-land square', 'all-land squares')} skipped.` : base
}

/** The confirm text for removing held squares, saying when some will come back. */
export function removeConfirmText(sum: SelectionSummary, radiusNm: number): string {
  const held = sum.remove.length - sum.removeRequested
  const parts: string[] = []
  if (held > 0) parts.push(`${plural(held, 'held square', 'held squares')} will be deleted.`)
  if (sum.removeRequested > 0) {
    parts.push(`${plural(sum.removeRequested, 'square you asked for', 'squares you asked for')} will no longer be downloaded.`)
  }
  if (sum.removeInRange > 0) {
    parts.push(`${plural(sum.removeInRange, 'square is', 'squares are')} within ${radiusNm} nm of the boat and will download again.`)
  }
  return parts.join(' ')
}
