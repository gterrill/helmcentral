import { useEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react'

// Whether two draft field values are the same. Fields are strings, numbers,
// booleans, null or flat lists of them. A list is compared in order unless the
// caller says its order carries no meaning (tags, aliases), in which case a
// server echo that reorders it is not an edit.
function sameValue(a: unknown, b: unknown, unordered: boolean): boolean {
  if (Array.isArray(a) && Array.isArray(b)) {
    if (a.length !== b.length) return false
    if (unordered) {
      const rest = [...b]
      return a.every((v) => {
        const at = rest.findIndex((w) => Object.is(v, w))
        if (at < 0) return false
        rest.splice(at, 1)
        return true
      })
    }
    return a.every((v, i) => Object.is(v, b[i]))
  }
  return Object.is(a, b)
}

export interface FollowingDraftOptions<D> {
  /** List fields whose order does not matter. */
  unordered?: readonly (keyof D & string)[]
  /** Fields that only make sense together (a zone and its bin). If the
   * operator has edited any field of a group, the whole group keeps the
   * draft's values, so the draft never mixes theirs with the server's. */
  groups?: readonly (readonly (keyof D & string)[])[]
}

/**
 * A form draft seeded from a record, that keeps following the record.
 *
 * The same record does come back changed: the store answers from its cache
 * first and then revalidates (a stale first answer), and a pending document is
 * polled while enrichment writes its suggested title. A draft seeded once
 * would keep the old values, read dirty, and Save would write them back.
 *
 * So when a fresher record of the same id lands, every field the operator has
 * not edited takes the fresh value, and every field they did edit keeps their
 * text. "Not edited" means the field still equals what the draft last took
 * from the record. A different id reseeds the whole draft. With no record the
 * draft is left as the caller set it (a blank new item) and the seed is
 * forgotten, so the next record seeds afresh.
 *
 * `initial` is what the draft holds before any record has seeded it.
 * `options.unordered` and `options.groups` are described on FollowingDraftOptions.
 */
export function useFollowingDraft<R extends { id: string }, D extends object, I extends D | null = D>(
  record: R | null | undefined,
  toDraft: (record: R) => D,
  initial: I,
  options?: FollowingDraftOptions<D>,
): [D | I, Dispatch<SetStateAction<D | I>>] {
  const [draft, setDraft] = useState<D | I>(initial)
  const seeded = useRef<{ id: string; draft: D } | null>(null)
  const toDraftRef = useRef(toDraft)
  toDraftRef.current = toDraft
  const optionsRef = useRef(options)
  optionsRef.current = options

  useEffect(() => {
    if (!record) {
      seeded.current = null
      return
    }
    const fresh = toDraftRef.current(record)
    const previous = seeded.current
    seeded.current = { id: record.id, draft: fresh }
    if (!previous || previous.id !== record.id) {
      setDraft(fresh)
      return
    }
    setDraft((current) => {
      if (current === null) return fresh
      const { unordered = [], groups = [] } = optionsRef.current ?? {}
      const keys = Object.keys(fresh) as (keyof D & string)[]
      const edited = new Set(keys.filter((key) => !sameValue((current as D)[key], previous.draft[key], unordered.includes(key))))
      for (const group of groups) {
        if (group.some((key) => edited.has(key))) group.forEach((key) => edited.add(key))
      }
      const merged: Record<string, unknown> = {}
      for (const key of keys) merged[key] = edited.has(key) ? (current as D)[key] : fresh[key]
      // Nothing differs from what the draft already holds: keep the same
      // object, so a poll that changed nothing does not re-render the form.
      if (keys.every((key) => sameValue(merged[key], (current as D)[key], false))) return current
      return merged as D
    })
  }, [record])

  return [draft, setDraft]
}
