import type { DistanceUnits } from '@/config/app-config'
import playerCss from '@/lib/passage-export-player.css.txt?raw'
import playerSource from '@/lib/passage-export-player.js?raw'
import {
  DONE_LAYERS,
  EXPORT_BASE_STYLE_URL,
  EXPORT_SEAMARK_ATTRIBUTION,
  EXPORT_SEAMARK_TILES,
  MAPLIBRE_CSS_SRI,
  MAPLIBRE_CSS_URL,
  MAPLIBRE_JS_SRI,
  MAPLIBRE_JS_URL,
  SEAMARK_LAYER_ID,
  SEAMARK_SOURCE_ID,
  TRACK_LAYERS,
  exportColour,
  insetZoom,
} from '@/lib/passage-export-style'
import {
  ABSENT,
  fmtDuration,
  fmtKn,
  fmtNM,
  fmtVolume,
  passageHeading,
  segmentBounds,
  timedSegments,
  type CruiseStats,
  type PassageDetail,
  type TimedSegment,
} from '@/lib/passages'

// The blog export (ADR 0174): one passage, a cruise or a combined selection
// turned into a single HTML snippet for a Ghost HTML card. The snippet carries
// the simplified track and the figures inline and loads only public map
// resources, so a reader's browser never needs to reach the boat.

export type PlaySeconds = 15 | 30 | 60
export const PLAY_LENGTHS: readonly PlaySeconds[] = [15, 30, 60]
export const DEFAULT_PLAY_SECONDS: PlaySeconds = 30

/** The most track points the snippet carries, across every passage. */
export const POINT_BUDGET = 1500

/** One night at anchor lasts this long on screen, and a long stay no more than
 * NIGHT_CAP_SECONDS in all. */
export const NIGHT_SECONDS = 1.5
export const NIGHT_CAP_SECONDS = 6

export interface ExportInput {
  kind: 'passage' | 'cruise' | 'combined'
  /** The card heading: the passage title, the cruise name, or a summary. */
  title: string
  passages: PassageDetail[]
  /** The server's figures for a cruise, used instead of summing the passages. */
  cruiseStats?: CruiseStats | null
  units: DistanceUnits
  zone: { offsetHours: number; label: string }
  /** Whether the overview map shows in the corner during the replay. */
  inset: boolean
  playSeconds: PlaySeconds
  /** An id for the card's elements. Random unless given. */
  cardId?: string
}

/** One passage as the player reads it. Short keys keep the snippet small. */
export interface ExportPassage {
  /** Heading shown in the readout. */
  n: string
  /** Line colour. */
  c: string
  /** Start and end, Unix seconds. */
  a: number
  b: number
  /** Segments of [t, lon, lat]; a gap in the log is a new segment. */
  s: [number, number, number][][]
  /** Nights at anchor between the previous passage and this one. */
  nb: number
  /** The number of the first of those nights. */
  n0: number
  /** Where those nights were spent: the previous passage's arrival. */
  pl: string
}

export interface ExportData {
  title: string
  dates: string
  zone: { o: number; l: string }
  /** Show the overview map during the replay. */
  inset: boolean
  insetZoom: number
  /** Underway play length, seconds. */
  play: number
  nightSeconds: number
  nightCap: number
  bounds: [number, number, number, number]
  stats: { l: string; v: string }[]
  passages: ExportPassage[]
  map: {
    style: string
    seamarks: { id: string; layerId: string; tiles: string; attribution: string }
    layers: unknown[]
    trackSource: string
    doneSource: string
  }
}

// ── track thinning ───────────────────────────────────────────────────────

/** Gaps in time longer than this keep both points either side: the boat sat
 * still (or the log paused) there, and the marker must not glide through it. */
const KEEP_GAP_SECONDS = 300

function simplifySegment(seg: TimedSegment, tolerance: number): TimedSegment {
  const n = seg.length
  if (n <= 2) return seg
  const midLat = seg.reduce((sum, p) => sum + p.lat, 0) / n
  const kx = Math.cos((midLat * Math.PI) / 180)
  const x = (i: number) => seg[i].lon * kx
  const y = (i: number) => seg[i].lat
  const keep = new Uint8Array(n)
  keep[0] = 1
  keep[n - 1] = 1
  for (let i = 1; i < n; i++) {
    if (seg[i].t - seg[i - 1].t > KEEP_GAP_SECONDS) { keep[i] = 1; keep[i - 1] = 1 }
  }
  const stack: [number, number][] = [[0, n - 1]]
  while (stack.length > 0) {
    const [lo, hi] = stack.pop() as [number, number]
    if (hi <= lo + 1) continue
    const dx = x(hi) - x(lo)
    const dy = y(hi) - y(lo)
    const len2 = dx * dx + dy * dy
    let worst = -1
    let worstDist = tolerance
    for (let i = lo + 1; i < hi; i++) {
      let dist: number
      if (len2 === 0) {
        dist = Math.hypot(x(i) - x(lo), y(i) - y(lo))
      } else {
        const f = Math.max(0, Math.min(1, ((x(i) - x(lo)) * dx + (y(i) - y(lo)) * dy) / len2))
        dist = Math.hypot(x(i) - (x(lo) + f * dx), y(i) - (y(lo) + f * dy))
      }
      if (dist > worstDist) { worstDist = dist; worst = i }
    }
    if (worst >= 0) {
      keep[worst] = 1
      stack.push([lo, worst], [worst, hi])
    } else {
      // Within tolerance, but a kept pause point inside the run still splits it.
      let split = -1
      for (let i = lo + 1; i < hi; i++) if (keep[i]) { split = i; break }
      if (split >= 0) stack.push([lo, split], [split, hi])
    }
  }
  const out: TimedSegment = []
  for (let i = 0; i < n; i++) if (keep[i]) out.push(seg[i])
  return out
}

function countAt(all: TimedSegment[], tolerance: number): number {
  let total = 0
  for (const seg of all) total += simplifySegment(seg, tolerance).length
  return total
}

/** Thins every segment with one shared tolerance, the smallest that fits the
 * budget, so a long passage does not starve a short one of detail. */
export function simplifyTracks(passages: TimedSegment[][], budget: number = POINT_BUDGET): TimedSegment[][] {
  const flat = passages.flat()
  let tolerance = 0
  if (countAt(flat, 0) > budget) {
    let lo = 0
    let hi = 90
    for (let i = 0; i < 40; i++) {
      const mid = (lo + hi) / 2
      if (countAt(flat, mid) > budget) lo = mid
      else hi = mid
    }
    tolerance = hi
  }
  return passages.map((segs) => segs.map((seg) => simplifySegment(seg, tolerance)))
}

// ── figures ──────────────────────────────────────────────────────────────

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

function localParts(unix: number, offsetHours: number) {
  const d = new Date((unix + offsetHours * 3600) * 1000)
  return { day: d.getUTCDate(), month: d.getUTCMonth(), year: d.getUTCFullYear() }
}

/** `14 Sep 2026`, `14 to 16 Sep 2026`, `28 Sep to 2 Oct 2026` or across a
 * year end, in the boat's own zone. */
export function dateRangeLabel(startUnix: number, endUnix: number, offsetHours: number): string {
  const a = localParts(startUnix, offsetHours)
  const b = localParts(endUnix, offsetHours)
  const full = (p: typeof a) => `${p.day} ${MONTHS[p.month]} ${p.year}`
  if (a.year === b.year && a.month === b.month && a.day === b.day) return full(a)
  if (a.year !== b.year) return `${full(a)} to ${full(b)}`
  if (a.month !== b.month) return `${a.day} ${MONTHS[a.month]} to ${full(b)}`
  return `${a.day} to ${full(b)}`
}

/** Whole boat-local midnights between two instants: the nights at anchor. */
export function nightsBetween(endUnix: number, nextStartUnix: number, offsetHours: number): number {
  const day = (t: number) => Math.floor((t + offsetHours * 3600) / 86400)
  return Math.max(0, day(nextStartUnix) - day(endUnix))
}

/** A heading for a hand-picked set of passages. */
export function combinedTitle(passages: Pick<PassageDetail, 'from_name' | 'to_name' | 'start'>[]): string {
  const sorted = [...passages].sort((a, b) => Date.parse(a.start) - Date.parse(b.start))
  const first = sorted[0]
  const last = sorted[sorted.length - 1]
  if (first?.from_name && last?.to_name) return `${first.from_name} to ${last.to_name}`
  return `${passages.length} passages`
}

function sumOrNull(values: (number | null)[]): number | null {
  if (values.length === 0 || values.some((v) => v === null || !Number.isFinite(v))) return null
  return (values as number[]).reduce((a, b) => a + b, 0)
}

function maxOrNull(values: (number | null)[]): number | null {
  const present = values.filter((v): v is number => typeof v === 'number' && Number.isFinite(v))
  return present.length === 0 ? null : Math.max(...present)
}

function statsStrip(input: ExportInput, nights: number): { l: string; v: string }[] {
  const { passages, cruiseStats, units } = input
  let distance: number
  let underway: number
  let fuel: number | null
  let gust: number | null
  let nightCount: number | null = null
  if (input.kind === 'cruise' && cruiseStats) {
    distance = cruiseStats.distance_nm
    underway = cruiseStats.underway_seconds
    fuel = cruiseStats.fuel_burned_l
    gust = maxOrNull([cruiseStats.underway_max_gust_kts, cruiseStats.anchor_max_gust_kts])
    nightCount = cruiseStats.nights
  } else {
    distance = passages.reduce((n, p) => n + p.stats.distance_nm, 0)
    underway = passages.reduce((n, p) => n + p.stats.underway_seconds, 0)
    fuel = sumOrNull(passages.map((p) => p.stats.fuel_burned_l))
    gust = maxOrNull(passages.map((p) => p.stats.max_gust_kts))
    if (input.kind !== 'passage') nightCount = nights
  }
  const strip = [
    { l: 'Distance', v: fmtNM(distance) },
    { l: 'Underway', v: fmtDuration(underway) },
    { l: 'Nights', v: nightCount === null ? ABSENT : String(nightCount) },
    { l: 'Fuel', v: fmtVolume(fuel, units) },
    { l: 'Max gust', v: fmtKn(gust) },
  ]
  return strip.filter((s) => s.v !== ABSENT)
}

const round = (v: number, places: number) => Math.round(v * 10 ** places) / 10 ** places

// ── preparing the data ───────────────────────────────────────────────────

/** Everything the player and the static images draw from. Throws when there
 * is no track to show: an export of nothing is a fault, not an empty card. */
export function prepareExport(input: ExportInput): ExportData {
  const sorted = [...input.passages].sort((a, b) => Date.parse(a.start) - Date.parse(b.start))
  const withTracks = sorted
    .map((p) => ({ p, segments: timedSegments(p.track, p.track_times).filter((s) => s.length > 0) }))
    .filter((x) => x.segments.length > 0)
  if (withTracks.length === 0) throw new Error('Nothing to export: no track was recorded for these passages')

  const thinned = simplifyTracks(withTracks.map((x) => x.segments))
  const bounds = segmentBounds(thinned.flat())
  if (!bounds) throw new Error('Nothing to export: no track was recorded for these passages')

  let nightNo = 1
  let totalNights = 0
  const passages: ExportPassage[] = withTracks.map(({ p }, i) => {
    const a = Date.parse(p.start) / 1000
    const b = Date.parse(p.end) / 1000
    const prev = i > 0 ? withTracks[i - 1].p : null
    const nb = prev ? nightsBetween(Date.parse(prev.end) / 1000, a, input.zone.offsetHours) : 0
    const entry: ExportPassage = {
      n: passageHeading(p),
      c: exportColour(i),
      a,
      b,
      s: thinned[i].map((seg) => seg.map((pt) => [Math.round(pt.t), round(pt.lon, 5), round(pt.lat, 5)] as [number, number, number])),
      nb,
      n0: nightNo,
      pl: prev?.to_name ?? '',
    }
    nightNo += nb
    totalNights += nb
    return entry
  })

  const first = passages[0]
  const last = passages[passages.length - 1]
  return {
    title: input.title,
    dates: dateRangeLabel(first.a, last.b, input.zone.offsetHours),
    zone: { o: input.zone.offsetHours, l: input.zone.label },
    inset: input.inset,
    insetZoom: round(insetZoom(bounds), 2),
    play: input.playSeconds,
    nightSeconds: NIGHT_SECONDS,
    nightCap: NIGHT_CAP_SECONDS,
    bounds: bounds.map((v) => round(v, 5)) as ExportData['bounds'],
    stats: statsStrip(input, totalNights),
    passages,
    map: {
      style: EXPORT_BASE_STYLE_URL,
      seamarks: { id: SEAMARK_SOURCE_ID, layerId: SEAMARK_LAYER_ID, tiles: EXPORT_SEAMARK_TILES, attribution: EXPORT_SEAMARK_ATTRIBUTION },
      layers: [...TRACK_LAYERS, ...DONE_LAYERS],
      trackSource: TRACK_LAYERS[0].source,
      doneSource: DONE_LAYERS[0].source,
    },
  }
}

// ── the snippet ──────────────────────────────────────────────────────────

/** JSON that is safe inside a <script> element: nothing in it can close the
 * element, open a comment or break a JavaScript string. */
export function safeJson(value: unknown): string {
  return JSON.stringify(value).replace(/[<>&\u2028\u2029]/g, (c) => `\\u${c.charCodeAt(0).toString(16).padStart(4, '0')}`)
}

export function escapeHtml(text: string): string {
  return text.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c] as string)
}

function newCardId(): string {
  const bytes = new Uint8Array(5)
  crypto.getRandomValues(bytes)
  return `hcp-${Array.from(bytes, (b) => b.toString(36).padStart(2, '0')).join('')}`
}

/** The player's source as written, for tests. */
export const PLAYER_SOURCE: string = playerSource

/** The player's source, ready to run for one card. */
export function playerScript(id: string): string {
  return `(function () {\n${playerSource}\nhcPassageCardInit(${JSON.stringify(id)}, window.maplibregl, document);\n})();`
}

/** The one self-contained HTML snippet for a Ghost HTML card. */
export function generateEmbed(input: ExportInput): string {
  const data = prepareExport(input)
  const id = input.cardId ?? newCardId()
  if (!/^[A-Za-z][A-Za-z0-9_-]*$/.test(id)) throw new Error(`Invalid card id: ${id}`)
  const css = playerCss.split('#HCP_ID').join(`#${id}`)
  return [
    `<!-- Helmcentral passage card. Paste into a Ghost HTML card. -->`,
    `<link rel="stylesheet" href="${MAPLIBRE_CSS_URL}" integrity="${MAPLIBRE_CSS_SRI}" crossorigin="anonymous">`,
    `<script src="${MAPLIBRE_JS_URL}" integrity="${MAPLIBRE_JS_SRI}" crossorigin="anonymous"></script>`,
    `<style>\n${css}</style>`,
    `<div id="${id}" class="hcp hcp-idle">`,
    `  <div class="hcp-head"><h3 class="hcp-title"></h3><p class="hcp-dates"></p></div>`,
    `  <div class="hcp-stage">`,
    `    <div class="hcp-map" role="region" aria-label="Passage map"></div>`,
    `    <div class="hcp-inset" hidden></div>`,
    `    <div class="hcp-readout" hidden><p class="hcp-caption" hidden></p><p class="hcp-passage"></p><p class="hcp-time"></p></div>`,
    `    <button type="button" class="hcp-btn"><span class="hcp-btn-icon" aria-hidden="true"></span><span class="hcp-btn-label">Play</span></button>`,
    `    <p class="hcp-error" role="alert" hidden></p>`,
    `  </div>`,
    `  <div class="hcp-stats"></div>`,
    `  <script type="application/json" id="${id}-data">${safeJson(data)}</script>`,
    `  <script>\n${playerScript(id)}\n  </script>`,
    `</div>`,
  ].join('\n')
}

/** A full page around the snippet, for previewing it outside Ghost. */
export function generateStandalonePage(input: ExportInput): string {
  const snippet = generateEmbed(input)
  return [
    '<!doctype html>',
    '<html lang="en">',
    '<head>',
    '<meta charset="utf-8">',
    '<meta name="viewport" content="width=device-width, initial-scale=1">',
    `<title>${escapeHtml(input.title)}</title>`,
    '<style>body{margin:0;background:#ffffff;color:#1c1917;font-family:system-ui,-apple-system,"Segoe UI",Roboto,sans-serif}.page{max-width:720px;margin:0 auto;padding:24px 16px}</style>',
    '</head>',
    '<body>',
    '<div class="page">',
    snippet,
    '</div>',
    '</body>',
    '</html>',
    '',
  ].join('\n')
}

/** A file name from the title and first date: `whitsundays-2026-09-14`. */
export function exportFileName(title: string, startUnix: number, offsetHours: number, ext: string): string {
  const slug = title
    .normalize('NFKD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 60)
    .replace(/-+$/g, '')
  const d = new Date((startUnix + offsetHours * 3600) * 1000)
  const date = `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, '0')}-${String(d.getUTCDate()).padStart(2, '0')}`
  return `${slug || 'passage'}-${date}.${ext}`
}
