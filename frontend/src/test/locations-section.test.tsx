import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { LocationsSection } from '@/components/inventory/locations-section'
import type { InventoryZone } from '@/hooks/use-inventory'

let zones: InventoryZone[]
const fetchMock = vi.fn()

beforeEach(() => {
  zones = [
    {
      id: 'z1', name: 'Engine room (stbd)', sort_index: 0,
      bins: [{ id: 'b1', zone_id: 'z1', code: 'ER-01', name: '', sort_index: 0 }],
    },
  ]
  fetchMock.mockReset()
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'

    if (u.endsWith('/api/inventory/zones') && method === 'GET') {
      return Promise.resolve({ ok: true, json: async () => ({ zones }) })
    }
    if (u.endsWith('/api/inventory/zones') && method === 'POST') {
      const body = JSON.parse(String(init?.body)) as { name: string }
      zones = [...zones, { id: 'z2', name: body.name, sort_index: 1, bins: [] }]
      return Promise.resolve({ ok: true, json: async () => ({ zone: zones[zones.length - 1] }) })
    }
    if (u.match(/\/api\/inventory\/zones\/z1$/) && method === 'PUT') {
      const body = JSON.parse(String(init?.body)) as { name: string }
      zones = zones.map((z) => (z.id === 'z1' ? { ...z, name: body.name } : z))
      return Promise.resolve({ ok: true, json: async () => ({ zone: zones[0] }) })
    }
    if (u.match(/\/api\/inventory\/zones\/z1$/) && method === 'DELETE') {
      return Promise.resolve({ ok: false, status: 409, json: async () => ({ error: 'zone is in use by 1 item' }) })
    }
    if (u.endsWith('/api/inventory/bins') && method === 'POST') {
      const body = JSON.parse(String(init?.body)) as { zone_id: string; code: string; name: string }
      zones = zones.map((z) => (z.id === body.zone_id
        ? { ...z, bins: [...z.bins, { id: 'b2', zone_id: z.id, code: body.code, name: body.name, sort_index: z.bins.length }] }
        : z))
      return Promise.resolve({ ok: true, json: async () => ({ bin: { id: 'b2', zone_id: body.zone_id, code: body.code, name: body.name, sort_index: 1 } }) })
    }
    if (u.match(/\/api\/inventory\/bins\/b1$/) && method === 'DELETE') {
      return Promise.resolve({ ok: false, status: 409, json: async () => ({ error: 'bin is in use by 1 item' }) })
    }
    return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
  })
  vi.stubGlobal('fetch', fetchMock)
})

describe('LocationsSection', () => {
  it('renders each zone with its bins', async () => {
    render(<LocationsSection />)
    await screen.findByDisplayValue('Engine room (stbd)')
    expect(screen.getByDisplayValue('ER-01')).toBeInTheDocument()
  })

  it('adds a zone from the inline form', async () => {
    render(<LocationsSection />)
    await screen.findByDisplayValue('Engine room (stbd)')

    fireEvent.change(screen.getByLabelText('New zone name'), { target: { value: 'Lazarette' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add zone' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/inventory/zones'),
      expect.objectContaining({ method: 'POST' }),
    ))
    await screen.findByDisplayValue('Lazarette')
  })

  it('renames a zone on blur when the name changed', async () => {
    render(<LocationsSection />)
    const input = await screen.findByDisplayValue('Engine room (stbd)')

    fireEvent.change(input, { target: { value: 'Engine room' } })
    fireEvent.blur(input)

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/inventory/zones/z1'),
      expect.objectContaining({ method: 'PUT' }),
    ))
  })

  it('does not call the API when a zone name is blurred unchanged', async () => {
    render(<LocationsSection />)
    const input = await screen.findByDisplayValue('Engine room (stbd)')
    fetchMock.mockClear()

    fireEvent.blur(input)

    expect(fetchMock.mock.calls.some(([, init]) => (init as RequestInit | undefined)?.method === 'PUT')).toBe(false)
  })

  it('adds a bin from a zone\'s inline form', async () => {
    render(<LocationsSection />)
    await screen.findByDisplayValue('Engine room (stbd)')

    fireEvent.change(screen.getByLabelText('New bin code for Engine room (stbd)'), { target: { value: 'ER-02' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add bin' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/inventory/bins'),
      expect.objectContaining({ method: 'POST' }),
    ))
    await screen.findByDisplayValue('ER-02')
  })

  it('shows the server\'s 409 message when deleting a zone in use is refused', async () => {
    render(<LocationsSection />)
    await screen.findByDisplayValue('Engine room (stbd)')

    fireEvent.click(screen.getByRole('button', { name: 'Delete zone Engine room (stbd)' }))

    await screen.findByText('zone is in use by 1 item')
  })

  it('shows the server\'s 409 message when deleting a bin in use is refused', async () => {
    render(<LocationsSection />)
    await screen.findByDisplayValue('Engine room (stbd)')

    fireEvent.click(screen.getByRole('button', { name: 'Delete bin ER-01' }))

    await screen.findByText('bin is in use by 1 item')
  })
})
