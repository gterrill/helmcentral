import type { DistanceUnits } from '@/config/app-config'
import { circlePolygon, destinationPoint } from '@/lib/geo'
import { dataDateText, distanceText, featureCollection, humanise, lonLat, pointsBounds, type Bounds } from '@/lib/marina-card'
import type { AnchorageCard, AnchorageDepth, AnchorageMooring, AnchorageRestrictedArea, MarinaShape, ShelterGrade } from '@/lib/mate-cards'
import { metersToFeet } from '@/lib/units'
import { formatDayMonthIn, formatTimeOfDay, formatWeekday } from '@/lib/vessel-time'

// What the anchorage card says, worked out from the card and nothing else.
// A figure the data does not carry is left out or marked, never guessed.

export const GRADE_LABEL: Record<ShelterGrade, string> = {
  sheltered: 'Sheltered',
  partial: 'Partly sheltered',
  exposed: 'Exposed',
  reef: 'Reef',
}

/**
 * The theme token each grade is drawn in, on the rose and on the map. Blue is
 * shelter, amber is the fringe of one, red is open to the wind, and a reef,
 * which depends on the tide, is muted.
 */
export const GRADE_TOKEN: Record<ShelterGrade, string> = {
  sheltered: '--primary',
  partial: '--gauge-primary',
  exposed: '--destructive',
  reef: '--muted-foreground',
}

/** The longest ray the shelter estimate casts, in nautical miles. */
export const MAX_FETCH_NM = 15
export const METRES_PER_NM = 1852

export interface RoseSector {
  bearingDeg: number
  grade: ShelterGrade
  fetchNm: number
  open: boolean
  /** The spoke's length as a share of the outer ring, 0 to 1. */
  share: number
  /** The sector spans bearing minus 5 to bearing plus 5 degrees. */
  from: number
  to: number
}

/** The 36 sectors of the rose; each spoke's length is its fetch up to 15 nm. */
export function roseSectors(card: AnchorageCard): RoseSector[] {
  return card.shelter.bearings.map((b) => ({
    bearingDeg: b.bearingDeg,
    grade: b.grade,
    fetchNm: b.fetchNm,
    open: b.open,
    share: b.open ? 1 : Math.min(Math.max(b.fetchNm, 0), MAX_FETCH_NM) / MAX_FETCH_NM,
    from: b.bearingDeg - 5,
    to: b.bearingDeg + 5,
  }))
}

/** The rose's accessible label: the arcs the position is sheltered, partly sheltered and exposed from. */
export function roseLabel(card: AnchorageCard): string {
  const arcs = card.shelter.arcs.map((a) => `${GRADE_LABEL[a.grade]} ${a.text}`)
  const said = card.shelter.summary !== '' ? card.shelter.summary : arcs.join('. ')
  return `Shelter by the direction the wind comes from. ${said}`.trim()
}

/** A time of day in the anchorage's zone: "Mon 15:00". */
export function timeText(iso: string, timeZone: string): string {
  return `${formatWeekday(new Date(iso), timeZone)} ${formatTimeOfDay(iso, timeZone)}`
}

/** A date and time in the anchorage's zone: "Mon 2 Nov 15:00". */
export function dateTimeText(iso: string, timeZone: string): string {
  return `${formatWeekday(new Date(iso), timeZone)} ${formatDayMonthIn(iso, timeZone)} ${formatTimeOfDay(iso, timeZone)}`
}

export const kn = (v: number | null): string => (v === null ? '--' : `${Math.round(v)}`)

/** A short length in the operator's units: "0.8 m", or "2.6 ft" when imperial. */
export function lengthText(m: number, units: DistanceUnits): string {
  return units === 'imperial' ? `${Number(metersToFeet(m).toFixed(1))} ft` : `${Number(m.toFixed(1))} m`
}

/** A vessel size limit, which is a whole number of metres or feet. */
export function sizeText(m: number, units: DistanceUnits): string {
  return units === 'imperial' ? `${Math.round(metersToFeet(m))} ft` : `${Number(m.toFixed(1))} m`
}

export function waveText(row: AnchorageCard['forecast']['rows'][number], units: DistanceUnits): string {
  if (row.waveM === null) return '--'
  const parts = [lengthText(row.waveM, units)]
  if (row.waveS !== null) parts.push(`${Number(row.waveS.toFixed(1))} s`)
  if (row.waveDir) parts.push(row.waveDir)
  return parts.join(' ')
}

export function windText(row: AnchorageCard['forecast']['rows'][number]): string {
  if (row.windKts === null) return '--'
  return row.gustKts === null ? `${kn(row.windKts)}` : `${kn(row.windKts)} g${kn(row.gustKts)}`
}

export function directionText(row: AnchorageCard['forecast']['rows'][number]): string {
  if (row.dir === '') return '--'
  return row.dirDeg === null ? row.dir : `${row.dir} ${String(Math.round(row.dirDeg)).padStart(3, '0')}°`
}

export type BadgeTone = 'critical' | 'warning' | 'neutral' | 'muted' | 'good'

export const GRADE_TONE: Record<ShelterGrade, BadgeTone> = { sheltered: 'good', partial: 'warning', exposed: 'critical', reef: 'muted' }

/** A restricted area as one line: its name (or its kind of restriction) and what is restricted. */
export interface RestrictedLine {
  label: string
  detail: string
  tone: BadgeTone
}

export function restrictedLine(r: AnchorageRestrictedArea, units: DistanceUnits): RestrictedLine {
  const restriction = r.restriction === '' ? '' : humanise(r.restriction)
  const label = r.name || restriction || 'Unnamed area'
  const parts: string[] = []
  if (r.source === 'protected_area') {
    parts.push(`${restriction || 'Protected area'}: anchoring rules depend on the zone, check the zoning`)
  } else if (r.prohibitsAnchoring) {
    parts.push('Anchoring prohibited')
  } else if (restriction) {
    parts.push(restriction)
  } else {
    parts.push('Restricted area')
  }
  // A protected area's category is the raw protect_class code list, which names nothing.
  if (r.category && r.source !== 'protected_area') parts.push(humanise(r.category))
  if (r.distanceM !== null) parts.push(r.distanceM === 0 ? 'you are inside it' : distanceText(r.distanceM, units))
  return { label, detail: parts.join(' · '), tone: r.prohibitsAnchoring ? 'critical' : r.source === 'protected_area' ? 'muted' : 'warning' }
}

export interface MooringGroup {
  /** "Up to 20 m · class B", or "Details not tagged". */
  label: string
  count: number
  nearestM: number
  untagged: boolean
}

/**
 * The public moorings grouped by what the record says about them, nearest
 * group first. A mooring with no size and no class is "Details not tagged";
 * the card never says what such a mooring takes.
 */
export function groupMoorings(list: AnchorageMooring[], units: DistanceUnits): MooringGroup[] {
  const groups = new Map<string, MooringGroup>()
  for (const m of list) {
    const label = m.detailsUntagged
      ? 'Details not tagged'
      : [m.vesselSizeM === null ? 'Size limit not tagged' : `Up to ${sizeText(m.vesselSizeM, units)}`, m.class ? `class ${m.class}` : null].filter(Boolean).join(' · ')
    const g = groups.get(label)
    if (g) {
      g.count += 1
      g.nearestM = Math.min(g.nearestM, m.distanceM)
    } else {
      groups.set(label, { label, count: 1, nearestM: m.distanceM, untagged: m.detailsUntagged })
    }
  }
  return [...groups.values()].sort((a, b) => a.nearestM - b.nearestM)
}

// ── depth and swing ──────────────────────────────────────────────────────

/** One line of the depth section: a check, its verdict and the sentence that says it. */
export interface DepthLine {
  key: 'depth' | 'clearance' | 'rode' | 'swing'
  title: string
  /** The verdict in words, so colour is never the only cue. */
  badge: string
  tone: BadgeTone
  text: string
  /** The basis or caveat behind the figure, when there is one. */
  detail: string
}

const skippedLine = (key: DepthLine['key'], title: string, reason: string): DepthLine => ({
  key,
  title,
  badge: 'Skipped',
  tone: 'muted',
  text: `Skipped: ${reason}.`,
  detail: '',
})

/** Where the depth came from, in words: "from you", or "from saved place X (charted 2026-11-01)". */
export function depthSourceText(d: AnchorageDepth): string {
  if (d.source === 'operator') return 'from you'
  if (d.source === 'saved_place') return `from saved place ${d.savedPlace} (${d.savedSource} ${d.savedDate})`
  return ''
}

/**
 * The depth section's lines, in the operator's units. With no depth there is
 * one line saying so and each check skipped with its reason: a check that did
 * not run is never drawn as passed.
 */
export function depthLines(card: AnchorageCard, units: DistanceUnits): DepthLine[] {
  const d = card.depth
  // A card stored before the depth checks existed has none: nothing to draw.
  if (d === null) return []
  const len = (m: number | null) => (m === null ? '--' : lengthText(m, units))
  const lines: DepthLine[] = []
  if (d.error !== '') {
    lines.push({ key: 'depth', title: 'Depth', badge: 'Unknown', tone: 'warning', text: d.error, detail: '' })
  } else if (!d.known || d.depthM === null) {
    lines.push({ key: 'depth', title: 'Depth', badge: 'Unknown', tone: 'warning', text: 'Depth unknown: check the chart.', detail: d.note })
  } else {
    lines.push({ key: 'depth', title: 'Depth', badge: len(d.depthM), tone: 'neutral', text: `${len(d.depthM)} at chart datum, ${depthSourceText(d)}`, detail: d.note })
  }

  const c = d.clearance
  if (c.status === 'skipped') {
    lines.push(skippedLine('clearance', 'Clearance at low water', c.skippedReason))
  } else {
    const shallow = c.status === 'too_shallow'
    lines.push({
      key: 'clearance',
      title: 'Clearance at low water',
      badge: shallow ? 'Too shallow' : 'OK',
      tone: shallow ? 'critical' : 'good',
      text: `Lowest water ${timeText(c.lowestWaterAt, card.timezone)} (${len(c.lowestWaterM)} on the tide table): ${len(c.depthAtLowM)} of water, ${len(c.clearanceM)} under a ${len(c.draftM)} draft. You want at least ${len(c.wantedM)}.`,
      detail: '',
    })
  }

  const r = d.rode
  if (r.status === 'skipped') {
    lines.push(skippedLine('rode', 'Rode at high water', r.skippedReason))
  } else {
    const short = r.status === 'short'
    lines.push({
      key: 'rode',
      title: 'Rode at high water',
      badge: short ? `Short by ${len(r.shortByM)}` : 'OK',
      tone: short ? 'critical' : 'good',
      text: `Highest water ${timeText(r.highestWaterAt, card.timezone)} (${len(r.highestWaterM)}): ${len(r.depthAtHighM)} of water. About ${len(r.rodeNeededM)} of rode (${r.scopeRatio === null ? '--' : Number(r.scopeRatio.toFixed(1))}:1, ${r.method}) against ${len(r.chainOnboardM)} aboard.`,
      detail: [
        r.windKts === null ? '' : `Planned at ${r.windKts} kts, the strongest forecast gust in the stay rounded up to its band.`,
        r.windCheckedUntil === '' ? '' : `The forecast ends at ${timeText(r.windCheckedUntil, card.timezone)}, before the stay does, so the wind was only checked up to then.`,
      ]
        .filter(Boolean)
        .join(' '),
    })
  }

  const w = d.swing
  if (w.status === 'skipped') {
    lines.push(skippedLine('swing', 'Swing', w.skippedReason))
  } else {
    lines.push({
      key: 'swing',
      title: 'Swing',
      badge: len(w.radiusM),
      tone: 'neutral',
      text: w.allChainOut
        ? `With all ${len(w.rodeM)} of chain out, short of the ${len(r.rodeNeededM)} needed, the boat swings within about ${len(w.radiusM)} of the anchor: ${len(w.bowOffsetM)} from bow to GPS and the boat's ${len(w.loaM)} length.`
        : `The boat swings within about ${len(w.radiusM)} of the anchor: ${len(w.rodeM)} of rode, ${len(w.bowOffsetM)} from bow to GPS and the boat's ${len(w.loaM)} length.`,
      detail: '',
    })
  }
  return lines
}

/** The footer's dated lines, each only when its date is known. */
export function footerLines(card: AnchorageCard): string[] {
  const lines: string[] = []
  if (card.forecast.fetchedAt !== '') lines.push(`Forecast fetched ${timeText(card.forecast.fetchedAt, card.timezone)}`)
  if (card.shelter.coastlineDownloaded !== '') lines.push(`Coastline downloaded ${dataDateText(card.shelter.coastlineDownloaded) || card.shelter.coastlineDownloaded}`)
  const osm = dataDateText(card.dataAsOf)
  lines.push(osm ? `OpenStreetMap · data as of ${osm}` : 'OpenStreetMap')
  return lines
}

// ── the map ──────────────────────────────────────────────────────────────

/**
 * What the map frames: the anchorage and one nautical mile round it, widened
 * only by the moorings (all within a mile). A large area or a long fetch ray is
 * drawn but may run off the map, so one regional-sized restricted area cannot
 * zoom the anchorage out of sight.
 */
export function anchorageFrame(card: AnchorageCard): Bounds {
  const pts: [number, number][] = [0, 90, 180, 270].map((bearing) => destinationPoint(card.lat, card.lon, (bearing * Math.PI) / 180, METRES_PER_NM))
  for (const m of card.features.moorings) pts.push([m.lat, m.lon])
  const bounds = pointsBounds(pts)
  if (!bounds) throw new Error('the anchorage map has nothing to frame')
  return bounds
}

export interface AnchorageFeatureCollections {
  rays: GeoJSON.FeatureCollection
  restrictedAreas: GeoJSON.FeatureCollection
  restrictedLines: GeoJSON.FeatureCollection
  anchorageAreas: GeoJSON.FeatureCollection
  seabedAreas: GeoJSON.FeatureCollection
  moorings: GeoJSON.FeatureCollection
  anchorage: GeoJSON.FeatureCollection
  /** The swing circle round the anchorage; empty when it was not computed. */
  swing: GeoJSON.FeatureCollection
}

/** Points on the circle of the swing radius round the anchorage; the ring closes. */
/** The swing circle as a closed ring of [lon, lat], or null when no radius was computed. */
export function swingRing(card: AnchorageCard): [number, number][] | null {
  if (card.depth === null) return null
  const radius = card.depth.swing.radiusM
  if (card.depth.swing.status !== 'computed' || radius === null || radius <= 0) return null
  return circlePolygon(card.lat, card.lon, radius)
}

/**
 * The map's shapes as GeoJSON. The 36 rays run from the anchorage on each
 * bearing for its fetch (15 nm where open), each carrying its grade. A closed
 * polygon is a Polygon; a line is a LineString; a point is a Point. The shore
 * the rays stop at is not drawn: the rays say where it is.
 */
export function anchorageFeatures(card: AnchorageCard, units: DistanceUnits): AnchorageFeatureCollections {
  const fc = featureCollection
  const rays = roseSectors(card).map((s): GeoJSON.Feature => ({
    type: 'Feature',
    properties: { grade: s.grade, bearing: s.bearingDeg },
    geometry: {
      type: 'LineString',
      coordinates: [[card.lon, card.lat], lonLat(destinationPoint(card.lat, card.lon, (s.bearingDeg * Math.PI) / 180, s.share * MAX_FETCH_NM * METRES_PER_NM))],
    },
  }))
  const polygon = (sh: MarinaShape, properties: GeoJSON.GeoJsonProperties): GeoJSON.Feature => ({ type: 'Feature', properties, geometry: { type: 'Polygon', coordinates: [sh.points.map(lonLat)] } })
  const outline = (sh: MarinaShape, properties: GeoJSON.GeoJsonProperties): GeoJSON.Feature =>
    sh.kind === 'point' ? { type: 'Feature', properties, geometry: { type: 'Point', coordinates: lonLat(sh.points[0]) } } : { type: 'Feature', properties, geometry: { type: 'LineString', coordinates: sh.points.map(lonLat) } }
  const ring = swingRing(card)
  const restricted = card.features.restrictedAreas
  const restrictedProps = (r: AnchorageRestrictedArea) => ({ name: r.name, prohibits: r.prohibitsAnchoring })
  return {
    rays: fc(rays),
    restrictedAreas: fc(restricted.flatMap((r) => r.shapes.filter((sh) => sh.kind === 'polygon').map((sh) => polygon(sh, restrictedProps(r))))),
    restrictedLines: fc(restricted.flatMap((r) => r.shapes.filter((sh) => sh.kind !== 'polygon').map((sh) => outline(sh, restrictedProps(r))))),
    anchorageAreas: fc(card.features.anchorageAreas.flatMap((a) => a.shapes.map((sh) => outline(sh, { name: a.name })))),
    seabedAreas: fc(card.features.seabedAreas.flatMap((a) => a.shapes.map((sh) => outline(sh, { name: a.name })))),
    moorings: fc(card.features.moorings.map((m): GeoJSON.Feature => ({
      type: 'Feature',
      properties: { label: m.vesselSizeM === null ? '' : sizeText(m.vesselSizeM, units) },
      geometry: { type: 'Point', coordinates: [m.lon, m.lat] },
    }))),
    anchorage: fc([{ type: 'Feature', properties: { name: card.name }, geometry: { type: 'Point', coordinates: [card.lon, card.lat] } }]),
    swing: fc(ring ? [{ type: 'Feature', properties: { radiusM: card.depth?.swing.radiusM ?? null }, geometry: { type: 'Polygon', coordinates: [ring] } }] : []),
  }
}
