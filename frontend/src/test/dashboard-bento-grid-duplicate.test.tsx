/**
 * The duplicate affordance (ADR 0049). Builtin widgets are one-per-page, so
 * only the token-id kinds — gauge, gauge group, embed — get the button.
 *
 * react-grid-layout has no real DOM layout engine in jsdom, so the library is
 * mocked to render its children, matching dashboard-bento-grid-embed-layout.
 */
import type { ComponentType, ReactNode } from 'react'
import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { DashboardBentoGrid } from '@/components/dashboard-bento-grid'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'
import { setViewportWidth } from './viewport'

vi.mock('react-grid-layout/legacy', () => {
  function MockGridLayout(props: { children: ReactNode }) {
    return props.children
  }
  return {
    default: MockGridLayout,
    WidthProvider: (Component: ComponentType<unknown>) => Component,
  }
})

const widgets: DashboardLayoutItem[] = [
  { id: 'wind', x: 0, y: 0, w: 4, h: 6 },
  { id: 'gauge:m1x8abcd', x: 4, y: 0, w: 3, h: 6, gauge: { path: 'a.b', label: 'Depth', display: 'numeric', quantity: 'raw', unit: 'raw' } },
  { id: 'embed:m1x8abcd', x: 7, y: 0, w: 5, h: 8, embed: { title: 'Grafana', url: 'https://grafana.local/a' } },
  { id: 'gauge-group:m1x8abcd', x: 0, y: 6, w: 6, h: 8, gaugeGroup: { title: 'Port', gauges: [{ path: 'a.b', label: 'RPM', display: 'numeric', quantity: 'raw', unit: 'raw' }] } },
]

function renderGrid(editing: boolean, onDuplicateWidget = vi.fn()) {
  render(
    <DashboardBentoGrid
      widgets={widgets}
      editing={editing}
      renderWidget={(w) => <div>{w.id}</div>}
      onRemoveWidget={vi.fn()}
      onDuplicateWidget={onDuplicateWidget}
      onLayoutSettle={vi.fn()}
    />,
  )
  return onDuplicateWidget
}

beforeEach(() => {
  setViewportWidth(1440)
})

describe('duplicate affordance', () => {
  it('offers duplicate for every multi-instance widget', () => {
    renderGrid(true)
    expect(screen.getByLabelText('Duplicate Depth widget')).toBeInTheDocument()
    expect(screen.getByLabelText('Duplicate Grafana widget')).toBeInTheDocument()
    expect(screen.getByLabelText('Duplicate Port widget')).toBeInTheDocument()
  })

  it('offers none for a builtin, which is one per page', () => {
    renderGrid(true)
    expect(screen.queryByLabelText('Duplicate Apparent Wind widget')).not.toBeInTheDocument()
    expect(screen.getAllByLabelText(/^Duplicate /)).toHaveLength(3)
  })

  it('reports the id that was duplicated', () => {
    const onDuplicateWidget = renderGrid(true)
    screen.getByLabelText('Duplicate Port widget').click()
    expect(onDuplicateWidget).toHaveBeenCalledWith('gauge-group:m1x8abcd')
  })

  it('is layout-mode chrome, absent otherwise', () => {
    renderGrid(false)
    expect(screen.queryAllByLabelText(/^Duplicate /)).toHaveLength(0)
  })
})
