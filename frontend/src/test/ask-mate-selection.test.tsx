import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'

import { AskMateSelection, createSelectionAnchor, DEFAULT_ASK_MATE_QUESTION } from '@/components/ask-mate-selection'

// Selecting text with fireEvent.mouseUp/touchend never actually moves a real
// caret under happy-dom (there's no layout engine to drag across), so tests
// build the Selection directly - selectNodeContents + addRange is what a
// real drag-select leaves behind - and then fire the same `selectionchange`
// event a real selection change dispatches, which is what the component
// itself listens for.
function selectContentsOf(el: Element) {
  const range = document.createRange()
  range.selectNodeContents(el)
  const selection = window.getSelection()
  selection?.removeAllRanges()
  selection?.addRange(range)
  fireEvent(document, new Event('selectionchange'))
}

function collapseSelection() {
  window.getSelection()?.removeAllRanges()
  fireEvent(document, new Event('selectionchange'))
}

describe('AskMateSelection', () => {
  it('shows the popover when selecting text in the note body', () => {
    render(
      <AskMateSelection noteId="n1" noteTitle="Fuel system" mateAvailable onAskMate={vi.fn()}>
        <p>Some passage about the fuel system.</p>
      </AskMateSelection>,
    )
    selectContentsOf(screen.getByText('Some passage about the fuel system.'))
    expect(screen.getByPlaceholderText('Ask Mate about this…')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Ask Mate' })).toBeInTheDocument()
  })

  it('sends the default question with the quote, title and id, as a new conversation, on an empty send', () => {
    const onAskMate = vi.fn()
    render(
      <AskMateSelection noteId="abc-123" noteTitle="Fuel system" mateAvailable onAskMate={onAskMate}>
        <p>Some passage about the fuel system.</p>
      </AskMateSelection>,
    )
    selectContentsOf(screen.getByText('Some passage about the fuel system.'))
    fireEvent.click(screen.getByRole('button', { name: 'Ask Mate' }))

    expect(onAskMate).toHaveBeenCalledTimes(1)
    const [message, options] = onAskMate.mock.calls[0]
    expect(message).toContain('> Some passage about the fuel system.')
    expect(message).toContain('Fuel system')
    expect(message).toContain('abc-123')
    expect(message).toContain(DEFAULT_ASK_MATE_QUESTION)
    expect(options).toEqual({ newConversation: true })
  })

  it('uses a typed question instead of the default', () => {
    const onAskMate = vi.fn()
    render(
      <AskMateSelection noteId="n1" noteTitle="Fuel system" mateAvailable onAskMate={onAskMate}>
        <p>Some passage about the fuel system.</p>
      </AskMateSelection>,
    )
    selectContentsOf(screen.getByText('Some passage about the fuel system.'))
    fireEvent.change(screen.getByPlaceholderText('Ask Mate about this…'), {
      target: { value: 'What does BOM stand for?' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Ask Mate' }))

    const [message] = onAskMate.mock.calls[0]
    expect(message).toContain('What does BOM stand for?')
    expect(message).not.toContain(DEFAULT_ASK_MATE_QUESTION)
  })

  it('sends on Enter in the input', () => {
    const onAskMate = vi.fn()
    render(
      <AskMateSelection noteId="n1" noteTitle="Fuel system" mateAvailable onAskMate={onAskMate}>
        <p>Some passage about the fuel system.</p>
      </AskMateSelection>,
    )
    selectContentsOf(screen.getByText('Some passage about the fuel system.'))
    const input = screen.getByPlaceholderText('Ask Mate about this…')
    fireEvent.change(input, { target: { value: 'What is this?' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    expect(onAskMate).toHaveBeenCalledTimes(1)
    expect(onAskMate.mock.calls[0][0]).toContain('What is this?')
  })

  it('shows nothing for a selection outside the note body', () => {
    render(
      <div>
        <p>Outside the note entirely.</p>
        <AskMateSelection noteId="n1" noteTitle="Fuel system" mateAvailable onAskMate={vi.fn()}>
          <p>Inside the note body.</p>
        </AskMateSelection>
      </div>,
    )
    selectContentsOf(screen.getByText('Outside the note entirely.'))
    expect(screen.queryByPlaceholderText('Ask Mate about this…')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Ask Mate' })).not.toBeInTheDocument()
  })

  it('shows nothing for a whitespace-only selection', () => {
    render(
      <AskMateSelection noteId="n1" noteTitle="Fuel system" mateAvailable onAskMate={vi.fn()}>
        <p>{'   '}</p>
      </AskMateSelection>,
    )
    selectContentsOf(screen.getByText((_, element) => element?.tagName === 'P'))
    expect(screen.queryByRole('button', { name: 'Ask Mate' })).not.toBeInTheDocument()
  })

  it('shows nothing when Mate is unavailable', () => {
    render(
      <AskMateSelection noteId="n1" noteTitle="Fuel system" mateAvailable={false} onAskMate={vi.fn()}>
        <p>Some passage about the fuel system.</p>
      </AskMateSelection>,
    )
    selectContentsOf(screen.getByText('Some passage about the fuel system.'))
    expect(screen.queryByRole('button', { name: 'Ask Mate' })).not.toBeInTheDocument()
  })

  it('truncates a long selection and marks the truncation visibly', () => {
    const longText = 'x'.repeat(2000)
    const onAskMate = vi.fn()
    render(
      <AskMateSelection noteId="n1" noteTitle="Fuel system" mateAvailable onAskMate={onAskMate}>
        <p>{longText}</p>
      </AskMateSelection>,
    )
    selectContentsOf(screen.getByText(longText))
    fireEvent.click(screen.getByRole('button', { name: 'Ask Mate' }))

    const [message] = onAskMate.mock.calls[0]
    expect(message).toContain('x'.repeat(1500))
    expect(message).not.toContain('x'.repeat(1501))
    expect(message.toLowerCase()).toContain('truncated')
  })

  it('dismisses the popover on Escape', () => {
    render(
      <AskMateSelection noteId="n1" noteTitle="Fuel system" mateAvailable onAskMate={vi.fn()}>
        <p>Some passage about the fuel system.</p>
      </AskMateSelection>,
    )
    selectContentsOf(screen.getByText('Some passage about the fuel system.'))
    expect(screen.getByRole('button', { name: 'Ask Mate' })).toBeInTheDocument()

    fireEvent.keyDown(screen.getByPlaceholderText('Ask Mate about this…'), { key: 'Escape' })
    expect(screen.queryByRole('button', { name: 'Ask Mate' })).not.toBeInTheDocument()
  })

  it('dismisses when the selection collapses', () => {
    render(
      <AskMateSelection noteId="n1" noteTitle="Fuel system" mateAvailable onAskMate={vi.fn()}>
        <p>Some passage about the fuel system.</p>
      </AskMateSelection>,
    )
    selectContentsOf(screen.getByText('Some passage about the fuel system.'))
    expect(screen.getByRole('button', { name: 'Ask Mate' })).toBeInTheDocument()

    collapseSelection()
    expect(screen.queryByRole('button', { name: 'Ask Mate' })).not.toBeInTheDocument()
  })

  // Bug: focusing the input (autoFocus, for a mouse-originated selection)
  // moves the browser's own document Selection into a collapsed state in
  // some browsers (Chromium among them) - the same `selectionchange` that
  // fires for a genuine "the operator clicked away" collapse also fires
  // here, so without an exception the popover used to dismiss itself the
  // instant it opened, before anyone could type into it.
  it('keeps the popover open when focusing its input collapses the document selection', () => {
    const onAskMate = vi.fn()
    render(
      <AskMateSelection noteId="n1" noteTitle="Fuel system" mateAvailable onAskMate={onAskMate}>
        <p>Some passage about the fuel system.</p>
      </AskMateSelection>,
    )
    selectContentsOf(screen.getByText('Some passage about the fuel system.'))
    const input = screen.getByPlaceholderText('Ask Mate about this…')

    // Exactly what autoFocus already does on mount for a mouse selection,
    // made explicit here so the test doesn't depend on jsdom/happy-dom's
    // own autofocus timing.
    input.focus()
    window.getSelection()?.removeAllRanges()
    fireEvent(document, new Event('selectionchange'))

    expect(screen.getByPlaceholderText('Ask Mate about this…')).toBeInTheDocument()

    fireEvent.change(input, { target: { value: 'What does BOM stand for?' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    expect(onAskMate).toHaveBeenCalledTimes(1)
    expect(onAskMate.mock.calls[0][0]).toContain('What does BOM stand for?')
  })

  // Touch variant of the same bug: on a touchscreen the pointerdown on the
  // input (and the selection collapse it causes) can land before focus
  // itself does, so the guard has to key off the pointerdown location too,
  // not only document.activeElement.
  it('keeps the popover open when a pointerdown on it collapses the selection before focus lands', () => {
    const onAskMate = vi.fn()
    render(
      <AskMateSelection noteId="n1" noteTitle="Fuel system" mateAvailable onAskMate={onAskMate}>
        <p>Some passage about the fuel system.</p>
      </AskMateSelection>,
    )
    selectContentsOf(screen.getByText('Some passage about the fuel system.'))
    const input = screen.getByPlaceholderText('Ask Mate about this…')

    fireEvent.pointerDown(input, { pointerType: 'touch' })
    window.getSelection()?.removeAllRanges()
    fireEvent(document, new Event('selectionchange'))

    expect(screen.getByPlaceholderText('Ask Mate about this…')).toBeInTheDocument()
  })

  // A real outside pointerdown must still dismiss even while the popup-
  // interaction guard above exists - proves the guard doesn't swallow every
  // dismissal, only the ones caused by the popup itself.
  it('still dismisses on a genuine outside pointerdown', () => {
    render(
      <div>
        <button type="button">Elsewhere</button>
        <AskMateSelection noteId="n1" noteTitle="Fuel system" mateAvailable onAskMate={vi.fn()}>
          <p>Some passage about the fuel system.</p>
        </AskMateSelection>
      </div>,
    )
    selectContentsOf(screen.getByText('Some passage about the fuel system.'))
    expect(screen.getByRole('button', { name: 'Ask Mate' })).toBeInTheDocument()

    fireEvent.pointerDown(screen.getByRole('button', { name: 'Elsewhere' }))
    expect(screen.queryByRole('button', { name: 'Ask Mate' })).not.toBeInTheDocument()
  })
})

describe('createSelectionAnchor', () => {
  function connectedFakeNode(): Node {
    const node = document.createElement('span')
    document.body.appendChild(node)
    return node
  }

  // Bug: the popover used to anchor to a one-time rect snapshot taken at
  // selection time, so it drifted the moment the reading pane scrolled or
  // resized. createSelectionAnchor instead wraps the Range itself, so
  // floating-ui's own repeated getBoundingClientRect() calls (it re-measures
  // on scroll/resize) read the range's CURRENT geometry every time.
  it("reflects an updated range rect after a simulated scroll", () => {
    let rect = new DOMRect(0, 100, 50, 20)
    const range = {
      startContainer: connectedFakeNode(),
      endContainer: connectedFakeNode(),
      getBoundingClientRect: () => rect,
    }
    const onStale = vi.fn()
    const anchor = createSelectionAnchor(range, onStale)

    expect(anchor.getBoundingClientRect()).toEqual(rect)

    // A scroll doesn't move the Range; it moves the viewport the Range's
    // rect is reported relative to - the browser recomputes the same
    // live Range against the new scroll position.
    rect = new DOMRect(0, 40, 50, 20)
    expect(anchor.getBoundingClientRect()).toEqual(rect)
    expect(onStale).not.toHaveBeenCalled()
  })

  it('reports staleness once the range detaches from the document, and returns a zero rect rather than a stale one', () => {
    const detachedNode = document.createElement('span') // never appended
    const range = {
      startContainer: detachedNode,
      endContainer: detachedNode,
      getBoundingClientRect: () => new DOMRect(0, 100, 50, 20),
    }
    const onStale = vi.fn()
    const anchor = createSelectionAnchor(range, onStale)

    const result = anchor.getBoundingClientRect()
    expect(onStale).toHaveBeenCalledTimes(1)
    expect(result.width).toBe(0)
    expect(result.height).toBe(0)
  })

  it('reports staleness once a previously real rect collapses to zero size (the note re-rendered)', () => {
    let rect = new DOMRect(0, 100, 50, 20)
    const range = {
      startContainer: connectedFakeNode(),
      endContainer: connectedFakeNode(),
      getBoundingClientRect: () => rect,
    }
    const onStale = vi.fn()
    const anchor = createSelectionAnchor(range, onStale)

    anchor.getBoundingClientRect()
    expect(onStale).not.toHaveBeenCalled()

    rect = new DOMRect(0, 0, 0, 0)
    anchor.getBoundingClientRect()
    expect(onStale).toHaveBeenCalledTimes(1)
  })

  // happy-dom (like jsdom) never lays out real text, so every genuine
  // Range.getBoundingClientRect() in this whole suite reports zero size
  // from the very first call - if that alone counted as "stale", every
  // other test in this file would self-dismiss the moment its popover
  // opened. Only a rect that HAD size and then lost it counts.
  it('does not report staleness for a rect that is zero-size from the very first read', () => {
    const range = {
      startContainer: connectedFakeNode(),
      endContainer: connectedFakeNode(),
      getBoundingClientRect: () => new DOMRect(0, 0, 0, 0),
    }
    const onStale = vi.fn()
    const anchor = createSelectionAnchor(range, onStale)

    anchor.getBoundingClientRect()
    anchor.getBoundingClientRect()
    expect(onStale).not.toHaveBeenCalled()
  })
})
