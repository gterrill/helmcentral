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
