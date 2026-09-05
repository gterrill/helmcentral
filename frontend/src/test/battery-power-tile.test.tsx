import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import { BatteryPowerTile } from '@/components/battery-power-tile'

const baseProps = {
  batterySocPercent: 82,
  chargingCurrentA: 14.2,
  chargingPowerW: 365.4,
  solarOutputW: 520.2,
  acOutputW: 780,
  dc12vPowerW: 144,
  dc24vVoltageV: 26.7,
  charger0CurrentA: null,
  charger0AcIn1CurrentA: null,
  charger0ChargingMode: null,
  charger0Error: null,
  batteryRatePercentPerHour: 1.1,
  timeToGoHours: 6.5,
  lastUpdateAgeS: 2,
}

test('renders Charger card with charger telemetry values', () => {
  render(
    <BatteryPowerTile
      {...baseProps}
      charger0CurrentA={23.4}
      charger0AcIn1CurrentA={10.8}
      charger0ChargingMode="bulk"
      charger0Error="none"
    />,
  )

  expect(screen.getByText('Charger')).toBeInTheDocument()
  expect(screen.getByText('23.4')).toBeInTheDocument()
  expect(screen.getByText('10.8')).toBeInTheDocument()
  expect(screen.getByText('Mode:')).toBeInTheDocument()
  expect(screen.getByText('bulk')).toBeInTheDocument()
  expect(screen.getByText('Error:')).toBeInTheDocument()
  expect(screen.getByText('none')).toBeInTheDocument()
})

test('renders Charger fallbacks when charger fields are unavailable', () => {
  render(<BatteryPowerTile {...baseProps} />)

  expect(screen.getByText('Charger')).toBeInTheDocument()
  expect(screen.getByText('Mode:')).toBeInTheDocument()
  expect(screen.getAllByText('—').length).toBeGreaterThan(0)
})

test('flags a stale feed instead of presenting its last readings as current', () => {
  // Same failure as the solar tile: when the Victron feed stops, the last
  // numbers it sent sit here looking like live measurements.
  render(<BatteryPowerTile {...baseProps} lastUpdateAgeS={5940} />)

  const badge = screen.getByTestId('tile-stale-badge')
  expect(badge).toBeInTheDocument()
  expect(badge).toHaveTextContent('1h 39m')

  // None of the frozen readings survive as numbers.
  expect(screen.queryByText('82')).not.toBeInTheDocument()
  expect(screen.queryByText('520')).not.toBeInTheDocument()
  expect(screen.getAllByText('—').length).toBeGreaterThan(0)
})

test('shows live readings and no stale badge when the feed is current', () => {
  render(<BatteryPowerTile {...baseProps} />)

  expect(screen.queryByTestId('tile-stale-badge')).not.toBeInTheDocument()
  expect(screen.getByText('82')).toBeInTheDocument()
})

test('uses a readout token, not the alert amber, for the normal discharging current and power', () => {
  // Discharging at anchor is the ordinary state, not a fault — text-amber-600
  // is reserved for actual alert thresholds elsewhere in the tile (e.g. a
  // charger error), so it must not light up permanently just because the
  // battery is supplying the boat rather than receiving power.
  render(<BatteryPowerTile {...baseProps} chargingCurrentA={-4.2} chargingPowerW={-120} />)

  const currentValue = screen.getByText('-4.2')
  const powerValue = screen.getByText('-120')
  expect(currentValue).not.toHaveClass('text-amber-600')
  expect(powerValue).not.toHaveClass('text-amber-600')
  expect(currentValue).toHaveClass('text-gauge-secondary')
  expect(powerValue).toHaveClass('text-gauge-secondary')
})

test('does not render the alert amber anywhere while discharging, charge rate falling and time-to-go counting down', () => {
  const { container } = render(
    <BatteryPowerTile
      {...baseProps}
      chargingCurrentA={-4.2}
      chargingPowerW={-120}
      batteryRatePercentPerHour={-1.4}
      timeToGoHours={-3.2}
    />,
  )

  expect(container.querySelectorAll('.text-amber-600')).toHaveLength(0)
})
