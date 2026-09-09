import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'

import { findActiveSurfBulletin, useForecastWarnings } from '@/hooks/use-forecast-warnings'
import type { ForecastWarnings } from '@/hooks/use-forecast-warnings'

describe('useForecastWarnings', () => {
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

    renderHook(() => useForecastWarnings())

    await act(async () => {
      await Promise.resolve()
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledWith('/api/forecast-warnings')

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
        json: async () => ({ provider: 'bom', region: 'Capricornia Coast', bulletins: [] }),
      }),
    )

    const { result } = renderHook(() => useForecastWarnings())
    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.activeWarning).toBeNull()
  })

  it('maps every bulletin/section returned by the API - the backend already scopes them to the vessel', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          provider: 'bom',
          region: 'Capricornia Coast',
          bulletins: [
            {
              id: 'IDQ20085',
              title: 'Marine Wind Warning Summary for Queensland',
              issued_at: '2026-07-05T01:51:00Z',
              details_url: 'http://www.bom.gov.au/qld/forecasts/map.shtml',
              category: 'wind',
              sections: [{ day: 'Sunday 5 July', warning_type: 'Strong Wind Warning' }],
            },
          ],
          cached: false,
          updated_at: '2026-07-05T02:00:00Z',
          ttl_seconds: 3600,
        }),
      }),
    )

    const { result } = renderHook(() => useForecastWarnings())
    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.activeWarning).not.toBeNull()
    expect(result.current.activeWarning?.provider).toBe('bom')
    expect(result.current.activeWarning?.region).toBe('Capricornia Coast')
    expect(result.current.activeWarning?.bulletins).toHaveLength(1)
    expect(result.current.activeWarning?.bulletins[0].id).toBe('IDQ20085')
    expect(result.current.activeWarning?.bulletins[0].sections[0].warningType).toBe('Strong Wind Warning')
    expect(result.current.activeWarning?.bulletins[0].detailsUrl).toBe('http://www.bom.gov.au/qld/forecasts/map.shtml')
    expect(result.current.activeWarning?.bulletins[0].issuedAt).toBe('2026-07-05T01:51:00Z')
    expect(result.current.isCached).toBe(false)
    expect(result.current.updatedAt).toBe('2026-07-05T02:00:00Z')
    expect(result.current.ttlSeconds).toBe(3600)
  })

  it('defaults detailsUrl to empty string when the API omits details_url', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          provider: 'bom',
          region: 'Capricornia Coast',
          bulletins: [
            {
              id: 'IDQ20085',
              title: 'Marine Wind Warning Summary for Queensland',
              sections: [{ day: 'Sunday 5 July', warning_type: 'Strong Wind Warning' }],
            },
          ],
        }),
      }),
    )

    const { result } = renderHook(() => useForecastWarnings())
    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.activeWarning?.bulletins[0].detailsUrl).toBe('')
  })

  it('defaults issuedAt to null when the API omits or blanks issued_at', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          provider: 'bom',
          region: 'Capricornia Coast',
          bulletins: [
            {
              id: 'IDQ20085',
              title: 'Marine Wind Warning Summary for Queensland',
              issued_at: '',
              sections: [{ day: 'Sunday 5 July', warning_type: 'Strong Wind Warning' }],
            },
          ],
        }),
      }),
    )

    const { result } = renderHook(() => useForecastWarnings())
    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.activeWarning?.bulletins[0].issuedAt).toBeNull()
  })

  it('surfaces an error message when the fetch fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        json: async () => ({ error: 'boom' }),
      }),
    )

    const { result } = renderHook(() => useForecastWarnings())
    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.error).toBe('boom')
    expect(result.current.activeWarning).toBeNull()
  })
})

// Mirrors findActiveWindBulletin's own contract (ADR 0087 adds the surf
// path alongside the wind one): the first surf bulletin that actually has
// sections, since a bulletin with none is the plugin listing a category
// with nothing currently active in it.
describe('findActiveSurfBulletin', () => {
  const windBulletin = {
    id: 'IDQ20085',
    title: 'Marine Wind Warning Summary for Queensland',
    issuedAt: null,
    detailsUrl: '',
    category: 'wind',
    sections: [{ day: 'Sunday 5 July', warningType: 'Strong Wind Warning' }],
  }

  it('returns the first surf bulletin with sections', () => {
    const surfBulletin = {
      id: 'IDQ21285',
      title: 'Hazardous Surf Warning Summary for Queensland',
      issuedAt: null,
      detailsUrl: 'http://www.bom.gov.au/qld/',
      category: 'surf',
      sections: [{ day: 'Sunday 5 July', warningType: 'Hazardous Surf Warning' }],
    }
    const warnings: ForecastWarnings = {
      provider: 'bom',
      region: 'Capricornia Coast',
      bulletins: [windBulletin, surfBulletin],
    }

    expect(findActiveSurfBulletin(warnings)).toBe(surfBulletin)
  })

  it('returns undefined when there is no surf bulletin', () => {
    const warnings: ForecastWarnings = { provider: 'bom', region: 'Capricornia Coast', bulletins: [windBulletin] }
    expect(findActiveSurfBulletin(warnings)).toBeUndefined()
  })

  it('returns undefined when the only surf bulletin has no sections', () => {
    const warnings: ForecastWarnings = {
      provider: 'bom',
      region: 'Capricornia Coast',
      bulletins: [{ id: 'IDQ21285', title: 'Hazardous Surf Warning Summary for Queensland', issuedAt: null, detailsUrl: '', category: 'surf', sections: [] }],
    }
    expect(findActiveSurfBulletin(warnings)).toBeUndefined()
  })

  it('returns undefined for null warnings', () => {
    expect(findActiveSurfBulletin(null)).toBeUndefined()
  })
})
