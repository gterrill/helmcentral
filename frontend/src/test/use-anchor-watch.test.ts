import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useAnchorWatch } from '@/hooks/use-anchor-watch'
import { ANCHOR_WATCH_ACTIVE_REFRESH_SECONDS, ANCHOR_WATCH_IDLE_REFRESH_SECONDS } from '@/config/app-config'
import { toast } from 'sonner'

vi.mock('sonner', () => ({ toast: { error: vi.fn() } }))

describe('useAnchorWatch mutation failures', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ active: true, lat: -21.1, lon: 149.2, radius_meters: 20 }),
    }))
  })

  it.each([
    ['Drop', 'Could not drop anchor'],
    ['Raise', 'Could not raise anchor'],
  ])('%s surfaces publish and network failures without changing watch state', async (operation, title) => {
    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2))
    await act(async () => { await Promise.resolve() })
    const before = result.current
    for (const failure of ['http', 'network', 'non-json']) {
      if (failure === 'network') vi.mocked(fetch).mockRejectedValueOnce(new Error('Connection lost'))
      else vi.mocked(fetch).mockResolvedValueOnce({
        ok: false, status: 502,
        json: async () => {
          if (failure === 'non-json') throw new Error('Not JSON')
          return { error: 'SignalK publish failed' }
        },
      } as Response)
      await act(async () => {
        if (operation === 'Drop') await result.current.setAnchorHere(-22, 150, { planningDepthM: null, planningTideHeightFt: null })
        else await result.current.clearAnchor()
      })
      expect(toast.error).toHaveBeenLastCalledWith(title, {
        description: failure === 'http' ? 'SignalK publish failed' : failure === 'network' ? 'Connection lost' : 'HTTP 502',
      })
      expect(result.current.anchorState).toBe(before.anchorState)
      expect(result.current.anchorLat).toBe(before.anchorLat)
      expect(result.current.anchorLon).toBe(before.anchorLon)
    }
    expect(toast.error).toHaveBeenCalledTimes(3)
  })

  it('only clears local state after a successful Raise', async () => {
    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2))
    await act(async () => { await Promise.resolve() })
    await act(async () => { await result.current.clearAnchor() })
    expect(result.current.anchorState).toBe('none')
    expect(toast.error).not.toHaveBeenCalled()
  })
})

// setAnchorHere is fed the live GPS fix (App.tsx / anchor-watch-tile.tsx), so
// it must ask the backend to apply the bow-offset correction (projecting
// forward by gps_from_bow_m along heading).
describe('useAnchorWatch bow-offset request shape', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ active: true, lat: -21.1, lon: 149.2 }),
    }))
  })

  it('setAnchorHere posts apply_bow_offset: true', async () => {
    const fetchMock = vi.mocked(fetch)
    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2))

    // Let the initial GET /api/anchor-watch on mount resolve and clear.
    await act(async () => { await Promise.resolve() })
    fetchMock.mockClear()

    await act(async () => {
      await result.current.setAnchorHere(-21.1, 149.2, { planningDepthM: 5, planningTideHeightFt: 2 })
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/anchor-watch', expect.objectContaining({
      method: 'POST',
    }))
    const [, init] = fetchMock.mock.calls[0]
    const body = JSON.parse(init!.body as string)
    expect(body.apply_bow_offset).toBe(true)
  })
})

// The planning depth / planning tide pair (ADR 0063) — a contemporaneous
// pair, carried by POST since a drop must never round-trip to a tide
// provider before a watch exists.
describe('useAnchorWatch planning-depth capture', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ active: true, lat: -21.1, lon: 149.2 }),
    }))
  })

  it('setAnchorHere posts the depth and tide pair when both are known', async () => {
    const fetchMock = vi.mocked(fetch)
    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2))
    await act(async () => { await Promise.resolve() })
    fetchMock.mockClear()

    await act(async () => {
      await result.current.setAnchorHere(-21.1, 149.2, { planningDepthM: 6.4, planningTideHeightFt: 1.8 })
    })

    const [, init] = fetchMock.mock.calls[0]
    const body = JSON.parse(init!.body as string)
    expect(body.planning_depth_m).toBe(6.4)
    expect(body.planning_tide_height_ft).toBe(1.8)
  })

  it('setAnchorHere posts -1 sentinels when there is no reading, rather than omitting the fields', async () => {
    const fetchMock = vi.mocked(fetch)
    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2))
    await act(async () => { await Promise.resolve() })
    fetchMock.mockClear()

    await act(async () => {
      await result.current.setAnchorHere(-21.1, 149.2, { planningDepthM: null, planningTideHeightFt: null })
    })

    const [, init] = fetchMock.mock.calls[0]
    const body = JSON.parse(init!.body as string)
    expect(body.planning_depth_m).toBe(-1)
    expect(body.planning_tide_height_ft).toBe(-1)
  })
})

// P1 from the impeccable critique of the anchor-watch map (2026-09-25):
// updateRadius silently no-op'd on a failed PATCH, so a rejected alarm-radius
// change looked identical to a successful one. Fixed by routing all three
// PATCH mutations through anchorRequest (same as setAnchorHere already does),
// which throws on a non-OK response or a network error rather
// than swallowing it — the caller (the drawer's radius stepper, the rode
// planner's Apply-as-alarm-radius path) is what shows the toast, so the hook
// itself just has to not eat the failure.
describe('useAnchorWatch updateRadius/updateRodeAndConditions/updatePlanningDepth failures', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ active: true, lat: -21.1, lon: 149.2, radius_meters: 20, rode_deployed_m: 30, planning_depth_m: 5, planning_tide_height_ft: 1 }),
    }))
  })

  const calls: Array<[string, (r: ReturnType<typeof useAnchorWatch>) => Promise<void>]> = [
    ['updateRadius', (r) => r.updateRadius(40)],
    ['adjustAnchor', (r) => r.adjustAnchor({ lat: -21.2, lon: 149.3, radiusMeters: 40 })],
    ['updateRodeAndConditions', (r) => r.updateRodeAndConditions(30, 'calm', 'sand')],
    ['updatePlanningDepth', (r) => r.updatePlanningDepth(5, 1)],
  ]

  it.each(calls)('%s throws on a non-OK response and leaves state unchanged', async (_name, call) => {
    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2))
    await act(async () => { await Promise.resolve() })
    const before = result.current

    vi.mocked(fetch).mockResolvedValueOnce({
      ok: false,
      status: 400,
      json: async () => ({ error: 'bad request' }),
    } as Response)

    await expect(act(async () => { await call(result.current) })).rejects.toThrow('bad request')

    expect(result.current.radiusMeters).toBe(before.radiusMeters)
    expect(result.current.rodeDeployedM).toBe(before.rodeDeployedM)
    expect(result.current.planningDepthM).toBe(before.planningDepthM)
  })

  it.each(calls)('%s throws on a network error and leaves state unchanged', async (_name, call) => {
    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2))
    await act(async () => { await Promise.resolve() })
    const before = result.current

    vi.mocked(fetch).mockRejectedValueOnce(new Error('Connection lost'))

    await expect(act(async () => { await call(result.current) })).rejects.toThrow('Connection lost')

    expect(result.current.radiusMeters).toBe(before.radiusMeters)
    expect(result.current.rodeDeployedM).toBe(before.rodeDeployedM)
    expect(result.current.planningDepthM).toBe(before.planningDepthM)
  })

  it.each(calls)('%s throws on a 500 with a non-JSON body, surfacing the HTTP status', async (_name, call) => {
    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2))
    await act(async () => { await Promise.resolve() })

    vi.mocked(fetch).mockResolvedValueOnce({
      ok: false,
      status: 500,
      json: async () => { throw new Error('Not JSON') },
    } as unknown as Response)

    await expect(act(async () => { await call(result.current) })).rejects.toThrow('HTTP 500')
  })
})

// updatePlanningDepth PATCHes the planning-depth pair — mirrors
// updateRodeAndConditions exactly (no optimistic update, replaces state with
// the server echo).
describe('useAnchorWatch updatePlanningDepth', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ active: true, lat: -21.1, lon: 149.2, planning_depth_m: 8, planning_tide_height_ft: 2.1 }),
    }))
  })

  it('PATCHes planning_depth_m and planning_tide_height_ft together', async () => {
    const fetchMock = vi.mocked(fetch)
    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2))
    await act(async () => { await Promise.resolve() })
    fetchMock.mockClear()

    await act(async () => {
      await result.current.updatePlanningDepth(8, 2.1)
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/anchor-watch', expect.objectContaining({ method: 'PATCH' }))
    const [, init] = fetchMock.mock.calls[0]
    const body = JSON.parse(init!.body as string)
    expect(body).toEqual({ planning_depth_m: 8, planning_tide_height_ft: 2.1 })
  })

  it('replaces state with the server echo rather than updating optimistically', async () => {
    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2))
    await act(async () => { await Promise.resolve() })

    await act(async () => {
      await result.current.updatePlanningDepth(8, 2.1)
    })

    expect(result.current.planningDepthM).toBe(8)
    expect(result.current.planningTideHeightFt).toBe(2.1)
  })
})

// adjustAnchor (the Adjust mode's Set write, ADR 0133's amendment): one
// atomic PATCH carrying lat, lon and radius_meters together — mirrors
// updatePlanningDepth exactly (no optimistic update, replaces state with the
// server echo).
describe('useAnchorWatch adjustAnchor', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ active: true, lat: -21.2, lon: 149.3, radius_meters: 40 }),
    }))
  })

  it('PATCHes lat, lon and radius_meters together', async () => {
    const fetchMock = vi.mocked(fetch)
    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2))
    await act(async () => { await Promise.resolve() })
    fetchMock.mockClear()

    await act(async () => {
      await result.current.adjustAnchor({ lat: -21.2, lon: 149.3, radiusMeters: 40 })
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/anchor-watch', expect.objectContaining({ method: 'PATCH' }))
    const [, init] = fetchMock.mock.calls[0]
    const body = JSON.parse(init!.body as string)
    expect(body).toEqual({ lat: -21.2, lon: 149.3, radius_meters: 40 })
  })

  it('replaces state with the server echo rather than updating optimistically', async () => {
    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2))
    await act(async () => { await Promise.resolve() })

    await act(async () => {
      await result.current.adjustAnchor({ lat: -21.2, lon: 149.3, radiusMeters: 40 })
    })

    expect(result.current.anchorLat).toBe(-21.2)
    expect(result.current.anchorLon).toBe(149.3)
    expect(result.current.radiusMeters).toBe(40)
  })

  // Code-review finding: Set (and Undo) used to send lat/lon unconditionally,
  // even for a radius-only change — the backend treats any lat/lon as a
  // genuine reposition (resets the self trail, requires a fresh SignalK
  // publish, 502s if SignalK is down). adjustAnchor must accept lat/lon as
  // optional (both or neither) and PATCH only radius_meters when they're
  // omitted.
  it('PATCHes radius_meters alone when lat/lon are omitted (a radius-only Adjust Set)', async () => {
    const fetchMock = vi.mocked(fetch)
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => ({ active: true, lat: -21.1, lon: 149.2, radius_meters: 40 }),
    } as Response)
    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2))
    await act(async () => { await Promise.resolve() })
    fetchMock.mockClear()

    await act(async () => {
      await result.current.adjustAnchor({ radiusMeters: 40 })
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/anchor-watch', expect.objectContaining({ method: 'PATCH' }))
    const [, init] = fetchMock.mock.calls[0]
    const body = JSON.parse(init!.body as string)
    expect(body).toEqual({ radius_meters: 40 })
  })
})

// Item A: /api/anchor-watch polls faster while a watch is set (place-name
// pinning and cross-session edits land close to the backend's own 5s
// track-poll cadence) and slower while idle (nothing changes it but an
// explicit operator action, which already applies optimistically).
describe('useAnchorWatch poll cadence', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('polls at the idle cadence while no watch is set', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ active: false }) })
    vi.stubGlobal('fetch', fetchMock)

    renderHook(() => useAnchorWatch(-21.1, 149.2))
    await act(async () => { await Promise.resolve() })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await act(async () => { await vi.advanceTimersByTimeAsync(ANCHOR_WATCH_IDLE_REFRESH_SECONDS * 1000 - 1000) })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('switches to the active cadence once the watch comes back active', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ active: true, lat: -21.1, lon: 149.2, radius_meters: 20 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    renderHook(() => useAnchorWatch(-21.1, 149.2))
    await act(async () => { await Promise.resolve() })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    // The idle-cadence interval armed on mount is torn down and replaced by
    // the active one once `active: true` lands — advancing by the (much
    // shorter) active interval is enough to see the next poll, which would
    // not happen this soon if the idle interval were still in effect.
    await act(async () => { await vi.advanceTimersByTimeAsync(ANCHOR_WATCH_ACTIVE_REFRESH_SECONDS * 1000) })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})

// Item D: every SSE-driven vessel-state tick re-renders App and therefore
// re-runs this hook. Without memoization the tile/drawer would see a new
// `watch` object identity every second even while anchored at a fixed spot,
// defeating their own React.memo.
describe('useAnchorWatch memoized result', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ active: false }) }))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns the same object reference across a re-render when nothing changed', async () => {
    const { result, rerender } = renderHook(
      ({ lat, lon }: { lat: number; lon: number }) => useAnchorWatch(lat, lon),
      { initialProps: { lat: -21.1, lon: 149.2 } },
    )
    await act(async () => { await Promise.resolve() })
    const first = result.current

    rerender({ lat: -21.1, lon: 149.2 })

    expect(result.current).toBe(first)
  })

  it('keeps stable identities for updateRadius, clearAnchor and updateRodeAndConditions across re-renders', async () => {
    const { result, rerender } = renderHook(
      ({ lat, lon }: { lat: number; lon: number }) => useAnchorWatch(lat, lon),
      { initialProps: { lat: -21.1, lon: 149.2 } },
    )
    await act(async () => { await Promise.resolve() })
    const first = result.current

    rerender({ lat: -21.1, lon: 149.2 })

    expect(result.current.updateRadius).toBe(first.updateRadius)
    expect(result.current.clearAnchor).toBe(first.clearAnchor)
    expect(result.current.updateRodeAndConditions).toBe(first.updateRodeAndConditions)
  })

  it('returns a new object once the underlying distance/bearing actually changes', async () => {
    // Distance/bearing are only derived from the vessel fix while a watch is
    // active (anchorLat/anchorLon non-null) — with no watch set, moving the
    // vessel changes nothing about the (all-null/defaulted) output, so this
    // needs an active watch to exercise the "did anything really change" path.
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ active: true, lat: -21.1, lon: 149.2, radius_meters: 20 }),
    }))
    const { result, rerender } = renderHook(
      ({ lat, lon }: { lat: number; lon: number }) => useAnchorWatch(lat, lon),
      { initialProps: { lat: -21.1, lon: 149.2 } },
    )
    await act(async () => { await Promise.resolve() })
    const first = result.current

    rerender({ lat: -21.2, lon: 149.3 })

    expect(result.current).not.toBe(first)
    expect(result.current.distanceMeters).not.toBe(first.distanceMeters)
  })
})

// `loaded` disambiguates setAt/anchorLat being null for "no watch is
// running" from "the first GET hasn't answered yet" — AnchorWatchMap uses it
// to decide whether a stored map centre from a past anchorage should still
// be discarded (anchor-session-recenter.test.tsx covers that consumer).
describe('useAnchorWatch loaded', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('starts false and flips true once the first GET resolves successfully', async () => {
    let resolveFetch!: (value: { ok: boolean; json: () => Promise<unknown> }) => void
    vi.stubGlobal('fetch', vi.fn().mockReturnValue(new Promise((resolve) => { resolveFetch = resolve })))

    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2))
    expect(result.current.loaded).toBe(false)

    await act(async () => {
      resolveFetch({ ok: true, json: async () => ({ active: false }) })
      await Promise.resolve()
    })

    expect(result.current.loaded).toBe(true)
  })

  // Fallback policy: a failed/errored poll must not be mistaken for a
  // confirmed "no watch" — that would be exactly the fallback that hides a
  // real fetch failure by reading "attempted" as "resolved". The next poll
  // (the idle-cadence interval, already covered on its own by the "poll
  // cadence" describe block above) is what actually gets a fresh try.
  it.each([
    ['a non-ok response', () => Promise.resolve({ ok: false, status: 502, json: async () => ({}) })],
    ['a network error', () => Promise.reject(new Error('Connection lost'))],
  ])('stays false after %s, and a later successful retry still flips it true', async (_label, failingFetch) => {
    vi.useFakeTimers()
    const fetchMock = vi.fn().mockImplementationOnce(failingFetch)
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2))
    await act(async () => { await Promise.resolve() })
    expect(result.current.loaded).toBe(false)

    fetchMock.mockResolvedValueOnce({ ok: true, json: async () => ({ active: false }) })
    await act(async () => { await vi.advanceTimersByTimeAsync(ANCHOR_WATCH_IDLE_REFRESH_SECONDS * 1000) })

    expect(result.current.loaded).toBe(true)

    vi.useRealTimers()
  })

  // code-review finding: `loaded` used to be set only by fetchState (the GET
  // poll). A successful mutation response is just as authoritative about
  // "we have heard from the server" as a GET — and on a fresh mount, the
  // operator can drop anchor (or the tile can auto-populate a session write)
  // before the first GET has even resolved, so waiting on the GET alone
  // would report "not loaded" while a real, current server state already
  // sits in hand.
  it('flips loaded true on a successful setAnchorHere even while the initial GET is still in flight', async () => {
    const fetchMock = vi.fn().mockImplementation((_url: string, init?: RequestInit) => {
      if (init === undefined) {
        // The initial GET /api/anchor-watch poll — left permanently pending
        // for this test, so `loaded` can only come from the POST below.
        return new Promise(() => {})
      }
      return Promise.resolve({ ok: true, json: async () => ({ active: true, lat: -21.1, lon: 149.2 }) })
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2))
    expect(result.current.loaded).toBe(false)

    await act(async () => {
      await result.current.setAnchorHere(-21.1, 149.2, { planningDepthM: null, planningTideHeightFt: null })
    })

    expect(result.current.loaded).toBe(true)
  })

  it.each([
    ['updateRadius', (r: ReturnType<typeof useAnchorWatch>) => r.updateRadius(25)],
    ['adjustAnchor', (r: ReturnType<typeof useAnchorWatch>) => r.adjustAnchor({ lat: -21.2, lon: 149.3, radiusMeters: 40 })],
    ['updateRodeAndConditions', (r: ReturnType<typeof useAnchorWatch>) => r.updateRodeAndConditions(30, 'calm', 'sand')],
    ['updatePlanningDepth', (r: ReturnType<typeof useAnchorWatch>) => r.updatePlanningDepth(5, 1)],
    ['clearAnchor', (r: ReturnType<typeof useAnchorWatch>) => r.clearAnchor()],
  ])('flips loaded true on a successful %s while the initial GET is still in flight', async (_name, call) => {
    const fetchMock = vi.fn().mockImplementation((_url: string, init?: RequestInit) => {
      if (init === undefined) {
        return new Promise(() => {})
      }
      return Promise.resolve({ ok: true, json: async () => ({ active: true, lat: -21.1, lon: 149.2, radius_meters: 20 }) })
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2))
    expect(result.current.loaded).toBe(false)

    await act(async () => { await call(result.current) })

    expect(result.current.loaded).toBe(true)
  })
})

// The backend puts a damaged anchor_watch.json into an explicit error state
// rather than an invented or empty watch: GET /api/anchor-watch reports it as
// an `error` string naming the file path and the parse error, alongside
// active: false. This hook must pass that straight through so the tile and
// the drawer can show it, and must not confuse it with the ordinary "no
// watch is set" case that also has active: false but no error.
describe('useAnchorWatch error', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('surfaces the server error field once the GET resolves', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ active: false, error: 'parsing anchor watch state (data/anchor_watch.json): unexpected end of JSON input' }),
    }))

    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2))
    await act(async () => { await Promise.resolve() })

    expect(result.current.error).toBe('parsing anchor watch state (data/anchor_watch.json): unexpected end of JSON input')
    expect(result.current.anchorState).toBe('none')
  })

  it('is null for the ordinary no-watch-set response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ active: false }),
    }))

    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2))
    await act(async () => { await Promise.resolve() })

    expect(result.current.error).toBeNull()
  })

  it('is null once a later successful poll reports an active watch', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ active: false, error: 'parsing anchor watch state (data/anchor_watch.json): boom' }) })
      .mockResolvedValue({ ok: true, json: async () => ({ active: true, lat: -21.1, lon: 149.2, radius_meters: 20 }) })
    vi.useFakeTimers()
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2))
    await act(async () => { await Promise.resolve() })
    expect(result.current.error).toBe('parsing anchor watch state (data/anchor_watch.json): boom')

    await act(async () => { await vi.advanceTimersByTimeAsync(ANCHOR_WATCH_IDLE_REFRESH_SECONDS * 1000) })
    expect(result.current.error).toBeNull()
    expect(result.current.anchorState).toBe('set')

    vi.useRealTimers()
  })
})
