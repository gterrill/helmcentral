import type { PassageMeasure } from '@/lib/mate-cards'

/**
 * The words for a measure a passage check could not make, shared by the Mate
 * passage card and the passage plan table so the two never name one thing
 * differently.
 */
export const MEASURE_WORDS: Record<PassageMeasure, string> = {
  // Only on cards saved by v0.49.0 chats; marginal is worked out now and never listed.
  wind_marginal: 'marginal wind limit',
  gust: 'gusts',
  wave: 'wave height',
  wave_direction: 'wave direction',
  wind_against_tide: 'wind against tide',
  daylight: 'daylight arrival',
}

export const MEASURE_ORDER: PassageMeasure[] = ['wind_marginal', 'gust', 'wave', 'wave_direction', 'wind_against_tide', 'daylight']

/**
 * The words for the measures named, in the order the cards list them, each once.
 * A name that is not a measure is shown as sent, never dropped.
 */
export function notCheckedWords(measures: Iterable<string>): string[] {
  const named = new Set(measures)
  const known = MEASURE_ORDER.filter((m) => named.has(m)).map((m) => MEASURE_WORDS[m])
  const unknown = [...named].filter((m) => !(m in MEASURE_WORDS))
  return [...known, ...unknown]
}
