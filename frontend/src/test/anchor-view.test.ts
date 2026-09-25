import { describe, expect, it } from 'vitest'
import { ANCHOR_VIEW_MAX_ZOOM, ANCHOR_VIEW_MIN_ZOOM, fitRadiusZoom } from '@/lib/anchor-view'

// A 390x500 phone viewport (the Anchor Watch drawer's map slot on a small
// screen) — the shorter side is 390.
const SHORT_SIDE_PX = 390

describe('fitRadiusZoom', () => {
  // Independently derived from MapLibre's real 512px-tile convention
  // (circumference/512 = 78271.51696402048, not the 256px-tile
  // 156543.03392 many "zoom to fit" snippets quote): zoom = log2(C *
  // cos(lat) / metersPerPixel), where metersPerPixel = (2*radius) /
  // (0.6*shortSide).
  it.each([
    [0, 20, 18.804636409966037],
    [0, 50, 17.482708315078675],
    [0, 300, 14.897745814357519],
    [45, 20, 18.304636409966037],
    [45, 50, 16.982708315078675],
    [45, 300, 14.397745814357519],
  ])('fits a %s° swing circle of %sm to 60%% of a 390px side at zoom %s', (lat, radiusM, expectedZoom) => {
    expect(fitRadiusZoom(radiusM, lat, SHORT_SIDE_PX)).toBeCloseTo(expectedZoom, 6)
  })

  it('increases zoom (more magnified) for a smaller radius, decreases for a larger one', () => {
    const small = fitRadiusZoom(20, 0, SHORT_SIDE_PX)
    const large = fitRadiusZoom(300, 0, SHORT_SIDE_PX)
    expect(small).toBeGreaterThan(large)
  })

  it('clamps to the 20 ceiling for a radius small enough to overshoot it', () => {
    expect(fitRadiusZoom(1, 0, SHORT_SIDE_PX)).toBe(ANCHOR_VIEW_MAX_ZOOM)
  })

  it('clamps to the 10 floor for a radius large enough to undershoot it', () => {
    expect(fitRadiusZoom(20000, 0, SHORT_SIDE_PX)).toBe(ANCHOR_VIEW_MIN_ZOOM)
  })

  it('clamps to the 10 floor rather than NaN/Infinity on a zero or negative radius', () => {
    expect(fitRadiusZoom(0, 0, SHORT_SIDE_PX)).toBe(ANCHOR_VIEW_MIN_ZOOM)
    expect(fitRadiusZoom(-5, 0, SHORT_SIDE_PX)).toBe(ANCHOR_VIEW_MIN_ZOOM)
  })

  it('clamps to the 10 floor rather than NaN/Infinity on a zero or negative container size', () => {
    expect(fitRadiusZoom(30, 0, 0)).toBe(ANCHOR_VIEW_MIN_ZOOM)
    expect(fitRadiusZoom(30, 0, -100)).toBe(ANCHOR_VIEW_MIN_ZOOM)
  })
})
