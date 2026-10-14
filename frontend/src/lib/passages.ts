import type { DistanceUnits } from '@/config/app-config'
import { formatQuantity, unitOption } from '@/lib/quantities'

// Passages page: the record shapes the backend returns and the pure helpers
// the page draws from (figure formatting, track segments, replay timeline,
// boat-local time). Kept React-free so each can be unit tested directly.

export interface PassageRecord {
  id: string
  start: string
  end: string
  from_name: string
  to_name: string
  title: string
  notes: string
  hidden: boolean
  edited: boolean
  cruise_id: string | null
  created_at: string
}

export interface PassageStats {
  distance_nm: number
  underway_seconds: number
  mean_sog_kts: number | null
  max_sog_kts: number | null
  fuel_burned_l: number | null
  tws_mean_kts: number | null
  max_gust_kts: number | null
  soc_start_pct: number | null
  soc_end_pct: number | null
  soc_min_pct: number | null
}

export interface PassageSummary extends PassageRecord {
  stats: PassageStats
  /** Longitude of the first recorded point, for dates in the boat's own zone. */
  start_lon: number | null
}

export interface PassageDetail extends PassageSummary {
  /** One list of [lat, lon] per segment. Segments are never joined. */
  track: [number, number][][]
  /** Unix seconds of each track point, same shape. */
  track_times: number[][]
  /** The replay track: thinned in time (about 30 s), with stops kept. The
   * boat marker follows this; the line on the map is `track`. */
  replay_track: [number, number][][]
  replay_times: number[][]
}

export interface CruiseRecord {
  id: string
  name: string
  notes: string
  created_at: string
}

export interface NightSoC {
  night: string
  sunset: string
  sunrise: string
  min_soc_pct: number | null
  dawn_soc_pct: number | null
}

export type WaterRiseLabel = 'Watermaker' | 'Dock fill'

export interface WaterRise {
  start: string
  end: string
  litres: number
  lat: number
  lon: number
  suggested_label: string | null
  looked_up_at: string | null
  label: string | null
}

export interface CruiseStats {
  passage_count: number
  departure: string | null
  arrival: string | null
  span_seconds: number
  underway_seconds: number
  nights: number
  distance_nm: number
  fuel_burned_l: number | null
  underway_tws_mean_kts: number | null
  underway_max_gust_kts: number | null
  anchor_tws_mean_kts: number | null
  anchor_max_gust_kts: number | null
  generator_hours: number | null
  lowest_soc_pct: number | null
  night_soc: NightSoC[]
  water_used_l: number | null
  water_taken_on_l: number | null
  water_rises: WaterRise[]
}

export interface ImportSummary {
  files: { name: string; points: number }[]
  points_read: number
  points_added: number
  points_rejected: number
  points_already_imported: number
  points_in_recorded_log: number
  passages_created: number
  passages_skipped: number
}

// ── figures ──────────────────────────────────────────────────────────────

export const ABSENT = '--'

function present(v: number | null | undefined): v is number {
  return typeof v === 'number' && Number.isFinite(v)
}

/** A figure to `digits` places, or `--` when the log holds no reading. */
export function fmtFigure(v: number | null | undefined, digits = 1): string {
  return present(v) ? v.toFixed(digits) : ABSENT
}

export const fmtNM = (v: number | null | undefined) => (present(v) ? `${v.toFixed(1)} NM` : ABSENT)
export const fmtKn = (v: number | null | undefined) => (present(v) ? `${v.toFixed(1)} kn` : ABSENT)
/** Litres as the Units setting asks: litres for metric, US gallons for
 * imperial. Distance and speed stay nautical (NM, kn) in both. */
export function fmtVolume(litres: number | null | undefined, units: DistanceUnits): string {
  if (!present(litres)) return ABSENT
  const unitId = units === 'imperial' ? 'gal' : 'L'
  return `${formatQuantity(litres / 1000, 'volume', unitId)} ${unitOption('volume', unitId).label}`
}
export const fmtPct = (v: number | null | undefined) => (present(v) ? `${Math.round(v)}%` : ABSENT)
export const fmtHours = (v: number | null | undefined) => (present(v) ? `${v.toFixed(1)} h` : ABSENT)

/** `3 h 05 m`, `42 m`, or `--`. Zero is a real figure and prints as `0 m`. */
export function fmtDuration(seconds: number | null | undefined): string {
  if (!present(seconds) || seconds < 0) return ABSENT
  const totalMinutes = Math.round(seconds / 60)
  const h = Math.floor(totalMinutes / 60)
  const m = totalMinutes % 60
  if (h === 0) return `${m} m`
  return `${h} h ${String(m).padStart(2, '0')} m`
}

/** `from → to`, with whichever end is unnamed left as `--`. */
export function passageRoute(p: Pick<PassageRecord, 'from_name' | 'to_name'>): string {
  return `${p.from_name || ABSENT} → ${p.to_name || ABSENT}`
}

/** The passage's own title, or its route when it has none. */
export function passageHeading(p: Pick<PassageRecord, 'title' | 'from_name' | 'to_name'>): string {
  return p.title.trim() || passageRoute(p)
}

// ── boat-local time ──────────────────────────────────────────────────────

/** The vessel's zone, from longitude, the same whole-hour rule the server
 * uses for its nights, so a night on the page is a night in the figures. */
export function vesselZone(lon: number): { offsetHours: number; label: string } {
  if (!Number.isFinite(lon) || lon < -180 || lon > 180) return { offsetHours: 0, label: 'UTC' }
  const offsetHours = Math.max(-12, Math.min(14, Math.round(lon / 15)))
  return { offsetHours, label: offsetHours === 0 ? 'UTC' : `UTC${offsetHours > 0 ? '+' : ''}${offsetHours}` }
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

/** `14 Mar 09:32 UTC+10` for a Unix-seconds instant at a longitude. */
export function formatLocalDateTime(unixSeconds: number, lon: number): string {
  const zone = vesselZone(lon)
  const d = new Date((unixSeconds + zone.offsetHours * 3600) * 1000)
  const hh = String(d.getUTCHours()).padStart(2, '0')
  const mm = String(d.getUTCMinutes()).padStart(2, '0')
  return `${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]} ${hh}:${mm} ${zone.label}`
}

/** `14 Mar` for an ISO instant at a longitude. */
export function formatLocalDate(iso: string, lon: number): string {
  const unix = Date.parse(iso) / 1000
  if (!Number.isFinite(unix)) return ABSENT
  const zone = vesselZone(lon)
  const d = new Date((unix + zone.offsetHours * 3600) * 1000)
  return `${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]}`
}

// ── track ────────────────────────────────────────────────────────────────

export interface TimedPoint {
  t: number
  lat: number
  lon: number
}

/** One unbroken run of fixes. A gap in the log starts a new segment, and a
 * line is never drawn across the gap. */
export type TimedSegment = TimedPoint[]

/** Pairs each segment's positions with its times. A time list that does not
 * match its segment is a server fault, so it throws rather than draw a guess. */
export function timedSegments(track: [number, number][][], times: number[][]): TimedSegment[] {
  if (track.length !== times.length) throw new Error('Passage track and track times do not match')
  return track.map((seg, i) => {
    if (seg.length !== times[i].length) throw new Error('Passage track and track times do not match')
    return seg.map(([lat, lon], j) => ({ t: times[i][j], lat, lon }))
  })
}

/** A GeoJSON MultiLineString of the segments that can be drawn (two points or
 * more), in [lon, lat] order, one line per segment. */
export function segmentsToGeoJSON(segments: TimedSegment[]): GeoJSON.Feature<GeoJSON.MultiLineString> {
  return {
    type: 'Feature',
    properties: {},
    geometry: {
      type: 'MultiLineString',
      coordinates: segments.filter((s) => s.length >= 2).map((s) => s.map((p) => [p.lon, p.lat])),
    },
  }
}

/** [west, south, east, north] over every point, or null when there are none. */
export function segmentBounds(segments: TimedSegment[]): [number, number, number, number] | null {
  let west = Infinity, south = Infinity, east = -Infinity, north = -Infinity
  for (const seg of segments) {
    for (const p of seg) {
      west = Math.min(west, p.lon); east = Math.max(east, p.lon)
      south = Math.min(south, p.lat); north = Math.max(north, p.lat)
    }
  }
  return Number.isFinite(west) ? [west, south, east, north] : null
}

/** Where the boat is at `t` (Unix seconds). Inside a segment the position is
 * interpolated between fixes. In a gap between segments it holds at the last
 * fix before the gap, so the marker never slides across water the log does
 * not cover. Before the first fix and after the last it holds at that end. */
export function positionAt(segments: TimedSegment[], t: number): { lat: number; lon: number } | null {
  const usable = segments.filter((s) => s.length > 0)
  if (usable.length === 0) return null
  if (t <= usable[0][0].t) return { lat: usable[0][0].lat, lon: usable[0][0].lon }
  let held: TimedPoint = usable[0][0]
  for (const seg of usable) {
    const first = seg[0]
    const last = seg[seg.length - 1]
    if (t < first.t) return { lat: held.lat, lon: held.lon }
    if (t <= last.t) {
      for (let i = 1; i < seg.length; i++) {
        const a = seg[i - 1]
        const b = seg[i]
        if (t <= b.t) {
          const span = b.t - a.t
          const f = span > 0 ? (t - a.t) / span : 1
          return { lat: a.lat + (b.lat - a.lat) * f, lon: a.lon + (b.lon - a.lon) * f }
        }
      }
    }
    held = last
  }
  return { lat: held.lat, lon: held.lon }
}

// ── replay timeline ──────────────────────────────────────────────────────

export interface TimelineEntry {
  id: string
  /** Unix seconds the passage starts and ends. */
  start: number
  end: number
  /** Where this passage begins on the replay clock. */
  offset: number
  segments: TimedSegment[]
}

export interface Timeline {
  entries: TimelineEntry[]
  /** Replay clock length in seconds: the passages end to end, the time between
   * them (nights at anchor) left out. */
  total: number
}

/** Lays passages end to end, oldest first, so a combined view replays one
 * after another instead of idling through the days between. */
export function buildTimeline(passages: { id: string; start: number; end: number; segments: TimedSegment[] }[]): Timeline {
  const sorted = [...passages].sort((a, b) => a.start - b.start)
  let offset = 0
  const entries = sorted.map((p) => {
    const entry = { ...p, offset }
    offset += Math.max(0, p.end - p.start)
    return entry
  })
  return { entries, total: offset }
}

/** The passage the replay clock is in and the real instant it stands for. */
export function locate(timeline: Timeline, clock: number): { entry: TimelineEntry; at: number } | null {
  if (timeline.entries.length === 0) return null
  const c = Math.max(0, Math.min(timeline.total, clock))
  for (let i = timeline.entries.length - 1; i >= 0; i--) {
    const e = timeline.entries[i]
    if (c >= e.offset) return { entry: e, at: Math.min(e.end, e.start + (c - e.offset)) }
  }
  return null
}

/** The replay clock value for a real instant inside a given passage. */
export function clockFor(timeline: Timeline, id: string, at: number): number | null {
  const e = timeline.entries.find((x) => x.id === id)
  if (!e) return null
  return e.offset + Math.max(0, Math.min(e.end - e.start, at - e.start))
}
