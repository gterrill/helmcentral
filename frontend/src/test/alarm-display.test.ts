import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { ActiveAlarm } from '@/hooks/use-alarms'
import {
  alarmConditionSentence,
  alarmDisplayUnitId,
  formatAlarmReading,
  formatAlarmTime,
  FORECAST_SURF_WARNING_PATH,
  FORECAST_WIND_WARNING_PATH,
} from '@/lib/alarm-display'

function makeAlarm(overrides: Partial<ActiveAlarm> = {}): ActiveAlarm {
  return {
    rule_id: 'helmcentral:barometer-falling',
    label: 'Barometer falling',
    path: 'helmcentral.environment.pressureRate',
    phase: 'active',
    state: 'warn',
    value: -0.03,
    message: 'Barometer falling: -0.03 Pa/s, clears above -0.02 Pa/s',
    silenced: false,
    can_silence: false,
    can_acknowledge: true,
    ...overrides,
  }
}

describe('alarmDisplayUnitId', () => {
  // These are the units the backend contract promises (Pa/s, Pa, K, m/s,
  // ratio, Hz, m3, m3/s); each maps to what a sailor actually reads, not
  // the SI unit SignalK carries it in.
  it('maps each known SI unit to the operator unit', () => {
    expect(alarmDisplayUnitId('Pa/s')).toBe('mbph')
    expect(alarmDisplayUnitId('Pa')).toBe('mb')
    expect(alarmDisplayUnitId('K')).toBe('C')
    expect(alarmDisplayUnitId('m/s')).toBe('kn')
    expect(alarmDisplayUnitId('ratio')).toBe('percent')
    expect(alarmDisplayUnitId('Hz')).toBe('rpm')
    expect(alarmDisplayUnitId('m3')).toBe('L')
    expect(alarmDisplayUnitId('m3/s')).toBe('Lph')
  })

  it('uses the quantity default unit for anything else it recognises', () => {
    expect(alarmDisplayUnitId('V')).toBe('V')
    expect(alarmDisplayUnitId('A')).toBe('A')
  })

  it('returns null for an unknown or empty SI unit, meaning show the raw number', () => {
    expect(alarmDisplayUnitId('parsecs')).toBeNull()
    expect(alarmDisplayUnitId('')).toBeNull()
    expect(alarmDisplayUnitId(undefined)).toBeNull()
  })
})

describe('formatAlarmReading', () => {
  it('formats a falling barometer in mb/hr', () => {
    expect(formatAlarmReading(-0.03, 'Pa/s')).toBe('-1.1 mb/hr')
  })

  it('formats a voltage reading', () => {
    expect(formatAlarmReading(12.3, 'V')).toBe('12.3 V')
  })

  it('formats an unknown-unit reading as a bare number, no trailing space', () => {
    expect(formatAlarmReading(3.14, undefined)).toBe('3.14')
  })

  // The unknown-unit branch used to just be formatQuantity's own
  // fixed-decimals output, which pads a whole number to "-150.00". This is
  // the same rounded-and-trimmed rule the alarm rules list has always used
  // for a threshold with no known unit (formerly its own formatRuleValue),
  // so a repeating decimal reads at two places and a whole number reads
  // clean.
  it('rounds an unknown-unit reading to two decimals and trims trailing zeros', () => {
    expect(formatAlarmReading(-150)).toBe('-150')
    expect(formatAlarmReading(0.5)).toBe('0.5')
    expect(formatAlarmReading(-0.027777)).toBe('-0.03')
  })
})

describe('alarmConditionSentence', () => {
  it('uses sailor prose for a falling barometer (op below, unit Pa/s)', () => {
    const alarm = makeAlarm({ op: 'below', threshold: -0.03, hysteresis: 0.01, clear_value: -0.02, value: -0.0305, unit: 'Pa/s' })
    expect(alarmConditionSentence(alarm)).toBe('Falling 1.1 mb/hr. Clears once the fall eases to 0.7 mb/hr.')
  })

  it('uses a generic sentence for an above rule', () => {
    const alarm = makeAlarm({
      label: 'House bank low',
      path: 'electrical.batteries.house.voltage',
      op: 'above',
      threshold: 14.9,
      clear_value: 14.4,
      value: 14.9,
      unit: 'V',
    })
    expect(alarmConditionSentence(alarm)).toBe('Now 14.9 V. Clears below 14.4 V.')
  })

  it('uses a generic sentence for a below rule that is not the barometer special case', () => {
    const alarm = makeAlarm({
      label: 'House bank low',
      path: 'electrical.batteries.house.voltage',
      op: 'below',
      threshold: 11.8,
      clear_value: 12.0,
      value: 11.7,
      unit: 'V',
    })
    expect(alarmConditionSentence(alarm)).toBe('Now 11.7 V. Clears above 12.0 V.')
  })

  // A below rule on Pa/s is not automatically a falling barometer: the
  // sailor's-prose branch only applies when the value is actually negative
  // (an actual fall), not merely because the unit and op line up.
  it('uses the generic sentence for a below rule on Pa/s that is not actually a fall', () => {
    const alarm = makeAlarm({ op: 'below', threshold: 0.03, clear_value: 0.05, value: 0.03, unit: 'Pa/s' })
    expect(alarmConditionSentence(alarm)).toBe('Now 1.1 mb/hr. Clears above 1.8 mb/hr.')
  })

  it('states the value alone for equal/notEqual rules', () => {
    const alarm = makeAlarm({ op: 'equal', threshold: 0, value: 12.3, unit: 'V' })
    expect(alarmConditionSentence(alarm)).toBe('Now 12.3 V.')
  })

  it('reports no data for a stale rule', () => {
    const alarm = makeAlarm({ op: 'stale', value: 0, message: 'Bilge pump: no data for 45s', unit: undefined })
    expect(alarmConditionSentence(alarm)).toContain('No data.')
  })

  // The two forecast-warning derived paths (ADR 0087) carry a small ranked
  // integer that means nothing to a sailor as "now 2" - the meaning lives
  // entirely in which path raised and how far up its own ladder the value
  // sits, not in the number itself, so these get their own fixed sentences
  // rather than the generic "now X, clears above Y" phrasing every other
  // above/below rule falls through to. Unit is '' for both paths, which is
  // exactly why the branch keys on alarm.path instead.
  it('renders the wind ladder as plain warning language, keyed on the wind path', () => {
    expect(alarmConditionSentence(makeAlarm({
      path: FORECAST_WIND_WARNING_PATH,
      op: 'above',
      unit: '',
      value: 1,
    }))).toBe('Strong wind warning in force. Details on the Forecast page.')

    expect(alarmConditionSentence(makeAlarm({
      path: FORECAST_WIND_WARNING_PATH,
      op: 'above',
      unit: '',
      value: 2,
    }))).toBe('Gale warning in force. Details on the Forecast page.')

    expect(alarmConditionSentence(makeAlarm({
      path: FORECAST_WIND_WARNING_PATH,
      op: 'above',
      unit: '',
      value: 3,
    }))).toBe('Storm warning in force. Details on the Forecast page.')
  })

  // The rule evaluates in SI, so a reading can arrive off-integer even
  // though the backend only ever publishes 0-3; the sentence still has to
  // land on one of the four words, hence Math.round rather than a strict
  // equality ladder.
  it('rounds the wind level before choosing a word', () => {
    expect(alarmConditionSentence(makeAlarm({ path: FORECAST_WIND_WARNING_PATH, op: 'above', unit: '', value: 2.4 })))
      .toBe('Gale warning in force. Details on the Forecast page.')
    expect(alarmConditionSentence(makeAlarm({ path: FORECAST_WIND_WARNING_PATH, op: 'above', unit: '', value: 2.6 })))
      .toBe('Storm warning in force. Details on the Forecast page.')
    expect(alarmConditionSentence(makeAlarm({ path: FORECAST_WIND_WARNING_PATH, op: 'above', unit: '', value: 4 })))
      .toBe('Storm warning in force. Details on the Forecast page.')
  })

  it('renders the surf warning in plain language, keyed on the surf path', () => {
    expect(alarmConditionSentence(makeAlarm({
      path: FORECAST_SURF_WARNING_PATH,
      op: 'above',
      unit: '',
      value: 1,
    }))).toBe('Surf warning in force. Details on the Forecast page.')
  })

  // "Forecast warnings unavailable" is a stale rule on the wind path, not a
  // third path, so it has to stay caught by the op === 'stale' branch above
  // rather than falling into the path-keyed forecast branch, which would
  // print a warning-level word for a reading that isn't there.
  it('still reports no data for a stale rule on the forecast wind path', () => {
    const alarm = makeAlarm({
      path: FORECAST_WIND_WARNING_PATH,
      op: 'stale',
      unit: '',
      value: 0,
      message: 'Forecast warnings unavailable: no data for 32m',
    })
    expect(alarmConditionSentence(alarm)).toContain('No data.')
  })

  // The bus message has no terminal punctuation of its own (SignalK builds
  // it as a fragment), so run straight into "Clears..." reads as one
  // run-on sentence. A missing period gets exactly one added.
  it('appends the message and the clears-when-the-source-clears line for a bus notification, adding a period the message lacks', () => {
    const alarm = makeAlarm({
      op: undefined,
      unit: undefined,
      message: 'Radar guard zone 1: target 100000294 acquired',
    })
    expect(alarmConditionSentence(alarm)).toBe('Radar guard zone 1: target 100000294 acquired. Clears when the source clears it.')
  })

  it('does not add a second period when the bus message already ends with one', () => {
    const alarm = makeAlarm({
      op: undefined,
      unit: undefined,
      message: 'Radar guard zone 1: target 100000294 acquired.',
    })
    expect(alarmConditionSentence(alarm)).toBe('Radar guard zone 1: target 100000294 acquired. Clears when the source clears it.')
  })

  it('does not add a period when the bus message already ends with ! or ?', () => {
    expect(alarmConditionSentence(makeAlarm({ op: undefined, unit: undefined, message: 'Anchor dragging!' })))
      .toBe('Anchor dragging! Clears when the source clears it.')
    expect(alarmConditionSentence(makeAlarm({ op: undefined, unit: undefined, message: 'Anchor dragging?' })))
      .toBe('Anchor dragging? Clears when the source clears it.')
  })
})

describe('formatAlarmTime', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders HH:MM when the timestamp is today', () => {
    vi.setSystemTime(new Date('2026-09-06T18:00:00'))
    expect(formatAlarmTime('2026-09-06T05:41:00')).toBe('05:41')
  })

  it('renders D Mon HH:MM when the timestamp is not today', () => {
    vi.setSystemTime(new Date('2026-09-06T18:00:00'))
    expect(formatAlarmTime('2026-08-30T05:41:00')).toBe('30 Aug 05:41')
  })

  it('returns null for missing or invalid input', () => {
    expect(formatAlarmTime(undefined)).toBeNull()
    expect(formatAlarmTime('not a date')).toBeNull()
  })
})
