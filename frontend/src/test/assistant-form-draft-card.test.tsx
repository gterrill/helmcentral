import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { AssistantFormDraftCard } from '@/components/assistant-form-draft-card'
import type { AssistantFormDraft } from '@/hooks/use-assistant-conversations'

// ADR 0165: the card under a Mate reply that filled in a PDF form. The form
// is a draft until the operator saves it to Documents.

const draft = (overrides: Partial<AssistantFormDraft> = {}): AssistantFormDraft => ({
  id: 'd1',
  messageId: 'm1',
  sourceDocumentId: 'doc0',
  title: 'Storm declaration 2026',
  folder: 'Insurance/2026',
  filename: 'storm (filled in).pdf',
  pageCount: 2,
  status: 'draft',
  ...overrides,
})

function respond(status: number, body: unknown) {
  return vi.fn().mockResolvedValue({ ok: status >= 200 && status < 300, status, json: async () => body })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('AssistantFormDraftCard', () => {
  it('draft: shows the title, page count, links to the PDF and the defaults for Save', () => {
    render(<AssistantFormDraftCard draft={draft()} canWrite onChange={vi.fn()} />)

    expect(screen.getByText('Not saved yet')).toBeInTheDocument()
    expect(screen.getByText('Storm declaration 2026')).toBeInTheDocument()
    expect(screen.getByText(/2 pages/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Open the form' })).toHaveAttribute('href', '/api/assistant/form-drafts/d1/content')
    expect(screen.getByRole('link', { name: 'Download' })).toHaveAttribute('href', '/api/assistant/form-drafts/d1/content?download=1')
    expect(screen.getByLabelText('Title')).toHaveValue('Storm declaration 2026')
    expect(screen.getByLabelText('Folder')).toHaveValue('Insurance/2026')
    expect(screen.getByRole('button', { name: 'Save to Documents' })).toBeEnabled()
    expect(screen.getByRole('button', { name: 'Dismiss' })).toBeEnabled()
  })

  it('Save sends the edited title and folder and hands the saved draft up', async () => {
    const fetchMock = respond(200, {
      draft: { id: 'd1', source_document_id: 'doc0', title: 'Cyclone declaration', folder: 'Insurance', filename: 'x.pdf', page_count: 2, status: 'saved' },
      document: { id: 'doc9' },
      duplicate: false,
    })
    vi.stubGlobal('fetch', fetchMock)
    const onChange = vi.fn()
    render(<AssistantFormDraftCard draft={draft()} canWrite onChange={onChange} />)

    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Cyclone declaration' } })
    fireEvent.change(screen.getByLabelText('Folder'), { target: { value: 'Insurance' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save to Documents' }))

    await waitFor(() => expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ id: 'd1', status: 'saved', documentId: 'doc9' })))
    expect(fetchMock).toHaveBeenCalledWith('/api/assistant/form-drafts/d1/save', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title: 'Cyclone declaration', folder: 'Insurance' }),
    })
  })

  it('Save is not offered without a title', () => {
    render(<AssistantFormDraftCard draft={draft()} canWrite onChange={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: '  ' } })
    expect(screen.getByRole('button', { name: 'Save to Documents' })).toBeDisabled()
  })

  it('Dismiss posts and hands the answer up', async () => {
    const fetchMock = respond(200, {
      draft: { id: 'd1', source_document_id: 'doc0', title: 'T', folder: '', filename: 'x.pdf', page_count: 1, status: 'dismissed' },
    })
    vi.stubGlobal('fetch', fetchMock)
    const onChange = vi.fn()
    render(<AssistantFormDraftCard draft={draft()} canWrite onChange={onChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }))

    await waitFor(() => expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ id: 'd1', status: 'dismissed' })))
    expect(fetchMock).toHaveBeenCalledWith('/api/assistant/form-drafts/d1/dismiss', { method: 'POST' })
  })

  it('a refused Save shows the server reason, keeps the buttons and changes nothing', async () => {
    vi.stubGlobal('fetch', respond(409, { error: 'this filled-in form was dismissed and can no longer be saved' }))
    const onChange = vi.fn()
    render(<AssistantFormDraftCard draft={draft()} canWrite onChange={onChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Save to Documents' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('this filled-in form was dismissed and can no longer be saved')
    expect(onChange).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Save to Documents' })).toBeEnabled()
  })

  it('a network failure on Save is shown too', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('Failed to fetch')))
    render(<AssistantFormDraftCard draft={draft()} canWrite onChange={vi.fn()} />)

    fireEvent.click(screen.getByRole('button', { name: 'Save to Documents' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Failed to fetch')
  })

  it('saved: links to the document and offers no buttons', () => {
    render(<AssistantFormDraftCard draft={draft({ status: 'saved', documentId: 'doc9' })} canWrite onChange={vi.fn()} />)

    expect(screen.getByText('Saved to Documents')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'View in Documents' })).toHaveAttribute('href', expect.stringContaining('doc9'))
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Title')).not.toBeInTheDocument()
  })

  it('dismissed: shows it as dismissed with no links or buttons', () => {
    render(<AssistantFormDraftCard draft={draft({ status: 'dismissed' })} canWrite onChange={vi.fn()} />)

    expect(screen.getByText('Dismissed', { selector: 'span' })).toBeInTheDocument()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('read-only: can open the form but not save or dismiss it', () => {
    render(<AssistantFormDraftCard draft={draft()} canWrite={false} onChange={vi.fn()} />)

    expect(screen.getByRole('link', { name: 'Open the form' })).toBeInTheDocument()
    expect(screen.getByText(/needs write access/)).toBeInTheDocument()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
})
