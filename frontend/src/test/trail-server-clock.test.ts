import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useAisTrails } from '@/hooks/use-ais-anchor-tracks'
import { useAnchorTrail } from '@/hooks/use-anchor-trail'
import { serverNowMs } from '@/lib/server-clock'
import { resetTelemetryStoresForTest } from '@/lib/test-reset'
import { trailNow } from '@/lib/trail-age-bands'

function makeResponse(body: unknown): Response {
  return { ok: true, json: async () => body } as Response
}

const SERVER = Date.UTC(2026, 10, 5, 12, 0, 0)
const HOUR = 60 * 60 * 1000

afterEach(() => {
  resetTelemetryStoresForTest()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('trail ages on the shared server clock', () => {
  it('uses the browser clock until a server time has arrived', () => {
    vi.useFakeTimers({ now: SERVER })
    expect(serverNowMs()).toBeNull()
    expect(trailNow()).toBe(SERVER)
  })

  it('is fed by an answer from the AIS endpoint', async () => {
    vi.useFakeTimers({ now: SERVER + 3 * HOUR })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      makeResponse({ trail_started_at: 'A', cursor: 0, tracks: [], server_time_ms: SERVER }),
    ))
    renderHook(() => useAisTrails(5000))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(Math.abs(trailNow() - SERVER)).toBeLessThan(1000)
  })

  it('is fed by an answer from the own trail endpoint', async () => {
    vi.useFakeTimers({ now: SERVER - 2 * HOUR })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      makeResponse({ points: [], trail_started_at: 'A', server_time_ms: SERVER }),
    ))
    renderHook(() => useAnchorTrail(5000))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(Math.abs(trailNow() - SERVER)).toBeLessThan(1000)
  })

  it('keeps AIS points that are 23 hours old by the server clock while the browser clock is 3 hours ahead', async () => {
    vi.useFakeTimers({ now: SERVER + 3 * HOUR })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(makeResponse({
      trail_started_at: 'A',
      cursor: 2,
      server_time_ms: SERVER,
      tracks: [{
        mmsi: '111',
        points: [
          { lat: -20, lon: 149, t_ms: SERVER - 23 * HOUR, new_segment: true },
          { lat: -20, lon: 149.001, t_ms: SERVER - 22 * HOUR },
        ],
      }],
    })))
    const { result } = renderHook(() => useAisTrails(5000))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(result.current.get('111')).toHaveLength(2)
  })
})
