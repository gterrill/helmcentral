import { describe, expect, it, vi, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'

import { formatClock, formatDate } from '@/hooks/use-vessel-identity'

async function loadHookModule() {
  vi.resetModules()
  return import('@/hooks/use-vessel-identity')
}

function stubVesselStateFetch(vesselState: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/api/vessel-state')) {
        return { ok: true, json: async () => vesselState }
      }
      // /api/settings (useVesselIdentity's own fetch and useAppConfig's) —
      // ok:false so both fall back to their compiled defaults undisturbed.
      return { ok: false, json: async () => ({}) }
    }),
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
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

describe('useVesselIdentity', () => {
  it('exposes the vessel-local zone reported by /api/vessel-state', async () => {
    stubVesselStateFetch({ datetime: '2026-09-12T20:31:00Z', timezone: 'Etc/GMT-10' })
    const { useVesselIdentity } = await loadHookModule()

    const { result } = renderHook(() => useVesselIdentity())

    await waitFor(() => expect(result.current.timeZone).toBe('Etc/GMT-10'))
  })

  it('does not set a timezone the backend never reported', async () => {
    stubVesselStateFetch({ datetime: '2026-09-12T20:31:00Z' })
    const { useVesselIdentity } = await loadHookModule()

    const { result } = renderHook(() => useVesselIdentity())

    // signalkConnected starts null and is only ever set once the fetch
    // response has actually been read, so waiting on it is proof the
    // (timezone-less) response was processed rather than still in flight.
    await waitFor(() => expect(result.current.signalkConnected).toBe(false))
    expect(result.current.timeZone).toBeUndefined()
  })
})
