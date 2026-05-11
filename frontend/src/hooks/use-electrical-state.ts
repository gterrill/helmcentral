import { useEffect, useState } from 'react'

interface ElectricalState {
  datetime: string
  battery_soc_percent: number
  charging_current_a: number
  charging_power_w: number
  solar_output_w: number
  ac_output_w: number
  dc_12v_power_w: number
  dc_12v_current_a: number
  dc_24v_voltage_v: number
  ac_loads_w: number
  source: string
}

export function useElectricalState(refreshInterval: number) {
  const [batterySocPercent, setBatterySocPercent] = useState<number | null>(null)
  const [chargingCurrentA, setChargingCurrentA] = useState<number | null>(null)
  const [chargingPowerW, setChargingPowerW] = useState<number | null>(null)
  const [solarOutputW, setSolarOutputW] = useState<number | null>(null)
  const [acOutputW, setAcOutputW] = useState<number | null>(null)
  const [dc12vPowerW, setDc12vPowerW] = useState<number | null>(null)
  const [dc12vCurrentA, setDc12vCurrentA] = useState<number | null>(null)
  const [dc24vVoltageV, setDc24vVoltageV] = useState<number | null>(null)
  const [acLoadsW, setAcLoadsW] = useState<number | null>(null)

  useEffect(() => {
    const fetchElectricalState = async () => {
      try {
        const response = await fetch('/api/electrical-state')

        if (!response.ok) {
          throw new Error('Failed to fetch electrical state')
        }

        const data = (await response.json()) as ElectricalState

        setBatterySocPercent(typeof data.battery_soc_percent === 'number' && data.battery_soc_percent >= 0 ? data.battery_soc_percent : null)
        setChargingCurrentA(typeof data.charging_current_a === 'number' && data.charging_current_a >= 0 ? data.charging_current_a : null)
        setChargingPowerW(typeof data.charging_power_w === 'number' && data.charging_power_w >= 0 ? data.charging_power_w : null)
        setSolarOutputW(typeof data.solar_output_w === 'number' && data.solar_output_w >= 0 ? data.solar_output_w : null)
        setAcOutputW(typeof data.ac_output_w === 'number' && data.ac_output_w >= 0 ? data.ac_output_w : null)
        setDc12vPowerW(typeof data.dc_12v_power_w === 'number' && data.dc_12v_power_w >= 0 ? data.dc_12v_power_w : null)
        setDc12vCurrentA(typeof data.dc_12v_current_a === 'number' && data.dc_12v_current_a >= 0 ? data.dc_12v_current_a : null)
        setDc24vVoltageV(typeof data.dc_24v_voltage_v === 'number' && data.dc_24v_voltage_v >= 0 ? data.dc_24v_voltage_v : null)
        setAcLoadsW(typeof data.ac_loads_w === 'number' && data.ac_loads_w >= 0 ? data.ac_loads_w : null)
      } catch (err) {
        console.error('Failed to fetch electrical state:', err)
      }
    }

    void fetchElectricalState()
    const timer = window.setInterval(() => {
      void fetchElectricalState()
    }, refreshInterval * 1000)

    return () => {
      window.clearInterval(timer)
    }
  }, [refreshInterval])

  return {
    batterySocPercent,
    chargingCurrentA,
    chargingPowerW,
    solarOutputW,
    acOutputW,
    dc12vPowerW,
    dc12vCurrentA,
    dc24vVoltageV,
    acLoadsW,
  }
}
