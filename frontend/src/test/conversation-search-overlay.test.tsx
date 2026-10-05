import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, fireEvent, within } from '@testing-library/react'

import { ConversationSearchOverlay } from '@/components/conversation-search-overlay'
import type { AssistantConversation } from '@/hooks/use-assistant-conversations'

// Mate UI cycle: search the Mate sheet's conversations. A command-palette
// style overlay (Dialog + Input + a filtered, keyboard-navigable list) - the
// sheet's own version of the /mate page's inline "Search conversations" box,
// for when the sheet's own header has no room for a whole list column.

function conversation(overrides: Partial<AssistantConversation> = {}): AssistantConversation {
  return { id: 'c1', title: 'Untitled', createdAt: '', updatedAt: '', ...overrides }
}

const TWELVE_CONVERSATIONS = Array.from({ length: 12 }, (_, i) => conversation({ id: `c${i}`, title: `Conversation ${i}` }))

describe('ConversationSearchOverlay', () => {
  it('is not in the document while closed', () => {
    render(<ConversationSearchOverlay open={false} onOpenChange={vi.fn()} conversations={TWELVE_CONVERSATIONS} onSelect={vi.fn()} />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('shows the 8 most recent conversations when the query is empty', () => {
    render(<ConversationSearchOverlay open onOpenChange={vi.fn()} conversations={TWELVE_CONVERSATIONS} onSelect={vi.fn()} />)

    const options = screen.getAllByRole('option')
    expect(options).toHaveLength(8)
    expect(options[0]).toHaveTextContent('Conversation 0')
    expect(options[7]).toHaveTextContent('Conversation 7')
  })

  it('filters by title as the operator types', () => {
    const list = [
      conversation({ id: 'a', title: 'Gloucester Island Anchorages' }),
      conversation({ id: 'b', title: 'Hamilton Island Weather' }),
    ]
    render(<ConversationSearchOverlay open onOpenChange={vi.fn()} conversations={list} onSelect={vi.fn()} />)

    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'gloucester' } })

    expect(screen.getByText('Gloucester Island Anchorages')).toBeInTheDocument()
    expect(screen.queryByText('Hamilton Island Weather')).not.toBeInTheDocument()
  })

  it('shows a no-match state when nothing filters in', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, json: async () => ({ results: [] }) })))
    render(<ConversationSearchOverlay open onOpenChange={vi.fn()} conversations={TWELVE_CONVERSATIONS} onSelect={vi.fn()} />)
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'zzz-no-match' } })
    expect(await screen.findByText('No matching conversations')).toBeInTheDocument()
    expect(screen.queryAllByRole('option')).toHaveLength(0)
    vi.unstubAllGlobals()
  })

  it('clicking a row selects it and closes the overlay', () => {
    const onSelect = vi.fn()
    const onOpenChange = vi.fn()
    render(<ConversationSearchOverlay open onOpenChange={onOpenChange} conversations={TWELVE_CONVERSATIONS} onSelect={onSelect} />)

    fireEvent.click(screen.getByText('Conversation 3'))

    expect(onSelect).toHaveBeenCalledWith('c3')
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('ArrowDown/ArrowUp move the highlighted row, Enter selects it', () => {
    const onSelect = vi.fn()
    render(<ConversationSearchOverlay open onOpenChange={vi.fn()} conversations={TWELVE_CONVERSATIONS} onSelect={onSelect} />)

    const input = screen.getByRole('textbox')
    // Row 0 is highlighted by default.
    expect(screen.getAllByRole('option')[0]).toHaveAttribute('aria-selected', 'true')

    fireEvent.keyDown(input, { key: 'ArrowDown' })
    fireEvent.keyDown(input, { key: 'ArrowDown' })
    expect(screen.getAllByRole('option')[2]).toHaveAttribute('aria-selected', 'true')

    fireEvent.keyDown(input, { key: 'ArrowUp' })
    expect(screen.getAllByRole('option')[1]).toHaveAttribute('aria-selected', 'true')

    fireEvent.keyDown(input, { key: 'Enter' })
    expect(onSelect).toHaveBeenCalledWith('c1')
  })

  it('does not move the highlight above the first or past the last row', () => {
    const list = [conversation({ id: 'a', title: 'Only one' })]
    render(<ConversationSearchOverlay open onOpenChange={vi.fn()} conversations={list} onSelect={vi.fn()} />)
    const input = screen.getByRole('textbox')

    fireEvent.keyDown(input, { key: 'ArrowUp' })
    expect(screen.getAllByRole('option')[0]).toHaveAttribute('aria-selected', 'true')

    fireEvent.keyDown(input, { key: 'ArrowDown' })
    expect(screen.getAllByRole('option')[0]).toHaveAttribute('aria-selected', 'true')
  })

  it('resets the query and the highlight each time it reopens', () => {
    const { rerender } = render(
      <ConversationSearchOverlay open onOpenChange={vi.fn()} conversations={TWELVE_CONVERSATIONS} onSelect={vi.fn()} />,
    )
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Conversation 5' } })
    expect(screen.getByDisplayValue('Conversation 5')).toBeInTheDocument()

    rerender(<ConversationSearchOverlay open={false} onOpenChange={vi.fn()} conversations={TWELVE_CONVERSATIONS} onSelect={vi.fn()} />)
    rerender(<ConversationSearchOverlay open onOpenChange={vi.fn()} conversations={TWELVE_CONVERSATIONS} onSelect={vi.fn()} />)

    expect(screen.getByRole('textbox')).toHaveValue('')
    expect(screen.getAllByRole('option')[0]).toHaveAttribute('aria-selected', 'true')
  })

  it('focuses the search input on open', () => {
    render(<ConversationSearchOverlay open onOpenChange={vi.fn()} conversations={TWELVE_CONVERSATIONS} onSelect={vi.fn()} />)
    expect(screen.getByRole('textbox')).toHaveFocus()
  })

  it('has an accessible dialog title even though nothing but the search box is visible', () => {
    render(<ConversationSearchOverlay open onOpenChange={vi.fn()} conversations={TWELVE_CONVERSATIONS} onSelect={vi.fn()} />)
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('Search conversations')).toBeInTheDocument()
  })

  it('shows no delete buttons unless onDelete is given', () => {
    render(<ConversationSearchOverlay open onOpenChange={vi.fn()} conversations={[conversation({ id: 'a', title: 'Alpha' })]} onSelect={vi.fn()} />)
    expect(screen.queryByRole('button', { name: 'Delete Alpha' })).not.toBeInTheDocument()
  })

  it('delete calls onDelete with the id, and does not select the row', () => {
    const onSelect = vi.fn()
    const onDelete = vi.fn()
    render(
      <ConversationSearchOverlay
        open
        onOpenChange={vi.fn()}
        conversations={[conversation({ id: 'a', title: 'Alpha' }), conversation({ id: 'b', title: 'Bravo' })]}
        onSelect={onSelect}
        onDelete={onDelete}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Delete Bravo' }))

    expect(onDelete).toHaveBeenCalledWith('b')
    expect(onSelect).not.toHaveBeenCalled()
    expect(screen.getAllByRole('option')).toHaveLength(2)
  })
})


describe('ConversationSearchOverlay server search', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('shows conversations whose messages match, with the snippet, and makes no request for an empty query', async () => {
    const fetchMock = vi.fn(async () => ({
      ok: true,
      json: async () => ({ results: [{ id: 'z', title: 'Fuel polisher', updated_at: '2026-09-01T00:00:00Z', snippet: 'no drain on the starboard Racor' }] }),
    }))
    vi.stubGlobal('fetch', fetchMock)
    const list = [conversation({ id: 'a', title: 'Gloucester Island' })]
    render(<ConversationSearchOverlay open onOpenChange={vi.fn()} conversations={list} onSelect={vi.fn()} />)
    expect(fetchMock).not.toHaveBeenCalled()

    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Starboard' } })

    expect(await screen.findByText(/no drain on the starboard Racor/)).toBeInTheDocument()
    expect(screen.getByText('Fuel polisher')).toBeInTheDocument()
    expect(screen.queryByText('Gloucester Island')).not.toBeInTheDocument()
  })

  it('does not duplicate a conversation that matches by title and by message', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({ results: [{ id: 'a', title: 'Racor bowl', updated_at: '2026-09-01T00:00:00Z', snippet: 'racor drain' }] }),
    })))
    render(<ConversationSearchOverlay open onOpenChange={vi.fn()} conversations={[conversation({ id: 'a', title: 'Racor bowl' })]} onSelect={vi.fn()} />)
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'racor' } })
    await screen.findByText('racor drain')
    expect(screen.getAllByRole('option')).toHaveLength(1)
  })

  it('says so when the message search fails, and keeps title matches', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 500, json: async () => ({}) })))
    render(<ConversationSearchOverlay open onOpenChange={vi.fn()} conversations={[conversation({ id: 'a', title: 'Racor bowl' })]} onSelect={vi.fn()} />)
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'racor' } })
    expect(await screen.findByRole('alert')).toHaveTextContent('Message search failed')
    expect(screen.getByText('Racor bowl')).toBeInTheDocument()
  })
})
