import { describe, expect, it } from 'vitest'

import {
  coastlineFailures,
  coastlineStatusLine,
  coverageRingGeoJSON,
  coverageSquaresGeoJSON,
  squareLabel,
  type OfflineCoastlineStatus,
} from '@/lib/offline-coastline'

function status(overrides: Partial<OfflineCoastlineStatus> = {}): OfflineCoastlineStatus {
  return {
    enabled: true,
    state: 'idle',
    has_fix: true,
    position: { lat: -20.1, lon: 148.9 },
    radius_nm: 200,
    squares: [],
    held: 0,
    wanted: 0,
    wanted_held: 0,
    failed: 0,
    edits_since: 0,
    squares_with_edits: 0,
    updating: 0,
    ...overrides,
  }
}

const NOON = (iso: string) => `${iso}T12:00:00Z`

describe('squareLabel', () => {
  it('names a square by its south-west corner', () => {
    expect(squareLabel(-21, 148)).toBe('21°S 148°E')
    expect(squareLabel(51, -2)).toBe('51°N 2°W')
  })
})

describe('coastlineStatusLine', () => {
  it('says it is off', () => {
    expect(coastlineStatusLine(status({ enabled: false, state: 'off' }))).toBe('Off. Nothing is downloaded or checked.')
  })

  it('shows coverage with the oldest date and edits since', () => {
    const line = coastlineStatusLine(status({
      held: 31, oldest_downloaded_at: NOON('2026-10-03'), edits_since: 12, squares_with_edits: 3,
    }), new Date(NOON('2026-10-20')))
    expect(line).toBe('Coverage: 31 squares · oldest 3 Oct · 12 edits since')
  })

  it('leaves out edits when there are none and says square in the singular', () => {
    expect(coastlineStatusLine(status({ held: 1, oldest_downloaded_at: NOON('2026-10-03') }), new Date(NOON('2026-10-20'))))
      .toBe('Coverage: 1 square · oldest 3 Oct')
    expect(coastlineStatusLine(status({ held: 2, oldest_downloaded_at: NOON('2026-10-03'), edits_since: 1, squares_with_edits: 1 }), new Date(NOON('2026-10-20'))))
      .toBe('Coverage: 2 squares · oldest 3 Oct · 1 edit since')
  })

  it('adds the year to a date from another year', () => {
    expect(coastlineStatusLine(status({ held: 4, oldest_downloaded_at: NOON('2025-10-03') }), new Date(NOON('2026-10-20'))))
      .toBe('Coverage: 4 squares · oldest 3 Oct 2025')
  })

  it('shows progress while downloading', () => {
    expect(coastlineStatusLine(status({ state: 'downloading', wanted: 25, wanted_held: 7, held: 7, oldest_downloaded_at: NOON('2026-10-03') })))
      .toBe('Downloading 7 of 25')
  })

  it('waits for a position fix, keeping what is held in view', () => {
    expect(coastlineStatusLine(status({ state: 'waiting_for_fix', has_fix: false, position: undefined })))
      .toBe('Waiting for a position fix')
    expect(coastlineStatusLine(status({
      state: 'waiting_for_fix', has_fix: false, position: undefined, held: 12, oldest_downloaded_at: NOON('2026-10-03'),
    }), new Date(NOON('2026-10-20')))).toBe('Waiting for a position fix · Coverage: 12 squares · oldest 3 Oct')
  })

  it('says how many squares are being updated', () => {
    expect(coastlineStatusLine(status({ state: 'updating', updating: 3, held: 31, oldest_downloaded_at: NOON('2026-10-03') })))
      .toBe('Updating 3 squares')
    expect(coastlineStatusLine(status({ state: 'updating', updating: 1 }))).toBe('Updating 1 square')
  })

  it('names squares waiting to retry', () => {
    expect(coastlineStatusLine(status({ state: 'retrying', failed: 2, held: 20, oldest_downloaded_at: NOON('2026-10-03') }), new Date(NOON('2026-10-20'))))
      .toBe('Coverage: 20 squares · oldest 3 Oct · 2 failed, will retry')
  })

  it('reports an error as the server worded it', () => {
    expect(coastlineStatusLine(status({ state: 'error', error: 'the osm-overpass plugin is too old for the offline coastline: install the updated plugin' })))
      .toBe('the osm-overpass plugin is too old for the offline coastline: install the updated plugin')
  })

  it('says when switched on but nothing is held yet', () => {
    expect(coastlineStatusLine(status({ state: 'idle', held: 0 }))).toBe('Coverage: none yet')
  })
})

describe('coastlineFailures', () => {
  it('lists squares with an error, named by label', () => {
    const s = status({
      squares: [
        { lat: -21, lon: 148, state: 'ready', in_range: true, edits_since: 0 },
        { lat: -21, lon: 149, state: 'failed', in_range: true, edits_since: 0, error: 'overpass rate limited', next_attempt_at: NOON('2026-10-03') },
        { lat: -22, lon: 148, state: 'ready', in_range: true, edits_since: 2, error: 'timed out' },
      ],
    })
    expect(coastlineFailures(s).map((f) => [f.label, f.error])).toEqual([
      ['21°S 149°E', 'overpass rate limited'],
      ['22°S 148°E', 'timed out'],
    ])
  })
})

describe('coverage geometry', () => {
  it('draws each square as a polygon with its state and age', () => {
    const now = new Date(NOON('2026-10-13'))
    const fc = coverageSquaresGeoJSON(status({
      squares: [
        { lat: -21, lon: 148, state: 'ready', in_range: true, edits_since: 0, downloaded_at: NOON('2026-10-03') },
        { lat: -21, lon: 149, state: 'failed', in_range: true, edits_since: 0, error: 'x' },
        { lat: -22, lon: 148, state: 'pending', in_range: true, edits_since: 0 },
      ],
    }), now)
    expect(fc.features).toHaveLength(3)
    const [ready, failed, pending] = fc.features
    expect(ready.properties).toMatchObject({ state: 'ready', age_days: 10 })
    expect(failed.properties).toMatchObject({ state: 'failed' })
    expect(pending.properties).toMatchObject({ state: 'pending' })
    expect(ready.geometry.coordinates[0]).toEqual([[148, -21], [149, -21], [149, -20], [148, -20], [148, -21]])
  })

  it('treats a square that failed an update as failed on the map though it holds data', () => {
    const fc = coverageSquaresGeoJSON(status({
      squares: [{ lat: -21, lon: 148, state: 'ready', in_range: true, edits_since: 0, downloaded_at: NOON('2026-10-03'), error: 'timed out' }],
    }), new Date(NOON('2026-10-13')))
    expect(fc.features[0].properties).toMatchObject({ state: 'ready', failed: true })
  })

  it('draws the coverage ring as a closed circle of the right size', () => {
    const ring = coverageRingGeoJSON(-20.1, 148.9, 200)
    const pts = ring.geometry.coordinates[0]
    expect(pts[0]).toEqual(pts[pts.length - 1])
    expect(pts.length).toBeGreaterThan(60)
    // 200 nm north is 3.33 degrees of latitude.
    const maxLat = Math.max(...pts.map((p) => p[1]))
    expect(maxLat).toBeGreaterThan(-20.1 + 3.3)
    expect(maxLat).toBeLessThan(-20.1 + 3.4)
  })
})
