import { describe, it, expect } from 'vitest'

import { buildConditionsSummary, type ConditionSummaryDay } from '@/lib/forecast-condition-summary'

function days(
  conditions: string[],
  options: { troughIndexes?: number[]; labels?: string[] } = {},
): ConditionSummaryDay[] {
  const names = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']
  const { troughIndexes = [], labels } = options
  return conditions.map((condition, idx) => ({
    condition,
    label: labels?.[idx] ?? `${names[idx % 7].slice(0, 3)} ${14 + idx}`,
    trough: troughIndexes.includes(idx),
  }))
}

describe('buildConditionsSummary', () => {
  it('renders nothing for an empty run', () => {
    expect(buildConditionsSummary([])).toBeNull()
  })

  it('names a single day directly, regardless of bucket', () => {
    expect(buildConditionsSummary(days(['Drizzle']))).toBe('Drizzle today.')
    expect(buildConditionsSummary(days(['Mostly Sunny']))).toBe('Mostly sunny today.')
  })

  // The spec's own worked example: two wet days, then eight fair days.
  it('matches the target two-run example', () => {
    expect(
      buildConditionsSummary(days(['Drizzle', 'Drizzle', ...Array(8).fill('Mostly Sunny')])),
    ).toBe('Drizzle expected the next 2 days.')
  })

  it('phrases a single run covering every day as "for all n days", wet and non-wet', () => {
    expect(buildConditionsSummary(days(Array(5).fill('Rain')))).toBe('Rain expected for all 5 days.')
    expect(buildConditionsSummary(days(Array(5).fill('Clear')))).toBe('Clear for all 5 days.')
  })

  it('phrases a non-wet first run without "expected"', () => {
    expect(buildConditionsSummary(days([...Array(3).fill('Clear'), 'Rain']))).toBe(
      'Clear the next 3 days.',
    )
  })

  it('phrases a wet first run with "expected"', () => {
    expect(buildConditionsSummary(days([...Array(3).fill('Rain'), 'Clear']))).toBe(
      'Rain expected the next 3 days.',
    )
  })

  it('names only the first run, however many follow', () => {
    expect(
      buildConditionsSummary(days([...Array(2).fill('Drizzle'), ...Array(3).fill('Clear'), 'Rain'])),
    ).toBe('Drizzle expected the next 2 days.')
    expect(buildConditionsSummary(days(['Drizzle', 'Clear', 'Clear', 'Rain', 'Cloudy']))).toBe('Drizzle today.')
  })

  // "Freezing Rain", "Mixed Rain & Snow" and "Heavy Snow" aren't in the
  // named condition lists at all - they only bucket as wet because they
  // contain "rain"/"snow", which is what this checks.
  it('buckets wet conditions by substring, including compounds not in the named lists', () => {
    expect(buildConditionsSummary(days(['Freezing Rain', 'Freezing Rain']))).toBe(
      'Freezing rain expected for all 2 days.',
    )
    expect(buildConditionsSummary(days(['Mixed Rain & Snow', 'Mixed Rain & Snow', 'Clear']))).toBe(
      'Mixed rain & snow expected the next 2 days.',
    )
    expect(buildConditionsSummary(days(['Heavy Snow', 'Heavy Snow', 'Clear']))).toBe(
      'Heavy snow expected the next 2 days.',
    )
  })

  it('buckets cloudy and fair conditions separately, both non-wet, with Partly Cloudy as fair', () => {
    expect(buildConditionsSummary(days(['Foggy', 'Partly Cloudy']))).toBe(
      'Foggy today.',
    )
  })

  it('falls back to the "other" bucket, treated as non-wet, for anything unrecognised', () => {
    expect(buildConditionsSummary(days(['Breezy', 'Breezy', 'Breezy']))).toBe('Breezy for all 3 days.')
    expect(buildConditionsSummary(days(['Clear', 'Windy']))).toBe('Clear today.')
    expect(buildConditionsSummary(days(['Unknown']))).toBe('Unknown today.')
  })

  it('labels a run by its most frequent condition, earliest breaking ties', () => {
    // Two "Cloudy" and two "Mostly Cloudy" in one cloudy run: tied at 2, and
    // "Cloudy" occurs first, so it wins the label.
    expect(
      buildConditionsSummary(days(['Cloudy', 'Mostly Cloudy', 'Cloudy', 'Mostly Cloudy', 'Clear'])),
    ).toBe('Cloudy the next 4 days.')
  })

  it('caps only the first letter of a multi-word label at the sentence start', () => {
    expect(buildConditionsSummary(days(['Mostly Sunny', 'Mostly Cloudy']))).toBe(
      'Mostly sunny today.',
    )
  })
})

describe('buildConditionsSummary with trough days', () => {
  it('mentions a trough in the first clause when it lands within a multi-day run', () => {
    const input = days(['Rain', 'Rain', 'Rain', 'Clear', 'Clear'], {
      troughIndexes: [1],
      labels: ['Sun 14', 'Mon 15', 'Tue 16', 'Wed 17', 'Thu 18'],
    })
    expect(buildConditionsSummary(input)).toBe(
      'Rain expected the next 3 days with a trough aloft on Mon 15.',
    )
  })

  it('does not mention a trough on a later run', () => {
    const input = days(['Clear', 'Clear', 'Clear', 'Rain', 'Rain'], {
      troughIndexes: [4],
      labels: ['Sun 14', 'Mon 15', 'Tue 16', 'Wed 17', 'Thu 18'],
    })
    expect(buildConditionsSummary(input)).toBe(
      'Clear the next 3 days.',
    )
  })

  it('joins two trough days in the first run with "and"', () => {
    const input = days(['Rain', 'Rain', 'Rain', 'Clear'], {
      troughIndexes: [0, 2],
      labels: ['Sun 14', 'Mon 15', 'Tue 16', 'Wed 17'],
    })
    expect(buildConditionsSummary(input)).toBe('Rain expected the next 3 days with troughs aloft on Sun 14 and Tue 16.')
  })

  it('drops the day label when the only trough day is today in a one-day first run', () => {
    const input = days(['Drizzle', 'Clear'], {
      troughIndexes: [0],
      labels: ['Sun 14', 'Mon 15'],
    })
    expect(buildConditionsSummary(input)).toBe('Drizzle today with a trough aloft.')
  })

  it('leaves out a trough in the later days', () => {
    const input = days(['Drizzle', 'Clear', 'Clear', 'Rain', 'Cloudy'], {
      troughIndexes: [4],
      labels: ['Sun 14', 'Mon 15', 'Tue 16', 'Wed 17', 'Thu 18'],
    })
    expect(buildConditionsSummary(input)).toBe('Drizzle today.')
  })

  it('appends a trough mention to a single run spanning the whole window', () => {
    const input = days(Array(10).fill('Mostly Sunny'), {
      troughIndexes: [6],
      labels: Array.from({ length: 10 }, (_, idx) => (idx === 6 ? 'Sat 20' : `Day ${idx}`)),
    })
    expect(buildConditionsSummary(input)).toBe('Mostly sunny for all 10 days with a trough aloft on Sat 20.')
  })
})
