import { useEffect, useState } from 'react'

// `initialId` (ADR 0074) lets a caller seed the value from somewhere other
// than localStorage — App.tsx passes the page id parsed off a deep link like
// `/dashboard/<pageId>` — and have it win on mount over whatever was already
// stored, then persist so the linked page becomes the remembered one for
// next time. Omitted (the overwhelmingly common case), this is unchanged:
// the stored id, or null.
export function useLocalStorageId(key: string, initialId?: string | null): [string | null, (id: string | null) => void] {
  const [id, setIdState] = useState<string | null>(
    () => initialId ?? globalThis.localStorage?.getItem(key) ?? null,
  )

  function setId(next: string | null) {
    if (next === null) {
      globalThis.localStorage?.removeItem(key)
    } else {
      globalThis.localStorage?.setItem(key, next)
    }
    setIdState(next)
  }

  // Runs once, only when a caller actually passed a concrete id — `undefined`
  // (no initialId argument at all) and `null` (a deep link that names no
  // page, e.g. `/forecast` or `/`) must both leave whatever's already stored
  // alone, or this would wipe the remembered page on every ordinary mount.
  useEffect(() => {
    if (initialId != null) {
      globalThis.localStorage?.setItem(key, initialId)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return [id, setId]
}
