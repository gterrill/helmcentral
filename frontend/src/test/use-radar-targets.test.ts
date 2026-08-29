import { describe, expect, it, vi, beforeEach } from 'vitest'
import { act, renderHook } from '@testing-library/react'

// use-radar-targets.ts is a thin telemetry consumer, same shape as
// use-nearby-vessels.ts: it subscribes to one SSE event and sanitizes the
// payload. Mock the shared telemetry module rather than standing up a real
// EventSource — use-telemetry-stream.test.ts already covers the transport.
const subscribeTelemetryMock = vi.fn()
let capturedListener: ((raw: string) => void) | null = null

vi.mock('@/hooks/use-telemetry-stream', () => ({
  subscribeTelemetry: (event: string, cb: (raw: string) => void) => {
    capturedListener = cb
    subscribeTelemetryMock(event, cb)
    return () => {}
  },
}))

import { useRadarTargets } from '@/hooks/use-radar-targets'

function emit(payload: unknown) {
  act(() => {
    capturedListener?.(JSON.stringify(payload))
  })
}

beforeEach(() => {
  subscribeTelemetryMock.mockClear()
  capturedListener = null
})

describe('useRadarTargets', () => {
  it('subscribes to the radar-targets telemetry event', () => {
    renderHook(() => useRadarTargets())
    expect(subscribeTelemetryMock).toHaveBeenCalledWith('radar-targets', expect.any(Function))
  })

  it('starts loading with no targets, no radars, and a disabled source', () => {
    const { result } = renderHook(() => useRadarTargets())
    expect(result.current.loading).toBe(true)
    expect(result.current.targets).toEqual([])
    expect(result.current.radars).toEqual([])
    expect(result.current.source).toBe('disabled')
  })

  it('propagates the source field and clears loading once a payload lands', () => {
    const { result } = renderHook(() => useRadarTargets())
    emit({ datetime: '2026-08-28T02:00:12Z', source: 'mayara-unreachable', radars: [], targets: [] })
    expect(result.current.source).toBe('mayara-unreachable')
    expect(result.current.loading).toBe(false)
  })

  it('keeps optional fields undefined rather than defaulting them to 0', () => {
    const { result } = renderHook(() => useRadarTargets())
    emit({
      datetime: '2026-08-28T02:00:12Z',
      source: 'mayara',
      radars: [{ id: 'fur6424A', name: 'DRS4D-NXT 6424', transmitting: true }],
      targets: [
        {
          id: 'fur6424A:100000016',
          radar_id: 'fur6424A',
          target_id: 100000016,
          status: 'tracking',
          bearing_rad: 0.768,
          range_m: 418,
          position_derived: false,
          is_dangerous: false,
          acquisition: 'auto',
          age_seconds: 3,
          // lat, lon, course_rad, sog_knots, cpa_m, tcpa_seconds, source_zone,
          // last_seen_at are all omitted, exactly as the wire contract allows.
        },
      ],
    })

    expect(result.current.targets).toHaveLength(1)
    const target = result.current.targets[0]
    expect(target.lat).toBeUndefined()
    expect(target.lon).toBeUndefined()
    expect(target.course_rad).toBeUndefined()
    expect(target.sog_knots).toBeUndefined()
    expect(target.cpa_m).toBeUndefined()
    expect(target.tcpa_seconds).toBeUndefined()
    expect(target.source_zone).toBeUndefined()
    expect(target.last_seen_at).toBeUndefined()
  })

  it('keeps a real zero CPA/TCPA rather than dropping or defaulting it', () => {
    const { result } = renderHook(() => useRadarTargets())
    emit({
      datetime: '2026-08-28T02:00:12Z',
      source: 'mayara',
      radars: [],
      targets: [
        {
          id: 'fur6424A:100000099',
          radar_id: 'fur6424A',
          target_id: 100000099,
          status: 'tracking',
          bearing_rad: 0.1,
          range_m: 5,
          position_derived: false,
          is_dangerous: true,
          acquisition: 'auto',
          age_seconds: 1,
          cpa_m: 0,
          tcpa_seconds: 0,
        },
      ],
    })

    expect(result.current.targets[0].cpa_m).toBe(0)
    expect(result.current.targets[0].tcpa_seconds).toBe(0)
  })

  it('drops a target missing a required numeric field instead of rendering NaN', () => {
    const { result } = renderHook(() => useRadarTargets())
    emit({
      datetime: '2026-08-28T02:00:12Z',
      source: 'mayara',
      radars: [],
      targets: [
        {
          id: 'fur6424A:100000016',
          radar_id: 'fur6424A',
          target_id: 100000016,
          status: 'tracking',
          bearing_rad: 'not-a-number',
          range_m: 418,
          position_derived: false,
          is_dangerous: false,
          acquisition: 'auto',
          age_seconds: 3,
        },
        {
          id: 'fur6424A:100000017',
          radar_id: 'fur6424A',
          target_id: 100000017,
          status: 'tracking',
          bearing_rad: 0.5,
          range_m: 300,
          position_derived: false,
          is_dangerous: false,
          acquisition: 'auto',
          age_seconds: 4,
        },
      ],
    })

    expect(result.current.targets).toHaveLength(1)
    expect(result.current.targets[0].id).toBe('fur6424A:100000017')
  })

  it('drops a target with a missing id or radar_id', () => {
    const { result } = renderHook(() => useRadarTargets())
    emit({
      datetime: '2026-08-28T02:00:12Z',
      source: 'mayara',
      radars: [],
      targets: [
        {
          id: '',
          radar_id: 'fur6424A',
          target_id: 1,
          status: 'tracking',
          bearing_rad: 0.1,
          range_m: 10,
          position_derived: false,
          is_dangerous: false,
          acquisition: 'auto',
          age_seconds: 1,
        },
      ],
    })

    expect(result.current.targets).toHaveLength(0)
  })

  it('drops malformed radar entries and keeps well-formed ones', () => {
    const { result } = renderHook(() => useRadarTargets())
    emit({
      datetime: '2026-08-28T02:00:12Z',
      source: 'mayara',
      radars: [
        { id: 'fur6424A', name: 'DRS4D-NXT 6424', transmitting: true },
        { id: '', name: 'Bad', transmitting: true },
      ],
      targets: [],
    })

    expect(result.current.radars).toHaveLength(1)
    expect(result.current.radars[0].id).toBe('fur6424A')
  })

  it('ignores an unrecognised source value rather than propagating it as-is', () => {
    const { result } = renderHook(() => useRadarTargets())
    emit({ datetime: '2026-08-28T02:00:12Z', source: 'not-a-real-source', radars: [], targets: [] })
    expect(result.current.source).toBe('disabled')
  })
})

// The wire contract, asserted against what the backend actually sends.
//
// After the rework onto the SignalK plugin (ADR 0062 amendment) the backend
// builds radarInfo from the snapshot's radars.<id>.controls tree, which
// carries userName and modelName but no brand. It emits {id, name} only.
//
// sanitizeRadar previously required brand and model to be strings, so every
// radar was dropped and the tile header fell back to a bare "Radar" instead
// of "DRS4D-NXT 6424". Both sides' tests passed throughout, because each
// asserted against its own idea of the shape.
describe('radar info matches the backend payload', () => {
  it('keeps a radar that carries only id and name', () => {
    const { result } = renderHook(() => useRadarTargets())
    emit({
      datetime: '2026-08-29T07:58:00Z',
      source: 'mayara',
      radars: [
        { id: 'fur6424A', name: 'DRS4D-NXT 6424', transmitting: true },
        { id: 'fur6424B', name: 'DRS4D-NXT 6424 B', transmitting: true },
      ],
      targets: [],
    })

    expect(result.current.radars).toHaveLength(2)
    expect(result.current.radars[0]).toEqual({ id: 'fur6424A', name: 'DRS4D-NXT 6424', transmitting: true })
  })

  it('still drops a radar with no usable id', () => {
    const { result } = renderHook(() => useRadarTargets())
    emit({
      datetime: '2026-08-29T07:58:00Z',
      source: 'mayara',
      radars: [{ id: '', name: 'Bad', transmitting: true }, { id: 'fur6424A', name: 'DRS4D-NXT 6424', transmitting: true }],
      targets: [],
    })

    expect(result.current.radars).toHaveLength(1)
    expect(result.current.radars[0].id).toBe('fur6424A')
  })
})
