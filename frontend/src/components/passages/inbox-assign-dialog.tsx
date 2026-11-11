import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { inboxApi, type InboxItem } from '@/lib/inbox-api'
import { assignRequest, initialAssignRows, rowProblem, type AssignRow } from '@/lib/inbox-assign'
import { placementOptions } from '@/lib/inbox-groups'
import type { PassageSummary } from '@/lib/passages'
import { errorMessage } from '@/lib/errors'
import { SIGN_TITLE_MAX } from '@/lib/replay-signs'

export interface InboxAssignDialogProps {
  items: InboxItem[]
  passages: PassageSummary[]
  onClose: () => void
}

// "Add to passage": an editable list of titles, one per photo, before any sign
// is made. Titles start as the nearest named place to the boat when the photo
// was taken, or blank. A photo that is not on a passage is pinned to the start
// or end of a passage the operator picks (the nearest one to begin with).
// Mounted only while open, so each opening starts from the photos then selected.
export function InboxAssignDialog({ items, passages, onClose }: InboxAssignDialogProps) {
  const [rows, setRows] = useState<AssignRow[]>(() => initialAssignRows(items, {}, passages))
  const [titlesState, setTitlesState] = useState<'loading' | 'ready' | string>('loading')
  const [busy, setBusy] = useState(false)
  const [failures, setFailures] = useState<Record<string, string>>({})
  const options = useMemo(() => placementOptions(passages), [passages])

  useEffect(() => {
    let cancelled = false
    inboxApi.suggestedTitles(items.map((i) => i.id)).then(
      (titles) => {
        if (cancelled) return
        // Only blank titles take a suggestion, so nothing the operator has typed is lost.
        const suggested = initialAssignRows(items, titles, passages)
        setRows((prev) => prev.map((r, i) => (r.title === '' ? { ...r, title: suggested[i].title } : r)))
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
      const results = await inboxApi.fileToPassage(assignRequest(rows))
      const failed = results.filter((r) => !r.ok)
      const made = results.length - failed.length
      if (made > 0) toast.success(made === 1 ? 'Added 1 sign' : `Added ${made} signs`)
      if (failed.length === 0) {
        onClose()
        return
      }
      setFailures(Object.fromEntries(failed.map((f) => [f.id, f.error ?? 'It was not added'])))
      setRows((prev) => prev.filter((r) => failed.some((f) => f.id === r.item.id)))
    } catch (err) {
      toast.error('Could not add to the passage', { description: errorMessage(err) })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !busy) onClose() }}>
      <DialogContent className="max-h-[90svh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Add {rows.length === 1 ? '1 photo' : `${rows.length} photos`} to a passage</DialogTitle>
          <DialogDescription>
            Each photo becomes a sign at the moment it was taken, shown full frame in the replay. Check the titles; each needs one.
          </DialogDescription>
        </DialogHeader>
        {titlesState === 'loading' && <p role="status" className="text-xs text-muted-foreground">Looking up place names…</p>}
        {titlesState !== 'loading' && titlesState !== 'ready' && (
          <p role="alert" className="text-xs text-destructive">Could not look up place names: {titlesState}. Type the titles yourself.</p>
        )}
        <ul className="flex flex-col gap-3">
          {rows.map((row, i) => {
            const problem = problems[i]
            const error = failures[row.item.id]
            const needsPlace = row.item.group.kind !== 'passage'
            return (
              <li key={row.item.id} className="flex min-w-0 gap-3">
                <img src={row.item.content_url} alt="" loading="lazy" className="h-16 w-16 shrink-0 rounded-md object-cover" />
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
