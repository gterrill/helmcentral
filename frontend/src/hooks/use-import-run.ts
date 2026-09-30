import { useCallback, useEffect, useRef, useState } from 'react'

import {
  fetchImportRun,
  patchImportDecisions,
  withDecision,
  type FileDecision,
  type ImportDecisions,
  type ImportDecisionsPatch,
  type ImportRun,
} from '@/lib/import-run'

// One import run and the operator's working copy of its decisions. The
// decisions are edited locally as the operator moves around a page and sent
// to the server, changed entries only, when they move to another page
// (flush). A rejected save is thrown to the caller and the changes are kept,
// so the page can show the server's reason and let the operator fix it.

function mergePatch(target: ImportDecisionsPatch, category: keyof ImportDecisions, key: string, value: unknown): void {
  const bucket = (target[category] ?? {}) as Record<string, unknown>
  bucket[key] = value
  ;(target as Record<string, unknown>)[category] = bucket
}

function isEmpty(patch: ImportDecisionsPatch): boolean {
  return Object.values(patch).every((bucket) => Object.keys(bucket ?? {}).length === 0)
}

/** The server's decisions with the operator's not-yet-sent edits laid over
 * them, so a response never makes an edit vanish from the screen. */
function overlayPending(decisions: ImportDecisions, pending: ImportDecisionsPatch): ImportDecisions {
  let out = decisions
  for (const category of Object.keys(pending) as Array<keyof ImportDecisions>) {
    for (const [key, value] of Object.entries(pending[category] ?? {})) {
      out = withDecision(out, category, key, value as ImportDecisions[typeof category][string])
    }
  }
  return out
}

export function useImportRun(runId: string) {
  const [run, setRun] = useState<ImportRun | null>(null)
  const [decisions, setDecisions] = useState<ImportDecisions | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const pendingRef = useRef<ImportDecisionsPatch>({})

  useEffect(() => {
    let cancelled = false
    pendingRef.current = {}
    setRun(null)
    setDecisions(null)
    setLoadError(null)
    fetchImportRun(runId).then(
      (loaded) => {
        if (cancelled) return
        setRun(loaded)
        setDecisions(loaded.decisions)
      },
      (err: unknown) => {
        if (!cancelled) setLoadError(err instanceof Error ? err.message : String(err))
      },
    )
    return () => { cancelled = true }
  }, [runId])

  const setDecision = useCallback(<K extends keyof ImportDecisions>(category: K, key: string, value: ImportDecisions[K][string]) => {
    mergePatch(pendingRef.current, category, key, value)
    setDecisions((previous) => (previous === null ? previous : withDecision(previous, category, key, value)))
  }, [])

  /** Sends what changed since the last save. Throws the server's message on
   * a refusal and keeps the changes for another try. */
  const flush = useCallback(async () => {
    if (isEmpty(pendingRef.current)) return
    const sending = pendingRef.current
    pendingRef.current = {}
    setSaving(true)
    try {
      const saved = await patchImportDecisions(runId, sending)
      setRun(saved)
      // Edits made while this save was in flight are still pending; keep them
      // on screen, they go out with the next flush.
      setDecisions(overlayPending(saved.decisions, pendingRef.current))
    } catch (err) {
      // Put the unsent entries back under anything edited in the meantime.
      const newer = pendingRef.current
      pendingRef.current = sending
      for (const category of Object.keys(newer) as Array<keyof ImportDecisions>) {
        for (const [key, value] of Object.entries(newer[category] ?? {})) mergePatch(pendingRef.current, category, key, value)
      }
      throw err
    } finally {
      setSaving(false)
    }
  }, [runId])

  /** Takes the run the server returned after an upload. Only the one file's
   * decision is adopted, so edits not yet sent on other pages survive; the
   * operator's unsent "attach to equipment" choice for this file wins over
   * the server's copy and stays pending. */
  const adoptUpload = useCallback((updated: ImportRun, fileKey: string) => {
    setRun(updated)
    const fileDecision: FileDecision | undefined = updated.decisions.files[fileKey]
    if (fileDecision === undefined) return
    let adopted = fileDecision
    const pendingFile = pendingRef.current.files?.[fileKey]
    if (pendingFile !== undefined) {
      adopted = { ...fileDecision, equipment_key: pendingFile.equipment_key }
      mergePatch(pendingRef.current, 'files', fileKey, adopted)
    }
    setDecisions((previous) => (previous === null ? previous : withDecision(previous, 'files', fileKey, adopted)))
  }, [])

  const adoptRun = useCallback((updated: ImportRun) => {
    pendingRef.current = {}
    setRun(updated)
    setDecisions(updated.decisions)
  }, [])

  return { run, decisions, loadError, saving, setDecision, flush, adoptUpload, adoptRun }
}
