/**
 * Turns the raw ActiveAlarm fields (SI value, SI unit, rule op) into the
 * sentence and time strings the Active Alarms card renders (PFD fix 1).
 *
 * The rule itself evaluates in SI, so a barometer rule fires at -0.03 Pa/s,
 * which is meaningless to a sailor who thinks in mb/hr. This module owns the
 * one place that translates a rule condition into operator language, so the
 * card and the banner say the same thing.
 */
import type { ActiveAlarm, SensorHealthEntry } from '@/hooks/use-alarms'
import { formatQuantity, quantityForSIUnit, unitOption } from '@/lib/quantities'
import { tankClearText } from '@/lib/tank-levels'

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
// the offending sensors, one named line each (live_sensors), and the ignore
// action submits the raw identifier carried beside each line.
export const ANOMALY_SENSOR_FROZEN_COUNT_PATH = 'helmcentral.anomaly.sensor.frozenCount'
export const ANOMALY_SENSOR_OUT_OF_RANGE_COUNT_PATH = 'helmcentral.anomaly.sensor.outOfRangeCount'
export const ANOMALY_SENSOR_SILENT_SOURCE_COUNT_PATH = 'helmcentral.anomaly.sensor.silentSourceCount'
const IGNORABLE_SENSOR_PATHS: readonly string[] = [
  ANOMALY_SENSOR_FROZEN_COUNT_PATH,
  ANOMALY_SENSOR_OUT_OF_RANGE_COUNT_PATH,
  ANOMALY_SENSOR_SILENT_SOURCE_COUNT_PATH,
]

export function isSensorHealthPath(path: string): boolean {
  return IGNORABLE_SENSOR_PATHS.includes(path)
}

function isSensorHealthAlarm(alarm: ActiveAlarm): boolean {
  return isSensorHealthPath(alarm.path)
}

/**
 * The named lines for a sensor-health count alarm: what is failing right now,
 * in the words the server wrote (backend/sensor_names.go). [] for any other
 * alarm, or when nothing is failing at this moment.
 *
 * Reads `live_sensors`, not `sensors` (the set at raise): a count alarm can
 * stay active for a long time while its specific offenders drift, and showing
 * or offering to ignore whatever tripped it first would name the wrong sensor.
 */
export function sensorHealthLines(alarm: ActiveAlarm): SensorHealthEntry[] {
  if (!isSensorHealthAlarm(alarm)) return []
  return alarm.live_sensors ?? []
}

/**
 * The sensors an "Ignore" action could offer for this alarm: the identifier
 * to submit (a SignalK path or $source id, never shown) and the label the
 * button reads.
 */
export function ignorableSensors(alarm: ActiveAlarm): { identifier: string; label: string }[] {
  return sensorHealthLines(alarm).map((e) => ({ identifier: e.identifier, label: e.label }))
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

  const tendency = tendencyReading(alarm)
  if (tendency === null) return null
  const { rising, windowWord, magnitudePa } = tendency
  const sentence = `${rising ? 'Up' : 'Down'} ${formatAlarmReading(magnitudePa, 'Pa')} in ${windowWord}.`
  const meaning = tendencyMeaning(alarm.path, rising, tierMagnitudePa(alarm, rising, magnitudePa))
  return meaning === null ? sentence : `${sentence} ${meaning}`
}

/**
 * The magnitude the meaning tier is read from: the reading as the card shows
 * it (to 0.1 mb, so "10.0 mb" never sits beside the 6 mb tier's wind), or the
 * rule's own threshold if that is higher, so a latched gale rule easing inside
 * its hysteresis band still reads as a gale under its gale title.
 */
function tierMagnitudePa(alarm: ActiveAlarm, rising: boolean, magnitudePa: number): number {
  const shown = Math.round(magnitudePa / 10) * 10
  const { threshold } = alarm
  if (threshold === undefined || (rising ? threshold <= 0 : threshold >= 0)) return shown
  return Math.max(shown, Math.abs(threshold))
}

/**
 * The live reading of a tendency alarm, or null when the alarm is not one or
 * its signs disagree (see lawOfStormsSentence).
 */
function tendencyReading(alarm: ActiveAlarm): { rising: boolean; windowWord: string; magnitudePa: number } | null {
  const windowWord = TENDENCY_WINDOW_WORDS[alarm.path]
  if (windowWord === undefined) return null

  const { op, unit, value, clear_value: clearValue } = alarm
  if (unit !== 'Pa' || (op !== 'above' && op !== 'below')) return null

  const rising = op === 'above'
  if (rising ? value <= 0 : value >= 0) return null
  if (clearValue !== undefined && (rising ? clearValue <= 0 : clearValue >= 0)) return null
  return { rising, windowWord, magnitudePa: Math.abs(value) }
}

// R. J. Ellis's tiers, the same figures alarm_seed_law_of_storms.go seeds
// its rules at. Retune one there and retune it here.
const PA_PER_MB = 100
const STRONG_WIND_3H_PA = 6 * PA_PER_MB
const GALE_3H_PA = 10 * PA_PER_MB
const WEATHER_BOMB_24H_PA = 24 * PA_PER_MB

/**
 * What a tendency move means for the weather, keyed on tierMagnitudePa so a
 * custom rule gets the tier its reading is actually in, or none. Wind ranges
 * are in knots: alarm readings have no wind-unit preference.
 */

function tendencyMeaning(path: string, rising: boolean, magnitudePa: number): string | null {
  if (path === PRESSURE_CHANGE_24H_PATH) {
    if (rising || magnitudePa < WEATHER_BOMB_24H_PA) return null
    return 'A low deepening this fast is a weather bomb: expect storm-force wind, 48 kn and over.'
  }
  if (path !== PRESSURE_CHANGE_3H_PATH || magnitudePa < STRONG_WIND_3H_PA) return null
  const lead = rising
    ? 'A rise this fast brings wind, not fair weather'
    : 'A fall this fast means a low closing in'
  return magnitudePa >= GALE_3H_PA
    ? `${lead}: expect a gale, 34 to 47 kn.`
    : `${lead}: expect 26 to 33 kn.`
}

/** The footer clause for a tendency alarm's clear point, or null. */
function tendencyClearText(alarm: ActiveAlarm): string | null {
  const tendency = tendencyReading(alarm)
  if (tendency === null || alarm.clear_value === undefined) return null
  const clears = formatAlarmReading(Math.abs(alarm.clear_value), 'Pa')
  return `Clears once the ${tendency.rising ? 'rise' : 'fall'} eases to ${clears}`
}

const METRES_PER_NM = 1852

/** "Target in guard zone 1." + figures -> "Target in guard zone 1 · 042°T · 1.4 NM · CPA 0.3 NM". */
function withRadarFigures(sentence: string, target: ActiveAlarm['radar_target']): string {
  if (!target) return sentence
  const degrees = Math.round(((target.bearing_rad * 180) / Math.PI + 360) % 360) % 360
  const parts = [
    `${String(degrees).padStart(3, '0')}°T`,
    `${(target.range_m / METRES_PER_NM).toFixed(1)} NM`,
  ]
  if (target.cpa_m !== undefined) parts.push(`CPA ${(target.cpa_m / METRES_PER_NM).toFixed(1)} NM`)
  return [sentence.replace(/\.$/, ''), ...parts].join(' · ')
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

  // A tank level alarm (ADR 0214) reads what the tank holds, "19% · 228 L", in
  // the words the server sets for every transport (one presenter, ADR 0157);
  // how it clears is the footer's (alarmFooterText).
  if (isTankAlarm(alarm)) return message

  if (op === undefined) {
    // An alarm with no rule behind it: a bus notification (the backend has
    // already stripped internal ids) or a source like the stream watchdog,
    // which sends its message raw. Nothing here knows how the producer
    // clears it, so no clearing claim is made. Live radar figures, when the
    // target is known, follow the sentence.
    const text = (message ?? '').trim().replace(/[.!?]+$/, '')
    return withRadarFigures(text === '' ? '' : `${text}.`, alarm.radar_target)
  }

  if (op === 'stale') {
    const trimmedMessage = message?.trim()
    const extra = trimmedMessage && trimmedMessage !== 'No data.' ? ` ${trimmedMessage}` : ''
    return `No data.${extra}`
  }

  // The three sensor-health count alarms read as one line per failing sensor,
  // never "Now 1. Clears below 0.5." -- a count tells the watchkeeper nothing.
  if (isSensorHealthAlarm(alarm)) {
    return sensorHealthLines(alarm).map((e) => e.text).join('; ')
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

/** Tank level alarms are derived on the server from each tank's levels (ADR 0214). */
export const TANK_ALARM_RULE_PREFIX = 'tank:'

export function isTankAlarm(alarm: ActiveAlarm): boolean {
  return alarm.rule_id.startsWith(TANK_ALARM_RULE_PREFIX)
}

/**
 * The footer line of a card that says how it clears, for the alarms where
 * Helmcentral knows (a tank's level, a barometer tendency rule). Null
 * elsewhere: a bus notification clears however its producer clears it, and
 * nothing here claims to know.
 */
export function alarmFooterText(alarm: ActiveAlarm): string | null {
  if (!isTankAlarm(alarm)) return tendencyClearText(alarm)
  if ((alarm.op !== 'above' && alarm.op !== 'below') || alarm.clear_value === undefined) return null
  return tankClearText(alarm.op, alarm.clear_value)
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

/** "12 Oct 2026" for the date an item went onto an ignore list; "" when unknown. */
export function formatIgnoredOn(iso?: string): string {
  if (!iso) return ''
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) return ''
  return at.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })
}
