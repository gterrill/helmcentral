/**
 * Tests for the motoring trail fetch and filter logic.
 *
 * The motoring trail is the vessel track from the 30 minutes prior to anchoring.
 * It is fetched on-demand when the user enters anchor reposition mode.
 *
 * Key invariants:
 *   - Points with timestamps BEFORE anchorSetAt are motoring points (shown in red)
 *   - Points with timestamps AT OR AFTER anchorSetAt are post-anchor (shown in yellow/amber)
 *   - If anchorSetAt is null, all points are treated as motoring context
 */

import { describe, it, expect } from 'vitest'
import type { TrailPoint } from '@/hooks/use-vessel-trail'

// ── Pure filter logic extracted from fetchMotoringTrail ───────────────────────
// This mirrors the logic in anchor-watch-map.tsx fetchMotoringTrail callback.
function filterMotoringPoints(
  rawPoints: Array<{ lat: number; lon: number; timestamp: string }>,
  anchorSetAtMs: number | null,
): TrailPoint[] {
  return rawPoints
    .map(p => ({ lat: p.lat, lon: p.lon, timestampMs: Date.parse(p.timestamp) }))
    .filter(p => anchorSetAtMs === null || (p.timestampMs !== undefined && p.timestampMs < anchorSetAtMs))
    .sort((a, b) => (a.timestampMs ?? 0) - (b.timestampMs ?? 0))
}

const ANCHOR_SET_AT = '2026-06-07T22:40:35Z'
const ANCHOR_SET_AT_MS = Date.parse(ANCHOR_SET_AT)

// Representative points from the real Influx data confirmed in probe runs
const PRE_DROP_POINTS = [
  { lat: -25.427162, lon: 152.987832, timestamp: '2026-06-07T04:19:43Z' },
  { lat: -25.419135, lon: 152.990420, timestamp: '2026-06-07T04:22:44Z' },
  { lat: -25.414924, lon: 152.994187, timestamp: '2026-06-07T04:24:43Z' },
  { lat: -25.401025, lon: 153.008046, timestamp: '2026-06-07T04:31:43Z' },
  { lat: -25.400631, lon: 153.009877, timestamp: '2026-06-07T04:32:43Z' },
]

const POST_ANCHOR_POINTS = [
  { lat: -25.402468, lon: 153.014255, timestamp: '2026-06-07T22:40:37Z' },
  { lat: -25.402413, lon: 153.014295, timestamp: '2026-06-07T22:40:43Z' },
  { lat: -25.402469, lon: 153.014255, timestamp: '2026-06-07T22:40:47Z' },
]

describe('filterMotoringPoints', () => {
  it('keeps only points before anchorSetAt', () => {
    const result = filterMotoringPoints(
      [...PRE_DROP_POINTS, ...POST_ANCHOR_POINTS],
      ANCHOR_SET_AT_MS,
    )
    expect(result).toHaveLength(PRE_DROP_POINTS.length)
    for (const pt of result) {
      expect(pt.timestampMs).toBeLessThan(ANCHOR_SET_AT_MS)
    }
  })

  it('returns points in chronological order', () => {
    // Intentionally reversed input
    const shuffled = [...PRE_DROP_POINTS].reverse()
    const result = filterMotoringPoints(shuffled, ANCHOR_SET_AT_MS)
    for (let i = 1; i < result.length; i++) {
      expect(result[i].timestampMs!).toBeGreaterThanOrEqual(result[i - 1].timestampMs!)
    }
  })

  it('returns all points when anchorSetAt is null', () => {
    const all = [...PRE_DROP_POINTS, ...POST_ANCHOR_POINTS]
    const result = filterMotoringPoints(all, null)
    expect(result).toHaveLength(all.length)
  })

  it('returns empty array when all points are post-anchor', () => {
    const result = filterMotoringPoints(POST_ANCHOR_POINTS, ANCHOR_SET_AT_MS)
    expect(result).toHaveLength(0)
  })

  it('needs at least 2 points to render a line (geometry guard)', () => {
    const single = [PRE_DROP_POINTS[0]]
    const result = filterMotoringPoints(single, ANCHOR_SET_AT_MS)
    // The map layer uses: motoringTrailGeoJSON.geometry.coordinates.length >= 2
    // A single point cannot form a line — caller must guard on length >= 2
    expect(result.length < 2).toBe(true)
  })
})

describe('filterMotoringPoints — edge cases', () => {
  it('excludes a point exactly at anchorSetAt (boundary is exclusive)', () => {
    const exactBoundary = [
      { lat: -25.402, lon: 153.014, timestamp: ANCHOR_SET_AT },
    ]
    const result = filterMotoringPoints(exactBoundary, ANCHOR_SET_AT_MS)
    // Point is NOT strictly less than cutoff — must be excluded
    expect(result).toHaveLength(0)
  })

  it('includes a point one millisecond before anchorSetAt', () => {
    const oneMs = new Date(ANCHOR_SET_AT_MS - 1).toISOString()
    const justBefore = [{ lat: -25.402, lon: 153.014, timestamp: oneMs }]
    const result = filterMotoringPoints(justBefore, ANCHOR_SET_AT_MS)
    expect(result).toHaveLength(1)
  })

  it('returns empty array when rawPoints is empty', () => {
    expect(filterMotoringPoints([], ANCHOR_SET_AT_MS)).toHaveLength(0)
  })
})
