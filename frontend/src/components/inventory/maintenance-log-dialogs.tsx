import { useEffect, useId, useMemo, useRef, useState } from 'react'
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
import { Combobox, ComboboxInput, ComboboxItem, ComboboxPopup } from '@/components/ui/combobox'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { PhotoStripEditor } from '@/components/inventory/photo-strip-editor'
import { apiBaseUrl } from '@/config/api'
import { BLANK_DRAFT, createEquipment, useEquipment, type EquipmentItem } from '@/hooks/use-inventory'
import {
  deleteMaintenanceLogPhoto,
  hoursAgeLabel,
  uploadMaintenanceLogPhoto,
  type MaintenanceLogEntry,
  type MaintenanceLogEntryInput,
  type MaintenanceLogKind,
  type MaintenanceLogPartInput,
  type MaintenanceRule,
} from '@/hooks/use-maintenance'
import { useUnsavedWorkReport } from '@/hooks/use-navigation-guard'
import { documentViewerHref } from '@/lib/document-citation'
import { addMonthsISO, STARTED_AFTER_REPAIR, startedAfterRepair, todayISO } from '@/lib/local-date'
import { errorMessage } from '@/lib/errors'
import { matchesAllWords } from '@/lib/word-match'

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
  /** The equipment list is still on its way; an empty `items` proves nothing. */
  itemsLoading: boolean
  /** Why the equipment list failed to load, or null. */
  itemsError: string | null
}

interface PartOption {
  id: string
  label: string
  /** Part number, then manufacturer and model: tells apart rows that share a name. */
  detail: string
}

/** Lower-cased, trimmed, with runs of whitespace as one space. */
function collapse(s: string): string {
  return s.trim().replace(/\s+/g, ' ').toLowerCase()
}

/** The id of the last row, which creates the typed text as a spare. */
const CREATE_OPTION_ID = '__create_spare__'

export function PartsEditor({ parts, onChange, items, itemsLoading, itemsError }: PartsEditorProps) {
  const [pickerId, setPickerId] = useState('')
  const [query, setQuery] = useState('')
  const [quantity, setQuantity] = useState('1')
  const [open, setOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  // Spares made from this field: the equipment list refetches after the
  // create, and until it lands they still need a name and still count as
  // taken.
  const [made, setMade] = useState<EquipmentItem[]>([])
  const creatingRef = useRef(false)

  const pool = useMemo(
    () => [...items, ...made.filter((m) => !items.some((i) => i.id === m.id))],
    [items, made],
  )
  const nameFor = (id: string) => pool.find((i) => i.id === id)?.name ?? id

  const typed = query.trim()
  // pickerId is the last part picked; it is the live pick only while the box
  // still holds its name, so typing away and back restores it.
  const picked = pickerId !== '' && query === nameFor(pickerId) ? pickerId : ''
  const showingPick = picked !== ''
  // Once a part is picked the input holds its name; the chevron should still
  // offer the whole list, as the old select did. Every typed word must match
  // one of the fields, so "jabsco impeller" finds the Jabsco impeller.
  const matches = useMemo<PartOption[]>(
    () =>
      pool
        .filter((i) => showingPick || matchesAllWords([i.name, i.part_number, i.manufacturer, i.model].join(' '), typed))
        .map((i) => ({
          id: i.id,
          label: i.name,
          detail: [i.part_number, [i.manufacturer, i.model].filter(Boolean).join(' ')].filter(Boolean).join(' · '),
        })),
    [pool, showingPick, typed],
  )
  // A new spare is offered only against a list that has actually arrived:
  // while it loads, or after it failed, "not in the list" would make a
  // second copy of a part already aboard.
  const listHere = !itemsLoading && itemsError === null
  const options = useMemo<PartOption[]>(() => {
    const exact = pool.some((i) => collapse(i.name) === collapse(typed))
    return listHere && typed !== '' && !exact
      ? [...matches, { id: CREATE_OPTION_ID, label: `Add "${typed}" as a spare`, detail: '' }]
      : matches
  }, [pool, matches, typed, listHere])
  const selected = pickerId ? { id: pickerId, label: nameFor(pickerId), detail: '' } : null
  const listId = useId()

  const createSpare = async () => {
    if (creatingRef.current || typed === '') return
    creatingRef.current = true
    setCreating(true)
    setCreateError(null)
    try {
      // Nobody has counted how many are aboard, so none are claimed.
      const created = await createEquipment({ ...BLANK_DRAFT, name: typed, status: 'stored', quantity: 0 })
      setMade((prev) => [...prev, created])
      setPickerId(created.id)
      setQuery(created.name)
    } catch (err) {
      setCreateError(errorMessage(err))
    } finally {
      creatingRef.current = false
      setCreating(false)
    }
  }

  const addPart = () => {
    if (!picked) return
    const qty = Number(quantity) || 1
    const existing = parts.find((p) => p.equipment_id === picked)
    if (existing) {
      onChange(parts.map((p) => (p.equipment_id === picked ? { ...p, quantity: p.quantity + qty } : p)))
    } else {
      onChange([...parts, { equipment_id: picked, quantity: qty }])
    }
    setPickerId('')
    setQuery('')
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
          <Combobox<PartOption>
            items={options}
            filter={null}
            value={selected}
            inputValue={query}
            open={open}
            onOpenChange={setOpen}
            itemToStringLabel={(o) => o.label}
            isItemEqualToValue={(a, b) => a.id === b.id}
            readOnly={creating}
            onInputValueChange={(text, details) => {
              // Only the operator's own typing moves the query. Base UI also
              // writes into the input on a pick or a close, which would put
              // the create row's wording in the box or undo a fresh pick.
              if (creating || creatingRef.current || details.reason !== 'input-change') return
              setQuery(text)
              setCreateError(null)
              if (text !== '') setOpen(true)
            }}
            onValueChange={(option) => {
              if (!option || creating || creatingRef.current) return
              if (option.id === CREATE_OPTION_ID) void createSpare()
              else {
                setPickerId(option.id)
                setQuery(option.label)
              }
            }}
          >
            <ComboboxInput aria-label="Add a part" placeholder="Add a part..." className="h-9" />
            <ComboboxPopup>
              {options.map((o) => (
                <ComboboxItem
                  key={o.id}
                  value={o}
                  aria-labelledby={`${listId}-${o.id}-name`}
                  aria-describedby={o.detail ? `${listId}-${o.id}-detail` : undefined}
                >
                  <span className="flex min-w-0 flex-col">
                    <span id={`${listId}-${o.id}-name`}>{o.label}</span>
                    {o.detail && (
                      <span id={`${listId}-${o.id}-detail`} className="truncate text-xs text-muted-foreground">{o.detail}</span>
                    )}
                  </span>
                </ComboboxItem>
              ))}
            </ComboboxPopup>
          </Combobox>
          <Input
            type="number"
            inputMode="numeric"
            min={1}
            className="h-9 w-16"
            aria-label="Quantity"
            value={quantity}
            onChange={(e) => setQuantity(e.target.value)}
          />
          <Button type="button" variant="outline" size="icon" aria-label="Add part" disabled={!picked || creating} onClick={addPart}>
            <Plus className="h-4 w-4" aria-hidden="true" />
          </Button>
        </div>
        {itemsError && <FieldError>Couldn't load the equipment list: {itemsError}</FieldError>}
        {creating && <FieldDescription>Adding the spare...</FieldDescription>}
        {createError && <FieldError>{createError}</FieldError>}
      </div>
    </Field>
  )
}

interface LogFormState {
  performedAt: string
  /** A repair's "started showing" date, blank for none. */
  startedAt: string
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
    startedAt: '',
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
    startedAt: entry.started_at ?? '',
    hours: entry.hours != null ? String(entry.hours) : '',
    description: entry.description,
    who: entry.who,
    cost: entry.cost != null ? String(entry.cost) : '',
    currency: entry.currency,
    // A part whose own equipment_id has gone to null (its item was
    // deleted - backend 2026-09-27 amendment) survives as pure history but
    // can never be re-specified here: PartsEditor only ever works with an
    // equipment_id to add/remove by, and there is no id left to name it
    // with. orphanedParts (below) is where it's actually shown.
    parts: entry.parts
      .filter((p): p is typeof p & { equipment_id: string } => p.equipment_id !== null)
      .map((p) => ({ equipment_id: p.equipment_id, quantity: p.quantity })),
  }
}

/** Parts used on this entry whose own item has since been deleted -
 * read-only history, shown separately from the editable PartsEditor list
 * above (formFromEntry's own comment explains why they can't be merged
 * into it). */
function orphanedParts(entry: MaintenanceLogEntry | null): MaintenanceLogEntry['parts'] {
  return entry ? entry.parts.filter((p) => p.equipment_id === null) : []
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
  // What the form was when it was opened or last reset: "unsaved" means
  // different from this, not different from a blank rebuilt on each render
  // (which moves at midnight and when the hours reading refreshes).
  const [initialForm, setInitialForm] = useState<LogFormState>(() => blankForm(rule?.current_hours))
  const [form, setForm] = useState<LogFormState>(initialForm)
  const [newDueDate, setNewDueDate] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [completedEntry, setCompletedEntry] = useState<MaintenanceLogEntry | null>(null)
  const [completedRule, setCompletedRule] = useState<MaintenanceRule | null>(null)

  const { items, loading: itemsLoading, error: itemsError } = useEquipment(rule !== null ? {} : null)

  // Anything changed since the form was opened and not yet written to the log is unsaved work.
  const entered = rule !== null && completedEntry === null
    && (newDueDate !== '' || JSON.stringify(form) !== JSON.stringify(initialForm))
  useUnsavedWorkReport('maintenance-complete', entered)

  useEffect(() => {
    if (rule) {
      const fresh = blankForm(rule.current_hours)
      setInitialForm(fresh)
      setForm(fresh)
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
      setError(errorMessage(err))
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
      setError(errorMessage(err))
    }
  }

  const handleRemovePhoto = async (documentId: string) => {
    if (!completedEntry) return
    try {
      const updated = await deleteMaintenanceLogPhoto(completedEntry.id, documentId)
      setCompletedEntry(updated)
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  return (
    <Dialog open={rule !== null} onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="max-h-[85vh] max-w-lg overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Complete: {rule?.description}</DialogTitle>
          <DialogDescription>
            Logs this service. If it is the latest one, the next falls due counted from it.
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
                    hoursAgeLabel(rule) && (
                      <FieldDescription>
                        {rule.hours_by_hand ? `${hoursAgeLabel(rule)}.` : `Live reading, ${hoursAgeLabel(rule)}.`}
                      </FieldDescription>
                    )
                  ) : (
                    rule && requiresHoursNow(rule) && (
                      <FieldDescription>
                        {rule.has_hour_meter_path
                          ? 'The live reading has never been received - enter the current reading from the gauge.'
                          : 'This item has no hour meter and no reading logged - enter the current reading from the gauge.'}
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
              <PartsEditor parts={form.parts} onChange={(parts) => setForm((p) => ({ ...p, parts }))} items={items} itemsLoading={itemsLoading} itemsError={itemsError} />
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

export const KIND_LABELS: Record<MaintenanceLogKind, string> = {
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

  const { items, loading: itemsLoading, error: itemsError } = useEquipment(open ? {} : null)
  useUnsavedWorkReport('maintenance-entry', open && dirty)

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
  const startedTooLate = startedAfterRepair(kind, form.startedAt, form.performedAt)

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
      // Only a repair has a fault to date; the server refuses it on anything else.
      const result = await onSave(
        { ...toInput(form), started_at: kind === 'repair' ? form.startedAt : '', equipment_id: equipmentId, kind },
        savedEntry?.id ?? null,
      )
      setSavedEntry(result)
      setDirty(false)
    } catch (err) {
      setError(errorMessage(err))
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
      setError(errorMessage(err))
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
      setError(errorMessage(err))
    }
  }

  const handleRemovePhoto = async (documentId: string) => {
    if (!savedEntry) return
    try {
      setSavedEntry(await deleteMaintenanceLogPhoto(savedEntry.id, documentId))
    } catch (err) {
      setError(errorMessage(err))
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
          {kind === 'repair' && (
            <Field>
              <FieldLabel htmlFor="maintenance-log-started">Started showing</FieldLabel>
              <Input
                id="maintenance-log-started"
                type="date"
                max={form.performedAt || undefined}
                value={form.startedAt}
                onChange={(e) => updateForm((p) => ({ ...p, startedAt: e.target.value }))}
              />
              {startedTooLate && <FieldError errors={[{ message: STARTED_AFTER_REPAIR }]} />}
              <FieldDescription>
                When the fault first showed. On an engine, the house bank or what charges it, readings from then until this repair are left out of what counts as normal.
              </FieldDescription>
            </Field>
          )}
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
          <PartsEditor parts={form.parts} onChange={(parts) => updateForm((p) => ({ ...p, parts }))} items={items} itemsLoading={itemsLoading} itemsError={itemsError} />

          {/* Code-review finding: a part whose own item has since been
              deleted survives as history (backend 2026-09-27 amendment) but
              can't be edited here - shown read-only, with the name it had
              when it was logged, so it isn't just silently missing. */}
          {orphanedParts(savedEntry).length > 0 && (
            <p className="text-xs text-muted-foreground">
              Also used (no longer in inventory): {orphanedParts(savedEntry).map((p) => `${p.equipment_name} x${p.quantity}`).join(', ')}
            </p>
          )}

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
            <Button type="button" disabled={saving || startedTooLate} onClick={() => { void handleSave() }}>
              {saving ? 'Saving...' : 'Save'}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
