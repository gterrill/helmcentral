// When a selection of passages can be merged into one with a join at each
// boundary. The server makes the same checks and has the last word; this says
// why the button is off before it is pressed.

import type { PassageSummary } from '@/lib/passages'

type Mergeable = Pick<PassageSummary, 'id' | 'start' | 'end' | 'cruise_id' | 'hidden'>

/** Why the selected passages cannot be merged, or null when they can. `all` is
 * every passage the operator can see, for finding one in between. */
export function mergeBlocker(selected: readonly Mergeable[], all: readonly Mergeable[]): string | null {
  if (selected.length < 2) return 'Select two or more passages to merge.'
  if (selected.some((p) => p.hidden)) return 'A hidden passage cannot be merged.'
  const legs = [...selected].sort((a, b) => Date.parse(a.start) - Date.parse(b.start))
  const ids = new Set(legs.map((p) => p.id))
  let cruise = legs[0].cruise_id
  for (let i = 1; i < legs.length; i++) {
    const a = legs[i - 1]
    const b = legs[i]
    if (cruise !== null && b.cruise_id !== null && cruise !== b.cruise_id) return 'These passages are in different cruises.'
    if (cruise === null) cruise = b.cruise_id
    const between = all.some((p) => !ids.has(p.id) && Date.parse(p.start) < Date.parse(b.start) && Date.parse(p.end) > Date.parse(a.end))
    if (between) return 'These passages are not next to each other. Another passage falls between them.'
  }
  return null
}
