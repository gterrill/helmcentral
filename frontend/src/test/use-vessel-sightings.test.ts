import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'

import { useVesselSightings } from '@/hooks/use-vessel-sightings'

describe('useVesselSightings', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('does not fetch when key is null', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    renderHook(() => useVesselSightings(null))

    await act(async () => {
      await Promise.resolve()
    })

    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('fetches the sightings endpoint for the given key, URL-encoded', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ sightings: [] }),
    })
    vi.stubGlobal('fetch', fetchMock)

    // Any key containing reserved characters exercises the encoding - this
    // isn't tied to a particular key format, just generic URL-safety.
    renderHook(() => useVesselSightings('abc:123 xyz'))

    await act(async () => {
      await Promise.resolve()
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/nearby-vessels/abc%3A123%20xyz/sightings')
  })

  it('returns parsed sightings and flips loading to false once resolved', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          sightings: [
            { seen_at: '2026-07-10T09:14:00Z', lat: -21.59, lon: 149.79, geoname: 'Airlie Beach', nav_context: 'anchored' },
          ],
        }),
      }),
    )

    const { result } = renderHook(() => useVesselSightings('316042555'))

    expect(result.current.loading).toBe(true)

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.loading).toBe(false)
    expect(result.current.sightings).toEqual([
      { seen_at: '2026-07-10T09:14:00Z', lat: -21.59, lon: 149.79, geoname: 'Airlie Beach', nav_context: 'anchored' },
    ])
  })

  it('clears sightings and stops loading when the key becomes null again', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          sightings: [
            { seen_at: '2026-07-10T09:14:00Z', lat: -21.59, lon: 149.79, geoname: 'Airlie Beach', nav_context: 'anchored' },
          ],
        }),
      }),
    )

    const { result, rerender } = renderHook(({ key }: { key: string | null }) => useVesselSightings(key), {
      initialProps: { key: '316042555' as string | null },
    })

    await act(async () => {
      await Promise.resolve()
    })
    expect(result.current.sightings).toHaveLength(1)

    rerender({ key: null })

    expect(result.current.sightings).toEqual([])
    expect(result.current.loading).toBe(false)
  })

  it('leaves sightings empty on a failed fetch', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, json: async () => ({}) }))

    const { result } = renderHook(() => useVesselSightings('316042555'))

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.loading).toBe(false)
    expect(result.current.sightings).toEqual([])
  })
})
