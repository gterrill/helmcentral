import { describe, expect, it } from 'vitest'

import { severityClass, severityFill, severityTextClass } from '@/lib/severity'

describe('severityClass', () => {
  it('gives each of the four live states a distinct treatment', () => {
    const classes = ['alert', 'warn', 'alarm', 'emergency'].map(severityClass)
    expect(new Set(classes).size).toBe(4)
  })

  // warn (amber-500) and alert (amber-400) used to be a one-shade near-miss
  // pair (Bujack et al. 2022): indistinguishable at arm's length. They now
  // sit on different hues entirely.
  it('does not put alert and warn on the same hue', () => {
    const alert = severityClass('alert')
    const warn = severityClass('warn')
    const hueOf = (cls: string) => cls.match(/-(sky|amber|red)-/)?.[1]
    expect(hueOf(alert)).toBeDefined()
    expect(hueOf(warn)).toBeDefined()
    expect(hueOf(alert)).not.toBe(hueOf(warn))
  })

  it('falls back to muted-foreground for an unknown state', () => {
    expect(severityClass('normal')).toBe('text-muted-foreground')
  })
})

/**
 * The gauge-zone ladder (dial-ring, gauge-tile, lamp-strip, engine-cluster,
 * cluster-readings). Same near-miss problem as severityClass, one shared
 * source instead of five hand-kept HSL/class switches.
 */
describe('severityFill', () => {
  it('gives each of the four live states a distinct fill', () => {
    const fills = ['alert', 'warn', 'alarm', 'emergency'].map(severityFill)
    expect(new Set(fills).size).toBe(4)
  })

  // alert used to share amber's hue with warn (a one-shade near-miss). It now
  // sits on a blue hue, clear of the 0-120 range amber/red occupy.
  it('keeps alert off the amber/red hue range', () => {
    const hue = Number(severityFill('alert').match(/hsl\((\d+)/)?.[1])
    expect(hue).toBeGreaterThanOrEqual(180)
  })
})

describe('severityTextClass', () => {
  it('gives each of the four live states a distinct text class', () => {
    const classes = ['alert', 'warn', 'alarm', 'emergency'].map((s) => severityTextClass(s, 'text-muted-foreground'))
    expect(new Set(classes).size).toBe(4)
  })

  // An out-of-range reading with no matching band is a warning-grade
  // condition, not a confirmed alarm, so it reads as warn's colour.
  it('maps outside to warn\'s class', () => {
    expect(severityTextClass('outside', 'text-muted-foreground')).toBe(severityTextClass('warn', 'text-muted-foreground'))
  })

  it('returns the caller\'s fallback for normal and for null', () => {
    expect(severityTextClass('normal', 'text-gauge-primary')).toBe('text-gauge-primary')
    expect(severityTextClass(null, 'text-gauge-primary')).toBe('text-gauge-primary')
  })
})
