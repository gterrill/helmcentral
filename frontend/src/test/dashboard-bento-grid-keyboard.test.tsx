/**
 * Keyboard access to the board: the drag handle had no tabIndex, role or
 * keyboard handler at all, so a power user could not move a single tile
 * without a mouse. Arrow keys now move the focused widget one grid cell and
 * commit through the same path a mouse drag uses.
 */
import { fireEvent, render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { DashboardBentoGrid } from '@/components/dashboard-bento-grid'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'
import { setViewportWidth } from './viewport'

function renderGrid(widgets: DashboardLayoutItem[], onLayoutSettle = vi.fn()) {
  const utils = render(
    <DashboardBentoGrid
      widgets={widgets}
      editing
      renderWidget={(w) => <div data-testid={`widget-${w.id}`}>{w.id}</div>}
      onRemoveWidget={() => {}}
      onDuplicateWidget={() => {}}
      onLayoutSettle={onLayoutSettle}
    />,
  )
  return { ...utils, onLayoutSettle }
}

describe('the drag handle', () => {
  it('is a focusable, meaningfully-announced control', () => {
    setViewportWidth(1280)
    const { getByLabelText } = renderGrid([{ id: 'wind', x: 0, y: 0, w: 4, h: 6 }])
    const handle = getByLabelText(/Drag handle for the Wind tile/i)
    expect(handle).toHaveAttribute('role', 'button')
    expect(handle).toHaveAttribute('tabIndex', '0')
  })

  it('shows a visible focus ring', () => {
    setViewportWidth(1280)
    const { getByLabelText } = renderGrid([{ id: 'wind', x: 0, y: 0, w: 4, h: 6 }])
    const handle = getByLabelText(/Drag handle for the Wind tile/i)
    expect(handle.className).toMatch(/focus-visible:ring/)
  })

  it('ArrowRight moves the widget one column and commits the change', () => {
    setViewportWidth(1280)
    const { getByLabelText, onLayoutSettle } = renderGrid([{ id: 'wind', x: 0, y: 0, w: 4, h: 6 }])
    const handle = getByLabelText(/Drag handle for the Wind tile/i)

    fireEvent.keyDown(handle, { key: 'ArrowRight' })

    expect(onLayoutSettle).toHaveBeenCalledTimes(1)
    const [next] = onLayoutSettle.mock.calls[0] as [DashboardLayoutItem[]]
    expect(next.find((w) => w.id === 'wind')).toMatchObject({ x: 1, y: 0, w: 4, h: 6 })
  })

  it('ArrowDown moves the widget one row', () => {
    setViewportWidth(1280)
    const { getByLabelText, onLayoutSettle } = renderGrid([{ id: 'wind', x: 0, y: 0, w: 4, h: 6 }])
    fireEvent.keyDown(getByLabelText(/Drag handle for the Wind tile/i), { key: 'ArrowDown' })

    const [next] = onLayoutSettle.mock.calls[0] as [DashboardLayoutItem[]]
    expect(next.find((w) => w.id === 'wind')).toMatchObject({ x: 0, y: 1 })
  })

  it('clamps at the left edge instead of going negative', () => {
    setViewportWidth(1280)
    const { getByLabelText, onLayoutSettle } = renderGrid([{ id: 'wind', x: 0, y: 0, w: 4, h: 6 }])
    fireEvent.keyDown(getByLabelText(/Drag handle for the Wind tile/i), { key: 'ArrowLeft' })
    expect(onLayoutSettle).not.toHaveBeenCalled()
  })

  it('clamps at the top edge instead of going negative', () => {
    setViewportWidth(1280)
    const { getByLabelText, onLayoutSettle } = renderGrid([{ id: 'wind', x: 0, y: 0, w: 4, h: 6 }])
    fireEvent.keyDown(getByLabelText(/Drag handle for the Wind tile/i), { key: 'ArrowUp' })
    expect(onLayoutSettle).not.toHaveBeenCalled()
  })

  it('clamps at the right edge of the 12-column grid', () => {
    setViewportWidth(1280)
    const { getByLabelText, onLayoutSettle } = renderGrid([{ id: 'wind', x: 8, y: 0, w: 4, h: 6 }])
    fireEvent.keyDown(getByLabelText(/Drag handle for the Wind tile/i), { key: 'ArrowRight' })
    expect(onLayoutSettle).not.toHaveBeenCalled()
  })

  it("leaves every other widget's geometry untouched by a keyboard move", () => {
    setViewportWidth(1280)
    const solar: DashboardLayoutItem = { id: 'solar', x: 4, y: 0, w: 4, h: 6 }
    const { getByLabelText, onLayoutSettle } = renderGrid([
      { id: 'wind', x: 0, y: 0, w: 4, h: 6 },
      solar,
    ])
    fireEvent.keyDown(getByLabelText(/Drag handle for the Wind tile/i), { key: 'ArrowDown' })

    const [next] = onLayoutSettle.mock.calls[0] as [DashboardLayoutItem[]]
    expect(next.find((w) => w.id === 'solar')).toEqual(solar)
  })

  it('ignores keys other than the arrows', () => {
    setViewportWidth(1280)
    const { getByLabelText, onLayoutSettle } = renderGrid([{ id: 'wind', x: 0, y: 0, w: 4, h: 6 }])
    fireEvent.keyDown(getByLabelText(/Drag handle for the Wind tile/i), { key: 'Enter' })
    expect(onLayoutSettle).not.toHaveBeenCalled()
  })
})
