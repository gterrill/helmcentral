import { useCallback, useRef, useState } from 'react'

/**
 * The busy state of a tile dialog's Save (ADR 0260). `run` starts the task in
 * the same tick, keeps `saving` true until it settles, and turns a rejection or
 * a throw into `error` for the dialog to show. A second `run` while one is out
 * is ignored, so Save cannot be submitted twice. Every tile dialog uses this
 * one hook, so they all behave alike.
 */
export function useTileSave() {
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const busy = useRef(false)

  const run = useCallback(async (task: () => void | Promise<void>, fallback = 'Could not save the tile'): Promise<void> => {
    if (busy.current) return
    busy.current = true
    setSaving(true)
    setError(null)
    try {
      await task()
    } catch (err) {
      setError(err instanceof Error ? err.message : fallback)
    } finally {
      busy.current = false
      setSaving(false)
    }
  }, [])

  const reset = useCallback(() => setError(null), [])

  /** `fail` shows an error from outside `run`, such as a Duplicate that could not settle its zones. */
  const fail = setError

  return { saving, error, run, reset, fail }
}
