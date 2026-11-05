import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useAisTrails } from '@/hooks/use-ais-anchor-tracks'
import { useAnchorTrail } from '@/hooks/use-anchor-trail'
import { resetTrailClock, serverNow, setServerTimeMs, subscribeTrailClock } from '@/lib/trail-clock'

function makeResponse(body: unknown): Response {
  return { ok: true, json: async () => body } as Response
}

const SERVER = Date.UTC(2026, 10, 5, 12, 0, 0)
const HOUR = 60 * 60 * 1000

afterEach(() => {
  resetTrailClock()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('trail clock', () => {
  it('is the browser clock until a server time arrives, and ignores a bad one', () => {
    vi.useFakeTimers({ now: SERVER })
    expect(serverNow()).toBe(SERVER)
    setServerTimeMs(undefined)
    setServerTimeMs(Number.NaN)
    setServerTimeMs('soon')
    expect(serverNow()).toBe(SERVER)
  })

  it('follows the server when the browser clock is off', () => {
    vi.useFakeTimers({ now: SERVER + 3 * HOUR })
    setServerTimeMs(SERVER)
    expect(serverNow()).toBe(SERVER)
    vi.advanceTimersByTime(5000)
    expect(serverNow()).toBe(SERVER + 5000)
  })

  it('tells a subscriber only when the offset moves by a second or more', () => {
    vi.useFakeTimers({ now: SERVER })
    const listener = vi.fn()
    subscribeTrailClock(listener)
    setServerTimeMs(SERVER + 3 * HOUR)
    expect(listener).toHaveBeenCalledTimes(1)
    setServerTimeMs(SERVER + 3 * HOUR + 400)
    expect(listener).toHaveBeenCalledTimes(1)
    setServerTimeMs(SERVER + 3 * HOUR + 1500)
    expect(listener).toHaveBeenCalledTimes(2)
  })

  it('is fed by an answer from the AIS endpoint', async () => {
    vi.useFakeTimers({ now: SERVER + 3 * HOUR })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      makeResponse({ trail_started_at: 'A', cursor: 0, tracks: [], server_time_ms: SERVER }),
    ))
    renderHook(() => useAisTrails(5000))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(serverNow()).toBe(SERVER)
  })

  it('is fed by an answer from the own trail endpoint', async () => {
    vi.useFakeTimers({ now: SERVER - 2 * HOUR })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      makeResponse({ points: [], trail_started_at: 'A', server_time_ms: SERVER }),
    ))
    renderHook(() => useAnchorTrail(5000))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(serverNow()).toBe(SERVER)
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
