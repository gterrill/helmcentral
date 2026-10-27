/**
 * How long a live store's reading lasts (ADR 0215). The server seeds a new
 * connection with every event's current payload, but the browser shares one
 * connection between all the stores, so a store that re-subscribes to a
 * connection already open hears nothing until that event next changes.
 * vessel-state and gauge-values are change-gated, alarms come every two
 * seconds. A store that emptied itself when its last reader left would show
 * the next reader blanks, or "no alarms", until then.
 *
 * Each test loads the stores fresh, so module state never leaks between them.
 */
import { render, renderHook, act, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const listeners = new Map<string, (raw: string) => void>()

vi.mock('@/hooks/use-telemetry-stream', () => ({
  subscribeTelemetry: (event: string, cb: (raw: string) => void) => {
    listeners.set(event, cb)
    return () => {
      if (listeners.get(event) === cb) listeners.delete(event)
    }
  },
}))

vi.mock('@/hooks/use-alarm-rules', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-alarm-rules')>()),
  useAlarmLog: () => ({ entries: [], refresh: vi.fn().mockResolvedValue(undefined) }),
}))

function emit(event: string, payload: unknown) {
  act(() => {
    listeners.get(event)?.(JSON.stringify(payload))
  })
}

const houseBankLow = {
  rule_id: 'house-bank-low', label: 'House Bank Low', path: 'electrical.batteries.house.capacity.stateOfCharge',
  phase: 'active', state: 'alarm', value: 0.18, message: 'House bank 18%',
  silenced: false, can_silence: false, can_acknowledge: true,
}

beforeEach(() => {
  listeners.clear()
  vi.resetModules()
  vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, json: async () => ({}) })))
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('alarms store', () => {
  it('keeps the list for a surface that mounts after every reader has gone', async () => {
    const { useAlarmListComparedOn, sameShownAlarm } = await import('@/hooks/use-alarms')
    const useAlarmList = () => useAlarmListComparedOn(sameShownAlarm)
    const first = renderHook(() => useAlarmList())
    emit('alarms', { alarms: [houseBankLow], worst: 'alarm' })
    expect(first.result.current).toHaveLength(1)
    first.unmount()

    const second = renderHook(() => useAlarmList())
    expect(second.result.current).toHaveLength(1)
  })

  it('does not report the worst state as normal before the first alarms event', async () => {
    const { useWorstAlarmState } = await import('@/hooks/use-alarms')
    const { result } = renderHook(() => useWorstAlarmState())
    expect(result.current).toBeNull()

    emit('alarms', { alarms: [], worst: 'normal' })
    expect(result.current).toBe('normal')
  })

  it('does not tell the Alarms page "All clear" before the first alarms event', async () => {
    const { LiveAlarmsDrawer } = await import('@/components/live-surfaces')
    render(
      <LiveAlarmsDrawer
        rules={[]}
        loading={false}
        error={null}
        createRule={vi.fn()}
        updateRule={vi.fn()}
        deleteRule={vi.fn()}
        collisionTuningUrl={null}
        forecastWarnings={null}
      />,
    )
    expect(await screen.findByText('Waiting for the alarm list')).toBeInTheDocument()
    expect(screen.queryByText('All clear')).not.toBeInTheDocument()

    emit('alarms', { alarms: [], worst: 'normal' })
    expect(await screen.findByText('All clear')).toBeInTheDocument()
  })
})

describe('vessel-state store', () => {
  it('keeps the fix for a tile that mounts after every reader has gone', async () => {
    const { useVesselStateSelector } = await import('@/hooks/use-vessel-state')
    const select = (s: { latitude: number | null }) => s.latitude
    const first = renderHook(() => useVesselStateSelector(select))
    emit('vessel-state', { latitude: -20.2747, longitude: 149.0737 })
    expect(first.result.current).toBe(-20.2747)
    first.unmount()

    const second = renderHook(() => useVesselStateSelector(select))
    expect(second.result.current).toBe(-20.2747)
  })
})

describe('gauge-values store', () => {
  it('keeps a reading for a gauge that mounts after every reader has gone', async () => {
    const { useGaugeValue } = await import('@/hooks/use-gauge-values')
    const first = renderHook(() => useGaugeValue('a.path'))
    emit('gauge-values', { values: { 'a.path': 12 }, ages: { 'a.path': 1 } })
    expect(first.result.current).toBe(12)
    first.unmount()

    const second = renderHook(() => useGaugeValue('a.path'))
    expect(second.result.current).toBe(12)
  })
})

describe('the CHK lamp', () => {
  it('reads no data, not clear, while the worst alarm state is unknown', async () => {
    const { LampStripTile } = await import('@/components/lamp-strip-tile')
    render(
      <LampStripTile
        config={{ title: 'Status', lamps: [], showCheck: true }}
        values={{}}
        worstAlarmState={null}
        editing={false}
        onConfigure={vi.fn()}
        onOpenAlarms={vi.fn()}
      />,
    )
    expect(screen.getByLabelText('CHK: no data')).toBeInTheDocument()
  })
})
