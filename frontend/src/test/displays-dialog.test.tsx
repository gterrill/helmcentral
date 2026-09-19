import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, within } from '@testing-library/react'

import { DisplaysDialog } from '@/components/displays-dialog'
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

function baseProps(overrides: Partial<React.ComponentProps<typeof DisplaysDialog>> = {}): React.ComponentProps<typeof DisplaysDialog> {
  return {
    open: true,
    onOpenChange: vi.fn(),
    displays: [makeDisplay()],
    pages: [],
    onCreate: vi.fn().mockResolvedValue(makeDisplay()),
    onUpdate: vi.fn().mockResolvedValue(makeDisplay()),
    onDelete: vi.fn().mockResolvedValue([]),
    canWrite: true,
    ...overrides,
  }
}

describe('DisplaysDialog', () => {
  it('shows an empty-state message with zero displays', () => {
    render(<DisplaysDialog {...baseProps({ displays: [] })} />)
    expect(screen.getByText(/No displays yet/)).toBeInTheDocument()
  })

  it('lists a display\'s name, slug, geometry, rotation and page count', () => {
    render(<DisplaysDialog {...baseProps({ pages: [{ display_id: 'd1' }, { display_id: 'd1' }, { display_id: 'other' }] })} />)
    expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('Flybridge')
    expect((screen.getByLabelText('Slug') as HTMLInputElement).value).toBe('flybridge')
    expect((screen.getByLabelText('Width for Flybridge') as HTMLInputElement).value).toBe('1920')
    expect((screen.getByLabelText('Height for Flybridge') as HTMLInputElement).value).toBe('360')
    expect((screen.getByLabelText('Scale for Flybridge') as HTMLInputElement).value).toBe('1')
    expect(screen.getByText('2 pages')).toBeInTheDocument()
  })

  it('labels the pixel-shift field "OLED panel" with help text on why', () => {
    render(<DisplaysDialog {...baseProps()} />)
    const label = screen.getByText('OLED panel').closest('label') as HTMLElement
    expect(label).toHaveAttribute('title', expect.stringContaining('burn'))
  })

  describe('inline edit — name', () => {
    it('commits a valid name on blur', () => {
      const onUpdate = vi.fn().mockResolvedValue(makeDisplay())
      render(<DisplaysDialog {...baseProps({ onUpdate })} />)
      const input = screen.getByLabelText('Name')
      fireEvent.change(input, { target: { value: 'Saloon' } })
      fireEvent.blur(input)
      expect(onUpdate).toHaveBeenCalledWith('d1', { name: 'Saloon' })
    })

    it('rejects an empty name: no update call, destructive border', () => {
      const onUpdate = vi.fn()
      render(<DisplaysDialog {...baseProps({ onUpdate })} />)
      const input = screen.getByLabelText('Name')
      fireEvent.change(input, { target: { value: '   ' } })
      fireEvent.blur(input)
      expect(onUpdate).not.toHaveBeenCalled()
      expect(input.className).toMatch(/destructive/)
    })

    it('commits on Enter too', () => {
      const onUpdate = vi.fn().mockResolvedValue(makeDisplay())
      render(<DisplaysDialog {...baseProps({ onUpdate })} />)
      const input = screen.getByLabelText('Name')
      fireEvent.change(input, { target: { value: 'Saloon' } })
      fireEvent.keyDown(input, { key: 'Enter' })
      expect(onUpdate).toHaveBeenCalledWith('d1', { name: 'Saloon' })
    })
  })

  describe('inline edit — slug', () => {
    it('commits a valid slug', () => {
      const onUpdate = vi.fn().mockResolvedValue(makeDisplay())
      render(<DisplaysDialog {...baseProps({ onUpdate })} />)
      const input = screen.getByLabelText('Slug')
      fireEvent.change(input, { target: { value: 'saloon-tv' } })
      fireEvent.blur(input)
      expect(onUpdate).toHaveBeenCalledWith('d1', { slug: 'saloon-tv' })
    })

    it('rejects a slug with uppercase or spaces: no update call, destructive border', () => {
      const onUpdate = vi.fn()
      render(<DisplaysDialog {...baseProps({ onUpdate })} />)
      const input = screen.getByLabelText('Slug')
      fireEvent.change(input, { target: { value: 'Saloon TV' } })
      fireEvent.blur(input)
      expect(onUpdate).not.toHaveBeenCalled()
      expect(input.className).toMatch(/destructive/)
    })
  })

  describe('inline edit — geometry', () => {
    it('commits width and height together when both are in range', () => {
      const onUpdate = vi.fn().mockResolvedValue(makeDisplay())
      render(<DisplaysDialog {...baseProps({ onUpdate })} />)
      fireEvent.change(screen.getByLabelText('Width for Flybridge'), { target: { value: '1280' } })
      fireEvent.blur(screen.getByLabelText('Width for Flybridge'))
      expect(onUpdate).toHaveBeenCalledWith('d1', { width: 1280, height: 360 })
    })

    it('accepts both zero (full viewport)', () => {
      const onUpdate = vi.fn().mockResolvedValue(makeDisplay())
      render(<DisplaysDialog {...baseProps({ onUpdate })} />)
      fireEvent.change(screen.getByLabelText('Width for Flybridge'), { target: { value: '0' } })
      fireEvent.change(screen.getByLabelText('Height for Flybridge'), { target: { value: '0' } })
      fireEvent.blur(screen.getByLabelText('Height for Flybridge'))
      expect(onUpdate).toHaveBeenCalledWith('d1', { width: 0, height: 0 })
    })

    it('rejects a single zero: no update call, destructive border on both fields', () => {
      const onUpdate = vi.fn()
      render(<DisplaysDialog {...baseProps({ onUpdate })} />)
      fireEvent.change(screen.getByLabelText('Width for Flybridge'), { target: { value: '0' } })
      fireEvent.blur(screen.getByLabelText('Width for Flybridge'))
      expect(onUpdate).not.toHaveBeenCalled()
      expect(screen.getByLabelText('Width for Flybridge').className).toMatch(/destructive/)
    })

    it('rejects a width below the server minimum', () => {
      const onUpdate = vi.fn()
      render(<DisplaysDialog {...baseProps({ onUpdate })} />)
      fireEvent.change(screen.getByLabelText('Width for Flybridge'), { target: { value: '100' } })
      fireEvent.blur(screen.getByLabelText('Width for Flybridge'))
      expect(onUpdate).not.toHaveBeenCalled()
    })
  })

  describe('inline edit — scale', () => {
    it('commits an in-range scale', () => {
      const onUpdate = vi.fn().mockResolvedValue(makeDisplay())
      render(<DisplaysDialog {...baseProps({ onUpdate })} />)
      const input = screen.getByLabelText('Scale for Flybridge')
      fireEvent.change(input, { target: { value: '1.5' } })
      fireEvent.blur(input)
      expect(onUpdate).toHaveBeenCalledWith('d1', { scale: 1.5 })
    })

    it('rejects a non-1 scale on a zero canvas', () => {
      const onUpdate = vi.fn()
      const zeroCanvas = makeDisplay({ width: 0, height: 0 })
      render(<DisplaysDialog {...baseProps({ displays: [zeroCanvas], onUpdate })} />)
      const input = screen.getByLabelText('Scale for Flybridge')
      fireEvent.change(input, { target: { value: '1.5' } })
      fireEvent.blur(input)
      expect(onUpdate).not.toHaveBeenCalled()
      expect(input.className).toMatch(/destructive/)
    })
  })

  it('changing rotation saves immediately', () => {
    const onUpdate = vi.fn().mockResolvedValue(makeDisplay())
    render(<DisplaysDialog {...baseProps({ onUpdate })} />)
    fireEvent.change(screen.getByLabelText('Rotation'), { target: { value: '0' } })
    expect(onUpdate).toHaveBeenCalledWith('d1', { rotate: 0 })
  })

  it('toggling OLED and keep-awake saves immediately', () => {
    const onUpdate = vi.fn().mockResolvedValue(makeDisplay())
    render(<DisplaysDialog {...baseProps({ onUpdate })} />)
    fireEvent.click(screen.getByLabelText('OLED panel'))
    expect(onUpdate).toHaveBeenCalledWith('d1', { pixel_shift: true })
    fireEvent.click(screen.getByLabelText('Keep awake'))
    expect(onUpdate).toHaveBeenCalledWith('d1', { wake_lock: true })
  })

  describe('New display', () => {
    it('is disabled until a name is entered', () => {
      render(<DisplaysDialog {...baseProps()} />)
      expect(screen.getByRole('button', { name: 'Add' })).toBeDisabled()
    })

    it('creates with 1920x1080, scale 1, rotate 0 and no explicit slug', () => {
      const onCreate = vi.fn().mockResolvedValue(makeDisplay({ id: 'd2', name: 'Saloon TV' }))
      render(<DisplaysDialog {...baseProps({ onCreate })} />)
      fireEvent.change(screen.getByLabelText('New display'), { target: { value: 'Saloon TV' } })
      fireEvent.click(screen.getByRole('button', { name: 'Add' }))
      expect(onCreate).toHaveBeenCalledWith({ name: 'Saloon TV', width: 1920, height: 1080, scale: 1, rotate: 0 })
    })

    it('clears the name field once the create succeeds', async () => {
      const onCreate = vi.fn().mockResolvedValue(makeDisplay({ id: 'd2', name: 'Saloon TV' }))
      render(<DisplaysDialog {...baseProps({ onCreate })} />)
      const input = screen.getByLabelText('New display') as HTMLInputElement
      fireEvent.change(input, { target: { value: 'Saloon TV' } })
      fireEvent.click(screen.getByRole('button', { name: 'Add' }))
      await vi.waitFor(() => expect(input.value).toBe(''))
    })

    it('submits on Enter', () => {
      const onCreate = vi.fn().mockResolvedValue(makeDisplay())
      render(<DisplaysDialog {...baseProps({ onCreate })} />)
      const input = screen.getByLabelText('New display')
      fireEvent.change(input, { target: { value: 'Saloon TV' } })
      fireEvent.keyDown(input, { key: 'Enter' })
      expect(onCreate).toHaveBeenCalled()
    })
  })

  describe('Delete', () => {
    it('names the release consequence, pluralized correctly', () => {
      render(<DisplaysDialog {...baseProps({ pages: [{ display_id: 'd1' }, { display_id: 'd1' }, { display_id: 'd1' }] })} />)
      fireEvent.click(screen.getByLabelText('Delete Flybridge'))
      const dialog = screen.getByRole('alertdialog')
      expect(within(dialog).getByText('Delete Flybridge?')).toBeInTheDocument()
      expect(within(dialog).getByText(/Its 3 pages stay, and move back into the Dashboard list/)).toBeInTheDocument()
    })

    it('singularizes for exactly one page', () => {
      render(<DisplaysDialog {...baseProps({ pages: [{ display_id: 'd1' }] })} />)
      fireEvent.click(screen.getByLabelText('Delete Flybridge'))
      expect(screen.getByText(/Its 1 page stay/)).toBeInTheDocument()
    })

    it('says so plainly when the display has no pages at all', () => {
      render(<DisplaysDialog {...baseProps({ pages: [] })} />)
      fireEvent.click(screen.getByLabelText('Delete Flybridge'))
      expect(screen.getByText('Delete Flybridge? It has no pages on it.')).toBeInTheDocument()
    })

    it('confirming calls onDelete with the display id', () => {
      const onDelete = vi.fn().mockResolvedValue([])
      render(<DisplaysDialog {...baseProps({ onDelete })} />)
      fireEvent.click(screen.getByLabelText('Delete Flybridge'))
      const dialog = screen.getByRole('alertdialog')
      fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))
      expect(onDelete).toHaveBeenCalledWith('d1')
    })

    it('cancelling does not delete', () => {
      const onDelete = vi.fn()
      render(<DisplaysDialog {...baseProps({ onDelete })} />)
      fireEvent.click(screen.getByLabelText('Delete Flybridge'))
      const dialog = screen.getByRole('alertdialog')
      fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
      expect(onDelete).not.toHaveBeenCalled()
    })
  })

  describe('read-only', () => {
    it('hides New display and every Delete button', () => {
      render(<DisplaysDialog {...baseProps({ canWrite: false })} />)
      expect(screen.queryByLabelText('New display')).not.toBeInTheDocument()
      expect(screen.queryByLabelText('Delete Flybridge')).not.toBeInTheDocument()
    })

    it('disables every field', () => {
      render(<DisplaysDialog {...baseProps({ canWrite: false })} />)
      expect(screen.getByLabelText('Name')).toBeDisabled()
      expect(screen.getByLabelText('Slug')).toBeDisabled()
      expect(screen.getByLabelText('Rotation')).toBeDisabled()
      expect(screen.getByLabelText('OLED panel')).toBeDisabled()
      expect(screen.getByLabelText('Keep awake')).toBeDisabled()
    })
  })
})
