import { useCallback, useEffect, useRef, useState } from 'react'
import { apiBaseUrl } from '@/config/api'

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

// Copied from use-notes.ts's own submitJSON rather than imported/shared,
// for the same reason that file gives for not importing use-documents.ts's
// copy: it isn't exported there, and a small helper is cheaper to
// duplicate than to introduce a shared module between features that
// otherwise don't know about each other. The one difference from both:
// DELETE .../checklist-runs/:runId answers 204 with no body
// (abandonChecklistRunHandler, checklist_runs_handlers.go), which a bare
// `response.json()` would throw on.
async function submitJSON<T>(url: string, method: string, body?: unknown): Promise<T> {
  const response = await fetch(url, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (!response.ok) {
    const payload = (await response.json().catch(() => ({}))) as { error?: string }
    throw new Error(payload.error ?? `HTTP ${response.status}`)
  }
  if (response.status === 204) return undefined as T
  return (await response.json()) as T
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
    submitJSON<{ run: ChecklistRun; resumed: boolean }>(
      `${apiBaseUrl}/api/notes/${encodeURIComponent(noteId)}/checklist-runs`,
      'POST',
    )
      .then((res) => {
        if (seq !== seqRef.current) return
        setRun(res.run)
        setResumed(res.resumed)
        setError(null)
      })
      .catch((err) => {
        if (seq !== seqRef.current) return
        setError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => {
        if (seq === seqRef.current) setLoading(false)
      })
  }, [noteId])

  const tick = useCallback(async (itemKey: string, occurrence: number, checked: boolean) => {
    if (!run) return
    const res = await submitJSON<{ run: ChecklistRun }>(
      `${apiBaseUrl}/api/checklist-runs/${encodeURIComponent(run.id)}/items`,
      'PATCH',
      { item_key: itemKey, occurrence, checked },
    )
    setRun(res.run)
  }, [run])

  const complete = useCallback(async () => {
    if (!run) return
    const res = await submitJSON<{ run: ChecklistRun }>(
      `${apiBaseUrl}/api/checklist-runs/${encodeURIComponent(run.id)}/complete`,
      'POST',
    )
    setRun(res.run)
  }, [run])

  const abandon = useCallback(async () => {
    if (!run) return
    await submitJSON<void>(`${apiBaseUrl}/api/checklist-runs/${encodeURIComponent(run.id)}`, 'DELETE')
  }, [run])

  return { run, resumed, loading, error, tick, complete, abandon }
}
