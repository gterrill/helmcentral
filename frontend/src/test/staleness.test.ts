import { describe, expect, test } from 'vitest'

import { STALE_AFTER_SECONDS, formatDataAge, isStale } from '@/lib/staleness'

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
