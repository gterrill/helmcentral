import { describe, expect, it } from 'vitest'
import {
  ADJUST_MAX_ZOOM,
  ADJUST_RING_FRACTION,
  adjustWarningActive,
  adjustZoomBounds,
  alarmRadiusBounds,
  anchorMoveOffset,
  buildAdjustCommitTargets,
  clampRadiusM,
  formatMovedLabel,
  formatRadiusDisplay,
  metersPerPixel,
  MIN_ALARM_RADIUS_M,
  POSITION_UNCHANGED_TOLERANCE_M,
  radiusForZoom,
  RADIUS_STEP_FT,
  RADIUS_STEP_M,
  radiusStepM,
  ringRadiusPx,
  snapRadiusM,
  zoomForRingRadius,
} from '@/lib/anchor-adjust'

describe('alarmRadiusBounds', () => {
  it('the floor is always 5 m regardless of chain or LOA', () => {
    expect(alarmRadiusBounds({ chainOnboardM: 150, loaM: 12 }).minM).toBe(MIN_ALARM_RADIUS_M)
    expect(alarmRadiusBounds({ chainOnboardM: 10, loaM: null }).minM).toBe(MIN_ALARM_RADIUS_M)
  })

  it('the ceiling is chain onboard plus LOA when LOA is resolved', () => {
    const bounds = alarmRadiusBounds({ chainOnboardM: 150, loaM: 12 })
    expect(bounds.maxM).toBe(162)
    expect(bounds.maxReason).toBeNull()
  })

  it('falls back to chain onboard alone with no LOA, and names why', () => {
    const bounds = alarmRadiusBounds({ chainOnboardM: 150, loaM: null })
    expect(bounds.maxM).toBe(150)
    expect(bounds.maxReason).toBe('without boat length')
  })
})

// ── Adjust mode geometry (part 2) ───────────────────────────────────────────

describe('metersPerPixel', () => {
  it('halves for every zoom level increase (the standard Web Mercator doubling)', () => {
    const lat = -25.29
    const atZ10 = metersPerPixel(lat, 10)
    const atZ11 = metersPerPixel(lat, 11)
    expect(atZ11).toBeCloseTo(atZ10 / 2, 6)
  })

  it('shrinks towards the poles by cos(lat)', () => {
    const atEquator = metersPerPixel(0, 12)
    const at60 = metersPerPixel(60, 12)
    expect(at60).toBeCloseTo(atEquator * Math.cos((60 * Math.PI) / 180), 6)
  })
})

describe('ringRadiusPx', () => {
  it('is 35% of the container short side', () => {
    expect(ringRadiusPx(400)).toBeCloseTo(140, 6)
  })
  it('matches the documented ADJUST_RING_FRACTION constant', () => {
    expect(ringRadiusPx(1000)).toBeCloseTo(1000 * ADJUST_RING_FRACTION, 6)
  })
})

describe('zoomForRingRadius / radiusForZoom round-trip', () => {
  it('radiusForZoom inverts zoomForRingRadius', () => {
    const lat = -25.29
    const ringPx = 140
    const radiusM = 24
    const zoom = zoomForRingRadius(radiusM, lat, ringPx)
    expect(radiusForZoom(zoom, lat, ringPx)).toBeCloseTo(radiusM, 6)
  })

  it('a bigger radius needs a smaller (more zoomed-out) zoom for the same ring', () => {
    const lat = -25.29
    const ringPx = 140
    expect(zoomForRingRadius(50, lat, ringPx)).toBeLessThan(zoomForRingRadius(10, lat, ringPx))
  })

  it('falls back to a safe zoom for un-computable input rather than NaN/Infinity', () => {
    expect(Number.isFinite(zoomForRingRadius(0, -25, 140))).toBe(true)
    expect(Number.isFinite(zoomForRingRadius(-5, -25, 140))).toBe(true)
    expect(Number.isFinite(zoomForRingRadius(24, -25, 0))).toBe(true)
  })
})

describe('adjustZoomBounds', () => {
  it('a larger max radius maps to a smaller minZoom, and vice versa', () => {
    const lat = -25.29
    const ringPx = 140
    const narrow = adjustZoomBounds({ minM: 5, maxM: 20, maxReason: null }, lat, ringPx)
    const wide = adjustZoomBounds({ minM: 5, maxM: 200, maxReason: null }, lat, ringPx)
    expect(wide.minZoom).toBeLessThan(narrow.minZoom)
    expect(narrow.minZoom).toBeLessThanOrEqual(narrow.maxZoom)
  })

  it('never exceeds ADJUST_MAX_ZOOM even for a tiny minimum radius', () => {
    const bounds = adjustZoomBounds({ minM: 5, maxM: 200, maxReason: null }, -25.29, 140)
    expect(bounds.maxZoom).toBeLessThanOrEqual(ADJUST_MAX_ZOOM)
  })
})

describe('radiusStepM', () => {
  it('steps 1 m under metric', () => {
    expect(radiusStepM(false)).toBe(RADIUS_STEP_M)
  })
  it('steps a round 5 ft under imperial, converted to metres', () => {
    expect(radiusStepM(true)).toBeCloseTo(RADIUS_STEP_FT / 3.28084, 6)
  })
})

describe('snapRadiusM', () => {
  it('rounds to the nearest whole metre under metric', () => {
    expect(snapRadiusM(23.6, false)).toBe(24)
    expect(snapRadiusM(23.2, false)).toBe(23)
  })
  it('rounds to the nearest whole foot under imperial, returned in metres', () => {
    // 24 m ~= 78.74 ft -> rounds to 79 ft -> back to metres
    const snapped = snapRadiusM(24, true)
    expect(Math.round(snapped * 3.28084)).toBe(79)
  })
  it('is idempotent: snapping an already-snapped value changes nothing', () => {
    const once = snapRadiusM(24.4, true)
    expect(snapRadiusM(once, true)).toBeCloseTo(once, 9)
  })
})

describe('clampRadiusM', () => {
  it('clamps within [minM, maxM]', () => {
    const bounds = { minM: 5, maxM: 50, maxReason: null }
    expect(clampRadiusM(2, bounds)).toBe(5)
    expect(clampRadiusM(100, bounds)).toBe(50)
    expect(clampRadiusM(20, bounds)).toBe(20)
  })
})

describe('anchorMoveOffset / formatMovedLabel', () => {
  it('reports null (no label) under 1 m of movement', () => {
    const offset = anchorMoveOffset(-25.29, 152.91, -25.290001, 152.91)
    expect(offset.distanceM).toBeLessThan(1)
    expect(formatMovedLabel(offset, false)).toBeNull()
  })

  it('formats distance and a 3-digit true bearing once moved 1 m or more', () => {
    // Due north (bearing 0) — displayed 000.
    const offset = anchorMoveOffset(-25.29, 152.91, -25.28991, 152.91)
    expect(offset.distanceM).toBeGreaterThan(1)
    const label = formatMovedLabel(offset, false)
    expect(label).toMatch(/^moved \d+ m · \d{3}°$/)
  })

  it('pads a bearing under 100° to 3 digits (e.g. 045°)', () => {
    // Roughly east-north-east.
    const offset = { distanceM: 8, bearingDeg: 45 }
    expect(formatMovedLabel(offset, false)).toBe('moved 8 m · 045°')
  })

  it('formats distance in feet under imperial', () => {
    const offset = { distanceM: 10, bearingDeg: 90 }
    expect(formatMovedLabel(offset, true)).toBe('moved 33 ft · 090°')
  })
})

describe('adjustWarningActive', () => {
  it('is false with no live boat position', () => {
    expect(adjustWarningActive(null, 20, 4.572)).toBe(false)
  })
  it('is false when the boat sits inside the draft radius plus the drag buffer', () => {
    expect(adjustWarningActive(24, 20, 4.572)).toBe(false)
  })
  it('is true once the boat is outside the draft radius plus the drag buffer', () => {
    expect(adjustWarningActive(25, 20, 4.572)).toBe(true)
  })
})

describe('formatRadiusDisplay', () => {
  it('formats metres, rounded, under metric', () => {
    expect(formatRadiusDisplay(24.4, false)).toBe('24 m')
  })
  it('formats feet, rounded, under imperial', () => {
    // 24 m * 3.28084 = 78.74 ft.
    expect(formatRadiusDisplay(24, true)).toBe('79 ft')
  })
})

// buildAdjustCommitTargets (code-review finding, ADR 0136): Set always sent
// lat/lon even when only the radius changed, and the backend treats any
// lat/lon as a genuine reposition (resets the self trail, requires a fresh
// SignalK publish) — a radius-only change has no business paying that cost,
// and 502s outright when SignalK happens to be down. Undo must mirror
// whatever Set actually sent, not silently restore a position Set itself
// never touched.
describe('buildAdjustCommitTargets', () => {
  const committed = { lat: -25.2938, lon: 152.9102, radiusMeters: 20 }
  // Stands in for "nothing to restore" in tests that don't care about it -
  // the same shape a legacy watch (no bow offset ever applied, no place
  // name resolved yet) would carry.
  const noFacts = { bowOffsetM: 0, bowOffsetApplied: false, bowOffsetReason: '', headingAtSetDeg: -1, placeName: '' }

  it('omits lat/lon on both Set and Undo when the draft position is unchanged (a radius-only Set)', () => {
    const { set, undo } = buildAdjustCommitTargets(committed, { lat: committed.lat, lon: committed.lon, radiusMeters: 30 }, noFacts)
    expect(set).toEqual({ radiusMeters: 30 })
    expect(undo).toEqual({ radiusMeters: 20 })
  })

  it(`omits lat/lon within the ${POSITION_UNCHANGED_TOLERANCE_M} m tolerance — GPS/render jitter, not a deliberate move`, () => {
    // ~0.1 m north of committed — inside tolerance.
    const jitteredLat = committed.lat + 0.1 / 111_320
    const { set } = buildAdjustCommitTargets(committed, { lat: jitteredLat, lon: committed.lon, radiusMeters: 20 }, noFacts)
    expect(set).toEqual({ radiusMeters: 20 })
  })

  it('includes lat/lon on both Set and Undo once the draft position genuinely moved', () => {
    const draft = { lat: -25.2943, lon: 152.9107, radiusMeters: 20 }
    const { set, undo } = buildAdjustCommitTargets(committed, draft, noFacts)
    expect(set).toEqual({ lat: draft.lat, lon: draft.lon, radiusMeters: 20 })
    expect(undo).toEqual({ lat: committed.lat, lon: committed.lon, radiusMeters: 20, restore: noFacts })
  })

  // Code-review finding (round 2): Undo re-sends the committed position as
  // another position-changing PATCH, which the backend's own "placed by hand
  // in Adjust" defaults would otherwise stamp onto it — wrongly marking a
  // genuine bow-corrected drop as unapplied and losing its resolved place
  // name, even though Undo is putting the anchor back exactly where it was.
  // `restore` carries the committed point's own facts (from the moment
  // Adjust opened) so the backend can put them back instead.
  it("Undo's restore carries the committed point's own bow-offset/heading/place-name facts", () => {
    const draft = { lat: -25.2943, lon: 152.9107, radiusMeters: 20 }
    const facts = { bowOffsetM: 8, bowOffsetApplied: true, bowOffsetReason: '', headingAtSetDeg: 45, placeName: 'Goldsmith Island' }
    const { undo } = buildAdjustCommitTargets(committed, draft, facts)
    expect(undo.restore).toEqual(facts)
  })

  it('Set never carries restore, even when the position moved — only Undo puts a point back', () => {
    const draft = { lat: -25.2943, lon: 152.9107, radiusMeters: 20 }
    const facts = { bowOffsetM: 8, bowOffsetApplied: true, bowOffsetReason: '', headingAtSetDeg: 45, placeName: 'Goldsmith Island' }
    const { set } = buildAdjustCommitTargets(committed, draft, facts)
    expect(set).not.toHaveProperty('restore')
  })

  it('a radius-only Set/Undo carries no restore either — restore only ever accompanies a position change', () => {
    const facts = { bowOffsetM: 8, bowOffsetApplied: true, bowOffsetReason: '', headingAtSetDeg: 45, placeName: 'Goldsmith Island' }
    const { set, undo } = buildAdjustCommitTargets(committed, { lat: committed.lat, lon: committed.lon, radiusMeters: 30 }, facts)
    expect(set).not.toHaveProperty('restore')
    expect(undo).not.toHaveProperty('restore')
  })
})
