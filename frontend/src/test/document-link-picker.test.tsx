import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { DocumentLinkPicker } from '@/components/inventory/document-link-picker'

const fetchMock = vi.fn()

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
})

describe('DocumentLinkPicker', () => {
  it('does not search while closed', () => {
    render(<DocumentLinkPicker open={false} onOpenChange={vi.fn()} onPick={vi.fn()} />)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('searches GET /api/documents?q= and lists results', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => ({
        results: [
          { document_id: 'd1', title: 'Onan Operator Manual', filename: 'onan-manual.pdf', status: 'indexed', page: 1, snippet: '', folder_id: null },
        ],
      }),
    })
    render(<DocumentLinkPicker open onOpenChange={vi.fn()} onPick={vi.fn()} />)

    fireEvent.change(screen.getByLabelText('Search documents'), { target: { value: 'onan' } })

    await screen.findByText('Onan Operator Manual')
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining('/api/documents?q=onan'))
  })

  it('excludes already-linked document ids from the results', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => ({
        results: [
          { document_id: 'd1', title: 'Manual', filename: 'manual.pdf', status: 'indexed', page: 1, snippet: '', folder_id: null },
          { document_id: 'd2', title: 'Invoice', filename: 'invoice.pdf', status: 'indexed', page: 1, snippet: '', folder_id: null },
        ],
      }),
    })
    render(<DocumentLinkPicker open onOpenChange={vi.fn()} onPick={vi.fn()} excludeIds={['d1']} />)

    fireEvent.change(screen.getByLabelText('Search documents'), { target: { value: 'doc' } })

    await screen.findByText('Invoice')
    expect(screen.queryByText('Manual')).not.toBeInTheDocument()
  })

  // The search list knows neither kind, MIME type nor date, so a pick reads
  // the document's own record (GET /api/documents/:id) - the editor's list
  // needs them to show a NOTE badge and a date before the link is saved.
  it('calls onPick with the chosen document\'s kind, MIME type and date, and closes', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/api/documents/d1')) {
        return Promise.resolve({ ok: true, json: async () => ({ id: 'd1', kind: 'note', mime: 'text/markdown', created_at: '2026-01-12T09:30:00Z' }) })
      }
      return Promise.resolve({
        ok: true,
        json: async () => ({
          results: [{ document_id: 'd1', title: 'Manual', filename: 'manual.md', status: 'indexed', page: 1, snippet: '', folder_id: null }],
        }),
      })
    })
    const onPick = vi.fn()
    const onOpenChange = vi.fn()
    render(<DocumentLinkPicker open onOpenChange={onOpenChange} onPick={onPick} />)

    fireEvent.change(screen.getByLabelText('Search documents'), { target: { value: 'man' } })
    fireEvent.click(await screen.findByRole('button', { name: /Manual/ }))

    await waitFor(() => expect(onPick).toHaveBeenCalledWith({
      document_id: 'd1', title: 'Manual', filename: 'manual.md', kind: 'note', mime: 'text/markdown', created_at: '2026-01-12T09:30:00Z',
    }))
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('does not pick, and says why, when the document\'s record cannot be read', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/api/documents/d1')) {
        return Promise.resolve({ ok: false, status: 404, json: async () => ({ error: 'document not found' }) })
      }
      return Promise.resolve({
        ok: true,
        json: async () => ({ results: [{ document_id: 'd1', title: 'Manual', filename: 'manual.pdf', status: 'indexed', page: 1, snippet: '', folder_id: null }] }),
      })
    })
    const onPick = vi.fn()
    const onOpenChange = vi.fn()
    render(<DocumentLinkPicker open onOpenChange={onOpenChange} onPick={onPick} />)

    fireEvent.change(screen.getByLabelText('Search documents'), { target: { value: 'man' } })
    fireEvent.click(await screen.findByRole('button', { name: /Manual/ }))

    expect(await screen.findByRole('alert')).toHaveTextContent('document not found')
    expect(onPick).not.toHaveBeenCalled()
    expect(onOpenChange).not.toHaveBeenCalledWith(false)
  })

  it('shows a message when nothing matches', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({ results: [] }) })
    render(<DocumentLinkPicker open onOpenChange={vi.fn()} onPick={vi.fn()} />)

    fireEvent.change(screen.getByLabelText('Search documents'), { target: { value: 'nothing' } })

    await waitFor(() => expect(screen.getByText(/no documents found/i)).toBeInTheDocument())
  })

  it('surfaces the search failure message', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 500, json: async () => ({ error: 'search index unavailable' }) })
    render(<DocumentLinkPicker open onOpenChange={vi.fn()} onPick={vi.fn()} />)

    fireEvent.change(screen.getByLabelText('Search documents'), { target: { value: 'x' } })

    await screen.findByText('search index unavailable')
  })

  // Picking waits on the document's record. Closing the picker (Cancel,
  // Escape) during that wait must abandon the pick, not add the document and
  // dirty the item after the operator walked away.
  it('ignores a pick whose record arrives after the picker was closed', async () => {
    let resolveRecord!: (v: unknown) => void
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/api/documents/d1')) return new Promise((r) => { resolveRecord = r })
      return Promise.resolve({
        ok: true,
        json: async () => ({ results: [{ document_id: 'd1', title: 'Manual', filename: 'manual.pdf', status: 'indexed', page: 1, snippet: '', folder_id: null }] }),
      })
    })
    const onPick = vi.fn()
    const onOpenChange = vi.fn()
    const { rerender } = render(<DocumentLinkPicker open onOpenChange={onOpenChange} onPick={onPick} />)

    fireEvent.change(screen.getByLabelText('Search documents'), { target: { value: 'man' } })
    fireEvent.click(await screen.findByRole('button', { name: /Manual/ }))

    // The operator cancels while the record is still loading.
    rerender(<DocumentLinkPicker open={false} onOpenChange={onOpenChange} onPick={onPick} />)
    await act(async () => { resolveRecord({ ok: true, json: async () => ({ kind: 'file', mime: 'application/pdf', created_at: '2026-01-12T09:30:00Z' }) }) })

    expect(onPick).not.toHaveBeenCalled()
    expect(onOpenChange).not.toHaveBeenCalledWith(false)
  })

  it('disables further picks while one is in flight', async () => {
    let resolveRecord!: (v: unknown) => void
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/api/documents/d1')) return new Promise((r) => { resolveRecord = r })
      return Promise.resolve({
        ok: true,
        json: async () => ({ results: [
          { document_id: 'd1', title: 'Manual', filename: 'manual.pdf', status: 'indexed', page: 1, snippet: '', folder_id: null },
          { document_id: 'd2', title: 'Wiring', filename: 'wiring.pdf', status: 'indexed', page: 1, snippet: '', folder_id: null },
        ] }),
      })
    })
    const onPick = vi.fn()
    render(<DocumentLinkPicker open onOpenChange={vi.fn()} onPick={onPick} />)
    fireEvent.change(screen.getByLabelText('Search documents'), { target: { value: 'x' } })
    fireEvent.click(await screen.findByRole('button', { name: /Manual/ }))

    expect(screen.getByRole('button', { name: /Wiring/ })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: /Wiring/ }))
    await act(async () => { resolveRecord({ ok: true, json: async () => ({ kind: 'file', mime: 'application/pdf', created_at: '2026-01-12T09:30:00Z' }) }) })
    await waitFor(() => expect(onPick).toHaveBeenCalledTimes(1))
    expect(onPick).toHaveBeenCalledWith(expect.objectContaining({ document_id: 'd1' }))
  })
})
