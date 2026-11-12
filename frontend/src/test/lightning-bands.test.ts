import { describe, it, expect, vi } from 'vitest'

import {
  LIGHTNING_STEP_EDGES,
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
