// The lower-third that names each passage of a cruise as it starts: "Day 3 ·
// Funnel Bay \u2192 Thomas Island". Shown only in a replay of several passages,
// for a couple of seconds from the moment the boat sets sail on each, and it
// holds nothing up. Everything here is pure, so the live replay draws it with
// HTML and a video draws the same with a canvas.

import { localDaysSince } from '@/lib/boat-nights'
import { mapLabelBox, type MapLabelBox, type MapLabelOptions } from '@/lib/map-label'
import type { InsetCorner } from '@/lib/passage-inset-view'
import { isMultiPassage, type Timeline } from '@/lib/passages'
import { boatOffsetHours, type StayPassage } from '@/lib/replay-stay'

/** Seconds a passage's label is up. */
export const PASSAGE_LABEL_S = 2

export interface ReplayPassageLabel {
  /** The passage (timeline entry) it names. */
  id: string
  text: string
}

/** What a frame says of the passage label under way. */
export interface PassageLabelView {
  label: ReplayPassageLabel
  /** Seconds since the passage set sail. */
  elapsed: number
}

/** The words: the day and the route, with whichever end has no name left out ("Day 3 \u00b7 from Funnel Bay"), or the day alone. */
export function passageLabelText(day: number, from: string, to: string): string {
  const a = from.trim()
  const b = to.trim()
  if (a && b) return `Day ${day} \u00b7 ${a} \u2192 ${b}`
  if (a) return `Day ${day} \u00b7 from ${a}`
  if (b) return `Day ${day} \u00b7 to ${b}`
  return `Day ${day}`
}

/** The labels of a replay of several passages, one for each; none for a single passage. Days are counted by the boat's own dates from the first departure, as the cruise page counts its nights; without a known zone there are none. */
export function passageLabelsOf(timeline: Timeline, passages: readonly (StayPassage)[]): ReplayPassageLabel[] {
  return passageLabelsAtOffset(timeline, passages, boatOffsetHours(timeline, passages))
}

/** The labels, with the boat's offset worked out already. */
export function passageLabelsAtOffset(timeline: Timeline, passages: readonly (StayPassage)[], offset: number | null): ReplayPassageLabel[] {
  if (!isMultiPassage(timeline)) return []
  // Without the boat's zone the days cannot be counted, and a label that guessed at them would be wrong: there are none.
  if (offset === null) return []
  const byId = new Map(passages.map((p) => [p.id, p]))
  const first = timeline.entries[0].start
  return timeline.entries.map((e) => {
    const p = byId.get(e.id)
    if (!p) throw new Error(`Passage ${e.id} is in the replay's timeline but not among its passages`)
    const day = 1 + localDaysSince(first, e.start, offset)
    return { id: e.id, text: passageLabelText(day, p.from_name, p.to_name) }
  })
}

/** The label, `elapsedS` seconds after its passage set sail, or null when it is not up. */
export function passageLabelView(label: ReplayPassageLabel, elapsedS: number): PassageLabelView | null {
  return elapsedS >= 0 && elapsedS < PASSAGE_LABEL_S ? { label, elapsed: elapsedS } : null
}

/** Where the label sits in a frame of `container`'s size: low and to the left,
 * on the line the stay's label is on, and clear of the overview map in a bottom
 * corner. It is up while the boat sails and the stay's label while it is in
 * port, so the two are never up together. */
export function passageLabelBox(container: { w: number; h: number }, elapsedS: number, insetCorner: InsetCorner | null = null, reserved: MapLabelOptions['reserved'] = []): MapLabelBox {
  return mapLabelBox(container, { align: 'left', elapsedS, durationS: PASSAGE_LABEL_S, insetCorner, reserved })
}
