// The time between two passages of a cruise, shown as a stay. The timeline
// leaves the gap out, so the boat would jump from one anchorage to the next; a
// stay is a short hold in the replay at the arrival, with the light running
// from the arrival to the departure, and a label that says how long it was.
//
// The hold itself (its length in the time map, seeking past it) is in
// lib/replay-time. This is what a stay is made of, what the frame says of it,
// and how it is worded and placed. Everything here is pure, so the live replay
// and a video of it show the same stay.

import { midnightsSince } from '@/lib/boat-nights'
import type { InsetCorner } from '@/lib/passage-inset-view'
import { fmtDuration, type Timeline } from '@/lib/passages'
import { CARD_FADE_S } from '@/lib/replay-signs'
import { STAY_S } from '@/lib/replay-time'
import { zoneKnown, zoneOffsetMinutes } from '@/lib/vessel-time'

/** A gap shorter than this is not a stay: the passages ran back to back. */
export const MIN_STAY_S = 60
const DAY_S = 86400

/** What a stay needs to know of the passages either side of it. */
export interface StayPassage {
  id: string
  from_name: string
  to_name: string
  timezone: string | null
}

export interface ReplayStay {
  /** Names this stay in the time map. */
  id: string
  /** 0 for the first stay of the replay. */
  index: number
  /** Where it falls on the timeline: the start of the passage that follows. */
  clock: number
  /** The real instant it is ordered by among the signs at the same clock: the arrival. */
  at: number
  /** The passage the boat arrived on, and the one it leaves on. */
  fromId: string
  toId: string
  /** Unix seconds the boat arrived, and set out again. */
  arrival: number
  departure: number
  /** The words for the label. */
  label: string
}

/** What a frame says of the stay it is in. */
export interface StayView {
  stay: ReplayStay
  /** 0 at the start of the stay to 1 at its end. */
  progress: number
  /** Seconds into the stay. */
  elapsed: number
  /** The real instant the light is worked out for. */
  sunAt: number
}

// ── the light ────────────────────────────────────────────────────────────

/** The real instant the sky and light show, `progress` through a stay. For a
 * stay of a day or less it is the true clock from the arrival to the
 * departure, run fast. For a longer one it is the last day before the
 * departure: one evening, one night and one morning pass, and it ends on the
 * departure's own light, so a stay of a week does not flash through seven
 * nights. */
export function staySunTime(arrival: number, departure: number, progress: number): number {
  const p = Math.max(0, Math.min(1, progress))
  const span = Math.min(Math.max(0, departure - arrival), DAY_S)
  return departure - (1 - p) * span
}

export function stayView(stay: ReplayStay, elapsedS: number): StayView {
  const elapsed = Math.max(0, Math.min(STAY_S, elapsedS))
  const progress = elapsed / STAY_S
  return { stay, progress, elapsed, sunAt: staySunTime(stay.arrival, stay.departure, progress) }
}

// ── the words ────────────────────────────────────────────────────────────

/** How long a stay was: its nights, counted by the rule the cruise page counts
 * its own (boat-local midnights at the zone's offset), or, if it spans no
 * midnight, the time in the app's usual form ("3 h 05 m"). With no offset the
 * nights cannot be counted, and the time is given alone. */
export function stayDurationText(arrival: number, departure: number, offsetHours: number | null): string {
  const nights = offsetHours === null ? 0 : midnightsSince(arrival, departure, offsetHours)
  if (nights >= 1) return nights === 1 ? '1 night' : `${nights} nights`
  return fmtDuration(departure - arrival)
}

/** The label of a stay: "2 nights · Blue Pearl Bay". The place is where the
 * arriving passage ended, else where the next one set out; with neither, the
 * time alone. */
export function stayLabelText(arriving: StayPassage, next: StayPassage, arrival: number, departure: number, offsetHours: number | null): string {
  const duration = stayDurationText(arrival, departure, offsetHours)
  const place = arriving.to_name.trim() || next.from_name.trim()
  return place ? `${duration} · ${place}` : duration
}

// ── the stays of a replay ────────────────────────────────────────────────

/** The stays of a replay: one between each pair of passages that were sailed
 * with at least MIN_STAY_S between them. Nights are counted at the first
 * passage's zone, as the cruise page counts them. */
export function staysOf(timeline: Timeline, passages: readonly StayPassage[]): ReplayStay[] {
  const byId = new Map(passages.map((p) => [p.id, p]))
  const first = timeline.entries[0]
  const zone = first ? byId.get(first.id)?.timezone ?? null : null
  const offsetHours = first && zone && zoneKnown(zone) ? zoneOffsetMinutes(zone, first.start * 1000) / 60 : null
  const out: ReplayStay[] = []
  for (let i = 0; i + 1 < timeline.entries.length; i++) {
    const from = timeline.entries[i]
    const to = timeline.entries[i + 1]
    if (to.start - from.end < MIN_STAY_S) continue
    const arriving = byId.get(from.id)
    const next = byId.get(to.id)
    if (!arriving || !next) throw new Error(`Stay between passages ${from.id} and ${to.id}: one of them is not in the replay's passages`)
    out.push({
      // Named by the passages either side, so a change to which passages are shown never leaves a stay's place pointing at another anchorage.
      id: `stay:${from.id}>${to.id}`, index: out.length, clock: to.offset, at: from.end, fromId: from.id, toId: to.id, arrival: from.end, departure: to.start,
      label: stayLabelText(arriving, next, from.end, to.start, offsetHours),
    })
  }
  return out
}

// ── where it goes ────────────────────────────────────────────────────────

/** How the label looks, for the page and a canvas alike: the page draws it with
 * these and the video draws the same. The colours are the page's card and text
 * colours, so a canvas takes them from its own theme. */
export const STAY_LABEL = {
  /** Font weight. */
  weight: 500,
  /** Corner radius, in the frame's pixels. */
  radius: 6,
  /** How much of the card colour shows behind the text, before the fade. */
  bgAlpha: 0.8,
  /** The line height as a multiple of the font size. */
  lineHeight: 1.4,
} as const

/** The overview map sits in a corner of the page's map, `h-32 w-44` and 8 px
 * from the side, with the bottom ones 32 px up from the foot. */
const INSET = { w: 176, h: 128, side: 8, bottom: 32 } as const
/** The space kept between the label and the edge of the map, and between it and the overview map. */
const EDGE = 24
const INSET_GAP = 12

export interface StayLabelBox {
  /** The middle of the label, across the frame. */
  cx: number
  /** The top of the label. */
  y: number
  fontPx: number
  padX: number
  padY: number
  /** The widest the text may be before it is shortened. */
  maxW: number
  /** 0 to 1: fades in and out over the stay. */
  alpha: number
}

/** Where the label sits in a frame of `container`'s size, and how big, `elapsedS`
 * into the stay: quiet, small, low and in the middle, clear of the cards at the
 * sides. When the overview map is shown in a bottom corner (`insetCorner`) the
 * label keeps clear of it, narrowing to the room that is left. In the frame's
 * own pixels, so the page and a canvas draw it alike. */
export function stayLabelBox(container: { w: number; h: number }, elapsedS: number, insetCorner: InsetCorner | null = null): StayLabelBox {
  const e = Math.max(0, Math.min(STAY_S, elapsedS))
  const alpha = Math.max(0, Math.min(1, Math.min(e, STAY_S - e) / CARD_FADE_S))
  let left = EDGE
  let right = container.w - EDGE
  if (insetCorner === 'bottom-left') left = Math.max(left, INSET.side + INSET.w + INSET_GAP)
  if (insetCorner === 'bottom-right') right = Math.min(right, container.w - INSET.side - INSET.w - INSET_GAP)
  return { cx: (left + right) / 2, y: container.h - 72, fontPx: 13, padX: 12, padY: 6, maxW: Math.max(0, right - left), alpha }
}
