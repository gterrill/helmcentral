import { useCallback, useSyncExternalStore } from 'react'

// Whether the passage replay shows the small overview map. One choice for the
// replay and for what the export carries, remembered in this browser. Every
// component that reads it shares one store, so a change in the Export menu
// reaches the replay at once, and the other way round.
const INSET_KEY = 'passages.insetMap'
const CHANGE_EVENT = 'passages-inset-preference'

// Holds the choice when storage is unavailable, so the toggle still works.
let memory: boolean | null = null

export function loadInsetPreference(): boolean {
  try {
    const raw = localStorage.getItem(INSET_KEY)
    if (raw !== null) return raw === '1'
  } catch {
    // fall through to the in-page value
  }
  return memory ?? false
}

export function saveInsetPreference(on: boolean): void {
  memory = on
  try {
    localStorage.setItem(INSET_KEY, on ? '1' : '0')
  } catch {
    // Remembering the toggle is a convenience only; the change below still
    // reaches this page.
  }
  window.dispatchEvent(new Event(CHANGE_EVENT))
}

function subscribe(onChange: () => void): () => void {
  window.addEventListener(CHANGE_EVENT, onChange)
  window.addEventListener('storage', onChange)
  return () => {
    window.removeEventListener(CHANGE_EVENT, onChange)
    window.removeEventListener('storage', onChange)
  }
}

/** The shared choice and its setter. */
export function useInsetPreference(): [boolean, (on: boolean) => void] {
  const on = useSyncExternalStore(subscribe, loadInsetPreference, () => false)
  const set = useCallback((next: boolean) => saveInsetPreference(next), [])
  return [on, set]
}
