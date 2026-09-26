import { describe, it, expect, vi } from 'vitest'
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

  it('shows a no-match state when nothing filters in', () => {
    render(<ConversationSearchOverlay open onOpenChange={vi.fn()} conversations={TWELVE_CONVERSATIONS} onSelect={vi.fn()} />)
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'zzz-no-match' } })
    expect(screen.getByText('No matching conversations')).toBeInTheDocument()
    expect(screen.queryAllByRole('option')).toHaveLength(0)
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
})
