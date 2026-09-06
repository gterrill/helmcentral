import { describe, expect, it } from 'vitest'

import {
  DEFAULT_SOC_PATH,
  hoursToBand,
  socBandsFromRules,
  socSeverity,
  type SocBands,
} from '@/lib/soc-bands'
import type { AlarmRule } from '@/hooks/use-alarm-rules'

const SOC_PATH = DEFAULT_SOC_PATH

function rule(overrides: Partial<AlarmRule> = {}): AlarmRule {
  return {
    id: 'r1',
    enabled: true,
    path: SOC_PATH,
    label: 'SoC band',
    op: 'below',
    value: 0.2,
    hysteresis: 0,
    dwell_seconds: 0,
    stale_after_seconds: 0,
    state: 'warn',
    methods: [],
    notify: null,
    escalate_after_seconds: 0,
    ...overrides,
  }
}

describe('socBandsFromRules', () => {
  it('converts a ratio-valued below/warn rule on the pinned path to a percent warnBelow', () => {
    const bands = socBandsFromRules([rule({ value: 0.2, state: 'warn' })], SOC_PATH)
    expect(bands).toEqual({ warnBelow: 20, alarmBelow: null })
  })

  it('takes a below/alarm rule as alarmBelow', () => {
    const bands = socBandsFromRules([rule({ value: 0.1, state: 'alarm' })], SOC_PATH)
    expect(bands).toEqual({ warnBelow: null, alarmBelow: 10 })
  })

  it('treats a value already over 1 as already a percent, not a ratio', () => {
    const bands = socBandsFromRules([rule({ value: 20, state: 'warn' })], SOC_PATH)
    expect(bands.warnBelow).toBe(20)
  })

  it('combines a warn rule and an alarm rule on the same path into one band pair', () => {
    const bands = socBandsFromRules(
      [rule({ value: 0.2, state: 'warn' }), rule({ id: 'r2', value: 0.1, state: 'alarm' })],
      SOC_PATH,
    )
    expect(bands).toEqual({ warnBelow: 20, alarmBelow: 10 })
  })

  it('takes the higher value when both alarm and emergency rules exist', () => {
    const bands = socBandsFromRules(
      [rule({ id: 'r1', value: 0.1, state: 'alarm' }), rule({ id: 'r2', value: 0.15, state: 'emergency' })],
      SOC_PATH,
    )
    expect(bands.alarmBelow).toBe(15)
  })

  it('takes the highest threshold when several warn rules exist', () => {
    const bands = socBandsFromRules(
      [rule({ id: 'r1', value: 0.15, state: 'warn' }), rule({ id: 'r2', value: 0.25, state: 'warn' })],
      SOC_PATH,
    )
    expect(bands.warnBelow).toBe(25)
  })

  it('ignores a rule on a different path', () => {
    const bands = socBandsFromRules([rule({ path: 'electrical.batteries.1.capacity.stateOfCharge' })], SOC_PATH)
    expect(bands).toEqual({ warnBelow: null, alarmBelow: null })
  })

  it('ignores a rule with an op other than below', () => {
    const bands = socBandsFromRules([rule({ op: 'above' })], SOC_PATH)
    expect(bands).toEqual({ warnBelow: null, alarmBelow: null })
  })

  it('ignores a disabled rule', () => {
    const bands = socBandsFromRules([rule({ enabled: false })], SOC_PATH)
    expect(bands).toEqual({ warnBelow: null, alarmBelow: null })
  })

  it('ignores an alert-state rule, since only warn and alarm/emergency define bands', () => {
    const bands = socBandsFromRules([rule({ state: 'alert' })], SOC_PATH)
    expect(bands).toEqual({ warnBelow: null, alarmBelow: null })
  })

  it('returns both null when there are no rules', () => {
    expect(socBandsFromRules([], SOC_PATH)).toEqual({ warnBelow: null, alarmBelow: null })
  })
})

describe('socSeverity', () => {
  const bands: SocBands = { warnBelow: 20, alarmBelow: 10 }

  it('is null when soc is above the warn band', () => {
    expect(socSeverity(63, bands)).toBeNull()
  })

  it('is warn when soc is below warnBelow but above alarmBelow', () => {
    expect(socSeverity(18, bands)).toBe('warn')
  })

  it('is alarm when soc is below alarmBelow', () => {
    expect(socSeverity(8, bands)).toBe('alarm')
  })

  it('is null when soc is null regardless of bands', () => {
    expect(socSeverity(null, bands)).toBeNull()
  })

  it('is null at any soc when both bands are null', () => {
    expect(socSeverity(1, { warnBelow: null, alarmBelow: null })).toBeNull()
  })
})

describe('hoursToBand', () => {
  const bands: SocBands = { warnBelow: 20, alarmBelow: 10 }

  it('returns the warn band as the target when discharging above it', () => {
    const result = hoursToBand(63, -2.0, bands)
    expect(result).toEqual({ targetPercent: 20, hours: 21.5 })
  })

  it('returns the alarm band as the target once soc has dropped below the warn band', () => {
    const result = hoursToBand(18, -2.0, bands)
    expect(result?.targetPercent).toBe(10)
    expect(result?.hours).toBeCloseTo(4, 5)
  })

  it('returns null once soc is at or below the alarm band, meaning "to empty"', () => {
    expect(hoursToBand(8, -2.0, bands)).toBeNull()
  })

  it('returns null when charging, since bands only describe discharge', () => {
    expect(hoursToBand(63, 2.0, bands)).toBeNull()
  })

  it('returns null when the rate is null', () => {
    expect(hoursToBand(63, null, bands)).toBeNull()
  })

  it('returns null when soc is null', () => {
    expect(hoursToBand(null, -2.0, bands)).toBeNull()
  })

  it('returns null when both bands are null, even while discharging', () => {
    expect(hoursToBand(63, -2.0, { warnBelow: null, alarmBelow: null })).toBeNull()
  })
})
