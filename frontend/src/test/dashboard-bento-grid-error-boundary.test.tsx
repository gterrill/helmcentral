/**
 * A widget that throws during render must not take the rest of the grid
 * down with it. React 19 unmounts the whole root on an uncaught render
 * error, which on the unattended wall-display kiosk (ADR 0089) would blank
 * every page of the feed until someone reloads it — dashboard-bento-grid.tsx
 * wraps every renderWidget() call site in a TileErrorBoundary (keyed by
 * widget id) precisely so one bad tile degrades to its own failed-tile card
 * instead.
 */
import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { DashboardBentoGrid } from '@/components/dashboard-bento-grid'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'
import { setViewportWidth } from './viewport'

const widgets: DashboardLayoutItem[] = [
  { id: 'vessel', x: 0, y: 0, w: 4, h: 2 },
  { id: 'wind', x: 4, y: 0, w: 4, h: 6 },
  { id: 'solar', x: 8, y: 0, w: 4, h: 6 },
]

// renderWidget itself (App.tsx's real implementation) just returns a React
// element referencing the widget's component — it does not execute that
// component's function body. The throw has to happen the same way, deferred
// to the point React actually renders that one element, or it would blow up
// during DashboardBentoGrid's own render instead of being caught by the
// TileErrorBoundary wrapped around it.
function Boom({ widgetId }: { widgetId: string }): never {
  throw new Error(`${widgetId} blew up`)
}

function renderWidgetThatThrowsFor(brokenId: string) {
  return (w: DashboardLayoutItem) => {
    if (w.id === brokenId) {
      return <Boom widgetId={w.id} />
    }
    return <div data-testid={`widget-${w.id}`}>{w.id}</div>
  }
}

function renderGrid(brokenId: string, heroId?: string) {
  return render(
    <DashboardBentoGrid
      widgets={widgets}
      editing={false}
      heroId={heroId}
      renderWidget={renderWidgetThatThrowsFor(brokenId)}
      onRemoveWidget={() => {}}
      onDuplicateWidget={() => {}}
      onLayoutSettle={() => {}}
    />,
  )
}

describe('DashboardBentoGrid tile error isolation', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('at helm width, shows the failed-tile card for the broken widget and still renders its siblings', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    setViewportWidth(1280)

    renderGrid('wind')

    expect(screen.getByTestId('tile-error-fallback')).toBeInTheDocument()
    expect(screen.getByText('Apparent Wind')).toBeInTheDocument()
    expect(screen.getByTestId('widget-vessel')).toBeInTheDocument()
    expect(screen.getByTestId('widget-solar')).toBeInTheDocument()
  })

  it('below lg (the CSS grid fallback), also isolates the failure to the one tile', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    setViewportWidth(500)

    renderGrid('wind')

    expect(screen.getByTestId('tile-error-fallback')).toBeInTheDocument()
    expect(screen.getByTestId('widget-vessel')).toBeInTheDocument()
    expect(screen.getByTestId('widget-solar')).toBeInTheDocument()
  })

  it('isolates a throwing hero widget from the rest of the grid too', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    setViewportWidth(1280)

    renderGrid('wind', 'wind')

    expect(screen.getByTestId('tile-error-fallback')).toBeInTheDocument()
    expect(screen.getByTestId('widget-vessel')).toBeInTheDocument()
    expect(screen.getByTestId('widget-solar')).toBeInTheDocument()
  })
})
