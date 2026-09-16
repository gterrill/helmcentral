import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { GaugeFields, defaultGaugeConfig } from '@/components/gauge-fields'
import type { GaugeWidgetConfig } from '@/lib/dashboard-widgets'
import type { SignalKPath } from '@/hooks/use-signalk-paths'

function config(overrides: Partial<GaugeWidgetConfig> = {}): GaugeWidgetConfig {
  return {
    path: 'propulsion.port.oilPressure',
    label: 'Oil Press',
    display: 'radial',
    quantity: 'pressure',
    unit: 'psi',
    min: 0,
    max: 100,
    ...overrides,
  }
}

function renderFields(value: GaugeWidgetConfig, onChange = vi.fn(), paths: SignalKPath[] = []) {
  render(<GaugeFields value={value} onChange={onChange} paths={paths} idPrefix="g" />)
  return onChange
}

describe('zone editing', () => {
  test('offers zones for the scaled display kinds', () => {
    renderFields(config())
    expect(screen.getByRole('button', { name: /add zone/i })).toBeInTheDocument()
  })

  // A numeric or indicator gauge has no scale to band, so there is nothing to
  // anchor a zone against.
  test('offers no zones for an unscaled display kind', () => {
    renderFields(config({ display: 'lamp' }))
    expect(screen.queryByRole('button', { name: /add zone/i })).not.toBeInTheDocument()
  })

  /**
   * Zones are expressed as a direction plus a threshold, not a free from/to
   * pair: the backend turns each band into one alarm threshold (ADR 0050), and
   * a band floating in the middle of the scale has no equivalent. Making the
   * editor incapable of producing one beats rejecting it after the fact.
   */
  test('a new zone is anchored to the bottom of the scale', () => {
    const onChange = renderFields(config())
    fireEvent.click(screen.getByRole('button', { name: /add zone/i }))

    const next = onChange.mock.calls[0][0] as GaugeWidgetConfig
    expect(next.zones).toHaveLength(1)
    expect(next.zones![0].from).toBe(0)
    expect(next.zones![0].to).toBeLessThan(100)
  })

  test('switching a zone to "above" re-anchors it to the top of the scale', () => {
    const zoned = config({ zones: [{ from: 0, to: 15, state: 'alarm' }] })
    const onChange = renderFields(zoned)

    fireEvent.change(screen.getByLabelText('Zone 1 direction'), { target: { value: 'above' } })

    const next = onChange.mock.calls[0][0] as GaugeWidgetConfig
    expect(next.zones![0].from).toBe(15)
    expect(next.zones![0].to).toBe(100)
  })

  test('editing the threshold keeps the zone anchored', () => {
    const zoned = config({ zones: [{ from: 80, to: 100, state: 'warn' }] })
    const onChange = renderFields(zoned)

    fireEvent.change(screen.getByLabelText('Zone 1 threshold'), { target: { value: '90' } })

    const next = onChange.mock.calls[0][0] as GaugeWidgetConfig
    expect(next.zones![0]).toEqual({ from: 90, to: 100, state: 'warn' })
  })

  test('changes the severity', () => {
    const zoned = config({ zones: [{ from: 0, to: 15, state: 'warn' }] })
    const onChange = renderFields(zoned)

    fireEvent.change(screen.getByLabelText('Zone 1 severity'), { target: { value: 'emergency' } })
    expect((onChange.mock.calls[0][0] as GaugeWidgetConfig).zones![0].state).toBe('emergency')
  })

  test('removes a zone', () => {
    const zoned = config({ zones: [{ from: 0, to: 15, state: 'alarm' }, { from: 90, to: 100, state: 'warn' }] })
    const onChange = renderFields(zoned)

    fireEvent.click(screen.getByRole('button', { name: 'Remove zone 1' }))
    expect((onChange.mock.calls[0][0] as GaugeWidgetConfig).zones).toEqual([{ from: 90, to: 100, state: 'warn' }])
  })

  // The operator needs to know the band is not merely decorative.
  test('says that a zone raises an alarm', () => {
    renderFields(config({ zones: [{ from: 0, to: 15, state: 'alarm' }] }))
    expect(screen.getByText(/raises? an alarm/i)).toBeInTheDocument()
  })

  test('shows the threshold in the display unit, not SI', () => {
    renderFields(config({ zones: [{ from: 0, to: 15, state: 'alarm' }] }))
    expect((screen.getByLabelText('Zone 1 threshold') as HTMLInputElement).value).toBe('15')
    // The unit sits beside the threshold input, not only in the unit select.
    const suffix = screen.getByLabelText('Zone 1 threshold').parentElement!
    expect(suffix).toHaveTextContent('psi')
  })
})

describe('choosePath', () => {
  // On the real boat most SignalK paths carry no units meta at all. Repicking
  // a path on a gauge that already had a known quantity must not clobber it —
  // the alternator temperature gauge stays °C, not silently drop to raw/K.
  test('picking a unit-less path keeps the gauge\'s existing quantity, unit and zones', () => {
    const zoned = config({
      quantity: 'temperature',
      unit: 'C',
      zones: [{ from: 80, to: 100, state: 'warn' }],
    })
    const noUnits: SignalKPath[] = [{ path: 'propulsion.port.alternatorTemperature' }]
    const onChange = renderFields(zoned, vi.fn(), noUnits)

    fireEvent.change(screen.getByLabelText('SignalK path'), {
      target: { value: 'propulsion.port.alternatorTemperature' },
    })

    const next = onChange.mock.calls[0][0] as GaugeWidgetConfig
    expect(next.path).toBe('propulsion.port.alternatorTemperature')
    expect(next.quantity).toBe('temperature')
    expect(next.unit).toBe('C')
    expect(next.zones).toEqual(zoned.zones)
  })

  test('picking a path with known units still infers the quantity and unit', () => {
    const zoned = config({ quantity: 'temperature', unit: 'C' })
    const withUnits: SignalKPath[] = [{ path: 'tanks.freshWater.pressure', units: 'Pa' }]
    const onChange = renderFields(zoned, vi.fn(), withUnits)

    fireEvent.change(screen.getByLabelText('SignalK path'), {
      target: { value: 'tanks.freshWater.pressure' },
    })

    const next = onChange.mock.calls[0][0] as GaugeWidgetConfig
    expect(next.quantity).toBe('pressure')
    expect(next.unit).toBe('kPa')
  })

  // A fresh gauge has never had a quantity assigned, so raw/raw picking a
  // unit-less path is unchanged behaviour, not a special case.
  test('a fresh gauge picking a unit-less path stays raw/raw', () => {
    const fresh = defaultGaugeConfig()
    const noUnits: SignalKPath[] = [{ path: 'propulsion.port.alternatorTemperature' }]
    const onChange = renderFields(fresh, vi.fn(), noUnits)

    fireEvent.change(screen.getByLabelText('SignalK path'), {
      target: { value: 'propulsion.port.alternatorTemperature' },
    })

    const next = onChange.mock.calls[0][0] as GaugeWidgetConfig
    expect(next.quantity).toBe('raw')
    expect(next.unit).toBe('raw')
  })
})
