/**
 * Equality for parsed JSON. Kept apart from telemetry-store.ts, which imports
 * the live stream: the query store (api-query.ts) compares responses with
 * these and must not pull the stream in with them.
 */

/** Structural equality for parsed JSON: objects, arrays and primitives. */
export function sameJSON(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (typeof a !== 'object' || typeof b !== 'object' || a === null || b === null) return false
  if (Array.isArray(a) !== Array.isArray(b)) return false
  if (Array.isArray(a)) {
    const other = b as unknown[]
    return a.length === other.length && a.every((item, i) => sameJSON(item, other[i]))
  }
  const aRecord = a as Record<string, unknown>
  const bRecord = b as Record<string, unknown>
  const aKeys = Object.keys(aRecord)
  if (aKeys.length !== Object.keys(bRecord).length) return false
  return aKeys.every((key) => key in bRecord && sameJSON(aRecord[key], bRecord[key]))
}

/** The same own keys in two flat objects, each holding the same value (Object.is). */
export function sameShallow<T extends object>(a: T, b: T): boolean {
  const keys = Object.keys(a) as Array<keyof T>
  return keys.length === Object.keys(b).length && keys.every((key) => key in b && Object.is(a[key], b[key]))
}

/** The same entries in two string lists, ignoring order and repeats. */
export function sameSet(a: string[], b: string[]): boolean {
  const left = new Set(a)
  const right = new Set(b)
  return left.size === right.size && [...left].every((entry) => right.has(entry))
}
