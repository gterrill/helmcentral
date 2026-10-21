import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { RouteTile } from '@/components/route-tile'
import type { ActiveRouteStatus } from '@/hooks/use-route-activation'

// One degree of latitude is 60 nm.
const active: ActiveRouteStatus = {
  state: 'active',
  route: { name: 'Marina to Anchorage', waypoints: [{ lat: 0, lon: 0, name: 'Marina' }, { lat: 1, lon: 0, name: 'Anchorage' }, { lat: 2, lon: 0 }] },
  pointIndex: 1,
  reverse: false,
  destination: null,
}

describe('RouteTile', () => {
  it('shows the active route name with the distance left to its end and an ETA', () => {
    render(<RouteTile status={active} latitude={0.5} longitude={0} speedKts={6} />)

    expect(screen.getByText('Marina to Anchorage')).toBeInTheDocument()
    // vessel -> Anchorage (30 nm) + Anchorage -> end (60 nm)
    expect(screen.getByText('90.1 nm')).toBeInTheDocument()
    expect(screen.getByText('ETA 15h 01m')).toBeInTheDocument()
  })

  it('shows -- for the ETA when the boat is not making way', () => {
    render(<RouteTile status={active} latitude={0.5} longitude={0} speedKts={0} />)

    expect(screen.getByText('90.1 nm')).toBeInTheDocument()
    expect(screen.getByText('ETA --')).toBeInTheDocument()
  })

  it('shows the destination name, distance and ETA when only a go-to is set', () => {
    const status: ActiveRouteStatus = { state: 'inactive', destination: { lat: 1, lon: 0, name: 'Hook Island' } }
    render(<RouteTile status={status} latitude={0} longitude={0} speedKts={10} />)

    expect(screen.getByText('Hook Island')).toBeInTheDocument()
    expect(screen.getByText('60.0 nm')).toBeInTheDocument()
    expect(screen.getByText('ETA 6h 00m')).toBeInTheDocument()
  })

  it('shows why route details are missing while keeping the destination', () => {
    const status: ActiveRouteStatus = {
      state: 'inactive',
      destination: { lat: 1, lon: 0, name: 'Hook Island' },
      routeError: 'Route details unavailable: gone',
    }
    render(<RouteTile status={status} latitude={0} longitude={0} speedKts={10} />)

    expect(screen.getByText('Hook Island')).toBeInTheDocument()
    expect(screen.getByText('ETA 6h 00m')).toBeInTheDocument()
    expect(screen.getByText('Route details unavailable: gone')).toBeInTheDocument()
  })

  it('calls an unnamed destination "Next waypoint"', () => {
    const status: ActiveRouteStatus = { state: 'inactive', destination: { lat: 1, lon: 0, name: '' } }
    render(<RouteTile status={status} latitude={0} longitude={0} speedKts={10} />)

    expect(screen.getByText('Next waypoint')).toBeInTheDocument()
  })

  it('shows -- when SignalK reports no route and no destination', () => {
    render(<RouteTile status={{ state: 'inactive', destination: null }} latitude={0} longitude={0} speedKts={6} />)

    expect(screen.getByTestId('route-tile-empty')).toHaveTextContent('--')
    expect(screen.queryByText(/No SignalK/)).not.toBeInTheDocument()
  })

  it('shows plain -- with no sub-line while the first poll is pending', () => {
    render(<RouteTile status={{ state: 'unknown', reason: 'loading' }} latitude={0} longitude={0} speedKts={6} />)

    expect(screen.getByTestId('route-tile-empty')).toHaveTextContent('--')
    expect(screen.queryByText('Route unavailable')).not.toBeInTheDocument()
  })

  it('shows -- and "Route unavailable" on an error, never "inactive" or "No SignalK"', () => {
    render(<RouteTile status={{ state: 'unknown', reason: 'error' }} latitude={0} longitude={0} speedKts={6} />)

    expect(screen.getByTestId('route-tile-empty')).toHaveTextContent('--')
    expect(screen.getByText('Route unavailable')).toBeInTheDocument()
    expect(screen.queryByText(/inactive|No SignalK/i)).not.toBeInTheDocument()
  })

  it('calls an unnamed active route "Active route"', () => {
    const status: ActiveRouteStatus = { ...(active as Extract<ActiveRouteStatus, { state: 'active' }>), route: { name: '  ', waypoints: [{ lat: 0, lon: 0 }, { lat: 1, lon: 0 }] } }
    render(<RouteTile status={status} latitude={0} longitude={0} speedKts={6} />)

    expect(screen.getByText('Active route')).toBeInTheDocument()
  })

  it('uses the destination when the active route has no waypoints', () => {
    const status: ActiveRouteStatus = {
      state: 'active', route: { name: 'Empty', waypoints: [] }, pointIndex: 0, reverse: false,
      destination: { lat: 1, lon: 0, name: 'Hook Island' },
    }
    render(<RouteTile status={status} latitude={0} longitude={0} speedKts={10} />)

    expect(screen.getByText('Hook Island')).toBeInTheDocument()
    expect(screen.queryByText('Empty')).not.toBeInTheDocument()
  })

  it('shows -- when the active route has no waypoints and no destination', () => {
    const status: ActiveRouteStatus = { state: 'active', route: { name: 'Empty', waypoints: [] }, pointIndex: 0, reverse: false, destination: null }
    render(<RouteTile status={status} latitude={0} longitude={0} speedKts={10} />)

    expect(screen.getByTestId('route-tile-empty')).toHaveTextContent('--')
  })

  it('shows -- for the distance without a position fix', () => {
    render(<RouteTile status={active} latitude={null} longitude={null} speedKts={6} />)

    expect(screen.getByText('Marina to Anchorage')).toBeInTheDocument()
    expect(screen.getByText('--')).toBeInTheDocument()
    expect(screen.getByText('ETA --')).toBeInTheDocument()
  })

  it('does nothing when tapped', () => {
    render(<RouteTile status={active} latitude={0.5} longitude={0} speedKts={6} />)
    expect(screen.getByTestId('route-tile').className).not.toMatch(/cursor-pointer/)
  })
})
