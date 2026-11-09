import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'

import { resetQueryStore } from '@/lib/api-query'
import { useConfiguredTideChart } from '@/hooks/use-configured-tide-chart'

const reply = (status: number, body: unknown) => ({ ok: status < 400, status, json: async () => body })

describe('useConfiguredTideChart', () => {
  beforeEach(() => resetQueryStore())
  afterEach(() => vi.unstubAllGlobals())

  it('asks for the configured chart with no provider and no station, and reads who answered', async () => {
    const fetchMock = vi.fn().mockResolvedValue(reply(200, {
      station: { station_id: '45.45,12.35', name: "Boat's position", lat: 45.45, lon: 12.35 },
      extremes: [], current_height_m: -0.4, direction: 'Falling',
      height_datum: 'mean_sea_level', provider: 'open-meteo', provider_name: 'Open-Meteo', position_based: true,
    }))
    vi.stubGlobal('fetch', fetchMock)
    const { result } = renderHook(() => useConfiguredTideChart('open-meteo|', true))
    await waitFor(() => expect(result.current.chart).not.toBeNull())
    expect(fetchMock.mock.calls[0][0]).toMatch(/\/api\/tide-chart$/)
    expect(result.current.positionBased).toBe(true)
    expect(result.current.providerName).toBe('Open-Meteo')
    expect(result.current.chart?.heightDatum).toBe('mean_sea_level')
  })

  it('sends only the place for a place, and nothing about a station', async () => {
    const fetchMock = vi.fn().mockResolvedValue(reply(200, { station: { station_id: 'X', name: 'X' }, extremes: [] }))
    vi.stubGlobal('fetch', fetchMock)
    renderHook(() => useConfiguredTideChart('bom', true, { lat: -27.2, lon: 153.1 }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(fetchMock.mock.calls[0][0]).toMatch(/\/api\/tide-chart\?lat=-27\.2&lon=153\.1$/)
  })

  it('reads the server\'s reason: no station chosen, or not set up', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(reply(502, { error: 'no tide station configured', reason: 'no_station' })))
    const { result } = renderHook(() => useConfiguredTideChart('bom|', true))
    await waitFor(() => expect(result.current.error).not.toBeNull())
    expect(result.current.noStation).toBe(true)
    expect(result.current.notSetUp).toBe(false)
  })

  it('does not fetch while disabled', () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    renderHook(() => useConfiguredTideChart('bom|', false))
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
