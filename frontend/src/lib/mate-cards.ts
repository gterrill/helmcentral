import type { DepthTrendData } from '@/hooks/use-depth-trend'
import type { TideToday } from '@/hooks/use-tide-today'

/**
 * Cards under a Mate reply (ADR 0191). Each kind is one member of this union,
 * keyed by `kind`. `error` is not a server kind: it is what the decoder makes
 * of an entry it cannot read, so the thread can say so for that message.
 */
export type MateCard = PassageWindowCard | TelemetryChartCard | WindRoseCard | TileCard | MarinaCard | AnchorageCard | { kind: 'error'; reason: string; rawKind?: string }

export type PassageState = 'go' | 'marginal' | 'no_go' | 'no_data'
export type PassageFactor = 'wind' | 'gust' | 'wave' | 'wind_against_tide' | 'daylight'
export type PassageSector = 'ahead' | 'abeam' | 'astern'
export type PassageMeasure = 'wind_marginal' | 'gust' | 'wave' | 'wave_direction' | 'wind_against_tide' | 'daylight'

export interface PassageWindowCell {
  /** Index into `legs`. */
  leg: number
  /** Local YYYY-MM-DD the leg departs. */
  date: string
  /** RFC3339 with the vessel-local offset. */
  depart: string
  arrive: string
  state: PassageState
  limiting: { factor: PassageFactor; sector: PassageSector | null; text: string } | null
  /** Why a `no_data` cell has no limiting text. */
  note?: string
  notChecked: PassageMeasure[]
}

export type PassageDecisionState = '' | 'went' | 'stayed' | 'just_looking'

/** What was done with a passage plan. `rowDate` is set only for `went`. */
export interface PassageDecision {
  id: number
  state: PassageDecisionState
  rowDate: string | null
  /** True while Helmcentral filled it in; an operator choice makes it false. */
  inferred: boolean
  passageId: string | null
}

/** The passage-window grid (ADR 0191): departure dates down, legs across. */
export interface PassageWindowCard {
  kind: 'passage_window'
  /** The stored card's id; absent on a card saved before decisions were kept. */
  cardId?: number
  /** What the operator did with the plan; absent on a card saved before decisions were kept. */
  decision?: PassageDecision
  timezone: string
  forecastFetchedAt: string
  provider: string
  waveProvider: string
  legs: { name: string; stayNightsAfter: number | null }[]
  rows: { departDate: string; verdict: PassageState; cells: PassageWindowCell[] }[]
}

export interface TelemetryChartSeries {
  path: string
  /** The `$source` the series is restricted to; null for any. */
  source: string | null
  label: string
  /** The operator's display unit; the endpoint converts into it. */
  unit: string
  /** The unit the boat declared for the path when the tool ran; "" if undeclared. */
  siUnit: string
  /** The stacked panel the series is drawn in. One unit to a panel; panels share the time axis. */
  panel: number
}

export interface TelemetryChartRange {
  from: string
  to: string
}

/** A telemetry chart (ADR 0191): the reference only; the card fetches the points. */
export interface TelemetryChartCard {
  kind: 'telemetry_chart'
  title: string
  view: 'time' | 'xy'
  /** Indices into `series`; set only when `view` is "xy". */
  xy: { x: number; y: number } | null
  timezone: string
  series: TelemetryChartSeries[]
  range: TelemetryChartRange
  compare: (TelemetryChartRange & { label: string })[]
}

/**
 * A wind rose (ADR 0221): the logged true wind counted into compass sectors and
 * speed bands. It stores the counts, not a reference to them: the figures are
 * small and the prose beside the card was written about exactly these.
 */
export interface WindRoseCard {
  kind: 'wind_rose'
  title: string
  timezone: string
  range: TelemetryChartRange
  /** Bearings are where the wind blows from, true. */
  reference: 'true'
  sectors: 8 | 16 | 32
  speedUnit: string
  /** Speed bands, lowest first. The last has `to` null, the open top band. */
  bins: { from: number; to: number | null }[]
  /** `counts[sector][band]`. Sector 0 is centred on north, then clockwise. */
  counts: number[][]
  samples: number
  calm: number
  calmBelow: number
  variable: number
  unpaired: number
  bucketMinutes: number
  meanSpeed: number | null
  strongest: { speed: number; at: string } | null
}

/**
 * What the Depth & Tide tile reads, as the server read it when Mate answered:
 * the vessel state's depth and navigation state, the tide service's body and
 * the three hour depth trend. `null` is a reading that was missing then; the
 * tile draws it as a dash, as it does on the dashboard.
 */
export interface DepthTideSnapshot {
  /** Metres below the transducer. */
  depth: number | null
  depthLastUpdateAgeS: number | null
  navigationState: string | null
  tide: TideToday | null
  depthTrend: DepthTrendData
}

/**
 * A dashboard tile in a Mate reply (ADR 0229): the tile's id, when the server
 * read it, and the figures it read. The newest reply draws the live tile; an
 * older one draws the same tile on `snapshot`, so the picture matches the prose.
 */
export type TileCard = { kind: 'tile'; tile: 'depth-tide'; capturedAt: string; snapshot: DepthTideSnapshot }

/** A point, a line or a closed polygon, as [lat, lon] pairs. */
export interface MarinaShape {
  kind: 'point' | 'line' | 'polygon'
  points: [number, number][]
}

export type MarinaLimitStatus = 'ok' | 'exceeds' | 'not charted' | 'particular not set'

/** A place on the marina map: a facility or a service ashore. */
export interface MarinaPoint {
  kind: string
  name: string
  lat: number
  lon: number
}

/**
 * What marina_details found (ADR 0242), drawn for the operator: the marina's
 * record and the map shapes the model never sees. A snapshot, like the passage
 * window: a reopened thread draws it without asking the map server again.
 * Empty strings are fields the map does not record; `dataAsOf` is "" when the
 * map server gave no data date.
 */
export interface MarinaCard {
  kind: 'marina'
  name: string
  osm: string
  lat: number
  lon: number
  berths: number | null
  fee: string
  powerSupply: string
  openingHours: string
  contact: { website: string; phone: string; vhf: string; email: string }
  chartedLimits: { maxDraughtM: number | null; maxLengthM: number | null; depthM: number | null }
  pontoonArms: string[]
  facilities: { kind: string; name: string; count: number; facts: Record<string, string> }[]
  entranceMarks: { kind: string; side: string; colour: string; name: string; count: number }[]
  ashore: { kind: string; name: string; openingHours: string; distanceM: number }[]
  vesselLimitCheck: { particular: 'draft' | 'length'; status: MarinaLimitStatus; boatM: number | null; chartedLimitM: number | null }[]
  dataAsOf: string
  /** The plugin cut a list to its bound, so the card may not show everything mapped. */
  truncated: boolean
  /** The plugin cut or simplified the map's shapes, so the map is not the whole of what is mapped. */
  geometryTruncated: boolean
  geometry: {
    outline: MarinaShape[]
    pontoons: { name: string; private: boolean; shape: MarinaShape }[]
    facilities: MarinaPoint[]
    ashore: MarinaPoint[]
  }
  /** Documents of the operator's tagged with this marina's name, newest first. */
  savedDocuments: { id: string; title: string }[]
}

export type ShelterGrade = 'sheltered' | 'partial' | 'exposed' | 'reef'

export interface AnchorageRestrictedArea {
  osm: string
  name: string
  /** "seamark" is a charted restricted area; "protected_area" a marine park or similar, with no distance. */
  source: 'seamark' | 'protected_area'
  restriction: string
  category: string
  distanceM: number | null
  prohibitsAnchoring: boolean
  /** One shape for each way the area is mapped; empty for a protected area. */
  shapes: MarinaShape[]
}

export interface AnchorageMooring {
  osm: string
  name: string
  ref: string
  /** The vessel size limit in metres, null when the mooring's record gives none. */
  vesselSizeM: number | null
  class: string
  detailsUntagged: boolean
  distanceM: number
  lat: number
  lon: number
}

export interface AnchorageArea {
  osm: string
  name: string
  category: string
  distanceM: number
  shapes: MarinaShape[]
}

export interface AnchorageSeabedArea {
  osm: string
  name: string
  surface: string
  distanceM: number
  shapes: MarinaShape[]
}

/**
 * What anchorage_details found (ADR 0244), drawn for the operator: the shelter
 * rose, the forecast and tide for the stay, and what the map records nearby
 * with the shapes the model never sees. A snapshot like the marina card. A section that
 * failed carries its `error` text and empty lists; the card never fills it.
 */
export interface AnchorageCard {
  kind: 'anchorage'
  name: string
  lat: number
  lon: number
  /** RFC 3339 with the anchorage's offset. */
  arrive: string
  leave: string
  timezone: string
  shelter: {
    summary: string
    insideReef: boolean
    reefName: string
    /** YYYY-MM-DD of the oldest coastline square used. */
    coastlineDownloaded: string
    /** One per 10 degrees, 000 to 350, where the wind comes FROM. */
    bearings: { bearingDeg: number; fetchNm: number; grade: ShelterGrade; open: boolean }[]
    arcs: { grade: ShelterGrade; fromDeg: number; toDeg: number; text: string }[]
  }
  forecast: {
    error: string
    note: string
    /** When the forecast was fetched; "" when the provider did not say. */
    fetchedAt: string
    wavesError: string
    wavesNoData: boolean
    rows: {
      time: string
      windKts: number | null
      gustKts: number | null
      dirDeg: number | null
      dir: string
      shelter: ShelterGrade | ''
      waveM: number | null
      waveS: number | null
      waveDir: string
    }[]
    days: {
      date: string
      windKtsMin: number | null
      windKtsMax: number | null
      gustKtsMax: number | null
      dirPrevailing: string
      shelterPrevailing: ShelterGrade | ''
    }[]
  }
  tide: {
    error: string
    station: string
    distanceNm: number
    timeBasis: string
    heightAtArrivalM: number | null
    direction: string
    heightNote: string
    /** Set when the tide prediction ends before the stay does. */
    note: string
    extremes: { time: string; type: 'high' | 'low'; heightM: number; inWindow: boolean }[]
    rangeM: number | null
    rangeNote: string
  }
  features: {
    error: string
    restrictedAreas: AnchorageRestrictedArea[]
    moorings: AnchorageMooring[]
    anchorageAreas: AnchorageArea[]
    seabedAreas: AnchorageSeabedArea[]
    bay: { osm: string; name: string; contains: boolean; distanceM: number } | null
    truncated: boolean
  }
  /** OpenStreetMap data time, "" when the map server gave none. */
  dataAsOf: string
}

export const STATES: readonly string[] = ['go', 'marginal', 'no_go', 'no_data']
const FACTORS: readonly string[] = ['wind', 'gust', 'wave', 'wind_against_tide', 'daylight']
const SECTORS: readonly string[] = ['ahead', 'abeam', 'astern']
// wind_marginal is only on cards saved by v0.49.0 chats; marginal is worked out now.
const MEASURES: readonly string[] = ['wind_marginal', 'gust', 'wave', 'wave_direction', 'wind_against_tide', 'daylight']

class CardFieldError extends Error {}

function bad(path: string, why: string): never {
  throw new CardFieldError(`${path}: ${why}`)
}
function obj(v: unknown, path: string): Record<string, unknown> {
  if (typeof v !== 'object' || v === null || Array.isArray(v)) bad(path, 'expected an object')
  return v as Record<string, unknown>
}
function arr(v: unknown, path: string): unknown[] {
  if (!Array.isArray(v)) bad(path, 'expected an array')
  return v as unknown[]
}
function str(v: unknown, path: string): string {
  if (typeof v !== 'string') bad(path, 'expected a string')
  return v as string
}
export function oneOf<T extends string>(v: unknown, allowed: readonly string[], path: string): T {
  if (typeof v !== 'string' || !allowed.includes(v)) bad(path, `expected one of ${allowed.join(', ')}`)
  return v as T
}
function stamp(v: unknown, path: string): string {
  const s = str(v, path)
  if (Number.isNaN(Date.parse(s))) bad(path, 'expected a timestamp')
  return s
}
function ymd(v: unknown, path: string): string {
  const s = str(v, path)
  if (!/^\d{4}-\d{2}-\d{2}$/.test(s)) bad(path, 'expected a YYYY-MM-DD date')
  return s
}

function zone(v: unknown, path: string): string {
  const s = str(v, path)
  try {
    new Intl.DateTimeFormat('en-GB', { timeZone: s })
  } catch {
    bad(path, 'expected a time zone name')
  }
  return s
}

/** Reads the server's decision view; throws a field error naming `path`. */
export function decodePassageDecision(raw: unknown, path = 'decision'): PassageDecision {
  const o = obj(raw, path)
  const id = o.id
  if (typeof id !== 'number' || !Number.isInteger(id) || id < 1) bad(`${path}.id`, 'expected a positive integer')
  const state = oneOf<PassageDecisionState>(o.state, ['', 'went', 'stayed', 'just_looking'], `${path}.state`)
  const rowDate = o.row_date === null ? null : ymd(o.row_date, `${path}.row_date`)
  if (typeof o.inferred !== 'boolean') bad(`${path}.inferred`, 'expected true or false')
  if (o.passage_id !== null && typeof o.passage_id !== 'string') bad(`${path}.passage_id`, 'expected a string or null')
  return { id: id as number, state, rowDate, inferred: o.inferred as boolean, passageId: o.passage_id as string | null }
}

function decodePassageWindow(raw: unknown): PassageWindowCard {
  const p = obj(raw, 'payload')
  const legs = arr(p.legs, 'legs').map((l, i) => {
    const o = obj(l, `legs[${i}]`)
    const stay = o.stay_nights_after
    if (stay !== null && (typeof stay !== 'number' || !Number.isFinite(stay))) bad(`legs[${i}].stay_nights_after`, 'expected a number or null')
    return { name: str(o.name, `legs[${i}].name`), stayNightsAfter: stay as number | null }
  })
  const rows = arr(p.rows, 'rows').map((r, ri) => {
    const ro = obj(r, `rows[${ri}]`)
    const cells = arr(ro.cells, `rows[${ri}].cells`).map((c, ci) => {
      const path = `rows[${ri}].cells[${ci}]`
      const co = obj(c, path)
      const leg = co.leg
      if (typeof leg !== 'number' || !Number.isInteger(leg) || leg < 0 || leg >= legs.length) bad(`${path}.leg`, 'expected a leg index')
      let limiting: PassageWindowCell['limiting'] = null
      if (co.limiting !== null) {
        const lo = obj(co.limiting, `${path}.limiting`)
        limiting = {
          factor: oneOf<PassageFactor>(lo.factor, FACTORS, `${path}.limiting.factor`),
          sector: lo.sector === null ? null : oneOf<PassageSector>(lo.sector, SECTORS, `${path}.limiting.sector`),
          text: str(lo.text, `${path}.limiting.text`),
        }
      }
      const notChecked = arr(co.not_checked, `${path}.not_checked`).map((m, mi) =>
        oneOf<PassageMeasure>(m, MEASURES, `${path}.not_checked[${mi}]`),
      )
      const cell: PassageWindowCell = {
        leg: leg as number,
        date: ymd(co.date, `${path}.date`),
        depart: stamp(co.depart, `${path}.depart`),
        arrive: stamp(co.arrive, `${path}.arrive`),
        state: oneOf<PassageState>(co.state, STATES, `${path}.state`),
        limiting,
        notChecked,
      }
      if (co.note !== undefined) cell.note = str(co.note, `${path}.note`)
      return cell
    })
    return {
      departDate: ymd(ro.depart_date, `rows[${ri}].depart_date`),
      verdict: oneOf<PassageState>(ro.verdict, STATES, `rows[${ri}].verdict`),
      cells,
    }
  })
  return {
    kind: 'passage_window',
    timezone: zone(p.timezone, 'timezone'),
    forecastFetchedAt: stamp(p.forecast_fetched_at, 'forecast_fetched_at'),
    provider: str(p.provider, 'provider'),
    waveProvider: str(p.wave_provider, 'wave_provider'),
    legs,
    rows,
  }
}

/**
 * A series' panel. A card saved before panels (ADR 0221) carried a left or right
 * axis instead; those read as panels 0 and 1, which is the stacked layout a
 * two-unit chart gets now.
 */
function panelOf(o: Record<string, unknown>, i: number): number {
  if (o.panel === undefined && o.axis !== undefined) {
    return oneOf<'left' | 'right'>(o.axis, ['left', 'right'], `series[${i}].axis`) === 'left' ? 0 : 1
  }
  const p = o.panel
  if (typeof p !== 'number' || !Number.isInteger(p) || p < 0 || p > 3) bad(`series[${i}].panel`, 'expected a panel number from 0 to 3')
  return p as number
}

function decodeTelemetryChart(raw: unknown): TelemetryChartCard {
  const p = obj(raw, 'payload')
  const series = arr(p.series, 'series').map((s, i): TelemetryChartSeries => {
    const o = obj(s, `series[${i}]`)
    const source = o.source
    if (source !== null && typeof source !== 'string') bad(`series[${i}].source`, 'expected a string or null')
    return {
      path: str(o.path, `series[${i}].path`),
      source: source as string | null,
      label: str(o.label, `series[${i}].label`),
      unit: str(o.unit, `series[${i}].unit`),
      siUnit: str(o.si_unit, `series[${i}].si_unit`),
      panel: panelOf(o, i),
    }
  })
  if (series.length < 1 || series.length > 4) bad('series', 'expected 1 to 4 series')
  series.forEach((s, i) => {
    if (s.panel >= series.length) bad(`series[${i}].panel`, 'expected a panel number below the number of series')
  })
  const view = oneOf<'time' | 'xy'>(p.view, ['time', 'xy'], 'view')
  let xy: TelemetryChartCard['xy'] = null
  if (view === 'xy') {
    if (p.xy === null || p.xy === undefined) bad('xy', 'expected x and y series indices for an xy view')
    const o = obj(p.xy, 'xy')
    const index = (v: unknown, path: string) => {
      if (typeof v !== 'number' || !Number.isInteger(v) || v < 0 || v >= series.length) bad(path, 'expected a series index')
      return v as number
    }
    xy = { x: index(o.x, 'xy.x'), y: index(o.y, 'xy.y') }
    if (xy.x === xy.y) bad('xy', 'expected two different series')
  } else if (p.xy !== null && p.xy !== undefined) {
    bad('xy', 'expected null for a time view')
  }
  const range = (v: unknown, path: string): TelemetryChartRange => {
    const o = obj(v, path)
    return { from: stamp(o.from, `${path}.from`), to: stamp(o.to, `${path}.to`) }
  }
  const compare = arr(p.compare, 'compare').map((c, i) => ({
    label: str(obj(c, `compare[${i}]`).label, `compare[${i}].label`),
    ...range(c, `compare[${i}]`),
  }))
  if (compare.length > 3) bad('compare', 'expected at most 3 ranges')
  return {
    kind: 'telemetry_chart',
    title: str(p.title, 'title'),
    view,
    xy,
    timezone: zone(p.timezone, 'timezone'),
    series,
    range: range(p.range, 'range'),
    compare,
  }
}

function nonNegInt(v: unknown, path: string): number {
  if (typeof v !== 'number' || !Number.isInteger(v) || v < 0) bad(path, 'expected a non-negative whole number')
  return v as number
}
function finite(v: unknown, path: string): number {
  if (typeof v !== 'number' || !Number.isFinite(v)) bad(path, 'expected a number')
  return v as number
}

function decodeWindRose(raw: unknown): WindRoseCard {
  const p = obj(raw, 'payload')
  const sectors = p.sectors
  if (sectors !== 8 && sectors !== 16 && sectors !== 32) bad('sectors', 'expected 8, 16 or 32')
  const bins = arr(p.bins, 'bins').map((b, i) => {
    const o = obj(b, `bins[${i}]`)
    const to = o.to === null ? null : finite(o.to, `bins[${i}].to`)
    return { from: finite(o.from, `bins[${i}].from`), to }
  })
  if (bins.length < 2) bad('bins', 'expected at least two speed bands')
  bins.forEach((b, i) => {
    if (i === 0 && b.from !== 0) bad('bins[0].from', 'expected the first band to start at 0')
    if (i > 0 && b.from !== bins[i - 1].to) bad(`bins[${i}]`, 'expected each band to start where the last ended')
    if (i < bins.length - 1 && (b.to === null || b.to <= b.from)) bad(`bins[${i}].to`, 'expected an upper edge above the lower')
  })
  if (bins[bins.length - 1].to !== null) bad('bins', 'expected the top band to be open-ended')
  const counts = arr(p.counts, 'counts')
  if (counts.length !== sectors) bad('counts', `expected ${sectors} rows, one for each sector`)
  const grid = counts.map((row, s) => {
    const r = arr(row, `counts[${s}]`)
    if (r.length !== bins.length) bad(`counts[${s}]`, `expected ${bins.length} counts, one for each speed band`)
    return r.map((n, b) => nonNegInt(n, `counts[${s}][${b}]`))
  })
  const samples = nonNegInt(p.samples, 'samples')
  const calm = nonNegInt(p.calm, 'calm')
  const variable = nonNegInt(p.variable, 'variable')
  const binned = grid.reduce((n, row) => n + row.reduce((a, b) => a + b, 0), 0)
  if (binned + calm + variable !== samples) bad('samples', 'expected the sectors, calm and variable to add up to the samples')
  const range = obj(p.range, 'range')
  const strongest = p.strongest === null ? null : (() => {
    const o = obj(p.strongest, 'strongest')
    return { speed: finite(o.speed, 'strongest.speed'), at: stamp(o.at, 'strongest.at') }
  })()
  return {
    kind: 'wind_rose',
    title: str(p.title, 'title'),
    timezone: zone(p.timezone, 'timezone'),
    range: { from: stamp(range.from, 'range.from'), to: stamp(range.to, 'range.to') },
    reference: oneOf<'true'>(p.reference, ['true'], 'reference'),
    sectors,
    speedUnit: str(p.speed_unit, 'speed_unit'),
    bins,
    counts: grid,
    samples,
    calm,
    calmBelow: finite(p.calm_below, 'calm_below'),
    variable,
    unpaired: nonNegInt(p.unpaired, 'unpaired'),
    bucketMinutes: nonNegInt(p.bucket_minutes, 'bucket_minutes'),
    meanSpeed: p.mean_speed === null ? null : finite(p.mean_speed, 'mean_speed'),
    strongest,
  }
}

function bool(v: unknown, path: string): boolean {
  if (typeof v !== 'boolean') bad(path, 'expected true or false')
  return v as boolean
}
/** A position, which MapLibre throws on outside the globe; `at` prefixes the field names. */
function position(lat: unknown, lon: unknown, at: string): { lat: number; lon: number } {
  const la = finite(lat, `${at}lat`)
  const lo = finite(lon, `${at}lon`)
  if (la < -90 || la > 90) bad(`${at}lat`, 'expected a latitude from -90 to 90')
  if (lo < -180 || lo > 180) bad(`${at}lon`, 'expected a longitude from -180 to 180')
  return { lat: la, lon: lo }
}

const SHAPE_MIN_POINTS = { point: 1, line: 2, polygon: 4 } as const

function decodeMarinaShape(raw: unknown, path: string): MarinaShape {
  const o = obj(raw, path)
  const kind = oneOf<MarinaShape['kind']>(o.kind, Object.keys(SHAPE_MIN_POINTS), `${path}.kind`)
  const points = arr(o.points, `${path}.points`).map((p, i): [number, number] => {
    const pair = arr(p, `${path}.points[${i}]`)
    if (pair.length !== 2) bad(`${path}.points[${i}]`, 'expected a [lat, lon] pair')
    const { lat, lon } = position(pair[0], pair[1], `${path}.points[${i}].`)
    return [lat, lon]
  })
  if (kind === 'point' ? points.length !== 1 : points.length < SHAPE_MIN_POINTS[kind]) {
    bad(`${path}.points`, `expected ${kind === 'point' ? 'one position' : `at least ${SHAPE_MIN_POINTS[kind]} positions`} for a ${kind}`)
  }
  return { kind, points }
}

function decodeMarinaPoint(raw: unknown, path: string): MarinaPoint {
  const o = obj(raw, path)
  return { kind: str(o.kind, `${path}.kind`), name: str(o.name, `${path}.name`), ...position(o.lat, o.lon, `${path}.`) }
}

function decodeMarinaFacility(raw: unknown, path: string): MarinaCard['facilities'][number] {
  const o = obj(raw, path)
  const facts: Record<string, string> = {}
  for (const [k, v] of Object.entries(obj(o.facts, `${path}.facts`))) facts[k] = str(v, `${path}.facts.${k}`)
  return { kind: str(o.kind, `${path}.kind`), name: str(o.name, `${path}.name`), count: nonNegInt(o.count, `${path}.count`), facts }
}

function decodeMarinaMark(raw: unknown, path: string): MarinaCard['entranceMarks'][number] {
  const o = obj(raw, path)
  return {
    kind: str(o.kind, `${path}.kind`),
    side: str(o.side, `${path}.side`),
    colour: str(o.colour, `${path}.colour`),
    name: str(o.name, `${path}.name`),
    count: nonNegInt(o.count, `${path}.count`),
  }
}

function decodeMarinaAshore(raw: unknown, path: string): MarinaCard['ashore'][number] {
  const o = obj(raw, path)
  return {
    kind: str(o.kind, `${path}.kind`),
    name: str(o.name, `${path}.name`),
    openingHours: str(o.opening_hours, `${path}.opening_hours`),
    distanceM: nonNegInt(o.distance_m, `${path}.distance_m`),
  }
}

function decodeMarinaLimitCheck(raw: unknown, path: string): MarinaCard['vesselLimitCheck'][number] {
  const o = obj(raw, path)
  return {
    particular: oneOf<'draft' | 'length'>(o.particular, ['draft', 'length'], `${path}.particular`),
    status: oneOf<MarinaLimitStatus>(o.status, ['ok', 'exceeds', 'not charted', 'particular not set'], `${path}.status`),
    boatM: nullableNumber(o.boat_m, `${path}.boat_m`, 0),
    chartedLimitM: nullableNumber(o.charted_limit_m, `${path}.charted_limit_m`, 0),
  }
}

function decodeMarina(raw: unknown): MarinaCard {
  const p = obj(raw, 'payload')
  const contact = obj(p.contact, 'contact')
  const limits = obj(p.charted_limits, 'charted_limits')
  const geometry = obj(p.geometry, 'geometry')
  return {
    kind: 'marina',
    name: str(p.name, 'name'),
    osm: str(p.osm, 'osm'),
    ...position(p.lat, p.lon, ''),
    berths: p.berths === null ? null : nonNegInt(p.berths, 'berths'),
    fee: str(p.fee, 'fee'),
    powerSupply: str(p.power_supply, 'power_supply'),
    openingHours: str(p.opening_hours, 'opening_hours'),
    contact: {
      website: str(contact.website, 'contact.website'),
      phone: str(contact.phone, 'contact.phone'),
      vhf: str(contact.vhf, 'contact.vhf'),
      email: str(contact.email, 'contact.email'),
    },
    chartedLimits: {
      maxDraughtM: nullableNumber(limits.max_draught_m, 'charted_limits.max_draught_m', 0),
      maxLengthM: nullableNumber(limits.max_length_m, 'charted_limits.max_length_m', 0),
      depthM: nullableNumber(limits.depth_m, 'charted_limits.depth_m', 0),
    },
    pontoonArms: arr(p.pontoon_arms, 'pontoon_arms').map((a, i) => str(a, `pontoon_arms[${i}]`)),
    facilities: arr(p.facilities, 'facilities').map((f, i) => decodeMarinaFacility(f, `facilities[${i}]`)),
    entranceMarks: arr(p.entrance_marks, 'entrance_marks').map((m, i) => decodeMarinaMark(m, `entrance_marks[${i}]`)),
    ashore: arr(p.ashore, 'ashore').map((a, i) => decodeMarinaAshore(a, `ashore[${i}]`)),
    vesselLimitCheck: arr(p.vessel_limit_check, 'vessel_limit_check').map((c, i) => decodeMarinaLimitCheck(c, `vessel_limit_check[${i}]`)),
    dataAsOf: str(p.data_as_of, 'data_as_of'),
    truncated: bool(p.truncated, 'truncated'),
    geometryTruncated: bool(p.geometry_truncated, 'geometry_truncated'),
    geometry: {
      outline: arr(geometry.outline, 'geometry.outline').map((s, i) => decodeMarinaShape(s, `geometry.outline[${i}]`)),
      pontoons: arr(geometry.pontoons, 'geometry.pontoons').map((pt, i) => {
        const path = `geometry.pontoons[${i}]`
        const o = obj(pt, path)
        return { name: str(o.name, `${path}.name`), private: bool(o.private, `${path}.private`), shape: decodeMarinaShape(o.shape, `${path}.shape`) }
      }),
      facilities: arr(geometry.facilities, 'geometry.facilities').map((f, i) => decodeMarinaPoint(f, `geometry.facilities[${i}]`)),
      ashore: arr(geometry.ashore, 'geometry.ashore').map((a, i) => decodeMarinaPoint(a, `geometry.ashore[${i}]`)),
    },
    savedDocuments: arr(p.saved_documents, 'saved_documents').map((d, i) => {
      const o = obj(d, `saved_documents[${i}]`)
      const id = str(o.id, `saved_documents[${i}].id`)
      if (id === '') bad(`saved_documents[${i}].id`, 'expected a document id')
      return { id, title: str(o.title, `saved_documents[${i}].title`) }
    }),
  }
}

const GRADES: readonly string[] = ['sheltered', 'partial', 'exposed', 'reef']

function nullableInt(v: unknown, path: string): number | null {
  return v === null ? null : nonNegInt(v, path)
}

function decodeAnchorageShelter(raw: unknown): AnchorageCard['shelter'] {
  const o = obj(raw, 'shelter')
  const bearings = arr(o.bearings, 'shelter.bearings').map((b, i) => {
    const p = `shelter.bearings[${i}]`
    const r = obj(b, p)
    const bearingDeg = nonNegInt(r.bearing_deg, `${p}.bearing_deg`)
    if (bearingDeg !== i * 10) bad(`${p}.bearing_deg`, `expected ${i * 10}, the bearings run 000 to 350 in tens`)
    return { bearingDeg, fetchNm: finite(r.fetch_nm, `${p}.fetch_nm`), grade: oneOf<ShelterGrade>(r.grade, GRADES, `${p}.grade`), open: bool(r.open, `${p}.open`) }
  })
  if (bearings.length !== 36) bad('shelter.bearings', 'expected 36 bearings, one for every 10 degrees')
  return {
    summary: str(o.summary, 'shelter.summary'),
    insideReef: bool(o.inside_reef, 'shelter.inside_reef'),
    reefName: str(o.reef_name, 'shelter.reef_name'),
    coastlineDownloaded: str(o.coastline_downloaded, 'shelter.coastline_downloaded'),
    bearings,
    arcs: arr(o.arcs, 'shelter.arcs').map((a, i) => {
      const p = `shelter.arcs[${i}]`
      const r = obj(a, p)
      return {
        grade: oneOf<ShelterGrade>(r.grade, GRADES, `${p}.grade`),
        fromDeg: nonNegInt(r.from_deg, `${p}.from_deg`),
        toDeg: nonNegInt(r.to_deg, `${p}.to_deg`),
        text: str(r.text, `${p}.text`),
      }
    }),
  }
}

function decodeAnchorageForecast(raw: unknown): AnchorageCard['forecast'] {
  const o = obj(raw, 'forecast')
  const fetchedAt = str(o.fetched_at, 'forecast.fetched_at')
  if (fetchedAt !== '') stamp(fetchedAt, 'forecast.fetched_at')
  const grade = (v: unknown, path: string) => oneOf<ShelterGrade | ''>(v, ['', ...GRADES], path)
  return {
    error: str(o.error, 'forecast.error'),
    note: str(o.note, 'forecast.note'),
    fetchedAt,
    wavesError: str(o.waves_error, 'forecast.waves_error'),
    wavesNoData: bool(o.waves_no_data, 'forecast.waves_no_data'),
    rows: arr(o.rows, 'forecast.rows').map((r, i) => {
      const p = `forecast.rows[${i}]`
      const x = obj(r, p)
      return {
        time: stamp(x.time, `${p}.time`),
        windKts: nullableNumber(x.wind_kts, `${p}.wind_kts`, 0),
        gustKts: nullableNumber(x.gust_kts, `${p}.gust_kts`, 0),
        dirDeg: nullableNumber(x.dir_deg, `${p}.dir_deg`, 0),
        dir: str(x.dir, `${p}.dir`),
        shelter: grade(x.shelter, `${p}.shelter`),
        waveM: nullableNumber(x.wave_m, `${p}.wave_m`, 0),
        waveS: nullableNumber(x.wave_s, `${p}.wave_s`, 0),
        waveDir: str(x.wave_dir, `${p}.wave_dir`),
      }
    }),
    days: arr(o.days, 'forecast.days').map((d, i) => {
      const p = `forecast.days[${i}]`
      const x = obj(d, p)
      return {
        date: ymd(x.date, `${p}.date`),
        windKtsMin: nullableNumber(x.wind_kts_min, `${p}.wind_kts_min`, 0),
        windKtsMax: nullableNumber(x.wind_kts_max, `${p}.wind_kts_max`, 0),
        gustKtsMax: nullableNumber(x.gust_kts_max, `${p}.gust_kts_max`, 0),
        dirPrevailing: str(x.dir_prevailing, `${p}.dir_prevailing`),
        shelterPrevailing: grade(x.shelter_prevailing, `${p}.shelter_prevailing`),
      }
    }),
  }
}

function decodeAnchorageTide(raw: unknown): AnchorageCard['tide'] {
  const o = obj(raw, 'tide')
  return {
    error: str(o.error, 'tide.error'),
    station: str(o.station, 'tide.station'),
    distanceNm: finite(o.distance_nm, 'tide.distance_nm'),
    timeBasis: str(o.time_basis, 'tide.time_basis'),
    heightAtArrivalM: nullableNumber(o.height_at_arrival_m, 'tide.height_at_arrival_m'),
    direction: str(o.direction, 'tide.direction'),
    heightNote: str(o.height_note, 'tide.height_note'),
    note: str(o.note, 'tide.note'),
    extremes: arr(o.extremes, 'tide.extremes').map((e, i) => {
      const p = `tide.extremes[${i}]`
      const x = obj(e, p)
      return {
        time: stamp(x.time, `${p}.time`),
        type: oneOf<'high' | 'low'>(x.type, ['high', 'low'], `${p}.type`),
        heightM: finite(x.height_m, `${p}.height_m`),
        inWindow: bool(x.in_window, `${p}.in_window`),
      }
    }),
    rangeM: nullableNumber(o.range_m, 'tide.range_m', 0),
    rangeNote: str(o.range_note, 'tide.range_note'),
  }
}

function decodeShapes(raw: unknown, path: string): MarinaShape[] {
  return arr(raw, path).map((s, i) => decodeMarinaShape(s, `${path}[${i}]`))
}

function decodeAnchorageFeatures(raw: unknown): AnchorageCard['features'] {
  const o = obj(raw, 'features')
  const bay = o.bay === null ? null : (() => {
    const b = obj(o.bay, 'features.bay')
    return { osm: str(b.osm, 'features.bay.osm'), name: str(b.name, 'features.bay.name'), contains: bool(b.contains, 'features.bay.contains'), distanceM: nonNegInt(b.distance_m, 'features.bay.distance_m') }
  })()
  return {
    error: str(o.error, 'features.error'),
    restrictedAreas: arr(o.restricted_areas, 'features.restricted_areas').map((r, i) => {
      const p = `features.restricted_areas[${i}]`
      const x = obj(r, p)
      return {
        osm: str(x.osm, `${p}.osm`),
        name: str(x.name, `${p}.name`),
        source: oneOf<'seamark' | 'protected_area'>(x.source, ['seamark', 'protected_area'], `${p}.source`),
        restriction: str(x.restriction, `${p}.restriction`),
        category: str(x.category, `${p}.category`),
        distanceM: nullableInt(x.distance_m, `${p}.distance_m`),
        prohibitsAnchoring: bool(x.prohibits_anchoring, `${p}.prohibits_anchoring`),
        shapes: decodeShapes(x.shapes, `${p}.shapes`),
      }
    }),
    moorings: arr(o.moorings, 'features.moorings').map((m, i) => {
      const p = `features.moorings[${i}]`
      const x = obj(m, p)
      return {
        osm: str(x.osm, `${p}.osm`),
        name: str(x.name, `${p}.name`),
        ref: str(x.ref, `${p}.ref`),
        vesselSizeM: nullableNumber(x.vessel_size_m, `${p}.vessel_size_m`, 0),
        class: str(x.class, `${p}.class`),
        detailsUntagged: bool(x.details_untagged, `${p}.details_untagged`),
        distanceM: nonNegInt(x.distance_m, `${p}.distance_m`),
        ...position(x.lat, x.lon, `${p}.`),
      }
    }),
    anchorageAreas: arr(o.anchorage_areas, 'features.anchorage_areas').map((a, i) => {
      const p = `features.anchorage_areas[${i}]`
      const x = obj(a, p)
      return { osm: str(x.osm, `${p}.osm`), name: str(x.name, `${p}.name`), category: str(x.category, `${p}.category`), distanceM: nonNegInt(x.distance_m, `${p}.distance_m`), shapes: decodeShapes(x.shapes, `${p}.shapes`) }
    }),
    seabedAreas: arr(o.seabed_areas, 'features.seabed_areas').map((a, i) => {
      const p = `features.seabed_areas[${i}]`
      const x = obj(a, p)
      return { osm: str(x.osm, `${p}.osm`), name: str(x.name, `${p}.name`), surface: str(x.surface, `${p}.surface`), distanceM: nonNegInt(x.distance_m, `${p}.distance_m`), shapes: decodeShapes(x.shapes, `${p}.shapes`) }
    }),
    bay,
    truncated: bool(o.truncated, 'features.truncated'),
  }
}

function decodeAnchorage(raw: unknown): AnchorageCard {
  const p = obj(raw, 'payload')
  const dataAsOf = str(p.data_as_of, 'data_as_of')
  if (dataAsOf !== '') stamp(dataAsOf, 'data_as_of')
  return {
    kind: 'anchorage',
    name: str(p.name, 'name'),
    ...position(p.lat, p.lon, ''),
    arrive: stamp(p.arrive, 'arrive'),
    leave: stamp(p.leave, 'leave'),
    timezone: zone(p.timezone, 'timezone'),
    shelter: decodeAnchorageShelter(p.shelter),
    forecast: decodeAnchorageForecast(p.forecast),
    tide: decodeAnchorageTide(p.tide),
    features: decodeAnchorageFeatures(p.features),
    dataAsOf,
  }
}


function nullableNumber(v: unknown, path: string, min?: number): number | null {
  if (v === null) return null
  const n = finite(v, path)
  if (min !== undefined && n < min) bad(path, `expected ${min} or more`)
  return n
}

function decodeTide(raw: unknown): TideToday | null {
  if (raw === null) return null
  const o = obj(raw, 'snapshot.tide')
  const optionalText = (v: unknown, path: string) => (v === undefined ? '' : str(v, path))
  const time = (v: unknown, path: string) => {
    const s = str(v, path)
    return s === '' ? s : stamp(s, path)
  }
  return {
    datetime: stamp(o.datetime, 'snapshot.tide.datetime'),
    current_tide_height_ft: finite(o.current_tide_height_ft, 'snapshot.tide.current_tide_height_ft'),
    tide_direction: str(o.tide_direction, 'snapshot.tide.tide_direction'),
    high_tide_time: time(o.high_tide_time, 'snapshot.tide.high_tide_time'),
    high_tide_height_ft: finite(o.high_tide_height_ft, 'snapshot.tide.high_tide_height_ft'),
    low_tide_time: time(o.low_tide_time, 'snapshot.tide.low_tide_time'),
    low_tide_height_ft: finite(o.low_tide_height_ft, 'snapshot.tide.low_tide_height_ft'),
    station_name: optionalText(o.station_name, 'snapshot.tide.station_name'),
    provider: optionalText(o.provider, 'snapshot.tide.provider'),
  }
}

function decodeDepthTrend(raw: unknown): DepthTrendData {
  const o = obj(raw, 'snapshot.depth_trend')
  const points = arr(o.points, 'snapshot.depth_trend.points').map((p, i) => {
    const po = obj(p, `snapshot.depth_trend.points[${i}]`)
    return { time: stamp(po.time, `snapshot.depth_trend.points[${i}].time`), depth_m: finite(po.depth_m, `snapshot.depth_trend.points[${i}].depth_m`) }
  })
  const since = oneOf<DepthTrendData['since']>(o.since, ['tide', 'window'], 'snapshot.depth_trend.since')
  const trend: DepthTrendData = { points, since }
  if (o.tide_type !== undefined) trend.tideType = oneOf<NonNullable<DepthTrendData['tideType']>>(o.tide_type, ['high', 'low'], 'snapshot.depth_trend.tide_type')
  if (o.tide_depth_m !== undefined) trend.tideDepthM = finite(o.tide_depth_m, 'snapshot.depth_trend.tide_depth_m')
  return trend
}

function decodeDepthTideSnapshot(raw: unknown): DepthTideSnapshot {
  const o = obj(raw, 'snapshot')
  if (o.navigation_state !== null && typeof o.navigation_state !== 'string') bad('snapshot.navigation_state', 'expected a string or null')
  return {
    depth: nullableNumber(o.depth_m, 'snapshot.depth_m', 0),
    depthLastUpdateAgeS: nullableNumber(o.depth_age_s, 'snapshot.depth_age_s', 0),
    navigationState: o.navigation_state as string | null,
    tide: decodeTide(o.tide),
    depthTrend: decodeDepthTrend(o.depth_trend),
  }
}

// One row per tile Mate can show. Each decoder reads the whole payload and
// returns its own card, so a new tile is a new member of TileCard and a new row.
const TILE_DECODERS: { [T in TileCard['tile']]: (p: Record<string, unknown>) => Extract<TileCard, { tile: T }> } = {
  'depth-tide': (p) => ({
    kind: 'tile',
    tile: 'depth-tide',
    capturedAt: stamp(p.captured_at, 'captured_at'),
    snapshot: decodeDepthTideSnapshot(p.snapshot),
  }),
}

function decodeTile(raw: unknown): TileCard {
  const p = obj(raw, 'payload')
  const tile = oneOf<TileCard['tile']>(p.tile, Object.keys(TILE_DECODERS), 'tile')
  return TILE_DECODERS[tile](p)
}

// `entry` is the whole stored card: a passage window also reads the `id` and
// `decision` that sit beside its payload (ADR 0211). Both are optional.
const DECODERS: Record<string, (payload: unknown, entry: Record<string, unknown>) => MateCard> = {
  passage_window: (payload, entry) => {
    const card = decodePassageWindow(payload)
    if (entry.id !== undefined) {
      if (typeof entry.id !== 'number' || !Number.isInteger(entry.id) || entry.id < 1) bad('id', 'expected a positive integer')
      card.cardId = entry.id as number
    }
    if (entry.decision !== undefined && entry.decision !== null) {
      if (card.cardId === undefined) bad('id', 'expected the card id beside its decision')
      const decision = decodePassageDecision(entry.decision)
      if (decision.state === 'went' && (decision.rowDate === null || !card.rows.some((r) => r.departDate === decision.rowDate))) {
        bad('decision.row_date', 'expected one of the card\'s departure dates for a went decision')
      }
      card.decision = decision
    }
    return card
  },
  telemetry_chart: (payload) => decodeTelemetryChart(payload),
  wind_rose: (payload) => decodeWindRose(payload),
  tile: (payload) => decodeTile(payload),
  marina: (payload) => decodeMarina(payload),
  anchorage: (payload) => decodeAnchorage(payload),
}

/**
 * Decode the `cards` field of a stored message. An absent field is no cards.
 * An entry this build cannot read (no kind, or a kind it does not know) becomes
 * an `error` entry in its place, so one bad card never blanks the thread and is
 * never silently dropped. A non-array value yields a single `error` entry.
 */
export function decodeMateCards(raw: unknown): MateCard[] {
  if (raw === undefined) return []
  if (!Array.isArray(raw)) return [{ kind: 'error', reason: 'message cards: expected an array' }]
  return raw.map((entry, i): MateCard => {
    const kind = typeof entry === 'object' && entry !== null ? (entry as { kind?: unknown }).kind : undefined
    if (typeof kind !== 'string') return { kind: 'error', reason: `message card ${i}: missing kind` }
    const decode = Object.prototype.hasOwnProperty.call(DECODERS, kind) ? DECODERS[kind] : undefined
    if (decode) {
      try {
        return decode((entry as { payload?: unknown }).payload, entry as Record<string, unknown>)
      } catch (err) {
        if (!(err instanceof CardFieldError)) throw err
        return { kind: 'error', reason: `message card ${i}: ${err.message}`, rawKind: kind }
      }
    }
    return { kind: 'error', reason: `message card ${i}: unknown kind "${kind}"`, rawKind: kind }
  })
}
