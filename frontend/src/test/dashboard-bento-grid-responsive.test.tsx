/**
 * Phase 2: below `lg` the dashboard is a CSS grid derived from the same persisted
 * 12-column layout — nothing extra is stored (ADR 0012's constraint holds, see ADR 0032).
 */
import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { DashboardBentoGrid } from '@/components/dashboard-bento-grid'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'
import { setViewportWidth } from './viewport'

// `vessel` is full-width, `wind`/`solar` are quarter-width — the shape the default
// layout in backend/dashboard_pages.go actually produces.
const widgets: DashboardLayoutItem[] = [
  { id: 'wind', x: 4, y: 2, w: 4, h: 6 },
  { id: 'vessel', x: 0, y: 0, w: 12, h: 2 },
  { id: 'solar', x: 0, y: 2, w: 4, h: 6 },
]

function renderGrid(editing = false) {
  return render(
    <DashboardBentoGrid
      widgets={widgets}
      editing={editing}
      renderWidget={(w) => <div data-testid={`widget-${w.id}`}>{w.id}</div>}
      onRemoveWidget={() => {}}
      onLayoutSettle={() => {}}
    />,
  )
}

describe('at helm width', () => {
  it('mounts the react-grid-layout grid', () => {
    setViewportWidth(1280)
    const { container } = renderGrid()
    expect(container.querySelector('.react-grid-layout')).not.toBeNull()
  })

  it('exposes drag and remove affordances in edit mode', () => {
    setViewportWidth(1280)
    const { container, getAllByLabelText } = renderGrid(true)
    expect(container.querySelector('.bento-drag-handle')).not.toBeNull()
    expect(getAllByLabelText(/Remove .* widget/)).toHaveLength(widgets.length)
  })
})

describe('at phone width', () => {
  it('replaces the grid with a single-column CSS grid in (y, x) order', () => {
    setViewportWidth(375)
    const { container, getAllByTestId } = renderGrid()

    expect(container.querySelector('.react-grid-layout')).toBeNull()

    const ids = getAllByTestId(/^widget-/).map((el) => el.dataset.testid)
    expect(ids).toEqual(['widget-vessel', 'widget-solar', 'widget-wind'])
  })

  it('carries the persisted height through as a floor, not a fixed height', () => {
    setViewportWidth(375)
    const { getByTestId } = renderGrid()

    // h=2 → 2*32 + 1*16 = 80; h=6 → 6*32 + 5*16 = 272. A floor, so a tile whose
    // content wraps at phone width grows instead of clipping.
    const vessel = getByTestId('widget-vessel').parentElement
    expect(vessel).toHaveStyle({ minHeight: '80px' })
    expect(vessel?.style.height).toBe('')

    expect(getByTestId('widget-wind').parentElement).toHaveStyle({ minHeight: '272px' })
  })

  it('offers no edit affordances even when editing is on', () => {
    setViewportWidth(375)
    const { container, queryByLabelText } = renderGrid(true)
    expect(container.querySelector('.bento-drag-handle')).toBeNull()
    expect(queryByLabelText(/Remove .* widget/)).toBeNull()
  })
})

describe('at tablet-portrait width', () => {
  it('pairs narrow widgets two-across and spans wide ones', () => {
    setViewportWidth(800)
    const { getByTestId } = renderGrid()

    // w=12 is at least half the 12-column grid, so it stays full-bleed.
    expect(getByTestId('widget-vessel').parentElement).toHaveClass('sm:col-span-2')
    // w=4 pairs up with its neighbour.
    expect(getByTestId('widget-wind').parentElement).not.toHaveClass('sm:col-span-2')
  })
})

describe('crossing the breakpoint', () => {
  it('never mounts a widget twice', () => {
    setViewportWidth(1280)
    const { getAllByTestId, rerender } = renderGrid()
    expect(getAllByTestId('widget-wind')).toHaveLength(1)

    setViewportWidth(375)
    rerender(
      <DashboardBentoGrid
        widgets={widgets}
        editing={false}
        renderWidget={(w) => <div data-testid={`widget-${w.id}`}>{w.id}</div>}
        onRemoveWidget={() => {}}
        onLayoutSettle={() => {}}
      />,
    )

    // ADR 0012's stated invariant for using a JS media query rather than CSS
    // hiding: only one of the two layouts is ever mounted.
    expect(getAllByTestId('widget-wind')).toHaveLength(1)
  })
})
