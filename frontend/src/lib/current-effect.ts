export type CurrentEffect = 'helping' | 'hindering' | 'none'

/** Round to a tenth of a knot, the precision the tiles show. */
export const roundTenth = (value: number) => Math.round(value * 10) / 10

/**
 * Whether the current helps or holds back progress along the course, judged on
 * the impact as shown (to 0.1 kt), so a figure that reads 0.0 is 'none'.
 * Null when the impact is unknown.
 */
export function classifyCurrentEffect(impactKts: number | null): CurrentEffect | null {
  if (impactKts === null) return null
  const rounded = roundTenth(impactKts)
  if (rounded === 0) return 'none'
  return rounded > 0 ? 'helping' : 'hindering'
}

/** The colour class both tiles give a helping or hindering current; '' for neutral. */
export function currentEffectColorClass(effect: CurrentEffect | null): string {
  if (effect === 'helping') return 'text-gauge-secondary'
  if (effect === 'hindering') return 'text-amber-600'
  return ''
}
