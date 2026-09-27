import { useEffect, useState } from 'react'
import { BookOpen, Plus, X } from 'lucide-react'

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
import { PhotoStripEditor } from '@/components/inventory/photo-strip-editor'
import { apiBaseUrl } from '@/config/api'
import { useEquipment, type EquipmentItem } from '@/hooks/use-inventory'
import {
  deleteMaintenanceLogPhoto,
  hoursAsOfLabel,
  uploadMaintenanceLogPhoto,
  type MaintenanceLogEntry,
  type MaintenanceLogEntryInput,
  type MaintenanceLogKind,
  type MaintenanceLogPartInput,
  type MaintenanceRule,
} from '@/hooks/use-maintenance'
import { documentViewerHref } from '@/lib/document-citation'
import { addMonthsISO, todayISO } from '@/lib/local-date'

// ADR 0138: the service log's own two write dialogs - completing a rule
// (spec §6, always kind='maintenance') and a standalone entry (repair,
// improvement, against an item with no rule). Both write the same shape
// (date, hours, description, who, cost, currency, parts, photos), so the
// parts editor and the photo strip below are shared; the two exported
// components differ only in which fields they actually show (a standalone
// entry also picks its own kind) and what they call to save.
//
// Photos follow the same two-phase pattern equipment photos already use for
// a brand new draft (equipment-editor.tsx): a log entry needs its own id
// before a photo can link to it, so the strip only appears once Save has
// produced one - "Save" first, then "Add photos" while the dialog stays
// open, then "Done".

interface PartsEditorProps {
  parts: MaintenanceLogPartInput[]
  onChange: (parts: MaintenanceLogPartInput[]) => void
  items: EquipmentItem[]
}

function PartsEditor({ parts, onChange, items }: PartsEditorProps) {
  const [pickerId, setPickerId] = useState('')
  const [quantity, setQuantity] = useState('1')

  const nameFor = (id: string) => items.find((i) => i.id === id)?.name ?? id

  const addPart = () => {
    if (!pickerId) return
    const qty = Number(quantity) || 1
    const existing = parts.find((p) => p.equipment_id === pickerId)
    if (existing) {
      onChange(parts.map((p) => (p.equipment_id === pickerId ? { ...p, quantity: p.quantity + qty } : p)))
    } else {
      onChange([...parts, { equipment_id: pickerId, quantity: qty }])
    }
    setPickerId('')
    setQuantity('1')
  }

  return (
    <Field>
      <FieldLabel>Parts used</FieldLabel>
      <div className="flex flex-col gap-2">
        {parts.map((p) => (
          <div key={p.equipment_id} className="flex items-center justify-between gap-2 rounded-md border border-border px-2 py-1.5 text-sm">
            <span className="min-w-0 truncate">{nameFor(p.equipment_id)}</span>
            <div className="flex shrink-0 items-center gap-2">
              <span className="text-muted-foreground tabular-nums">x{p.quantity}</span>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={`Remove ${nameFor(p.equipment_id)}`}
                onClick={() => onChange(parts.filter((x) => x.equipment_id !== p.equipment_id))}
              >
                <X className="h-3.5 w-3.5" aria-hidden="true" />
              </Button>
            </div>
          </div>
        ))}
        <div className="flex items-center gap-2">
          <Select value={pickerId} onValueChange={(value) => setPickerId(value ?? '')}>
            <SelectTrigger aria-label="Add a part" className="h-9 min-w-0 flex-1">
              <SelectValue>{(value: string) => (value ? nameFor(value) : 'Add a part...')}</SelectValue>
            </SelectTrigger>
            <SelectPopup>
              {items.map((item) => (
                <SelectItem key={item.id} value={item.id}>{item.name}</SelectItem>
              ))}
            </SelectPopup>
          </Select>
          <Input
            type="number"
            inputMode="numeric"
            min={1}
            className="h-9 w-16"
            aria-label="Quantity"
            value={quantity}
            onChange={(e) => setQuantity(e.target.value)}
          />
          <Button type="button" variant="outline" size="icon" aria-label="Add part" disabled={!pickerId} onClick={addPart}>
            <Plus className="h-4 w-4" aria-hidden="true" />
          </Button>
        </div>
      </div>
    </Field>
  )
}

interface LogFormState {
  performedAt: string
  hours: string
  description: string
  who: string
  cost: string
  currency: string
  parts: MaintenanceLogPartInput[]
}

function blankForm(prefillHours?: number | null): LogFormState {
  return {
    performedAt: todayISO(),
    hours: prefillHours != null ? String(prefillHours) : '',
    description: '',
    who: '',
    cost: '',
    currency: '',
    parts: [],
  }
}

function formFromEntry(entry: MaintenanceLogEntry): LogFormState {
  return {
    performedAt: entry.performed_at,
    hours: entry.hours != null ? String(entry.hours) : '',
    description: entry.description,
    who: entry.who,
    cost: entry.cost != null ? String(entry.cost) : '',
    currency: entry.currency,
    parts: entry.parts.map((p) => ({ equipment_id: p.equipment_id, quantity: p.quantity })),
  }
}

function toInput(form: LogFormState): Omit<MaintenanceLogEntryInput, 'equipment_id' | 'kind'> {
  return {
    performed_at: form.performedAt,
    hours: form.hours.trim() === '' ? null : Number(form.hours),
    description: form.description.trim(),
    who: form.who.trim(),
    cost: form.cost.trim() === '' ? null : Number(form.cost),
    currency: form.currency.trim(),
    parts: form.parts,
  }
}

async function uploadPhotoFiles(logEntryId: string, files: File[]): Promise<MaintenanceLogEntry> {
  let latest: MaintenanceLogEntry | null = null
  for (const file of files) {
    latest = await uploadMaintenanceLogPhoto(logEntryId, file, file.name)
  }
  if (!latest) throw new Error('no files to upload')
  return latest
}

// ── complete a rule ─────────────────────────────────────────────────────

interface MaintenanceCompleteDialogProps {
  rule: MaintenanceRule | null
  onCancel: () => void
  onComplete: (input: Omit<MaintenanceLogEntryInput, 'equipment_id' | 'kind'>) => Promise<{ rule: MaintenanceRule; entry: MaintenanceLogEntry }>
}

// requiresHoursNow/requiresNewDueDateNow mirror the backend's own two
// completion-time requirements (completeMaintenanceRuleHandler,
// maintenance_handlers.go) client-side, so the operator sees why Complete
// is disabled rather than submitting and getting a 400 back: hours are
// required whenever the rule runs on hours at all (live or typed in from
// the gauge), and a fixed-due-date rule with no interval_months has no
// formula to advance its own due date from, so the operator's own next
// date is required.
function requiresHoursNow(rule: MaintenanceRule): boolean {
  return rule.interval_hours != null
}

function requiresNewDueDateNow(rule: MaintenanceRule): boolean {
  return rule.fixed_due_date !== '' && rule.interval_months == null
}

export function MaintenanceCompleteDialog({ rule, onCancel, onComplete }: MaintenanceCompleteDialogProps) {
  const [form, setForm] = useState<LogFormState>(() => blankForm(rule?.current_hours))
  const [newDueDate, setNewDueDate] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [completedEntry, setCompletedEntry] = useState<MaintenanceLogEntry | null>(null)
  const [completedRule, setCompletedRule] = useState<MaintenanceRule | null>(null)

  const { items } = useEquipment(rule !== null ? {} : null)

  useEffect(() => {
    if (rule) {
      setForm(blankForm(rule.current_hours))
      setNewDueDate('')
      setCompletedEntry(null)
      setCompletedRule(null)
      setError(null)
    }
  }, [rule])

  const missingHours = rule != null && requiresHoursNow(rule) && form.hours.trim() === ''
  const missingNewDueDate = rule != null && requiresNewDueDateNow(rule) && newDueDate.trim() === ''
  const canComplete = !missingHours && !missingNewDueDate

  const handleSave = async () => {
    if (!rule || !canComplete) return
    setSaving(true)
    setError(null)
    try {
      const input = requiresNewDueDateNow(rule) ? { ...toInput(form), new_due_date: newDueDate } : toInput(form)
      const result = await onComplete(input)
      setCompletedEntry(result.entry)
      setCompletedRule(result.rule)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  const handleFilesPicked = async (files: File[]) => {
    if (!completedEntry) return
    setError(null)
    try {
      const updated = await uploadPhotoFiles(completedEntry.id, files)
      setCompletedEntry(updated)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  const handleRemovePhoto = async (documentId: string) => {
    if (!completedEntry) return
    try {
      const updated = await deleteMaintenanceLogPhoto(completedEntry.id, documentId)
      setCompletedEntry(updated)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <Dialog open={rule !== null} onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="max-h-[85vh] max-w-lg overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Complete: {rule?.description}</DialogTitle>
          <DialogDescription>
            Writes a service log entry and resets this rule's baseline to today.
          </DialogDescription>
        </DialogHeader>

        {!completedEntry ? (
          <>
            <FieldGroup>
              <div className="grid grid-cols-2 gap-3">
                <Field>
                  <FieldLabel htmlFor="maintenance-complete-date">Date</FieldLabel>
                  <Input id="maintenance-complete-date" type="date" value={form.performedAt} onChange={(e) => setForm((p) => ({ ...p, performedAt: e.target.value }))} />
                </Field>
                <Field>
                  <FieldLabel htmlFor="maintenance-complete-hours">
                    Hours (gauge reading){rule && requiresHoursNow(rule) ? ' (required)' : ''}
                  </FieldLabel>
                  <Input
                    id="maintenance-complete-hours"
                    type="number"
                    inputMode="decimal"
                    value={form.hours}
                    onChange={(e) => setForm((p) => ({ ...p, hours: e.target.value }))}
                  />
                  {/* Always the figure on the physical gauge, never true
                      hours - the server adds whichever meter-reset offset
                      applies when it stores this (2026-09-27 amendment,
                      docs/adr/0138). A live reading never goes stale (an
                      hour meter only has a reading while its engine runs),
                      so a prefilled value just says how old it is; only a
                      genuinely never-received reading asks the operator to
                      read the gauge themselves. */}
                  {rule && rule.current_hours != null ? (
                    hoursAsOfLabel(rule.hours_as_of) && (
                      <FieldDescription>Live reading, {hoursAsOfLabel(rule.hours_as_of)}.</FieldDescription>
                    )
                  ) : (
                    rule && requiresHoursNow(rule) && (
                      <FieldDescription>
                        {rule.has_hour_meter_path
                          ? 'The live reading has never been received - enter the current reading from the gauge.'
                          : 'This item has no live hour meter - enter the current reading from the gauge.'}
                      </FieldDescription>
                    )
                  )}
                </Field>
              </div>
              {rule && requiresNewDueDateNow(rule) && (
                <Field>
                  <FieldLabel htmlFor="maintenance-complete-new-due-date">New due date (required)</FieldLabel>
                  <Input
                    id="maintenance-complete-new-due-date"
                    type="date"
                    value={newDueDate}
                    onChange={(e) => setNewDueDate(e.target.value)}
                  />
                  <FieldDescription>
                    This rule has no monthly interval to compute the next date from - enter it yourself.
                  </FieldDescription>
                </Field>
              )}
              {rule && rule.fixed_due_date !== '' && rule.interval_months != null && (
                <p className="text-sm text-muted-foreground">
                  Next due: {addMonthsISO(form.performedAt || todayISO(), rule.interval_months)}
                </p>
              )}
              <Field>
                <FieldLabel htmlFor="maintenance-complete-description">Notes</FieldLabel>
                <Textarea id="maintenance-complete-description" rows={3} value={form.description} onChange={(e) => setForm((p) => ({ ...p, description: e.target.value }))} />
              </Field>
              <div className="grid grid-cols-3 gap-3">
                <Field className="col-span-1">
                  <FieldLabel htmlFor="maintenance-complete-who">Who</FieldLabel>
                  <Input id="maintenance-complete-who" value={form.who} onChange={(e) => setForm((p) => ({ ...p, who: e.target.value }))} />
                </Field>
                <Field className="col-span-1">
                  <FieldLabel htmlFor="maintenance-complete-cost">Cost</FieldLabel>
                  <Input id="maintenance-complete-cost" type="number" inputMode="decimal" min={0} value={form.cost} onChange={(e) => setForm((p) => ({ ...p, cost: e.target.value }))} />
                </Field>
                <Field className="col-span-1">
                  <FieldLabel htmlFor="maintenance-complete-currency">Currency</FieldLabel>
                  <Input id="maintenance-complete-currency" value={form.currency} onChange={(e) => setForm((p) => ({ ...p, currency: e.target.value }))} placeholder="AUD" />
                </Field>
              </div>
              <PartsEditor parts={form.parts} onChange={(parts) => setForm((p) => ({ ...p, parts }))} items={items} />
            </FieldGroup>
            {error && <FieldError errors={[{ message: error }]} />}
            <DialogFooter>
              <Button type="button" variant="outline" onClick={onCancel} disabled={saving}>Cancel</Button>
              <Button type="button" disabled={saving || !canComplete} onClick={() => { void handleSave() }}>
                {saving ? 'Completing...' : 'Complete'}
              </Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <p className="text-sm text-muted-foreground">Logged. Add photos if you like, then close.</p>
            <PhotoStripEditor
              photos={completedEntry.photo_ids.map((id) => ({ id, previewUrl: `${apiBaseUrl}/api/documents/${encodeURIComponent(id)}/content`, isCover: false }))}
              onFilesPicked={(files) => { void handleFilesPicked(files) }}
              onMakeCover={() => {}}
              onRemove={(id) => { void handleRemovePhoto(id) }}
            />
            {completedRule?.procedure_note_id && (
              <a
                href={documentViewerHref(completedRule.procedure_note_id)}
                className="inline-flex items-center gap-1.5 self-start text-sm text-primary hover:underline"
              >
                <BookOpen className="h-4 w-4" aria-hidden="true" />
                Open procedure note to update
              </a>
            )}
            {error && <FieldError errors={[{ message: error }]} />}
            <DialogFooter>
              <Button type="button" onClick={onCancel}>Done</Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}

// ── standalone log entry (create/edit) ───────────────────────────────────

interface MaintenanceLogEntryDialogProps {
  /** null: closed. undefined: creating a fresh standalone entry against
   * equipmentId. A MaintenanceLogEntry: editing that one. */
  entry: MaintenanceLogEntry | null | undefined
  equipmentId: string
  open: boolean
  onCancel: () => void
  // Code-review finding: the caller used to decide create-vs-update from
  // its OWN `editingEntry` state, which never changed after a brand new
  // entry's first save - creatingEntry stayed true, so a second Save on
  // the still-open dialog called create again (a silent duplicate, with no
  // photos and no Delete, since the caller's own state never learned this
  // entry now has an id). existingId is this dialog's own savedEntry.id -
  // the one place that actually knows whether there is now something to
  // update - so the caller never has to guess.
  onSave: (input: MaintenanceLogEntryInput, existingId: string | null) => Promise<MaintenanceLogEntry>
  onDelete?: (id: string) => Promise<void>
}

const KIND_LABELS: Record<MaintenanceLogKind, string> = {
  maintenance: 'Maintenance',
  repair: 'Repair',
  improvement: 'Improvement',
}

export function MaintenanceLogEntryDialog({ entry, equipmentId, open, onCancel, onSave, onDelete }: MaintenanceLogEntryDialogProps) {
  const [form, setForm] = useState<LogFormState>(() => blankForm())
  const [kind, setKind] = useState<MaintenanceLogKind>('repair')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [savedEntry, setSavedEntry] = useState<MaintenanceLogEntry | null>(entry ?? null)
  // Code-review finding: savedEntry alone used to decide whether Save even
  // showed - which is right for a brand new entry (nothing left to save
  // once it exists, only photos), but wrong for EDITING one, where
  // savedEntry starts non-null immediately and Save vanished on open,
  // leaving Done as the only button - a further edit was silently
  // discarded on close. dirty tracks "changed since the last successful
  // save" separately, so Save stays available (and Done reads as Cancel)
  // for as long as there is something it would actually save.
  const [dirty, setDirty] = useState(false)

  const { items } = useEquipment(open ? {} : null)

  useEffect(() => {
    if (open) {
      setForm(entry ? formFromEntry(entry) : blankForm())
      setKind(entry?.kind ?? 'repair')
      setSavedEntry(entry ?? null)
      setDirty(false)
      setError(null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, entry?.id])

  const isEditing = Boolean(entry)

  const updateForm = (updater: (prev: LogFormState) => LogFormState) => {
    setForm(updater)
    setDirty(true)
  }

  const updateKind = (next: MaintenanceLogKind) => {
    setKind(next)
    setDirty(true)
  }

  const handleSave = async () => {
    setSaving(true)
    setError(null)
    try {
      const result = await onSave({ ...toInput(form), equipment_id: equipmentId, kind }, savedEntry?.id ?? null)
      setSavedEntry(result)
      setDirty(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  // Code-review finding: this had no error handling at all - a failed
  // delete's rejection went uncaught, so onCancel simply never ran (the
  // dialog correctly stayed open) but the operator saw no reason why
  // nothing happened. Errors now surface the same way every other write
  // in this dialog already does (FieldError, role="alert"), and onCancel
  // only fires once the delete has actually succeeded.
  //
  // Keyed on savedEntry, not entry: entry only reflects what the dialog
  // was OPENED with (undefined for a brand new standalone entry), while
  // savedEntry is whatever actually exists on the server right now - true
  // the instant a fresh entry's first Save returns, which is exactly when
  // Delete has to start working too (the other half of the same
  // code-review finding as onSave's existingId above).
  const handleDelete = async () => {
    if (!savedEntry || !onDelete) return
    setSaving(true)
    setError(null)
    try {
      await onDelete(savedEntry.id)
      onCancel()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  const handleFilesPicked = async (files: File[]) => {
    if (!savedEntry) return
    setError(null)
    try {
      setSavedEntry(await uploadPhotoFiles(savedEntry.id, files))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  const handleRemovePhoto = async (documentId: string) => {
    if (!savedEntry) return
    try {
      setSavedEntry(await deleteMaintenanceLogPhoto(savedEntry.id, documentId))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next) onCancel() }}>
      <DialogContent className="max-h-[85vh] max-w-lg overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{isEditing ? 'Edit log entry' : 'Add log entry'}</DialogTitle>
          <DialogDescription>A repair or improvement, standalone - not tied to a rule.</DialogDescription>
        </DialogHeader>

        <FieldGroup>
          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel htmlFor="maintenance-log-date">Date</FieldLabel>
              <Input id="maintenance-log-date" type="date" value={form.performedAt} onChange={(e) => updateForm((p) => ({ ...p, performedAt: e.target.value }))} />
            </Field>
            <Field>
              <FieldLabel htmlFor="maintenance-log-kind">Kind</FieldLabel>
              <Select value={kind} onValueChange={(v) => updateKind(v as MaintenanceLogKind)}>
                <SelectTrigger id="maintenance-log-kind" aria-label="Kind">
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
          <Field>
            <FieldLabel htmlFor="maintenance-log-hours">Hours (gauge reading)</FieldLabel>
            <Input id="maintenance-log-hours" type="number" inputMode="decimal" value={form.hours} onChange={(e) => updateForm((p) => ({ ...p, hours: e.target.value }))} />
          </Field>
          <Field>
            <FieldLabel htmlFor="maintenance-log-description">Description</FieldLabel>
            <Textarea id="maintenance-log-description" rows={3} value={form.description} onChange={(e) => updateForm((p) => ({ ...p, description: e.target.value }))} />
          </Field>
          <div className="grid grid-cols-3 gap-3">
            <Field className="col-span-1">
              <FieldLabel htmlFor="maintenance-log-who">Who</FieldLabel>
              <Input id="maintenance-log-who" value={form.who} onChange={(e) => updateForm((p) => ({ ...p, who: e.target.value }))} />
            </Field>
            <Field className="col-span-1">
              <FieldLabel htmlFor="maintenance-log-cost">Cost</FieldLabel>
              <Input id="maintenance-log-cost" type="number" inputMode="decimal" min={0} value={form.cost} onChange={(e) => updateForm((p) => ({ ...p, cost: e.target.value }))} />
            </Field>
            <Field className="col-span-1">
              <FieldLabel htmlFor="maintenance-log-currency">Currency</FieldLabel>
              <Input id="maintenance-log-currency" value={form.currency} onChange={(e) => updateForm((p) => ({ ...p, currency: e.target.value }))} placeholder="AUD" />
            </Field>
          </div>
          <PartsEditor parts={form.parts} onChange={(parts) => updateForm((p) => ({ ...p, parts }))} items={items} />

          {savedEntry && (
            <Field>
              <FieldLabel>Photos</FieldLabel>
              <PhotoStripEditor
                photos={savedEntry.photo_ids.map((id) => ({ id, previewUrl: `${apiBaseUrl}/api/documents/${encodeURIComponent(id)}/content`, isCover: false }))}
                onFilesPicked={(files) => { void handleFilesPicked(files) }}
                onMakeCover={() => {}}
                onRemove={(id) => { void handleRemovePhoto(id) }}
              />
            </Field>
          )}
        </FieldGroup>

        {error && <FieldError errors={[{ message: error }]} />}

        <DialogFooter>
          {savedEntry && onDelete && (
            <Button type="button" variant="ghost" className="mr-auto text-destructive" disabled={saving} onClick={() => { void handleDelete() }}>
              Delete
            </Button>
          )}
          <Button type="button" variant="outline" onClick={onCancel} disabled={saving}>
            {savedEntry && !dirty ? 'Done' : 'Cancel'}
          </Button>
          {(!savedEntry || dirty) && (
            <Button type="button" disabled={saving} onClick={() => { void handleSave() }}>
              {saving ? 'Saving...' : 'Save'}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
