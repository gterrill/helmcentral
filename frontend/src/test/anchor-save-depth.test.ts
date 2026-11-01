import { describe, expect, it } from 'vitest'

import { depthSourceText, dropDay, saveFigureM, saveTargetPlace, SAVE_DEPTH_RADIUS_M, saveDepthLabel } from '@/lib/anchor-save-depth'
import type { SavedPlace } from '@/hooks/use-saved-places'

const place = (id: string, lat: number, lon: number, extra: Partial<SavedPlace> = {}): SavedPlace => ({
  id, name: id, lat, lon, note: '', depth_m: null, depth_source: '', depth_date: '', created_at: '', updated_at: '', ...extra,
})

describe('saveTargetPlace', () => {
  it('is 0.1 nm', () => {
    expect(SAVE_DEPTH_RADIUS_M).toBeCloseTo(185.2, 1)
  })

  it('picks the nearest within range, with or without a depth', () => {
    // 0.001 deg of latitude is about 111 m.
    const near = place('near', -20.001, 149)
    const nearer = place('nearer', -20.0005, 149)
    expect(saveTargetPlace([near, nearer], -20, 149)?.id).toBe('nearer')
    expect(saveTargetPlace([near], -20, 149)?.id).toBe('near')
  })

  it('prefers the nearest place that has a depth, as the anchorage reader does', () => {
    const a = place('A', -20.0002, 149)
    const b = place('B', -20.001, 149, { depth_m: 150, depth_source: 'charted', depth_date: '2026-10-30' })
    expect(saveTargetPlace([a, b], -20, 149)?.id).toBe('B')
  })

  it('ignores a place with a depth that is out of range', () => {
    const a = place('A', -20.0002, 149)
    const far = place('far', -20.003, 149, { depth_m: 5, depth_source: 'charted', depth_date: '2026-10-30' })
    expect(saveTargetPlace([a, far], -20, 149)?.id).toBe('A')
  })

  it('is null when nothing is within 0.1 nm', () => {
    expect(saveTargetPlace([place('far', -20.003, 149)], -20, 149)).toBeNull()
    expect(saveTargetPlace([], -20, 149)).toBeNull()
  })
})

describe('saveFigureM', () => {
  it('rounds to a centimetre', () => {
    expect(saveFigureM(4.2349)).toBe(4.23)
    expect(saveFigureM(4.2351)).toBe(4.24)
  })
})

describe('depthSourceText', () => {
  it('says where the figure came from', () => {
    expect(depthSourceText('sounder')).toBe('from the sounder')
    expect(depthSourceText('operator')).toBe('the depth you entered')
  })
})

describe('dropDay', () => {
  it('is the date at the boat, not the device or UTC', () => {
    expect(dropDay('2026-11-01T20:00:00Z', 'Australia/Brisbane')).toBe('2026-11-02')
    expect(dropDay('2026-11-01T20:00:00Z', 'UTC')).toBe('2026-11-01')
  })

  it('is null without a zone or a valid time, so a depth is never dated by a guess', () => {
    expect(dropDay('2026-11-01T20:00:00Z', null)).toBeNull()
    expect(dropDay(null, 'UTC')).toBeNull()
    expect(dropDay('not a date', 'UTC')).toBeNull()
  })
})

describe('wording', () => {
  it('words the button', () => {
    expect(saveDepthLabel(4.2, 'metric')).toBe('Save 4.2 m at datum')
    expect(saveDepthLabel(4.2, 'imperial')).toBe('Save 13.8 ft at datum')
  })
})
