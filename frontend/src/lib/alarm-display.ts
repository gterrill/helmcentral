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

// The Law of Storms barometer paths (ADR 0095). The three tendency paths
// carry unit Pa (mb through ALARM_UNIT_OVERRIDES above), the two index
// paths carry unit '' like the forecast paths do, so they get the same
// path-keyed treatment rather than the generic above/below phrasing.
export const PRESSURE_CHANGE_3H_PATH = 'helmcentral.environment.pressureChange3h'
export const PRESSURE_CHANGE_12H_PATH = 'helmcentral.environment.pressureChange12h'
export const PRESSURE_CHANGE_24H_PATH = 'helmcentral.environment.pressureChange24h'
export const STORM_INDEX_PATH = 'helmcentral.environment.stormIndex'
export const SEVERE_THUNDERSTORM_INDEX_PATH = 'helmcentral.environment.severeThunderstormIndex'

// The three sensor-health count paths (anomaly_detector.go): a count alarm
// on its own gives the operator nothing to act on, so its evidence names
// the offending SignalK paths or $source ids (comma-separated) - the alarm
// card's "Ignore this sensor" action splits that list back out.
export const ANOMALY_SENSOR_FROZEN_COUNT_PATH = 'helmcentral.anomaly.sensor.frozenCount'
export const ANOMALY_SENSOR_OUT_OF_RANGE_COUNT_PATH = 'helmcentral.anomaly.sensor.outOfRangeCount'
export const ANOMALY_SENSOR_SILENT_SOURCE_COUNT_PATH = 'helmcentral.anomaly.sensor.silentSourceCount'
const IGNORABLE_SENSOR_PATHS: readonly string[] = [
  ANOMALY_SENSOR_FROZEN_COUNT_PATH,
  ANOMALY_SENSOR_OUT_OF_RANGE_COUNT_PATH,
  ANOMALY_SENSOR_SILENT_SOURCE_COUNT_PATH,
]

/**
 * The identifiers (SignalK paths or $source ids) an "Ignore this sensor"
 * action could offer for this alarm, or [] when it isn't one of the three
 * sensor-health count alarms, or when nothing is currently flagged
 * (live_evidence absent - the count is 0 right now, so there's nothing to
 * ignore).
 *
 * Reads `live_evidence`, not the alarm's own `evidence` -- `evidence` is
 * frozen at the moment the alarm first raised, while a count alarm like
 * this one can stay continuously active for a long time as its specific
 * offenders drift (one sensor recovers, another starts failing, and the
 * count itself never drops enough to clear and re-raise). Offering to
 * ignore whatever tripped the alarm originally, rather than whatever is
 * actually failing now, would be offering the wrong sensor.
 */
export function ignorableSensorIdentifiers(alarm: ActiveAlarm): string[] {
  if (!IGNORABLE_SENSOR_PATHS.includes(alarm.path)) return []
  const evidence = (alarm.live_evidence ?? '').trim()
  if (evidence === '') return []
  return evidence.split(',').map((s) => s.trim()).filter((s) => s.length > 0)
}

// The English word for each tendency path's window, used in the card
// sentence rather than the path's own camelCase name.
const TENDENCY_WINDOW_WORDS: Record<string, string> = {
  [PRESSURE_CHANGE_3H_PATH]: 'three hours',
  [PRESSURE_CHANGE_12H_PATH]: 'twelve hours',
  [PRESSURE_CHANGE_24H_PATH]: 'twenty-four hours',
}

const ALARM_UNIT_OVERRIDES: Record<string, string> = {
  'Pa/s': 'mbph',
  Pa: 'mb',
  K: 'C',
  'm/s': 'kn',
  ratio: 'percent',
  Hz: 'rpm',
  m3: 'L',
  'm3/s': 'Lph',
  // deltaK/deltaPa (the twin-engine differential detector's residual paths)
  // route to their own difference quantities so a residual never gains an
  // absolute unit's offset -- see quantities.ts's temperatureDelta/
  // pressureDelta.
  deltaK: 'deltaC',
  deltaPa: 'deltaKPa',
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
 *
 * `linked` is true when the caller is rendering a real bulletin link
 * (ADR 0114, `forecastWarningDetailsUrl`) right next to this sentence; the
 * trailing "Details on the Forecast page." clause is then dropped, because
 * pointing at the page is redundant with a link straight to the bulletin.
 */
function forecastWarningSentence(alarm: ActiveAlarm, linked: boolean): string | null {
  const detail = linked ? '' : ' Details on the Forecast page.'
  if (alarm.path === FORECAST_SURF_WARNING_PATH) {
    return `Surf warning in force.${detail}`
  }
  if (alarm.path === FORECAST_WIND_WARNING_PATH) {
    const level = Math.round(alarm.value)
    if (level >= 3) return `Storm warning in force.${detail}`
    if (level === 2) return `Gale warning in force.${detail}`
    return `Strong wind warning in force.${detail}`
  }
  return null
}

/**
 * The card sentence for the five Law of Storms barometer paths (ADR 0095),
 * or null for every other path. The two index paths are unitless 0/1
 * signature flags, exactly like the forecast ladder above, so they get
 * fixed prose keyed on the path. The three tendency paths carry a Pa
 * reading of a rise or fall over a fixed window; "now 6.0 mb" says nothing
 * about which direction the barometer is moving or over what window, so
 * this reads it as "Up"/"Down" over the window word instead. The sign of
 * value has to actually agree with the op - an above rule reads a rise, so
 * it needs value > 0, and a below rule reads a fall, so it needs value < 0 -
 * the same guard the Pa/s falling-barometer special case above applies, or
 * a below rule holding a stale positive reading would get "Down" language
 * for something that isn't falling. clear_value, when the rule sets one, is
 * held to the same side of zero as value: a clear point that crossed zero
 * would mean the rule clears on the opposite phenomenon from the one it
 * alarms on, which is not a shape any of the seeded rules produce.
 *
 * The index sentences below duplicate the backend's own numbers
 * (weather_trend.go's lawOfStormsStormFall3hPa etc.), the same trade the
 * forecast sentences above make with the forecast ladder's thresholds.
 */
function lawOfStormsSentence(alarm: ActiveAlarm): string | null {
  if (alarm.path === STORM_INDEX_PATH) {
    return 'Storm signature: barometer down 4 mb in three hours below 1009 mb.'
  }
  if (alarm.path === SEVERE_THUNDERSTORM_INDEX_PATH) {
    return 'Severe thunderstorm signature: barometer down 4 mb in three hours and 8 mb in twelve hours below 1005 mb.'
  }

  const windowWord = TENDENCY_WINDOW_WORDS[alarm.path]
  if (windowWord === undefined) return null

  const { op, unit, value, clear_value: clearValue } = alarm
  if (unit !== 'Pa' || (op !== 'above' && op !== 'below')) return null

  const rising = op === 'above'
  if (rising ? value <= 0 : value >= 0) return null
  if (clearValue !== undefined && (rising ? clearValue <= 0 : clearValue >= 0)) return null

  const direction = rising ? 'Up' : 'Down'
  const magnitude = formatAlarmReading(Math.abs(value), unit)
  const sentence = `${direction} ${magnitude} in ${windowWord}.`
  if (clearValue === undefined) return sentence

  const verb = rising ? 'rise' : 'fall'
  const clears = formatAlarmReading(Math.abs(clearValue), unit)
  return `${sentence} Clears once the ${verb} eases to ${clears}.`
}

/**
 * The operator-facing sentence for an alarm card: what it is doing now, and
 * what will clear it. A rule alarm renders its own sentence from the
 * structured fields; a bus notification (no rule behind it, so no op) has
 * only its message, plus a note that it clears on its own.
 *
 * `options.forecastDetailsLinked` (ADR 0114) only affects the two forecast
 * sentences below; every other branch ignores it. Every existing caller
 * omits the options argument, which keeps its default behaviour.
 */
export function alarmConditionSentence(alarm: ActiveAlarm, options?: { forecastDetailsLinked?: boolean }): string {
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
  const forecastSentence = forecastWarningSentence(alarm, options?.forecastDetailsLinked === true)
  if (forecastSentence !== null) return forecastSentence

  // Same reasoning, same ordering: a stale Law of Storms rule was already
  // caught by the op === 'stale' branch above, so this only ever sees a
  // live reading.
  const lawOfStormsSentenceResult = lawOfStormsSentence(alarm)
  if (lawOfStormsSentenceResult !== null) return lawOfStormsSentenceResult

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
