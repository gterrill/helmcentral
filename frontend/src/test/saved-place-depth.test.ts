import { describe, expect, it } from 'vitest'

import { depthSummary, depthToText, parseDepthText, todayDate } from '@/lib/saved-place-depth'

describe('parseDepthText', () => {
  it('reads metres, feet and an empty box', () => {
    expect(parseDepthText('4', 'metric')).toEqual({ ok: true, depthM: 4 })
    expect(parseDepthText(' 4,5 ', 'metric')).toEqual({ ok: true, depthM: 4.5 })
    expect(parseDepthText('-0.8', 'metric')).toEqual({ ok: true, depthM: -0.8 })
    expect(parseDepthText('', 'metric')).toEqual({ ok: true, depthM: null })
    const feet = parseDepthText('10', 'imperial')
    expect(feet.ok && feet.depthM).toBeCloseTo(3.048, 3)
  })

  it('refuses what is not a depth or is outside the server bounds', () => {
    for (const bad of ['deep', '4 m', '1e3', '5000', '-50', '--2']) {
      expect(parseDepthText(bad, 'metric')).toEqual({ ok: false })
    }
    expect(parseDepthText('4000', 'imperial')).toEqual({ ok: false })
  })
})

describe('todayDate', () => {
  it('is the date at the boat, not at the device', () => {
    const at = new Date('2026-11-01T20:00:00Z')
    expect(todayDate('Australia/Brisbane', at)).toBe('2026-11-02')
    expect(todayDate('UTC', at)).toBe('2026-11-01')
  })

  it('is null until the boat has reported a zone, so a depth is never dated by a guess', () => {
    expect(todayDate(undefined)).toBeNull()
  })
})

describe('depth text', () => {
  it('shows to a tenth in the operator units', () => {
    expect(depthToText(4, 'metric')).toBe('4.0')
    expect(depthToText(4, 'imperial')).toBe('13.1')
    expect(depthToText(null, 'metric')).toBe('')
  })

  it('summarises a depth with its source and date', () => {
    expect(depthSummary({ depth_m: 4, depth_source: 'sounded', depth_date: '2026-10-20' }, 'metric')).toBe('4.0 m (sounded 2026-10-20)')
    expect(depthSummary({ depth_m: null, depth_source: '', depth_date: '' }, 'metric')).toBe('')
  })
})
