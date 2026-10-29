// The Watches page's schedule picker (ADR 0227). A watch stores a five-field
// cron string on the boat's clock. The operator picks a preset; each preset
// writes the cron the server's own presets write, and an existing cron is read
// back into the preset that writes it, or Custom when none does. The words and
// the next run times always come from the server, never from here.

export const EVERY_HOURS_CHOICES = [2, 3, 4, 6, 12] as const

export type SchedulePreset =
  | { kind: 'hourly' }
  | { kind: 'every'; hours: number }
  | { kind: 'daily'; hour: number; minute: number }
  | { kind: 'weekdays'; hour: number; minute: number; days: number[] }
  | { kind: 'window'; from: number; to: number }
  | { kind: 'custom'; cron: string }

export type SchedulePresetKind = SchedulePreset['kind']

export function cronOf(preset: SchedulePreset): string {
  switch (preset.kind) {
    case 'hourly':
      return '0 * * * *'
    case 'every':
      return `0 */${preset.hours} * * *`
    case 'daily':
      return `${preset.minute} ${preset.hour} * * *`
    case 'weekdays': {
      const days = [...new Set(preset.days)].sort((a, b) => a - b)
      return `${preset.minute} ${preset.hour} * * ${days.join(',')}`
    }
    case 'window':
      return preset.from <= preset.to
        ? `0 ${preset.from}-${preset.to} * * *`
        : `0 ${preset.from}-23,0-${preset.to} * * *`
    case 'custom':
      return preset.cron
  }
}

const NUMBER = /^\d{1,2}$/

function int(text: string): number | null {
  return NUMBER.test(text) ? Number(text) : null
}

/** The preset that writes `cron`, or Custom when no preset does. */
export function parseCron(cron: string): SchedulePreset {
  const custom: SchedulePreset = { kind: 'custom', cron }
  const fields = cron.trim().split(/\s+/)
  if (fields.length !== 5) return custom
  const [min, hour, dom, month, dow] = fields
  if (dom !== '*' || month !== '*') return custom
  const normalised = fields.join(' ')

  const matches = (preset: SchedulePreset): SchedulePreset =>
    cronOf(preset) === normalised ? preset : custom

  const minute = int(min)
  if (minute === null || minute > 59) return custom

  if (minute === 0 && hour === '*' && dow === '*') return { kind: 'hourly' }

  const every = /^\*\/(\d{1,2})$/.exec(hour)
  if (every && minute === 0 && dow === '*') {
    const hours = Number(every[1])
    return EVERY_HOURS_CHOICES.includes(hours as (typeof EVERY_HOURS_CHOICES)[number]) ? matches({ kind: 'every', hours }) : custom
  }

  const h = int(hour)
  if (h !== null && h <= 23) {
    if (dow === '*') return matches({ kind: 'daily', hour: h, minute })
    const days = dow.split(',').map(int)
    if (days.every((d): d is number => d !== null && d <= 6)) {
      return matches({ kind: 'weekdays', hour: h, minute, days })
    }
    return custom
  }

  if (minute === 0 && dow === '*') {
    const plain = /^(\d{1,2})-(\d{1,2})$/.exec(hour)
    if (plain) return matches({ kind: 'window', from: Number(plain[1]), to: Number(plain[2]) })
    const wrapped = /^(\d{1,2})-23,0-(\d{1,2})$/.exec(hour)
    if (wrapped) return matches({ kind: 'window', from: Number(wrapped[1]), to: Number(wrapped[2]) })
  }
  return custom
}
