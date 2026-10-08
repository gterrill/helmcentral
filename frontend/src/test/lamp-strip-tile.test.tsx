import { render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { LampStripTile } from '@/components/lamp-strip-tile'
import type { LampStripWidgetConfig } from '@/lib/dashboard-widgets'
import type { ActiveAlarm } from '@/hooks/use-alarms'

function alarm(path: string, state: ActiveAlarm['state'], label = 'Some Alarm'): ActiveAlarm {
  return { rule_id: `r-${path}-${state}`, label, path, phase: 'active', state, value: 1, message: '', silenced: false, can_silence: false, can_acknowledge: false }
}

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

function renderStrip(config = ribbon, worstAlarmState = 'normal', onOpenAlarms = vi.fn(), ages?: Record<string, number | null>, alarms: ActiveAlarm[] = []) {
  render(
    <LampStripTile
      config={config}
      values={values}
      ages={ages}
      worstAlarmState={worstAlarmState}
      alarms={alarms}
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

  test('an "on" cell is the healthy green, not Signal Blue', () => {
    renderStrip()
    const cell = screen.getByLabelText('GEN: on')
    expect(cell.className).toContain('emerald')
    expect(cell.className).not.toContain('primary')
  })

  test('cell text: name never wraps, reading says On / Off / --', () => {
    renderStrip()
    const name = screen.getByText('GEN')
    expect(name.className).toContain('whitespace-nowrap')
    expect(screen.getByLabelText('GEN: on')).toHaveTextContent('On')
    expect(screen.getByLabelText('WING: off')).toHaveTextContent('Off')
    expect(screen.getByLabelText('INV: no data')).toHaveTextContent('--')
  })

  test('no data is a dashed outline, distinct from off', () => {
    renderStrip()
    expect(screen.getByLabelText('INV: no data').className).toContain('border-dashed')
    expect(screen.getByLabelText('WING: off').className).not.toContain('border-dashed')
  })

  test('every lamp cell has the same fixed width', () => {
    renderStrip()
    for (const l of ['GEN: on', 'WING: off', 'INV: no data']) {
      expect(screen.getByLabelText(l).className).toContain('w-32')
    }
  })

  test('an active alarm on the lamp path turns the cell red even when the value is 0', () => {
    renderStrip(ribbon, 'alarm', vi.fn(), undefined, [alarm('propulsion.wing.revolutions', 'alarm')])
    const cell = screen.getByLabelText('WING: alarm')
    expect(cell).toHaveAttribute('data-state', 'alarm')
    expect(cell.className).toContain('red')
  })

  test('a warn alarm is amber and the worst alarm on the path wins', () => {
    renderStrip(ribbon, 'alarm', vi.fn(), undefined, [
      alarm('electrical.generator.state', 'warn'),
      alarm('electrical.generator.state', 'alarm'),
      alarm('electrical.inverter.state', 'warn'),
    ])
    expect(screen.getByLabelText('GEN: alarm')).toHaveAttribute('data-state', 'alarm')
    const inv = screen.getByLabelText('INV: warn')
    expect(inv.className).toContain('amber')
  })

  test('a lamp with an alarm opens the alarms drawer; one without does nothing', () => {
    const onOpenAlarms = renderStrip(ribbon, 'alarm', vi.fn(), undefined, [alarm('propulsion.wing.revolutions', 'alarm')])
    screen.getByLabelText('GEN: on').click()
    expect(onOpenAlarms).not.toHaveBeenCalled()
    screen.getByLabelText('WING: alarm').click()
    expect(onOpenAlarms).toHaveBeenCalledOnce()
  })

  test('an alarm raised from the network (notifications. prefix) colours the lamp bound to the bare path', () => {
    renderStrip(
      { title: 'S', lamps: [{ path: 'bilge.aft.pump.state', label: 'Aft Bilge' }], showCheck: false },
      'alarm', vi.fn(), undefined, [alarm('notifications.bilge.aft.pump.state', 'alarm')],
    )
    const cell = screen.getByLabelText('Aft Bilge: alarm')
    expect(cell.className).toContain('red')
  })

  test('a blank-label lamp with a long path segment cannot overflow its cell', () => {
    renderStrip({ title: 'S', lamps: [{ path: 'electrical.alternator.0.chargingModeNumber', label: '' }], showCheck: false })
    const name = screen.getByText('chargingModeNumber')
    expect(name.className).toContain('truncate')
    expect(name).toHaveAttribute('title', 'chargingModeNumber')
    expect(name.parentElement!.className).toContain('min-w-0')
  })

  test('the lit CHK cell truncates inside the tile and carries the full text as a title', () => {
    renderStrip(ribbon, 'alarm', vi.fn(), undefined, [alarm('x', 'alarm', 'A Very Long Alarm Title Indeed')])
    const chk = screen.getByLabelText('CHK: alarm')
    expect(chk.className).toContain('min-w-0')
    expect(chk.className).toContain('max-w-full')
    const text = screen.getByText('CHK · 1 · A Very Long Alarm Title Indeed')
    expect(text.className).toContain('truncate')
    expect(text).toHaveAttribute('title', 'CHK · 1 · A Very Long Alarm Title Indeed')
  })

  describe('groups', () => {
    const grouped: LampStripWidgetConfig = {
      title: 'Status',
      lamps: [
        { path: 'a', label: 'A', group: 'Propulsion' },
        { path: 'b', label: 'B', group: 'Propulsion' },
        { path: 'c', label: 'C', group: 'Power' },
        { path: 'd', label: 'D' },
      ],
      showCheck: false,
    }
    test('one heading per consecutive run, none for ungrouped lamps', () => {
      renderStrip(grouped)
      expect(screen.getAllByText('Propulsion')).toHaveLength(1)
      expect(screen.getAllByText('Power')).toHaveLength(1)
      expect(screen.getAllByTestId('lamp-group')).toHaveLength(3)
    })
    test('a repeated group after a break gets its own heading', () => {
      renderStrip({ ...grouped, lamps: [grouped.lamps[0], grouped.lamps[2], { path: 'e', label: 'E', group: 'Propulsion' }] })
      expect(screen.getAllByText('Propulsion')).toHaveLength(2)
    })
  })

  test('invert lights a lamp whose healthy state is off', () => {
    renderStrip({
      ...ribbon,
      lamps: [{ path: 'propulsion.wing.revolutions', label: 'WING', invert: true }],
      showCheck: false,
    })
    expect(screen.getByLabelText('WING: on')).toBeInTheDocument()
  })

  test('an inverted lamp reads what the signal does, not whether it is healthy', () => {
    renderStrip({
      ...ribbon,
      lamps: [{ path: 'propulsion.wing.revolutions', label: 'WING', invert: true }, { path: 'electrical.generator.state', label: 'GEN', invert: true }],
      showCheck: false,
    })
    // Signal 0: healthy (lit) but the reading is Off.
    const wing = screen.getByLabelText('WING: on')
    expect(wing).toHaveTextContent('Off')
    // Signal 1: not healthy (unlit) and the reading is On.
    const gen = screen.getByLabelText('GEN: off')
    expect(gen).toHaveTextContent('On')
  })

  test('the cell row scrolls vertically so a short tile never hides a lamp', () => {
    renderStrip()
    expect(screen.getByTestId('lamp-strip-row').className).toContain('overflow-y-auto')
  })

  test('the check lamp takes its colour from the worst active alarm', () => {
    const { unmount } = render(
      <LampStripTile config={ribbon} values={values} worstAlarmState="normal" editing={false} onConfigure={vi.fn()} onOpenAlarms={vi.fn()} />,
    )
    expect(screen.getByLabelText(/^CHK/).className).not.toContain('red')
    expect(screen.getByLabelText(/^CHK/)).toHaveTextContent(/^CHK$/)
    unmount()

    renderStrip(ribbon, 'alarm', vi.fn(), undefined, [alarm('x', 'warn', 'Low Fuel'), alarm('y', 'alarm', 'Aft Bilge')])
    const chk = screen.getByLabelText('CHK: alarm')
    expect(chk).toHaveTextContent('CHK · 2 · Aft Bilge')
    expect(chk.className).toContain('red')
  })

  test('the check lamp opens the alarms drawer', () => {
    const onOpenAlarms = renderStrip(ribbon, 'warn', vi.fn(), undefined, [alarm('x', 'warn', 'Low Fuel')])
    screen.getByLabelText('CHK: warn').click()
    expect(onOpenAlarms).toHaveBeenCalledOnce()
  })

  test('omits the check lamp when it is not wanted', () => {
    renderStrip({ ...ribbon, showCheck: false })
    expect(screen.queryByLabelText(/^CHK/)).not.toBeInTheDocument()
  })

  // The ribbon keeps one cell shape and wraps rather than scrolling or widening the tile.
  test('wraps rather than stretching the tile', () => {
    renderStrip()
    const row = screen.getByTestId('lamp-strip-row')
    expect(row.className).toContain('flex-wrap')
    expect(row.className).toContain('min-w-0')
    expect(row.className).not.toContain('overflow-x-auto')
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
      expect(lamp.className).not.toContain('emerald')
      expect(lamp.className).toContain('border-dashed')
      expect(lamp).toHaveTextContent('--')
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
      renderStrip(ribbon, 'warn', vi.fn(), { 'electrical.generator.state': 300 }, [alarm('x', 'warn', 'Low Fuel')])
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
