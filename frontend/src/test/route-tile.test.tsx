import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { RouteTile } from '@/components/route-tile'
import type { Route } from '@/hooks/use-routes'

const route: Route = {
  id: 'r1',
  name: 'Marina to Anchorage',
  waypoints: [{ lat: 0, lon: 0 }, { lat: 1, lon: 0 }],
  planning_speed_kts: 6,
  created_at: '',
  updated_at: '',
}

describe('RouteTile', () => {
  it('renders nothing when no route is marked for the dashboard', () => {
    const { container } = render(<RouteTile speedKts={6} routes={[route]} dashboardRouteId={null} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('renders nothing when the marked route id is not in the routes list', () => {
    const { container } = render(<RouteTile speedKts={6} routes={[]} dashboardRouteId="missing" />)
    expect(container).toBeEmptyDOMElement()
  })

  it('renders the marked route name, distance, and ETA', () => {
    render(<RouteTile speedKts={6} routes={[route]} dashboardRouteId="r1" />)

    expect(screen.getByText('Marina to Anchorage')).toBeInTheDocument()
    expect(screen.getByText('60.0 nm')).toBeInTheDocument()
    expect(screen.getByText('ETA 10h 00m')).toBeInTheDocument()
  })

  it('calls onOpen when clicked', () => {
    const onOpen = vi.fn()
    render(<RouteTile speedKts={6} routes={[route]} dashboardRouteId="r1" onOpen={onOpen} />)

    fireEvent.click(screen.getByTestId('route-tile'))
    expect(onOpen).toHaveBeenCalled()
  })
})
