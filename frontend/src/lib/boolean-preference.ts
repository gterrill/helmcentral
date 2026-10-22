import { useCallback, useSyncExternalStore } from 'react'

export interface BooleanPreference {
  load: () => boolean
  save: (on: boolean) => void
  /** The shared choice and its setter. */
  use: () => [boolean, (on: boolean) => void]
}

/** A yes/no choice remembered in this browser. Every reader of one key shares
 * one store, so a change in one place reaches the others at once. An in-page
 * copy holds the choice when storage is unavailable, so toggles still work. */
export function createBooleanPreference(key: string, defaultOn: boolean): BooleanPreference {
  const changeEvent = `boolean-preference:${key}`
  let memory: boolean | null = null

  const load = (): boolean => {
    try {
      const raw = localStorage.getItem(key)
      if (raw !== null) return raw === '1'
    } catch {
      // fall through to the in-page value
    }
    return memory ?? defaultOn
  }

  const save = (on: boolean): void => {
    memory = on
    try {
      localStorage.setItem(key, on ? '1' : '0')
    } catch {
      // Remembering the choice is a convenience only.
    }
    window.dispatchEvent(new Event(changeEvent))
  }

  const subscribe = (onChange: () => void): (() => void) => {
    window.addEventListener(changeEvent, onChange)
    window.addEventListener('storage', onChange)
    return () => {
      window.removeEventListener(changeEvent, onChange)
      window.removeEventListener('storage', onChange)
    }
  }

  const use = (): [boolean, (on: boolean) => void] => {
    const on = useSyncExternalStore(subscribe, load, () => defaultOn)
    const set = useCallback((next: boolean) => save(next), [])
    return [on, set]
  }

  return { load, save, use }
}
