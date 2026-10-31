import type { DistanceUnits } from '@/config/app-config'
import { formatPoiDistance } from '@/lib/format'
import type { MarinaCard, MarinaShape } from '@/lib/mate-cards'

// What the marina card says, worked out from the card and nothing else. The
// words are the operator's: slugs like haul_out become "Haul-out", and a
// figure the map does not record is left out rather than guessed.

const KIND_LABELS: Record<string, string> = {
  fuel: 'Fuel',
  power: 'Power',
  water: 'Water',
  pump_out: 'Pump-out',
  haul_out: 'Haul-out',
  boatyard: 'Boatyard',
  crane: 'Crane',
  ramp: 'Ramp',
  visitor_berth: 'Visitor berth',
  toilets: 'Toilets',
  showers: 'Showers',
  laundry: 'Laundry',
  chandler: 'Chandler',
  boat_shop: 'Boat shop',
  breakwater: 'Breakwater',
  other: 'Other',
}

/** Sentence case with the slug's separators opened up: "boat_shop" is "Boat shop". */
export function humanise(slug: string): string {
  const words = slug.replace(/[_:]+/g, ' ').trim()
  return words === '' ? '' : words.charAt(0).toUpperCase() + words.slice(1)
}

/** A facility, mark or service kind as the operator says it. */
export function kindLabel(kind: string): string {
  return KIND_LABELS[kind] ?? humanise(kind)
}

const lower = (s: string) => s.charAt(0).toLowerCase() + s.slice(1)

/** One tagged fact as a short phrase: crane:type=travel_lift is "travel lift". */
export function factText(key: string, value: string): string {
  switch (key) {
    case 'fee':
      return value === 'yes' ? 'fee charged' : value === 'no' ? 'no fee' : `fee ${value}`
    case 'opening_hours':
      return `open ${value}`
    case 'crane:type':
      return value.replace(/_/g, ' ')
    case 'floating':
      return value === 'yes' ? 'floating' : `floating ${value}`
    default:
      return `${lower(humanise(key))} ${value.replace(/_/g, ' ')}`
  }
}

/** The fuels a fuel facility lists: fuel:diesel=yes and fuel:octane_95=yes are "diesel, octane 95". */
export function fuelTypes(facts: Record<string, string>): string[] {
  return Object.entries(facts)
    .filter(([k, v]) => k.startsWith('fuel:') && v !== 'no')
    .map(([k]) => k.slice('fuel:'.length).replace(/_/g, ' '))
    .sort()
}

export interface FacilityLine {
  label: string
  /** The line under the label: the facility's name and facts. */
  detail: string
}

/**
 * A fuel dock with no fuel tags reads "Fuel · type unknown": the map says the
 * fuel is there, not which fuel, and the card must not suggest it has diesel.
 */
export function facilityLine(f: MarinaCard['facilities'][number]): FacilityLine {
  const parts: string[] = []
  if (f.name) parts.push(f.name)
  if (f.kind === 'fuel') {
    const types = fuelTypes(f.facts)
    parts.push(types.length > 0 ? types.join(', ') : 'type unknown')
  }
  for (const [k, v] of Object.entries(f.facts).sort(([a], [b]) => a.localeCompare(b))) {
    if (k.startsWith('fuel:')) continue
    parts.push(factText(k, v))
  }
  const label = f.count > 1 ? `${kindLabel(f.kind)} ×${f.count}` : kindLabel(f.kind)
  return { label, detail: parts.join(' · ') }
}

/** "Beacon ×2 · port · red". A side the map does not tag is said so. */
export function markLine(m: MarinaCard['entranceMarks'][number]): string {
  const side = m.side === 'not tagged' || m.side === '' ? 'side not tagged' : m.side
  const parts = [m.count > 1 ? `${humanise(m.kind)} ×${m.count}` : humanise(m.kind), side]
  if (m.colour) parts.push(m.colour)
  if (m.name) parts.push(m.name)
  return parts.join(' · ')
}

/** A distance in the operator's units: "580 m" under a kilometre, "1.2 km" over, nautical miles for imperial. */
export function distanceText(m: number, units: DistanceUnits): string {
  return units === 'metric' && m < 1000 ? `${Math.round(m)} m` : formatPoiDistance(m, units)
}

export function metres(v: number): string {
  return `${Number(v.toFixed(1))} m`
}

export type LimitTone = 'critical' | 'neutral' | 'muted'

export interface LimitBadge {
  text: string
  tone: LimitTone
  /** The sentence behind the badge, for hover and assistive tech. */
  title: string
}

/**
 * One badge per particular. "exceeds" is the only alarm colour; "ok" is plain;
 * a limit that is not charted, or a boat figure that is not set, is muted and
 * says so in words, since neither means the boat fits.
 */
export function limitBadge(c: MarinaCard['vesselLimitCheck'][number]): LimitBadge {
  const name = c.particular === 'draft' ? 'Draft' : 'Length'
  // Only exceeds and ok have both figures to give.
  const compared = () => {
    const known = c.boatM !== null && c.chartedLimitM !== null
    return {
      figures: known ? ` · ${metres(c.boatM as number)} of ${metres(c.chartedLimitM as number)}` : '',
      sentence: known
        ? `The boat's ${c.particular} is ${metres(c.boatM as number)}; the charted maximum is ${metres(c.chartedLimitM as number)}.`
        : `The boat's ${c.particular} was compared with the charted maximum.`,
    }
  }
  switch (c.status) {
    case 'exceeds': {
      const { figures, sentence } = compared()
      return { text: `${name} exceeds${figures}`, tone: 'critical', title: sentence }
    }
    case 'ok': {
      const { figures, sentence } = compared()
      return { text: `${name} ok${figures}`, tone: 'neutral', title: `${sentence} Charted limits can be out of date: confirm with the marina.` }
    }
    case 'not charted':
      return { text: `${name} not charted`, tone: 'muted', title: `No maximum ${c.particular} is charted for this marina. Ask the marina.` }
    case 'particular not set':
      return { text: `${name} particular not set`, tone: 'muted', title: `The boat's ${c.particular} is not set, so the charted limit could not be checked.` }
  }
}

/** "Power available" for yes, "No power" for no, and the map's own words otherwise. */
function yesNo(value: string, yes: string, no: string, other: (v: string) => string): string | null {
  if (value === '') return null
  if (value === 'yes') return yes
  if (value === 'no') return no
  return other(value)
}

/** The facts under the marina's name, in reading order, each only when the map records it. */
export function headerFacts(card: MarinaCard): string[] {
  const facts: string[] = []
  if (card.berths !== null) facts.push(`${card.berths} berths`)
  const power = yesNo(card.powerSupply, 'Power available', 'No power', (v) => `Power ${v}`)
  if (power) facts.push(power)
  const fee = yesNo(card.fee, 'Fee charged', 'No fee', (v) => `Fee ${v}`)
  if (fee) facts.push(fee)
  if (card.openingHours) facts.push(`Open ${card.openingHours}`)
  return facts
}

/**
 * The address only when it is a web page. The map is edited by anyone, so a
 * website tag can hold javascript: or data: text; only http and https become a
 * link, and anything else is shown as plain text by the caller.
 */
export function safeHref(raw: string): string | null {
  try {
    const url = new URL(raw.trim())
    return url.protocol === 'http:' || url.protocol === 'https:' ? url.href : null
  } catch {
    return null
  }
}

/** A tel: link only for something that is shaped like a phone number. */
export function phoneHref(raw: string): string | null {
  const t = raw.trim()
  return /^\+?[\d\s().-]{5,}$/.test(t) ? `tel:${t.replace(/[\s().-]/g, '')}` : null
}

/** The host and path of a web address, without the scheme: "coralseamarina.com/berthing". */
export function websiteLabel(href: string): string {
  const url = new URL(href)
  const path = url.pathname.replace(/\/+$/, '')
  return `${url.host.replace(/^www\./, '')}${path}`
}

/** The OSM data date as a day: "29 Oct 2026". Empty when the card has none or it cannot be read. */
export function dataDateText(iso: string): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return ''
  return new Intl.DateTimeFormat('en-GB', { day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' }).format(t)
}

export type Bounds = [[number, number], [number, number]]

/** The south-west and north-east corners of a set of shapes, as [lon, lat] for MapLibre. */
export function shapesBounds(shapes: MarinaShape[]): Bounds | null {
  let minLat = Infinity
  let minLon = Infinity
  let maxLat = -Infinity
  let maxLon = -Infinity
  for (const sh of shapes) {
    for (const [lat, lon] of sh.points) {
      minLat = Math.min(minLat, lat)
      maxLat = Math.max(maxLat, lat)
      minLon = Math.min(minLon, lon)
      maxLon = Math.max(maxLon, lon)
    }
  }
  return Number.isFinite(minLat) ? [[minLon, minLat], [maxLon, maxLat]] : null
}

/** The outline shapes and pontoon shapes the map draws: a point has no extent and is drawn as neither. */
function drawnShapes(card: MarinaCard): MarinaShape[] {
  return [...card.geometry.outline, ...card.geometry.pontoons.map((p) => p.shape)].filter((s) => s.kind !== 'point')
}

/** What the map frames: the marina's outline and its pontoons, nothing else. */
export function marinaFrame(card: MarinaCard): Bounds | null {
  return shapesBounds(drawnShapes(card))
}

/**
 * The point half-way along a line's length, as [lat, lon]. Lengths are
 * measured with longitude scaled by the cosine of the mean latitude, as the
 * server does when it places a facility on a line.
 */
export function lineMidpoint(points: [number, number][]): [number, number] {
  const k = Math.cos((points.reduce((sum, p) => sum + p[0], 0) / points.length) * (Math.PI / 180))
  const length = (a: [number, number], b: [number, number]) => Math.hypot(b[0] - a[0], (b[1] - a[1]) * k)
  let half = points.slice(1).reduce((sum, p, i) => sum + length(points[i], p), 0) / 2
  for (let i = 1; i < points.length; i++) {
    const l = length(points[i - 1], points[i])
    if (l >= half && l > 0) {
      const t = half / l
      const [a, b] = [points[i - 1], points[i]]
      return [a[0] + t * (b[0] - a[0]), a[1] + t * (b[1] - a[1])]
    }
    half -= l
  }
  return points[points.length - 1]
}

export interface MarinaFeatureCollections {
  outline: GeoJSON.FeatureCollection
  pontoons: GeoJSON.FeatureCollection
  pontoonLabels: GeoJSON.FeatureCollection
  facilities: GeoJSON.FeatureCollection
  ashore: GeoJSON.FeatureCollection
}

const lonLat = ([lat, lon]: [number, number]): [number, number] => [lon, lat]

/**
 * The marina's outline, pontoons and points as GeoJSON for the map. A point
 * shape has no extent and is not drawn as a line; a closed polygon is drawn
 * by its edge only, so the water and the berths stay visible through it.
 * Pontoons with a name also give a label at the middle of the line (or at
 * the point). The decoder has already checked every shape's point count.
 */
export function marinaFeatures(card: MarinaCard): MarinaFeatureCollections {
  const fc = (features: GeoJSON.Feature[]): GeoJSON.FeatureCollection => ({ type: 'FeatureCollection', features })
  const line = (pts: [number, number][], properties: GeoJSON.GeoJsonProperties): GeoJSON.Feature => ({
    type: 'Feature',
    properties,
    geometry: { type: 'LineString', coordinates: pts.map(lonLat) },
  })
  const outline = card.geometry.outline
    .filter((s) => s.kind !== 'point')
    .map((s) => line(s.points, {}))
  const pontoons = card.geometry.pontoons
    .filter((p) => p.shape.kind !== 'point')
    .map((p) => line(p.shape.points, { name: p.name, private: p.private }))
  const pontoonLabels = card.geometry.pontoons
    .filter((p) => p.name !== '' && !p.private)
    .map((p): GeoJSON.Feature => {
      const mid = p.shape.kind === 'line' ? lineMidpoint(p.shape.points) : p.shape.points[0]
      return { type: 'Feature', properties: { name: p.name }, geometry: { type: 'Point', coordinates: lonLat(mid) } }
    })
  const point = (p: { kind: string; name: string; lat: number; lon: number }): GeoJSON.Feature => ({
    type: 'Feature',
    properties: { kind: p.kind, label: p.name || kindLabel(p.kind) },
    geometry: { type: 'Point', coordinates: [p.lon, p.lat] },
  })
  return {
    outline: fc(outline),
    pontoons: fc(pontoons),
    pontoonLabels: fc(pontoonLabels),
    facilities: fc(card.geometry.facilities.map(point)),
    ashore: fc(card.geometry.ashore.map(point)),
  }
}

/** Whether there is anything to draw. With no outline line or polygon and no pontoon line the card shows no map. */
export function hasMarinaGeometry(card: MarinaCard): boolean {
  return drawnShapes(card).length > 0
}
