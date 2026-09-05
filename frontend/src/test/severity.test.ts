import { describe, expect, it } from 'vitest'

import { severityClass } from '@/lib/severity'

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
