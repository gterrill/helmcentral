/**
 * The hero tile (ADR 0072): one widget per page can be promoted to a
 * full-width row above the grid. The load-bearing guarantee under test here
 * is that promoting or demoting a hero never disturbs any other widget's
 * authored position — react-grid-layout's own compaction is what would
 * otherwise shuffle things when an item's slot appears to empty out.
 */
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { DashboardBentoGrid } from '@/components/dashboard-bento-grid'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'
import { setViewportWidth } from './viewport'

const widgets: DashboardLayoutItem[] = [
  { id: 'vessel', x: 0, y: 0, w: 12, h: 2 },
  { id: 'wind', x: 0, y: 2, w: 4, h: 6 },
  { id: 'solar', x: 4, y: 2, w: 4, h: 6 },
]

function renderGrid(heroId: string | undefined, editing = false) {
  return render(
    <DashboardBentoGrid
      widgets={widgets}
      editing={editing}
      heroId={heroId}
      renderWidget={(w) => <div data-testid={`widget-${w.id}`}>{w.id}</div>}
      onRemoveWidget={() => {}}
      onDuplicateWidget={() => {}}
      onLayoutSettle={() => {}}
    />,
  )
}

describe('at helm width', () => {
  it('renders the hero widget in its own row above the grid', () => {
    setViewportWidth(1280)
    const { container } = renderGrid('wind')

    const heroRow = container.querySelector('.bento-hero-row')
    expect(heroRow).not.toBeNull()
    expect(heroRow?.querySelector('[data-testid="widget-wind"]')).not.toBeNull()

    // DOM order: the hero row precedes the grid.
    const grid = container.querySelector('.react-grid-layout')
    const position = heroRow!.compareDocumentPosition(grid!)
    expect(position & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('never renders the hero widget a second time inside the grid', () => {
    setViewportWidth(1280)
    const { getAllByTestId } = renderGrid('wind')
    expect(getAllByTestId('widget-wind')).toHaveLength(1)
  })

  it("still reserves the hero's slot in the grid so nothing else compacts into it", () => {
    setViewportWidth(1280)
    const { container } = renderGrid('wind')
    // Three widgets in, three react-grid-item boxes out, even though only two
    // of them show real content (the hero's box is an invisible placeholder).
    expect(container.querySelectorAll('.react-grid-item')).toHaveLength(widgets.length)
  })

  it('offers no edit affordances on the frozen grid slot, only on the hero row', () => {
    setViewportWidth(1280)
    const { getAllByLabelText } = renderGrid('wind', true)
    // Exactly one Remove control for the hero tile, not two.
    expect(getAllByLabelText(/Remove Wind tile/i)).toHaveLength(1)
  })

  it('demoting the hero (heroId undefined) puts the widget straight back in the grid', () => {
    setViewportWidth(1280)
    const { container, getByTestId } = renderGrid(undefined)
    expect(container.querySelector('.bento-hero-row')).toBeNull()
    expect(getByTestId('widget-wind')).toBeInTheDocument()
  })

  it("never uses a shadow for the hero's chrome (the Flat Board Rule)", () => {
    setViewportWidth(1280)
    const { container } = renderGrid('wind')
    const heroRow = container.querySelector('.bento-hero-row')
    expect(heroRow?.className ?? '').not.toMatch(/shadow/)
  })
})

describe('at phone width', () => {
  it('still renders the hero in its own row, and only once', () => {
    setViewportWidth(375)
    const { container, getAllByTestId } = renderGrid('wind')
    expect(container.querySelector('.bento-hero-row')).not.toBeNull()
    expect(getAllByTestId('widget-wind')).toHaveLength(1)
  })

  it('excludes the hero from the narrow reflowed list', () => {
    setViewportWidth(375)
    const { container } = renderGrid('wind')
    const narrowList = container.querySelector('.grid-cols-1')
    expect(narrowList?.querySelector('[data-testid="widget-wind"]')).toBeNull()
  })
})

describe('without a hero', () => {
  it('renders exactly as it did before the hero feature existed', () => {
    setViewportWidth(1280)
    const { container } = renderGrid(undefined)
    expect(container.querySelector('.bento-hero-row')).toBeNull()
    expect(screen.queryByTestId('widget-vessel')).toBeInTheDocument()
  })
})
