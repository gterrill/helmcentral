import { renderHook, act } from '@testing-library/react'
import { vi, describe, it, expect } from 'vitest'
import { useServerTrails } from '../hooks/use-server-trails'

describe('useServerTrails', () => {
  it('updates ais trails', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        ais: {
          "vessel1": [
            { lat: 10, lon: 20, timestamp: "2026-06-11T20:00:00Z" },
            { lat: 11, lon: 21, timestamp: "2026-06-11T20:00:05Z" }
          ]
        }
      })
    })
    vi.stubGlobal('fetch', mockFetch)

    const { result } = renderHook(() => useServerTrails(5000))
    
    await act(async () => {
      await new Promise(r => setTimeout(r, 100))
    })

    const aisTrails = result.current.getAisTrails()
    console.log("AIS TRAILS KEYS:", Array.from(aisTrails.keys()))
    expect(aisTrails.get('vessel1')?.length).toBe(2)
  })
})
