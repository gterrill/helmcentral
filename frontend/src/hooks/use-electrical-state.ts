import { subscribeTelemetry } from '@/hooks/use-telemetry-stream'
import { shownAge } from '@/lib/staleness'
import { createTelemetryStore, keepUnchanged, sameShallow, useStoreFields, type SubscribeTelemetry, type TelemetryUpdate } from '@/lib/telemetry-store'

export interface AlternatorInstance {
  currentA: number | null
  voltageV: number | null
  powerW: number | null
  temperatureC: number | null
}

interface ElectricalState {
  datetime: string
  last_update_age_s: number
  battery_soc_percent: number
  /** The house bank's state-of-charge path; absent when no bank is chosen. */
  soc_path?: string
  battery_capacity_ah: number
  charging_current_a: number | null
  charging_power_w: number | null
  solar_output_w: number
  ac_output_w: number
  dc_12v_power_w: number
  dc_12v_current_a: number
  /** The chosen voltage readout battery; -1 and an empty label when none is set. */
  voltage_readout_v: number
  voltage_readout_label: string
  ac_loads_w: number
  generator_real_power_w: number
  /** A generator instance is chosen in the roles. */
  generator_configured: boolean | null
  generator_none?: boolean
  /** A house bank is chosen in the roles; null when the backend cannot read them. */
  house_bank_configured: boolean | null
  /** The chosen house bank's item has no SignalK source (or is gone). */
  house_bank_unresolved?: boolean | null
  alternator_0_current_a: number
  alternator_0_voltage_v: number
  alternator_0_power_w: number
  alternator_0_temperature_c: number
  alternator_1_current_a: number
  alternator_1_voltage_v: number
  alternator_1_power_w: number
  alternator_1_temperature_c: number
  charger_0_current_a: number
  charger_0_acin_1_current_a: number
  charger_0_charging_mode: string
  charger_0_error: string
  source: string
}

function parseAlt(v: unknown): number | null {
  return typeof v === 'number' && v >= 0 ? v : null
}

// Bank current and power are signed (negative is discharge), so no number can
// mean unknown; the backend sends null.
function parseSigned(v: unknown): number | null {
  return typeof v === 'number' ? v : null
}

/** The electrical state as the tiles read it: parsed, with absent and -1 readings as null. */
export interface ElectricalStateValue {
  lastUpdateAgeS: number | null
  batterySocPercent: number | null
  socPath: string | null
  chargingCurrentA: number | null
  chargingPowerW: number | null
  solarOutputW: number | null
  acOutputW: number | null
  dc12vPowerW: number | null
  voltageReadoutV: number | null
  voltageReadoutLabel: string | null
  generatorRealPowerW: number | null
  /** A generator instance is chosen in the roles; null while that is not known. */
  generatorConfigured: boolean | null
  generatorNone: boolean
  /** A house bank is chosen in the roles; null while that is not known. */
  houseBankConfigured: boolean | null
  houseBankUnresolved: boolean
  alternator0: AlternatorInstance
  alternator1: AlternatorInstance
  charger0CurrentA: number | null
  charger0AcIn1CurrentA: number | null
  charger0ChargingMode: string | null
  charger0Error: string | null
  batteryRatePercentPerHour: number | null
  timeToGoHours: number | null
}

export type ElectricalStateField = keyof ElectricalStateValue

const NO_ALTERNATOR: AlternatorInstance = { currentA: null, voltageV: null, powerW: null, temperatureC: null }

/** Every reading unknown: the state before the first electrical-state event. */
export const INITIAL_ELECTRICAL_STATE: ElectricalStateValue = {
  lastUpdateAgeS: null,
  batterySocPercent: null,
  socPath: null,
  chargingCurrentA: null,
  chargingPowerW: null,
  solarOutputW: null,
  acOutputW: null,
  dc12vPowerW: null,
  voltageReadoutV: null,
  voltageReadoutLabel: null,
  generatorRealPowerW: null,
  generatorConfigured: null,
  generatorNone: false,
  houseBankConfigured: null,
  houseBankUnresolved: false,
  alternator0: NO_ALTERNATOR,
  alternator1: NO_ALTERNATOR,
  charger0CurrentA: null,
  charger0AcIn1CurrentA: null,
  charger0ChargingMode: null,
  charger0Error: null,
  batteryRatePercentPerHour: null,
  timeToGoHours: null,
}

const nonNegative = (value: unknown): number | null => (typeof value === 'number' && value >= 0 ? value : null)
const trimmedText = (value: unknown): string | null => (typeof value === 'string' && value.trim().length > 0 ? value.trim() : null)

/** The bank's rate and time to go, from the configured house bank capacity only; no estimate from SoC deltas. */
function rateAndTimeToGo(socPercent: number | null, capacityAh: number | null, currentA: number | null): { rate: number | null; timeToGo: number | null } {
  if (socPercent === null) return { rate: null, timeToGo: null }
  const rate = capacityAh !== null && currentA !== null ? (currentA / capacityAh) * 100 : null
  if (rate === null || !Number.isFinite(rate)) return { rate: null, timeToGo: null }
  // A bank at rest has a known rate of zero and no time to go; keeping that
  // apart from an unknown rate stops the dawn projection reading "unknown" as
  // "holding steady".
  if (Math.abs(rate) <= 0.01) return { rate: 0, timeToGo: null }
  // Charging: time until full. Discharging: time until empty, negative to
  // signal the direction.
  return { rate, timeToGo: rate > 0 ? Math.max(0, 100 - socPercent) / rate : -(socPercent / Math.abs(rate)) }
}

/** One electrical-state event, parsed the way every tile reads it. */
function parseElectricalState(data: ElectricalState): ElectricalStateValue {
  const batterySocPercent = nonNegative(data.battery_soc_percent)
  const chargingCurrentA = parseSigned(data.charging_current_a)
  const capacityAh = typeof data.battery_capacity_ah === 'number' && data.battery_capacity_ah > 0 ? data.battery_capacity_ah : null
  const readoutV = nonNegative(data.voltage_readout_v)
  const readoutLabel = trimmedText(data.voltage_readout_label)
  // A figure without its name, or a name without a figure, is not a row.
  const readout = readoutV !== null && readoutLabel !== null
  const { rate, timeToGo } = rateAndTimeToGo(batterySocPercent, capacityAh, chargingCurrentA)
  return {
    lastUpdateAgeS: parseAlt(data.last_update_age_s),
    batterySocPercent,
    socPath: typeof data.soc_path === 'string' && data.soc_path !== '' ? data.soc_path : null,
    chargingCurrentA,
    chargingPowerW: parseSigned(data.charging_power_w),
    solarOutputW: nonNegative(data.solar_output_w),
    acOutputW: nonNegative(data.ac_output_w),
    dc12vPowerW: nonNegative(data.dc_12v_power_w),
    voltageReadoutV: readout ? readoutV : null,
    voltageReadoutLabel: readout ? readoutLabel : null,
    generatorRealPowerW: nonNegative(data.generator_real_power_w),
    generatorConfigured: typeof data.generator_configured === 'boolean' ? data.generator_configured : null,
    generatorNone: data.generator_none === true,
    houseBankConfigured: typeof data.house_bank_configured === 'boolean' ? data.house_bank_configured : null,
    houseBankUnresolved: data.house_bank_unresolved === true,
    alternator0: { currentA: parseAlt(data.alternator_0_current_a), voltageV: parseAlt(data.alternator_0_voltage_v), powerW: parseAlt(data.alternator_0_power_w), temperatureC: parseAlt(data.alternator_0_temperature_c) },
    alternator1: { currentA: parseAlt(data.alternator_1_current_a), voltageV: parseAlt(data.alternator_1_voltage_v), powerW: parseAlt(data.alternator_1_power_w), temperatureC: parseAlt(data.alternator_1_temperature_c) },
    charger0CurrentA: nonNegative(data.charger_0_current_a),
    charger0AcIn1CurrentA: nonNegative(data.charger_0_acin_1_current_a),
    charger0ChargingMode: trimmedText(data.charger_0_charging_mode),
    charger0Error: trimmedText(data.charger_0_error),
    batteryRatePercentPerHour: rate,
    timeToGoHours: timeToGo,
  }
}

/** Whether a field shows the same: the age by what a tile shows for it, an alternator reading by reading. */
function sameField(field: ElectricalStateField, a: unknown, b: unknown): boolean {
  if (field === 'lastUpdateAgeS') return shownAge(a as number | null) === shownAge(b as number | null)
  if (field === 'alternator0' || field === 'alternator1') return sameShallow(a as AlternatorInstance, b as AlternatorInstance)
  return Object.is(a, b)
}

/*
 * One store for the `electrical-state` event (ADR 0215; lib/telemetry-store.ts).
 * A field that shows the same keeps its previous value, the alternators
 * included, and a tick in which nothing shows differently keeps the snapshot
 * and notifies nobody. On the boat the bank, DC and solar figures move on
 * nearly every five-second tick; the alternators, the charger and the roles
 * rarely do, so the Alternator and Generator tiles sit still while the
 * Battery & Power tile follows the bank.
 */
function applyElectricalState(previous: ElectricalStateValue, raw: string): TelemetryUpdate<ElectricalStateValue> | null {
  let data: ElectricalState
  try {
    data = JSON.parse(raw) as ElectricalState
  } catch (err) {
    console.error('Failed to parse electrical state event:', err)
    return null
  }
  const next = keepUnchanged(previous, parseElectricalState(data), sameField)
  return next === null ? null : { state: next }
}

// Read at start, not at import: a module that only imports this one's
// helpers or types never touches the stream.
const listenToStream: SubscribeTelemetry = (event, onData) => subscribeTelemetry(event, onData)

const store = createTelemetryStore(listenToStream, 'electrical-state', INITIAL_ELECTRICAL_STATE, applyElectricalState)

/** One slice of the electrical state; see TelemetryStore.useSelector. */
export const useElectricalSelector = store.useSelector

/** The named fields, as one object kept while none of them changed. Pass a constant list. */
export function useElectricalFields<K extends ElectricalStateField>(fields: readonly K[]): Pick<ElectricalStateValue, K> {
  return useStoreFields(store, fields)
}
