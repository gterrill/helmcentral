import { describe, expect, test } from 'vitest'

import type { SignalKPath } from '@/hooks/use-signalk-paths'
import { LAMP_STRIP_MAX_LAMPS } from '@/lib/dashboard-widgets'
import { suggestRibbonLamps } from '@/lib/ribbon-defaults'

import fixture from './fixtures/signalk-paths-2026-09-08.json'

describe('suggestRibbonLamps', () => {
  test('resolves the live vessel fixture to the expected default set, in order', () => {
    const paths = (fixture as { paths: SignalKPath[] }).paths
    expect(suggestRibbonLamps(paths)).toEqual([
      { path: 'propulsion.port.revolutions', label: 'Port' },
      { path: 'propulsion.starboard.revolutions', label: 'Stbd' },
      { path: 'electrical.generator.0.stateNumber', label: 'Gen' },
      { path: 'electrical.inverters.276.acState.acIn1Available', label: 'Shore' },
      { path: 'electrical.alternator.0.chargingModeNumber', label: 'Alt 0' },
      { path: 'electrical.alternator.1.chargingModeNumber', label: 'Alt 1' },
    ])
  })

  test('an empty path list yields nothing', () => {
    expect(suggestRibbonLamps([])).toEqual([])
  })

  test('a single-engine boat with only propulsion.0.revolutions yields "Eng 0"', () => {
    const paths: SignalKPath[] = [{ path: 'propulsion.0.revolutions', value: 1200 }]
    expect(suggestRibbonLamps(paths)).toEqual([{ path: 'propulsion.0.revolutions', label: 'Eng 0' }])
  })

  test('a path list with 20 engines is capped at 16', () => {
    const paths: SignalKPath[] = Array.from({ length: 20 }, (_, i) => ({
      path: `propulsion.${i}.revolutions`,
      value: 1000 + i,
    }))
    const result = suggestRibbonLamps(paths)
    expect(result).toHaveLength(LAMP_STRIP_MAX_LAMPS)
  })

  test('does not suggest an inverting-mode, anchor, autopilot or bilge lamp absent from the fixture', () => {
    const paths = (fixture as { paths: SignalKPath[] }).paths
    const suggested = suggestRibbonLamps(paths)
    expect(suggested.some((lamp) => lamp.path.includes('inverterMode'))).toBe(false)
    expect(suggested.some((lamp) => lamp.path.includes('anchor'))).toBe(false)
    expect(suggested.some((lamp) => lamp.path.includes('autopilot'))).toBe(false)
    expect(suggested.some((lamp) => lamp.path.includes('switches.bank'))).toBe(false)
  })

  test('no suggestion carries invert', () => {
    const paths = (fixture as { paths: SignalKPath[] }).paths
    expect(suggestRibbonLamps(paths).every((lamp) => lamp.invert === undefined)).toBe(true)
  })
})
