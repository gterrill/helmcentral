import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { RouteSummaryPanel } from '@/components/route-summary-panel'
import { calculateLegs, calculateRouteTotals } from '@/lib/route-calc'

const waypoints = [
  { lat: 0, lon: 0, name: 'Marina' },
  { lat: 1, lon: 0 },
]

function renderPanel(overrides: Partial<Parameters<typeof RouteSummaryPanel>[0]> = {}) {
  const legs = calculateLegs(waypoints, 6)
  const totals = calculateRouteTotals(legs)
  return render(
    <RouteSummaryPanel
      waypoints={waypoints}
      legs={legs}
      totals={totals}
      speedKts={6}
      onSpeedChange={() => undefined}
      onMoveWaypoint={() => undefined}
      onDeleteWaypoint={() => undefined}
      {...overrides}
    />,
  )
}

describe('RouteSummaryPanel', () => {
  it('shows an empty-state hint with no waypoints', () => {
    render(
      <RouteSummaryPanel
        waypoints={[]}
        legs={[]}
        totals={{ totalDistanceM: 0, totalEtaHours: 0 }}
        speedKts={6}
        onSpeedChange={() => undefined}
        onMoveWaypoint={() => undefined}
        onDeleteWaypoint={() => undefined}
      />,
    )
    expect(screen.getByText('Click the map to start adding waypoints.')).toBeInTheDocument()
  })

  it('renders waypoint names and per-leg distance/bearing/ETA', () => {
    renderPanel()

    expect(screen.getByText('Marina')).toBeInTheDocument()
    expect(screen.getByText('Waypoint 2')).toBeInTheDocument()
    expect(screen.getByText('60.0 nm')).toBeInTheDocument()
    expect(screen.getByText('0°')).toBeInTheDocument()
    expect(screen.getByText('ETA 10h 00m')).toBeInTheDocument()
  })

  it('shows route totals', () => {
    renderPanel()
    expect(screen.getByText(/60.0 nm · ETA 10h 00m/)).toBeInTheDocument()
  })

  it('disables move-up on the first waypoint and move-down on the last', () => {
    renderPanel()
    expect(screen.getByRole('button', { name: 'Move waypoint 1 up' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Move waypoint 2 down' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Move waypoint 1 down' })).not.toBeDisabled()
  })

  it('calls onMoveWaypoint and onDeleteWaypoint', () => {
    const onMoveWaypoint = vi.fn()
    const onDeleteWaypoint = vi.fn()
    renderPanel({ onMoveWaypoint, onDeleteWaypoint })

    fireEvent.click(screen.getByRole('button', { name: 'Move waypoint 1 down' }))
    expect(onMoveWaypoint).toHaveBeenCalledWith(0, 'down')

    fireEvent.click(screen.getByRole('button', { name: 'Delete waypoint 2' }))
    expect(onDeleteWaypoint).toHaveBeenCalledWith(1)
  })

  it('calls onSpeedChange when the speed input changes', () => {
    const onSpeedChange = vi.fn()
    renderPanel({ onSpeedChange })

    fireEvent.change(screen.getByLabelText('Planning speed in knots'), { target: { value: '8' } })
    expect(onSpeedChange).toHaveBeenCalledWith(8)
  })
})
