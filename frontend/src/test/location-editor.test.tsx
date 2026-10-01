import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { LocationEditor } from '@/components/inventory/location-editor'
import type { InventoryZone } from '@/hooks/use-inventory'

let zones: InventoryZone[]
const fetchMock = vi.fn()
let headerHost: HTMLElement

function putCalls(suffix: string) {
  return fetchMock.mock.calls.filter(([url, init]) =>
    String(url).endsWith(suffix) && (init as RequestInit | undefined)?.method === 'PUT')
}

beforeEach(() => {
  // The save bar portals into the app header's SaveBarSlot.
  headerHost = document.createElement('header')
  const slot = document.createElement('div')
  slot.id = 'save-bar-slot'
  headerHost.appendChild(slot)
  document.body.appendChild(headerHost)

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
    if (u.match(/\/api\/inventory\/zones\/z1$/) && method === 'PUT') {
      const body = JSON.parse(String(init?.body)) as { name: string }
      zones = zones.map((z) => (z.id === 'z1' ? { ...z, name: body.name } : z))
      return Promise.resolve({ ok: true, json: async () => ({ zone: zones[0] }) })
    }
    if (u.match(/\/api\/inventory\/zones\/z1$/) && method === 'DELETE') {
      return Promise.resolve({ ok: false, status: 409, json: async () => ({ error: 'location is in use by 1 item' }) })
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
    if (u.match(/\/api\/inventory\/bins\/b2$/) && method === 'DELETE') {
      zones = zones.map((z) => ({ ...z, bins: z.bins.filter((b) => b.id !== 'b2') }))
      return Promise.resolve({ ok: true, json: async () => ({}) })
    }
    return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
  })
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => { headerHost.remove() })

function renderEditor(props: Partial<React.ComponentProps<typeof LocationEditor>> = {}) {
  return render(<LocationEditor id="z1" onBack={vi.fn()} onDeleted={vi.fn()} {...props} />)
}

describe('LocationEditor', () => {
  it('shows the location name and its bins', async () => {
    renderEditor()
    expect(await screen.findByDisplayValue('Engine room (stbd)')).toBeInTheDocument()
    expect(screen.getByDisplayValue('ER-01')).toBeInTheDocument()
  })

  it('says so when the location does not exist', async () => {
    renderEditor({ id: 'nope' })
    await screen.findByText('This location could not be found.')
  })

  it('goes back through the Back control', async () => {
    const onBack = vi.fn()
    renderEditor({ onBack })
    await screen.findByDisplayValue('Engine room (stbd)')
    fireEvent.click(screen.getByRole('button', { name: 'Back' }))
    expect(onBack).toHaveBeenCalled()
  })

  it('never reports unsaved changes just from loading', async () => {
    const onDirtyChange = vi.fn()
    renderEditor({ onDirtyChange })
    await screen.findByDisplayValue('Engine room (stbd)')
    expect(onDirtyChange).not.toHaveBeenCalledWith(true)
  })

  it('renames through the save bar, and only once it is dirty', async () => {
    const onDirtyChange = vi.fn()
    renderEditor({ onDirtyChange })
    const input = await screen.findByLabelText('Location name')
    expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument()

    fireEvent.change(input, { target: { value: 'Engine room' } })
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))

    await waitFor(() => expect(putCalls('/api/inventory/zones/z1')).toHaveLength(1))
    expect(JSON.parse(String((putCalls('/api/inventory/zones/z1')[0][1] as RequestInit).body))).toEqual({ name: 'Engine room' })
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument())
    expect(onDirtyChange).toHaveBeenLastCalledWith(false)
  })

  it('discards an unsaved rename', async () => {
    renderEditor()
    const input = await screen.findByLabelText('Location name')
    fireEvent.change(input, { target: { value: 'Engine room' } })
    fireEvent.click(await screen.findByRole('button', { name: 'Discard' }))
    expect(screen.getByLabelText('Location name')).toHaveValue('Engine room (stbd)')
    expect(putCalls('/api/inventory/zones/z1')).toHaveLength(0)
  })

  it('shows the server message in the save bar when a rename is refused', async () => {
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      if (String(url).endsWith('/api/inventory/zones') && method === 'GET') {
        return Promise.resolve({ ok: true, json: async () => ({ zones }) })
      }
      return Promise.resolve({ ok: false, status: 409, json: async () => ({ error: 'that name is taken' }) })
    })
    renderEditor()
    fireEvent.change(await screen.findByLabelText('Location name'), { target: { value: 'Salon' } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    await screen.findByText('that name is taken')
  })

  it('adds a bin with a code and optional name', async () => {
    renderEditor()
    await screen.findByDisplayValue('Engine room (stbd)')

    fireEvent.change(screen.getByLabelText('New bin code'), { target: { value: 'ER-02' } })
    fireEvent.change(screen.getByLabelText('New bin name'), { target: { value: 'Spares' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add bin' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/inventory/bins'),
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ zone_id: 'z1', code: 'ER-02', name: 'Spares' }) }),
    ))
    await screen.findByDisplayValue('ER-02')
  })

  it('removes a bin', async () => {
    renderEditor()
    await screen.findByDisplayValue('Engine room (stbd)')
    fireEvent.change(screen.getByLabelText('New bin code'), { target: { value: 'ER-02' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add bin' }))
    await screen.findByDisplayValue('ER-02')

    fireEvent.click(screen.getByRole('button', { name: 'Remove bin ER-02' }))

    await waitFor(() => expect(screen.queryByDisplayValue('ER-02')).not.toBeInTheDocument())
  })

  it('shows the server message verbatim when removing a bin in use is refused', async () => {
    renderEditor()
    await screen.findByDisplayValue('Engine room (stbd)')
    fireEvent.click(screen.getByRole('button', { name: 'Remove bin ER-01' }))
    await screen.findByText('bin is in use by 1 item')
  })

  it('confirms before deleting the location, then reports it gone', async () => {
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      if (String(url).endsWith('/api/inventory/zones') && method === 'GET') {
        return Promise.resolve({ ok: true, json: async () => ({ zones }) })
      }
      if (String(url).endsWith('/api/inventory/zones/z1') && method === 'DELETE') {
        zones = []
        return Promise.resolve({ ok: true, json: async () => ({}) })
      }
      return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
    })
    const onDeleted = vi.fn()
    renderEditor({ onDeleted })
    await screen.findByDisplayValue('Engine room (stbd)')

    fireEvent.click(screen.getByRole('button', { name: 'More actions' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: /Delete location/ }))
    expect(onDeleted).not.toHaveBeenCalled()
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))

    await waitFor(() => expect(onDeleted).toHaveBeenCalled())
  })

  it('shows the server 409 message verbatim in the confirm and keeps the page', async () => {
    const onDeleted = vi.fn()
    renderEditor({ onDeleted })
    await screen.findByDisplayValue('Engine room (stbd)')

    fireEvent.click(screen.getByRole('button', { name: 'More actions' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: /Delete location/ }))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))

    await screen.findByText('location is in use by 1 item')
    expect(onDeleted).not.toHaveBeenCalled()
  })

  it('renders read-only without any write controls', async () => {
    renderEditor({ canWrite: false })
    const input = await screen.findByLabelText('Location name')
    expect(input).toBeDisabled()
    expect(screen.getByLabelText('Bin code')).toBeDisabled()
    expect(screen.queryByLabelText('New bin code')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Add bin' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Remove bin ER-01' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'More actions' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument()
  })

  // ADR 0127: each bin's code opens its bin page.
  it('opens the bin page when the bin code button is clicked', async () => {
    const onOpenBin = vi.fn()
    renderEditor({ onOpenBin })
    await screen.findByDisplayValue('Engine room (stbd)')
    fireEvent.click(screen.getByRole('button', { name: 'Open bin ER-01' }))
    expect(onOpenBin).toHaveBeenCalledWith('ER-01')
  })

  it('commits each bin field\'s own latest value on blur (quick code-then-name edit)', async () => {
    renderEditor()
    await screen.findByDisplayValue('Engine room (stbd)')
    const codeInput = screen.getByLabelText('Bin code')
    const nameInput = screen.getByLabelText('Bin name')

    fireEvent.change(codeInput, { target: { value: 'ER-02' } })
    fireEvent.blur(codeInput)
    fireEvent.change(nameInput, { target: { value: 'Spares' } })
    fireEvent.blur(nameInput)

    await waitFor(() => expect(putCalls('/api/inventory/bins/b1').length).toBeGreaterThanOrEqual(2))
    const calls = putCalls('/api/inventory/bins/b1')
    const lastBody = JSON.parse(String((calls[calls.length - 1][1] as RequestInit).body)) as { code: string; name: string }
    expect(lastBody.code).toBe('ER-02')
    expect(lastBody.name).toBe('Spares')
  })

  it('keeps the name field\'s typed value and focus through a code-save refetch', async () => {
    renderEditor()
    await screen.findByDisplayValue('Engine room (stbd)')
    const codeInput = screen.getByLabelText('Bin code')
    let nameInput = screen.getByLabelText('Bin name')

    fireEvent.change(codeInput, { target: { value: 'ER-02' } })
    fireEvent.blur(codeInput)
    nameInput.focus()
    fireEvent.change(nameInput, { target: { value: 'Spa' } })

    await waitFor(() => expect(putCalls('/api/inventory/bins/b1')).toHaveLength(1))
    await waitFor(() => expect(screen.getByLabelText('Bin code')).toHaveValue('ER-02'))

    nameInput = screen.getByLabelText('Bin name')
    expect(nameInput).toHaveValue('Spa')
    expect(nameInput).toHaveFocus()

    fireEvent.blur(nameInput)
    await waitFor(() => expect(putCalls('/api/inventory/bins/b1')).toHaveLength(2))
    const body = JSON.parse(String((putCalls('/api/inventory/bins/b1')[1][1] as RequestInit).body)) as { code: string; name: string }
    expect(body).toEqual({ code: 'ER-02', name: 'Spa' })
  })

  // ADR 0127: renaming a code orphans that bin's tags, and the rename UI says so.
  it('warns that tags no longer open the bin after its code changes', async () => {
    renderEditor()
    const input = await screen.findByDisplayValue('ER-01')
    fireEvent.change(input, { target: { value: 'ER-09' } })
    fireEvent.blur(input)
    await screen.findByText('Tags written for ER-01 no longer open this bin.')
  })

  it('does not warn on a name-only or unchanged commit', async () => {
    renderEditor()
    const input = await screen.findByDisplayValue('ER-01')
    fireEvent.blur(input)
    expect(screen.queryByText(/no longer open this bin/)).not.toBeInTheDocument()
  })

  it('does not show the rename warning until the rename actually succeeds', async () => {
    let resolvePut!: () => void
    const putPromise = new Promise<void>((resolve) => { resolvePut = resolve })
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const u = String(url)
      const method = init?.method ?? 'GET'
      if (u.endsWith('/api/inventory/zones') && method === 'GET') {
        return Promise.resolve({ ok: true, json: async () => ({ zones }) })
      }
      if (u.match(/\/api\/inventory\/bins\/b1$/) && method === 'PUT') {
        const body = JSON.parse(String(init?.body)) as { code?: string; name?: string }
        return putPromise.then(() => {
          zones = zones.map((z) => ({ ...z, bins: z.bins.map((b) => (b.id === 'b1' ? { ...b, ...body } : b)) }))
          return { ok: true, json: async () => ({ bin: zones[0].bins[0] }) }
        })
      }
      return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
    })
    renderEditor()
    const input = await screen.findByDisplayValue('ER-01')
    fireEvent.change(input, { target: { value: 'ER-09' } })
    fireEvent.blur(input)
    expect(screen.queryByText(/no longer open this bin/)).not.toBeInTheDocument()
    resolvePut()
    await screen.findByText('Tags written for ER-01 no longer open this bin.')
  })

  it('clears the rename warning and shows the failure when a later rename fails', async () => {
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const u = String(url)
      const method = init?.method ?? 'GET'
      if (u.endsWith('/api/inventory/zones') && method === 'GET') {
        return Promise.resolve({ ok: true, json: async () => ({ zones }) })
      }
      if (u.match(/\/api\/inventory\/bins\/b1$/) && method === 'PUT') {
        const body = JSON.parse(String(init?.body)) as { code?: string; name?: string }
        if (body.code === 'ER-09') {
          zones = zones.map((z) => ({ ...z, bins: z.bins.map((b) => (b.id === 'b1' ? { ...b, ...body } : b)) }))
          return Promise.resolve({ ok: true, json: async () => ({ bin: zones[0].bins[0] }) })
        }
        return Promise.resolve({ ok: false, status: 409, json: async () => ({ error: 'bin code already in use' }) })
      }
      return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
    })
    renderEditor()
    const input = await screen.findByDisplayValue('ER-01')
    fireEvent.change(input, { target: { value: 'ER-09' } })
    fireEvent.blur(input)
    await screen.findByText('Tags written for ER-01 no longer open this bin.')

    const updated = screen.getByLabelText('Bin code')
    fireEvent.change(updated, { target: { value: 'ER-10' } })
    fireEvent.blur(updated)

    await screen.findByText('bin code already in use')
    expect(screen.queryByText(/no longer open this bin/)).not.toBeInTheDocument()
  })
})
