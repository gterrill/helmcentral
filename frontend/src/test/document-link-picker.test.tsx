import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
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

  it('calls onPick with the chosen document and closes', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => ({
        results: [{ document_id: 'd1', title: 'Manual', filename: 'manual.pdf', status: 'indexed', page: 1, snippet: '', folder_id: null }],
      }),
    })
    const onPick = vi.fn()
    const onOpenChange = vi.fn()
    render(<DocumentLinkPicker open onOpenChange={onOpenChange} onPick={onPick} />)

    fireEvent.change(screen.getByLabelText('Search documents'), { target: { value: 'man' } })
    fireEvent.click(await screen.findByRole('button', { name: /Manual/ }))

    expect(onPick).toHaveBeenCalledWith({ document_id: 'd1', title: 'Manual', filename: 'manual.pdf' })
    expect(onOpenChange).toHaveBeenCalledWith(false)
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
})
