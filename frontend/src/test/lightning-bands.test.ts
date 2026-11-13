import { describe, it, expect, vi } from 'vitest'

import {
  LIGHTNING_STEP_EDGES,
  dayLightning,
  lightningAt,
  lightningLabel,
  lightningOpacity,
  lightningPercent,
  lightningStatus,
  lightningStep,
  lightningTip,
  parseLightningWindow,
  placeLightningOnSamples,
  type LightningFeed,
} from '@/lib/lightning-bands'

const H = 3_600_000
const T0 = Date.UTC(2026, 10, 13, 0, 0, 0)

describe('lightningStep', () => {
  it('draws nothing under 10 percent', () => {
    expect(lightningStep(0)).toBe(0)
    expect(lightningStep(0.0999)).toBe(0)
  })

  it('puts each edge in the step above it', () => {
    expect(LIGHTNING_STEP_EDGES).toEqual([0.1, 0.3, 0.6])
    expect(lightningStep(0.1)).toBe(1)
    expect(lightningStep(0.2999)).toBe(1)
    expect(lightningStep(0.3)).toBe(2)
    expect(lightningStep(0.5999)).toBe(2)
    expect(lightningStep(0.6)).toBe(3)
    expect(lightningStep(1)).toBe(3)
  })

  it('refuses a value that is not a probability rather than inventing a step', () => {
    expect(() => lightningStep(Number.NaN)).toThrow(RangeError)
    expect(() => lightningStep(-0.01)).toThrow(RangeError)
    expect(() => lightningStep(1.01)).toThrow(RangeError)
    expect(() => lightningStep(Number.POSITIVE_INFINITY)).toThrow(RangeError)
  })

  it('raises the fill opacity with each step', () => {
    expect(lightningOpacity(1)).toBe(0.4)
    expect(lightningOpacity(2)).toBe(0.7)
    expect(lightningOpacity(3)).toBe(1)
  })
})

describe('parseLightningWindow', () => {
  it('reads the API window into milliseconds', () => {
    expect(
      parseLightningWindow({ start: '2026-11-13T00:00:00Z', end: '2026-11-13T03:00:00Z', probability: 0.4 }),
    ).toEqual({ startMs: T0, endMs: T0 + 3 * H, probability: 0.4 })
  })

  it('throws on a window it cannot read', () => {
    expect(() => parseLightningWindow({ start: 'soon', end: '2026-11-13T03:00:00Z', probability: 0.4 })).toThrow()
    expect(() => parseLightningWindow({ start: '2026-11-13T03:00:00Z', end: '2026-11-13T00:00:00Z', probability: 0.4 })).toThrow()
    expect(() => parseLightningWindow({ start: '2026-11-13T00:00:00Z', end: '2026-11-13T03:00:00Z', probability: 4 })).toThrow()
    expect(() => parseLightningWindow({ start: '2026-11-13T00:00:00Z', end: '2026-11-13T03:00:00Z' })).toThrow()
  })
})

describe('placeLightningOnSamples', () => {
  // Hourly points 0 to 23 at 10 px an hour, plot 0 to 230.
  const hours = Array.from({ length: 24 }, (_, i) => i)
  const ms = hours.map((h) => T0 + h * H)
  const xs = hours.map((h) => h * 10)
  const win = (startH: number, endH: number, probability = 0.4) => ({ startMs: T0 + startH * H, endMs: T0 + endH * H, probability })

  it('draws the slots of the samples a window contains, each running halfway to its neighbours', () => {
    // Contains hours 4, 5 and 6 (7 is the end, excluded): 3.5 to 6.5.
    const { blocks } = placeLightningOnSamples([win(4, 7)], ms, xs)
    expect(blocks).toEqual([{ key: String(T0 + 4 * H), x: 35, width: 30, step: 2 }])
  })

  it('stops at the sample itself at either end of the data', () => {
    const last = placeLightningOnSamples([win(23, 26)], ms, xs)
    expect(last.blocks).toEqual([{ key: String(T0 + 23 * H), x: 225, width: 5, step: 2 }])
    const first = placeLightningOnSamples([win(-2, 1)], ms, xs)
    expect(first.blocks).toEqual([{ key: String(T0), x: 0, width: 5, step: 2 }])
  })

  it('draws nothing for a window that ends exactly at the first sample', () => {
    const { blocks, peak } = placeLightningOnSamples([win(-3, 0)], ms, xs)
    expect(blocks).toEqual([])
    expect(peak).toBeNull()
  })

  it('merges consecutive slots of one step across back-to-back windows and splits at a quiet one', () => {
    const { blocks } = placeLightningOnSamples(
      [win(0, 3, 0.2), win(3, 6, 0.25), win(6, 9, 0.05), win(9, 12, 0.2), win(12, 15, 0.5)],
      ms,
      xs,
    )
    expect(blocks).toEqual([
      { key: String(T0), x: 0, width: 55, step: 1 },
      { key: String(T0 + 9 * H), x: 85, width: 30, step: 1 },
      { key: String(T0 + 12 * H), x: 115, width: 30, step: 2 },
    ])
  })

  it('hatches the slots no window covers, and not a window that says quiet', () => {
    const { gaps, blocks } = placeLightningOnSamples([win(4, 7, 0.4), win(7, 10, 0.05)], ms, xs)
    // Held from hour 4 to hour 9, quiet from 7. Uncovered: hours 0-3 and 10-23.
    expect(blocks).toHaveLength(1)
    expect(gaps).toEqual([
      { key: String(T0), x: 0, width: 35 },
      { key: String(T0 + 10 * H), x: 95, width: 135 },
    ])
  })

  it('hatches the whole chart when nothing is held, and nothing when everything is', () => {
    expect(placeLightningOnSamples([], ms, xs).gaps).toEqual([{ key: String(T0), x: 0, width: 230 }])
    expect(placeLightningOnSamples([win(-3, 30)], ms, xs).gaps).toEqual([])
  })

  it('names the first sample the peak window contains, always a time on the chart', () => {
    const early = placeLightningOnSamples([win(-2, 1, 0.9), win(10, 13, 0.3)], ms, xs)
    expect(early.peak).toEqual({ window: win(-2, 1, 0.9), atMs: T0 })
    const later = placeLightningOnSamples([win(4, 7, 0.3), win(10, 13, 0.8)], ms, xs)
    expect(later.peak?.atMs).toBe(T0 + 10 * H)
  })

  it('works on samples that are not evenly spaced in time', () => {
    const six = [0, 6, 12, 18, 24, 30].map((h) => T0 + h * H)
    const sx = [0, 1, 2, 3, 4, 5].map((i) => i * 100)
    const { blocks } = placeLightningOnSamples([win(3, 9)], six, sx)
    // Contains the 6 h sample only: halfway to each neighbour.
    expect(blocks).toEqual([{ key: String(T0 + 6 * H), x: 50, width: 100, step: 2 }])
  })

  it('refuses samples and positions that differ in length', () => {
    expect(() => placeLightningOnSamples([win(0, 3)], ms, xs.slice(1))).toThrow(RangeError)
  })
})

describe('lightning wording', () => {
  const w = (probability: number) => ({ startMs: T0, endMs: T0 + 3 * H, probability })

  it('gives the percent and the tip', () => {
    expect(lightningPercent(w(0.456))).toBe(46)
    expect(lightningTip([w(0.35)], T0 + H)).toBe('Lightning 35%')
    expect(lightningTip([w(0.35)], T0 + 4 * H)).toBeNull()
  })

  it('labels the peak, or says it is below 10 percent', () => {
    expect(lightningLabel(w(0.7), 'Mon 4.00 PM')).toBe('Lightning probability, highest 70% at Mon 4.00 PM')
    expect(lightningLabel(w(0.0999), 'x')).toBe('Lightning probability, below 10% throughout')
  })

  it('does not claim below 10% throughout over hours with no data', () => {
    expect(lightningLabel(w(0.7), 'Mon 4.00 PM', true)).toBe(
      'Lightning probability, highest 70% at Mon 4.00 PM, no data for part of it',
    )
    expect(lightningLabel(w(0.05), 'x', true)).toBe('Lightning probability, below 10% where there is data, no data for the rest')
    expect(lightningLabel(null, '', true)).toBe('No lightning data')
    expect(lightningLabel(null, '')).toBe('No lightning data')
  })

})

describe('lightningStatus', () => {
  const feed = (overrides: Partial<LightningFeed> = {}): LightningFeed => ({
    windows: [{ startMs: T0, endMs: T0 + 3 * H, probability: 0.4 }],
    provider: 'ukmo-lightning',
    providerSeen: true,
    refreshing: false,
    refreshError: null,
    error: null,
    updatedAt: null,
    ttlSeconds: null,
    ...overrides,
  })

  it('is ready for held windows in a known zone', () => {
    expect(lightningStatus(feed(), false, 'Australia/Brisbane')).toEqual({ kind: 'ready', error: null })
  })

  it('says nothing at all until some answer has named a provider', () => {
    const quiet = { windows: [], providerSeen: false, provider: null }
    expect(lightningStatus(undefined, false, 'UTC')).toEqual({ kind: 'none' })
    expect(lightningStatus(feed(quiet), false, 'UTC')).toEqual({ kind: 'none' })
    expect(lightningStatus(feed({ ...quiet, error: 'down' }), false, 'UTC')).toEqual({ kind: 'none' })
    expect(lightningStatus(feed({ ...quiet, refreshing: true }), false, 'UTC')).toEqual({ kind: 'none' })
    expect(lightningStatus(feed({ ...quiet, refreshError: 'x' }), false, undefined)).toEqual({ kind: 'none' })
    expect(lightningStatus(feed(quiet), true, 'UTC')).toEqual({ kind: 'none' })
  })

  it('stays silent about a refresh in progress but still ready once a provider was named', () => {
    expect(lightningStatus(feed({ windows: [], refreshing: true }), false, 'UTC')).toEqual({ kind: 'ready', error: null })
  })

  it('always carries the first line of a request error or a refresh error, even behind held windows', () => {
    expect(lightningStatus(feed({ error: 'down\nmore' }), false, 'UTC')).toEqual({ kind: 'ready', error: 'down' })
    expect(lightningStatus(feed({ refreshError: 'bucket gone\nx' }), false, 'UTC')).toEqual({ kind: 'ready', error: 'bucket gone' })
    expect(lightningStatus(feed({ windows: [], error: 'down' }), false, 'UTC')).toEqual({ kind: 'ready', error: 'down' })
  })

  it('is an error when the zone is missing or unknown', () => {
    const quiet = vi.spyOn(console, 'error').mockImplementation(() => {})
    expect(lightningStatus(feed(), false, undefined)).toEqual({ kind: 'error', message: 'the boat has not reported its time zone' })
    expect(lightningStatus(feed(), false, 'Mars/Olympus')).toEqual({ kind: 'error', message: 'Zone Mars/Olympus unknown to this browser' })
    quiet.mockRestore()
  })

  it('says nothing once the latest answer names no provider, though one was seen before', () => {
    expect(lightningStatus(feed({ provider: null, windows: [] }), false, 'UTC')).toEqual({ kind: 'none' })
    expect(lightningStatus(feed({ provider: null, windows: [], error: 'down' }), true, 'UTC')).toEqual({ kind: 'none' })
  })

  it('reports a stay once a provider is known, and nothing before', () => {
    expect(lightningStatus(feed(), true, undefined)).toEqual({ kind: 'stay' })
    expect(lightningStatus(feed({ windows: [] }), true, 'Australia/Brisbane')).toEqual({ kind: 'stay' })
  })
})

describe('lightningAt', () => {
  const a = { startMs: T0, endMs: T0 + 3 * H, probability: 0.05 }
  const b = { startMs: T0 + 3 * H, endMs: T0 + 6 * H, probability: 0.45 }

  it('finds the window holding an instant, start included and end excluded', () => {
    expect(lightningAt([a, b], T0 + 3 * H)).toBe(b)
    expect(lightningAt([a, b], T0 + 3 * H - 1)).toBe(a)
    expect(lightningAt([a, b], T0 + 6 * H)).toBeNull()
  })
})

describe('dayLightning', () => {
  // Brisbane is UTC+10 all year: 2026-06-14 runs 14:00Z on the 13th to 14:00Z on the 14th.
  const DAY = Date.UTC(2026, 5, 13, 14)
  const at = (hour: number, probability: number, hours = 3) => ({ startMs: DAY + hour * H, endMs: DAY + (hour + hours) * H, probability })
  const steps = (r: ReturnType<typeof dayLightning>) => r.segments.map((seg) => seg.step)
  const BNE = 'Australia/Brisbane'
  const EMPTY = { segments: [], peak: null, sentence: null }

  it('places the windows on the day as the chart band does, one slot per hour', () => {
    const r = dayLightning([at(0, 0.05), at(3, 0.2), at(6, 0.45), at(9, 0.8), at(12, 0.02, 12)], '2026-06-14', BNE)
    expect(steps(r)).toEqual([1, 2, 3])
    // Hours 9 to 11, each slot halfway to its neighbours, on a 24-point axis.
    expect(r.segments[2].from).toBeCloseTo(8.5 / 23)
    expect(r.segments[2].to).toBeCloseTo(11.5 / 23)
    expect(r.peak?.probability).toBe(0.8)
    expect(r.sentence).toBe('Lightning likely from 9 AM to 12 PM.')
  })

  it('keeps a window to its own hours where the zone is not on the 3-hour grid', () => {
    // New York in June is UTC-4: 18Z-21Z is 2 PM to 5 PM.
    const nyDay = Date.UTC(2026, 5, 14, 4)
    const w = (startZ: number, probability: number) => ({ startMs: nyDay + (startZ - 4) * H, endMs: nyDay + (startZ - 1) * H, probability })
    const r = dayLightning([3, 6, 9, 12, 15, 18, 21, 24, 27].map((z) => w(z, z === 18 ? 0.8 : 0.02)), '2026-06-14', 'America/New_York')
    expect(steps(r)).toEqual([3])
    expect(r.sentence).toBe('Lightning likely from 2 PM to 5 PM.')
  })

  it('does not carry a storm just past midnight into the day before', () => {
    expect(dayLightning([at(0, 0.02, 24), at(24, 0.8)], '2026-06-14', BNE)).toEqual(EMPTY)
  })

  it('words the sentence by the strongest step, over its first stretch', () => {
    expect(dayLightning([at(0, 0.02, 21), at(21, 0.15)], '2026-06-14', BNE).sentence).toBe('Slight chance of lightning from 9 PM to midnight.')
    expect(dayLightning([at(0, 0.05), at(3, 0.35), at(6, 0.15), at(9, 0.4), at(12, 0.02, 12)], '2026-06-14', BNE).sentence).toBe(
      'Lightning possible from 3 AM to 6 AM.',
    )
    expect(dayLightning([at(0, 0.7), at(3, 0.02, 18), at(21, 0.7)], '2026-06-14', BNE).sentence).toBe('Lightning likely from 12 AM to 3 AM.')
  })

  it('says throughout the day only when the strongest step holds every hour', () => {
    expect(dayLightning([at(0, 0.5, 24)], '2026-06-14', BNE).sentence).toBe('Lightning possible throughout the day.')
  })

  it('reads overlapping windows the way the chart band does, without failing', () => {
    // The chart takes the first window holding an hour, so A's 15% wins 3 to 6 AM.
    const r = dayLightning([at(0, 0.15, 6), at(3, 0.7), at(6, 0.02, 18)], '2026-06-14', BNE)
    expect(steps(r)).toEqual([1])
    expect(r.peak?.probability).toBe(0.15)
    expect(r.sentence).toBe('Slight chance of lightning from 12 AM to 6 AM.')
  })

  it('leaves out the hours before the run and marks the hours past its end as no data', () => {
    const r = dayLightning([at(6, 0.4), at(9, 0.1, 1)], '2026-06-14', BNE)
    expect(steps(r)).toEqual([2, 1, null])
    // The chart band's own slot for 6 AM, from halfway after 5 AM.
    expect(r.segments[0].from).toBeCloseTo(5.5 / 23)
    expect(r.segments[2].to).toBeCloseTo(1)
  })

  it('has no peak or sentence on a quiet day, though hours past the run still show', () => {
    const r = dayLightning([at(0, 0.05, 12)], '2026-06-14', BNE)
    expect(steps(r)).toEqual([null])
    expect(r.peak).toBeNull()
    expect(r.sentence).toBeNull()
  })

  it('has nothing to lay out with no window held, or on a day before the run', () => {
    expect(dayLightning([], '2026-06-14', BNE)).toEqual(EMPTY)
    expect(dayLightning([at(30, 0.8)], '2026-06-14', BNE)).toEqual(EMPTY)
  })

  it('ends the sentence where the stretch ends, not where its window does', () => {
    // The 20% window comes first, so the chart reads it for noon to 3 PM.
    expect(dayLightning([at(12, 0.2), at(0, 0.7, 24)], '2026-06-14', BNE).sentence).toBe('Lightning likely from 12 AM to 12 PM.')
  })

  it('still draws a run that starts in the last hour of the day', () => {
    const r = dayLightning([at(23, 0.7)], '2026-06-14', BNE)
    expect(steps(r)).toEqual([3])
    expect(r.segments[0].to - r.segments[0].from).toBeGreaterThan(0)
    expect(r.sentence).toBe('Lightning likely from 11 PM to midnight.')
  })

  it('places a clocks-change day by wall-clock hour, as the chart does', () => {
    // Sydney springs forward on 2026-10-04 (AEDT, UTC+11 after 2 AM): 3 PM to 6 PM is 04:00Z to 07:00Z.
    const r = dayLightning(
      [
        { startMs: Date.UTC(2026, 9, 3, 14), endMs: Date.UTC(2026, 9, 4, 4), probability: 0.02 },
        { startMs: Date.UTC(2026, 9, 4, 4), endMs: Date.UTC(2026, 9, 4, 7), probability: 0.7 },
        { startMs: Date.UTC(2026, 9, 4, 7), endMs: Date.UTC(2026, 9, 4, 13), probability: 0.02 },
      ],
      '2026-10-04',
      'Australia/Sydney',
    )
    expect(r.segments[0].from).toBeCloseTo(14.5 / 23)
    expect(r.sentence).toBe('Lightning likely from 3 PM to 6 PM.')
  })

  it('lays a clocks-forward day over its 23 hours', () => {
    // Sydney springs forward at 2 AM on 2026-10-04: the day runs 14:00Z on the 3rd to 13:00Z on the 4th.
    const midnight = Date.UTC(2026, 9, 3, 14)
    const r = dayLightning([{ startMs: midnight, endMs: midnight + 23 * H, probability: 0.5 }], '2026-10-04', 'Australia/Sydney')
    expect(r.segments).toEqual([expect.objectContaining({ from: 0, to: 1, step: 2 })])
    expect(r.sentence).toBe('Lightning possible throughout the day.')
  })
})
