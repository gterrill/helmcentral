import { describe, expect, it } from 'vitest'

import { cronOf, parseCron, type SchedulePreset } from '@/lib/watch-schedule'

// The Watches page writes cron and reads it back (ADR 0227). The presets must
// write exactly the strings the server's own presets write, and parseCron must
// recognise those strings and nothing else as a preset.

const cases: Array<[SchedulePreset, string]> = [
  [{ kind: 'hourly' }, '0 * * * *'],
  [{ kind: 'every', hours: 2 }, '0 */2 * * *'],
  [{ kind: 'every', hours: 12 }, '0 */12 * * *'],
  [{ kind: 'daily', hour: 6, minute: 0 }, '0 6 * * *'],
  [{ kind: 'daily', hour: 7, minute: 30 }, '30 7 * * *'],
  [{ kind: 'weekdays', hour: 7, minute: 30, days: [1, 3, 5] }, '30 7 * * 1,3,5'],
  [{ kind: 'window', from: 22, to: 6 }, '0 22-23,0-6 * * *'],
  [{ kind: 'window', from: 9, to: 17 }, '0 9-17 * * *'],
  [{ kind: 'custom', cron: '*/20 * * * *' }, '*/20 * * * *'],
]

describe('cronOf', () => {
  it.each(cases)('writes %j as %s', (preset, cron) => {
    expect(cronOf(preset)).toBe(cron)
  })

  it('sorts the chosen days and drops repeats', () => {
    expect(cronOf({ kind: 'weekdays', hour: 8, minute: 0, days: [5, 1, 1, 3] })).toBe('0 8 * * 1,3,5')
  })

  it('writes no days as an empty field the server refuses, not as every day', () => {
    expect(cronOf({ kind: 'weekdays', hour: 8, minute: 0, days: [] })).toBe('0 8 * * ')
  })
})

describe('parseCron', () => {
  it.each(cases.filter(([p]) => p.kind !== 'custom'))('reads %s back as %j', (preset, cron) => {
    expect(parseCron(cron)).toEqual(preset)
  })

  it('reads a hand-typed cron the presets cannot write as Custom', () => {
    expect(parseCron('0 */5 * * *')).toEqual({ kind: 'custom', cron: '0 */5 * * *' })
    expect(parseCron('0 8 * * 1-5')).toEqual({ kind: 'custom', cron: '0 8 * * 1-5' })
    expect(parseCron('15 * * * *')).toEqual({ kind: 'custom', cron: '15 * * * *' })
  })

  it('does not read a window whose hours the presets would write differently', () => {
    expect(parseCron('0 22-23,0-5,7 * * *')).toEqual({ kind: 'custom', cron: '0 22-23,0-5,7 * * *' })
  })

  it('ignores extra spaces', () => {
    expect(parseCron('  0   6  * * * ')).toEqual({ kind: 'daily', hour: 6, minute: 0 })
  })
})
