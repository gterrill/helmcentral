import { describe, expect, it } from 'vitest'

import {
  DEFAULT_INSET_VIEW, INSET_CORNERS, INSET_ZOOM_CEILING, INSET_ZOOM_FLOOR, clampInsetStep, insetStepLabel, offsetInsetZoom,
} from '@/lib/passage-inset-view'

describe('the overview view', () => {
  it('defaults to the top right corner at the automatic zoom', () => {
    expect(DEFAULT_INSET_VIEW).toEqual({ corner: 'top-right', zoomOffset: 0 })
    expect(INSET_CORNERS.map((c) => c.label)).toEqual(['Top left', 'Top right', 'Bottom left', 'Bottom right'])
  })

  it('adds the steps to the automatic zoom and keeps the result in range', () => {
    expect(offsetInsetZoom(4.2, 0)).toBe(4.2)
    expect(offsetInsetZoom(4.2, 2)).toBeCloseTo(6.2, 5)
    expect(offsetInsetZoom(4.2, -3)).toBeCloseTo(1.2, 5)
    expect(offsetInsetZoom(1, -3)).toBe(INSET_ZOOM_FLOOR)
    expect(offsetInsetZoom(11, 3)).toBe(INSET_ZOOM_CEILING)
  })

  it('clamps and labels the step', () => {
    expect(clampInsetStep(5)).toBe(3)
    expect(clampInsetStep(-5)).toBe(-3)
    expect([insetStepLabel(2), insetStepLabel(0), insetStepLabel(-1)]).toEqual(['+2', '0', '-1'])
  })
})
