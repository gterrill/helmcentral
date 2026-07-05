import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'

import { useMarineWarnings } from '@/hooks/use-marine-warnings'

describe('useMarineWarnings', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('uses a 60-minute default refresh interval', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({}),
    })
    vi.stubGlobal('fetch', fetchMock)

    renderHook(() => useMarineWarnings())

    await act(async () => {
      await Promise.resolve()
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(59 * 60 * 1000)
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(60 * 1000)
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('produces no active warning when there are no bulletins', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ state: 'QLD', zone: 'Capricornia Coast', bulletins: [], has_active_warning: false }),
      }),
    )

    const { result } = renderHook(() => useMarineWarnings())
    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.activeWarning).toBeNull()
  })

  it('surfaces a bulletin whose section is active for the vessel zone', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          state: 'QLD',
          zone: 'Capricornia Coast',
          bulletins: [
            {
              product_id: 'IDQ20085',
              title: 'Marine Wind Warning Summary for Queensland',
              issued_at_raw: '11:51 am EST on Sunday 5 July 2026',
              issued_at: '2026-07-05T01:51:00Z',
              has_active_warning: true,
              has_active_warning_for_zone: true,
              details_url: 'http://www.bom.gov.au/qld/forecasts/map.shtml',
              sections: [
                { day: 'Sunday 5 July', warning_type: 'Strong Wind Warning', zones: ['Capricornia Coast'] },
              ],
            },
          ],
          has_active_warning: true,
        }),
      }),
    )

    const { result } = renderHook(() => useMarineWarnings())
    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.activeWarning).not.toBeNull()
    expect(result.current.activeWarning?.bulletins).toHaveLength(1)
    expect(result.current.activeWarning?.bulletins[0].sections[0].warningType).toBe('Strong Wind Warning')
    expect(result.current.activeWarning?.bulletins[0].detailsUrl).toBe('http://www.bom.gov.au/qld/forecasts/map.shtml')
  })

  it('defaults detailsUrl to empty string when the API omits details_url', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          state: 'QLD',
          zone: 'Capricornia Coast',
          bulletins: [
            {
              product_id: 'IDQ20085',
              title: 'Marine Wind Warning Summary for Queensland',
              has_active_warning: true,
              has_active_warning_for_zone: true,
              sections: [
                { day: 'Sunday 5 July', warning_type: 'Strong Wind Warning', zones: ['Capricornia Coast'] },
              ],
            },
          ],
          has_active_warning: true,
        }),
      }),
    )

    const { result } = renderHook(() => useMarineWarnings())
    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.activeWarning?.bulletins[0].detailsUrl).toBe('')
  })

  // Regression test for the backend fix: has_active_warning=true only means
  // "active somewhere in the state" - it must NOT drive the banner. Only
  // has_active_warning_for_zone should. A Hazardous Surf Warning active for
  // the Gold Coast must not alert a vessel near Yeppoon just because both are
  // in Queensland.
  it('does not surface a bulletin that is active for the state but not the vessel zone', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          state: 'QLD',
          zone: 'Capricornia Coast',
          bulletins: [
            {
              product_id: 'IDQ21285',
              title: 'Hazardous Surf Warning Summary for Queensland',
              issued_at_raw: '11:51 am EST on Sunday 5 July 2026',
              issued_at: '2026-07-05T01:51:00Z',
              has_active_warning: true,
              has_active_warning_for_zone: false,
              sections: [{ day: 'Sunday 5 July', warning_type: 'Hazardous Surf Warning', zones: ['Gold Coast'] }],
            },
          ],
          has_active_warning: false,
        }),
      }),
    )

    const { result } = renderHook(() => useMarineWarnings())
    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.activeWarning).toBeNull()
  })

  it('excludes cancellation sections even when the bulletin is zone-active', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          state: 'QLD',
          zone: 'Capricornia Coast',
          bulletins: [
            {
              product_id: 'IDQ20085',
              title: 'Marine Wind Warning Summary for Queensland',
              has_active_warning: true,
              has_active_warning_for_zone: true,
              sections: [{ day: 'Sunday 5 July', warning_type: 'Cancellation', zones: ['Capricornia Coast'] }],
            },
          ],
          has_active_warning: true,
        }),
      }),
    )

    const { result } = renderHook(() => useMarineWarnings())
    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.activeWarning).toBeNull()
  })

  it('surfaces nothing when zone is empty even if a bulletin claims zone-active', async () => {
    // VIC/TAS/WA incomplete zone tables: zone is "" so nothing should show.
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          state: 'VIC',
          zone: '',
          bulletins: [
            {
              product_id: 'IDV20000',
              title: 'Marine Wind Warning Summary for Victoria',
              has_active_warning: true,
              has_active_warning_for_zone: false,
              sections: [{ day: 'Sunday 5 July', warning_type: 'Strong Wind Warning', zones: ['Central Gippsland Coast'] }],
            },
          ],
          has_active_warning: false,
        }),
      }),
    )

    const { result } = renderHook(() => useMarineWarnings())
    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.activeWarning).toBeNull()
  })
})
