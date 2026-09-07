import { render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { LampStripTile } from '@/components/lamp-strip-tile'
import type { LampStripWidgetConfig } from '@/lib/dashboard-widgets'
import { severityFill } from '@/lib/severity'

const ribbon: LampStripWidgetConfig = {
  title: 'Status',
  lamps: [
    { path: 'electrical.generator.state', label: 'GEN' },
    { path: 'propulsion.wing.revolutions', label: 'WING' },
    { path: 'electrical.inverter.state', label: 'INV' },
  ],
  showCheck: true,
}

const values = {
  'electrical.generator.state': 1,
  'propulsion.wing.revolutions': 0,
  // inverter absent: nothing has reported it.
}

function renderStrip(config = ribbon, worstAlarmState = 'normal', onOpenAlarms = vi.fn(), ages?: Record<string, number | null>) {
  render(
    <LampStripTile
      config={config}
      values={values}
      ages={ages}
      worstAlarmState={worstAlarmState}
      editing={false}
      onConfigure={vi.fn()}
      onOpenAlarms={onOpenAlarms}
    />,
  )
  return onOpenAlarms
}

describe('LampStripTile', () => {
  test('renders one lamp per entry, labelled', () => {
    renderStrip()
    expect(screen.getByText('GEN')).toBeInTheDocument()
    expect(screen.getByText('WING')).toBeInTheDocument()
    expect(screen.getByText('INV')).toBeInTheDocument()
  })

  test('lights a lamp on a non-zero value and leaves zero unlit', () => {
    renderStrip()
    expect(screen.getByLabelText('GEN: on')).toBeInTheDocument()
    expect(screen.getByLabelText('WING: off')).toBeInTheDocument()
  })

  // An absent path is not a zero. A lamp lit because nothing reported would be
  // the same dangerous failure the gauges' structural dash exists to prevent.
  test('an absent path reads as no data, not as off', () => {
    renderStrip()
    expect(screen.getByLabelText('INV: no data')).toBeInTheDocument()
  })

  /**
   * An indicator is not a control (ADR 0080): "on" is the healthy green,
   * not Signal Blue, which DESIGN.md reserves for interactive chrome.
   */
  test('lights an "on" lamp in the healthy green, not Signal Blue', () => {
    renderStrip()
    const lamp = screen.getByLabelText('GEN: on')
    const circle = lamp.querySelector('circle')!
    expect(circle.getAttribute('fill')).toBe(severityFill('normal'))
  })

  test('invert lights a lamp whose healthy state is off', () => {
    renderStrip({
      ...ribbon,
      lamps: [{ path: 'propulsion.wing.revolutions', label: 'WING', invert: true }],
      showCheck: false,
    })
    expect(screen.getByLabelText('WING: on')).toBeInTheDocument()
  })

  test('the check lamp takes its colour from the worst active alarm', () => {
    const { unmount } = render(
      <LampStripTile config={ribbon} values={values} worstAlarmState="normal" editing={false} onConfigure={vi.fn()} onOpenAlarms={vi.fn()} />,
    )
    expect(screen.getByLabelText(/^CHK/).className).not.toContain('red')
    unmount()

    renderStrip(ribbon, 'alarm')
    expect(screen.getByLabelText('CHK: alarm')).toBeInTheDocument()
  })

  test('the check lamp opens the alarms drawer', () => {
    const onOpenAlarms = renderStrip(ribbon, 'warn')
    screen.getByLabelText('CHK: warn').click()
    expect(onOpenAlarms).toHaveBeenCalledOnce()
  })

  test('omits the check lamp when it is not wanted', () => {
    renderStrip({ ...ribbon, showCheck: false })
    expect(screen.queryByLabelText(/^CHK/)).not.toBeInTheDocument()
  })

  // Sixteen lamps must scroll inside the tile, never stretch it.
  test('scrolls rather than stretching the tile', () => {
    renderStrip()
    expect(screen.getByTestId('lamp-strip-row').className).toContain('overflow-x-auto')
  })

  /**
   * ADR 0083: a frozen source must never leave a lamp lit. GEN's value (1)
   * would normally read "on"; a stale age overrides that to the no-data
   * treatment instead.
   */
  describe('staleness (ADR 0083)', () => {
    test('a frozen path goes unlit with the stale marker, never on', () => {
      renderStrip(ribbon, 'normal', vi.fn(), { 'electrical.generator.state': 300 })
      expect(screen.queryByLabelText('GEN: on')).not.toBeInTheDocument()
      const lamp = screen.getByLabelText('GEN: stale 5m')
      expect(lamp).toHaveAttribute('data-state', 'stale')
      const circle = lamp.querySelector('circle')!
      // The same unlit border colour "no data" gets, not the healthy green.
      expect(circle.getAttribute('fill')).not.toBe(severityFill('normal'))
    })

    test('a fresh age leaves an on lamp lit', () => {
      renderStrip(ribbon, 'normal', vi.fn(), { 'electrical.generator.state': 4 })
      expect(screen.getByLabelText('GEN: on')).toBeInTheDocument()
    })

    test('an unknown age never counts as stale', () => {
      renderStrip()
      expect(screen.getByLabelText('GEN: on')).toBeInTheDocument()
    })

    // The CHK lamp reads the alarm rollup, not a bound path, so ages never
    // touch it.
    test('leaves the CHK lamp alone', () => {
      renderStrip(ribbon, 'warn', vi.fn(), { 'electrical.generator.state': 300 })
      expect(screen.getByLabelText('CHK: warn')).toBeInTheDocument()
    })
  })

  test('offers the config button only in layout mode', () => {
    const onConfigure = vi.fn()
    const { rerender } = render(
      <LampStripTile config={ribbon} values={values} worstAlarmState="normal" editing={false} onConfigure={onConfigure} onOpenAlarms={vi.fn()} />,
    )
    expect(screen.queryByLabelText('Configure Status')).not.toBeInTheDocument()

    rerender(<LampStripTile config={ribbon} values={values} worstAlarmState="normal" editing onConfigure={onConfigure} onOpenAlarms={vi.fn()} />)
    screen.getByLabelText('Configure Status').click()
    expect(onConfigure).toHaveBeenCalledOnce()
  })
})
