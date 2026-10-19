/**
 * Cards under a Mate reply (ADR 0191). Each kind is one member of this union,
 * keyed by `kind`. `error` is not a server kind: it is what the decoder makes
 * of an entry it cannot read, so the thread can say so for that message.
 */
export type MateCard = PassageWindowCard | { kind: 'error'; reason: string; rawKind?: string }

export type PassageState = 'go' | 'marginal' | 'no_go' | 'no_data'
export type PassageFactor = 'wind' | 'gust' | 'wave' | 'wind_against_tide' | 'daylight'
export type PassageSector = 'ahead' | 'abeam' | 'astern'
export type PassageMeasure = 'wind_marginal' | 'gust' | 'wave' | 'wind_against_tide' | 'daylight'

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

/** The passage-window grid (ADR 0191): departure dates down, legs across. */
export interface PassageWindowCard {
  kind: 'passage_window'
  timezone: string
  forecastFetchedAt: string
  provider: string
  waveProvider: string
  legs: { name: string; stayNightsAfter: number | null }[]
  rows: { departDate: string; verdict: PassageState; cells: PassageWindowCell[] }[]
}

const STATES: readonly string[] = ['go', 'marginal', 'no_go', 'no_data']
const FACTORS: readonly string[] = ['wind', 'gust', 'wave', 'wind_against_tide', 'daylight']
const SECTORS: readonly string[] = ['ahead', 'abeam', 'astern']
const MEASURES: readonly string[] = ['wind_marginal', 'gust', 'wave', 'wind_against_tide', 'daylight']

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
    if (kind === 'passage_window') {
      try {
        return decodePassageWindow((entry as { payload?: unknown }).payload)
      } catch (err) {
        if (!(err instanceof CardFieldError)) throw err
        return { kind: 'error', reason: `message card ${i}: ${err.message}`, rawKind: kind }
      }
    }
    return { kind: 'error', reason: `message card ${i}: unknown kind "${kind}"`, rawKind: kind }
  })
}
