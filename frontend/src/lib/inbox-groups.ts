import type { InboxGroupKind, InboxItem } from '@/lib/inbox-api'
import { hammingHex } from '@/lib/image-fingerprint'
import { formatLocalDate, type PassageSummary } from '@/lib/passages'

// How the Inbox view arranges what is waiting, and the suggestions it makes
// about culling. Nothing here removes or files anything: the flags are hints
// the operator can ignore.

export interface InboxGroup {
  /** `underway`, `passage:<id>`, `off_passage` or `no_time`. */
  key: string
  kind: InboxGroupKind
  passageId: string | null
  label: string
  items: InboxItem[]
  /** Whether the group's photos can be made signs now. Photos taken underway
   * wait for the passage to be recorded. */
  canAssign: boolean
}

/**
 * A passage as the Inbox names it: its title, else "From → To" with whichever
 * end has a name, then the date. With no title and no names, just the date.
 */
export function inboxPassageLabel(p: PassageSummary): string {
  const date = formatLocalDate(p.start, p.timezone)
  const title = p.title.trim()
  const ends = [p.from_name.trim(), p.to_name.trim()].filter((n) => n !== '')
  const name = title !== '' ? title : ends.join(' → ')
  return name === '' ? date : `${name} · ${date}`
}

const byCapture = (a: InboxItem, b: InboxItem) =>
  (a.capture_ms ?? Infinity) - (b.capture_ms ?? Infinity) || a.created_ms - b.created_ms || a.id.localeCompare(b.id)

/**
 * Groups in the order the operator works through them: what is happening now,
 * each passage newest first, photos between passages, photos with no time.
 * Items are in capture order within a group.
 */
export function groupInbox(items: readonly InboxItem[], passages: readonly PassageSummary[]): InboxGroup[] {
  const byPassage = new Map(passages.map((p) => [p.id, p]))
  const buckets = new Map<string, InboxGroup>()
  const add = (key: string, make: () => Omit<InboxGroup, 'key' | 'items'>, item: InboxItem) => {
    let g = buckets.get(key)
    if (!g) {
      g = { key, items: [], ...make() }
      buckets.set(key, g)
    }
    g.items.push(item)
  }
  for (const item of items) {
    const { kind, passage_id: passageId } = item.group
    if (kind === 'passage' && passageId) {
      const p = byPassage.get(passageId)
      add(`passage:${passageId}`, () => ({
        kind, passageId, canAssign: true,
        label: p ? inboxPassageLabel(p) : 'A passage that is no longer listed',
      }), item)
    } else if (kind === 'underway') {
      add('underway', () => ({ kind, passageId: null, canAssign: false, label: 'Underway now' }), item)
    } else if (kind === 'no_time') {
      add('no_time', () => ({ kind, passageId: null, canAssign: true, label: 'No time' }), item)
    } else {
      add('off_passage', () => ({ kind: 'off_passage', passageId: null, canAssign: true, label: 'Not on a passage' }), item)
    }
  }
  const startOf = (g: InboxGroup) => {
    const p = g.passageId ? byPassage.get(g.passageId) : undefined
    return p ? Date.parse(p.start) : g.items[0]?.capture_ms ?? 0
  }
  const rank = (g: InboxGroup) => (g.kind === 'underway' ? 0 : g.kind === 'passage' ? 1 : g.kind === 'off_passage' ? 2 : 3)
  const groups = [...buckets.values()]
  for (const g of groups) g.items.sort(byCapture)
  return groups.sort((a, b) => rank(a) - rank(b) || (a.kind === 'passage' ? startOf(b) - startOf(a) : 0))
}

// ── culling suggestions ─────────────────────────────────────────────────

export interface DuplicateInfo {
  /** The frame of the set with the best sharpness, the one to keep. */
  keeperId: string
  isKeeper: boolean
  /** How many frames the set holds. */
  size: number
}

/** Frames whose perceptual hashes differ by at most this many of 64 bits are the same shot. */
export const DUPLICATE_MAX_DISTANCE = 6
/** And that were taken within this long of one another. */
export const DUPLICATE_WINDOW_MS = 60_000

const comparable = (i: InboxItem) => i.mime.startsWith('image/') && i.fingerprint !== '' && i.capture_ms !== null

/**
 * Sets of near-identical frames taken close together, a burst or a retake, with
 * the sharpest of each set (the earliest on a tie) named the keeper. A frame
 * joins a set through any one member, so a slow pan that drifts is still one set.
 */
export function findDuplicates(items: readonly InboxItem[]): Map<string, DuplicateInfo> {
  const frames = items.filter(comparable).sort(byCapture)
  const parent = frames.map((_, i) => i)
  const find = (i: number): number => (parent[i] === i ? i : (parent[i] = find(parent[i])))
  for (let i = 0; i < frames.length; i++) {
    for (let j = i + 1; j < frames.length; j++) {
      if (frames[j].capture_ms! - frames[i].capture_ms! > DUPLICATE_WINDOW_MS) break
      if (frames[i].fingerprint.length === frames[j].fingerprint.length && hammingHex(frames[i].fingerprint, frames[j].fingerprint) <= DUPLICATE_MAX_DISTANCE) {
        parent[find(i)] = find(j)
      }
    }
  }
  const sets = new Map<number, InboxItem[]>()
  frames.forEach((f, i) => {
    const root = find(i)
    sets.set(root, [...(sets.get(root) ?? []), f])
  })
  const out = new Map<string, DuplicateInfo>()
  for (const set of sets.values()) {
    if (set.length < 2) continue
    // Capture order already holds, so the first of the sharpest is the earliest.
    const keeper = set.reduce((best, f) => (f.sharpness > best.sharpness ? f : best))
    for (const f of set) out.set(f.id, { keeperId: keeper.id, isKeeper: f.id === keeper.id, size: set.length })
  }
  return out
}

/** A frame scoring under this share of the batch's median sharpness is flagged soft. */
export const BLUR_RATIO = 0.4
/** Fewest scored frames to compare against; fewer than this and nothing is flagged. */
export const BLUR_MIN_BATCH = 4

/** Ids of frames far softer than the rest of the batch. Photos of a plain sea and sky score low honestly, which is why this only suggests. */
export function findBlurry(items: readonly InboxItem[]): Set<string> {
  const scored = items.filter((i) => i.mime.startsWith('image/') && i.sharpness > 0)
  if (scored.length < BLUR_MIN_BATCH) return new Set()
  const sorted = scored.map((i) => i.sharpness).sort((a, b) => a - b)
  const mid = sorted.length >> 1
  const median = sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2
  return new Set(scored.filter((i) => i.sharpness < median * BLUR_RATIO).map((i) => i.id))
}

// ── placing a photo that is not on a passage ──────────────────────────────

export interface PlacementEdge {
  passageId: string
  edge: 'start' | 'end'
  tMs: number
}

/** The start or end of a passage nearest in time to a capture time, or null with no passages. */
export function nearestEdge(captureMs: number, passages: readonly PassageSummary[]): PlacementEdge | null {
  let best: (PlacementEdge & { gap: number }) | null = null
  for (const p of passages) {
    for (const [edge, tMs] of [['start', Date.parse(p.start)], ['end', Date.parse(p.end)]] as const) {
      const gap = Math.abs(captureMs - tMs)
      if (best === null || gap < best.gap) best = { passageId: p.id, edge, tMs, gap }
    }
  }
  return best && { passageId: best.passageId, edge: best.edge, tMs: best.tMs }
}

export interface PlacementOption extends PlacementEdge {
  label: string
}

/** Every passage start and end the operator can attach a photo to, oldest first. */
export function placementOptions(passages: readonly PassageSummary[]): PlacementOption[] {
  return passages
    .flatMap((p) => {
      const name = inboxPassageLabel(p)
      return [
        { passageId: p.id, edge: 'start' as const, tMs: Date.parse(p.start), label: `Start of ${name}` },
        { passageId: p.id, edge: 'end' as const, tMs: Date.parse(p.end), label: `End of ${name}` },
      ]
    })
    .sort((a, b) => a.tMs - b.tMs)
}
