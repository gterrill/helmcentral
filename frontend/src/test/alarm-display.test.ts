import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { ActiveAlarm } from '@/hooks/use-alarms'
import {
  alarmConditionSentence,
  alarmDisplayUnitId,
  formatAlarmReading,
  formatAlarmTime,
  FORECAST_SURF_WARNING_PATH,
  FORECAST_WIND_WARNING_PATH,
  PRESSURE_CHANGE_3H_PATH,
  PRESSURE_CHANGE_12H_PATH,
  PRESSURE_CHANGE_24H_PATH,
  SEVERE_THUNDERSTORM_INDEX_PATH,
  STORM_INDEX_PATH,
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

  // deltaK/deltaPa are the twin-engine differential detector's residual
  // units. They must resolve through their own difference quantities, not
  // through 'temperature'/'pressure', or a residual would gain K's -273.15
  // offset it must never carry.
  it('maps the difference units to their own operator units', () => {
    expect(alarmDisplayUnitId('deltaK')).toBe('deltaC')
    expect(alarmDisplayUnitId('deltaPa')).toBe('deltaKPa')
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

  it('formats a temperature residual with no K offset', () => {
    expect(formatAlarmReading(4, 'deltaK')).toBe('4.0 °C')
    expect(formatAlarmReading(-4, 'deltaK')).toBe('-4.0 °C')
  })

  it('formats a pressure residual in kPa, not mb', () => {
    expect(formatAlarmReading(50000, 'deltaPa')).toBe('50.0 kPa')
    expect(formatAlarmReading(-15000, 'deltaPa')).toBe('-15.0 kPa')
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
  // ADR 0114: when the caller is rendering a real bulletin link next to the
  // sentence, the trailing "Details on the Forecast page." clause is
  // redundant and drops out. Every other caller (and every assertion above
  // this one) keeps calling alarmConditionSentence with no options argument
  // at all, and its behaviour is unchanged.
  describe('forecastDetailsLinked option', () => {
    it('drops the Forecast-page clause from the wind ladder sentences when linked', () => {
      expect(alarmConditionSentence(
        makeAlarm({ path: FORECAST_WIND_WARNING_PATH, op: 'above', unit: '', value: 1 }),
        { forecastDetailsLinked: true },
      )).toBe('Strong wind warning in force.')

      expect(alarmConditionSentence(
        makeAlarm({ path: FORECAST_WIND_WARNING_PATH, op: 'above', unit: '', value: 2 }),
        { forecastDetailsLinked: true },
      )).toBe('Gale warning in force.')

      expect(alarmConditionSentence(
        makeAlarm({ path: FORECAST_WIND_WARNING_PATH, op: 'above', unit: '', value: 3 }),
        { forecastDetailsLinked: true },
      )).toBe('Storm warning in force.')
    })

    it('drops the Forecast-page clause from the surf warning sentence when linked', () => {
      expect(alarmConditionSentence(
        makeAlarm({ path: FORECAST_SURF_WARNING_PATH, op: 'above', unit: '', value: 1 }),
        { forecastDetailsLinked: true },
      )).toBe('Surf warning in force.')
    })

    it('keeps the Forecast-page clause when forecastDetailsLinked is false', () => {
      expect(alarmConditionSentence(
        makeAlarm({ path: FORECAST_WIND_WARNING_PATH, op: 'above', unit: '', value: 2 }),
        { forecastDetailsLinked: false },
      )).toBe('Gale warning in force. Details on the Forecast page.')
    })

    it('has no effect on a non-forecast sentence', () => {
      const alarm = makeAlarm({
        label: 'House bank low',
        path: 'electrical.batteries.house.voltage',
        op: 'above',
        threshold: 14.9,
        clear_value: 14.4,
        value: 14.9,
        unit: 'V',
      })
      expect(alarmConditionSentence(alarm, { forecastDetailsLinked: true })).toBe('Now 14.9 V. Clears below 14.4 V.')
    })
  })

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

  // Law of Storms tendency rules (ADR 0095) fire on a Pa reading of a
  // pressureChangeNh derived path, so "now 6.0 mb" says nothing about
  // whether the barometer is rising or falling on that window. Pa already
  // maps to mb through ALARM_UNIT_OVERRIDES, so only the sailor's-words
  // wrapper is new here, same trade as the Pa/s falling-barometer case
  // above.
  it('reads a rising three-hour tendency rule with its clear point', () => {
    const alarm = makeAlarm({
      label: 'Barometer up 6 mb in three hours',
      path: PRESSURE_CHANGE_3H_PATH,
      op: 'above',
      unit: 'Pa',
      value: 600,
      clear_value: 550,
    })
    expect(alarmConditionSentence(alarm)).toBe('Up 6.0 mb in three hours. Clears once the rise eases to 5.5 mb.')
  })

  it('reads a falling twelve-hour tendency rule with no clear point configured', () => {
    const alarm = makeAlarm({
      label: 'Barometer down 8 mb in twelve hours',
      path: PRESSURE_CHANGE_12H_PATH,
      op: 'below',
      unit: 'Pa',
      value: -800,
    })
    expect(alarmConditionSentence(alarm)).toBe('Down 8.0 mb in twelve hours.')
  })

  it('reads the weather-bomb twenty-four-hour tendency rule with its clear point', () => {
    const alarm = makeAlarm({
      label: 'Weather bomb',
      path: PRESSURE_CHANGE_24H_PATH,
      op: 'below',
      unit: 'Pa',
      value: -2400,
      clear_value: -2300,
    })
    expect(alarmConditionSentence(alarm)).toBe('Down 24.0 mb in twenty-four hours. Clears once the fall eases to 23.0 mb.')
  })

  // A below rule on a tendency path is not automatically a fall, the same
  // guard as the Pa/s falling-barometer case: the value has to actually be
  // negative, or a below rule holding a positive reading (which can happen
  // transiently while the rule is still latched from an earlier fall) would
  // get "Down" language for a reading that is not falling at all. It falls
  // through to the ordinary above/below sentence instead.
  it('falls through to the generic sentence when a below tendency rule holds a positive value', () => {
    const alarm = makeAlarm({
      label: 'Barometer down 6 mb in three hours',
      path: PRESSURE_CHANGE_3H_PATH,
      op: 'below',
      unit: 'Pa',
      value: 600,
      clear_value: 650,
    })
    expect(alarmConditionSentence(alarm)).toBe('Now 6.0 mb. Clears above 6.5 mb.')
  })

  // The storm and severe-thunderstorm signatures are unitless 0/1 flags
  // (the same shape as the forecast ladder above), so the sentence is fixed
  // prose keyed on the path rather than the generic phrasing. The numbers
  // in the prose duplicate the Go constants in weather_trend.go, the same
  // trade the forecast sentences make with their own backend thresholds.
  it('renders the storm-signature sentence, keyed on the storm index path', () => {
    const alarm = makeAlarm({
      label: 'Storm signature',
      path: STORM_INDEX_PATH,
      op: 'above',
      unit: '',
      value: 1,
    })
    expect(alarmConditionSentence(alarm)).toBe('Storm signature: barometer down 4 mb in three hours below 1009 mb.')
  })

  it('renders the severe-thunderstorm-signature sentence, keyed on the severe index path', () => {
    const alarm = makeAlarm({
      label: 'Severe thunderstorm signature',
      path: SEVERE_THUNDERSTORM_INDEX_PATH,
      op: 'above',
      unit: '',
      value: 1,
    })
    expect(alarmConditionSentence(alarm))
      .toBe('Severe thunderstorm signature: barometer down 4 mb in three hours and 8 mb in twelve hours below 1005 mb.')
  })

  // A stale rule has to still read "No data." on a tendency path: the
  // op === 'stale' branch sits above the Law of Storms check for exactly
  // this reason, the same ordering the forecast paths rely on.
  it('still reports no data for a stale rule on a tendency path', () => {
    const alarm = makeAlarm({
      path: PRESSURE_CHANGE_3H_PATH,
      op: 'stale',
      unit: 'Pa',
      value: 0,
      message: 'Barometer tendency: no data for 40m',
    })
    expect(alarmConditionSentence(alarm)).toContain('No data.')
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
