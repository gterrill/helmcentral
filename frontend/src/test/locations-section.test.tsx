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
    if (u.match(/\/api\/inventory\/bins\/b1$/) && method === 'PUT') {
      const body = JSON.parse(String(init?.body)) as { code?: string; name?: string }
      zones = zones.map((z) => ({ ...z, bins: z.bins.map((b) => (b.id === 'b1' ? { ...b, ...body } : b)) }))
      const updated = zones.flatMap((z) => z.bins).find((b) => b.id === 'b1')
      return Promise.resolve({ ok: true, json: async () => ({ bin: updated }) })
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

  it('commits each field\'s own latest value on blur, not the other field\'s stale value from the last render (quick code-then-name edit)', async () => {
    render(<LocationsSection />)
    await screen.findByDisplayValue('Engine room (stbd)')

    const codeInput = screen.getByLabelText('Bin code')
    const nameInput = screen.getByLabelText('Bin name')

    // Edit code, blur (fires the code PUT), then edit name and blur again -
    // all before a re-render can land, the same "quickly editing code then
    // name" sequence the bug report describes.
    fireEvent.change(codeInput, { target: { value: 'ER-02' } })
    fireEvent.blur(codeInput)
    fireEvent.change(nameInput, { target: { value: 'Spares' } })
    fireEvent.blur(nameInput)

    await waitFor(() => {
      const putCalls = fetchMock.mock.calls.filter(([url, init]) =>
        String(url).endsWith('/api/inventory/bins/b1') && (init as RequestInit | undefined)?.method === 'PUT')
      expect(putCalls.length).toBeGreaterThanOrEqual(2)
    })

    const putCalls = fetchMock.mock.calls.filter(([url, init]) =>
      String(url).endsWith('/api/inventory/bins/b1') && (init as RequestInit | undefined)?.method === 'PUT')
    const lastBody = JSON.parse(String((putCalls[putCalls.length - 1][1] as RequestInit).body)) as { code: string; name: string }
    // The name blur must send the code the operator just typed, not revert
    // it to whatever the code field held before this edit started.
    expect(lastBody.code).toBe('ER-02')
    expect(lastBody.name).toBe('Spares')
  })

  it('keeps the name field\'s typed value and focus through a code-save refetch, then sends both on its own blur', async () => {
    render(<LocationsSection />)
    await screen.findByDisplayValue('Engine room (stbd)')

    const codeInput = screen.getByLabelText('Bin code')
    let nameInput = screen.getByLabelText('Bin name')

    // Edit code and blur it - this fires the code PUT and the refetch that
    // follows it. Before either resolves, the operator has already tabbed
    // into name and started typing.
    fireEvent.change(codeInput, { target: { value: 'ER-02' } })
    fireEvent.blur(codeInput)
    nameInput.focus()
    fireEvent.change(nameInput, { target: { value: 'Spa' } })

    // Let the code PUT and its refetch land - zones now carries the new
    // code under the same bin id, name untouched by that save.
    await waitFor(() => {
      const putCalls = fetchMock.mock.calls.filter(([url, init]) =>
        String(url).endsWith('/api/inventory/bins/b1') && (init as RequestInit | undefined)?.method === 'PUT')
      expect(putCalls).toHaveLength(1)
    })
    await waitFor(() => expect(screen.getByLabelText('Bin code')).toHaveValue('ER-02'))

    // The refetch must not have remounted the row: the name field keeps
    // whatever was typed into it, and keeps focus.
    nameInput = screen.getByLabelText('Bin name')
    expect(nameInput).toHaveValue('Spa')
    expect(nameInput).toHaveFocus()

    fireEvent.blur(nameInput)

    await waitFor(() => {
      const putCalls = fetchMock.mock.calls.filter(([url, init]) =>
        String(url).endsWith('/api/inventory/bins/b1') && (init as RequestInit | undefined)?.method === 'PUT')
      expect(putCalls).toHaveLength(2)
    })
    const putCalls = fetchMock.mock.calls.filter(([url, init]) =>
      String(url).endsWith('/api/inventory/bins/b1') && (init as RequestInit | undefined)?.method === 'PUT')
    const lastBody = JSON.parse(String((putCalls[1][1] as RequestInit).body)) as { code: string; name: string }
    // The name blur sends the new code plus the new name - not a stale
    // pre-edit code reverted by a remount.
    expect(lastBody.code).toBe('ER-02')
    expect(lastBody.name).toBe('Spa')
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

  // ADR 0127: "reaching it without a tag" - a bin's code opens its bin page.
  it('opens the bin page when the bin code button is clicked', async () => {
    const onOpenBin = vi.fn()
    render(<LocationsSection onOpenBin={onOpenBin} />)
    await screen.findByDisplayValue('Engine room (stbd)')

    fireEvent.click(screen.getByRole('button', { name: 'Open bin ER-01' }))

    expect(onOpenBin).toHaveBeenCalledWith('ER-01')
  })

  // ADR 0127: renaming a code orphans that bin's tags, and the rename UI
  // says so.
  it('warns that tags no longer open the bin after its code changes on blur', async () => {
    render(<LocationsSection />)
    const input = await screen.findByDisplayValue('ER-01')

    fireEvent.change(input, { target: { value: 'ER-09' } })
    fireEvent.blur(input)

    await screen.findByText('Tags written for ER-01 no longer open this bin.')
  })

  it('does not warn when a bin is renamed with the same code (name-only edit)', async () => {
    render(<LocationsSection />)
    const input = await screen.findByDisplayValue('ER-01')

    fireEvent.blur(input)

    expect(screen.queryByText(/no longer open this bin/)).not.toBeInTheDocument()
  })
})
