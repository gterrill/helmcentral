import { describe, it, expect } from 'vitest'
import { classifyTidePhase } from '@/lib/tide-phase'
import type { TideExtremePoint } from '@/hooks/use-tide-chart'

// Heights chosen so consecutive-pair ranges (tidal range per half-cycle) trend
// 0.5, 0.5, 1.0, 1.0, 2.0, 2.0, 1.0, 1.0, 0.5, 0.5 across the window - a clear
// peak in the middle and matching troughs at both ends.
const TIMES = [
  '2026-07-01T00:00:00Z',
  '2026-07-01T06:00:00Z',
  '2026-07-01T12:00:00Z',
  '2026-07-01T18:00:00Z',
  '2026-07-02T00:00:00Z',
  '2026-07-02T06:00:00Z',
  '2026-07-02T12:00:00Z',
  '2026-07-02T18:00:00Z',
  '2026-07-03T00:00:00Z',
  '2026-07-03T06:00:00Z',
  '2026-07-03T12:00:00Z',
]
const HEIGHTS = [1.0, 1.5, 1.0, 2.0, 1.0, 3.0, 1.0, 2.0, 1.0, 1.5, 1.0]

function buildExtremes(): TideExtremePoint[] {
  return TIMES.map((time, i) => ({ time, heightM: HEIGHTS[i], high: i % 2 === 1 }))
}

describe('classifyTidePhase', () => {
  it('returns spring when now falls in the widest-range pair', () => {
    const now = new Date('2026-07-02T03:00:00Z') // between t4 (1.0) and t5 (3.0), range 2.0 = max
    expect(classifyTidePhase(buildExtremes(), now)).toBe('spring')
  })

  it('returns neap when now falls in the narrowest-range pair', () => {
    const now = new Date('2026-07-01T03:00:00Z') // between t0 (1.0) and t1 (1.5), range 0.5 = min
    expect(classifyTidePhase(buildExtremes(), now)).toBe('neap')
  })

  it('returns null when now falls in a mid-range pair', () => {
    const now = new Date('2026-07-01T15:00:00Z') // between t2 (1.0) and t3 (2.0), range 1.0, mid-span
    expect(classifyTidePhase(buildExtremes(), now)).toBeNull()
  })

  it('returns null when the extremes show no range variation', () => {
    const flat: TideExtremePoint[] = TIMES.map((time, i) => ({
      time,
      heightM: i % 2 === 0 ? 1.0 : 2.0,
      high: i % 2 === 1,
    }))
    expect(classifyTidePhase(flat, new Date('2026-07-02T03:00:00Z'))).toBeNull()
  })

  it('returns null when now falls outside the extremes window', () => {
    expect(classifyTidePhase(buildExtremes(), new Date('2026-06-20T00:00:00Z'))).toBeNull()
  })

  it('returns null for fewer than two extremes', () => {
    expect(classifyTidePhase([], new Date())).toBeNull()
    expect(classifyTidePhase([{ time: TIMES[0], heightM: 1, high: true }], new Date())).toBeNull()
  })
})
