import { useCallback, useEffect, useRef, useState } from 'react'

/**
 * The busy state of a tile dialog's Save. `run` starts the task in
 * the same tick, keeps `saving` true until it settles, and turns a rejection or
 * a throw into `error` for the dialog to show. A second `run` while one is out
 * is ignored, so Save cannot be submitted twice. Every tile dialog uses this
 * one hook, so they all behave alike.
 *
 * `tileKey` names the tile the dialog is on (its id, or null while closed).
 * When it changes, the busy state and the error are dropped and a run still
 * out for the old tile can no longer touch them, so a late failure never shows
 * on the next tile.
 */
export function useTileSave(tileKey: string | null | undefined) {
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const busy = useRef(false)
  const generation = useRef(0)

  useEffect(() => {
    generation.current++
    busy.current = false
    setSaving(false)
    setError(null)
  }, [tileKey])

  const run = useCallback(async (task: () => void | Promise<void>, fallback = 'Could not save the tile'): Promise<void> => {
    if (busy.current) return
    busy.current = true
    const mine = generation.current
    setSaving(true)
    setError(null)
    try {
      await task()
    } catch (err) {
      if (generation.current === mine) setError(err instanceof Error ? err.message : fallback)
    } finally {
      if (generation.current === mine) {
        busy.current = false
        setSaving(false)
      }
    }
  }, [])

  return { saving, error, run }
}
