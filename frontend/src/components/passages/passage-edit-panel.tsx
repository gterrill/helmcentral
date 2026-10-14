import { ChevronDown, EyeOff, GitMerge, Scissors } from 'lucide-react'
import { useEffect, useRef, useState, type MutableRefObject } from 'react'
import { toast } from 'sonner'

import { NameCruiseDialog } from '@/components/passages/name-cruise-dialog'
import type { ReplayCursor } from '@/components/passages/replay-panel'
import { ConfirmDelete, FormSection } from '@/components/patterns'
import { useNavigationGuard } from '@/hooks/use-navigation-guard'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { passagesApi } from '@/lib/passages-api'
import { passageHeading, type CruiseRecord, type PassageDetail, type PassageRecord } from '@/lib/passages'

function failure(action: string, err: unknown) {
  toast.error(`Could not ${action}`, { description: err instanceof Error ? err.message : String(err) })
}

export interface PassageEditPanelProps {
  passage: PassageDetail
  cruises: CruiseRecord[]
  /** The next older and next newer visible passages, for Merge. */
  olderNeighbour: PassageRecord | null
  newerNeighbour: PassageRecord | null
  cursorRef: MutableRefObject<ReplayCursor | null>
  canWrite: boolean
  onChanged: () => void
  onMerged: (id: string) => void
  onHidden: () => void
}

export function PassageEditPanel({
  passage, cruises, olderNeighbour, newerNeighbour, cursorRef, canWrite, onChanged, onMerged, onHidden,
}: PassageEditPanelProps) {
  const [title, setTitle] = useState(passage.title)
  const [fromName, setFromName] = useState(passage.from_name)
  const [toName, setToName] = useState(passage.to_name)
  const [notes, setNotes] = useState(passage.notes)
  const [saving, setSaving] = useState(false)
  const [busy, setBusy] = useState(false)
  const [hideOpen, setHideOpen] = useState(false)
  const [newCruiseOpen, setNewCruiseOpen] = useState(false)
  const gate = useRef(false)

  // The record can change underneath (a split clears the first half's To).
  // Fields the operator has not touched follow it; fields they have typed in
  // are left alone.
  const [base, setBase] = useState({ title: passage.title, from: passage.from_name, to: passage.to_name, notes: passage.notes })
  useEffect(() => {
    const next = { title: passage.title, from: passage.from_name, to: passage.to_name, notes: passage.notes }
    if (next.title === base.title && next.from === base.from && next.to === base.to && next.notes === base.notes) return
    if (title === base.title) setTitle(next.title)
    if (fromName === base.from) setFromName(next.from)
    if (toName === base.to) setToName(next.to)
    if (notes === base.notes) setNotes(next.notes)
    setBase(next)
  }, [passage.title, passage.from_name, passage.to_name, passage.notes]) // eslint-disable-line react-hooks/exhaustive-deps

  const dirty = title !== passage.title || fromName !== passage.from_name || toName !== passage.to_name || notes !== passage.notes

  // One edit at a time: a second click while a request is out would act on a
  // record the first is about to change.
  const run = async (action: string, fn: () => Promise<void>) => {
    if (gate.current) return
    gate.current = true
    setBusy(true)
    try {
      await fn()
    } catch (err) {
      failure(action, err)
    } finally {
      gate.current = false
      setBusy(false)
    }
  }

  const persist = async () => {
    await passagesApi.patch(passage.id, { title, from_name: fromName, to_name: toName, notes })
    onChanged()
  }

  // Unsaved edits hold any move off this passage. Staying on it (a Split
  // or Trim re-lands here) is not a move away.
  useNavigationGuard(dirty
    ? {
        owner: 'passage-details',
        label: 'Details',
        blocks: (_from, to) => (
          to.panel === 'passages' && to.route.view === 'passage' && to.route.id === passage.id ? null : { kind: 'dirty' }
        ),
        save: persist,
      }
    : null)

  const save = async () => {
    setSaving(true)
    try {
      await persist()
    } catch (err) {
      failure('save passage', err)
    } finally {
      setSaving(false)
    }
  }

  const cursorTime = (): string | null => {
    const c = cursorRef.current
    if (!c || c.id !== passage.id) return null
    return new Date(c.at * 1000).toISOString()
  }
  const withCursor = (action: string, fn: (at: string) => Promise<void>) => {
    const at = cursorTime()
    if (!at) {
      toast.error(`Could not ${action}`, { description: 'Move the replay to the point you mean first.' })
      return
    }
    void run(action, () => fn(at))
  }

  const move = (cruiseId: string | null) =>
    run('move passage', async () => {
      await passagesApi.patch(passage.id, { cruise_id: cruiseId })
      onChanged()
    })

  const disabled = !canWrite || busy
  const editDisabled = disabled || dirty

  return (
    <>
      <FormSection
        title="Details"
        action={
          <Button type="button" size="sm" disabled={!canWrite || !dirty || saving} onClick={() => void save()}>
            {saving ? 'Saving…' : 'Save'}
          </Button>
        }
      >
        <div className="flex flex-col gap-2">
          <Label htmlFor="passage-title">Title</Label>
          <Input id="passage-title" value={title} placeholder={passageHeading({ ...passage, title: '' })} disabled={!canWrite} onChange={(e) => setTitle(e.target.value)} />
        </div>
        <div className="grid grid-cols-2 gap-2">
          <div className="flex min-w-0 flex-col gap-2">
            <Label htmlFor="passage-from">From</Label>
            <Input id="passage-from" value={fromName} disabled={!canWrite} onChange={(e) => setFromName(e.target.value)} />
          </div>
          <div className="flex min-w-0 flex-col gap-2">
            <Label htmlFor="passage-to">To</Label>
            <Input id="passage-to" value={toName} disabled={!canWrite} onChange={(e) => setToName(e.target.value)} />
          </div>
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="passage-notes">Notes</Label>
          <Textarea id="passage-notes" rows={5} value={notes} disabled={!canWrite} onChange={(e) => setNotes(e.target.value)} />
        </div>
      </FormSection>

      <FormSection
        title="Edit passage"
        description={dirty
          ? 'Save your changes before splitting, trimming or merging.'
          : 'Split and trim act on the point the replay is showing.'}
      >
        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" size="sm" disabled={editDisabled} onClick={() =>
            withCursor('split passage', async (at) => { await passagesApi.split(passage.id, at); onChanged() })}>
            <Scissors className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
            Split here
          </Button>
          <Button type="button" variant="outline" size="sm" disabled={editDisabled} onClick={() =>
            withCursor('trim passage', async (at) => { await passagesApi.trim(passage.id, at, passage.end); onChanged() })}>
            Trim start to here
          </Button>
          <Button type="button" variant="outline" size="sm" disabled={editDisabled} onClick={() =>
            withCursor('trim passage', async (at) => { await passagesApi.trim(passage.id, passage.start, at); onChanged() })}>
            Trim end to here
          </Button>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" size="sm" disabled={editDisabled || !olderNeighbour} onClick={() =>
            olderNeighbour && void run('merge passages', async () => {
              const merged = await passagesApi.merge(olderNeighbour.id, passage.id)
              onMerged(merged.id)
            })}>
            <GitMerge className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
            Merge with previous
          </Button>
          <Button type="button" variant="outline" size="sm" disabled={editDisabled || !newerNeighbour} onClick={() =>
            newerNeighbour && void run('merge passages', async () => {
              const merged = await passagesApi.merge(passage.id, newerNeighbour.id)
              onMerged(merged.id)
            })}>
            <GitMerge className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
            Merge with next
          </Button>
        </div>
        <div className="flex flex-wrap gap-2">
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button type="button" variant="outline" size="sm" disabled={disabled}>
                  Move to cruise
                  <ChevronDown className="h-4 w-4" data-icon="inline-end" aria-hidden="true" />
                </Button>
              }
            />
            <DropdownMenuContent align="start">
              {cruises.map((c) => (
                <DropdownMenuItem key={c.id} disabled={c.id === passage.cruise_id} onClick={() => void move(c.id)}>
                  {c.name}
                </DropdownMenuItem>
              ))}
              {cruises.length > 0 && <DropdownMenuSeparator />}
              <DropdownMenuItem onClick={() => setNewCruiseOpen(true)}>New cruise…</DropdownMenuItem>
              <DropdownMenuItem disabled={passage.cruise_id === null} onClick={() => void move(null)}>Remove from cruise</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <Button type="button" variant="outline" size="sm" disabled={disabled} onClick={() => setHideOpen(true)}>
            <EyeOff className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
            Hide passage
          </Button>
        </div>
      </FormSection>

      <ConfirmDelete
        open={hideOpen}
        onOpenChange={setHideOpen}
        title="Hide this passage?"
        description="It leaves the list and does not come back by itself. The recorded track stays in the log."
        confirmLabel="Hide passage"
        deleting={busy}
        onConfirm={() => void run('hide passage', async () => {
          await passagesApi.patch(passage.id, { hidden: true })
          setHideOpen(false)
          onHidden()
        })}
      />
      <NameCruiseDialog
        open={newCruiseOpen}
        onOpenChange={setNewCruiseOpen}
        title="New cruise"
        confirmLabel="Create and move"
        busy={busy}
        onConfirm={(name) => void run('create cruise', async () => {
          const cruise = await passagesApi.createCruise(name)
          await passagesApi.patch(passage.id, { cruise_id: cruise.id })
          setNewCruiseOpen(false)
          onChanged()
        })}
      />
    </>
  )
}
