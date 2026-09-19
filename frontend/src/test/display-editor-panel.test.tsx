import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, within } from '@testing-library/react'

import { DisplayEditorPanel, type DisplayEditorPage, type DisplayEditorPanelProps } from '@/components/display-editor-panel'
import type { Display } from '@/lib/displays'

function makeDisplay(overrides: Partial<Display> = {}): Display {
  return {
    id: 'd1',
    name: 'Flybridge',
    slug: 'flybridge',
    width: 1920,
    height: 360,
    scale: 1,
    rotate: 180,
    pixel_shift: false,
    wake_lock: false,
    created_at: '',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

function makePage(overrides: Partial<DisplayEditorPage> = {}): DisplayEditorPage {
  return { id: 'p1', name: 'Engine', display_id: 'd1', dwell_seconds: 30, show_when: 'always', ...overrides }
}

function baseProps(overrides: Partial<DisplayEditorPanelProps> = {}): DisplayEditorPanelProps {
  return {
    display: makeDisplay(),
    pages: [],
    displays: [makeDisplay()],
    onBack: vi.fn(),
    onUpdate: vi.fn().mockResolvedValue(makeDisplay()),
    onDelete: vi.fn().mockResolvedValue([]),
    onDisplayPatch: vi.fn(),
    onDuplicateToDisplay: vi.fn(),
    onOpenPage: vi.fn(),
    onCreatePage: vi.fn().mockResolvedValue({ id: 'new', name: 'Untitled page' }),
    onReorder: vi.fn().mockResolvedValue(true),
    reordering: false,
    canWrite: true,
    ...overrides,
  }
}

describe('DisplayEditorPanel', () => {
  it('shows a not-found state and a way back when display is null', () => {
    const onBack = vi.fn()
    render(<DisplayEditorPanel {...baseProps({ display: null, onBack })} />)
    expect(screen.getByText(/could not be found/i)).toBeInTheDocument()
    screen.getByRole('link', { name: 'Wall displays' }).click()
    expect(onBack).toHaveBeenCalled()
  })

  it('breadcrumb shows "Wall displays / <name>" and the first segment navigates back', () => {
    const onBack = vi.fn()
    render(<DisplayEditorPanel {...baseProps({ onBack })} />)
    expect(screen.getByText('Flybridge')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('link', { name: 'Wall displays' }))
    expect(onBack).toHaveBeenCalled()
  })

  describe('toolbar validation (harvested bounds)', () => {
    it('commits a valid name on blur and rejects an empty one', () => {
      const onUpdate = vi.fn().mockResolvedValue(makeDisplay())
      render(<DisplayEditorPanel {...baseProps({ onUpdate })} />)
      const input = screen.getByLabelText('Name')
      fireEvent.change(input, { target: { value: 'Saloon' } })
      fireEvent.blur(input)
      expect(onUpdate).toHaveBeenCalledWith('d1', { name: 'Saloon' })

      fireEvent.change(input, { target: { value: '   ' } })
      fireEvent.blur(input)
      expect(onUpdate).toHaveBeenCalledTimes(1)
      expect(input.className).toMatch(/destructive/)
    })

    it('rejects a slug with uppercase or spaces', () => {
      const onUpdate = vi.fn()
      render(<DisplayEditorPanel {...baseProps({ onUpdate })} />)
      const input = screen.getByLabelText('Address')
      fireEvent.change(input, { target: { value: 'Saloon TV' } })
      fireEvent.blur(input)
      expect(onUpdate).not.toHaveBeenCalled()
      expect(input.className).toMatch(/destructive/)
    })

    it('commits width and height together, rejects a single zero', () => {
      const onUpdate = vi.fn().mockResolvedValue(makeDisplay())
      render(<DisplayEditorPanel {...baseProps({ onUpdate })} />)
      fireEvent.change(screen.getByLabelText('Width for Flybridge'), { target: { value: '1280' } })
      fireEvent.blur(screen.getByLabelText('Width for Flybridge'))
      expect(onUpdate).toHaveBeenCalledWith('d1', { width: 1280, height: 360 })

      onUpdate.mockClear()
      fireEvent.change(screen.getByLabelText('Width for Flybridge'), { target: { value: '0' } })
      fireEvent.blur(screen.getByLabelText('Width for Flybridge'))
      expect(onUpdate).not.toHaveBeenCalled()
    })

    it('rejects a non-1 scale on a zero canvas', () => {
      const onUpdate = vi.fn()
      render(<DisplayEditorPanel {...baseProps({ display: makeDisplay({ width: 0, height: 0 }), onUpdate })} />)
      const input = screen.getByLabelText('Scale for Flybridge')
      fireEvent.change(input, { target: { value: '1.5' } })
      fireEvent.blur(input)
      expect(onUpdate).not.toHaveBeenCalled()
      expect(input.className).toMatch(/destructive/)
    })

    it('never fabricates a message: field errors surface as a destructive border, not text, leaving the server\'s own toast as the only copy shown', () => {
      const onUpdate = vi.fn()
      render(<DisplayEditorPanel {...baseProps({ onUpdate })} />)
      const input = screen.getByLabelText('Name')
      fireEvent.change(input, { target: { value: '' } })
      fireEvent.blur(input)
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    })
  })

  it('rotation options read "Upright" and "180°", and changing saves immediately', () => {
    const onUpdate = vi.fn().mockResolvedValue(makeDisplay())
    render(<DisplayEditorPanel {...baseProps({ onUpdate })} />)
    const select = screen.getByLabelText('Rotation') as HTMLSelectElement
    expect(within(select).getByText('Upright')).toBeInTheDocument()
    expect(within(select).getByText('180°')).toBeInTheDocument()
    fireEvent.change(select, { target: { value: '0' } })
    expect(onUpdate).toHaveBeenCalledWith('d1', { rotate: 0 })
  })

  it('toggling OLED and keep-awake saves immediately', () => {
    const onUpdate = vi.fn().mockResolvedValue(makeDisplay())
    render(<DisplayEditorPanel {...baseProps({ onUpdate })} />)
    fireEvent.click(screen.getByRole('switch', { name: 'OLED panel' }))
    expect(onUpdate).toHaveBeenCalledWith('d1', { pixel_shift: true })
    fireEvent.click(screen.getByRole('switch', { name: 'Keep awake' }))
    expect(onUpdate).toHaveBeenCalledWith('d1', { wake_lock: true })
  })

  it('Preview opens /display/<slug> in a new tab, safely', () => {
    render(<DisplayEditorPanel {...baseProps()} />)
    const preview = screen.getByRole('link', { name: /Preview/i })
    expect(preview).toHaveAttribute('href', '/display/flybridge')
    expect(preview).toHaveAttribute('target', '_blank')
    expect(preview).toHaveAttribute('rel', expect.stringContaining('noopener'))
  })

  describe('Delete display', () => {
    it('names what survives, pluralized correctly', () => {
      render(<DisplayEditorPanel {...baseProps({ pages: [makePage({ id: 'p1' }), makePage({ id: 'p2' })] })} />)
      fireEvent.click(screen.getByRole('button', { name: 'Delete display' }))
      const dialog = screen.getByRole('alertdialog')
      expect(within(dialog).getByText(/Its 2 pages stay, and move back into the Dashboard list/)).toBeInTheDocument()
    })

    it('confirming calls onDelete and navigates back on success', async () => {
      const onDelete = vi.fn().mockResolvedValue(['p1'])
      const onBack = vi.fn()
      render(<DisplayEditorPanel {...baseProps({ onDelete, onBack })} />)
      fireEvent.click(screen.getByRole('button', { name: 'Delete display' }))
      fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Delete' }))
      expect(onDelete).toHaveBeenCalledWith('d1')
      await vi.waitFor(() => expect(onBack).toHaveBeenCalled())
    })

    it('does not navigate back when the delete fails', async () => {
      const onDelete = vi.fn().mockResolvedValue(null)
      const onBack = vi.fn()
      render(<DisplayEditorPanel {...baseProps({ onDelete, onBack })} />)
      fireEvent.click(screen.getByRole('button', { name: 'Delete display' }))
      fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Delete' }))
      await vi.waitFor(() => expect(onDelete).toHaveBeenCalled())
      expect(onBack).not.toHaveBeenCalled()
    })

    it('cancelling does not delete', () => {
      const onDelete = vi.fn()
      render(<DisplayEditorPanel {...baseProps({ onDelete })} />)
      fireEvent.click(screen.getByRole('button', { name: 'Delete display' }))
      fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Cancel' }))
      expect(onDelete).not.toHaveBeenCalled()
    })
  })

  describe('pages table', () => {
    it('shows a teaching empty state when the display has no pages', () => {
      render(<DisplayEditorPanel {...baseProps({ pages: [] })} />)
      expect(screen.getByText(/Nothing on this screen yet/)).toBeInTheDocument()
      // Names both ways in: creating one here is the point of the change.
      expect(screen.getByText(/New page starts a blank one here/)).toBeInTheDocument()
    })

    it('Add page lists only pages not on any display, and assigns with the default dwell', () => {
      const onDisplayPatch = vi.fn()
      const pages = [
        makePage({ id: 'p1', name: 'Engine', display_id: 'd1' }),
        makePage({ id: 'p2', name: 'Weather', display_id: undefined }),
        makePage({ id: 'p3', name: 'Chart', display_id: 'other' }),
      ]
      render(<DisplayEditorPanel {...baseProps({ pages, onDisplayPatch })} />)
      const addSelect = screen.getByLabelText('Add page') as HTMLSelectElement
      expect(within(addSelect).getByText('Weather')).toBeInTheDocument()
      expect(within(addSelect).queryByText('Chart')).not.toBeInTheDocument()
      fireEvent.change(addSelect, { target: { value: 'p2' } })
      expect(onDisplayPatch).toHaveBeenCalledWith('p2', { display_id: 'd1', dwell_seconds: 30 })
    })

    it('order buttons are disabled at the ends and move within this display only', () => {
      const onReorder = vi.fn().mockResolvedValue(true)
      const pages = [
        makePage({ id: 'p1', name: 'Engine', display_id: 'd1' }),
        makePage({ id: 'p2', name: 'Weather', display_id: 'd1' }),
        makePage({ id: 'p3', name: 'Chart', display_id: undefined }),
      ]
      render(<DisplayEditorPanel {...baseProps({ pages, onReorder })} />)
      expect(screen.getByRole('button', { name: 'Move Engine up' })).toHaveAttribute('aria-disabled', 'true')
      expect(screen.getByRole('button', { name: 'Move Weather down' })).toHaveAttribute('aria-disabled', 'true')

      fireEvent.click(screen.getByRole('button', { name: 'Move Weather up' }))
      expect(onReorder).toHaveBeenCalledWith(['p2', 'p1', 'p3'])
    })

    it('links a page name to its own editor via onOpenPage', () => {
      const onOpenPage = vi.fn()
      render(<DisplayEditorPanel {...baseProps({ pages: [makePage()], onOpenPage })} />)
      fireEvent.click(screen.getByRole('button', { name: 'Engine' }))
      expect(onOpenPage).toHaveBeenCalledWith('p1')
    })

    it('commits dwell on blur, rejects an out-of-range value', () => {
      const onDisplayPatch = vi.fn()
      render(<DisplayEditorPanel {...baseProps({ pages: [makePage()], onDisplayPatch })} />)
      const dwell = screen.getByLabelText('Dwell seconds for Engine')
      fireEvent.change(dwell, { target: { value: '45' } })
      fireEvent.blur(dwell)
      expect(onDisplayPatch).toHaveBeenCalledWith('p1', { dwell_seconds: 45 })

      onDisplayPatch.mockClear()
      fireEvent.change(dwell, { target: { value: '4' } })
      fireEvent.blur(dwell)
      expect(onDisplayPatch).not.toHaveBeenCalled()
      expect(dwell.className).toMatch(/destructive/)
    })

    it('commits dwell on Enter too', () => {
      const onDisplayPatch = vi.fn()
      render(<DisplayEditorPanel {...baseProps({ pages: [makePage()], onDisplayPatch })} />)
      const dwell = screen.getByLabelText('Dwell seconds for Engine')
      fireEvent.change(dwell, { target: { value: '60' } })
      fireEvent.keyDown(dwell, { key: 'Enter' })
      expect(onDisplayPatch).toHaveBeenCalledWith('p1', { dwell_seconds: 60 })
    })

    it('changing condition patches show_when', () => {
      const onDisplayPatch = vi.fn()
      render(<DisplayEditorPanel {...baseProps({ pages: [makePage()], onDisplayPatch })} />)
      fireEvent.change(screen.getByLabelText('Condition for Engine'), { target: { value: 'anchored' } })
      expect(onDisplayPatch).toHaveBeenCalledWith('p1', { show_when: 'anchored' })
    })

    it('Duplicate lists every other display and hands off pageId/targetDisplayId', () => {
      const onDuplicateToDisplay = vi.fn()
      const displays = [makeDisplay({ id: 'd1', name: 'Flybridge' }), makeDisplay({ id: 'd2', name: 'Saloon TV' })]
      render(<DisplayEditorPanel {...baseProps({ pages: [makePage()], displays, onDuplicateToDisplay })} />)
      fireEvent.click(screen.getByRole('button', { name: /Duplicate/i }))
      fireEvent.click(screen.getByRole('menuitem', { name: 'Saloon TV' }))
      expect(onDuplicateToDisplay).toHaveBeenCalledWith('p1', 'd2')
    })

    it('Remove from this display unassigns rather than deleting the page', () => {
      const onDisplayPatch = vi.fn()
      render(<DisplayEditorPanel {...baseProps({ pages: [makePage()], onDisplayPatch })} />)
      expect(screen.queryByRole('button', { name: /^Delete$/ })).not.toBeInTheDocument()
      fireEvent.click(screen.getByRole('button', { name: 'Remove from this display' }))
      expect(onDisplayPatch).toHaveBeenCalledWith('p1', { display_id: '' })
    })
  })

  describe('read-only', () => {
    it('hides Delete display, Add page, and Remove from this display', () => {
      render(<DisplayEditorPanel {...baseProps({ pages: [makePage()], canWrite: false })} />)
      expect(screen.queryByRole('button', { name: 'Delete display' })).not.toBeInTheDocument()
      expect(screen.queryByLabelText('Add page')).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Remove from this display' })).not.toBeInTheDocument()
    })

    it('disables every toolbar field and the order buttons', () => {
      render(<DisplayEditorPanel {...baseProps({ pages: [makePage()], canWrite: false })} />)
      expect(screen.getByLabelText('Name')).toBeDisabled()
      expect(screen.getByLabelText('Address')).toBeDisabled()
      expect(screen.getByLabelText('Rotation')).toBeDisabled()
      expect(screen.getByRole('switch', { name: 'OLED panel' })).toHaveAttribute('data-disabled')
      expect(screen.getByRole('switch', { name: 'Keep awake' })).toHaveAttribute('data-disabled')
      expect(screen.getByRole('button', { name: 'Move Engine down' })).toHaveAttribute('aria-disabled', 'true')
    })
  })

  it('creates a blank page already on this display, without leaving to find one first', async () => {
    const onCreatePage = vi.fn().mockResolvedValue({ id: 'new', name: 'Untitled page' })
    render(<DisplayEditorPanel {...baseProps({ onCreatePage })} />)

    fireEvent.click(screen.getByRole('button', { name: /new page/i }))

    await vi.waitFor(() => expect(onCreatePage).toHaveBeenCalledWith('d1'))
  })

  it('marks a page with no tiles, since displayFeed skips it and it would otherwise look like the add silently failed', () => {
    render(<DisplayEditorPanel {...baseProps({
      pages: [
        makePage({ id: 'p1', name: 'Wall: Engines', widgets: [{}] }),
        makePage({ id: 'p2', name: 'Untitled page', widgets: [] }),
      ],
    })} />)

    const empty = screen.getByText('Untitled page').closest('tr') as HTMLElement
    expect(within(empty).getByText(/no tiles yet/i)).toBeInTheDocument()

    const filled = screen.getByText('Wall: Engines').closest('tr') as HTMLElement
    expect(within(filled).queryByText(/no tiles yet/i)).not.toBeInTheDocument()
  })
})
