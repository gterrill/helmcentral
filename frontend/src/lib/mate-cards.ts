/**
 * Cards under a Mate reply (ADR 0191). Each kind is one member of this union,
 * keyed by `kind`. `error` is not a server kind: it is what the decoder makes
 * of an entry it cannot read, so the thread can say so for that message.
 */
export type MateCard = PassageWindowCard | TelemetryChartCard | { kind: 'error'; reason: string; rawKind?: string }

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
  axis: 'left' | 'right'
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

const STATES: readonly string[] = ['go', 'marginal', 'no_go', 'no_data']
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
function oneOf<T extends string>(v: unknown, allowed: readonly string[], path: string): T {
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
      axis: oneOf<'left' | 'right'>(o.axis, ['left', 'right'], `series[${i}].axis`),
    }
  })
  if (series.length < 1 || series.length > 4) bad('series', 'expected 1 to 4 series')
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
