import { describe, it, expect, vi } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useVesselDiscovery } from '@/hooks/use-vessel-discovery'
import { invalidateQueries } from '@/lib/api-query'

const body = {
  connected: true,
  groups: [{ kind: 'engine', mappable: true, rows: [{ source: 'propulsion.port', instance: 'port', bus_name: '', charger_input: false, reporting: true, metrics: null, item: null }] }],
  detectors: { frozen: { ready: true }, battery: { ready: false, missing: 'Choose your house bank under Roles' } },
}

describe('useVesselDiscovery', () => {
  it('reads what the boat reports and each check\'s setup status', async () => {
    const fetchMock = vi.fn(async (url: string) => { void url; return { ok: true, json: async () => body } })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useVesselDiscovery())
    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/api\/vessel\/discovery$/)
    expect(result.current.discovery.connected).toBe(true)
    expect(result.current.discovery.groups[0].rows[0].source).toBe('propulsion.port')
    // A row's metrics are always a list, even when the server sent none.
    expect(result.current.discovery.groups[0].rows[0].metrics).toEqual([])
    expect(result.current.discovery.detectors.battery.missing).toBe('Choose your house bank under Roles')
    expect(result.current.error).toBeNull()
  })

  it('never leaves the tracking or signature label undefined', async () => {
    await invalidateQueries(['/api/vessel/discovery'])
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, json: async () => body })))

    const { result } = renderHook(() => useVesselDiscovery())
    await waitFor(() => expect(result.current.discovery.connected).toBe(true))

    expect(result.current.discovery.tracking).toEqual({ since: null, seeded: false, covers: false, error: '' })
    expect(result.current.discovery.groups[0].signature_label).toBe('')
    expect(result.current.discovery.groups[0].rows[0].last_seen).toBeNull()
  })

  it('surfaces a failure rather than masking it', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 500, json: async () => ({ error: 'boom' }) })))

    const { result } = renderHook(() => useVesselDiscovery())
    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(result.current.error).toBe('boom')
  })
})
