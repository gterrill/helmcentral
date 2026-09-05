import { describe, expect, test } from 'vitest'

import { STALE_AFTER_SECONDS, ageFromPayload, formatDataAge, isStale } from '@/lib/staleness'

describe('isStale', () => {
  test('fresh data is not stale', () => {
    expect(isStale(0)).toBe(false)
    expect(isStale(5)).toBe(false)
    expect(isStale(STALE_AFTER_SECONDS)).toBe(false)
  })

  test('data older than the threshold is stale', () => {
    expect(isStale(STALE_AFTER_SECONDS + 1)).toBe(true)
    expect(isStale(6000)).toBe(true)
  })

  test('an unknown age is not reported as stale', () => {
    // A source that never carries a timestamp gives us no freshness signal.
    // Flagging it would cry wolf on every such source forever.
    expect(isStale(null)).toBe(false)
  })

  test('honours a caller-supplied threshold', () => {
    expect(isStale(30, 10)).toBe(true)
    expect(isStale(30, 60)).toBe(false)
  })
})

describe('formatDataAge', () => {
  test('renders a compact human age', () => {
    expect(formatDataAge(45)).toBe('45s')
    expect(formatDataAge(120)).toBe('2m')
    expect(formatDataAge(5940)).toBe('1h 39m')
    expect(formatDataAge(7200)).toBe('2h')
    expect(formatDataAge(180000)).toBe('2d')
  })

  test('renders unknown ages as an em dash', () => {
    expect(formatDataAge(null)).toBe('—')
  })
})

describe('ageFromPayload', () => {
  // The backend emits -1 for "this source carries no timestamp" (ADR 0068),
  // because a JSON number field cannot be absent the way a Go zero value can.
  // The UI contract is `number | null`, and null is what reads as "not stale".
  // Mapping the sentinel at the boundary keeps that meaning in one place
  // instead of teaching every tile about -1.
  test('maps the backend unknown sentinel to null', () => {
    expect(ageFromPayload(-1)).toBeNull()
  })

  test('keeps a real age', () => {
    expect(ageFromPayload(0)).toBe(0)
    expect(ageFromPayload(5940)).toBe(5940)
  })

  test('treats a missing or malformed value as unknown rather than fresh', () => {
    expect(ageFromPayload(undefined)).toBeNull()
    expect(ageFromPayload(null)).toBeNull()
    expect(ageFromPayload('120')).toBeNull()
    expect(ageFromPayload(Number.NaN)).toBeNull()
    expect(ageFromPayload(Number.POSITIVE_INFINITY)).toBeNull()
  })
})
