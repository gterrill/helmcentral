import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'

import { NoteMarkdown } from '@/components/note-markdown'

// ADR 0116, plan §5/§6: a note's own renderer, trusted with exactly one
// image src shape (`hc-doc:<uuid>`, rewritten to a same-origin content URL)
// and nothing else. NoteMarkdown is a React.lazy wrapper (kiosk bundle-split,
// commit 70acb53's pattern) so, as with help-markdown.test.tsx, the first
// content-bearing assertion in each test awaits the lazy chunk with
// findBy*/findAllBy* before anything else on the same tree.

const UUID = '0f3b1c2e-8a4d-4f21-9c33-1d2e3f4a5b6c'

describe('NoteMarkdown image handling', () => {
  it('renders an <img>, rewritten to the content URL, for a valid hc-doc: reference', async () => {
    const { container } = render(
      <NoteMarkdown content={`![Fuel manifold](hc-doc:${UUID})`} onNavigate={vi.fn()} />,
    )

    const img = await screen.findByRole('img', { name: 'Fuel manifold' })
    expect(img).toHaveAttribute('src', `/api/documents/${UUID}/content`)
    expect(container.querySelectorAll('img')).toHaveLength(1)
  })

  it('renders no <img> at all for an ordinary https: image URL - alt text only', async () => {
    const { container } = render(
      <NoteMarkdown content="![alt text](https://evil.example/p.gif)" onNavigate={vi.fn()} />,
    )

    await screen.findByText('alt text')
    expect(container.querySelector('img')).toBeNull()
  })

  it('renders no <img> at all for a data: URL - alt text only', async () => {
    const { container } = render(
      <NoteMarkdown content="![alt text](data:image/png;base64,AAAA)" onNavigate={vi.fn()} />,
    )

    await screen.findByText('alt text')
    expect(container.querySelector('img')).toBeNull()
  })

  it('renders no <img> for hc-doc: with an uppercase-hex (invalid) uuid', async () => {
    const { container } = render(
      <NoteMarkdown content={`![alt text](hc-doc:${UUID.toUpperCase()})`} onNavigate={vi.fn()} />,
    )

    await screen.findByText('alt text')
    expect(container.querySelector('img')).toBeNull()
  })
})

describe('NoteMarkdown heading ids', () => {
  it('gives an h1 (# in the markdown) the GitHub slug id', async () => {
    render(<NoteMarkdown content="# Genset start-up" onNavigate={vi.fn()} />)

    const heading = await screen.findByText('Genset start-up')
    expect(heading).toHaveAttribute('id', 'genset-start-up')
  })

  it('gives an h3 (### in the markdown) the GitHub slug id too', async () => {
    render(<NoteMarkdown content="### Warm start" onNavigate={vi.fn()} />)

    const heading = await screen.findByText('Warm start')
    expect(heading).toHaveAttribute('id', 'warm-start')
  })
})

describe('NoteMarkdown links', () => {
  it('prevents the default navigation and calls onNavigate with {kind: "note"} for an hc-note: link', async () => {
    const onNavigate = vi.fn()
    render(
      <NoteMarkdown content={`[Warm start](hc-note:${UUID})`} onNavigate={onNavigate} />,
    )

    const link = await screen.findByRole('link', { name: 'Warm start' })
    const notCancelled = fireClickReturningWhetherDefaultRan(link)

    expect(notCancelled).toBe(false)
    expect(onNavigate).toHaveBeenCalledWith({ kind: 'note', id: UUID })
  })

  it('treats a #hash link as an anchor and calls onNavigate with the anchor kind', async () => {
    const onNavigate = vi.fn()
    render(<NoteMarkdown content="[Warm start](#warm-start)" onNavigate={onNavigate} />)

    const link = await screen.findByRole('link', { name: 'Warm start' })
    fireClickReturningWhetherDefaultRan(link)

    expect(onNavigate).toHaveBeenCalledWith({ kind: 'anchor', hash: 'warm-start' })
  })

  it('keeps target=_blank and rel=noreferrer, and does not call onNavigate, for an external link', async () => {
    const onNavigate = vi.fn()
    render(<NoteMarkdown content="[wazero](https://wazero.io/)" onNavigate={onNavigate} />)

    const link = await screen.findByRole('link', { name: 'wazero' })
    expect(link).toHaveAttribute('href', 'https://wazero.io/')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noreferrer')
    expect(onNavigate).not.toHaveBeenCalled()
  })

  it('renders an unsafe link (javascript:) as plain text, never an anchor, and never calls onNavigate', async () => {
    const onNavigate = vi.fn()
    const { container } = render(
      <NoteMarkdown content="[click me](javascript:alert(1))" onNavigate={onNavigate} />,
    )

    await screen.findByText('click me')
    expect(container.querySelector('a')).toBeNull()
    expect(onNavigate).not.toHaveBeenCalled()
  })

  function fireClickReturningWhetherDefaultRan(element: HTMLElement): boolean {
    const event = new MouseEvent('click', { bubbles: true, cancelable: true })
    return element.dispatchEvent(event)
  }
})
