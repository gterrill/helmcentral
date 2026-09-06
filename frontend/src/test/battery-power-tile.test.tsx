import { render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import { BatteryPowerTile } from '@/components/battery-power-tile'
import { projectSocAtDawn, type OvernightProjection } from '@/lib/soc-projection'

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

test('the state-of-charge numeral is the only text-gauge-primary element on the tile', () => {
  const { container } = render(<BatteryPowerTile {...baseProps} />)

  const gaugePrimary = container.querySelectorAll('.text-gauge-primary')
  expect(gaugePrimary).toHaveLength(1)
  expect(gaugePrimary[0]).toHaveTextContent('82')
})

test('the Loads total reads as plain consumption, not a gauge colour', () => {
  render(<BatteryPowerTile {...baseProps} acOutputW={222} dc12vPowerW={380} />)

  expect(screen.getByText('602')).toHaveClass('text-foreground')
})

test('the % after the SoC numeral is a unit suffix, not the readout', () => {
  render(<BatteryPowerTile {...baseProps} />)

  const percent = screen.getByText('%')
  expect(percent).toHaveClass('font-display')
  expect(percent).toHaveClass('text-muted-foreground')
})

test('no text-[11px] element is also a shouting uppercase label', () => {
  // The micro-typography scale reserves text-[11px] for sub-readouts and
  // text-[10px] for uppercase labels; the two must never mix on one element.
  const { container } = render(<BatteryPowerTile {...baseProps} />)

  const elevenPx = Array.from(container.querySelectorAll('*')).filter((el) =>
    el.classList.contains('text-[11px]'),
  )
  expect(elevenPx.length).toBeGreaterThan(0)
  for (const el of elevenPx) {
    expect(el.classList.contains('uppercase')).toBe(false)
  }
})

test('renders four sub-cards at anchor with no charger reporting', () => {
  const { container } = render(<BatteryPowerTile {...baseProps} />)

  expect(container.querySelectorAll('.rounded-md.border')).toHaveLength(4)
})

test('renders a fifth sub-card once a charger reports', () => {
  const { container } = render(
    <BatteryPowerTile {...baseProps} charger0ChargingMode="bulk" charger0AcIn1CurrentA={10.8} />,
  )

  expect(container.querySelectorAll('.rounded-md.border')).toHaveLength(5)
})

test('labels reflect the recomposed layout: Net, Solar and Loads', () => {
  render(<BatteryPowerTile {...baseProps} />)

  expect(screen.getByText('Net')).toBeInTheDocument()
  expect(screen.getByText('Solar')).toBeInTheDocument()
  expect(screen.getByText('Loads')).toBeInTheDocument()
})

test('the old per-field labels are gone', () => {
  render(
    <BatteryPowerTile
      {...baseProps}
      charger0ChargingMode="bulk"
      charger0AcIn1CurrentA={10.8}
      charger0Error="overtemp"
    />,
  )

  for (const gone of ['Battery', 'AC Draw', 'DC Draw', 'Charger', 'Time Remaining', 'Charge Rate', 'Mode:', 'Error:']) {
    expect(screen.queryByText(gone)).not.toBeInTheDocument()
  }
})

test('a positive rate reads "To full" with the signed hours spelled out', () => {
  render(<BatteryPowerTile {...baseProps} batteryRatePercentPerHour={1.1} timeToGoHours={6.5} />)

  expect(screen.getByText('To full')).toBeInTheDocument()
  expect(screen.getByText('6h 30m')).toBeInTheDocument()
})

test('a negative rate reads "To empty" with the signed hours spelled out', () => {
  render(<BatteryPowerTile {...baseProps} batteryRatePercentPerHour={-1.4} timeToGoHours={-3.2} />)

  expect(screen.getByText('To empty')).toBeInTheDocument()
  expect(screen.getByText('3h 12m')).toBeInTheDocument()
})

test('Loads totals AC and DC draw into one figure', () => {
  render(<BatteryPowerTile {...baseProps} acOutputW={222} dc12vPowerW={380} />)

  expect(screen.getByText('602')).toBeInTheDocument()
  expect(screen.getByText('AC 222 · DC 380')).toBeInTheDocument()
})

test('a missing load component blanks the total rather than showing a partial sum', () => {
  render(<BatteryPowerTile {...baseProps} acOutputW={222} dc12vPowerW={null} />)

  const dash = screen.getByText('—')
  expect(dash).toHaveClass('text-foreground')
  expect(screen.getByText('AC 222 · DC —')).toBeInTheDocument()
})

test('no Shore line when the charger has reported nothing', () => {
  render(<BatteryPowerTile {...baseProps} />)

  expect(screen.queryByText('Shore')).not.toBeInTheDocument()
})

test('Shore line reports the mode and AC-in current once a charger reports', () => {
  render(<BatteryPowerTile {...baseProps} charger0ChargingMode="bulk" charger0AcIn1CurrentA={10.8} />)

  expect(screen.getByText('Shore')).toBeInTheDocument()
  expect(screen.getByText(/bulk/)).toBeInTheDocument()
  expect(screen.getByText(/10\.8/)).toBeInTheDocument()
})

test('a charger error replaces the mode word and renders even with everything else unknown', () => {
  const { container } = render(
    <BatteryPowerTile
      {...baseProps}
      charger0CurrentA={null}
      charger0AcIn1CurrentA={null}
      charger0ChargingMode={null}
      charger0Error="overtemp"
    />,
  )

  const errorEl = container.querySelector('.text-red-600')
  expect(errorEl).not.toBeNull()
  expect(errorEl).toHaveTextContent('overtemp')
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

const bands = { warnBelow: 20, alarmBelow: 10 }

test('SoC in band keeps the plain gauge colour and draws both band markers at their threshold widths', () => {
  render(<BatteryPowerTile {...baseProps} batterySocPercent={63} socBands={bands} />)

  expect(screen.getByText('63')).toHaveClass('text-gauge-primary')
  expect(screen.getByTestId('soc-band-alarm')).toHaveStyle({ width: '10%' })
  expect(screen.getByTestId('soc-band-warn')).toHaveStyle({ width: '10%' })
})

test('SoC below the warn band colours the numeral amber', () => {
  render(<BatteryPowerTile {...baseProps} batterySocPercent={18} socBands={bands} />)

  expect(screen.getByText('18')).toHaveClass('text-amber-600')
})

test('SoC below the alarm band colours the numeral red', () => {
  render(<BatteryPowerTile {...baseProps} batterySocPercent={8} socBands={bands} />)

  expect(screen.getByText('8')).toHaveClass('text-red-600')
})

test('discharging above the warn band reads "To {warn}%" with the hours to reach it', () => {
  render(
    <BatteryPowerTile
      {...baseProps}
      batterySocPercent={63}
      batteryRatePercentPerHour={-2.0}
      timeToGoHours={-21.5}
      socBands={bands}
    />,
  )

  expect(screen.getByText('To 20%')).toBeInTheDocument()
  // (63 - 20) / 2.0 = 21.5h, and formatTimeToGo rounds anything >= 10h to a
  // whole number of hours.
  expect(screen.getByText('22h')).toBeInTheDocument()
})

test('discharging between the warn and alarm bands reads "To {alarm}%"', () => {
  render(
    <BatteryPowerTile
      {...baseProps}
      batterySocPercent={18}
      batteryRatePercentPerHour={-2.0}
      timeToGoHours={-4}
      socBands={bands}
    />,
  )

  expect(screen.getByText('To 10%')).toBeInTheDocument()
})

test('discharging below the alarm band falls back to the plain "To empty" label', () => {
  render(
    <BatteryPowerTile
      {...baseProps}
      batterySocPercent={8}
      batteryRatePercentPerHour={-2.0}
      timeToGoHours={-1}
      socBands={bands}
    />,
  )

  expect(screen.getByText('To empty')).toBeInTheDocument()
})

test('with no socBands prop the tile behaves exactly as before: no band markers, plain colour, "To empty"', () => {
  render(
    <BatteryPowerTile
      {...baseProps}
      batterySocPercent={8}
      batteryRatePercentPerHour={-2.0}
      timeToGoHours={-1}
    />,
  )

  expect(screen.queryByTestId('soc-band-alarm')).not.toBeInTheDocument()
  expect(screen.queryByTestId('soc-band-warn')).not.toBeInTheDocument()
  expect(screen.getByText('8')).toHaveClass('text-gauge-primary')
  expect(screen.getByText('To empty')).toBeInTheDocument()
})

// Dawn projection (Phase 4). now/sunset/sunrise below are the daytime
// fixture from the plan: sunset comes before sunrise, so it's the
// afternoon and the next sunset is still hours away.
afterEach(() => {
  vi.useRealTimers()
})

const DAYTIME_NOW = new Date('2026-09-07T02:00:00Z')

function overnightFixture(overrides: Partial<OvernightProjection> = {}): OvernightProjection {
  return {
    socPath: 'electrical.batteries.0.capacity.stateOfCharge',
    sunset: new Date('2026-09-07T07:56:00Z'),
    sunrise: new Date('2026-09-07T20:07:00Z'),
    basis: 'history',
    nightRatePercentPerHour: -2.5,
    nightsUsed: 4,
    nightsConsidered: 7,
    reason: null,
    ...overrides,
  }
}

test('renders the dawn estimate for a history basis with the nights-used token', () => {
  vi.useFakeTimers()
  vi.setSystemTime(DAYTIME_NOW)

  const overnight = overnightFixture({ nightRatePercentPerHour: -2.5, nightsUsed: 4 })
  render(
    <BatteryPowerTile
      {...baseProps}
      batterySocPercent={63}
      batteryRatePercentPerHour={2.8}
      overnight={overnight}
    />,
  )

  const expected = projectSocAtDawn({
    socPercent: 63,
    liveRatePercentPerHour: 2.8,
    projection: overnight,
    now: DAYTIME_NOW,
  })

  expect(screen.getByText(/Dawn/)).toBeInTheDocument()
  expect(screen.getByTestId('dawn-estimate')).toHaveTextContent(`${Math.round(expected!.socPercent)}%`)
  // Twice on purpose: inline at lg and up, and as its own footer row below
  // lg, where a tablet has no hover to reveal the tooltip.
  expect(screen.getAllByText(/4 nights/)).toHaveLength(2)
})

test('renders the dawn estimate for a linear basis with the backend reason in its title', () => {
  vi.useFakeTimers()
  vi.setSystemTime(DAYTIME_NOW)

  const overnight = overnightFixture({
    basis: 'linear',
    nightRatePercentPerHour: null,
    reason: 'influxdb not configured',
  })
  render(
    <BatteryPowerTile
      {...baseProps}
      batterySocPercent={63}
      batteryRatePercentPerHour={2.8}
      overnight={overnight}
    />,
  )

  expect(screen.getAllByText(/at current rate/)).toHaveLength(2)
  const outer = screen.getByText(/Dawn/)
  expect(outer).toHaveAttribute('title', expect.stringContaining('influxdb not configured'))
})

test('colours the dawn estimate by the SoC severity ladder when it lands in a band', () => {
  vi.useFakeTimers()
  vi.setSystemTime(DAYTIME_NOW)

  const overnight = overnightFixture({ nightRatePercentPerHour: -1.0 })
  render(
    <BatteryPowerTile
      {...baseProps}
      batterySocPercent={30}
      batteryRatePercentPerHour={0}
      overnight={overnight}
      socBands={{ warnBelow: 20, alarmBelow: 10 }}
    />,
  )

  const estimate = screen.getByTestId('dawn-estimate')
  expect(estimate).toHaveClass('text-amber-600')
  expect(estimate).toHaveTextContent('18%')
})

test('shows a dash and the backend reason when the basis is none', () => {
  vi.useFakeTimers()
  vi.setSystemTime(DAYTIME_NOW)

  const overnight = overnightFixture({
    basis: 'none',
    nightRatePercentPerHour: null,
    reason: 'need 2 usable nights, have 1',
  })
  render(<BatteryPowerTile {...baseProps} overnight={overnight} />)

  const estimate = screen.getByTestId('dawn-estimate')
  expect(estimate).toHaveTextContent('—')
  const outer = screen.getByText(/Dawn/)
  expect(outer).toHaveAttribute('title', expect.stringContaining('need 2 usable nights, have 1'))
})

test('omits the dawn segment entirely when no overnight projection is supplied', () => {
  render(<BatteryPowerTile {...baseProps} />)

  expect(screen.queryByText(/Dawn/)).not.toBeInTheDocument()
})

test('blanks the dawn estimate on a stale feed even though the overnight projection is present', () => {
  vi.useFakeTimers()
  vi.setSystemTime(DAYTIME_NOW)

  const overnight = overnightFixture()
  render(<BatteryPowerTile {...baseProps} lastUpdateAgeS={5940} overnight={overnight} />)

  const estimate = screen.getByTestId('dawn-estimate')
  expect(estimate).toHaveTextContent('—')
})
