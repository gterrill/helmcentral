import { describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'

import { WallDisplaysPanel, type WallDisplaysPanelProps } from '@/components/wall-displays-panel'
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

function baseProps(overrides: Partial<WallDisplaysPanelProps> = {}): WallDisplaysPanelProps {
  return {
    displays: [makeDisplay()],
    pages: [],
    loading: false,
    error: null,
    onRetry: vi.fn(),
    onOpenDisplay: vi.fn(),
    onCreateDisplay: vi.fn(),
    onOpenManual: vi.fn(),
    canWrite: true,
    ...overrides,
  }
}

describe('WallDisplaysPanel', () => {
  it('shows the breadcrumb root', () => {
    render(<WallDisplaysPanel {...baseProps()} />)
    expect(screen.getByText('Wall displays')).toBeInTheDocument()
  })

  describe('loading', () => {
    it('renders skeleton rows instead of a spinner or the empty state', () => {
      const { container } = render(<WallDisplaysPanel {...baseProps({ loading: true, displays: [] })} />)
      expect(container.querySelectorAll('tbody tr').length).toBeGreaterThan(0)
      expect(screen.queryByText(/A wall display is its own screen/)).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /New display/i })).not.toBeInTheDocument()
    })
  })

  describe('load error', () => {
    it('names the problem and offers Retry', () => {
      const onRetry = vi.fn()
      render(<WallDisplaysPanel {...baseProps({ error: 'Failed to load displays', displays: [], onRetry })} />)
      expect(screen.getByRole('alert')).toHaveTextContent('Failed to load displays')
      screen.getByRole('button', { name: 'Retry' }).click()
      expect(onRetry).toHaveBeenCalled()
    })
  })

  describe('empty state', () => {
    it('teaches what a wall display is and how canvas numbers are measured, and offers New display', () => {
      render(<WallDisplaysPanel {...baseProps({ displays: [] })} />)
      expect(screen.getByText(/A wall display is its own screen/)).toBeInTheDocument()
      expect(screen.getByText(/display-probe\.html/)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /New display/i })).toBeInTheDocument()
    })

    it('links to the wall-display how-to in the manual', () => {
      const onOpenManual = vi.fn()
      render(<WallDisplaysPanel {...baseProps({ displays: [], onOpenManual })} />)
      screen.getByRole('button', { name: /How to set up a wall display/i }).click()
      expect(onOpenManual).toHaveBeenCalledWith({ page: 'how-to/set-up-a-wall-display' })
    })

    it('hides New display when read-only', () => {
      render(<WallDisplaysPanel {...baseProps({ displays: [], canWrite: false })} />)
      expect(screen.queryByRole('button', { name: /New display/i })).not.toBeInTheDocument()
    })
  })

  describe('populated table', () => {
    it('lists name, address, canvas, rotation and page count', () => {
      render(<WallDisplaysPanel {...baseProps({ pages: [{ display_id: 'd1' }, { display_id: 'd1' }, { display_id: 'other' }] })} />)
      const row = screen.getByRole('row', { name: /Flybridge/ })
      expect(within(row).getByText('/display/flybridge')).toBeInTheDocument()
      expect(within(row).getByText('1920 × 360 @ 1×')).toBeInTheDocument()
      expect(within(row).getByText('180°')).toBeInTheDocument()
      expect(within(row).getByText('2')).toBeInTheDocument()
    })

    it('reads rotation 0 as "Upright"', () => {
      render(<WallDisplaysPanel {...baseProps({ displays: [makeDisplay({ rotate: 0 })] })} />)
      expect(screen.getByText('Upright')).toBeInTheDocument()
    })

    it('reads an unmeasured (0x0) canvas as "Not measured"', () => {
      render(<WallDisplaysPanel {...baseProps({ displays: [makeDisplay({ width: 0, height: 0 })] })} />)
      expect(screen.getByText('Not measured')).toBeInTheDocument()
    })

    it('renders a flag badge only when the flag is on', () => {
      const { rerender } = render(<WallDisplaysPanel {...baseProps({ displays: [makeDisplay({ pixel_shift: true })] })} />)
      expect(screen.getByText('OLED panel')).toBeInTheDocument()
      expect(screen.queryByText('Keep awake')).not.toBeInTheDocument()

      rerender(<WallDisplaysPanel {...baseProps({ displays: [makeDisplay({ pixel_shift: false, wake_lock: true })] })} />)
      expect(screen.queryByText('OLED panel')).not.toBeInTheDocument()
      expect(screen.getByText('Keep awake')).toBeInTheDocument()
    })

    it('clicking the name opens that display\'s editor', () => {
      const onOpenDisplay = vi.fn()
      render(<WallDisplaysPanel {...baseProps({ onOpenDisplay })} />)
      screen.getByRole('button', { name: /Flybridge/ }).click()
      expect(onOpenDisplay).toHaveBeenCalledWith('d1')
    })

    it('the preview action opens /display/<slug> in a new tab, safely', () => {
      render(<WallDisplaysPanel {...baseProps()} />)
      const preview = screen.getByRole('link', { name: /Preview Flybridge/i })
      expect(preview).toHaveAttribute('href', '/display/flybridge')
      expect(preview).toHaveAttribute('target', '_blank')
      expect(preview).toHaveAttribute('rel', expect.stringContaining('noopener'))
      expect(preview).toHaveAttribute('rel', expect.stringContaining('noreferrer'))
    })

    it('the New display button creates without opening a dialog', () => {
      const onCreateDisplay = vi.fn()
      render(<WallDisplaysPanel {...baseProps({ onCreateDisplay })} />)
      screen.getByRole('button', { name: /New display/i }).click()
      expect(onCreateDisplay).toHaveBeenCalled()
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })
})
