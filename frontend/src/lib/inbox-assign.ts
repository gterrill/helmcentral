import type { InboxItem } from '@/lib/inbox-api'
import { nearestEdgeMs } from '@/lib/inbox-groups'
import type { PassageSummary } from '@/lib/passages'
import { SIGN_TITLE_MAX } from '@/lib/replay-signs'

// The step between "add these photos to a passage" and the signs being made:
// one row per photo with an editable title and, for a photo that is not on a
// passage, the place to pin it.

export interface AssignRow {
  item: InboxItem
  title: string
  /** Where to pin it, Unix ms, when the photo's own time will not do; null to use the photo's time. */
  tMs: number | null
}

const clip = (s: string) => Array.from(s).slice(0, SIGN_TITLE_MAX).join('')

/** Rows with the suggested titles in and, between passages, the nearest passage start or end chosen. */
export function initialAssignRows(items: readonly InboxItem[], titles: Readonly<Record<string, string>>, passages: readonly PassageSummary[]): AssignRow[] {
  return items.map((item) => {
    let tMs: number | null = null
    if (item.group.kind === 'off_passage' && item.capture_ms !== null) tMs = nearestEdgeMs(item.capture_ms, passages)
    return { item, title: clip((titles[item.id] ?? '').trim()), tMs }
  })
}

/** Whether the row is a photo, whether it has a title, and whether it has somewhere to go; the first problem in words, or null. */
export function rowProblem(row: AssignRow): string | null {
  if (!row.item.mime.startsWith('image/')) return 'Only a photo can be a sign'
  const title = row.title.trim()
  if (title === '') return 'A title is required'
  if (Array.from(title).length > SIGN_TITLE_MAX) return `A title is at most ${SIGN_TITLE_MAX} characters`
  if (row.item.group.kind !== 'passage' && row.tMs === null) return 'Choose where on a passage it goes'
  return null
}

/** The body of the filing call. */
export function assignRequest(rows: readonly AssignRow[]): { id: string; title: string; t_ms?: number }[] {
  return rows.map((r) => ({ id: r.item.id, title: r.title.trim(), ...(r.tMs !== null ? { t_ms: r.tMs } : {}) }))
}
