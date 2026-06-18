import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { RoutePlannerDrawer } from '@/components/route-planner-drawer'
import type { Route } from '@/hooks/use-routes'
import type { ActiveRouteStatus } from '@/hooks/use-route-activation'

const routeA: Route = {
  id: 'a',
  name: 'Route A',
  waypoints: [{ lat: 0, lon: 0 }, { lat: 1, lon: 0 }],
  created_at: '',
  updated_at: '',
}

const routeB: Route = {
  id: 'b',
  name: 'Route B',
  waypoints: [{ lat: 2, lon: 0 }, { lat: 3, lon: 0 }],
  created_at: '',
  updated_at: '',
}

function renderDrawer(overrides: Partial<Parameters<typeof RoutePlannerDrawer>[0]> = {}) {
  return render(
    <RoutePlannerDrawer
      isDarkTheme={false}
      routes={[routeA, routeB]}
      loading={false}
      error={null}
      createRoute={vi.fn()}
      updateRoute={vi.fn()}
      deleteRoute={vi.fn()}
      dashboardRouteId={null}
      onSetDashboardRouteId={vi.fn()}
      activationStatus={{ state: 'inactive' }}
      activating={false}
      deactivating={false}
      activateError={null}
      onActivate={vi.fn().mockResolvedValue(true)}
      onDeactivate={vi.fn().mockResolvedValue(true)}
      {...overrides}
    />,
  )
}

describe('RoutePlannerDrawer activation controls', () => {
  it('shows an Activate button for each route when nothing is active', () => {
    renderDrawer()
    expect(screen.getByRole('button', { name: 'Activate Route A' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Activate Route B' })).toBeInTheDocument()
  })

  it('shows Deactivate and an ACTIVE badge on the active route only', () => {
    renderDrawer({ activationStatus: { state: 'active', routeId: 'a', pointIndex: 0, reverse: false } })

    expect(screen.getByRole('button', { name: 'Deactivate Route A' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Activate Route B' })).toBeInTheDocument()
    expect(screen.getByText('ACTIVE')).toBeInTheDocument()
  })

  it('calls onActivate with the route id when Activate is clicked', async () => {
    const onActivate = vi.fn().mockResolvedValue(true)
    renderDrawer({ onActivate })

    fireEvent.click(screen.getByRole('button', { name: 'Activate Route A' }))

    expect(onActivate).toHaveBeenCalledWith('a')
  })

  it('calls onDeactivate when Deactivate is clicked', async () => {
    const onDeactivate = vi.fn().mockResolvedValue(true)
    renderDrawer({
      activationStatus: { state: 'active', routeId: 'a', pointIndex: 0, reverse: false },
      onDeactivate,
    })

    fireEvent.click(screen.getByRole('button', { name: 'Deactivate Route A' }))

    expect(onDeactivate).toHaveBeenCalled()
  })

  it('disables activation buttons on other rows while one is in flight', () => {
    renderDrawer({ activating: true })

    expect(screen.getByRole('button', { name: 'Activate Route A' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Activate Route B' })).toBeDisabled()
  })

  it('shows the raw error text verbatim when activateError is set', () => {
    renderDrawer({ activateError: 'signalk returned status 400: bad geometry' })
    expect(screen.getByText('signalk returned status 400: bad geometry')).toBeInTheDocument()
  })

  it('shows a status-unknown indicator and no ACTIVE badge when status is unknown', () => {
    renderDrawer({ activationStatus: { state: 'unknown' } })
    expect(screen.getByText('status unknown')).toBeInTheDocument()
    expect(screen.queryByText('ACTIVE')).not.toBeInTheDocument()
  })

  it('shows a header note when an unrecognized route is active', () => {
    const status: ActiveRouteStatus = { state: 'active', routeId: null, pointIndex: 0, reverse: false }
    renderDrawer({ activationStatus: status })
    expect(screen.getByText(/isn't one of your saved routes/)).toBeInTheDocument()
    expect(screen.queryByText('ACTIVE')).not.toBeInTheDocument()
  })

  it('does not affect the dashboard star toggle behavior', () => {
    const onSetDashboardRouteId = vi.fn()
    renderDrawer({
      onSetDashboardRouteId,
      activationStatus: { state: 'active', routeId: 'a', pointIndex: 0, reverse: false },
    })

    fireEvent.click(screen.getByRole('button', { name: 'Show Route A on dashboard' }))

    expect(onSetDashboardRouteId).toHaveBeenCalledWith('a')
  })
})
