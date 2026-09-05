import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import { PositionTile } from '@/components/position-tile'

const baseProps = {
  latitude: -25.1,
  longitude: 152.9,
  headingTrue: 90,
  gnssValidationState: 'ok',
  gnssQualityIndicator: 2,
  gnssHdop: 0.9,
  gnssValidationReason: null,
  gnssSatellites: 11,
  placeName: 'Great Sandy Strait',
  lastUpdateAgeS: null,
}

test('renders the fix and place name', () => {
  render(<PositionTile {...baseProps} />)

  expect(screen.getByText('Great Sandy Strait')).toBeInTheDocument()
  expect(screen.getByText(/SATS/)).toBeInTheDocument()
})

// GNSS carries no per-fix age in the vessel-state payload today (see
// signalk.go's fetchSignalKVesselState), so lastUpdateAgeS is null until a
// source actually publishes one — this only proves the wiring is correct
// once an age is supplied, not that one exists yet.

test('flags the tile stale once a feed age passes the threshold', () => {
  render(<PositionTile {...baseProps} lastUpdateAgeS={5940} />)

  const badge = screen.getByTestId('tile-stale-badge')
  expect(badge).toHaveTextContent('1h 39m')
})

test('does not flag the tile stale when no update age is known', () => {
  render(<PositionTile {...baseProps} lastUpdateAgeS={null} />)

  expect(screen.queryByTestId('tile-stale-badge')).not.toBeInTheDocument()
})
