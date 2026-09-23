import { describe, expect, it, vi, afterEach, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'

import { formatClock, formatDate, formatWeekday, isSameLocalDate } from '@/hooks/use-vessel-identity'

// use-vessel-identity.ts is a module-level shared store (item C): a single
// 1s clock and a single subscription to the SSE `vessel-state` event, shared
// by every component that calls useVesselIdentity() regardless of how many
// there are. Mock the shared telemetry module rather than standing up a real
// EventSource -- use-telemetry-stream.test.ts already covers the transport --
// and capture the listener/unsubscribe so the store tests below can drive and
// inspect it directly.
const subscribeTelemetryMock = vi.fn()
const unsubscribeMock = vi.fn()
let capturedListener: ((raw: string) => void) | null = null

vi.mock('@/hooks/use-telemetry-stream', () => ({
  subscribeTelemetry: (event: string, cb: (raw: string) => void) => {
    capturedListener = cb
    subscribeTelemetryMock(event, cb)
    return unsubscribeMock
  },
}))

// boat.model now comes through the existing use-app-config single-flight
// source rather than a separate poll — see config/app-config.ts's
// normalizeBoatModel and its own use-app-config.test.ts coverage.
vi.mock('@/hooks/use-app-config', () => ({
  useAppConfig: () => ({ boatModel: 'Riviera 445' }),
}))

/** Reloads the module fresh so its module-level store (subscriber count,
 * snapshot, clock timer) doesn't leak state between tests. */
async function loadHookModule() {
  vi.resetModules()
  subscribeTelemetryMock.mockClear()
  unsubscribeMock.mockClear()
  capturedListener = null
  return import('@/hooks/use-vessel-identity')
}

function emitVesselState(payload: unknown) {
  act(() => {
    capturedListener?.(JSON.stringify(payload))
  })
}

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

// The wall display's browser sits on UTC while the boat is at UTC+10 (live
// finding: the dashboard clock read 08:31 PM at 06:31 AM boat time). These
// pin formatClock/formatDate to an explicit vessel zone rather than the
// browser's, so the assertions hold regardless of the machine running the
// suite — which happens to be Australia/Lindeman, itself UTC+10, so a test
// that only omitted timeZone would not actually prove anything.

describe('formatClock', () => {
  it('renders vessel-local time for a zone east of UTC, not the browser zone', () => {
    const instant = new Date('2026-09-12T20:31:00Z')

    const { timePart, meridiem } = formatClock(instant, 'Etc/GMT-10')

    expect(`${timePart} ${meridiem}`).toBe('06:31:00 AM')
  })

  it('falls back to the browser zone without throwing when given an unknown zone name', () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const instant = new Date('2026-09-12T20:31:00Z')

    let result: ReturnType<typeof formatClock> | undefined
    expect(() => {
      result = formatClock(instant, 'Not/AZone')
    }).not.toThrow()

    expect(result).toEqual(formatClock(instant))
    expect(warnSpy).toHaveBeenCalledTimes(1)
  })
})

describe('formatDate', () => {
  it('crosses the vessel-local midnight boundary independently of the browser zone', () => {
    // 2026-09-12T20:31Z is already Sep 13 local at UTC+10, but still Sep 12 at UTC.
    const instant = new Date('2026-09-12T20:31:00Z')

    expect(formatDate(instant, { timeZone: 'Etc/GMT-10' })).toContain('Sep 13, 2026')
    expect(formatDate(instant, { timeZone: 'UTC' })).toContain('Sep 12, 2026')
  })

  it('falls back to the browser zone without throwing when given an unknown zone name', () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const instant = new Date('2026-09-12T20:31:00Z')

    let result: string | undefined
    expect(() => {
      result = formatDate(instant, { timeZone: 'Also/NotAZone' })
    }).not.toThrow()

    expect(result).toBe(formatDate(instant))
    expect(warnSpy).toHaveBeenCalledTimes(1)
  })
})

// ADR 0125: the clock tile's ETA line prefixes a 3-letter weekday when the
// ETA isn't today in vessel-local time (HelmCast did this).
describe('formatWeekday', () => {
  it('names the weekday in the given vessel-local zone, independent of the browser zone', () => {
    // 2026-09-12T20:31Z is Sep 12 (Saturday) at UTC, but already Sep 13
    // (Sunday) at UTC+10.
    const instant = new Date('2026-09-12T20:31:00Z')

    expect(formatWeekday(instant, 'UTC')).toBe('Sat')
    expect(formatWeekday(instant, 'Etc/GMT-10')).toBe('Sun')
  })
})

describe('isSameLocalDate', () => {
  it('is true for two instants on the same calendar date in the given zone', () => {
    const a = new Date('2026-09-12T01:00:00Z')
    const b = new Date('2026-09-12T23:00:00Z')
    expect(isSameLocalDate(a, b, 'UTC')).toBe(true)
  })

  it('is false once the zone-local calendar date differs', () => {
    const a = new Date('2026-09-12T23:00:00Z')
    const b = new Date('2026-09-13T01:00:00Z')
    expect(isSameLocalDate(a, b, 'UTC')).toBe(false)
  })

  it('crosses the vessel-local midnight boundary independently of the browser zone', () => {
    // Same UTC instants as the "false" case above, but at UTC+10 both
    // already fall on Sep 13 - the zone the comparison runs in changes the
    // answer, exactly like formatDate's own boundary-crossing test above.
    const a = new Date('2026-09-12T23:00:00Z')
    const b = new Date('2026-09-13T01:00:00Z')
    expect(isSameLocalDate(a, b, 'Etc/GMT-10')).toBe(true)
  })
})

describe('useVesselIdentity', () => {
  it('exposes the vessel-local zone reported over the vessel-state stream', async () => {
    const { useVesselIdentity } = await loadHookModule()
    const { result, unmount } = renderHook(() => useVesselIdentity())

    emitVesselState({ datetime: '2026-09-12T20:31:00Z', timezone: 'Etc/GMT-10' })

    expect(result.current.timeZone).toBe('Etc/GMT-10')
    unmount()
  })

  it('does not set a timezone the backend never reported, and reads signalkConnected off the source field', async () => {
    const { useVesselIdentity } = await loadHookModule()
    const { result, unmount } = renderHook(() => useVesselIdentity())

    emitVesselState({ datetime: '2026-09-12T20:31:00Z' })

    expect(result.current.timeZone).toBeUndefined()
    // No `source` field on this payload, same as the backend answering with
    // anything other than 'signalk'.
    expect(result.current.signalkConnected).toBe(false)
    unmount()
  })

  it('flips signalkConnected true once a payload reports source: signalk', async () => {
    const { useVesselIdentity } = await loadHookModule()
    const { result, unmount } = renderHook(() => useVesselIdentity())

    emitVesselState({ datetime: '2026-09-12T20:31:00Z', source: 'signalk' })

    expect(result.current.signalkConnected).toBe(true)
    unmount()
  })

  it('builds the boat name from name and vessel_prefix', async () => {
    const { useVesselIdentity } = await loadHookModule()
    const { result, unmount } = renderHook(() => useVesselIdentity())

    emitVesselState({ name: 'Pikorua', vessel_prefix: 'M/V' })

    expect(result.current.boatName).toBe('M/V Pikorua')
    unmount()
  })

  it('defaults the prefix to M/V when the backend omits it', async () => {
    const { useVesselIdentity } = await loadHookModule()
    const { result, unmount } = renderHook(() => useVesselIdentity())

    emitVesselState({ name: 'Pikorua' })

    expect(result.current.boatName).toBe('M/V Pikorua')
    unmount()
  })

  it('updates vesselStatus from the stream, defaulting to At Anchor before any event arrives', async () => {
    const { useVesselIdentity } = await loadHookModule()
    const { result, unmount } = renderHook(() => useVesselIdentity())

    expect(result.current.vesselStatus).toBe('At Anchor')

    emitVesselState({ status: 'Underway' })

    expect(result.current.vesselStatus).toBe('Underway')
    unmount()
  })

  it('reads the boat model through useAppConfig rather than a poll of its own', async () => {
    const { useVesselIdentity } = await loadHookModule()
    const { result, unmount } = renderHook(() => useVesselIdentity())

    expect(result.current.boatModel).toBe('Riviera 445')
    unmount()
  })
})

// Item C: use-vessel-identity.ts is a ref-counted module-level store
// (following use-app-config.ts's and use-telemetry-stream.ts's existing
// singleton patterns), not one setInterval/one SSE subscription per
// consumer — vessel-status-bar.tsx (always mounted), marine-header.tsx and
// clock-tile.tsx used to each run their own.
describe('useVesselIdentity shared store', () => {
  it('subscribes to the vessel-state stream once no matter how many components mount', async () => {
    const { useVesselIdentity } = await loadHookModule()

    const a = renderHook(() => useVesselIdentity())
    const b = renderHook(() => useVesselIdentity())

    expect(subscribeTelemetryMock).toHaveBeenCalledTimes(1)
    expect(subscribeTelemetryMock).toHaveBeenCalledWith('vessel-state', expect.any(Function))

    a.unmount()
    b.unmount()
  })

  it('starts one shared 1s clock no matter how many components mount', async () => {
    const setIntervalSpy = vi.spyOn(globalThis, 'setInterval')
    const { useVesselIdentity } = await loadHookModule()

    const a = renderHook(() => useVesselIdentity())
    const callsAfterFirstMount = setIntervalSpy.mock.calls.length
    expect(callsAfterFirstMount).toBeGreaterThan(0)

    const b = renderHook(() => useVesselIdentity())

    // A second subscriber must not arm a second clock timer.
    expect(setIntervalSpy.mock.calls.length).toBe(callsAfterFirstMount)

    a.unmount()
    b.unmount()
    setIntervalSpy.mockRestore()
  })

  it('ticks every subscriber\'s `now` together from the one shared timer', async () => {
    const { useVesselIdentity } = await loadHookModule()
    const a = renderHook(() => useVesselIdentity())
    const b = renderHook(() => useVesselIdentity())

    const beforeA = a.result.current.now.getTime()
    const beforeB = b.result.current.now.getTime()

    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })

    expect(a.result.current.now.getTime()).toBe(beforeA + 1000)
    expect(b.result.current.now.getTime()).toBe(beforeB + 1000)

    a.unmount()
    b.unmount()
  })

  it('unsubscribes from the stream and stops the clock only once the last subscriber unmounts', async () => {
    const clearIntervalSpy = vi.spyOn(globalThis, 'clearInterval')
    const { useVesselIdentity } = await loadHookModule()

    const a = renderHook(() => useVesselIdentity())
    const b = renderHook(() => useVesselIdentity())

    a.unmount()
    expect(unsubscribeMock).not.toHaveBeenCalled()
    expect(clearIntervalSpy).not.toHaveBeenCalled()

    b.unmount()
    expect(unsubscribeMock).toHaveBeenCalledTimes(1)
    expect(clearIntervalSpy).toHaveBeenCalledTimes(1)

    clearIntervalSpy.mockRestore()
  })

  it('starts a fresh subscription and clock for a new subscriber after the last one unsubscribed', async () => {
    const { useVesselIdentity } = await loadHookModule()

    const a = renderHook(() => useVesselIdentity())
    a.unmount()
    expect(unsubscribeMock).toHaveBeenCalledTimes(1)

    const b = renderHook(() => useVesselIdentity())
    expect(subscribeTelemetryMock).toHaveBeenCalledTimes(2)

    b.unmount()
  })
})
