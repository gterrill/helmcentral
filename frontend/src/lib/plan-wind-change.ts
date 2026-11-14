import { UNSURE, windChangeChipText } from '@/lib/forecast-wind-changes'
import { formatWindRange, type PlanWindChange } from '@/lib/plans'

// The plan table's Wind column words the changes the assessment sends (ADR 0287).
// The server decides which changes fall in a row, where the wind before and after
// blows from against the leg's course, and which are amber. This only words them.

/** "NNW→SE 7 AM" and, on a leg, "astern → ahead" (or "stays ahead"). */
export function planWindChangeLine(change: PlanWindChange): { text: string; relation: string | null } {
  const text = windChangeChipText(change)
  if (change.sector === null || change.fromSector === null) return { text, relation: null }
  const relation = change.fromSector === change.sector ? `stays ${change.sector}` : `${change.fromSector} → ${change.sector}`
  return { text, relation }
}

/** Every change on its own line, with the wind after it and how sure the forecast is. */
export function planWindChangesTitle(changes: PlanWindChange[]): string {
  return changes
    .map((c) => {
      const { text, relation } = planWindChangeLine(c)
      return `${text}${relation ? `, ${relation}` : ''}, ${formatWindRange(c.speedMinKn, c.speedMaxKn)} after${c.lowerConfidence ? ` (${UNSURE})` : ''}`
    })
    .join('\n')
}
