import { describe, expect, it } from 'vitest'

import {
  fuelBarZones,
  tankLevelsError,
  tankTone,
  tankClearText,
} from '@/lib/tank-levels'

describe('tankTone', () => {
  const fuel = { warn_percent: 20, alarm_percent: 10 }
  const waste = { warn_percent: 80, alarm_percent: 90 }

  it('goes warn below the warn level and critical below the alarm level for a low-bad tank', () => {
    expect(tankTone('fuel', 25, fuel)).toBe('normal')
    expect(tankTone('fuel', 20, fuel)).toBe('normal')
    expect(tankTone('fuel', 19.9, fuel)).toBe('warn')
    expect(tankTone('fuel', 10, fuel)).toBe('warn')
    expect(tankTone('fuel', 9.9, fuel)).toBe('critical')
  })

  it('goes warn above the warn level and critical above the alarm level for waste', () => {
    expect(tankTone('waste', 80, waste)).toBe('normal')
    expect(tankTone('waste', 80.1, waste)).toBe('warn')
    expect(tankTone('waste', 90, waste)).toBe('warn')
    expect(tankTone('waste', 90.1, waste)).toBe('critical')
  })
})

describe('fuelBarZones', () => {
  it('draws the alarm band to the foot and the warn band above it', () => {
    expect(fuelBarZones('fuel', { warn_percent: 20, alarm_percent: 10 })).toEqual([
      { from: 0, to: 10, state: 'alarm' },
      { from: 10, to: 20, state: 'warn' },
    ])
  })
})

describe('tankLevelsError', () => {
  it('accepts a blank pair', () => {
    expect(tankLevelsError('fuel', null, null, { warn: 20, alarm: 10 })).toBeNull()
  })
  it('refuses figures outside 0-100', () => {
    expect(tankLevelsError('fuel', 120, null, { warn: 20, alarm: 10 })).toMatch(/0 and 100/)
  })
  it('refuses a low-bad tank whose alarm is not below its warn, counting blanks as the default', () => {
    expect(tankLevelsError('fuel', 8, null, { warn: 20, alarm: 10 })).toMatch(/lower than the warn/)
    expect(tankLevelsError('fuel', null, 25, { warn: 20, alarm: 10 })).toMatch(/lower than the warn/)
  })
  it('refuses a waste tank whose alarm is not above its warn', () => {
    expect(tankLevelsError('waste', 95, null, { warn: 80, alarm: 90 })).toMatch(/higher than the warn/)
  })
})

describe('tankClearText', () => {
  it('says above for a low-bad tank and below for waste, five points back', () => {
    expect(tankClearText('below', 0.25)).toBe('Clears above 25%')
    expect(tankClearText('above', 0.75)).toBe('Clears below 75%')
  })
})
