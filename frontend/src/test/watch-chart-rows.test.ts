import { describe, expect, it } from 'vitest'

import { panelRows } from '@/lib/watch-chart'
import type { WatchChartData } from '@/lib/watches'

const MIN = 60_000

function chart(points: [number, number][]): WatchChartData {
  return {
    finding_index: 0,
    title: 't',
    unit: 'A',
    timezone: 'UTC',
    utc_offset_seconds: 0,
    window: { from: '2026-10-29T00:00:00Z', to: '2026-10-29T12:00:00Z' },
    series: [{ label: 'Current', unit: 'A', points }],
  }
}

describe('panelRows', () => {
  it('puts every point in a row, in time order', () => {
    const rows = panelRows(chart([[2 * MIN, 5], [0, 4], [MIN, 6]]), [0])
    expect(rows.map((r) => [r.t, r.s0])).toEqual([[0, 4], [MIN, 6], [2 * MIN, 5]])
  })

  it('leaves a gap wider than four steps open with a null row, as the PNG does', () => {
    // A 30 minute step, then a 3 hour hole: over 4 x 30 minutes.
    const pts: [number, number][] = [[0, 1], [30 * MIN, 2], [60 * MIN, 3], [240 * MIN, 4], [270 * MIN, 5]]
    const rows = panelRows(chart(pts), [0])
    const nulls = rows.filter((r) => r.s0 === null)
    expect(nulls).toHaveLength(1)
    expect(nulls[0].t).toBeGreaterThan(60 * MIN)
    expect(nulls[0].t).toBeLessThan(240 * MIN)
    expect(rows).toHaveLength(6)
  })

  it('joins a gap of exactly four steps', () => {
    const pts: [number, number][] = [[0, 1], [30 * MIN, 2], [60 * MIN, 3], [180 * MIN, 4]]
    expect(panelRows(chart(pts), [0]).filter((r) => r.s0 === null)).toHaveLength(0)
  })
})
