import { useEffect, useState } from 'react'

interface ElectricalState {
  datetime: string
  battery_soc_percent: number
  battery_capacity_ah: number
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
  const [batteryRatePercentPerHour, setBatteryRatePercentPerHour] = useState<number | null>(null)
  const [timeToGoHours, setTimeToGoHours] = useState<number | null>(null)

  useEffect(() => {
    let previousSocSample: { socPercent: number; timestampMs: number } | null = null
    let smoothedChargeRatePercentPerHour: number | null = null

    const fetchElectricalState = async () => {
      try {
        const response = await fetch('/api/electrical-state')

        if (!response.ok) {
          throw new Error('Failed to fetch electrical state')
        }

        const data = (await response.json()) as ElectricalState

        const nextBatterySocPercent =
          typeof data.battery_soc_percent === 'number' && data.battery_soc_percent >= 0 ? data.battery_soc_percent : null

        setBatterySocPercent(nextBatterySocPercent)
        setChargingCurrentA(typeof data.charging_current_a === 'number' ? data.charging_current_a : null)
        setChargingPowerW(typeof data.charging_power_w === 'number' ? data.charging_power_w : null)
        setSolarOutputW(typeof data.solar_output_w === 'number' && data.solar_output_w >= 0 ? data.solar_output_w : null)
        setAcOutputW(typeof data.ac_output_w === 'number' && data.ac_output_w >= 0 ? data.ac_output_w : null)
        setDc12vPowerW(typeof data.dc_12v_power_w === 'number' && data.dc_12v_power_w >= 0 ? data.dc_12v_power_w : null)
        setDc12vCurrentA(typeof data.dc_12v_current_a === 'number' && data.dc_12v_current_a >= 0 ? data.dc_12v_current_a : null)
        setDc24vVoltageV(typeof data.dc_24v_voltage_v === 'number' && data.dc_24v_voltage_v >= 0 ? data.dc_24v_voltage_v : null)
        setAcLoadsW(typeof data.ac_loads_w === 'number' && data.ac_loads_w >= 0 ? data.ac_loads_w : null)

        const batteryCapacityAh =
          typeof data.battery_capacity_ah === 'number' && data.battery_capacity_ah > 0 ? data.battery_capacity_ah : null
        const chargingCurrentA =
          typeof data.charging_current_a === 'number' ? data.charging_current_a : null

        if (nextBatterySocPercent === null) {
          previousSocSample = null
          smoothedChargeRatePercentPerHour = null
          setBatteryRatePercentPerHour(null)
          setTimeToGoHours(null)
          return
        }

        const parsedTimestampMs = Date.parse(data.datetime)
        const sampleTimestampMs = Number.isFinite(parsedTimestampMs) ? parsedTimestampMs : Date.now()
        const currentSample = { socPercent: nextBatterySocPercent, timestampMs: sampleTimestampMs }

        if (previousSocSample !== null) {
          const elapsedHours = (currentSample.timestampMs - previousSocSample.timestampMs) / (60 * 60 * 1000)
          if (elapsedHours > 0) {
            const instantRatePercentPerHour = (currentSample.socPercent - previousSocSample.socPercent) / elapsedHours
            if (Number.isFinite(instantRatePercentPerHour)) {
              const alpha = 0.35
              smoothedChargeRatePercentPerHour =
                smoothedChargeRatePercentPerHour === null
                  ? instantRatePercentPerHour
                  : alpha * instantRatePercentPerHour + (1 - alpha) * smoothedChargeRatePercentPerHour
            }
          }
        }

        previousSocSample = currentSample

        let rate = smoothedChargeRatePercentPerHour
        if (batteryCapacityAh !== null && chargingCurrentA !== null && batteryCapacityAh > 0) {
          rate = (chargingCurrentA / batteryCapacityAh) * 100
        }

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
    batteryRatePercentPerHour,
    timeToGoHours,
  }
}
