import { renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { AlarmRule } from '@/hooks/use-alarm-rules'
import { useSocBands } from '@/hooks/use-soc-bands'

const BANK_SOC = 'electrical.batteries.51.capacity.stateOfCharge'

const rule: AlarmRule = {
  id: 'r1',
  enabled: true,
  path: BANK_SOC,
  label: 'SoC band',
  op: 'below',
  value: 0.2,
  hysteresis: 0,
  dwell_seconds: 0,
  stale_after_seconds: 0,
  state: 'warn',
  methods: [],
  notify: null,
  escalate_after_seconds: 0,
}

describe('useSocBands', () => {
  it('reads bands from rules on the house bank path', () => {
    const { result } = renderHook(() => useSocBands([rule], BANK_SOC))
    expect(result.current).toEqual({ warnBelow: 20, alarmBelow: null })
  })

  it('has no bands when the house bank is not known, even with a rule on another bank', () => {
    const { result } = renderHook(() => useSocBands([{ ...rule, path: 'electrical.batteries.0.capacity.stateOfCharge' }], undefined))
    expect(result.current).toEqual({ warnBelow: null, alarmBelow: null })
  })

  it('ignores rules that sit on a different bank than the one chosen', () => {
    const { result } = renderHook(() => useSocBands([{ ...rule, path: 'electrical.batteries.0.capacity.stateOfCharge' }], BANK_SOC))
    expect(result.current).toEqual({ warnBelow: null, alarmBelow: null })
  })
})
