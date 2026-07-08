import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { DashboardPageSwitcher } from '@/components/dashboard-page-switcher'
import type { DashboardPage } from '@/hooks/use-dashboard-pages'

describe('DashboardPageSwitcher', () => {
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

  it('rename flow: click pencil, type new name, press Enter', async () => {
    const onRename = vi.fn()
    const mockFns = {
      onSelect: vi.fn(),
      onCreate: vi.fn(),
      onRename,
      onDelete: vi.fn(),
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
})
