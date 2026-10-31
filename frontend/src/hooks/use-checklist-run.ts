import { useCallback, useEffect, useRef, useState } from 'react'
import { apiRequest } from '@/lib/api-query'
import { errorMessage } from '@/lib/errors'

// Plan "Notes and the Boat's Manual" §3/§4, ADR 0118: the checklist
// runner's data layer, the same small "no react-query, one instance per
// caller, throw-the-server's-own-message" idiom use-notes.ts uses.
// Deliberately its own hook rather than folded into use-notes.ts: a note's
// CRUD and a run's tick state are different lifecycles (a run is started,
// ticked many times and closed; a note is read once and occasionally
// patched), and ChecklistRunner (checklist-runner.tsx) is the only caller.

// The note's own checklist TEMPLATE - parseChecklistItems' own JSON shape
// (notes_format.go), no tick state at all. This is what GET /api/notes/:id's
// own `checklist` field carries (use-notes.ts's NoteDetail): the current
// body's items, present even with no active run, so a reader can decide
// whether "Start checklist" belongs on screen without needing a run to
// exist first.
export interface ChecklistItem {
  item_key: string
  occurrence: number
  text: string
  depth: number
}

export interface ChecklistItemState extends ChecklistItem {
  checked: boolean
  checked_at?: string
}

export interface ChecklistChangedItem {
  item_key: string
  occurrence: number
  text: string
  checked_at: string
}

// Mirrors checklist_runs_store.go's checklistRunView field for field.
// completed_at/abandoned_at are absent (not null) on an open run - the
// backend's own `omitempty` - so both are optional here rather than
// `string | null`.
export interface ChecklistRun {
  id: string
  document_id: string
  started_at: string
  completed_at?: string
  abandoned_at?: string
  items: ChecklistItemState[]
  changed: ChecklistChangedItem[]
  total: number
  checked_count: number
}

/**
 * Starts (or resumes - plan §3's "POST on a note that already has an
 * active run returns 200 {run, resumed:true}") a checklist run for noteId
 * on mount, and exposes the tick/complete/abandon actions ChecklistRunner
 * drives. Every action sets `run` straight from the server's own response,
 * never merged with whatever the caller already had locally - plan §3's
 * own reasoning: a tick may be the last thing that happens before the
 * operator walks away from the screen, so the client must never guess.
 */
export function useChecklistRun(noteId: string) {
  const [run, setRun] = useState<ChecklistRun | null>(null)
  const [resumed, setResumed] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Same ordering guard use-notes.ts's own refresh() uses: noteId changing
  // mid-flight must not let an older start() overwrite a newer one's
  // result.
  const seqRef = useRef(0)

  useEffect(() => {
    const seq = (seqRef.current += 1)
    setLoading(true)
    // A POST that starts or resumes a run, not a read, so it stays out of
    // useApiQuery and keeps its own ordering guard.
    apiRequest<{ run: ChecklistRun; resumed: boolean }>(
      `/api/notes/${encodeURIComponent(noteId)}/checklist-runs`,
      { method: 'POST' },
    )
      .then((res) => {
        if (seq !== seqRef.current) return
        setRun(res.run)
        setResumed(res.resumed)
        setError(null)
      })
      .catch((err) => {
        if (seq !== seqRef.current) return
        setError(errorMessage(err))
      })
      .finally(() => {
        if (seq === seqRef.current) setLoading(false)
      })
  }, [noteId])

  const tick = useCallback(async (itemKey: string, occurrence: number, checked: boolean) => {
    if (!run) return
    const res = await apiRequest<{ run: ChecklistRun }>(
      `/api/checklist-runs/${encodeURIComponent(run.id)}/items`,
      { method: 'PATCH', json: { item_key: itemKey, occurrence, checked } },
    )
    setRun(res.run)
  }, [run])

  const complete = useCallback(async () => {
    if (!run) return
    const res = await apiRequest<{ run: ChecklistRun }>(
      `/api/checklist-runs/${encodeURIComponent(run.id)}/complete`,
      { method: 'POST' },
    )
    setRun(res.run)
  }, [run])

  const abandon = useCallback(async () => {
    if (!run) return
    await apiRequest<void>(`/api/checklist-runs/${encodeURIComponent(run.id)}`, { method: 'DELETE' })
  }, [run])

  return { run, resumed, loading, error, tick, complete, abandon }
}
