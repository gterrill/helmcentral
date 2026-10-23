// The Offline coastline settings page's view of the server's coverage status
// (GET /api/offline-coastline), and the few pure helpers that turn it into
// words and map shapes. Words follow GLOSSARY.md: coverage, coverage square,
// edits since. Never "tile" (that is a dashboard tile), "cell" or "region".

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

export interface OfflineCoastlineStatus {
  enabled: boolean
  state: OfflineCoastlineState
  error?: string
  /** While paused: what the map server did, and when the worker tries again. */
  pause_reason?: string
  pause_detail?: string
  retry_at?: string
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
  oldest_downloaded_at?: string
}

/** A square named by its south-west corner, e.g. "21°S 148°E". */
export function squareLabel(lat: number, lon: number): string {
  const ns = lat < 0 ? 'S' : 'N'
  const ew = lon < 0 ? 'W' : 'E'
  return `${Math.abs(lat)}°${ns} ${Math.abs(lon)}°${ew}`
}

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`

function shortDate(iso: string, now: Date): string {
  const d = new Date(iso)
  const opts: Intl.DateTimeFormatOptions = { day: 'numeric', month: 'short' }
  if (d.getFullYear() !== now.getFullYear()) opts.year = 'numeric'
  return d.toLocaleDateString('en-GB', opts)
}

/** Local HH:MM, or null when the time is unreadable. */
export function clockTime(iso: string): string | null {
  const t = new Date(iso)
  if (Number.isNaN(t.getTime())) return null
  return t.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' })
}

function coverageText(s: OfflineCoastlineStatus, now: Date): string {
  if (s.held === 0) return 'Coverage: none yet'
  const parts = [`Coverage: ${plural(s.held, 'square', 'squares')}`]
  if (s.oldest_downloaded_at) parts.push(`oldest ${shortDate(s.oldest_downloaded_at, now)}`)
  if (s.edits_since > 0) parts.push(`${plural(s.edits_since, 'edit', 'edits')} since`)
  return parts.join(' · ')
}

/** The one line under the switch. */
export function coastlineStatusLine(s: OfflineCoastlineStatus, now: Date = new Date()): string {
  switch (s.state) {
    case 'off':
      return 'Off. Nothing is downloaded or checked.'
    case 'error':
      return s.error ?? 'Offline coastline cannot run.'
    case 'paused': {
      const at = s.retry_at ? clockTime(s.retry_at) : null
      return `${s.pause_reason ?? 'The map server is refusing requests'}. ${at ? `Trying again about ${at}` : 'Trying again later'}`
    }
    case 'waiting_for_fix':
      return s.held > 0
        ? `Waiting for a position fix · ${coverageText(s, now)}`
        : 'Waiting for a position fix'
    case 'updating':
      return `Updating ${plural(s.updating, 'square', 'squares')}`
    case 'downloading':
      return `Downloading ${s.wanted_held} of ${s.wanted}`
    case 'retrying':
      return `${coverageText(s, now)} · ${s.failed} failed, will retry`
    case 'idle':
      return coverageText(s, now)
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

const DAY_MS = 86_400_000

export interface SquareFeatureProps {
  state: CoastlineSquareState
  failed: boolean
  in_range: boolean
  has_edits: boolean
  age_days: number
  label: string
}

export interface SquareFeature {
  type: 'Feature'
  properties: SquareFeatureProps
  geometry: { type: 'Polygon'; coordinates: number[][][] }
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
        age_days: q.downloaded_at ? Math.max(0, Math.round((now.getTime() - new Date(q.downloaded_at).getTime()) / DAY_MS)) : 0,
        label: squareLabel(q.lat, q.lon),
      },
      geometry: {
        type: 'Polygon',
        coordinates: [[[q.lon, q.lat], [q.lon + 1, q.lat], [q.lon + 1, q.lat + 1], [q.lon, q.lat + 1], [q.lon, q.lat]]],
      },
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
