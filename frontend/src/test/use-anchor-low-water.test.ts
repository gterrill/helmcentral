import { describe, it, expect, afterEach, beforeEach, vi } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useAnchorLowWater } from '@/hooks/use-anchor-low-water'
import { resetQueryStore } from '@/lib/api-query'

// ADR 0272: the low-water clearance is worked out once, on the server, and the
// Anchor Watch page only reads it. The hook turns the wire answer into the
// shape the readings use and refuses an answer it cannot read.
function answer(body: unknown) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => body }))
}

describe('useAnchorLowWater', () => {
  beforeEach(() => resetQueryStore())
  afterEach(() => vi.unstubAllGlobals())

  it('reads a resolved verdict from /api/anchor-watch/low-water', async () => {
    answer({
      status: 'too_shallow',
      depth_now_m: 2,
      tide_fall_m: 0.61,
      depth_at_low_m: 1.39,
      draft_m: 1.2,
      clearance_m: 0.19,
      margin_m: 0.5,
      low_tide_time: '2026-11-08T04:45:00Z',
      worked_out_at: '2026-11-08T04:00:00Z',
    })
    const { result } = renderHook(() => useAnchorLowWater())

    await waitFor(() => expect(result.current.lowWater).not.toBeNull())
    expect(result.current.lowWater).toEqual({
      status: 'too_shallow',
      depthAtLowM: 1.39,
      clearanceM: 0.19,
      lowTideTime: '2026-11-08T04:45:00Z',
    })
    expect(result.current.failed).toBe(false)
    expect((fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0][0]).toContain('/api/anchor-watch/low-water')
  })

  it('reads an unknown verdict with its reason', async () => {
    answer({ status: 'unknown', reason: 'no_draft', margin_m: 0.5, worked_out_at: '2026-11-08T04:00:00Z' })
    const { result } = renderHook(() => useAnchorLowWater())

    await waitFor(() => expect(result.current.lowWater).not.toBeNull())
    expect(result.current.lowWater).toEqual({ status: 'unknown', reason: 'no_draft' })
    expect(result.current.workedOutAt).toBe('2026-11-08T04:00:00Z')
  })

  it('refuses a verdict it cannot read rather than guessing one', async () => {
    answer({ status: 'unknown', reason: 'no_such_reason', worked_out_at: '2026-11-08T04:00:00Z' })
    const { result } = renderHook(() => useAnchorLowWater())

    await waitFor(() => expect(result.current.failed).toBe(true))
    expect(result.current.lowWater).toBeNull()
  })

  it('refuses a resolved verdict that carries no figures', async () => {
    answer({ status: 'ok', worked_out_at: '2026-11-08T04:00:00Z' })
    const { result } = renderHook(() => useAnchorLowWater())

    await waitFor(() => expect(result.current.failed).toBe(true))
    expect(result.current.lowWater).toBeNull()
  })

  it('reports a failed request as an error with no verdict', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 502, json: async () => ({ error: 'the boat is unavailable' }) }))
    const { result } = renderHook(() => useAnchorLowWater())

    await waitFor(() => expect(result.current.failed).toBe(true))
    expect(result.current.lowWater).toBeNull()
  })

  it('refuses an answer with no worked_out_at', async () => {
    answer({ status: 'unknown', reason: 'no_draft' })
    const { result } = renderHook(() => useAnchorLowWater())

    await waitFor(() => expect(result.current.failed).toBe(true))
    expect(result.current.lowWater).toBeNull()
  })

  it('keeps the last verdict and its time, marked failed, when a later poll fails', async () => {
    const ok = {
      ok: true,
      json: async () => ({ status: 'unknown', reason: 'no_tide_station', worked_out_at: '2026-11-08T04:00:00Z' }),
    }
    const bad = { ok: false, status: 502, json: async () => ({ error: 'the boat is unavailable' }) }
    // The second poll waits until the verdict has been seen, so the test sees it.
    let release: () => void = () => {}
    const gate = new Promise<void>((resolve) => { release = resolve })
    const fetchMock = vi.fn().mockResolvedValueOnce(ok).mockImplementation(async () => { await gate; return bad })
    vi.stubGlobal('fetch', fetchMock)
    const { result } = renderHook(() => useAnchorLowWater(0.05))

    await waitFor(() => expect(result.current.lowWater).not.toBeNull())
    expect(result.current.failed).toBe(false)
    release()
    await waitFor(() => expect(result.current.failed).toBe(true))
    expect(result.current.lowWater).toEqual({ status: 'unknown', reason: 'no_tide_station' })
    expect(result.current.workedOutAt).toBe('2026-11-08T04:00:00Z')
  })
  // ADR 0274: the calibration arrives beside the station estimate.
  const CALIBRATED = {
    status: 'ok',
    depth_at_low_m: 2.4,
    depth_at_low_half_width_m: 0.2,
    clearance_m: 1.2,
    low_tide_time: '2026-11-08T04:45:00Z',
    cautious_source: 'shallowest_sounded',
    verdict_clearance_m: 0.5,
    verdict_half_width_m: 0.3,
    worked_out_at: '2026-11-08T04:00:00Z',
    calibration: {
      ready: true,
      hours_of_readings: 4.2,
      shallowest_sounded: { value_m: 2.0, half_width_m: 0.3 },
      shallowest_differs: true,
      high_tide_time: '2026-11-08T10:27:00Z',
      high_at_boat: { value_m: 5.1, half_width_m: 0.2 },
      deepest_sounded: { value_m: 5.4, half_width_m: 0.3 },
      station_match_note: 'The tide here does not follow it.',
    },
  }

  it('reads a calibrated depth with its interval and the figures beside it', async () => {
    answer(CALIBRATED)
    const { result } = renderHook(() => useAnchorLowWater())

    await waitFor(() => expect(result.current.lowWater).not.toBeNull())
    expect(result.current.lowWater).toEqual({
      status: 'ok',
      depthAtLowM: 2.4,
      depthAtLowHalfWidthM: 0.2,
      clearanceM: 1.2,
      lowTideTime: '2026-11-08T04:45:00Z',
      cautiousSource: 'shallowest_sounded',
      verdictClearanceM: 0.5,
      verdictHalfWidthM: 0.3,
      calibration: {
        ready: true,
        hoursOfReadings: 4.2,
        shallowestSounded: { depthM: 2.0, halfWidthM: 0.3 },
        shallowestDiffers: true,
        highTideTime: '2026-11-08T10:27:00Z',
        highAtBoat: { depthM: 5.1, halfWidthM: 0.2 },
        deepestSounded: { depthM: 5.4, halfWidthM: 0.3 },
        deepestDiffers: false,
        stationMatchNote: 'The tide here does not follow it.',
      },
    })
  })

  it('reads that the tide comes from the position model', async () => {
    answer({ ...CALIBRATED, tide_is_point: true })
    const { result } = renderHook(() => useAnchorLowWater())

    await waitFor(() => expect(result.current.lowWater).not.toBeNull())
    const v = result.current.lowWater
    expect(v?.status === 'ok' && v.tideIsPoint).toBe(true)
  })

  it('reads a station estimate with the calibration not ready yet', async () => {
    answer({
      status: 'ok', depth_at_low_m: 2.4, clearance_m: 1.2, low_tide_time: '2026-11-08T04:45:00Z',
      calibration: { ready: false, not_ready: 'readings_too_scattered', hours_of_readings: 2 },
      worked_out_at: '2026-11-08T04:00:00Z',
    })
    const { result } = renderHook(() => useAnchorLowWater())

    await waitFor(() => expect(result.current.lowWater).not.toBeNull())
    expect(result.current.lowWater).toMatchObject({
      calibration: { ready: false, notReady: 'readings_too_scattered', hoursOfReadings: 2 },
    })
  })

  it.each([
    ['a calibrated depth with no interval', { ...CALIBRATED, depth_at_low_half_width_m: undefined }],
    ['a ready calibration with no shallowest spot', { ...CALIBRATED, calibration: { ...CALIBRATED.calibration, shallowest_sounded: undefined } }],
    ['a calibration that is not ready and does not say why', { ...CALIBRATED, calibration: { ready: false, hours_of_readings: 1 } }],
    ['a calibrated verdict without the clearance behind it', { ...CALIBRATED, verdict_clearance_m: undefined }],
    ['a verdict resting on a figure it does not know', { ...CALIBRATED, cautious_source: 'hunch' }],
  ])('refuses %s rather than guessing', async (_name, body) => {
    answer(body)
    const { result } = renderHook(() => useAnchorLowWater())

    await waitFor(() => expect(result.current.failed).toBe(true))
    expect(result.current.lowWater).toBeNull()
  })
})
