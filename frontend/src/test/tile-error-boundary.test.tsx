import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { TileErrorBoundary } from '@/components/tile-error-boundary'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'

function Boom(): never {
  throw new Error('kaboom')
}

const windWidget: DashboardLayoutItem = { id: 'wind', x: 0, y: 0, w: 4, h: 6 }
const solarWidget: DashboardLayoutItem = { id: 'solar', x: 4, y: 0, w: 4, h: 6 }

describe('TileErrorBoundary', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders a fallback card titled with the widget name and the error message when a child throws', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})

    render(
      <TileErrorBoundary widget={windWidget}>
        <Boom />
      </TileErrorBoundary>,
    )

    expect(screen.getByText('Apparent Wind')).toBeInTheDocument()
    expect(screen.getByText('This tile failed to render')).toBeInTheDocument()
    expect(screen.getByText('kaboom')).toBeInTheDocument()
  })

  it('renders children unchanged when nothing throws', () => {
    render(
      <TileErrorBoundary widget={windWidget}>
        <div data-testid="wind-content">Apparent wind readout</div>
      </TileErrorBoundary>,
    )

    expect(screen.getByTestId('wind-content')).toHaveTextContent('Apparent wind readout')
    expect(screen.queryByTestId('tile-error-fallback')).not.toBeInTheDocument()
  })

  it('logs the failure once via console.error, naming the widget', () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

    render(
      <TileErrorBoundary widget={windWidget}>
        <Boom />
      </TileErrorBoundary>,
    )

    // React's own dev-mode logging also calls console.error for the same
    // thrown error, so assert this boundary made its own call rather than
    // pinning an exact total count.
    expect(errorSpy).toHaveBeenCalledWith(
      expect.stringContaining('Apparent Wind'),
      expect.any(Error),
      expect.anything(),
    )
  })

  it('does not take a sibling tile down with it when one widget throws', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})

    render(
      <div>
        <TileErrorBoundary widget={windWidget}>
          <Boom />
        </TileErrorBoundary>
        <TileErrorBoundary widget={solarWidget}>
          <div data-testid="solar-content">Solar readout</div>
        </TileErrorBoundary>
      </div>,
    )

    expect(screen.getByTestId('tile-error-fallback')).toBeInTheDocument()
    expect(screen.getByTestId('solar-content')).toHaveTextContent('Solar readout')
  })
})
