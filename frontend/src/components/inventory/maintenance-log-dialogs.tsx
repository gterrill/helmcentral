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
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { PhotoStripEditor } from '@/components/inventory/photo-strip-editor'
import { apiBaseUrl } from '@/config/api'
import { useEquipment, type EquipmentItem } from '@/hooks/use-inventory'
import {
  deleteMaintenanceLogPhoto,
  uploadMaintenanceLogPhoto,
  type MaintenanceLogEntry,
  type MaintenanceLogEntryInput,
  type MaintenanceLogKind,
  type MaintenanceLogPartInput,
  type MaintenanceRule,
} from '@/hooks/use-maintenance'
import { documentViewerHref } from '@/lib/document-citation'

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

function todayISO(): string {
  return new Date().toISOString().slice(0, 10)
}

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

export function MaintenanceCompleteDialog({ rule, onCancel, onComplete }: MaintenanceCompleteDialogProps) {
  const [form, setForm] = useState<LogFormState>(() => blankForm(rule?.current_hours))
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [completedEntry, setCompletedEntry] = useState<MaintenanceLogEntry | null>(null)
  const [completedRule, setCompletedRule] = useState<MaintenanceRule | null>(null)

  const { items } = useEquipment(rule !== null ? {} : null)

  useEffect(() => {
    if (rule) {
      setForm(blankForm(rule.current_hours))
      setCompletedEntry(null)
      setCompletedRule(null)
      setError(null)
    }
  }, [rule])

  const handleSave = async () => {
    setSaving(true)
    setError(null)
    try {
      const result = await onComplete(toInput(form))
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
                  <FieldLabel htmlFor="maintenance-complete-hours">Hours</FieldLabel>
                  <Input
                    id="maintenance-complete-hours"
                    type="number"
                    inputMode="decimal"
                    value={form.hours}
                    onChange={(e) => setForm((p) => ({ ...p, hours: e.target.value }))}
                  />
                </Field>
              </div>
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
              <Button type="button" disabled={saving} onClick={() => { void handleSave() }}>
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
  onSave: (input: MaintenanceLogEntryInput) => Promise<MaintenanceLogEntry>
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

  const { items } = useEquipment(open ? {} : null)

  useEffect(() => {
    if (open) {
      setForm(entry ? formFromEntry(entry) : blankForm())
      setKind(entry?.kind ?? 'repair')
      setSavedEntry(entry ?? null)
      setError(null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, entry?.id])

  const isEditing = Boolean(entry)

  const handleSave = async () => {
    setSaving(true)
    setError(null)
    try {
      const result = await onSave({ ...toInput(form), equipment_id: equipmentId, kind })
      setSavedEntry(result)
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
              <Input id="maintenance-log-date" type="date" value={form.performedAt} onChange={(e) => setForm((p) => ({ ...p, performedAt: e.target.value }))} />
            </Field>
            <Field>
              <FieldLabel htmlFor="maintenance-log-kind">Kind</FieldLabel>
              <Select value={kind} onValueChange={(v) => setKind(v as MaintenanceLogKind)}>
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
            <FieldLabel htmlFor="maintenance-log-hours">Hours</FieldLabel>
            <Input id="maintenance-log-hours" type="number" inputMode="decimal" value={form.hours} onChange={(e) => setForm((p) => ({ ...p, hours: e.target.value }))} />
          </Field>
          <Field>
            <FieldLabel htmlFor="maintenance-log-description">Description</FieldLabel>
            <Textarea id="maintenance-log-description" rows={3} value={form.description} onChange={(e) => setForm((p) => ({ ...p, description: e.target.value }))} />
          </Field>
          <div className="grid grid-cols-3 gap-3">
            <Field className="col-span-1">
              <FieldLabel htmlFor="maintenance-log-who">Who</FieldLabel>
              <Input id="maintenance-log-who" value={form.who} onChange={(e) => setForm((p) => ({ ...p, who: e.target.value }))} />
            </Field>
            <Field className="col-span-1">
              <FieldLabel htmlFor="maintenance-log-cost">Cost</FieldLabel>
              <Input id="maintenance-log-cost" type="number" inputMode="decimal" min={0} value={form.cost} onChange={(e) => setForm((p) => ({ ...p, cost: e.target.value }))} />
            </Field>
            <Field className="col-span-1">
              <FieldLabel htmlFor="maintenance-log-currency">Currency</FieldLabel>
              <Input id="maintenance-log-currency" value={form.currency} onChange={(e) => setForm((p) => ({ ...p, currency: e.target.value }))} placeholder="AUD" />
            </Field>
          </div>
          <PartsEditor parts={form.parts} onChange={(parts) => setForm((p) => ({ ...p, parts }))} items={items} />

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
          {isEditing && onDelete && (
            <Button type="button" variant="ghost" className="mr-auto text-destructive" disabled={saving} onClick={() => { if (entry) void onDelete(entry.id).then(onCancel) }}>
              Delete
            </Button>
          )}
          <Button type="button" variant="outline" onClick={onCancel} disabled={saving}>
            {savedEntry ? 'Done' : 'Cancel'}
          </Button>
          {!savedEntry && (
            <Button type="button" disabled={saving} onClick={() => { void handleSave() }}>
              {saving ? 'Saving...' : 'Save'}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
