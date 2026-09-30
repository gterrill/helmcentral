/**
 * A list tile at the bottom of its column is drawn as tall as its content
 * needs, never taller than the saved height, and only outside edit mode. The
 * saved layout is never what shrinks: a drag, resize or keyboard move commits
 * the operator's own height.
 */
import { fireEvent, render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { DashboardBentoGrid, gridPixelHeight } from '@/components/dashboard-bento-grid'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'
import { useReportTileHeight } from '@/lib/tile-content-height'
import { setViewportWidth } from './viewport'

function ListTile({ id, needRows }: { id: string; needRows: number }) {
  useReportTileHeight(gridPixelHeight(needRows))
  return <div data-testid={`widget-${id}`}>{id}</div>
}

const widgets: DashboardLayoutItem[] = [
  { id: 'wind', x: 0, y: 0, w: 4, h: 6 },
  { id: 'nearby-vessels', x: 4, y: 0, w: 4, h: 9 },
  { id: 'solar', x: 0, y: 6, w: 4, h: 4 },
]

function renderGrid(opts: { editing?: boolean; widgets?: DashboardLayoutItem[]; onLayoutSettle?: () => void } = {}) {
  return render(
    <DashboardBentoGrid
      widgets={opts.widgets ?? widgets}
      editing={opts.editing ?? false}
      // Every tile reports a 3-row need; which of them may act on it is the grid's call.
      renderWidget={(w) => <ListTile id={w.id} needRows={3} />}
      onRemoveWidget={() => {}}
      onDuplicateWidget={() => {}}
      onLayoutSettle={opts.onLayoutSettle ?? (() => {})}
    />,
  )
}

const itemHeight = (container: HTMLElement, id: string) => {
  const el = container.querySelector<HTMLElement>(`[data-testid="widget-${id}"]`)!.closest<HTMLElement>('.react-grid-item')!
  return el.style.height
}

describe('at helm width', () => {
  it('shrinks a bottom tile to its content and leaves the others alone', () => {
    setViewportWidth(1280)
    const { container } = renderGrid()
    // nearby-vessels has nothing under it: 9 rows down to the 3 it needs.
    expect(itemHeight(container, 'nearby-vessels')).toBe(`${gridPixelHeight(3)}px`)
    // solar is bottom of its column too, and needs 3 of its 4 rows.
    expect(itemHeight(container, 'solar')).toBe(`${gridPixelHeight(3)}px`)
    // wind has solar beneath it, so it keeps all six.
    expect(itemHeight(container, 'wind')).toBe(`${gridPixelHeight(6)}px`)
  })

  it('never shrinks while editing', () => {
    setViewportWidth(1280)
    const { container } = renderGrid({ editing: true })
    expect(itemHeight(container, 'nearby-vessels')).toBe(`${gridPixelHeight(9)}px`)
    expect(itemHeight(container, 'solar')).toBe(`${gridPixelHeight(4)}px`)
  })

  it('commits the saved height, not the shrunk one, from an edit-mode move', () => {
    setViewportWidth(1280)
    const onLayoutSettle = vi.fn()
    const { getByLabelText } = renderGrid({ editing: true, onLayoutSettle })
    fireEvent.keyDown(getByLabelText(/Drag handle for the Nearby Vessels tile/i), { key: 'ArrowRight' })
    const [next] = onLayoutSettle.mock.calls[0] as [DashboardLayoutItem[]]
    expect(next.find((w) => w.id === 'nearby-vessels')).toMatchObject({ x: 5, h: 9 })
    expect(next.find((w) => w.id === 'solar')).toMatchObject({ h: 4 })
  })

  it('restores the full height when edit mode is entered', () => {
    setViewportWidth(1280)
    const { container, rerender } = renderGrid()
    expect(itemHeight(container, 'nearby-vessels')).toBe(`${gridPixelHeight(3)}px`)
    rerender(
      <DashboardBentoGrid
        widgets={widgets}
        editing
        renderWidget={(w) => <ListTile id={w.id} needRows={3} />}
        onRemoveWidget={() => {}}
        onDuplicateWidget={() => {}}
        onLayoutSettle={() => {}}
      />,
    )
    expect(itemHeight(container, 'nearby-vessels')).toBe(`${gridPixelHeight(9)}px`)
  })
})

describe('at phone width', () => {
  it('shrinks only the last tile in stack order', () => {
    setViewportWidth(375)
    const { container } = renderGrid()
    const floor = (id: string) => container.querySelector<HTMLElement>(`[data-testid="widget-${id}"]`)!.closest<HTMLElement>('.bento-stack-cell')!.style.minHeight
    // Stack order is (y, x): wind, nearby-vessels, solar. Solar is last.
    expect(floor('wind')).toBe(`${gridPixelHeight(6)}px`)
    expect(floor('nearby-vessels')).toBe(`${gridPixelHeight(9)}px`)
    expect(floor('solar')).toBe(`${gridPixelHeight(3)}px`)
  })
})
