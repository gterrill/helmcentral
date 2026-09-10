import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor, within, act } from '@testing-library/react'
import { DashboardPageSwitcher } from '@/components/dashboard-page-switcher'
import type { DashboardPage } from '@/hooks/use-dashboard-pages'

describe('DashboardPageSwitcher', () => {
  const reorderProps = {
    onSelect: vi.fn(), onCreate: vi.fn(), onRename: vi.fn(), onDelete: vi.fn(),
    onReorder: vi.fn().mockResolvedValue(true), canWrite: true, reordering: false,
  }

  it('moves pages without selecting them, disables boundaries, and retains focus after moving', async () => {
    const onSelect = vi.fn()
    const onReorder = vi.fn().mockResolvedValue(true)
    const { rerender } = render(<DashboardPageSwitcher pages={mockPages} activePageId="p1" {...reorderProps} onReorder={onReorder} onSelect={onSelect} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Reorder pages' })) })
    expect(screen.getByRole('button', { name: 'Done' })).toHaveFocus()
    expect(screen.getByLabelText('Move Page A up')).toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByLabelText('Move Page B down')).toHaveAttribute('aria-disabled', 'true')
    fireEvent.click(screen.getByLabelText('Move Page A up'))
    expect(onReorder).not.toHaveBeenCalled()
    expect(screen.queryByLabelText('Rename Page A')).not.toBeInTheDocument()
    const move = screen.getByLabelText('Move Page B up')
    await act(async () => { move.focus(); fireEvent.click(move) })
    expect(onReorder).toHaveBeenCalledWith(['p2', 'p1'])
    expect(onSelect).not.toHaveBeenCalled()
    rerender(<DashboardPageSwitcher pages={[mockPages[1], mockPages[0]]} activePageId="p1" {...reorderProps} onReorder={onReorder} onSelect={onSelect} />)
    expect(within(screen.getByRole('list', { name: 'Dashboard page order' })).getAllByRole('listitem')[0]).toHaveTextContent('Page B')
    expect(screen.getByLabelText('Switch dashboard page')).toHaveTextContent('Page A')
    expect(screen.getByLabelText('Move Page B up')).toHaveFocus()
    await act(async () => { fireEvent.click(screen.getByLabelText('Move Page B down')) })
    expect(onReorder).toHaveBeenLastCalledWith(['p1', 'p2'])
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Done' })) })
    expect(screen.getByRole('button', { name: 'Reorder pages' })).toHaveFocus()
    expect(await screen.findByLabelText('Rename Page A')).toBeInTheDocument()
  })

  it('disables movement while saving and shows saving status', async () => {
    const { rerender } = render(<DashboardPageSwitcher pages={mockPages} activePageId="p1" {...reorderProps} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))
    fireEvent.click(await screen.findByRole('button', { name: 'Reorder pages' }))
    rerender(<DashboardPageSwitcher pages={mockPages} activePageId="p1" {...reorderProps} reordering />)
    expect(screen.getByLabelText('Move Page A down')).toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByLabelText('Move Page B up')).toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByText('Saving order…')).toBeInTheDocument()
  })

  it('read-only users can select pages but cannot manage or reorder them', async () => {
    const onSelect = vi.fn()
    render(<DashboardPageSwitcher pages={mockPages} activePageId="p1" {...reorderProps} canWrite={false} onSelect={onSelect} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))
    expect(await screen.findByRole('button', { name: 'Page B' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Reorder pages' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'New Page' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Rename Page A')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Delete Page A')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Page B' }))
    expect(onSelect).toHaveBeenCalledWith('p2')
  })

  it('keeps reorder mode open and shows a failed save without changing order', async () => {
    render(<DashboardPageSwitcher pages={mockPages} activePageId="p1" {...reorderProps} onReorder={vi.fn().mockResolvedValue(false)} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))
    fireEvent.click(await screen.findByRole('button', { name: 'Reorder pages' }))
    fireEvent.click(screen.getByLabelText('Move Page B up'))
    expect(await screen.findByText(/Order not saved/)).toBeInTheDocument()
    expect(within(screen.getByRole('list', { name: 'Dashboard page order' })).getAllByRole('listitem')[0]).toHaveTextContent('Page A')
  })

  it('does not offer reordering for a single page', async () => {
    render(<DashboardPageSwitcher pages={[mockPages[0]]} activePageId="p1" {...reorderProps} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))
    await screen.findByRole('button', { name: 'New Page' })
    expect(screen.queryByRole('button', { name: 'Reorder pages' })).not.toBeInTheDocument()
  })

  const mockPages: DashboardPage[] = [
    { id: 'p1', name: 'Page A', widgets: [], created_at: '', updated_at: '' },
    { id: 'p2', name: 'Page B', widgets: [], created_at: '', updated_at: '' },
  ]

  it('trigger button text shows the active page name', () => {
    const mockFns = {
      onSelect: vi.fn(),
      onCreate: vi.fn(),
      onRename: vi.fn(),
      onDelete: vi.fn(),
      onSetSkin: vi.fn(),
    }

    render(
      <DashboardPageSwitcher
        pages={mockPages}
        activePageId="p1"
        {...mockFns}
      />
    )

    const trigger = screen.getByLabelText('Switch dashboard page')
    expect(trigger).toHaveTextContent('Page A')
  })

  it('fallback to "Dashboard" when activePageId is null', () => {
    const mockFns = {
      onSelect: vi.fn(),
      onCreate: vi.fn(),
      onRename: vi.fn(),
      onDelete: vi.fn(),
      onSetSkin: vi.fn(),
    }

    render(
      <DashboardPageSwitcher
        pages={mockPages}
        activePageId={null}
        {...mockFns}
      />
    )

    const trigger = screen.getByLabelText('Switch dashboard page')
    expect(trigger).toHaveTextContent('Dashboard')
  })

  it('clicking a page name calls onSelect and closes the popover', async () => {
    const onSelect = vi.fn()
    const mockFns = {
      onSelect,
      onCreate: vi.fn(),
      onRename: vi.fn(),
      onDelete: vi.fn(),
      onSetSkin: vi.fn(),
    }

    render(
      <DashboardPageSwitcher
        pages={mockPages}
        activePageId="p1"
        {...mockFns}
      />
    )

    // Open the popover
    const trigger = screen.getByLabelText('Switch dashboard page')
    fireEvent.click(trigger)

    // Wait for the content to appear in the DOM and find Page B
    await waitFor(() => {
      const allPageTexts = screen.queryAllByText(/Page [AB]/)
      // Should have multiple occurrences of page names when popover is open
      expect(allPageTexts.length).toBeGreaterThan(1)
    })

    // Find the page content (not in trigger)
    const pageButtons = screen.getAllByRole('button')
    const pageBButton = pageButtons.find(btn => btn.textContent?.includes('Page B') && btn !== trigger)

    expect(pageBButton).toBeTruthy()
    fireEvent.click(pageBButton!)

    expect(onSelect).toHaveBeenCalledWith('p2')

    // Popover should close, so multiple instances of page names should no longer exist
    await waitFor(() => {
      const allPageTexts = screen.queryAllByText(/Page [AB]/)
      // Only the trigger button should have page text now
      expect(allPageTexts).toHaveLength(1)
    })
  })

  it('with only 1 page, no delete button is rendered', () => {
    const singlePageMock: DashboardPage[] = [
      { id: 'p1', name: 'Page A', widgets: [], created_at: '', updated_at: '' },
    ]

    const mockFns = {
      onSelect: vi.fn(),
      onCreate: vi.fn(),
      onRename: vi.fn(),
      onDelete: vi.fn(),
      onSetSkin: vi.fn(),
    }

    render(
      <DashboardPageSwitcher
        pages={singlePageMock}
        activePageId="p1"
        {...mockFns}
      />
    )

    // Open the popover
    const trigger = screen.getByLabelText('Switch dashboard page')
    fireEvent.click(trigger)

    // The trash icon should not be queryable
    const trashButtons = screen.queryAllByRole('button', { name: /trash|delete/i })
    expect(trashButtons).toHaveLength(0)
  })

  it('clicking the trash icon asks for confirmation instead of deleting immediately', async () => {
    const onDelete = vi.fn()
    const mockFns = {
      onSelect: vi.fn(),
      onCreate: vi.fn(),
      onRename: vi.fn(),
      onDelete,
      onSetSkin: vi.fn(),
    }

    render(<DashboardPageSwitcher pages={mockPages} activePageId="p1" {...mockFns} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))

    const deleteButton = await screen.findByLabelText('Delete Page A')
    fireEvent.click(deleteButton)

    expect(onDelete).not.toHaveBeenCalled()
    expect(await screen.findByText('Delete "Page A"?')).toBeInTheDocument()
  })

  it('confirming the dialog deletes the named page', async () => {
    const onDelete = vi.fn()
    const mockFns = {
      onSelect: vi.fn(),
      onCreate: vi.fn(),
      onRename: vi.fn(),
      onDelete,
      onSetSkin: vi.fn(),
    }

    render(<DashboardPageSwitcher pages={mockPages} activePageId="p1" {...mockFns} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))
    fireEvent.click(await screen.findByLabelText('Delete Page A'))

    await screen.findByText('Delete "Page A"?')
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))

    expect(onDelete).toHaveBeenCalledWith('p1')
  })

  it('cancelling the dialog leaves the page alone', async () => {
    const onDelete = vi.fn()
    const mockFns = {
      onSelect: vi.fn(),
      onCreate: vi.fn(),
      onRename: vi.fn(),
      onDelete,
      onSetSkin: vi.fn(),
    }

    render(<DashboardPageSwitcher pages={mockPages} activePageId="p1" {...mockFns} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))
    fireEvent.click(await screen.findByLabelText('Delete Page A'))

    await screen.findByText('Delete "Page A"?')
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(onDelete).not.toHaveBeenCalled()
    await waitFor(() => {
      expect(screen.queryByText('Delete "Page A"?')).not.toBeInTheDocument()
    })
  })

  it('rename flow: click pencil, type new name, press Enter', async () => {
    const onRename = vi.fn()
    const mockFns = {
      onSelect: vi.fn(),
      onCreate: vi.fn(),
      onRename,
      onDelete: vi.fn(),
      onSetSkin: vi.fn(),
    }

    render(
      <DashboardPageSwitcher
        pages={mockPages}
        activePageId="p1"
        {...mockFns}
      />
    )

    // Open the popover
    const trigger = screen.getByLabelText('Switch dashboard page')
    fireEvent.click(trigger)

    // Wait for popover content to be in the DOM
    await waitFor(() => {
      const allPageTexts = screen.queryAllByText(/Page [AB]/)
      expect(allPageTexts.length).toBeGreaterThan(1)
    })

    // Find pencil buttons
    const allButtons = screen.getAllByRole('button')

    // Find the pencil button for Page A (it should be after "Page A" text in the same div)
    let pageAPencilButton: HTMLElement | null = null
    for (const btn of allButtons) {
      const label = btn.getAttribute('aria-label')
      if (label?.includes('Rename') && label?.includes('Page A')) {
        pageAPencilButton = btn
        break
      }
    }

    if (!pageAPencilButton) {
      throw new Error('Could not find pencil button for Page A')
    }

    // Click the pencil button
    fireEvent.click(pageAPencilButton)

    // An input field should appear for editing
    const input = await screen.findByRole('textbox') as HTMLInputElement
    expect(input.value).toBe('Page A')

    // Clear and type new name
    fireEvent.change(input, { target: { value: 'Renamed' } })
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' })

    await waitFor(() => {
      expect(onRename).toHaveBeenCalledWith('p1', 'Renamed')
    })
  })

  it('clearing the name to empty and pressing Enter does not submit, and stays editable', async () => {
    // Regression test: the rename input selects all text on focus (so typing
    // immediately replaces it), which means a single Backspace - a completely
    // natural way to "remove characters" - deletes the whole name in one
    // keystroke. Confirming that with Enter must not silently close the
    // field back to the unchanged name with no feedback; it should stay open
    // so the empty box makes the no-op obvious.
    const onRename = vi.fn()
    const mockFns = {
      onSelect: vi.fn(),
      onCreate: vi.fn(),
      onRename,
      onDelete: vi.fn(),
      onSetSkin: vi.fn(),
    }

    render(
      <DashboardPageSwitcher
        pages={mockPages}
        activePageId="p1"
        {...mockFns}
      />
    )

    fireEvent.click(screen.getByLabelText('Switch dashboard page'))

    const renameBtn = await screen.findByLabelText('Rename Page A')
    fireEvent.click(renameBtn)

    const input = (await screen.findByRole('textbox')) as HTMLInputElement
    expect(input.value).toBe('Page A')

    // Simulate select-all-on-focus followed by one Backspace wiping everything.
    fireEvent.change(input, { target: { value: '' } })
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' })

    expect(onRename).not.toHaveBeenCalled()
    // The input must still be present and editable - not silently reverted
    // to the read-only button showing "Page A" again.
    expect(screen.getByRole('textbox')).toBeInTheDocument()
  })

  it('"New Page" button calls onCreate', async () => {
    const onCreate = vi.fn()
    const mockFns = {
      onSelect: vi.fn(),
      onCreate,
      onRename: vi.fn(),
      onDelete: vi.fn(),
      onSetSkin: vi.fn(),
    }

    render(
      <DashboardPageSwitcher
        pages={mockPages}
        activePageId="p1"
        {...mockFns}
      />
    )

    // Open the popover
    const trigger = screen.getByLabelText('Switch dashboard page')
    fireEvent.click(trigger)

    // Find and click the "+ New Page" button
    const newPageButton = await screen.findByText(/New Page/)
    fireEvent.click(newPageButton)

    expect(onCreate).toHaveBeenCalled()
  })

  /**
   * ADR 0060 put the skin here because the popover was the only place a
   * page-level property lived. It is a layout decision, so it now sits with the
   * other layout controls next to Add Widget, and keeping a second copy here
   * would be two controls for one setting.
   */
  const mockFns = () => ({
    onSelect: vi.fn(),
    onCreate: vi.fn(),
    onRename: vi.fn(),
    onDelete: vi.fn(),
  })

  it('leaves the skin to the layout controls', async () => {
    render(<DashboardPageSwitcher pages={mockPages} activePageId="p1" {...mockFns()} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))

    await screen.findByText(/New Page/)
    expect(screen.queryByLabelText(/skin/i)).not.toBeInTheDocument()
  })

  // ADR 0089: a page flagged for the wall display stays in this same list -
  // there is no separate kiosk page list - so it gets a small glyph instead.
  // The trigger button also renders the active page's own name (hidden below
  // `sm`, but present in the DOM), so the active page in each of these is a
  // third, differently-named page - otherwise getByText would match both the
  // trigger and the row it's meant to isolate.
  it('marks a kiosk-flagged row with its duration, and an unflagged row with no glyph', () => {
    const flagged: DashboardPage[] = [
      { id: 'p1', name: 'Kiosk Page', widgets: [], created_at: '', updated_at: '', kiosk: true, kiosk_seconds: 30 },
      { id: 'p2', name: 'Plain Page', widgets: [], created_at: '', updated_at: '' },
      { id: 'p3', name: 'Active Page', widgets: [], created_at: '', updated_at: '' },
    ]
    render(<DashboardPageSwitcher pages={flagged} activePageId="p3" {...mockFns()} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))

    const rowA = screen.getByText('Kiosk Page').closest('button') as HTMLElement
    const rowB = screen.getByText('Plain Page').closest('button') as HTMLElement
    expect(within(rowA).getByText('30s')).toBeInTheDocument()
    expect(within(rowB).queryByText(/\ds$/)).not.toBeInTheDocument()
  })

  it('adds an anchor glyph for a page whose kiosk condition is "anchored"', () => {
    const pages: DashboardPage[] = [
      { id: 'p1', name: 'Wall: Anchor', widgets: [], created_at: '', updated_at: '', kiosk: true, kiosk_seconds: 30, kiosk_when: 'anchored' },
      { id: 'p2', name: 'Active Page', widgets: [], created_at: '', updated_at: '' },
    ]
    render(<DashboardPageSwitcher pages={pages} activePageId="p2" {...mockFns()} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))

    const row = screen.getByText('Wall: Anchor').closest('button') as HTMLElement
    expect(within(row).getByTitle(/while anchored/)).toBeInTheDocument()
  })
})
