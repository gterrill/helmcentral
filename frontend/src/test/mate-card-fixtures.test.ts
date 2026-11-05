import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

import { decodeMateCards, MATE_CARD_KINDS } from '@/lib/mate-cards'

// backend/testdata/mate_cards holds one stored card per kind, written by the
// Go card builders (`go test -run Golden -update`). Decoding all of them is
// what stops the two sides drifting apart (ADR 0261).
const DIR = join(__dirname, '../../../backend/testdata/mate_cards')
const fixtures = readdirSync(DIR)
  .filter((f) => f.endsWith('.json'))
  .sort()
  .map((file) => ({ file, stored: JSON.parse(readFileSync(join(DIR, file), 'utf8')) as { kind: string; payload: unknown } }))

type Json = null | boolean | number | string | Json[] | { [key: string]: Json }

/** Wraps a value so every key the decoder reads is recorded under a path where
 * array elements share one `[]` step. Reading a key counts; ignoring it does not. */
function recordReads(value: unknown, path: string, read: Set<string>): unknown {
  if (typeof value !== 'object' || value === null) return value
  const isArray = Array.isArray(value)
  const step = (key: string) => (isArray ? (/^\d+$/.test(key) ? `${path}[]` : null) : `${path}.${key}`)
  return new Proxy(value, {
    get(target, key, receiver) {
      const child = typeof key === 'string' ? step(key) : null
      if (child === null) return Reflect.get(target, key, receiver)
      read.add(child)
      return recordReads(Reflect.get(target, key, receiver), child, read)
    },
    has(target, key) {
      const child = typeof key === 'string' ? step(key) : null
      if (child !== null) read.add(child)
      return Reflect.has(target, key)
    },
  })
}

function pathsOf(value: Json, path: string, out: Set<string>) {
  if (Array.isArray(value)) {
    for (const v of value) {
      out.add(`${path}[]`)
      pathsOf(v, `${path}[]`, out)
    }
  } else if (typeof value === 'object' && value !== null) {
    for (const [k, v] of Object.entries(value)) {
      out.add(`${path}.${k}`)
      pathsOf(v, `${path}.${k}`, out)
    }
  }
}

/** Keys the browser deliberately drops from a stored card. Each is a boat-time
 * label Go writes for Mate to quote, beside the RFC 3339 instant the card reads
 * (anchorage_depth.go), or a flag the card derives from another field. */
const UNREAD_ON_PURPOSE = new Set([
  '[].payload.depth.known', // the card derives it from depth_m
  '[].payload.depth.clearance.lowest_water_at',
  '[].payload.depth.clearance.tide_checked_until',
  '[].payload.depth.rode.highest_water_at',
  '[].payload.depth.rode.tide_checked_until',
  '[].payload.depth.rode.wind_checked_until',
  '[].payload.depth.swing.tide_checked_until',
])

function unreadKeys(file: string, stored: { kind: string; payload: unknown }): string[] {
  const read = new Set<string>()
  const [card] = decodeMateCards(recordReads(JSON.parse(JSON.stringify([stored])), '', read))
  if (card.kind === 'error') throw new Error(`${file} did not decode: ${card.reason}`)
  const all = new Set<string>()
  pathsOf([stored] as unknown as Json, '', all)
  return [...all].filter((p) => !read.has(p) && !UNREAD_ON_PURPOSE.has(p)).sort()
}

describe('the Go golden card fixtures', () => {
  it.each(fixtures)('$file decodes to a card of its own kind', ({ stored }) => {
    const [card] = decodeMateCards([stored])
    expect(card, JSON.stringify(card)).toMatchObject({ kind: stored.kind })
  })

  // The decoders read named keys and ignore the rest, so a field Go adds or
  // renames would vanish without a word. Every key in a fixture must be read.
  it.each(fixtures)('$file has no key its decoder leaves unread', ({ file, stored }) => {
    expect(unreadKeys(file, stored)).toEqual([])
  })

  it('has a fixture for every kind the browser decodes', () => {
    const have = new Set(fixtures.map((f) => f.stored.kind))
    expect(MATE_CARD_KINDS.filter((k) => !have.has(k))).toEqual([])
  })

  it('has no fixture for a kind the browser does not decode', () => {
    const known = new Set<string>(MATE_CARD_KINDS)
    expect(fixtures.filter((f) => !known.has(f.stored.kind)).map((f) => f.file)).toEqual([])
  })
})

// The decoded values, not just the kind: a field renamed on the Go side whose
// decoder reads it as optional would decode to a default and pass the checks above.
describe('what the decoders make of the Go golden cards', () => {
  const card = (file: string) => {
    const f = fixtures.find((x) => x.file === file)
    if (!f) throw new Error(`no fixture ${file}`)
    const [c] = decodeMateCards([f.stored])
    return c as any // eslint-disable-line @typescript-eslint/no-explicit-any -- each test narrows by the file it names
  }

  it('passage_window_itinerary: every verdict, limit, sector, stay and note', () => {
    const c = card('passage_window_itinerary.json')
    expect(c.legs).toEqual([
      { name: 'Alpha → Bravo', stayNightsAfter: 1 },
      { name: 'Bravo → Charlie', stayNightsAfter: null },
    ])
    expect(c.rows.map((r: { verdict: string }) => r.verdict)).toEqual(['marginal', 'no_go', 'no_go', 'no_go', 'no_go', 'no_go', 'no_data'])
    const cells = c.rows.flatMap((r: { cells: unknown[] }) => r.cells) as { state: string; limiting: { factor: string; sector: string | null } | null; note?: string; notChecked: string[] }[]
    const limits = new Set(cells.map((x) => (x.limiting ? `${x.limiting.factor}/${x.limiting.sector}` : 'none')))
    expect([...limits].sort()).toEqual(['gust/astern', 'none', 'wave/ahead', 'wind/abeam', 'wind/ahead', 'wind/astern'])
    expect(new Set(cells.map((x) => x.state))).toEqual(new Set(['go', 'marginal', 'no_go', 'no_data']))
    expect(cells.find((x) => x.state === 'no_data')?.note).toBe('No forecast this far ahead')
    expect(cells.some((x) => x.notChecked.includes('wave'))).toBe(true)
    expect(cells.some((x) => x.notChecked.includes('wind_against_tide'))).toBe(true)
  })

  it('passage_window_daylight: a daylight limit has no sector', () => {
    const cell = card('passage_window_daylight.json').rows[0].cells[0]
    expect(cell.limiting.factor).toBe('daylight')
    expect(cell.limiting.sector).toBeNull()
  })

  it('passage_window_decided_*: the id and the decision beside the payload', () => {
    const op = card('passage_window_decided_operator.json')
    expect(op.cardId).toBe(1)
    expect(op.decision).toEqual({ id: 1, state: 'went', rowDate: '2026-10-21', inferred: false, passageId: null })
    const inferred = card('passage_window_decided_inferred.json')
    expect(inferred.decision).toEqual({ id: 1, state: 'went', rowDate: '2026-10-20', inferred: true, passageId: 'passage-1' })
  })

  it('telemetry_chart: a time view with a comparison, and an xy view with a named source', () => {
    const time = card('telemetry_chart_house_soc.json')
    expect(time).toMatchObject({ view: 'time', xy: null, title: 'House bank state of charge' })
    expect(time.compare).toHaveLength(1)
    expect(time.compare[0].label).toBe('Later')
    const xy = card('telemetry_chart_xy.json')
    expect(xy.view).toBe('xy')
    expect(xy.xy).toEqual({ x: 0, y: 1 })
    expect(xy.series.map((s: { source: string | null; panel: number }) => [s.source, s.panel])).toEqual([[null, 0], ['alt-a', 1]])
  })

  it('wind_rose: sectors, tally and the strongest gust, present and absent', () => {
    const rose = card('wind_rose_eight_sectors.json')
    expect(rose).toMatchObject({ sectors: 8, samples: 6, calm: 1, variable: 0, speedUnit: 'kn' })
    expect(rose.strongest.speed).toBeCloseTo(33.89, 1)
    const none = card('wind_rose_no_gust.json')
    expect(none.sectors).toBe(16)
    expect(none.strongest).toBeNull()
  })

  it('tile: a full depth and tide snapshot, and one with both readings missing', () => {
    const full = card('tile_depth_tide.json')
    expect(full.tile).toBe('depth-tide')
    expect(full.snapshot).toMatchObject({ depth: 3.3, navigationState: 'anchored' })
    expect(full.snapshot.tide).toMatchObject({ station_name: 'Example Harbour', tide_direction: 'Rising' })
    expect(full.snapshot.depthTrend).toMatchObject({ since: 'tide', tideType: 'low', tideDepthM: 2.6 })
    const missing = card('tile_depth_tide_missing.json')
    expect(missing.snapshot).toMatchObject({ depth: null, tide: null, navigationState: null })
    expect(missing.snapshot.depthTrend).toEqual({ points: [], since: 'window' })
  })

  it('marina and anchorage: the card the Go side writes', () => {
    expect(card('marina_coral_sea.json')).toMatchObject({ name: 'Coral Sea Marina Resort', berths: 520 })
    const anchorage = card('anchorage_butterfly_bay.json')
    expect(anchorage.name).toBe('West Butterfly Bay')
    expect(anchorage.depth.depthM).toBe(4)
    expect(anchorage.shelter.bearings).toHaveLength(36)
  })

  it('marina_small and anchorage_no_depth: absent berths, exceeded limit, skipped checks', () => {
    const marina = card('marina_small.json')
    expect(marina).toMatchObject({ berths: null, truncated: true, geometryTruncated: true })
    expect(marina.vesselLimitCheck.map((c: { particular: string; status: string }) => [c.particular, c.status])).toEqual([['draft', 'exceeds'], ['length', 'ok']])
    expect(marina.geometry.pontoons[0]).toMatchObject({ private: true, shape: { kind: 'line' } })
    const anchorage = card('anchorage_no_depth.json')
    expect(anchorage.depth).toMatchObject({ known: false, depthM: null, source: 'none' })
    expect(anchorage.depth.clearance.status).toBe('skipped')
    expect(anchorage.depth.clearance.skippedReason).not.toBe('')
  })
})
