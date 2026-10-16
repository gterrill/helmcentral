import { useEffect, useState } from 'react'

import { subscribeTelemetry } from '@/hooks/use-telemetry-stream'

interface AlternatorInstance {
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
  /** A house bank is chosen in the roles; null when the backend cannot read them. */
  house_bank_configured: boolean | null
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

export function useElectricalState() {
  const [lastUpdateAgeS, setLastUpdateAgeS] = useState<number | null>(null)
  const [batterySocPercent, setBatterySocPercent] = useState<number | null>(null)
  const [socPath, setSocPath] = useState<string | null>(null)
  const [chargingCurrentA, setChargingCurrentA] = useState<number | null>(null)
  const [chargingPowerW, setChargingPowerW] = useState<number | null>(null)
  const [solarOutputW, setSolarOutputW] = useState<number | null>(null)
  const [acOutputW, setAcOutputW] = useState<number | null>(null)
  const [dc12vPowerW, setDc12vPowerW] = useState<number | null>(null)
  const [dc12vCurrentA, setDc12vCurrentA] = useState<number | null>(null)
  const [voltageReadoutV, setVoltageReadoutV] = useState<number | null>(null)
  const [voltageReadoutLabel, setVoltageReadoutLabel] = useState<string | null>(null)
  const [generatorConfigured, setGeneratorConfigured] = useState<boolean | null>(null)
  const [houseBankConfigured, setHouseBankConfigured] = useState<boolean | null>(null)
  const [acLoadsW, setAcLoadsW] = useState<number | null>(null)
  const [generatorRealPowerW, setGeneratorRealPowerW] = useState<number | null>(null)
  const [alternator0, setAlternator0] = useState<AlternatorInstance>({ currentA: null, voltageV: null, powerW: null, temperatureC: null })
  const [alternator1, setAlternator1] = useState<AlternatorInstance>({ currentA: null, voltageV: null, powerW: null, temperatureC: null })
  const [charger0CurrentA, setCharger0CurrentA] = useState<number | null>(null)
  const [charger0AcIn1CurrentA, setCharger0AcIn1CurrentA] = useState<number | null>(null)
  const [charger0ChargingMode, setCharger0ChargingMode] = useState<string | null>(null)
  const [charger0Error, setCharger0Error] = useState<string | null>(null)
  const [batteryRatePercentPerHour, setBatteryRatePercentPerHour] = useState<number | null>(null)
  const [timeToGoHours, setTimeToGoHours] = useState<number | null>(null)

  useEffect(() => {
    const applyElectricalState = (payload: unknown) => {
      try {
        const data = payload as ElectricalState

        const nextBatterySocPercent =
          typeof data.battery_soc_percent === 'number' && data.battery_soc_percent >= 0 ? data.battery_soc_percent : null

        setLastUpdateAgeS(parseAlt(data.last_update_age_s))
        setBatterySocPercent(nextBatterySocPercent)
        setSocPath(typeof data.soc_path === 'string' && data.soc_path !== '' ? data.soc_path : null)
        setChargingCurrentA(parseSigned(data.charging_current_a))
        setChargingPowerW(parseSigned(data.charging_power_w))
        setSolarOutputW(typeof data.solar_output_w === 'number' && data.solar_output_w >= 0 ? data.solar_output_w : null)
        setAcOutputW(typeof data.ac_output_w === 'number' && data.ac_output_w >= 0 ? data.ac_output_w : null)
        setDc12vPowerW(typeof data.dc_12v_power_w === 'number' && data.dc_12v_power_w >= 0 ? data.dc_12v_power_w : null)
        setDc12vCurrentA(typeof data.dc_12v_current_a === 'number' && data.dc_12v_current_a >= 0 ? data.dc_12v_current_a : null)
        const readoutV = typeof data.voltage_readout_v === 'number' && data.voltage_readout_v >= 0 ? data.voltage_readout_v : null
        const readoutLabel = typeof data.voltage_readout_label === 'string' && data.voltage_readout_label.trim() !== '' ? data.voltage_readout_label.trim() : null
        // A figure without its name, or a name without a figure, is not a row.
        setVoltageReadoutV(readoutV !== null && readoutLabel !== null ? readoutV : null)
        setVoltageReadoutLabel(readoutV !== null && readoutLabel !== null ? readoutLabel : null)
        setGeneratorConfigured(typeof data.generator_configured === 'boolean' ? data.generator_configured : null)
        setHouseBankConfigured(typeof data.house_bank_configured === 'boolean' ? data.house_bank_configured : null)
        setAcLoadsW(typeof data.ac_loads_w === 'number' && data.ac_loads_w >= 0 ? data.ac_loads_w : null)
        setGeneratorRealPowerW(typeof data.generator_real_power_w === 'number' && data.generator_real_power_w >= 0 ? data.generator_real_power_w : null)
        setAlternator0({ currentA: parseAlt(data.alternator_0_current_a), voltageV: parseAlt(data.alternator_0_voltage_v), powerW: parseAlt(data.alternator_0_power_w), temperatureC: parseAlt(data.alternator_0_temperature_c) })
        setAlternator1({ currentA: parseAlt(data.alternator_1_current_a), voltageV: parseAlt(data.alternator_1_voltage_v), powerW: parseAlt(data.alternator_1_power_w), temperatureC: parseAlt(data.alternator_1_temperature_c) })
        setCharger0CurrentA(typeof data.charger_0_current_a === 'number' && data.charger_0_current_a >= 0 ? data.charger_0_current_a : null)
        setCharger0AcIn1CurrentA(typeof data.charger_0_acin_1_current_a === 'number' && data.charger_0_acin_1_current_a >= 0 ? data.charger_0_acin_1_current_a : null)
        setCharger0ChargingMode(typeof data.charger_0_charging_mode === 'string' && data.charger_0_charging_mode.trim().length > 0 ? data.charger_0_charging_mode.trim() : null)
        setCharger0Error(typeof data.charger_0_error === 'string' && data.charger_0_error.trim().length > 0 ? data.charger_0_error.trim() : null)

        const batteryCapacityAh =
          typeof data.battery_capacity_ah === 'number' && data.battery_capacity_ah > 0 ? data.battery_capacity_ah : null
        const chargingCurrentA = parseSigned(data.charging_current_a)

        if (nextBatterySocPercent === null) {
          setBatteryRatePercentPerHour(null)
          setTimeToGoHours(null)
          return
        }

        // Rate comes only from the configured house bank capacity; no estimate from SoC deltas.
        const rate =
          batteryCapacityAh !== null && chargingCurrentA !== null ? (chargingCurrentA / batteryCapacityAh) * 100 : null

        if (rate === null || !Number.isFinite(rate) || Math.abs(rate) <= 0.01) {
          setBatteryRatePercentPerHour(null)
          setTimeToGoHours(null)
          return
        }

        setBatteryRatePercentPerHour(rate)
        if (rate > 0) {
          // Charging: time until full
          setTimeToGoHours(Math.max(0, 100 - nextBatterySocPercent) / rate)
        } else {
          // Discharging: time until empty (returned as negative to signal direction)
          setTimeToGoHours(-(nextBatterySocPercent / Math.abs(rate)))
        }
      } catch (err) {
        console.error('Failed to fetch electrical state:', err)
      }
    }

    return subscribeTelemetry('electrical-state', (raw) => {
      applyElectricalState(JSON.parse(raw))
    })
  }, [])

  return {
    lastUpdateAgeS,
    batterySocPercent,
    socPath,
    chargingCurrentA,
    chargingPowerW,
    solarOutputW,
    acOutputW,
    dc12vPowerW,
    dc12vCurrentA,
    voltageReadoutV,
    voltageReadoutLabel,
    acLoadsW,
    generatorRealPowerW,
    generatorConfigured,
    houseBankConfigured,
    alternator0,
    alternator1,
    charger0CurrentA,
    charger0AcIn1CurrentA,
    charger0ChargingMode,
    charger0Error,
    batteryRatePercentPerHour,
    timeToGoHours,
  }
}
