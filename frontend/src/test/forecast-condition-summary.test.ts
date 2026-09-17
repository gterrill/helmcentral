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
    dayName: names[idx % 7],
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
    ).toBe('Drizzle expected the next 2 days, followed by 8 mostly sunny days.')
  })

  it('phrases a single run covering every day as "for all n days", wet and non-wet', () => {
    expect(buildConditionsSummary(days(Array(5).fill('Rain')))).toBe('Rain expected for all 5 days.')
    expect(buildConditionsSummary(days(Array(5).fill('Clear')))).toBe('Clear for all 5 days.')
  })

  it('phrases a non-wet first run without "expected"', () => {
    expect(buildConditionsSummary(days([...Array(3).fill('Clear'), 'Rain']))).toBe(
      'Clear the next 3 days, followed by a day of rain.',
    )
  })

  it('phrases a wet first run with "expected"', () => {
    expect(buildConditionsSummary(days([...Array(3).fill('Rain'), 'Clear']))).toBe(
      'Rain expected the next 3 days, followed by a clear day.',
    )
  })

  it('appends a third clause the same way as the second, when there are exactly three runs', () => {
    expect(
      buildConditionsSummary(days([...Array(2).fill('Drizzle'), ...Array(3).fill('Clear'), 'Rain'])),
    ).toBe('Drizzle expected the next 2 days, followed by 3 clear days, then a day of rain.')
  })

  it('collapses a fourth run onto "changeable through" the last day, naming its dayName', () => {
    const run = days(['Drizzle', 'Clear', 'Clear', 'Rain', 'Cloudy'])
    // dayNames follow Sunday..Saturday by index: the fifth day (idx 4) is Thursday.
    expect(buildConditionsSummary(run)).toBe(
      'Drizzle today, followed by 2 clear days, then changeable through Thursday.',
    )
  })

  // "Freezing Rain", "Mixed Rain & Snow" and "Heavy Snow" aren't in the
  // named condition lists at all - they only bucket as wet because they
  // contain "rain"/"snow", which is what this checks.
  it('buckets wet conditions by substring, including compounds not in the named lists', () => {
    expect(buildConditionsSummary(days(['Freezing Rain', 'Freezing Rain']))).toBe(
      'Freezing rain expected for all 2 days.',
    )
    expect(buildConditionsSummary(days(['Clear', 'Mixed Rain & Snow']))).toBe(
      'Clear today, followed by a day of mixed rain & snow.',
    )
    expect(buildConditionsSummary(days(['Clear', 'Heavy Snow']))).toBe(
      'Clear today, followed by a day of heavy snow.',
    )
  })

  it('buckets cloudy and fair conditions separately, both non-wet, with Partly Cloudy as fair', () => {
    expect(buildConditionsSummary(days(['Foggy', 'Partly Cloudy']))).toBe(
      'Foggy today, followed by a partly cloudy day.',
    )
  })

  it('falls back to the "other" bucket, treated as non-wet, for anything unrecognised', () => {
    expect(buildConditionsSummary(days(['Breezy', 'Breezy', 'Breezy']))).toBe('Breezy for all 3 days.')
    expect(buildConditionsSummary(days(['Clear', 'Windy']))).toBe('Clear today, followed by a windy day.')
    expect(buildConditionsSummary(days(['Unknown']))).toBe('Unknown today.')
  })

  it('labels a run by its most frequent condition, earliest breaking ties', () => {
    // Two "Cloudy" and two "Mostly Cloudy" in one cloudy run: tied at 2, and
    // "Cloudy" occurs first, so it wins the label.
    expect(
      buildConditionsSummary(days(['Cloudy', 'Mostly Cloudy', 'Cloudy', 'Mostly Cloudy', 'Clear'])),
    ).toBe('Cloudy the next 4 days, followed by a clear day.')
  })

  it('lowercases a multi-word label mid-sentence but only caps the first letter at the sentence start', () => {
    // Mostly Sunny (fair) and Mostly Cloudy (cloudy) are different buckets,
    // so this is two runs - one at the sentence start, one mid-sentence.
    expect(buildConditionsSummary(days(['Mostly Sunny', 'Mostly Cloudy']))).toBe(
      'Mostly sunny today, followed by a mostly cloudy day.',
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
      'Rain expected the next 3 days with a trough aloft on Mon 15, followed by 2 clear days.',
    )
  })

  it('mentions a trough in the second ("followed by") run, not the first', () => {
    const input = days(['Clear', 'Clear', 'Clear', 'Rain', 'Rain'], {
      troughIndexes: [4],
      labels: ['Sun 14', 'Mon 15', 'Tue 16', 'Wed 17', 'Thu 18'],
    })
    expect(buildConditionsSummary(input)).toBe(
      'Clear the next 3 days, followed by 2 days of rain with a trough aloft on Thu 18.',
    )
  })

  it('joins two trough days in the same clause with "and"', () => {
    const input = days(['Clear', 'Rain', 'Rain', 'Rain'], {
      troughIndexes: [1, 3],
      labels: ['Sun 14', 'Mon 15', 'Tue 16', 'Wed 17'],
    })
    expect(buildConditionsSummary(input)).toBe(
      'Clear today, followed by 3 days of rain with troughs aloft on Mon 15 and Wed 17.',
    )
  })

  it('drops the day label when the only trough day is today in a one-day first run', () => {
    const input = days(['Drizzle', 'Clear'], {
      troughIndexes: [0],
      labels: ['Sun 14', 'Mon 15'],
    })
    expect(buildConditionsSummary(input)).toBe('Drizzle today with a trough aloft, followed by a clear day.')
  })

  it('mentions a trough inside the collapsed "changeable" tail', () => {
    const input = days(['Drizzle', 'Clear', 'Clear', 'Rain', 'Cloudy'], {
      troughIndexes: [4],
      labels: ['Sun 14', 'Mon 15', 'Tue 16', 'Wed 17', 'Thu 18'],
    })
    // dayNames follow Sunday..Saturday by index: the fifth day (idx 4) is Thursday.
    expect(buildConditionsSummary(input)).toBe(
      'Drizzle today, followed by 2 clear days, then changeable through Thursday with a trough aloft on Thu 18.',
    )
  })

  it('appends a trough mention to a single run spanning the whole window', () => {
    const input = days(Array(10).fill('Mostly Sunny'), {
      troughIndexes: [6],
      labels: Array.from({ length: 10 }, (_, idx) => (idx === 6 ? 'Sat 20' : `Day ${idx}`)),
    })
    expect(buildConditionsSummary(input)).toBe('Mostly sunny for all 10 days with a trough aloft on Sat 20.')
  })
})
