/**
 * The layout chrome no longer offers copy buttons. Duplicate is a modal action
 * on the tile itself, so the grid stays focused on arrangement and removal.
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
  it('keeps duplicate off the layout chrome entirely', () => {
    renderGrid(true)
    expect(screen.queryByLabelText(/Duplicate/i)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /duplicate/i })).not.toBeInTheDocument()
  })

  it('is absent outside layout mode as well', () => {
    renderGrid(false)
    expect(screen.queryByLabelText(/Duplicate/i)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /duplicate/i })).not.toBeInTheDocument()
  })
})
