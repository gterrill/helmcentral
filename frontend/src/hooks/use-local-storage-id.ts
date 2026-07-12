import { useState } from 'react'

export function useLocalStorageId(key: string): [string | null, (id: string | null) => void] {
  const [id, setIdState] = useState<string | null>(
    () => globalThis.localStorage?.getItem(key) ?? null,
  )

  function setId(next: string | null) {
    if (next === null) {
      globalThis.localStorage?.removeItem(key)
    } else {
      globalThis.localStorage?.setItem(key, next)
    }
    setIdState(next)
  }

  return [id, setId]
}
