import { useEffect, useState } from 'react'

// Storage can be missing or throw (a private window, blocked site data), and
// what is kept here is only a convenience, so a failed read or write leaves the
// value unremembered rather than breaking the page.
function readStored(key: string): string | null {
  try {
    return globalThis.localStorage?.getItem(key) ?? null
  } catch {
    return null
  }
}

function writeStored(key: string, value: string | null): void {
  try {
    if (value === null) globalThis.localStorage?.removeItem(key)
    else globalThis.localStorage?.setItem(key, value)
  } catch {
    // Storage blocked: the value is simply not remembered.
  }
}

// `initialId` (ADR 0074) lets a caller seed the value from somewhere other
// than localStorage — App.tsx passes the page id parsed off a deep link like
// `/dashboard/<pageId>` — and have it win on mount over whatever was already
// stored, then persist so the linked page becomes the remembered one for
// next time. Omitted (the overwhelmingly common case), this is unchanged:
// the stored id, or null.
// `persist: false` keeps the value in memory and never touches
// localStorage. The wall display route uses it: the rotation advances the
// active page every few seconds, and the sidebar opens a wall in a new tab
// of the SAME browser, so persisting those writes would leave the
// operator's own dashboard remembering a wall page.
export function useLocalStorageId(key: string, initialId?: string | null, persist = true): [string | null, (id: string | null) => void] {
  const [id, setIdState] = useState<string | null>(() => initialId ?? readStored(key))

  function setId(next: string | null) {
    if (persist) writeStored(key, next)
    setIdState(next)
  }

  // Runs once, only when a caller actually passed a concrete id — `undefined`
  // (no initialId argument at all) and `null` (a deep link that names no
  // page, e.g. `/forecast` or `/`) must both leave whatever's already stored
  // alone, or this would wipe the remembered page on every ordinary mount.
  useEffect(() => {
    if (persist && initialId != null) {
      writeStored(key, initialId)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return [id, setId]
}
