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

  it('reports no rate when the bank current is unknown (the backend sends -1)', () => {
    const { result } = renderHook(() => useElectricalState())
    emit({ datetime: '2026-09-14T00:00:00Z', battery_soc_percent: 53, battery_capacity_ah: 200, charging_current_a: -1 })
    expect(result.current.batteryRatePercentPerHour).toBeNull()
    expect(result.current.timeToGoHours).toBeNull()
  })

  it('still reports a real discharge', () => {
    const { result } = renderHook(() => useElectricalState())
    emit({ datetime: '2026-09-14T00:00:00Z', battery_soc_percent: 50, battery_capacity_ah: 200, charging_current_a: -10 })
    expect(result.current.batteryRatePercentPerHour).toBeCloseTo(-5, 4)
    expect(result.current.timeToGoHours).toBeCloseTo(-10, 4)
  })
})
