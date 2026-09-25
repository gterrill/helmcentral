import { describe, it, expect, vi } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { useVesselSettings } from '@/hooks/use-vessel-settings'

const emptySettings = { engines: [], house_bank: null }
const emptyCandidates = { engines: [], batteries: [], detectors: {} }

describe('useVesselSettings', () => {
  it('fetches settings and candidates together on mount', async () => {
    const fetchMock = vi.fn(async (url: string) => {
      if (String(url).endsWith('/api/vessel')) {
        return { ok: true, json: async () => ({ engines: [{ instance: 'port', name: 'Port', equipment_id: '' }], house_bank: null }) }
      }
      if (String(url).endsWith('/api/vessel/candidates')) {
        return {
          ok: true,
          json: async () => ({
            engines: [{ instance: 'port', rpm: 1800, coolant_c: 76 }],
            batteries: [],
            detectors: { frozen: { ready: true }, battery: { ready: false, missing: 'Pick your house bank' }, engines: { ready: false, missing: 'Tick a second engine' } },
          }),
        }
      }
      throw new Error(`unexpected fetch ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useVesselSettings())
    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(result.current.settings.engines).toHaveLength(1)
    expect(result.current.settings.engines[0].instance).toBe('port')
    expect(result.current.candidates.engines[0].rpm).toBe(1800)
    expect(result.current.candidates.detectors.battery.missing).toBe('Pick your house bank')
    expect(result.current.error).toBeNull()
  })

  it('surfaces a fetch failure rather than masking it', async () => {
    const fetchMock = vi.fn(async (url: string) => {
      if (String(url).endsWith('/api/vessel')) return { ok: false, status: 500, json: async () => ({ error: 'boom' }) }
      return { ok: true, json: async () => emptyCandidates }
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useVesselSettings())
    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(result.current.error).toBe('boom')
  })

  it('save() POSTs the whole vessel block and refetches candidates afterward', async () => {
    let stored = emptySettings
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      if (String(url).endsWith('/api/vessel') && init?.method === 'POST') {
        stored = JSON.parse(String(init.body))
        return { ok: true, json: async () => stored }
      }
      if (String(url).endsWith('/api/vessel')) {
        return { ok: true, json: async () => stored }
      }
      if (String(url).endsWith('/api/vessel/candidates')) {
        return { ok: true, json: async () => emptyCandidates }
      }
      throw new Error(`unexpected fetch ${url} ${init?.method}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useVesselSettings())
    await waitFor(() => expect(result.current.loading).toBe(false))

    const next = { engines: [{ instance: 'port', name: 'Port', equipment_id: 'eq-1' }], house_bank: null }
    await act(async () => { await result.current.save(next) })

    expect(result.current.settings.engines[0].equipment_id).toBe('eq-1')
    // Initial GET x2, then the POST, then the refetch GET x2.
    expect(fetchMock).toHaveBeenCalledTimes(5)
    expect(result.current.saving).toBe(false)
  })

  it('save() rejects and leaves saving false when the server refuses', async () => {
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      if (String(url).endsWith('/api/vessel') && init?.method === 'POST') {
        return { ok: false, status: 400, json: async () => ({ error: 'invalid' }) }
      }
      if (String(url).endsWith('/api/vessel')) return { ok: true, json: async () => emptySettings }
      return { ok: true, json: async () => emptyCandidates }
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useVesselSettings())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await expect(act(async () => { await result.current.save(emptySettings) })).rejects.toThrow('invalid')
    expect(result.current.saving).toBe(false)
  })
})
