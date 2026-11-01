import type { DistanceUnits } from '@/config/app-config'
import { destinationPoint } from '@/lib/geo'
import { dataDateText, distanceText, featureCollection, humanise, lonLat, pointsBounds, type Bounds } from '@/lib/marina-card'
import type { AnchorageCard, AnchorageMooring, AnchorageRestrictedArea, MarinaShape, ShelterGrade } from '@/lib/mate-cards'
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
  if (r.category) parts.push(humanise(r.category))
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
  }
}
