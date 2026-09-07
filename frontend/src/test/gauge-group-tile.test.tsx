import { render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { GaugeGroupTile } from '@/components/gauge-group-tile'
import type { GaugeGroupWidgetConfig } from '@/lib/dashboard-widgets'

const portEngine: GaugeGroupWidgetConfig = {
  title: 'Port',
  gauges: [
    { path: 'propulsion.port.revolutions', label: 'RPM', display: 'numeric', quantity: 'frequency', unit: 'rpm', decimals: 0 },
    { path: 'propulsion.port.oilPressure', label: 'Oil Press', display: 'numeric', quantity: 'pressure', unit: 'psi' },
    { path: 'propulsion.port.exhaustTemperature', label: 'Exhaust', display: 'numeric', quantity: 'temperature', unit: 'C' },
  ],
}

const values = {
  'propulsion.port.revolutions': 30,
  'propulsion.port.oilPressure': 241325,
  // exhaustTemperature deliberately absent: the sensor has not reported.
}

describe('GaugeGroupTile', () => {
  test('titles itself from the group title', () => {
    render(<GaugeGroupTile config={portEngine} values={values} editing={false} onConfigure={vi.fn()} />)
    expect(screen.getByText('Port')).toBeInTheDocument()
  })

  test('renders one labelled readout per member, converted from SI', () => {
    render(<GaugeGroupTile config={portEngine} values={values} editing={false} onConfigure={vi.fn()} />)

    // The label and the unit suffix both read "RPM"/"rpm" here — an operator
    // labelling an RPM gauge "RPM" is the obvious thing to do, and the unit
    // suffix stays regardless, so both are expected.
    expect(screen.getAllByText(/^rpm$/i)).toHaveLength(2)
    expect(screen.getByText('1800')).toBeInTheDocument()
    expect(screen.getByText('Oil Press')).toBeInTheDocument()
    expect(screen.getByText('35.0')).toBeInTheDocument()
  })

  // The whole point of a group is that one dead sensor does not blank the tile.
  test('an absent member shows the structural dash, never a zero', () => {
    render(<GaugeGroupTile config={portEngine} values={values} editing={false} onConfigure={vi.fn()} />)

    expect(screen.getByText('Exhaust')).toBeInTheDocument()
    expect(screen.getByText('--')).toBeInTheDocument()
    expect(screen.queryByText('0')).not.toBeInTheDocument()
  })

  test('colours a member whose reading is in an alarm zone', () => {
    const config: GaugeGroupWidgetConfig = {
      title: 'Port',
      gauges: [{
        path: 'propulsion.port.oilPressure',
        label: 'Oil Press',
        display: 'numeric',
        quantity: 'pressure',
        unit: 'psi',
        zones: [{ from: 0, to: 20, state: 'alarm' }],
      }],
    }
    const { rerender } = render(
      <GaugeGroupTile config={config} values={{ 'propulsion.port.oilPressure': 241325 }} editing={false} onConfigure={vi.fn()} />,
    )
    expect(screen.getByText('35.0').className).toContain('text-gauge-primary')

    rerender(
      <GaugeGroupTile config={config} values={{ 'propulsion.port.oilPressure': 68947 }} editing={false} onConfigure={vi.fn()} />,
    )
    expect(screen.getByText('10.0').className).toContain('text-red-600')
  })

  test('offers the config button only in layout mode', () => {
    const onConfigure = vi.fn()
    const { rerender } = render(
      <GaugeGroupTile config={portEngine} values={values} editing={false} onConfigure={onConfigure} />,
    )
    expect(screen.queryByLabelText('Configure Port')).not.toBeInTheDocument()

    rerender(<GaugeGroupTile config={portEngine} values={values} editing onConfigure={onConfigure} />)
    screen.getByLabelText('Configure Port').click()
    expect(onConfigure).toHaveBeenCalledOnce()
  })

  test('falls back to a structural title when the group is unnamed', () => {
    render(<GaugeGroupTile config={{ ...portEngine, title: '  ' }} values={values} editing={false} onConfigure={vi.fn()} />)
    expect(screen.getByText('Gauges')).toBeInTheDocument()
  })
})

/**
 * The tile edge carries the worst of every member's zone (ADR 0081).
 */
describe('tile state (ADR 0081)', () => {
  const zoned: GaugeGroupWidgetConfig = {
    title: 'Port',
    gauges: [
      { path: 'propulsion.port.revolutions', label: 'RPM', display: 'numeric', quantity: 'frequency', unit: 'rpm', decimals: 0 },
      {
        path: 'propulsion.port.oilPressure', label: 'Oil Press', display: 'numeric', quantity: 'pressure', unit: 'psi',
        zones: [{ from: 0, to: 20, state: 'warn' }],
      },
      {
        path: 'propulsion.port.exhaustTemperature', label: 'Exhaust', display: 'numeric', quantity: 'temperature', unit: 'C',
        zones: [{ from: 90, to: 200, state: 'alarm' }],
      },
    ],
  }

  test('carries the worst member zone up to the tile edge', () => {
    const { container } = render(
      <GaugeGroupTile
        config={zoned}
        values={{ 'propulsion.port.revolutions': 30, 'propulsion.port.oilPressure': 68947, 'propulsion.port.exhaustTemperature': 373.15 }}
        editing={false} onConfigure={vi.fn()}
      />,
    )
    expect(container.querySelector('[data-slot="card"]')).toHaveAttribute('data-state', 'alarm')
  })

  test('carries no state when every member is in its normal band or has none', () => {
    const { container } = render(
      <GaugeGroupTile config={zoned} values={{ 'propulsion.port.revolutions': 30 }} editing={false} onConfigure={vi.fn()} />,
    )
    expect(container.querySelector('[data-slot="card"]')).not.toHaveAttribute('data-state')
  })
})
