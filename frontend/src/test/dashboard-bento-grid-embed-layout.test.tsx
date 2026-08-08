/**
 * Regression test for the embed-drag silent-save-failure bug: dragging/resizing
 * ANY widget on a page that also has an `embed:` widget used to strip that
 * widget's `embed` config on the way out of `commit` (dashboard-bento-grid.tsx),
 * because react-grid-layout's LayoutItem only carries `{i,x,y,w,h}`. The backend
 * then rejects the whole PATCH (validateEmbedWidget in dashboard_pages.go) and
 * the page silently reverts to its last-saved layout on reload.
 *
 * react-grid-layout has no real DOM layout engine to drive in jsdom, so this
 * mocks the library itself: the mock renders `children` and captures the
 * `onDragStop` callback the component wires up, which the test then invokes
 * directly with fresh geometry to simulate RGL settling after a drag.
 */
import type { ComponentType, ReactNode } from 'react'
import { render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { LayoutItem } from 'react-grid-layout/legacy'

import { DashboardBentoGrid } from '@/components/dashboard-bento-grid'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'
import { setViewportWidth } from './viewport'

const capturedOnDragStop: { current: ((layout: readonly LayoutItem[]) => void) | null } = { current: null }

vi.mock('react-grid-layout/legacy', () => {
  function MockGridLayout(props: { children: ReactNode; onDragStop?: (layout: readonly LayoutItem[]) => void }) {
    capturedOnDragStop.current = props.onDragStop ?? null
    return props.children
  }
  return {
    default: MockGridLayout,
    WidthProvider: (Component: ComponentType<unknown>) => Component,
  }
})

const widgets: DashboardLayoutItem[] = [
  { id: 'wind', x: 0, y: 0, w: 4, h: 6 },
  {
    id: 'embed:m1x8abcd',
    x: 4,
    y: 0,
    w: 4,
    h: 6,
    embed: { title: 'Windrose', url: 'https://grafana.local/d-solo/a' },
  },
]

describe('dragging a widget on a page that also has an embed widget', () => {
  it('preserves the embed config while applying the settled geometry', () => {
    // The RGL path (and thus onDragStop) only mounts at `lg` and above.
    setViewportWidth(1280)
    const onLayoutSettle = vi.fn()

    render(
      <DashboardBentoGrid
        widgets={widgets}
        editing
        renderWidget={(w) => <div>{w.id}</div>}
        onRemoveWidget={() => {}}
        onLayoutSettle={onLayoutSettle}
      />,
    )

    expect(capturedOnDragStop.current).toBeInstanceOf(Function)

    // Simulate RGL settling after the `wind` tile was dragged: it reports
    // geometry for every tile on the grid, `embed` config included nowhere.
    capturedOnDragStop.current!([
      { i: 'wind', x: 2, y: 1, w: 4, h: 6 },
      { i: 'embed:m1x8abcd', x: 6, y: 2, w: 4, h: 6 },
    ])

    expect(onLayoutSettle).toHaveBeenCalledTimes(1)
    expect(onLayoutSettle).toHaveBeenCalledWith([
      { id: 'wind', x: 2, y: 1, w: 4, h: 6 },
      {
        id: 'embed:m1x8abcd',
        x: 6,
        y: 2,
        w: 4,
        h: 6,
        embed: { title: 'Windrose', url: 'https://grafana.local/d-solo/a' },
      },
    ])
  })
})
