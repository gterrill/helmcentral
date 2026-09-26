import { describe, it, expect, afterEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'

import { AssistantMarkdown } from '@/components/assistant-markdown'
import { __resetDocumentCitationCacheForTests } from '@/hooks/use-document-citation'

// Mate UI cycle: document sources as icons. Mate now cites a document as
// `[Title](/documents?document=<id>)` (assistant_prompt.go's citation
// guidance) instead of the old free-text parenthetical mention - the
// markdown renderer turns that specific link shape into a small icon button
// whose tooltip/accessible name is the link's own text, matching the icon to
// the document's kind (fetched once per id, from GET /api/documents/:id).
//
// Same lazy-chunk-await idiom as assistant-markdown.test.tsx: the impl only
// loads once something actually renders through the Suspense boundary.

function jsonResponse(body: unknown, init: { ok?: boolean; status?: number } = {}) {
  return {
    ok: init.ok ?? true,
    status: init.status ?? 200,
    json: async () => body,
  }
}

describe('AssistantMarkdown document citations', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    __resetDocumentCitationCacheForTests()
  })

  it('renders a PDF citation as an icon link, not visible link text', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({
      id: 'doc-1', title: 'Yanmar 4JH Service Manual', filename: 'yanmar.pdf', mime: 'application/pdf', kind: 'file',
    })))

    render(<AssistantMarkdown content="Replace the impeller every 200 hours [Yanmar 4JH Service Manual](/documents?document=doc-1)." />)

    const link = await screen.findByRole('link', { name: 'Yanmar 4JH Service Manual' })
    expect(link).toHaveAttribute('href', '/documents?document=doc-1')
    // The citation's visible text is an icon, not the title as plain text -
    // the tooltip/accessible name carries the title instead.
    expect(link).not.toHaveTextContent('Yanmar 4JH Service Manual')
    expect(link.querySelector('svg')).not.toBeNull()
  })

  it('never opens a citation link in a new tab (in-app navigation, not external)', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({
      id: 'doc-1', title: 'Manual', filename: 'manual.pdf', mime: 'application/pdf', kind: 'file',
    })))

    render(<AssistantMarkdown content="[Manual](/documents?document=doc-1)" />)

    const link = await screen.findByRole('link', { name: 'Manual' })
    expect(link).not.toHaveAttribute('target', '_blank')
  })

  it('picks the spreadsheet icon for a spreadsheet mime', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({
      id: 'doc-2', title: 'Fuel log', filename: 'fuel.csv', mime: 'text/csv', kind: 'file',
    })))
    render(<AssistantMarkdown content="[Fuel log](/documents?document=doc-2)" />)
    const link = await screen.findByRole('link', { name: 'Fuel log' })
    expect(link.querySelector('svg.lucide-file-spreadsheet')).not.toBeNull()
  })

  it('picks the image icon for an image mime', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({
      id: 'doc-3', title: 'Data plate photo', filename: 'plate.jpg', mime: 'image/jpeg', kind: 'file',
    })))
    render(<AssistantMarkdown content="[Data plate photo](/documents?document=doc-3)" />)
    const link = await screen.findByRole('link', { name: 'Data plate photo' })
    expect(link.querySelector('svg.lucide-file-image')).not.toBeNull()
  })

  it('picks the note icon for kind=note regardless of mime', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({
      id: 'doc-4', title: 'Anchoring checklist', filename: 'anchoring.md', mime: 'text/markdown', kind: 'note',
    })))
    render(<AssistantMarkdown content="[Anchoring checklist](/documents?document=doc-4)" />)
    const link = await screen.findByRole('link', { name: 'Anchoring checklist' })
    expect(link.querySelector('svg.lucide-sticky-note')).not.toBeNull()
  })

  it('marks a deleted/unknown document id with a broken icon and "Document not found"', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ error: 'document not found' }, { ok: false, status: 404 })))

    render(<AssistantMarkdown content="[Old receipt](/documents?document=gone)" />)

    const link = await screen.findByRole('link', { name: 'Document not found' })
    expect(link).toHaveAttribute('href', '/documents?document=gone')
    expect(link.querySelector('svg')).not.toBeNull()
  })

  it('does not silently hide a broken citation - it stays visible and focusable', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({}, { ok: false, status: 404 })))
    render(<AssistantMarkdown content="[Old receipt](/documents?document=gone)" />)
    const link = await screen.findByRole('link', { name: 'Document not found' })
    link.focus()
    expect(link).toHaveFocus()
  })

  it('shares one fetch across two citations of the same document in one reply', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({
      id: 'doc-5', title: 'Engine manual', filename: 'engine.pdf', mime: 'application/pdf', kind: 'file',
    }))
    vi.stubGlobal('fetch', fetchMock)

    render(<AssistantMarkdown content="See [Engine manual](/documents?document=doc-5) and again [Engine manual](/documents?document=doc-5)." />)

    await waitFor(() => expect(screen.getAllByRole('link', { name: 'Engine manual' })).toHaveLength(2))
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('leaves an ordinary external link exactly as before (unaffected by the citation renderer)', async () => {
    render(<AssistantMarkdown content="[OpenRouter](https://openrouter.ai)" />)

    const link = await screen.findByRole('link', { name: 'OpenRouter' })
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noreferrer')
    expect(link).toHaveTextContent('OpenRouter')
  })

  it('renders an old-style plain-text mention exactly as before (no migration)', async () => {
    render(<AssistantMarkdown content="Two Furuno NMEA 2000 power taps are fitted (Operations/Equipment List, navigation and electronics)." />)

    expect(await screen.findByText(/Two Furuno NMEA 2000 power taps are fitted/)).toBeInTheDocument()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('clicking a citation navigates to the document viewer href', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({
      id: 'doc-6', title: 'Manual', filename: 'manual.pdf', mime: 'application/pdf', kind: 'file',
    })))
    render(<AssistantMarkdown content="[Manual](/documents?document=doc-6)" />)
    const link = await screen.findByRole('link', { name: 'Manual' })
    // jsdom does not implement navigation - this asserts the link is a real,
    // clickable anchor with the correct destination, not a JS-only handler.
    fireEvent.click(link)
    expect(link).toHaveAttribute('href', '/documents?document=doc-6')
  })
})
