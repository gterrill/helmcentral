import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor, within, act } from '@testing-library/react'
import { DashboardPageSwitcher } from '@/components/dashboard-page-switcher'
import type { DashboardPage } from '@/hooks/use-dashboard-pages'
import { setViewportWidth } from './viewport'

describe('DashboardPageSwitcher', () => {
  const reorderProps = {
    onSelect: vi.fn(), onCreate: vi.fn(),
    onReorder: vi.fn().mockResolvedValue(true), canWrite: true, reordering: false,
  }

  it('moves pages without selecting them, disables boundaries, and retains focus after moving', async () => {
    const onSelect = vi.fn()
    const onReorder = vi.fn().mockResolvedValue(true)
    const { rerender } = render(<DashboardPageSwitcher pages={mockPages} allPages={mockPages} activePageId="p1" {...reorderProps} onReorder={onReorder} onSelect={onSelect} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Reorder pages' })) })
    expect(screen.getByRole('button', { name: 'Done' })).toHaveFocus()
    expect(screen.getByLabelText('Move Page A up')).toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByLabelText('Move Page B down')).toHaveAttribute('aria-disabled', 'true')
    fireEvent.click(screen.getByLabelText('Move Page A up'))
    expect(onReorder).not.toHaveBeenCalled()
    // Reorder mode's own list swaps out the ordinary per-row select button:
    // there's nothing named "Page A" to click while it's showing.
    expect(screen.queryByRole('button', { name: 'Page A' })).not.toBeInTheDocument()
    const move = screen.getByLabelText('Move Page B up')
    await act(async () => { move.focus(); fireEvent.click(move) })
    expect(onReorder).toHaveBeenCalledWith(['p2', 'p1'])
    expect(onSelect).not.toHaveBeenCalled()
    rerender(<DashboardPageSwitcher pages={[mockPages[1], mockPages[0]]} allPages={[mockPages[1], mockPages[0]]} activePageId="p1" {...reorderProps} onReorder={onReorder} onSelect={onSelect} />)
    expect(within(screen.getByRole('list', { name: 'Dashboard page order' })).getAllByRole('listitem')[0]).toHaveTextContent('Page B')
    expect(screen.getByLabelText('Switch dashboard page')).toHaveTextContent('Page A')
    expect(screen.getByLabelText('Move Page B up')).toHaveFocus()
    await act(async () => { fireEvent.click(screen.getByLabelText('Move Page B down')) })
    expect(onReorder).toHaveBeenLastCalledWith(['p1', 'p2'])
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Done' })) })
    expect(screen.getByRole('button', { name: 'Reorder pages' })).toHaveFocus()
    // Back to the ordinary per-row select button once reorder mode ends.
    expect(await screen.findByRole('button', { name: 'Page A' })).toBeInTheDocument()
  })

  it('disables movement while saving and shows saving status', async () => {
    const { rerender } = render(<DashboardPageSwitcher pages={mockPages} allPages={mockPages} activePageId="p1" {...reorderProps} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))
    fireEvent.click(await screen.findByRole('button', { name: 'Reorder pages' }))
    rerender(<DashboardPageSwitcher pages={mockPages} allPages={mockPages} activePageId="p1" {...reorderProps} reordering />)
    expect(screen.getByLabelText('Move Page A down')).toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByLabelText('Move Page B up')).toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByText('Saving order…')).toBeInTheDocument()
  })

  it('read-only users can select pages but cannot manage or reorder them', async () => {
    const onSelect = vi.fn()
    render(<DashboardPageSwitcher pages={mockPages} allPages={mockPages} activePageId="p1" {...reorderProps} canWrite={false} onSelect={onSelect} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))
    expect(await screen.findByRole('button', { name: 'Page B' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Reorder pages' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'New Page' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Rename Page A')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Page B' }))
    expect(onSelect).toHaveBeenCalledWith('p2')
  })

  it('keeps reorder mode open and shows a failed save without changing order', async () => {
    render(<DashboardPageSwitcher pages={mockPages} allPages={mockPages} activePageId="p1" {...reorderProps} onReorder={vi.fn().mockResolvedValue(false)} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))
    fireEvent.click(await screen.findByRole('button', { name: 'Reorder pages' }))
    fireEvent.click(screen.getByLabelText('Move Page B up'))
    expect(await screen.findByText(/Order not saved/)).toBeInTheDocument()
    expect(within(screen.getByRole('list', { name: 'Dashboard page order' })).getAllByRole('listitem')[0]).toHaveTextContent('Page A')
  })

  it('does not offer reordering for a single page', async () => {
    render(<DashboardPageSwitcher pages={[mockPages[0]]} allPages={[mockPages[0]]} activePageId="p1" {...reorderProps} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))
    await screen.findByRole('button', { name: 'New Page' })
    expect(screen.queryByRole('button', { name: 'Reorder pages' })).not.toBeInTheDocument()
  })

  const mockPages: DashboardPage[] = [
    { id: 'p1', name: 'Page A', widgets: [], created_at: '', updated_at: '' },
    { id: 'p2', name: 'Page B', widgets: [], created_at: '', updated_at: '' },
  ]

  it('trigger button text shows the active page name', () => {
    render(<DashboardPageSwitcher pages={mockPages} allPages={mockPages} activePageId="p1" {...mockFns()} />)

    const trigger = screen.getByLabelText('Switch dashboard page')
    expect(trigger).toHaveTextContent('Page A')
  })

  it('fallback to "Dashboard" when activePageId is null', () => {
    render(<DashboardPageSwitcher pages={mockPages} allPages={mockPages} activePageId={null} {...mockFns()} />)

    const trigger = screen.getByLabelText('Switch dashboard page')
    expect(trigger).toHaveTextContent('Dashboard')
  })

  it('clicking a page name calls onSelect and closes the popover', async () => {
    const onSelect = vi.fn()
    render(<DashboardPageSwitcher pages={mockPages} allPages={mockPages} activePageId="p1" {...mockFns()} onSelect={onSelect} />)

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

  // Renaming moved to the page-title-field in the layout toolbar (ADR 0107),
  // and deleting moved to the layout toolbar's own Delete page control
  // (see layout-toolbar.test.tsx): this popover no longer has a pencil, a
  // trash icon, an edit state, or a confirmation dialog at all.
  it('has no Rename control, in this list or anywhere else on the page', async () => {
    render(<DashboardPageSwitcher pages={mockPages} allPages={mockPages} activePageId="p1" {...mockFns()} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))

    await screen.findByText(/New Page/)
    expect(screen.queryByRole('button', { name: /rename/i })).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/rename/i)).not.toBeInTheDocument()
    // No inline edit state exists to fall into either — the row is a plain
    // button, never a textbox.
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
  })

  it('has no Delete control, with one page or with several', async () => {
    render(<DashboardPageSwitcher pages={mockPages} allPages={mockPages} activePageId="p1" {...mockFns()} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))

    await screen.findByText(/New Page/)
    expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/delete/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/Delete "/)).not.toBeInTheDocument()
  })

  it('"New Page" button calls onCreate', async () => {
    const onCreate = vi.fn()
    render(<DashboardPageSwitcher pages={mockPages} allPages={mockPages} activePageId="p1" {...mockFns()} onCreate={onCreate} />)

    // Open the popover
    const trigger = screen.getByLabelText('Switch dashboard page')
    fireEvent.click(trigger)

    // Find and click the "+ New Page" button
    const newPageButton = await screen.findByText(/New Page/)
    fireEvent.click(newPageButton)

    expect(onCreate).toHaveBeenCalled()
  })

  // Rename and Delete page both moved into the layout toolbar (ADR 0107),
  // and the toolbar only renders in layout mode, which itself only exists
  // at `lg` and up. A page created below that width would be stuck named
  // "Untitled page" with no toolbar reachable to fix that or delete it, so
  // New Page follows the same `canEditLayout` gate `App.tsx` uses — while
  // selecting and reordering, neither of which needs layout mode, stay
  // available at every width.
  describe('New Page availability by width', () => {
    it('hides New Page below the lg breakpoint but keeps select and reorder', async () => {
      setViewportWidth(900)
      const onSelect = vi.fn()
      render(<DashboardPageSwitcher pages={mockPages} allPages={mockPages} activePageId="p1" {...reorderProps} onSelect={onSelect} />)
      fireEvent.click(screen.getByLabelText('Switch dashboard page'))

      expect(await screen.findByRole('button', { name: 'Page B' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Reorder pages' })).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'New Page' })).not.toBeInTheDocument()

      fireEvent.click(screen.getByRole('button', { name: 'Page B' }))
      expect(onSelect).toHaveBeenCalledWith('p2')
    })

    it('shows New Page at the lg breakpoint and above', async () => {
      setViewportWidth(1024)
      render(<DashboardPageSwitcher pages={mockPages} allPages={mockPages} activePageId="p1" {...reorderProps} />)
      fireEvent.click(screen.getByLabelText('Switch dashboard page'))

      expect(await screen.findByRole('button', { name: 'New Page' })).toBeInTheDocument()
    })
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
  })

  it('leaves the skin to the layout controls', async () => {
    render(<DashboardPageSwitcher pages={mockPages} allPages={mockPages} activePageId="p1" {...mockFns()} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))

    await screen.findByText(/New Page/)
    expect(screen.queryByLabelText(/skin/i)).not.toBeInTheDocument()
  })

  // ADR 0110: a page assigned to a wall display can never appear as a row
  // here at all — not even with a glyph, as the old kiosk-flagged rows used
  // to render (that glyph moved to display-sidebar-group.tsx's
  // WallPageGlyph). This is the switcher's own defensive filter (the `pages`
  // prop doc): even a caller that forgets to filter gets the correct result.
  it('never renders a page that has a display_id, even if the caller passes one in', () => {
    const withWallPage: DashboardPage[] = [
      { id: 'p1', name: 'Wall: Engines', widgets: [], created_at: '', updated_at: '', display_id: 'd1', dwell_seconds: 30 },
      { id: 'p2', name: 'Plain Page', widgets: [], created_at: '', updated_at: '' },
      { id: 'p3', name: 'Active Page', widgets: [], created_at: '', updated_at: '' },
    ]
    render(<DashboardPageSwitcher pages={withWallPage} allPages={withWallPage} activePageId="p3" {...mockFns()} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))

    expect(screen.queryByText('Wall: Engines')).not.toBeInTheDocument()
    expect(screen.getByText('Plain Page')).toBeInTheDocument()
  })

  // Trap 1 (ADR 0110 plan §6): the reorder endpoint rejects a partial page
  // list, so moving the two *visible* pages here has to submit every page id
  // — the wall page keeps its own absolute slot untouched.
  it('reorder submits every page id, keeping a wall page in its own slot', async () => {
    const onReorder = vi.fn().mockResolvedValue(true)
    const allPages: DashboardPage[] = [
      { id: 'p1', name: 'Page A', widgets: [], created_at: '', updated_at: '' },
      { id: 'wall', name: 'Wall: Engines', widgets: [], created_at: '', updated_at: '', display_id: 'd1', dwell_seconds: 30 },
      { id: 'p2', name: 'Page B', widgets: [], created_at: '', updated_at: '' },
    ]
    render(<DashboardPageSwitcher pages={allPages} allPages={allPages} activePageId="p1" {...reorderProps} onReorder={onReorder} />)
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))
    fireEvent.click(await screen.findByRole('button', { name: 'Reorder pages' }))
    await act(async () => { fireEvent.click(screen.getByLabelText('Move Page B up')) })

    // Visible order becomes [p2, p1]; the wall page keeps its own slot
    // (index 1) rather than being pushed to the end or dropped.
    expect(onReorder).toHaveBeenCalledWith(['p2', 'wall', 'p1'])
  })

  // Trap 2 (ADR 0110 plan §6): without the override, the trigger falls back
  // to the *visible* list, so an active wall page (absent from it) reads
  // "Dashboard" — exactly the bug the prop exists to fix.
  describe('activePageName', () => {
    const withWallPage: DashboardPage[] = [
      { id: 'wall', name: 'Wall: Engines', widgets: [], created_at: '', updated_at: '', display_id: 'd1', dwell_seconds: 30 },
      { id: 'p2', name: 'Plain Page', widgets: [], created_at: '', updated_at: '' },
    ]

    it('falls back to "Dashboard" for an active wall page when omitted', () => {
      render(<DashboardPageSwitcher pages={withWallPage} allPages={withWallPage} activePageId="wall" {...mockFns()} />)
      expect(screen.getByLabelText('Switch dashboard page')).toHaveTextContent('Dashboard')
    })

    it('overrides the trigger label when supplied', () => {
      render(<DashboardPageSwitcher pages={withWallPage} allPages={withWallPage} activePageId="wall" activePageName="Wall: Engines" {...mockFns()} />)
      expect(screen.getByLabelText('Switch dashboard page')).toHaveTextContent('Wall: Engines')
    })
  })
})
