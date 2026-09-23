import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useState } from 'react'
import { Plus, Trash2, X } from 'lucide-react'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { DocumentLinkPicker, type DocumentLinkPickerResult } from '@/components/inventory/document-link-picker'
import { useEquipmentProfiles } from '@/hooks/use-equipment-profiles'
import { useSignalKPaths } from '@/hooks/use-signalk-paths'
import {
  EQUIPMENT_SYSTEMS,
  EQUIPMENT_SYSTEM_LABELS,
  InventoryValidationError,
  createEquipment,
  useEquipmentItem,
  useInventoryZones,
  type EquipmentCategory,
  type EquipmentInput,
  type EquipmentItem,
  type EquipmentStatus,
  type EquipmentSystem,
  type InventoryFieldError,
} from '@/hooks/use-inventory'

// ADR 0123: the Specifications & IDs form plus the Documents tab, for one
// equipment record - `id === null` is the "New item" draft (App.tsx's
// inventoryCreatingEquipment), everything else is an existing record bound
// to useEquipmentItem(id). Mirrors document-details-page.tsx's own shape
// throughout: an explicit local draft, an imperative `save()` handle for
// App.tsx's navigation guard (the same Settings/Details dirty-save contract,
// generalized a third time), and the aliases chips are the same
// add/remove/Enter-to-add interaction as that page's own tags editor.

const NO_ZONE_VALUE = '__no_zone__'
const NO_BIN_VALUE = '__no_bin__'
const NO_PROFILE_VALUE = '__no_profile__'

const BLANK_DRAFT: EquipmentInput = {
  name: '',
  category: 'general',
  system: 'other',
  manufacturer: '',
  model: '',
  serial: '',
  quantity: 1,
  status: 'deployed',
  zone_id: null,
  bin_id: null,
  location_detail: '',
  install_date: '',
  hour_meter_path: '',
  profile_id: '',
  aliases: [],
  verified_aboard: false,
  notes: '',
}

function draftFromItem(item: EquipmentItem): EquipmentInput {
  return {
    name: item.name,
    category: item.category,
    system: item.system,
    manufacturer: item.manufacturer,
    model: item.model,
    serial: item.serial,
    quantity: item.quantity,
    status: item.status,
    zone_id: item.zone_id,
    bin_id: item.bin_id,
    location_detail: item.location_detail,
    install_date: item.install_date,
    hour_meter_path: item.hour_meter_path,
    profile_id: item.profile_id,
    aliases: item.aliases,
    verified_aboard: item.verified_aboard,
    notes: item.notes,
  }
}

function sameStringArray(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((v, i) => v === b[i])
}

// Order-insensitive, same reasoning as document-details-page.tsx's own
// sameTagSet: the draft only ever grows an id at the end (picker "pick") or
// removes one in place (Remove), but comparing as sets is what actually
// matches "did the link SET change", not "did the array happen to reorder".
function sameIdSet(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false
  const sortedA = [...a].sort()
  const sortedB = [...b].sort()
  return sortedA.every((v, i) => v === sortedB[i])
}

function sameDraft(a: EquipmentInput, b: EquipmentInput): boolean {
  return a.name === b.name
    && a.category === b.category
    && a.system === b.system
    && a.manufacturer === b.manufacturer
    && a.model === b.model
    && a.serial === b.serial
    && a.quantity === b.quantity
    && a.status === b.status
    && a.zone_id === b.zone_id
    && a.bin_id === b.bin_id
    && a.location_detail === b.location_detail
    && a.install_date === b.install_date
    && a.hour_meter_path === b.hour_meter_path
    && a.profile_id === b.profile_id
    && a.verified_aboard === b.verified_aboard
    && a.notes === b.notes
    && sameStringArray(a.aliases, b.aliases)
}

interface DocEntry {
  document_id: string
  title: string
  filename: string
}

interface EquipmentEditorProps {
  /** null is the "New item" draft - see this file's header comment. */
  id: string | null
  onBack: () => void
  /** Fired once, right after a successful create POST - App.tsx uses this to
   * navigate to `/inventory/equipment/<id>` (plan: "Create flow: POST, then
   * navigate to the new id; do not leave the operator on a blank form"). */
  onCreated: (id: string) => void
  onDeleted: () => void
  onDirtyChange?: (dirty: boolean) => void
  canWrite?: boolean
}

export interface EquipmentEditorHandle {
  save: () => Promise<void>
}

export const EquipmentEditor = forwardRef<EquipmentEditorHandle, EquipmentEditorProps>(function EquipmentEditor(
  { id, onBack, onCreated, onDeleted, onDirtyChange, canWrite = true },
  ref,
) {
  const { item, documents, loading, error, update, remove, setLinkedDocuments } = useEquipmentItem(id)
  const { zones } = useInventoryZones()
  const { profiles } = useEquipmentProfiles(true)
  const { paths } = useSignalKPaths(true)

  const [draft, setDraft] = useState<EquipmentInput>(BLANK_DRAFT)
  const [docEntries, setDocEntries] = useState<DocEntry[]>([])
  const [aliasInput, setAliasInput] = useState('')
  const [pickerOpen, setPickerOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [fieldErrors, setFieldErrors] = useState<InventoryFieldError[]>([])
  const [pendingDelete, setPendingDelete] = useState(false)
  const [deleting, setDeleting] = useState(false)

  // Re-seeds only when a DIFFERENT record has loaded (id, or - for a brand
  // new draft - a one-time reset to blank), not on every incidental
  // re-render of the same one, mirroring document-details-page.tsx's own
  // draft effect and its reasoning: update()'s echoed-back item would
  // otherwise re-run this and discard an edit in progress.
  useEffect(() => {
    if (id === null) {
      setDraft(BLANK_DRAFT)
      return
    }
    if (item) setDraft(draftFromItem(item))
    // eslint-disable-next-line react-hooks/exhaustive-deps -- keyed on id/item id only, see comment above.
  }, [id, item?.id])

  useEffect(() => {
    if (id === null) {
      setDocEntries([])
      return
    }
    setDocEntries(documents.map((d) => ({ document_id: d.document_id, title: d.title, filename: d.filename })))
    // Keyed on the fetched set's own content, not the array reference -
    // useEquipmentItem builds a fresh `documents` array on every refresh()
    // even when the set is unchanged, and re-seeding on every one of those
    // would throw away a locally staged add/remove before Save ever runs.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, documents.map((d) => d.document_id).join(',')])

  const baseline = id === null ? BLANK_DRAFT : (item ? draftFromItem(item) : null)
  const baselineDocIds = useMemo(() => documents.map((d) => d.document_id), [documents])
  const draftDirty = baseline !== null && !sameDraft(draft, baseline)
  const linksDirty = id !== null && !sameIdSet(docEntries.map((d) => d.document_id), baselineDocIds)
  const dirty = draftDirty || linksDirty

  useEffect(() => { onDirtyChange?.(dirty) }, [dirty, onDirtyChange])

  // Trap: the bin select is constrained to the CHOSEN zone's bins - with no
  // zone chosen yet, every bin across every zone is offered instead (so a
  // bin can be picked first), and picking one then derives its zone below.
  const availableBins = draft.zone_id
    ? (zones.find((z) => z.id === draft.zone_id)?.bins ?? [])
    : zones.flatMap((z) => z.bins)

  const handleZoneChange = (zoneId: string | null) => {
    setDraft((prev) => {
      const zone = zones.find((z) => z.id === zoneId)
      // Changing zones drops a bin selection that doesn't belong to the new
      // zone - the server rejects a zone/bin pair that disagree (ADR 0123),
      // and silently keeping a stale bin here would just move that failure
      // to Save instead of preventing it.
      const binStillValid = prev.bin_id !== null && (zone?.bins.some((b) => b.id === prev.bin_id) ?? false)
      return { ...prev, zone_id: zoneId, bin_id: binStillValid ? prev.bin_id : null }
    })
  }

  const handleBinChange = (binId: string | null) => {
    setDraft((prev) => {
      if (binId === null) return { ...prev, bin_id: null }
      // Trap: a bin picked with no zone set has to set the zone FROM that
      // bin (a bin implies its zone, ADR 0123) - otherwise this would send a
      // zone/bin pair that disagree and the server would reject it.
      const owningZone = zones.find((z) => z.bins.some((b) => b.id === binId))
      return { ...prev, bin_id: binId, zone_id: owningZone ? owningZone.id : prev.zone_id }
    })
  }

  // Picking a profile prefills BLANK manufacturer/model only, and only sets
  // category to mechanical for an engine/alternator/generator kind - never
  // overwrites a value the operator has already typed (the plan's own
  // wording), the same "don't clobber an in-progress edit" rule this whole
  // page follows for everything else.
  const handleProfileChange = (profileId: string) => {
    setDraft((prev) => {
      const profile = profiles.find((p) => p.id === profileId)
      const next: EquipmentInput = { ...prev, profile_id: profileId }
      if (profile) {
        if (!next.manufacturer.trim() && profile.manufacturer) next.manufacturer = profile.manufacturer
        if (!next.model.trim() && profile.model) next.model = profile.model
        if (profile.kind === 'engine' || profile.kind === 'generator' || profile.kind === 'alternator') {
          next.category = 'mechanical'
        }
      }
      return next
    })
  }

  const addAlias = () => {
    const name = aliasInput.trim()
    // Case-insensitive, matching the server's own normalizeAliases
    // (backend/inventory_store.go) - otherwise "Genset" and "genset" would
    // sit in the draft as two aliases only for the save to collapse them to
    // one, leaving the draft one alias ahead of what actually got stored.
    if (name === '' || draft.aliases.some((a) => a.toLowerCase() === name.toLowerCase())) {
      setAliasInput('')
      return
    }
    setDraft((prev) => ({ ...prev, aliases: [...prev.aliases, name] }))
    setAliasInput('')
  }
  const removeAlias = (name: string) => setDraft((prev) => ({ ...prev, aliases: prev.aliases.filter((a) => a !== name) }))

  const addDocument = (doc: DocumentLinkPickerResult) => {
    if (docEntries.some((d) => d.document_id === doc.document_id)) return
    setDocEntries((prev) => [...prev, doc])
  }
  const removeDocument = (documentId: string) => setDocEntries((prev) => prev.filter((d) => d.document_id !== documentId))

  // Trap: SignalK publishes runTime under all sorts of prefixes
  // (electrical.generator.0.runTime, propulsion.port.runTime...) - the one
  // constant is the last segment, matched case-insensitively since some
  // plugins spell it runtime.
  const hourMeterPaths = useMemo(
    () => paths.filter((p) => /^runtime$/i.test(p.path.split('.').pop() ?? '')),
    [paths],
  )

  // Exposed via useImperativeHandle so App.tsx's "Save and Continue" (the
  // dirty-navigation guard, wired through InventoryPanel the same way
  // ADR 0115 §2 wires DocumentDetailsPageHandle) can save this draft without
  // knowing anything about create vs. update. Throws rather than swallowing
  // (AGENTS.md fallback policy) so the guard dialog can show the server's
  // own message instead of navigating past a save that didn't happen.
  const performSave = useCallback(async () => {
    setSaveError(null)
    setFieldErrors([])
    try {
      if (id === null) {
        const created = await createEquipment(draft)
        onCreated(created.id)
        return
      }
      const sentDraft = draft
      const updated = await update(draft)
      // Re-seed the draft from the server's own normalised record (trimmed
      // strings, case-insensitively deduped aliases - backend/inventory_
      // handlers.go and inventory_store.go's normalizeAliases). The effect
      // above only re-seeds on a DIFFERENT id, so without this a save that
      // only changed what the server trims off (e.g. "  Onan  " -> "Onan")
      // would leave `dirty` stuck true forever - the draft still says
      // "  Onan  ", the record now says "Onan", and nothing ever compares
      // them again.
      //
      // Code review: this used to call setDraft unconditionally, which
      // overwrote whatever the operator typed WHILE this PUT was in flight
      // - a functional setDraft here still snapshots `draft` as of when
      // Save was pressed (in `sentDraft`, captured above), so a newer draft
      // the operator has since typed is left alone. It only re-seeds when
      // the current draft is still exactly what was sent: sameDraft, not
      // `===`, because setDraft's own callback receives the very state that
      // would otherwise be replaced, and that's the one comparison that
      // means "nothing changed since Save was pressed." Left alone, the
      // newer draft still differs from the also-just-updated baseline (this
      // save's own record), so `dirty` correctly stays true instead of
      // dropping with no warning that the newer edit was never sent.
      setDraft((current) => (sameDraft(current, sentDraft) ? draftFromItem(updated) : current))
      // Trap: only sent when the link SET actually changed - comparing ids
      // as sets, not array order, so re-saving an untouched Documents list
      // never issues a no-op PUT.
      if (!sameIdSet(docEntries.map((d) => d.document_id), baselineDocIds)) {
        await setLinkedDocuments(docEntries.map((d) => d.document_id))
      }
    } catch (err) {
      if (err instanceof InventoryValidationError) setFieldErrors(err.fields)
      throw err
    }
  }, [id, draft, docEntries, baselineDocIds, update, setLinkedDocuments, onCreated])

  useImperativeHandle(ref, () => ({ save: performSave }), [performSave])

  const handleSave = async () => {
    setSaving(true)
    try {
      await performSave()
    } catch (err) {
      // AGENTS.md fallback policy: the server's own message, draft left
      // exactly as typed.
      setSaveError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  const handleConfirmDelete = async () => {
    setPendingDelete(false)
    setDeleting(true)
    try {
      await remove()
      onDeleted()
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err))
    } finally {
      setDeleting(false)
    }
  }

  if (id !== null && loading && !item) {
    return <div className="flex h-full min-h-[240px] items-center justify-center text-sm text-muted-foreground">Loading...</div>
  }
  if (id !== null && error) {
    return (
      <p role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
        {error}
      </p>
    )
  }
  if (id !== null && !item) {
    return (
      <div className="flex h-full min-h-[240px] items-center justify-center text-sm text-muted-foreground">
        This equipment record could not be found.
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      <FieldSet className="rounded-md border border-border bg-card p-4">
        <FieldLegend variant="label">{id === null ? 'New item' : 'Specifications & IDs'}</FieldLegend>
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="equipment-name">Name</FieldLabel>
            <Input id="equipment-name" value={draft.name} onChange={(e) => setDraft((p) => ({ ...p, name: e.target.value }))} />
          </Field>

          <Field orientation="responsive">
            <Field>
              <FieldLabel htmlFor="equipment-category">Category</FieldLabel>
              <Select value={draft.category} onValueChange={(v) => { if (v) setDraft((p) => ({ ...p, category: v as EquipmentCategory })) }}>
                <SelectTrigger id="equipment-category" aria-label="Category">
                  <SelectValue>{(v: string) => (v === 'mechanical' ? 'Mechanical' : 'General')}</SelectValue>
                </SelectTrigger>
                <SelectPopup>
                  <SelectItem value="mechanical">Mechanical</SelectItem>
                  <SelectItem value="general">General</SelectItem>
                </SelectPopup>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor="equipment-system">System</FieldLabel>
              <Select value={draft.system} onValueChange={(v) => { if (v) setDraft((p) => ({ ...p, system: v as EquipmentSystem })) }}>
                <SelectTrigger id="equipment-system" aria-label="System">
                  <SelectValue>{(v: string) => EQUIPMENT_SYSTEM_LABELS[v as EquipmentSystem]}</SelectValue>
                </SelectTrigger>
                <SelectPopup>
                  {EQUIPMENT_SYSTEMS.map((sys) => (
                    <SelectItem key={sys} value={sys}>{EQUIPMENT_SYSTEM_LABELS[sys]}</SelectItem>
                  ))}
                </SelectPopup>
              </Select>
            </Field>
          </Field>

          <Field orientation="responsive">
            <Field>
              <FieldLabel htmlFor="equipment-manufacturer">Manufacturer</FieldLabel>
              <Input id="equipment-manufacturer" value={draft.manufacturer} onChange={(e) => setDraft((p) => ({ ...p, manufacturer: e.target.value }))} />
            </Field>
            <Field>
              <FieldLabel htmlFor="equipment-model">Model</FieldLabel>
              <Input id="equipment-model" value={draft.model} onChange={(e) => setDraft((p) => ({ ...p, model: e.target.value }))} />
            </Field>
          </Field>

          <Field orientation="responsive">
            <Field>
              <FieldLabel htmlFor="equipment-serial">Serial</FieldLabel>
              <Input id="equipment-serial" value={draft.serial} onChange={(e) => setDraft((p) => ({ ...p, serial: e.target.value }))} />
            </Field>
            <Field>
              <FieldLabel htmlFor="equipment-quantity">Quantity</FieldLabel>
              <Input
                id="equipment-quantity"
                type="number"
                min={1}
                value={draft.quantity}
                onChange={(e) => setDraft((p) => ({ ...p, quantity: Math.max(1, Number(e.target.value) || 1) }))}
              />
            </Field>
          </Field>

          <Field>
            <FieldLabel htmlFor="equipment-status">Status</FieldLabel>
            <Select value={draft.status} onValueChange={(v) => { if (v) setDraft((p) => ({ ...p, status: v as EquipmentStatus })) }}>
              <SelectTrigger id="equipment-status" aria-label="Status">
                <SelectValue>{(v: string) => (v === 'deployed' ? 'Deployed' : 'Stored')}</SelectValue>
              </SelectTrigger>
              <SelectPopup>
                <SelectItem value="deployed">Deployed</SelectItem>
                <SelectItem value="stored">Stored</SelectItem>
              </SelectPopup>
            </Select>
          </Field>

          <Field orientation="responsive">
            <Field>
              <FieldLabel htmlFor="equipment-zone">Zone</FieldLabel>
              <Select value={draft.zone_id ?? NO_ZONE_VALUE} onValueChange={(v) => handleZoneChange(v === NO_ZONE_VALUE ? null : (v ?? null))}>
                <SelectTrigger id="equipment-zone" aria-label="Zone">
                  <SelectValue>{(v: string) => (v === NO_ZONE_VALUE ? 'No zone' : zones.find((z) => z.id === v)?.name ?? v)}</SelectValue>
                </SelectTrigger>
                <SelectPopup>
                  <SelectItem value={NO_ZONE_VALUE}>No zone</SelectItem>
                  {zones.map((z) => <SelectItem key={z.id} value={z.id}>{z.name}</SelectItem>)}
                </SelectPopup>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor="equipment-bin">Bin</FieldLabel>
              <Select value={draft.bin_id ?? NO_BIN_VALUE} onValueChange={(v) => handleBinChange(v === NO_BIN_VALUE ? null : (v ?? null))}>
                <SelectTrigger id="equipment-bin" aria-label="Bin">
                  <SelectValue>{(v: string) => (v === NO_BIN_VALUE ? 'No bin' : availableBins.find((b) => b.id === v)?.code ?? v)}</SelectValue>
                </SelectTrigger>
                <SelectPopup>
                  <SelectItem value={NO_BIN_VALUE}>No bin</SelectItem>
                  {availableBins.map((b) => <SelectItem key={b.id} value={b.id}>{b.code}</SelectItem>)}
                </SelectPopup>
              </Select>
            </Field>
          </Field>

          <Field>
            <FieldLabel htmlFor="equipment-location-detail">Location detail</FieldLabel>
            <Input id="equipment-location-detail" value={draft.location_detail} onChange={(e) => setDraft((p) => ({ ...p, location_detail: e.target.value }))} />
            <FieldDescription>The part a bin code cannot carry - "outboard side, behind the raw water strainer".</FieldDescription>
          </Field>

          <Field orientation="responsive">
            <Field>
              <FieldLabel htmlFor="equipment-install-date">Install date</FieldLabel>
              <Input id="equipment-install-date" type="date" value={draft.install_date} onChange={(e) => setDraft((p) => ({ ...p, install_date: e.target.value }))} />
            </Field>
            <Field>
              <FieldLabel htmlFor="equipment-hour-meter-path">Hour-meter path</FieldLabel>
              <Input
                id="equipment-hour-meter-path"
                list="equipment-hour-meter-paths"
                value={draft.hour_meter_path}
                onChange={(e) => setDraft((p) => ({ ...p, hour_meter_path: e.target.value }))}
              />
              <datalist id="equipment-hour-meter-paths">
                {hourMeterPaths.map((p) => <option key={p.path} value={p.path} />)}
              </datalist>
              <FieldDescription>For mechanical gear - the SignalK path its runtime is published on.</FieldDescription>
            </Field>
          </Field>

          <Field>
            <FieldLabel htmlFor="equipment-profile">Profile</FieldLabel>
            <Select value={draft.profile_id || NO_PROFILE_VALUE} onValueChange={(v) => handleProfileChange(v === NO_PROFILE_VALUE ? '' : (v ?? ''))}>
              <SelectTrigger id="equipment-profile" aria-label="Profile">
                <SelectValue>{(v: string) => (v === NO_PROFILE_VALUE ? 'No profile' : profiles.find((p) => p.id === v)?.name ?? v)}</SelectValue>
              </SelectTrigger>
              <SelectPopup>
                <SelectItem value={NO_PROFILE_VALUE}>No profile</SelectItem>
                {profiles.map((p) => <SelectItem key={p.id} value={p.id}>{p.name}</SelectItem>)}
              </SelectPopup>
            </Select>
            <FieldDescription>Fills in blank manufacturer, model and category. Gear with no profile is normal.</FieldDescription>
          </Field>

          <Field>
            <FieldLabel>Aliases</FieldLabel>
            <div className="flex flex-wrap gap-1.5">
              {draft.aliases.length === 0 && <span className="text-xs text-muted-foreground">No aliases yet.</span>}
              {draft.aliases.map((alias) => (
                <Badge key={alias} variant="secondary" className="gap-1">
                  {alias}
                  <button type="button" aria-label={`Remove alias ${alias}`} onClick={() => removeAlias(alias)}>
                    <X className="h-3 w-3" aria-hidden="true" />
                  </button>
                </Badge>
              ))}
            </div>
            <div className="flex items-center gap-2">
              <Input
                aria-label="Add alias"
                value={aliasInput}
                onChange={(e) => setAliasInput(e.target.value)}
                onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addAlias() } }}
                className="max-w-48"
              />
              <Button type="button" variant="outline" size="sm" onClick={addAlias}>Add</Button>
            </div>
            <FieldDescription>What the crew actually calls it - "genset" finds the generator.</FieldDescription>
          </Field>

          <Field orientation="horizontal">
            <Switch checked={draft.verified_aboard} onCheckedChange={(checked) => setDraft((p) => ({ ...p, verified_aboard: checked }))} aria-label="Verified aboard" />
            <FieldLabel>Verified aboard</FieldLabel>
          </Field>

          <Field>
            <FieldLabel htmlFor="equipment-notes">Notes</FieldLabel>
            <Textarea id="equipment-notes" value={draft.notes} onChange={(e) => setDraft((p) => ({ ...p, notes: e.target.value }))} rows={3} />
          </Field>
        </FieldGroup>

        {fieldErrors.length > 0 && (
          <FieldError
            className="rounded-md border border-destructive/40 bg-destructive/10 p-2"
            errors={fieldErrors.map((f) => ({ message: `${f.field}: ${f.message}` }))}
          />
        )}
        {saveError && (
          <p role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {saveError}
          </p>
        )}

        <div className="flex items-center gap-2">
          <Button type="button" onClick={() => { void handleSave() }} disabled={saving || !canWrite}>
            {saving ? 'Saving...' : 'Save'}
          </Button>
          <Button type="button" variant="outline" onClick={onBack}>Back</Button>
          {id !== null && (
            <Button
              type="button"
              variant="ghost"
              className="ml-auto gap-2 text-destructive"
              aria-label="Delete equipment"
              onClick={() => setPendingDelete(true)}
              disabled={deleting || !canWrite}
            >
              <Trash2 className="h-4 w-4" aria-hidden="true" />
              Delete
            </Button>
          )}
        </div>
      </FieldSet>

      {id !== null && (
        <FieldSet className="rounded-md border border-border bg-card p-4">
          <FieldLegend variant="label">Documents</FieldLegend>
          <div className="flex flex-col gap-2">
            {docEntries.length === 0 && <p className="text-sm text-muted-foreground">No documents linked yet.</p>}
            {docEntries.map((doc) => (
              <div key={doc.document_id} className="flex items-center justify-between gap-2 rounded-md border border-border px-3 py-2">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">{doc.title || doc.filename}</p>
                  <p className="truncate text-xs text-muted-foreground">{doc.filename}</p>
                </div>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label={`Remove document ${doc.title || doc.filename}`}
                  onClick={() => removeDocument(doc.document_id)}
                >
                  <X className="h-4 w-4" aria-hidden="true" />
                </Button>
              </div>
            ))}
            {canWrite && (
              <Button type="button" variant="outline" size="sm" className="self-start gap-2" onClick={() => setPickerOpen(true)}>
                <Plus className="h-4 w-4" aria-hidden="true" />
                Add document
              </Button>
            )}
          </div>
        </FieldSet>
      )}

      <DocumentLinkPicker
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        onPick={addDocument}
        excludeIds={docEntries.map((d) => d.document_id)}
      />

      <AlertDialog open={pendingDelete} onOpenChange={(isOpen) => { if (!isOpen) setPendingDelete(false) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete &quot;{item?.name}&quot;?</AlertDialogTitle>
            <AlertDialogDescription>This removes the equipment record. It can&apos;t be undone.</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              onClick={() => { void handleConfirmDelete() }}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
})
