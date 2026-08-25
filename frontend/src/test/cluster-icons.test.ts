import { describe, expect, test } from 'vitest'

import { CLUSTER_ICON_NAMES, iconForSlot } from '@/lib/cluster-icons'

describe('iconForSlot', () => {
  const slot = (path: string, quantity: string) => ({ path, quantity })

  test.each([
    ['propulsion.port.temperature', 'temperature', 'thermometer'],
    ['propulsion.port.oilPressure', 'pressure', 'cog'],
    ['propulsion.port.boostPressure', 'pressure', 'rabbit'],
    ['propulsion.port.runTime', 'duration', 'clock'],
    ['propulsion.port.fuel.rate', 'volumetricFlow', 'fuel'],
    ['propulsion.port.revolutions', 'frequency', 'gauge'],
  ])('%s -> %s', (path, quantity, expected) => {
    expect(iconForSlot(slot(path, quantity))).toBe(expected)
  })

  /**
   * `transmission.oilTemperature` contains both "oil" and "temperature". It is
   * a temperature, so the thermometer has to win — which means checking for
   * temperature before oil, not in whatever order reads nicely.
   */
  test('a name carrying two hints resolves to the measured quantity', () => {
    expect(iconForSlot(slot('propulsion.port.transmission.oilTemperature', 'temperature'))).toBe('thermometer')
    expect(iconForSlot(slot('propulsion.port.transmission.oilPressure', 'pressure'))).toBe('cog')
  })

  // Quantity is the fallback when the path says nothing recognisable.
  test('falls back to the quantity when the path gives no hint', () => {
    expect(iconForSlot(slot('some.vendor.xyz123', 'temperature'))).toBe('thermometer')
    expect(iconForSlot(slot('some.vendor.xyz123', 'duration'))).toBe('clock')
  })

  test('falls back to a neutral icon when nothing matches', () => {
    expect(iconForSlot(slot('some.vendor.xyz123', 'raw'))).toBe('gauge')
  })

  test('is case-insensitive about the path', () => {
    expect(iconForSlot(slot('PROPULSION.PORT.BOOSTPRESSURE', 'pressure'))).toBe('rabbit')
  })

  test('every inferred name is one the renderer knows', () => {
    const paths = [
      'a.temperature', 'a.oilPressure', 'a.boostPressure', 'a.runTime',
      'a.fuel.rate', 'a.revolutions', 'a.unknown',
    ]
    for (const path of paths) {
      expect(CLUSTER_ICON_NAMES).toContain(iconForSlot(slot(path, 'raw')))
    }
  })
})
