import { describe, expect, it, beforeEach, vi } from 'vitest'
import { act, renderHook } from '@testing-library/react'

let capturedListener: ((raw: string) => void) | null = null

vi.mock('@/hooks/use-telemetry-stream', () => ({
  subscribeTelemetry: (_event: string, cb: (raw: string) => void) => {
    capturedListener = cb
    return () => {}
  },
}))

import { useElectricalState } from '@/hooks/use-electrical-state'

function emit(payload: unknown) {
  act(() => {
    capturedListener?.(JSON.stringify(payload))
  })
}

beforeEach(() => {
  capturedListener = null
})

describe('useElectricalState charge rate', () => {
  it('reports no rate when the house bank capacity is unknown, even as SoC changes', () => {
    const { result } = renderHook(() => useElectricalState())
    emit({ datetime: '2026-09-14T00:00:00Z', battery_soc_percent: 53, battery_capacity_ah: -1, charging_current_a: 20.7 })
    emit({ datetime: '2026-09-14T01:00:00Z', battery_soc_percent: 54, battery_capacity_ah: -1, charging_current_a: 20.7 })
    expect(result.current.batteryRatePercentPerHour).toBeNull()
    expect(result.current.timeToGoHours).toBeNull()
  })

  it('derives rate and time to full from charging current and bank capacity', () => {
    const { result } = renderHook(() => useElectricalState())
    emit({ datetime: '2026-09-14T00:00:00Z', battery_soc_percent: 53, battery_capacity_ah: 1440, charging_current_a: 20.7 })
    expect(result.current.batteryRatePercentPerHour).toBeCloseTo(1.4375, 4)
    expect(result.current.timeToGoHours).toBeCloseTo(47 / 1.4375, 4)
  })

  it('reports no rate when the bank current is unknown (the backend sends null)', () => {
    const { result } = renderHook(() => useElectricalState())
    emit({ datetime: '2026-09-14T00:00:00Z', battery_soc_percent: 53, battery_capacity_ah: 200, charging_current_a: null })
    expect(result.current.chargingCurrentA).toBeNull()
    expect(result.current.batteryRatePercentPerHour).toBeNull()
    expect(result.current.timeToGoHours).toBeNull()
  })

  it('keeps a real -1 A discharge', () => {
    const { result } = renderHook(() => useElectricalState())
    emit({ datetime: '2026-09-14T00:00:00Z', battery_soc_percent: 50, battery_capacity_ah: 200, charging_current_a: -1, charging_power_w: -1 })
    expect(result.current.chargingCurrentA).toBe(-1)
    expect(result.current.chargingPowerW).toBe(-1)
    expect(result.current.batteryRatePercentPerHour).toBeCloseTo(-0.5, 4)
  })

  it('still reports a real discharge', () => {
    const { result } = renderHook(() => useElectricalState())
    emit({ datetime: '2026-09-14T00:00:00Z', battery_soc_percent: 50, battery_capacity_ah: 200, charging_current_a: -10 })
    expect(result.current.batteryRatePercentPerHour).toBeCloseTo(-5, 4)
    expect(result.current.timeToGoHours).toBeCloseTo(-10, 4)
  })
})

describe('useElectricalState socPath', () => {
  it('is null before any tick', () => {
    const { result } = renderHook(() => useElectricalState())
    expect(result.current.socPath).toBeNull()
  })

  it('carries the house bank state-of-charge path, and drops it when the bank is unset', () => {
    const { result } = renderHook(() => useElectricalState())
    emit({ datetime: '2026-09-14T00:00:00Z', soc_path: 'electrical.batteries.51.capacity.stateOfCharge' })
    expect(result.current.socPath).toBe('electrical.batteries.51.capacity.stateOfCharge')
    emit({ datetime: '2026-09-14T00:00:05Z' })
    expect(result.current.socPath).toBeNull()
  })
})

describe('useElectricalState voltage readout and generator', () => {
  it('carries the chosen battery voltage and its label', () => {
    const { result } = renderHook(() => useElectricalState())
    emit({ datetime: '2026-09-14T00:00:00Z', voltage_readout_v: 27.9, voltage_readout_label: 'Aft House' })
    expect(result.current.voltageReadoutV).toBe(27.9)
    expect(result.current.voltageReadoutLabel).toBe('Aft House')
  })

  it('reads no voltage readout when none is set (the backend sends -1 and no label)', () => {
    const { result } = renderHook(() => useElectricalState())
    emit({ datetime: '2026-09-14T00:00:00Z', voltage_readout_v: -1, voltage_readout_label: '' })
    expect(result.current.voltageReadoutV).toBeNull()
    expect(result.current.voltageReadoutLabel).toBeNull()
  })

  it('does not read the retired 24 V field', () => {
    const { result } = renderHook(() => useElectricalState())
    emit({ datetime: '2026-09-14T00:00:00Z', dc_24v_voltage_v: 24.4 })
    expect(result.current.voltageReadoutV).toBeNull()
  })

  it('says whether a generator is set up', () => {
    const { result } = renderHook(() => useElectricalState())
    emit({ datetime: '2026-09-14T00:00:00Z', generator_configured: true })
    expect(result.current.generatorConfigured).toBe(true)
    emit({ datetime: '2026-09-14T00:00:00Z', generator_configured: false })
    expect(result.current.generatorConfigured).toBe(false)
  })

  it('does not know whether a generator is set up until the backend says (initially, or null)', () => {
    const { result } = renderHook(() => useElectricalState())
    expect(result.current.generatorConfigured).toBeNull()
    emit({ datetime: '2026-09-14T00:00:00Z', generator_configured: true })
    emit({ datetime: '2026-09-14T00:00:00Z', generator_configured: null })
    expect(result.current.generatorConfigured).toBeNull()
  })

  it('says whether a house bank is chosen, and not until the backend says', () => {
    const { result } = renderHook(() => useElectricalState())
    expect(result.current.houseBankConfigured).toBeNull()
    emit({ datetime: '2026-09-14T00:00:00Z', house_bank_configured: false })
    expect(result.current.houseBankConfigured).toBe(false)
    emit({ datetime: '2026-09-14T00:00:00Z', house_bank_configured: true })
    expect(result.current.houseBankConfigured).toBe(true)
    emit({ datetime: '2026-09-14T00:00:00Z', house_bank_configured: null })
    expect(result.current.houseBankConfigured).toBeNull()
  })
})
