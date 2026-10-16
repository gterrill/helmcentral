import { describe, expect, test } from 'vitest'

import { bulletFor, chargeStageLabel } from '@/lib/engine-cluster-bullet'
import type { GaugeWidgetConfig } from '@/lib/dashboard-widgets'

const PATH = 'propulsion.port.oilPressure'
const PSI = 6894.757

function oil(zones: GaugeWidgetConfig['zones']): GaugeWidgetConfig {
  return { path: PATH, label: 'Oil', display: 'numeric', quantity: 'pressure', unit: 'psi', min: 0, max: 100, zones }
}

const at = (psi: number) => ({ [PATH]: psi * PSI })

describe('bullet graph from a slot\'s zones', () => {
  test('a profile that ships only the normal band draws that band and no setpoint ticks', () => {
    const model = bulletFor(oil([{ from: 40, to: 100, state: 'normal' }]), at(60))
    expect(model.bands).toEqual([{ left: 40, width: 60, state: 'normal', active: true }])
    expect(model.marks).toEqual([])
  })

  test('the band the reading is inside is active; the others dim', () => {
    const model = bulletFor(oil([
      { from: 0, to: 15, state: 'alarm' },
      { from: 15, to: 25, state: 'warn' },
      { from: 25, to: 100, state: 'normal' },
    ]), at(20))
    expect(model.bands.map((b) => [b.state, b.active])).toEqual([['alarm', false], ['warn', true], ['normal', false]])
    expect(model.tone).toBe('warn')
  })

  test('a reading outside the only band is amber, never red', () => {
    const model = bulletFor(oil([{ from: 40, to: 100, state: 'normal' }]), at(22))
    expect(model.zone).toBe('outside')
    expect(model.tone).toBe('warn')
    expect(model.tone).not.toBe('alarm')
  })

  test('a reading inside the normal band stays neutral', () => {
    expect(bulletFor(oil([{ from: 40, to: 100, state: 'normal' }]), at(60)).tone).toBe('neutral')
  })

  test('with no zones at all there is nothing to colour', () => {
    const model = bulletFor(oil(undefined), at(22))
    expect(model.bands).toEqual([])
    expect(model.tone).toBe('neutral')
  })

  test('setpoint ticks mark the threshold each end-anchored band starts at, labelled', () => {
    const model = bulletFor(oil([
      { from: 0, to: 15, state: 'alarm' },
      { from: 15, to: 25, state: 'warn' },
      { from: 25, to: 100, state: 'normal' },
    ]), at(60))
    expect(model.marks.map((m) => [m.left, m.state, m.label])).toEqual([[15, 'alarm', '15']])
  })

  test('an above-zone (raw-water loss on a wet exhaust) marks its lower edge', () => {
    const egt: GaugeWidgetConfig = {
      path: 'propulsion.0.exhaustTemperature', label: 'EGT', display: 'numeric', quantity: 'temperature', unit: 'C',
      min: 0, max: 100, zones: [{ from: 0, to: 60, state: 'normal' }, { from: 60, to: 100, state: 'alarm' }],
    }
    const model = bulletFor(egt, { 'propulsion.0.exhaustTemperature': 80 + 273.15 })
    expect(model.marks).toEqual([{ left: 60, state: 'alarm', label: '60', labelSide: 'after' }])
    expect(model.tone).toBe('alarm')
  })

  test('no reading means no position and a neutral tone', () => {
    const model = bulletFor(oil([{ from: 40, to: 100, state: 'normal' }]), {})
    expect(model.position).toBeNull()
    expect(model.tone).toBe('neutral')
    expect(model.text).toBeNull()
  })

  test('a stale reading is treated as no reading', () => {
    const model = bulletFor(oil([{ from: 40, to: 100, state: 'normal' }]), at(60), { [PATH]: 9000 })
    expect(model.position).toBeNull()
    expect(model.stale).toBe(true)
  })
})

describe('charge stage', () => {
  test('names the stage the regulator reports', () => {
    expect(chargeStageLabel(4)).toBe('ABSORPTION')
    expect(chargeStageLabel(5)).toBe('FLOAT')
  })
  test('shows an unknown number as itself rather than guessing a stage', () => {
    expect(chargeStageLabel(77)).toBe('STATE 77')
  })
  test('has no stage with no reading', () => {
    expect(chargeStageLabel(null)).toBeNull()
  })
})
