/**
 * After a drag, react-grid-layout holds the dragged geometry itself. It goes
 * back to the layout it is given only when that layout changes, compared deep
 * (fast-equals). If the server answers the write with different geometry than
 * the drag left (clamped, normalised, or the original because the write was
 * refused or overtaken), the stored layout may be equal to the one the grid
 * was given before the drag, and the page list keeps it as the same object.
 * The grid must still bring the tile back to what is stored.
 *
 * The fake below behaves as RGL does in that respect: it keeps its own layout,
 * moves a tile on a drag, and resyncs to the `layout` prop only when the prop
 * changes by value.
 */
import { act, render, screen } from '@testing-library/react'
import { Children, isValidElement, useEffect, useRef, useState, type ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'

interface FakeItem { i: string; x: number; y: number; w: number; h: number }
let drag: (id: string, x: number, y: number) => void = () => {}

vi.mock('react-grid-layout/legacy', () => {
  function FakeGrid({ layout, children, onDragStop }: { layout: FakeItem[]; children: ReactNode; onDragStop: (l: FakeItem[]) => void }) {
    const [inner, setInner] = useState(layout)
    const innerRef = useRef(inner)
    innerRef.current = inner
    const prevProps = useRef(layout)
    useEffect(() => {
      if (JSON.stringify(layout) !== JSON.stringify(prevProps.current)) setInner(layout)
      prevProps.current = layout
    }, [layout])
    drag = (id, x, y) => {
      const next = innerRef.current.map((item) => (item.i === id ? { ...item, x, y } : item))
      setInner(next)
      onDragStop(next)
    }
    return (
      <div>
        {Children.map(children, (child) => {
          if (!isValidElement(child)) return child
          const id = String(child.key)
          const item = inner.find((it) => it.i === id)
          return <div data-testid={`cell-${id}`} data-x={item?.x} data-y={item?.y}>{child}</div>
        })}
      </div>
    )
  }
  return { default: FakeGrid, WidthProvider: <P,>(C: P) => C }
})

import { DashboardBentoGrid } from '@/components/dashboard-bento-grid'

const WIDGETS: DashboardLayoutItem[] = [
  { id: 'wind', x: 0, y: 0, w: 4, h: 4 },
  { id: 'depth-tide', x: 4, y: 0, w: 4, h: 4 },
]

function deferred() {
  let resolve!: () => void
  const promise = new Promise<void>((r) => { resolve = r })
  return { promise, resolve }
}

function renderGrid(onLayoutSettle: (next: DashboardLayoutItem[]) => Promise<unknown>) {
  return render(
    <DashboardBentoGrid
      widgets={WIDGETS}
      editing
      renderWidget={(w) => <span>{w.id}</span>}
      onRemoveWidget={vi.fn()}
      onDuplicateWidget={vi.fn()}
      onLayoutSettle={onLayoutSettle}
    />,
  )
}

describe('DashboardBentoGrid after a drag is answered', () => {
  it('puts the tile back where the server stored it when the answer keeps the old layout', async () => {
    const answer = deferred()
    renderGrid(() => answer.promise)

    act(() => { drag('wind', 8, 0) })
    expect(screen.getByTestId('cell-wind').dataset.x).toBe('8')

    // The server answered with the page as it was; the page list keeps the
    // same widgets, so the grid's `widgets` prop does not change.
    await act(async () => { answer.resolve(); await answer.promise })

    expect(screen.getByTestId('cell-wind').dataset.x).toBe('0')
  })

  it('keeps the tile where it was dropped when the answer stores the drop', async () => {
    const answer = deferred()
    const { rerender } = renderGrid(() => answer.promise)

    act(() => { drag('wind', 8, 0) })
    const stored = WIDGETS.map((w) => (w.id === 'wind' ? { ...w, x: 8 } : w))
    await act(async () => {
      rerender(
        <DashboardBentoGrid widgets={stored} editing renderWidget={(w) => <span>{w.id}</span>} onRemoveWidget={vi.fn()} onDuplicateWidget={vi.fn()} onLayoutSettle={() => answer.promise} />,
      )
      answer.resolve()
      await answer.promise
    })

    expect(screen.getByTestId('cell-wind').dataset.x).toBe('8')
  })
})
