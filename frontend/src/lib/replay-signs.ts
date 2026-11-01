// Signs on the replay (ADR 0204). A sign is a note pinned to a real instant;
// these functions say where each one stands on the track and which ones are on
// screen at a given moment of watching. Everything here is pure.

import { positionAt, type Timeline } from '@/lib/passages'
import { PHOTO_ASPECT } from '@/lib/replay-time'

export type SignVisibility = 'always' | 'passing' | 'after'

export const SIGN_VISIBILITIES: readonly { id: SignVisibility; label: string }[] = [
  { id: 'always', label: 'Always' },
  { id: 'passing', label: 'While passing' },
  { id: 'after', label: 'After passing' },
]

/** What a join with no title yet is called in lists. */
export const AWAITING_NAME = 'Awaiting a name'

export const SIGN_TITLE_MAX = 80
export const SIGN_NOTE_MAX = 280
export const SIGN_SHOW_MIN = 1
export const SIGN_SHOW_MAX = 30
export const SIGN_SHOW_DEFAULT = 4

/** One sign as the server holds it. The photo fields are null when there is no
 * photo or its document has been deleted. */
export interface ReplaySign {
  id: string
  t_ms: number
  title: string
  note: string
  document_id: string | null
  photo_url: string | null
  visibility: SignVisibility
  show_s: number
  /** A join: the track from here on is drawn as a new leg. Only a join may
   * have no title; it waits for a name from the track. */
  starts_leg: boolean
  /** The operator's choice for the photo to fill the frame in a replay. Null is the default (see photoMomentOn). */
  photo_moment: boolean | null
}

/** Whether a sign's photo gets a moment in the replay: it must have a photo,
 * and the operator must not have switched it off. Null means the default, which is on. */
export function photoMomentOn(sign: Pick<ReplaySign, 'photo_url' | 'photo_moment'>): boolean {
  return sign.photo_url != null && sign.photo_moment !== false
}

/** The signs whose photos hold the replay for a moment, and where the boat meets them: the ones with a title to show (a join waiting for a name draws nothing) and a photo that is not switched off, less any in `excluded` (a photo that would not load). */
export function momentsOf(placed: readonly ResolvedSign[], excluded: ReadonlySet<string> = new Set()): { clock: number; id: string }[] {
  return placed.filter((r) => r.sign.title !== '' && photoMomentOn(r.sign) && !excluded.has(r.sign.id)).map((r) => ({ clock: r.clock, id: r.sign.id }))
}

/** A sign placed on the replay. */
export interface ResolvedSign {
  sign: ReplaySign
  /** Timeline seconds where the sign is. */
  clock: number
  /** Unix seconds of the sign. */
  at: number
  entryId: string
  lat: number
  lon: number
}

/** How long before the boat arrives a passing sign appears, in seconds of watching. */
export const SIGN_LEAD_S = 0.5
/** The most cards on screen at once. */
export const MAX_SIGN_CARDS = 2

/** Where an instant stands on the replay: the passage entry and the track
 * position, or null in a trimmed-out stretch, a gap, or where the track has no
 * position. The one rule for "does this moment land in the replay". */
export function placeAt(at: number, timeline: Timeline) {
  const entry = timeline.entries.find((e) => at >= e.start && at <= e.end)
  if (!entry) return null
  const pos = positionAt(entry.segments, at)
  return pos ? { entry, pos } : null
}

/** The signs whose time falls inside a replayed passage, oldest first, each at
 * the track position for its time. A sign in a trimmed-out stretch, in the gap
 * between passages, or where the track has no position is not placed; it is
 * left as it is, not deleted. */
export function resolveSigns(signs: readonly ReplaySign[], timeline: Timeline): ResolvedSign[] {
  const out: ResolvedSign[] = []
  for (const sign of signs) {
    const at = sign.t_ms / 1000
    const placed = placeAt(at, timeline)
    if (!placed) continue
    const { entry, pos } = placed
    out.push({ sign, at, entryId: entry.id, clock: entry.offset + (at - entry.start), lat: pos.lat, lon: pos.lon })
  }
  return out.sort((a, b) => a.sign.t_ms - b.sign.t_ms || a.sign.id.localeCompare(b.sign.id))
}

/** When a sign is up, in seconds of watching: from (inclusive) to (exclusive). */
function window(r: ResolvedSign, reachedAt: number): { from: number; to: number } {
  switch (r.sign.visibility) {
    case 'always': return { from: -Infinity, to: Infinity }
    case 'after': return { from: reachedAt, to: Infinity }
    case 'passing': return { from: reachedAt - SIGN_LEAD_S, to: reachedAt + r.sign.show_s }
  }
}

/**
 * The cards on screen after `wall` seconds of watching, in the order they
 * stand: always signs first, then the rest oldest to newest. At most
 * MAX_SIGN_CARDS.
 *
 * Signs take their places in the order they appear. When a third would be up,
 * the one that appeared earliest gives way for good: it does not come back when
 * a later one expires. Always signs are never the one that gives way: with two
 * of them nothing else shows, and with more than two the earliest in time show.
 * The answer is worked out from the signs alone, replaying the appearances up
 * to `wall`, so a seek lands on exactly what continuous play would show.
 * Watching time is wall-clock: `wallAt` (the replay clock's mapping) is what
 * makes a slowed stretch last as long as it feels.
 */
export function visibleSigns(resolved: readonly ResolvedSign[], wall: number, wallAt: (clock: number) => number): ResolvedSign[] {
  interface Entry { r: ResolvedSign; from: number; to: number }
  const entries: Entry[] = []
  for (const r of resolved) {
    const w = window(r, wallAt(r.clock))
    if (w.from <= wall) entries.push({ r, ...w })
  }
  entries.sort((a, b) => a.from - b.from || a.r.sign.t_ms - b.r.sign.t_ms || a.r.sign.id.localeCompare(b.r.sign.id))
  let up: Entry[] = []
  for (const e of entries) {
    up = up.filter((u) => u.to > e.from)
    up.push(e)
    if (up.length > MAX_SIGN_CARDS) {
      const give = up.find((u) => u.r.sign.visibility !== 'always') ?? up[up.length - 1]
      up = up.filter((u) => u !== give)
    }
  }
  const live = up.filter((u) => u.to > wall)
  const always = live.filter((u) => u.r.sign.visibility === 'always')
  return [...always, ...live.filter((u) => u.r.sign.visibility !== 'always')].map((u) => u.r)
}

/** How far above its point a card floats, by the place it holds, so two cards
 * for nearby points do not sit on top of each other. */
export const SIGN_LIFT_PX = [20, 104] as const
/** The gap kept between a card and the edge of the map. */
export const SIGN_EDGE_MARGIN_PX = 6
/** The shortest leader line a card is squeezed down to. */
export const SIGN_MIN_LEADER_PX = 8
/** Room kept clear at the top for the Play, Pause and zoom controls. */
export const SIGN_TOP_MARGIN_PX = 48
/** The leader must still meet the card, so a card slides no further than this
 * short of its edge from centred over its point. */
const LEADER_INSET_PX = 6

export interface SignPlacementInput {
  /** The sign's point in the map container, in pixels. */
  point: { x: number; y: number }
  container: { w: number; h: number }
  /** The card's own size. */
  card: { w: number; h: number }
  /** The leader line length the card would like. */
  lift: number
}

export interface SignPlacement {
  /** Sideways shift of the card; the leader and dot stay on the point. */
  dx: number
  /** The card sits under its point, the leader going up to it. */
  below: boolean
  /** The leader line's length. */
  leader: number
}

/** Where a card sits so it stays whole inside the map. It slides sideways near
 * an edge, drops below its point when there is more room there, and shortens
 * its leader (down to a minimum) when it is tight. With an unknown map or card
 * size it is left as it prefers. Shared by the app and the exported player. */
export function placeSignCard({ point, container, card, lift }: SignPlacementInput): SignPlacement {
  if (container.w <= 0 || container.h <= 0 || card.w <= 0 || card.h <= 0) return { dx: 0, below: false, leader: lift }
  const m = SIGN_EDGE_MARGIN_PX
  let dx = 0
  if (card.w + 2 * m <= container.w) {
    const left = point.x - card.w / 2
    if (left < m) dx = m - left
    else if (left + card.w > container.w - m) dx = container.w - m - (left + card.w)
    const reach = Math.max(0, card.w / 2 - LEADER_INSET_PX)
    dx = Math.max(-reach, Math.min(reach, dx))
  }
  const room = point.y - card.h - SIGN_TOP_MARGIN_PX
  if (room >= SIGN_MIN_LEADER_PX) return { dx, below: false, leader: Math.min(lift, room) }
  const under = container.h - m - point.y - card.h
  if (under > room) return { dx, below: true, leader: Math.max(SIGN_MIN_LEADER_PX, Math.min(lift, under)) }
  return { dx, below: false, leader: SIGN_MIN_LEADER_PX }
}

// ── the card's size ──────────────────────────────────────────────────────
// SignCard is built from these, so the page and the replay's photo moment, which
// grows from the card's photo, cannot disagree about where the photo is.

/** The width of a card with a photo, and of one with text alone. */
export const SIGN_CARD_PHOTO_W_PX = 256
export const SIGN_CARD_TEXT_W_PX = 224
/** The space round the card's contents, and between its photo and its text. */
export const SIGN_CARD_PAD_PX = 8
export const SIGN_CARD_GAP_PX = 8
/** The line heights of the title and of the note, which shows two lines at most. */
export const SIGN_TITLE_LINE_PX = 20
export const SIGN_NOTE_LINE_PX = 16
const SIGN_NOTE_MAX_LINES = 2
/** About how many characters of the note fit on a line of a card with a photo. */
const SIGN_NOTE_LINE_CHARS = 36
/** How far the card's leader meets the point: the dot's height that shows. */
const SIGN_DOT_NET_PX = 4

/** How many lines of the note the card shows. */
function noteLines(note: string): number {
  if (note === '') return 0
  const lines = note.split('\n').reduce((n, line) => n + Math.max(1, Math.ceil(line.length / SIGN_NOTE_LINE_CHARS)), 0)
  return Math.min(SIGN_NOTE_MAX_LINES, lines)
}

/** Where the photo of a sign's card is in the map, in the map's pixels: the card
 * as `placeSignCard` puts it for the sign's point, its photo inside the padding.
 * The card's height is worked out from its contents, since the page has not
 * measured it. */
export function signCardPhotoRect(
  sign: Pick<ReplaySign, 'note'>,
  point: { x: number; y: number },
  container: { w: number; h: number },
  lift: number,
): { x: number; y: number; w: number; h: number } {
  const w = Math.min(SIGN_CARD_PHOTO_W_PX, Math.max(0, container.w - 2 * SIGN_EDGE_MARGIN_PX))
  const photoW = w - 2 * SIGN_CARD_PAD_PX
  const photoH = photoW / PHOTO_ASPECT
  const text = SIGN_TITLE_LINE_PX + noteLines(sign.note) * SIGN_NOTE_LINE_PX
  const h = 2 * SIGN_CARD_PAD_PX + photoH + SIGN_CARD_GAP_PX + text
  const place = placeSignCard({ point, container, card: { w, h }, lift })
  const x = point.x - w / 2 + place.dx
  const y = place.below ? point.y + SIGN_DOT_NET_PX + place.leader : point.y - SIGN_DOT_NET_PX - place.leader - h
  return { x: x + SIGN_CARD_PAD_PX, y: y + SIGN_CARD_PAD_PX, w: photoW, h: photoH }
}
