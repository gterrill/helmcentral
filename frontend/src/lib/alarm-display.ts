/**
 * Turns the raw ActiveAlarm fields (SI value, SI unit, rule op) into the
 * sentence and time strings the Active Alarms card renders (PFD fix 1).
 *
 * The rule itself evaluates in SI, so a barometer rule fires at -0.03 Pa/s,
 * which is meaningless to a sailor who thinks in mb/hr. This module owns the
 * one place that translates a rule condition into operator language, so the
 * card and the banner say the same thing.
 */
import type { ActiveAlarm } from '@/hooks/use-alarms'
import { formatQuantity, quantityForSIUnit, unitOption } from '@/lib/quantities'

// SI unit -> the unit id an alarm reading is shown in. Not every quantity's
// own first unit is what an alarm should show (pressure's first unit is
// kPa, an alarm shows mb), so this table is deliberately separate from
// quantityForSIUnit's own unit ordering.
// The two forecast-warning derived paths (ADR 0087). Both carry unit '',
// so a rule on either can't be told apart from the SI unit the way the
// falling-barometer special case below is - the card sentence has to key
// on the path itself.
export const FORECAST_WIND_WARNING_PATH = 'helmcentral.environment.forecastWindWarningLevel'
export const FORECAST_SURF_WARNING_PATH = 'helmcentral.environment.forecastSurfWarning'

const ALARM_UNIT_OVERRIDES: Record<string, string> = {
  'Pa/s': 'mbph',
  Pa: 'mb',
  K: 'C',
  'm/s': 'kn',
  ratio: 'percent',
  Hz: 'rpm',
  m3: 'L',
  'm3/s': 'Lph',
}

/**
 * The unit id an alarm reading in this SI unit is shown in. Null means the
 * SI unit is unknown or absent, so the caller shows a bare number.
 */
export function alarmDisplayUnitId(siUnit?: string): string | null {
  const trimmed = (siUnit ?? '').trim()
  if (trimmed === '') return null
  if (trimmed in ALARM_UNIT_OVERRIDES) return ALARM_UNIT_OVERRIDES[trimmed]

  const quantity = quantityForSIUnit(trimmed)
  if (quantity.id === 'raw') return null
  return quantity.units[0].id
}

/** e.g. "-1.1 mb/hr", "12.3 V", or "3.14" with no unit when none is known. */
export function formatAlarmReading(value: number, siUnit?: string): string {
  const unitId = alarmDisplayUnitId(siUnit)
  if (unitId === null) {
    // No unit is known, so this is a bare number rather than a converted
    // reading. It is rounded to two decimals and trimmed of trailing zeros,
    // the same rule the rules list has always applied to an unconverted
    // threshold, so a whole number reads "-150" rather than "-150.00".
    const formatted = formatQuantity(value, 'raw', 'raw')
    if (formatted === null) return String(value)
    return Number(formatted).toString()
  }

  const quantity = quantityForSIUnit(siUnit)
  const formatted = formatQuantity(value, quantity.id, unitId)
  if (formatted === null) return String(value)

  const unit = unitOption(quantity.id, unitId)
  return unit.label ? `${formatted} ${unit.label}` : formatted
}

/**
 * The card sentence for the two forecast-warning derived paths, or null for
 * every other path. Both paths carry a small ranked integer (wind 0-3, surf
 * 0-1) that is meaningless to a sailor as a bare number - "now 2" says
 * nothing about what is happening - so the sentence is fixed warning
 * language keyed on which path raised, not the generic "now X, clears above
 * Y" phrasing every other above/below rule gets. Math.round guards against
 * an off-integer reading (the rule evaluates in SI, so it can land between
 * whole numbers even though the backend only ever publishes 0-3) landing
 * between two words instead of on one of them.
 */
function forecastWarningSentence(alarm: ActiveAlarm): string | null {
  if (alarm.path === FORECAST_SURF_WARNING_PATH) {
    return 'Surf warning in force. Details on the Forecast page.'
  }
  if (alarm.path === FORECAST_WIND_WARNING_PATH) {
    const level = Math.round(alarm.value)
    if (level >= 3) return 'Storm warning in force. Details on the Forecast page.'
    if (level === 2) return 'Gale warning in force. Details on the Forecast page.'
    return 'Strong wind warning in force. Details on the Forecast page.'
  }
  return null
}

/**
 * The operator-facing sentence for an alarm card: what it is doing now, and
 * what will clear it. A rule alarm renders its own sentence from the
 * structured fields; a bus notification (no rule behind it, so no op) has
 * only its message, plus a note that it clears on its own.
 */
export function alarmConditionSentence(alarm: ActiveAlarm): string {
  const { op, unit, value, clear_value: clearValue, message } = alarm

  if (op === undefined) {
    // SignalK builds this message as a fragment with no terminal
    // punctuation, so running straight into "Clears..." reads as one
    // run-on sentence. Add the period it is missing; don't double up one
    // it already has.
    const sentence = /[.!?]$/.test(message) ? message : `${message}.`
    return `${sentence} Clears when the source clears it.`
  }

  if (op === 'stale') {
    const trimmedMessage = message?.trim()
    const extra = trimmedMessage && trimmedMessage !== 'No data.' ? ` ${trimmedMessage}` : ''
    return `No data.${extra}`
  }

  // Forecast wind warnings on 'stale' fall through the branch above (a
  // dead provider still reads "No data."); a live reading on either
  // forecast path gets its own sentence here, ahead of every other
  // op-based branch below, none of which know what a bare 0-3 or 0-1
  // integer is supposed to mean.
  const forecastSentence = forecastWarningSentence(alarm)
  if (forecastSentence !== null) return forecastSentence

  if (op === 'equal' || op === 'notEqual') {
    return `Now ${formatAlarmReading(value, unit)}.`
  }

  // op is 'above' or 'below' here. A falling barometer gets the sailor's own
  // words rather than the generic "now X, clears above Y" phrasing. That is
  // the whole point of the rewrite: a millibar-per-hour fall rate read as
  // "now -1.1" tells nobody anything. Unit and op alone are not enough to
  // call this a fall though: the value (and, if the rule sets one, the
  // clear point) has to actually be negative, or a below-Pa/s rule with a
  // positive threshold would get "falling" language for a reading that
  // isn't falling at all.
  const isFallingBarometer = unit === 'Pa/s' && op === 'below' && value < 0
    && (clearValue === undefined || clearValue <= 0)
  if (isFallingBarometer) {
    const falling = formatAlarmReading(Math.abs(value), unit)
    if (clearValue === undefined) return `Falling ${falling}.`
    const clears = formatAlarmReading(Math.abs(clearValue), unit)
    return `Falling ${falling}. Clears once the fall eases to ${clears}.`
  }

  const now = formatAlarmReading(value, unit)
  if (clearValue === undefined) return `Now ${now}.`
  // 'below' fires when the value drops under threshold, so it clears once
  // the value climbs back above clear_value, and the reverse for 'above'.
  const direction = op === 'below' ? 'above' : 'below'
  const clears = formatAlarmReading(clearValue, unit)
  return `Now ${now}. Clears ${direction} ${clears}.`
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

/** "HH:MM" for today, "D Mon HH:MM" otherwise. Null for missing/invalid input. */
export function formatAlarmTime(iso?: string): string | null {
  if (!iso) return null
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return null

  const now = new Date()
  const isToday = date.getFullYear() === now.getFullYear()
    && date.getMonth() === now.getMonth()
    && date.getDate() === now.getDate()

  const hh = String(date.getHours()).padStart(2, '0')
  const mm = String(date.getMinutes()).padStart(2, '0')
  const time = `${hh}:${mm}`

  return isToday ? time : `${date.getDate()} ${MONTHS[date.getMonth()]} ${time}`
}
