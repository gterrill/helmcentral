import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { KIND_LABELS, PartsEditor } from '@/components/inventory/maintenance-log-dialogs'
import { PhotoStripEditor } from '@/components/inventory/photo-strip-editor'
import { apiBaseUrl } from '@/config/api'
import { useEquipment } from '@/hooks/use-inventory'
import type { MaintenanceLogKind, MaintenanceLogPartInput } from '@/hooks/use-maintenance'
import {
  completeOpenJob,
  createOpenJob,
  deleteOpenJobPhoto,
  dropOpenJob,
  updateOpenJob,
  uploadOpenJobPhoto,
  type OpenJob,
  type OpenJobInput,
} from '@/hooks/use-open-jobs'
import { useUnsavedWorkReport } from '@/hooks/use-navigation-guard'
import { errorMessage } from '@/lib/errors'
import { STARTED_AFTER_REPAIR, startedAfterRepair, todayISO } from '@/lib/local-date'

// ADR 0269: the three dialogs behind the Open jobs list. Add/edit takes the
// job's own fields and then its photos (a job needs an id before a photo can
// link to it, so the strip shows once Save has made one, the same two steps
// the service log entry takes). Done is the service log form. Drop asks for an
// optional reason.

const NO_ITEM = '__none__'

function photoUrl(id: string): string {
  return `${apiBaseUrl}/api/documents/${encodeURIComponent(id)}/content`
}

// ── add / edit ──────────────────────────────────────────────────────────

interface OpenJobFormState {
  equipmentId: string
  title: string
  notes: string
  beforeOn: string
}

function formFor(job: OpenJob | null, presetEquipmentId: string | undefined): OpenJobFormState {
  return {
    equipmentId: job ? (job.equipment_id ?? '') : (presetEquipmentId ?? ''),
    title: job?.title ?? '',
    notes: job?.notes ?? '',
    beforeOn: job?.before_on ?? '',
  }
}

interface OpenJobDialogProps {
  open: boolean
  /** The job being edited; null or undefined when adding one. */
  job: OpenJob | null | undefined
  /** The item a new job starts on (an item's own page). */
  presetEquipmentId?: string
  onClose: () => void
}

export function OpenJobDialog({ open, job, presetEquipmentId, onClose }: OpenJobDialogProps) {
  const [form, setForm] = useState<OpenJobFormState>(() => formFor(job ?? null, presetEquipmentId))
  const [saved, setSaved] = useState<OpenJob | null>(job ?? null)
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const { items } = useEquipment(open ? {} : null)
  useUnsavedWorkReport('open-job', open && (dirty || (saved === null && form.title.trim() !== '')))

  useEffect(() => {
    if (open) {
      setForm(formFor(job ?? null, presetEquipmentId))
      setSaved(job ?? null)
      setDirty(false)
      setError(null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, job?.id])

  const change = (patch: Partial<OpenJobFormState>) => {
    setForm((prev) => ({ ...prev, ...patch }))
    setDirty(true)
  }

  const handleSave = async () => {
    setSaving(true)
    setError(null)
    try {
      const input: OpenJobInput = {
        equipment_id: form.equipmentId === '' ? null : form.equipmentId,
        title: form.title.trim(),
        notes: form.notes.trim(),
        before_on: form.beforeOn,
      }
      // A second Save on the same open dialog edits the job the first made.
      setSaved(saved ? await updateOpenJob(saved.id, input) : await createOpenJob(input))
      setDirty(false)
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setSaving(false)
    }
  }

  const handleFilesPicked = async (files: File[]) => {
    if (!saved) return
    setError(null)
    try {
      let latest = saved
      for (const file of files) latest = await uploadOpenJobPhoto(saved.id, file, file.name)
      setSaved(latest)
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  const handleRemovePhoto = async (documentId: string, deletePhoto: boolean) => {
    if (!saved) return
    setError(null)
    try {
      setSaved(await deleteOpenJobPhoto(saved.id, documentId, deletePhoto))
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next) onClose() }}>
      <DialogContent className="max-h-[85vh] max-w-lg overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{job ? 'Edit job' : 'Add job'}</DialogTitle>
          <DialogDescription>
            A problem you know about and have not fixed yet. Done writes it to the service log;
            Drop closes it without a log line.
          </DialogDescription>
        </DialogHeader>

        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="open-job-title">Title</FieldLabel>
            <Input
              id="open-job-title"
              value={form.title}
              onChange={(e) => change({ title: e.target.value })}
              placeholder="Leaks at the outlet"
              maxLength={200}
              autoFocus
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="open-job-item">Item</FieldLabel>
            <Select value={form.equipmentId === '' ? NO_ITEM : form.equipmentId} onValueChange={(v) => change({ equipmentId: v === NO_ITEM ? '' : (v ?? '') })}>
              <SelectTrigger id="open-job-item" aria-label="Item">
                <SelectValue>
                  {(value: string) => (value === NO_ITEM ? 'No item' : (items.find((i) => i.id === value)?.name ?? value))}
                </SelectValue>
              </SelectTrigger>
              <SelectPopup>
                <SelectItem value={NO_ITEM}>No item</SelectItem>
                {items.map((item) => (
                  <SelectItem key={item.id} value={item.id}>{item.name}</SelectItem>
                ))}
              </SelectPopup>
            </Select>
          </Field>
          <Field>
            <FieldLabel htmlFor="open-job-before">Before</FieldLabel>
            <Input id="open-job-before" type="date" value={form.beforeOn} onChange={(e) => change({ beforeOn: e.target.value })} />
            <FieldDescription>Optional. The date you want it fixed by.</FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor="open-job-notes">Notes</FieldLabel>
            <Textarea id="open-job-notes" rows={3} value={form.notes} onChange={(e) => change({ notes: e.target.value })} maxLength={4000} />
          </Field>
          {saved && (
            <Field>
              <FieldLabel>Photos</FieldLabel>
              <PhotoStripEditor
                photos={saved.photo_ids.map((id) => ({ id, previewUrl: photoUrl(id), isCover: false }))}
                onFilesPicked={(files) => { void handleFilesPicked(files) }}
                onMakeCover={() => {}}
                onRemove={(id, del) => { void handleRemovePhoto(id, del) }}
              />
            </Field>
          )}
        </FieldGroup>
        {error && <FieldError errors={[{ message: error }]} />}

        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose} disabled={saving}>
            {saved && !dirty ? 'Close' : 'Cancel'}
          </Button>
          {(!saved || dirty) && (
            <Button type="button" disabled={saving || form.title.trim() === ''} onClick={() => { void handleSave() }}>
              {saving ? 'Saving...' : 'Save'}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ── done ────────────────────────────────────────────────────────────────

interface DoneFormState {
  performedAt: string
  /** A repair's "started showing" date, blank for none. */
  startedAt: string
  kind: MaintenanceLogKind
  hours: string
  description: string
  who: string
  cost: string
  currency: string
  parts: MaintenanceLogPartInput[]
}

/** The day the job was opened is the first guess at when the fault started showing. */
function blankDoneForm(job?: OpenJob | null): DoneFormState {
  return { performedAt: todayISO(), startedAt: job?.opened_on ?? '', kind: 'repair', hours: '', description: '', who: '', cost: '', currency: '', parts: [] }
}

interface OpenJobDoneDialogProps {
  job: OpenJob | null
  onClose: () => void
}

/** The service log form, for a job that is fixed. Marking it done writes one
 * line to the service log and closes the job. */
export function OpenJobDoneDialog({ job, onClose }: OpenJobDoneDialogProps) {
  const [form, setForm] = useState<DoneFormState>(blankDoneForm)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [logged, setLogged] = useState(false)

  const { items } = useEquipment(job !== null ? {} : null)
  useUnsavedWorkReport('open-job-done', job !== null && !logged)

  useEffect(() => {
    if (job) {
      setForm(blankDoneForm(job))
      setError(null)
      setLogged(false)
    }
  }, [job])

  const update = (patch: Partial<DoneFormState>) => setForm((prev) => ({ ...prev, ...patch }))

  // The job's open date is only a first guess, so a repair date before it is the
  // operator's to fix here, not a refusal from the server on a field they never touched.
  const startedTooLate = startedAfterRepair(form.kind, form.startedAt, form.performedAt)

  const handleSave = async () => {
    if (!job || startedTooLate) return
    setSaving(true)
    setError(null)
    try {
      await completeOpenJob(job.id, {
        performed_at: form.performedAt,
        // Only a repair has a fault to date; the server refuses it on anything else.
        started_at: form.kind === 'repair' ? form.startedAt : '',
        kind: form.kind,
        hours: form.hours.trim() === '' ? null : Number(form.hours),
        description: form.description.trim(),
        who: form.who.trim(),
        cost: form.cost.trim() === '' ? null : Number(form.cost),
        currency: form.currency.trim(),
        parts: form.parts,
      })
      setLogged(true)
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={job !== null} onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="max-h-[85vh] max-w-lg overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Done: {job?.title}</DialogTitle>
          <DialogDescription>
            Writes one line to the service log and closes this job. Any photos on the job go on the line too.
          </DialogDescription>
        </DialogHeader>

        {logged ? (
          <>
            <p className="text-sm text-muted-foreground">Logged in the service log. The job is closed.</p>
            <DialogFooter>
              <Button type="button" onClick={onClose}>Close</Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <FieldGroup>
              <div className="grid grid-cols-2 gap-3">
                <Field>
                  <FieldLabel htmlFor="open-job-done-date">Date</FieldLabel>
                  <Input id="open-job-done-date" type="date" value={form.performedAt} onChange={(e) => update({ performedAt: e.target.value })} />
                </Field>
                <Field>
                  <FieldLabel htmlFor="open-job-done-kind">Kind</FieldLabel>
                  <Select value={form.kind} onValueChange={(v) => update({ kind: v as MaintenanceLogKind })}>
                    <SelectTrigger id="open-job-done-kind" aria-label="Kind">
                      <SelectValue>{(value: string) => KIND_LABELS[value as MaintenanceLogKind]}</SelectValue>
                    </SelectTrigger>
                    <SelectPopup>
                      <SelectItem value="repair">Repair</SelectItem>
                      <SelectItem value="improvement">Improvement</SelectItem>
                      <SelectItem value="maintenance">Maintenance</SelectItem>
                    </SelectPopup>
                  </Select>
                </Field>
              </div>
              {form.kind === 'repair' && (
                <Field>
                  <FieldLabel htmlFor="open-job-done-started">Started showing</FieldLabel>
                  <Input
                    id="open-job-done-started"
                    type="date"
                    max={form.performedAt || undefined}
                    value={form.startedAt}
                    onChange={(e) => update({ startedAt: e.target.value })}
                  />
                  <FieldDescription>
                    When the fault first showed. On an engine, the house battery or what charges it, readings from then until this repair are left out of what counts as normal.
                  </FieldDescription>
                  {startedTooLate && <FieldError errors={[{ message: STARTED_AFTER_REPAIR }]} />}
                </Field>
              )}
              {job?.equipment_id && (
                <Field>
                  <FieldLabel htmlFor="open-job-done-hours">Hours (gauge reading)</FieldLabel>
                  <Input id="open-job-done-hours" type="number" inputMode="decimal" value={form.hours} onChange={(e) => update({ hours: e.target.value })} />
                </Field>
              )}
              <Field>
                <FieldLabel htmlFor="open-job-done-notes">Notes</FieldLabel>
                <Textarea
                  id="open-job-done-notes"
                  rows={3}
                  value={form.description}
                  onChange={(e) => update({ description: e.target.value })}
                  placeholder={job?.title}
                />
                <FieldDescription>What you did. Left blank, the line reads as the job's title.</FieldDescription>
              </Field>
              <div className="grid grid-cols-3 gap-3">
                <Field>
                  <FieldLabel htmlFor="open-job-done-who">Who</FieldLabel>
                  <Input id="open-job-done-who" value={form.who} onChange={(e) => update({ who: e.target.value })} />
                </Field>
                <Field>
                  <FieldLabel htmlFor="open-job-done-cost">Cost</FieldLabel>
                  <Input id="open-job-done-cost" type="number" inputMode="decimal" min={0} value={form.cost} onChange={(e) => update({ cost: e.target.value })} />
                </Field>
                <Field>
                  <FieldLabel htmlFor="open-job-done-currency">Currency</FieldLabel>
                  <Input id="open-job-done-currency" value={form.currency} onChange={(e) => update({ currency: e.target.value })} placeholder="AUD" />
                </Field>
              </div>
              <PartsEditor parts={form.parts} onChange={(parts) => update({ parts })} items={items} />
            </FieldGroup>
            {error && <FieldError errors={[{ message: error }]} />}
            <DialogFooter>
              <Button type="button" variant="outline" onClick={onClose} disabled={saving}>Cancel</Button>
              <Button type="button" disabled={saving || form.performedAt === '' || startedTooLate} onClick={() => { void handleSave() }}>
                {saving ? 'Saving...' : 'Mark done'}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}

// ── drop ────────────────────────────────────────────────────────────────

interface OpenJobDropDialogProps {
  job: OpenJob | null
  onClose: () => void
}

/** Closes a job without a log line. The reason is optional; Enter in it drops
 * the job. */
export function OpenJobDropDialog({ job, onClose }: OpenJobDropDialogProps) {
  const [reason, setReason] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setReason('')
    setError(null)
  }, [job])

  const handleDrop = async () => {
    if (!job) return
    setSaving(true)
    setError(null)
    try {
      await dropOpenJob(job.id, reason.trim())
      onClose()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={job !== null} onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Drop: {job?.title}</DialogTitle>
          <DialogDescription>
            Closes the job without a service log line, for something that fixed itself, was
            never a problem or is no longer worth doing.
          </DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="open-job-drop-reason">Reason</FieldLabel>
          <Input
            id="open-job-drop-reason"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter' && !saving) void handleDrop() }}
            placeholder="Optional"
            autoFocus
          />
        </Field>
        {error && <FieldError errors={[{ message: error }]} />}
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose} disabled={saving}>Cancel</Button>
          <Button type="button" disabled={saving} onClick={() => { void handleDrop() }}>
            {saving ? 'Dropping...' : 'Drop job'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
