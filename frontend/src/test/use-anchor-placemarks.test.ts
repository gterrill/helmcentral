import { describe, it, expect, beforeEach, vi } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { useAnchorPlacemarks } from '@/hooks/use-anchor-placemarks'
import { useAnchorWatch } from '@/hooks/use-anchor-watch'

vi.mock('sonner', () => ({ toast: { error: vi.fn() } }))

const PINS = [
  { id: 'p1', lat: -25.293, lon: 152.91, label: 'channel edge', created_at: '2026-11-18T00:00:00Z' },
  { id: 'p2', lat: -25.294, lon: 152.92, label: '', created_at: '2026-11-18T00:01:00Z' },
]

function stubServer() {
  const fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const path = String(url)
    if (path.includes('/api/anchor-watch/placemarks')) {
      return Promise.resolve({ ok: true, json: async () => ({ placemarks: PINS }) })
    }
    if (init?.method === 'DELETE') return Promise.resolve({ ok: true, json: async () => ({}) })
    return Promise.resolve({ ok: true, json: async () => ({ active: true, lat: -21.1, lon: 149.2, radius_meters: 20 }) })
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('useAnchorPlacemarks', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // Regression guard: pins can be dropped before anchoring, so the poll must
  // not wait on a watch.
  it('fetches the pins with no anchor watch active', async () => {
    const fetchMock = stubServer()
    const { result } = renderHook(() => useAnchorPlacemarks())
    await waitFor(() => expect(result.current.placemarks).toHaveLength(2))
    const urls = fetchMock.mock.calls.map((c) => String(c[0]))
    expect(urls.some((u) => u.includes('/api/anchor-watch/placemarks'))).toBe(true)
  })

  it('empties the pins on screen as soon as the anchor is raised', async () => {
    stubServer()
    const { result } = renderHook(() => ({ pins: useAnchorPlacemarks(), watch: useAnchorWatch() }))
    await waitFor(() => expect(result.current.pins.placemarks).toHaveLength(2))

    await act(async () => { await result.current.watch.clearAnchor() })

    expect(result.current.pins.placemarks).toHaveLength(0)
  })
})
