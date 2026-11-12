import { FileText } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { filingOutcome, inboxApi, inboxPreviewUrl, plural, type InboxItem } from '@/lib/inbox-api'
import { assignRequest, initialAssignRows, rowProblem, type AssignRow } from '@/lib/inbox-assign'
import { placementOptions } from '@/lib/inbox-groups'
import type { PassageSummary } from '@/lib/passages'
import { errorMessage } from '@/lib/errors'
import { SIGN_TITLE_MAX } from '@/lib/replay-signs'

export interface InboxAssignDialogProps {
  items: InboxItem[]
  passages: PassageSummary[]
  /** Mate is set up, so the suggested titles are its own and take longer. */
  mateTitles?: boolean
  onClose: () => void
}

// "Add to passage": an editable list of titles, one per photo, before any sign
// is made. Titles start as Mate's when it is set up, else the nearest named
// place to the boat when the photo was taken, or blank. A title that could not
// be made says why beside its row. A photo that is not on a passage is pinned to the start
// or end of a passage the operator picks (the nearest one to begin with).
// Mounted only while open, so each opening starts from the photos then selected.
export function InboxAssignDialog({ items, passages, mateTitles = false, onClose }: InboxAssignDialogProps) {
  const [rows, setRows] = useState<AssignRow[]>(() => initialAssignRows(items, {}, passages))
  const [titlesState, setTitlesState] = useState<'loading' | 'ready' | string>('loading')
  const [titleErrors, setTitleErrors] = useState<Readonly<Record<string, string>>>({})
  // Set when Mate's settings could not be read, so the titles below are place names.
  const [mateNotice, setMateNotice] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [failures, setFailures] = useState<Record<string, string>>({})
  const options = useMemo(() => placementOptions(passages), [passages])

  useEffect(() => {
    let cancelled = false
    inboxApi.suggestedTitles(items.map((i) => i.id)).then(
      ({ titles, errors, mateError }) => {
        if (cancelled) return
        setTitleErrors(errors)
        setMateNotice(mateError ?? null)
        // Only blank titles take a suggestion, so nothing the operator has typed is lost.
        const suggested = initialAssignRows(items, titles, passages)
        // By id: rows may have shrunk since the lookup began.
        const byId = new Map(suggested.map((r) => [r.item.id, r.title]))
        setRows((prev) => prev.map((r) => (r.title === '' && byId.has(r.item.id) ? { ...r, title: byId.get(r.item.id)! } : r)))
        setTitlesState('ready')
      },
      (err) => { if (!cancelled) setTitlesState(errorMessage(err)) },
    )
    return () => { cancelled = true }
    // The rows are made once per opening from the items then selected.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const edit = (id: string, patch: Partial<Pick<AssignRow, 'title' | 'tMs'>>) =>
    setRows((prev) => prev.map((r) => (r.item.id === id ? { ...r, ...patch } : r)))
  const problems = rows.map(rowProblem)
  const ready = rows.length > 0 && problems.every((p) => p === null)

  const confirm = async () => {
    setBusy(true)
    try {
      // A request that failed part-way still hands back what the earlier ones filed.
      const { results, error } = await filingOutcome(inboxApi.fileToPassage(assignRequest(rows)))
      const failed = results.filter((r) => !r.ok)
      const made = results.length - failed.length
      if (error !== null) {
        toast.error(made > 0 ? `Added ${plural(made, 'sign')}; the rest could not be sent` : 'Could not add to the passage', { description: error })
        // Nothing was answered: the rows stay as they were, so the operator can try again.
        if (results.length === 0) return
      } else if (made > 0) {
        toast.success(`Added ${plural(made, 'sign')}`)
      }
      if (failed.length === 0 && error === null) {
        onClose()
        return
      }
      const answered = new Set(results.map((r) => r.id))
      setFailures({
        ...(error === null ? {} : Object.fromEntries(rows.filter((r) => !answered.has(r.item.id)).map((r) => [r.item.id, error]))),
        ...Object.fromEntries(failed.map((f) => [f.id, f.error ?? 'It was not added'])),
      })
      setRows((prev) => prev.filter((r) => failed.some((f) => f.id === r.item.id) || (error !== null && !answered.has(r.item.id))))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !busy) onClose() }}>
      <DialogContent className="max-h-[90svh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Add {plural(rows.length, 'photo')} to a passage</DialogTitle>
          <DialogDescription>
            Each photo becomes a sign at the moment it was taken, shown full frame in the replay. Check the titles; each needs one.
          </DialogDescription>
        </DialogHeader>
        {titlesState === 'loading' && (
          <p role="status" className="text-xs text-muted-foreground">{mateTitles ? 'Mate is writing titles…' : 'Looking up place names…'}</p>
        )}
        {titlesState !== 'loading' && titlesState !== 'ready' && (
          <p role="alert" className="text-xs text-destructive">
            Could not {mateTitles ? 'get titles from Mate' : 'look up place names'}: {titlesState}. Type the titles yourself.
          </p>
        )}
        {mateNotice && <p className="text-xs text-muted-foreground" data-testid="inbox-titles-mate-error">{mateNotice}</p>}
        <ul className="flex flex-col gap-3">
          {rows.map((row, i) => {
            const problem = problems[i]
            const error = failures[row.item.id]
            const needsPlace = row.item.group.kind !== 'passage'
            return (
              <li key={row.item.id} className="flex min-w-0 gap-3">
                {inboxPreviewUrl(row.item) ? (
                  <img src={inboxPreviewUrl(row.item)} alt="" loading="lazy" className="h-16 w-16 shrink-0 rounded-md object-cover" />
                ) : (
                  <span className="flex h-16 w-16 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
                    <FileText className="h-6 w-6" aria-hidden="true" />
                  </span>
                )}
                <div className="flex min-w-0 flex-1 flex-col gap-1.5">
                  <Input
                    aria-label={`Title for ${row.item.filename}`}
                    value={row.title}
                    maxLength={SIGN_TITLE_MAX}
                    placeholder="Title"
                    onChange={(e) => edit(row.item.id, { title: e.target.value })}
                  />
                  {needsPlace && (
                    <select
                      aria-label={`Where ${row.item.filename} goes`}
                      className="h-9 w-full min-w-0 rounded-md border border-input bg-background px-2 text-sm"
                      value={row.tMs === null ? '' : String(row.tMs)}
                      onChange={(e) => edit(row.item.id, { tMs: e.target.value === '' ? null : Number(e.target.value) })}
                    >
                      <option value="">Choose where it goes…</option>
                      {options.map((o) => <option key={`${o.passageId}:${o.edge}`} value={o.tMs}>{o.label}</option>)}
                    </select>
                  )}
                  {error ? (
                    <p role="alert" className="text-xs text-destructive">{error}</p>
                  ) : titleErrors[row.item.id] && row.title === '' ? (
                    <p className="text-xs text-destructive">{titleErrors[row.item.id]}</p>
                  ) : problem && row.title !== '' ? (
                    <p className="text-xs text-muted-foreground">{problem}</p>
                  ) : null}
                </div>
              </li>
            )
          })}
        </ul>
        <DialogFooter className="gap-2">
          <Button type="button" variant="outline" disabled={busy} onClick={onClose}>Cancel</Button>
          <Button type="button" disabled={!ready || busy} onClick={() => void confirm()}>
            {busy ? 'Adding…' : rows.length === 1 ? 'Add sign' : `Add ${rows.length} signs`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
