// The order an index shows its records in, and the step to the record either
// side of one. Pure and React-free: an index orders its rows with these, and
// a details page orders the same list with the same call, so the arrows on
// the page can never step through anything but what the operator saw.

export interface ListSort {
  /** The column's id. */
  id: string
  desc: boolean
}

/** `name` is ascending, `-name` descending. Anything else is no sort. */
export function parseListSort(raw: string | null | undefined): ListSort | null {
  if (!raw) return null
  const desc = raw.startsWith('-')
  const id = desc ? raw.slice(1) : raw
  return /^[A-Za-z][A-Za-z0-9_-]*$/.test(id) ? { id, desc } : null
}

export function formatListSort(sort: ListSort): string {
  return sort.desc ? `-${sort.id}` : sort.id
}

export function sameListSort(a: ListSort | null, b: ListSort | null): boolean {
  if (a === null || b === null) return a === b
  return a.id === b.id && a.desc === b.desc
}

type SortValue = string | number | undefined

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' })

function compareValues(a: SortValue, b: SortValue): number {
  if (typeof a === 'number' && typeof b === 'number') return a - b
  return collator.compare(String(a), String(b))
}

/**
 * The rows in the order of `sort`. A column the accessors do not name is no
 * sort. Equal values keep their given order in either direction, and a row
 * with no value goes last in either direction.
 */
export function orderRows<T>(
  rows: readonly T[],
  sort: ListSort | null,
  accessors: Record<string, (row: T) => SortValue>,
): T[] {
  const access = sort ? accessors[sort.id] : undefined
  if (!sort || !access) return [...rows]
  const keyed = rows.map((row, index) => ({ row, index, value: access(row) }))
  keyed.sort((a, b) => {
    const aMissing = a.value === undefined
    const bMissing = b.value === undefined
    if (aMissing || bMissing) return aMissing === bMissing ? a.index - b.index : aMissing ? 1 : -1
    const order = compareValues(a.value, b.value)
    return order === 0 ? a.index - b.index : sort.desc ? -order : order
  })
  return keyed.map((k) => k.row)
}

/** The records either side of `id` in an ordered list. Null at each end, and
 * for both when `id` is not in the list. */
export function neighbours<T extends { id: string }>(ordered: readonly T[], id: string): { previous: T | null; next: T | null }
export function neighbours<T>(ordered: readonly T[], id: string, getId: (row: T) => string): { previous: T | null; next: T | null }
export function neighbours<T>(
  ordered: readonly T[],
  id: string,
  getId: (row: T) => string = (row) => (row as { id: string }).id,
): { previous: T | null; next: T | null } {
  const at = ordered.findIndex((row) => getId(row) === id)
  if (at < 0) return { previous: null, next: null }
  return { previous: ordered[at - 1] ?? null, next: ordered[at + 1] ?? null }
}

/** `list` with `patch` applied. A field patched to undefined, null or an
 * empty string is removed, so a default state is an empty object. */
export function patchList<T extends object>(list: T, patch: { [K in keyof T]?: T[K] | null }): T {
  const next: Record<string, unknown> = { ...(list as Record<string, unknown>) }
  for (const [key, value] of Object.entries(patch)) {
    if (value === undefined || value === null || value === '') delete next[key]
    else next[key] = value
  }
  return next as T
}
