import { describe, it, expect, vi } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { useIgnoredSensors } from '@/hooks/use-ignored-sensors'

describe('useIgnoredSensors', () => {
  it('fetches the current list on mount', async () => {
    const fetchMock = vi.fn(async () => ({ ok: true, json: async () => ({ identifiers: ['propulsion.port.exhaustTemperature'] }) }))
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useIgnoredSensors())
    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(result.current.identifiers).toEqual(['propulsion.port.exhaustTemperature'])
    expect(result.current.error).toBeNull()
  })

  it('ignore() POSTs the identifier and updates the list from the response', async () => {
    const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'POST') {
        expect(JSON.parse(String(init.body))).toEqual({ identifier: 'venus.battery.512' })
        return { ok: true, json: async () => ({ identifiers: ['venus.battery.512'] }) }
      }
      return { ok: true, json: async () => ({ identifiers: [] }) }
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useIgnoredSensors())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => { await result.current.ignore('venus.battery.512') })
    expect(result.current.identifiers).toEqual(['venus.battery.512'])
  })

  it('unignore() DELETEs and removes it from the local list', async () => {
    const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'DELETE') return { ok: true, json: async () => ({}) }
      return { ok: true, json: async () => ({ identifiers: ['a', 'b'] }) }
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useIgnoredSensors())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => { await result.current.unignore('a') })
    expect(result.current.identifiers).toEqual(['b'])
    expect(fetchMock).toHaveBeenLastCalledWith('/api/alarms/ignored-sensors/a', { method: 'DELETE' })
  })

  it('surfaces a fetch failure rather than masking it', async () => {
    const fetchMock = vi.fn(async () => ({ ok: false, status: 500, json: async () => ({ error: 'boom' }) }))
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useIgnoredSensors())
    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(result.current.error).toBe('boom')
  })
})
