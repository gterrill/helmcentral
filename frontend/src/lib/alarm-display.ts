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
    return formatQuantity(value, 'raw', 'raw') ?? String(value)
  }

  const quantity = quantityForSIUnit(siUnit)
  const formatted = formatQuantity(value, quantity.id, unitId)
  if (formatted === null) return String(value)

  const unit = unitOption(quantity.id, unitId)
  return unit.label ? `${formatted} ${unit.label}` : formatted
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
    return `${message} Clears when the source clears it.`
  }

  if (op === 'stale') {
    const trimmedMessage = message?.trim()
    const extra = trimmedMessage && trimmedMessage !== 'No data.' ? ` ${trimmedMessage}` : ''
    return `No data.${extra}`
  }

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
