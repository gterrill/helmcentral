import { describe, expect, it } from 'vitest'
import { renderHook } from '@testing-library/react'
import { useElectricalSelector, type ElectricalStateValue } from '@/hooks/use-electrical-state'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'

const stream = installFakeTelemetryStream()

const selectAll = (state: ElectricalStateValue) => state
const useElectricalState = () => useElectricalSelector(selectAll)

const emit = (payload: unknown) => stream.emit('electrical-state', payload)

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

  it('reports a zero rate, not an unknown one, for a bank at rest', () => {
    const { result } = renderHook(() => useElectricalState())
    emit({ datetime: '2026-09-14T00:00:00Z', battery_soc_percent: 50, battery_capacity_ah: 200, charging_current_a: 0 })
    expect(result.current.batteryRatePercentPerHour).toBe(0)
    expect(result.current.timeToGoHours).toBeNull()
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

  it('says when the boat has no generator', () => {
    const { result } = renderHook(() => useElectricalState())
    expect(result.current.generatorNone).toBe(false)
    emit({ datetime: '2026-09-14T00:00:00Z', generator_configured: false, generator_none: true })
    expect(result.current.generatorNone).toBe(true)
    emit({ datetime: '2026-09-14T00:00:00Z', generator_configured: false, generator_none: false })
    expect(result.current.generatorNone).toBe(false)
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

  it('says when the chosen house bank has lost its SignalK source', () => {
    const { result } = renderHook(() => useElectricalState())
    expect(result.current.houseBankUnresolved).toBe(false)
    emit({ datetime: '2026-09-14T00:00:00Z', house_bank_configured: true, house_bank_unresolved: true })
    expect(result.current.houseBankUnresolved).toBe(true)
    emit({ datetime: '2026-09-14T00:00:00Z', house_bank_configured: true })
    expect(result.current.houseBankUnresolved).toBe(false)
  })
})

describe('electrical store (ADR 0215)', () => {
  it('claims no reading before the first event', () => {
    const { result } = renderHook(() => useElectricalState())
    expect(result.current.batterySocPercent).toBeNull()
    expect(result.current.generatorConfigured).toBeNull()
    expect(result.current.houseBankConfigured).toBeNull()
  })

  it('keeps the snapshot when only datetime and a fresh age moved', () => {
    const { result } = renderHook(() => useElectricalState())
    const payload = { datetime: '2026-10-27T04:20:11Z', last_update_age_s: 0, battery_soc_percent: 82, charging_current_a: -27.4 }
    emit(payload)
    const first = result.current

    emit({ ...payload, datetime: '2026-10-27T04:20:16Z', last_update_age_s: 0.4 })

    expect(result.current).toBe(first)
  })

  it('keeps the alternator objects when only the bank moved', () => {
    const { result } = renderHook(() => useElectricalState())
    const payload = { battery_soc_percent: 82, charging_current_a: -27.4, alternator_0_current_a: 12, alternator_1_current_a: -1 }
    emit(payload)
    const { alternator0, alternator1 } = result.current

    emit({ ...payload, charging_current_a: -29.6 })

    expect(result.current.chargingCurrentA).toBe(-29.6)
    expect(result.current.alternator0).toBe(alternator0)
    expect(result.current.alternator1).toBe(alternator1)
  })
})
