import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, within } from '@testing-library/react'
import { LayoutToolbar } from '@/components/layout-toolbar'
import type { DashboardPage } from '@/hooks/use-dashboard-pages'

// Delete page (ADR 0107 follow-up): moved here from the page switcher's old
// per-row trash icon. With the switcher popover open, clicking that icon
// used to open the delete confirmation behind the popover — two edit
// surfaces stacked on the same trigger. The toolbar is the one surface every
// other layout edit (rename, Add Widget, Skin, Hero, Kiosk) already uses;
// Delete page now matches, as the last item, set apart from the rest by a
// divider since it is the one destructive control in the row.

const page: DashboardPage = { id: 'p1', name: 'Page A', widgets: [], created_at: '', updated_at: '' }

function baseProps(overrides: Partial<React.ComponentProps<typeof LayoutToolbar>> = {}): React.ComponentProps<typeof LayoutToolbar> {
  return {
    page,
    namingPageId: null,
    onSaveName: vi.fn(),
    onDoneNaming: vi.fn(),
    placedWidgetIds: [],
    onAddWidget: vi.fn(),
    multiInstanceEntries: [],
    onOpenRibbon: vi.fn(),
    onSetSkin: vi.fn(),
    onSetHero: vi.fn(),
    onKioskPatch: vi.fn(),
    onDeletePage: vi.fn(),
    pageCount: 2,
    canWrite: true,
    ...overrides,
  }
}

describe('LayoutToolbar — page name field', () => {
  it('is an editable field for a user with write access', () => {
    render(<LayoutToolbar {...baseProps({ canWrite: true })} />)
    expect(screen.getByLabelText('Page name')).toBeInTheDocument()
  })

  // The old rename pencil was behind canWrite; PageTitleField on its own
  // isn't. Without this, a read-only user could type a new name into a
  // field that looked live and have the backend reject the PATCH.
  it('is static text, not an editable field, for a read-only user', () => {
    render(<LayoutToolbar {...baseProps({ canWrite: false })} />)
    expect(screen.queryByLabelText('Page name')).not.toBeInTheDocument()
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
    expect(screen.getByText('Page A')).toBeInTheDocument()
  })
})

describe('LayoutToolbar — Delete page', () => {
  it('is absent with a single page', () => {
    render(<LayoutToolbar {...baseProps({ pageCount: 1 })} />)
    expect(screen.queryByRole('button', { name: 'Delete page' })).not.toBeInTheDocument()
  })

  it('is present with two or more pages', () => {
    render(<LayoutToolbar {...baseProps({ pageCount: 2 })} />)
    expect(screen.getByRole('button', { name: 'Delete page' })).toBeInTheDocument()
  })

  it('is absent for a read-only user even with several pages', () => {
    render(<LayoutToolbar {...baseProps({ pageCount: 2, canWrite: false })} />)
    expect(screen.queryByRole('button', { name: 'Delete page' })).not.toBeInTheDocument()
  })

  it('clicking Delete page asks for confirmation instead of deleting immediately', () => {
    const onDeletePage = vi.fn()
    render(<LayoutToolbar {...baseProps({ onDeletePage })} />)

    fireEvent.click(screen.getByRole('button', { name: 'Delete page' }))

    expect(onDeletePage).not.toHaveBeenCalled()
    const dialog = screen.getByRole('alertdialog')
    expect(within(dialog).getByText('Delete "Page A"?')).toBeInTheDocument()
  })

  it('confirming the dialog deletes the page the toolbar is editing', () => {
    const onDeletePage = vi.fn()
    render(<LayoutToolbar {...baseProps({ onDeletePage })} />)

    fireEvent.click(screen.getByRole('button', { name: 'Delete page' }))
    const dialog = screen.getByRole('alertdialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))

    expect(onDeletePage).toHaveBeenCalledWith('p1')
  })

  it('cancelling the dialog leaves the page alone', () => {
    const onDeletePage = vi.fn()
    render(<LayoutToolbar {...baseProps({ onDeletePage })} />)

    fireEvent.click(screen.getByRole('button', { name: 'Delete page' }))
    const dialog = screen.getByRole('alertdialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))

    expect(onDeletePage).not.toHaveBeenCalled()
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })
})
