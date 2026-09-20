import { ArrowLeft, Check, TriangleAlert } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import { useChecklistRun, type ChecklistItemState } from '@/hooks/use-checklist-run'
import { cn } from '@/lib/utils'

// Plan "Notes and the Boat's Manual" §7, ADR 0118: the checklist runner -
// "a mode of the reading view, not a panel." Both notes reading surfaces
// (documents-panel.tsx's viewer Sheet and documents/manual-folder-view.tsx's
// reading pane) swap their NoteMarkdown for this component in place, rather
// than navigating anywhere, when the operator taps **Start checklist**.
// Built once here so the hit-target/resume/changed-item behaviour is
// identical wherever a checklist is run from.
//
// The numbers below are deliberate, not decoration (plan §7): one-handed
// at a helm screen in a seaway means large targets and no precision, so a
// row is min-h-14/p-4 and its checkbox h-6 w-6, both well past the 44px
// hit-target floor the rest of this app already uses.

export interface ChecklistRunnerProps {
  noteId: string
  /** Shown in the sticky header, above the progress count - which note
   * this run belongs to, since the runner replaces the reading view
   * entirely rather than sitting inside a frame that still shows a title. */
  noteTitle: string
  /** Back to the reading view - does NOT abandon the run. The run stays
   * active and GET /api/notes/:id's own active_run is how a caller decides
   * whether "Start checklist" should say "Resume" next time. */
  onExit: () => void
}

function formatClockTime(iso: string): string {
  const parsed = new Date(iso)
  if (Number.isNaN(parsed.getTime())) return ''
  return parsed.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

// mm or hh:mm elapsed since startedIso, as of nowMillis - both passed in
// (rather than read from Date.now() inside this function) so a test can
// assert an exact string instead of racing the clock.
function formatElapsed(startedIso: string, nowMillis: number): string {
  const started = new Date(startedIso).getTime()
  if (Number.isNaN(started)) return ''
  const totalMinutes = Math.max(0, Math.floor((nowMillis - started) / 60000))
  const hours = Math.floor(totalMinutes / 60)
  const minutes = totalMinutes % 60
  return hours > 0 ? `${hours}h ${minutes}m elapsed` : `${minutes}m elapsed`
}

function ChecklistItemRow({
  item,
  isFirstUnchecked,
  disabled,
  onToggle,
  registerFirstUnchecked,
}: {
  item: ChecklistItemState
  isFirstUnchecked: boolean
  disabled: boolean
  onToggle: () => void
  registerFirstUnchecked: (el: HTMLButtonElement | null) => void
}) {
  return (
    <button
      type="button"
      ref={isFirstUnchecked ? registerFirstUnchecked : undefined}
      disabled={disabled}
      aria-pressed={item.checked}
      onClick={onToggle}
      // paddingLeft carries the item's nesting depth (parseChecklistItems'
      // own Depth, notes_format.go) - a sub-step under a heading stays
      // visually indented in the runner the same way it read in the note.
      style={{ paddingLeft: `${1 + item.depth * 1.25}rem` }}
      className={cn(
        'flex min-h-14 w-full items-center gap-3 border-b border-border p-4 text-left disabled:opacity-60',
        item.checked && 'text-muted-foreground line-through',
      )}
    >
      <span
        aria-hidden="true"
        className={cn(
          'flex h-6 w-6 shrink-0 items-center justify-center rounded border-2 border-border',
          item.checked && 'border-primary bg-primary text-primary-foreground',
        )}
      >
        {item.checked && <Check className="h-4 w-4" aria-hidden="true" />}
      </span>
      <span className="flex-1 text-sm text-foreground">{item.text}</span>
    </button>
  )
}

export function ChecklistRunner({ noteId, noteTitle, onExit }: ChecklistRunnerProps) {
  const { run, resumed, loading, error, tick, complete, abandon } = useChecklistRun(noteId)
  const [actionError, setActionError] = useState<string | null>(null)
  const [now, setNow] = useState(() => Date.now())

  // Elapsed time is the one thing on this screen that changes with no
  // action of its own - a plain interval keeps the sticky header's "Xm
  // elapsed" honest without re-fetching anything.
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 15000)
    return () => clearInterval(id)
  }, [])

  const firstUncheckedRef = useRef<HTMLButtonElement | null>(null)
  const hasScrolledRef = useRef(false)
  useEffect(() => {
    if (!run || hasScrolledRef.current) return
    hasScrolledRef.current = true
    // Resuming lands on the first unchecked item (plan §7) - a fresh run
    // has no checked items at all, so this is a harmless no-op scroll to
    // the top of the list in that case.
    firstUncheckedRef.current?.scrollIntoView({ block: 'start' })
  }, [run])

  const firstUncheckedKey = useMemo(() => {
    if (!run) return null
    const item = run.items.find((candidate) => !candidate.checked)
    return item ? `${item.item_key}:${item.occurrence}` : null
  }, [run])

  const handleToggle = useCallback(async (item: ChecklistItemState) => {
    try {
      await tick(item.item_key, item.occurrence, !item.checked)
      setActionError(null)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    }
  }, [tick])

  const handleComplete = useCallback(async () => {
    try {
      await complete()
      setActionError(null)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    }
  }, [complete])

  const handleAbandon = useCallback(async () => {
    try {
      await abandon()
      onExit()
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    }
  }, [abandon, onExit])

  if (loading && !run) {
    return <p className="p-4 text-sm text-muted-foreground">Starting checklist…</p>
  }
  if (error) {
    return <p role="alert" className="p-4 text-sm text-destructive">{error}</p>
  }
  if (!run) return null

  const closed = Boolean(run.completed_at ?? run.abandoned_at)

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="sticky top-0 z-10 flex items-center gap-3 border-b border-border bg-background/95 p-3 backdrop-blur">
        <Button type="button" variant="ghost" size="sm" onClick={onExit}>
          <ArrowLeft className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
          Back
        </Button>
        <div className="flex min-w-0 flex-1 flex-col items-center">
          <span className="w-full truncate text-center text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
            {noteTitle}
          </span>
          <span className="font-display text-2xl font-bold tabular-nums tracking-tight text-gauge-primary">
            {run.checked_count} / {run.total}
          </span>
          <span className="text-[11px] text-muted-foreground">
            {resumed
              ? `Started ${formatClockTime(run.started_at)}, ${run.checked_count} of ${run.total} done`
              : `Started ${formatClockTime(run.started_at)} · ${formatElapsed(run.started_at, now)}`}
          </span>
        </div>
        {/* Balances the Back button so the KPI stack stays visually
            centred - matches nothing interactive, just width. */}
        <div className="w-[4.5rem] shrink-0" aria-hidden="true" />
      </div>

      {actionError && <p role="alert" className="p-3 text-sm text-destructive">{actionError}</p>}

      <div className="min-h-0 flex-1 overflow-auto">
        {/* run.items is rendered in the CURRENT body's own order, exactly
            as the server returned it - never re-sorted by checked state,
            so a completed row stays exactly where it was (plan §7: "the
            list must never reorder under a thumb"). */}
        <ul>
          {run.items.map((item) => (
            <li key={`${item.item_key}:${item.occurrence}`}>
              <ChecklistItemRow
                item={item}
                isFirstUnchecked={`${item.item_key}:${item.occurrence}` === firstUncheckedKey}
                disabled={closed}
                onToggle={() => { void handleToggle(item) }}
                registerFirstUnchecked={(el) => { firstUncheckedRef.current = el }}
              />
            </li>
          ))}
        </ul>

        {run.changed.length > 0 && (
          <div className="border-t border-border">
            <p className="p-3 text-[10px] font-semibold uppercase tracking-wider text-amber-600 dark:text-amber-400">
              Re-check
            </p>
            <ul>
              {run.changed.map((item) => (
                <li
                  key={`${item.item_key}:${item.occurrence}`}
                  className="flex min-h-14 items-start gap-3 border-b border-border p-4"
                >
                  <TriangleAlert className="mt-0.5 h-5 w-5 shrink-0 text-amber-600 dark:text-amber-400" aria-hidden="true" />
                  <span className="flex-1 text-sm text-muted-foreground">
                    Edited since you ticked it — re-check:{' '}
                    <span className="text-foreground">{item.text}</span>
                  </span>
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>

      <div className="flex items-center justify-between gap-2 border-t border-border p-3">
        <Button type="button" variant="ghost" size="sm" className="text-muted-foreground" disabled={closed} onClick={() => { void handleAbandon() }}>
          Abandon checklist
        </Button>
        <Button type="button" size="sm" disabled={closed} onClick={() => { void handleComplete() }}>
          Complete checklist
        </Button>
      </div>
    </div>
  )
}
