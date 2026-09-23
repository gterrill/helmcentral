import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from 'react'
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
import { PhotoStripEditor, type PhotoStripPhoto } from '@/components/inventory/photo-strip-editor'
import { TagRow } from '@/components/inventory/tag-row'
import { apiBaseUrl } from '@/config/api'
import { useEquipmentProfiles } from '@/hooks/use-equipment-profiles'
import { useSignalKPaths } from '@/hooks/use-signalk-paths'
import { formatAppLocation } from '@/lib/app-location'
import {
  EQUIPMENT_SYSTEMS,
  EQUIPMENT_SYSTEM_LABELS,
  InventoryValidationError,
  createEquipment,
  deleteEquipmentPhoto,
  setEquipmentPhotoOrder,
  uploadEquipmentPhoto,
  useEquipmentItem,
  useInventoryZones,
  type EquipmentCategory,
  type EquipmentInput,
  type EquipmentItem,
  type EquipmentStatus,
  type EquipmentSystem,
  type InventoryFieldError,
} from '@/hooks/use-inventory'
import { downscaleImage } from '@/lib/image-downscale'

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

/** ADR 0127: a picked-but-not-yet-uploaded photo on a brand new draft -
 * downscaled already (lib/image-downscale.ts runs the moment a file is
 * picked, never deferred to Save), previewed through an object URL that
 * MUST be revoked once it's no longer needed (a successful upload, a
 * Remove, or the component unmounting with the draft abandoned). */
interface LocalPhoto {
  id: string
  blob: Blob
  filename: string
  previewUrl: string
}

/** A photo whose upload failed right after Save created the item - holds
 * the Blob itself (not just an id) so Retry can re-send the exact same
 * bytes without asking the operator to pick the file again. No previewUrl:
 * by the time this exists, `id` is no longer null and the photo strip has
 * already switched to reading the saved item's own photo_ids, so there is
 * nothing left displaying this Blob to revoke a URL for. */
interface FailedPhotoUpload {
  blob: Blob
  filename: string
  error: string
}

/** ADR 0127: JPEG/PNG re-encoding always renames to .jpg (downscaleImage's
 * own output format) - a camera capture's filename is often meaningless
 * anyway ("image.heic", "blob"), and giving every upload a real extension
 * that matches its actual bytes is one less thing to get wrong. */
function photoFilename(original: string): string {
  const base = original.replace(/\.[^.]+$/, '').trim()
  return `${base || 'photo'}.jpg`
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
  /** ADR 0127: the bin page's "Full item" action pre-sets a brand new
   * draft's location - read only once, when a NEW draft (id === null)
   * first mounts (the same "re-seed only on a different record" effect
   * below), never on a saved record being edited. */
  initialZoneId?: string | null
  initialBinId?: string | null
}

export interface EquipmentEditorHandle {
  save: () => Promise<void>
}

export const EquipmentEditor = forwardRef<EquipmentEditorHandle, EquipmentEditorProps>(function EquipmentEditor(
  { id, onBack, onCreated, onDeleted, onDirtyChange, canWrite = true, initialZoneId = null, initialBinId = null },
  ref,
) {
  const { item, documents, loading, error, refresh, update, remove, setLinkedDocuments } = useEquipmentItem(id)
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

  // ADR 0127: the photo row's own state - see LocalPhoto/FailedPhotoUpload's
  // doc comments above for why a draft needs BOTH of these (not just one
  // list that empties on success) rather than folding into `draft` itself:
  // photo changes are never part of the specifications form's dirty check
  // (the plan's own "photo changes don't make the form dirty"), and they
  // survive the id===null -> id=<created> transition Save causes (this
  // component instance is NOT remounted by that - only `id` the prop
  // changes - so this local state rides straight through it, which is
  // exactly what lets the "Saved, but 2 of 5 didn't upload" notice still
  // show once the editor is showing the saved record).
  const [localPhotos, setLocalPhotos] = useState<LocalPhoto[]>([])
  const [failedPhotoUploads, setFailedPhotoUploads] = useState<FailedPhotoUpload[]>([])
  const [photoNotice, setPhotoNotice] = useState<string | null>(null)
  const [retryingPhotos, setRetryingPhotos] = useState(false)

  // Re-seeds only when a DIFFERENT record has loaded (id, or - for a brand
  // new draft - a one-time reset to blank), not on every incidental
  // re-render of the same one, mirroring document-details-page.tsx's own
  // draft effect and its reasoning: update()'s echoed-back item would
  // otherwise re-run this and discard an edit in progress.
  useEffect(() => {
    if (id === null) {
      // ADR 0127: the bin page's "Full item" pre-sets a brand new draft's
      // location - read once, at the moment this draft is born (this
      // effect's own "only on a different record" reasoning applies
      // identically to a NEW record: EquipmentEditor unmounts and remounts
      // fresh between one "New item" press and the next, so there is
      // exactly one render where initialZoneId/initialBinId matter).
      setDraft({ ...BLANK_DRAFT, zone_id: initialZoneId, bin_id: initialBinId })
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
    // ADR 0127: photo-tagged links are excluded here - the photo row above
    // shows them, and "Photo links are managed only through the photo
    // routes" (the plan's own words) means this Documents-tab list must
    // never offer to manage one too, the same restriction the backend's
    // SetEquipmentDocuments already enforces on the write side.
    const photoIds = new Set(item?.photo_ids ?? [])
    setDocEntries(
      documents
        .filter((d) => !photoIds.has(d.document_id))
        .map((d) => ({ document_id: d.document_id, title: d.title, filename: d.filename })),
    )
    // Keyed on the fetched set's own content, not the array reference -
    // useEquipmentItem builds a fresh `documents` array on every refresh()
    // even when the set is unchanged, and re-seeding on every one of those
    // would throw away a locally staged add/remove before Save ever runs.
    // photo_ids is keyed the same way for the same reason.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, documents.map((d) => d.document_id).join(','), (item?.photo_ids ?? []).join(',')])

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

  // Object URLs are a browser-level resource, not a React one - abandoning
  // a draft (Back, or navigating away) without ever pressing Save must
  // still release them, or every unfinished "New item" leaks one blob URL
  // per photo picked for the life of the tab. Deliberately an EMPTY
  // dependency array with a ref-mirrored read at cleanup time (not
  // `[localPhotos]`, which would revoke and immediately re-grant new
  // object URLs on every single photo pick) - this only ever runs once, on
  // unmount, over whatever the list holds at that moment.
  const localPhotosRef = useRef<LocalPhoto[]>(localPhotos)
  useEffect(() => { localPhotosRef.current = localPhotos }, [localPhotos])
  useEffect(() => () => {
    for (const photo of localPhotosRef.current) URL.revokeObjectURL(photo.previewUrl)
  }, [])

  // ── photo row (ADR 0127) ────────────────────────────────────────────────
  // Two independent sets of handlers below - one for a brand new draft
  // (id === null, acting on localPhotos), one for a saved record (acting on
  // the server immediately, like the document links already do) - composed
  // into the single onFilesPicked/onMakeCover/onRemove PhotoStripEditor
  // actually receives further down, which branches on `id` itself.

  const addLocalPhotos = async (files: File[]) => {
    for (const file of files) {
      try {
        const downscaled = await downscaleImage(file)
        const previewUrl = URL.createObjectURL(downscaled)
        setLocalPhotos((prev) => [...prev, { id: crypto.randomUUID(), blob: downscaled, filename: photoFilename(file.name), previewUrl }])
      } catch (err) {
        // AGENTS.md fallback policy: the real reason a photo couldn't be
        // prepared (a canvas failure, an unreadable file), never silently
        // dropped.
        setSaveError(err instanceof Error ? err.message : String(err))
      }
    }
  }

  const makeCoverLocal = (photoId: string) => {
    setLocalPhotos((prev) => {
      const idx = prev.findIndex((p) => p.id === photoId)
      if (idx <= 0) return prev
      const next = [...prev]
      const [moved] = next.splice(idx, 1)
      next.unshift(moved)
      return next
    })
  }

  const removeLocalPhoto = (photoId: string) => {
    setLocalPhotos((prev) => {
      const target = prev.find((p) => p.id === photoId)
      if (target) URL.revokeObjectURL(target.previewUrl)
      return prev.filter((p) => p.id !== photoId)
    })
  }

  const uploadPhotosToSavedItem = async (targetId: string, files: File[]) => {
    for (const file of files) {
      try {
        const downscaled = await downscaleImage(file)
        await uploadEquipmentPhoto(targetId, downscaled, photoFilename(file.name))
      } catch (err) {
        setSaveError(err instanceof Error ? err.message : String(err))
        break
      }
    }
    await refresh()
  }

  const makeCoverSaved = async (photoId: string) => {
    if (id === null || !item) return
    try {
      await setEquipmentPhotoOrder(id, [photoId, ...item.photo_ids.filter((p) => p !== photoId)])
      await refresh()
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err))
    }
  }

  const removeSavedPhoto = async (photoId: string) => {
    if (id === null) return
    try {
      await deleteEquipmentPhoto(id, photoId)
      await refresh()
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err))
    }
  }

  // Retry re-sends ONLY the photos that failed - the ones that uploaded
  // fine on the first pass are already linked server-side and are never
  // touched again.
  const retryFailedPhotoUploads = async () => {
    if (id === null || failedPhotoUploads.length === 0) return
    setRetryingPhotos(true)
    const stillFailing: FailedPhotoUpload[] = []
    for (const photo of failedPhotoUploads) {
      try {
        await uploadEquipmentPhoto(id, photo.blob, photo.filename)
      } catch (err) {
        stillFailing.push({ ...photo, error: err instanceof Error ? err.message : String(err) })
      }
    }
    setFailedPhotoUploads(stillFailing)
    setPhotoNotice(stillFailing.length === 0
      ? null
      : `Saved, but ${stillFailing.length} photo${stillFailing.length === 1 ? '' : 's'} didn't upload: ${stillFailing[0].error}`)
    setRetryingPhotos(false)
    await refresh()
  }

  const photoStripPhotos: PhotoStripPhoto[] = id === null
    ? localPhotos.map((p, i) => ({ id: p.id, previewUrl: p.previewUrl, isCover: i === 0 }))
    : (item?.photo_ids ?? []).map((photoId, i) => ({
        id: photoId,
        previewUrl: `${apiBaseUrl}/api/documents/${encodeURIComponent(photoId)}/content`,
        isCover: i === 0,
      }))

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
        // ADR 0127: "create the item; upload the photos one at a time, in
        // order; release the object URLs." A failed upload does NOT roll
        // back the create or stop the remaining photos from being tried -
        // "there is no silent rollback" (the plan's own words) means the
        // item stays exactly as saved, and every photo gets its own
        // attempt regardless of an earlier one failing.
        if (localPhotos.length > 0) {
          const toUpload = localPhotos
          setLocalPhotos([])
          const failures: FailedPhotoUpload[] = []
          for (const photo of toUpload) {
            try {
              await uploadEquipmentPhoto(created.id, photo.blob, photo.filename)
              URL.revokeObjectURL(photo.previewUrl)
            } catch (err) {
              URL.revokeObjectURL(photo.previewUrl)
              failures.push({ blob: photo.blob, filename: photo.filename, error: err instanceof Error ? err.message : String(err) })
            }
          }
          if (failures.length > 0) {
            setFailedPhotoUploads(failures)
            setPhotoNotice(`Saved, but ${failures.length} of ${toUpload.length} photo${toUpload.length === 1 ? '' : 's'} didn't upload: ${failures[0].error}`)
          }
        }
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
  }, [id, draft, docEntries, baselineDocIds, localPhotos, update, setLinkedDocuments, onCreated])

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
        <FieldLegend variant="label">Photos</FieldLegend>
        <PhotoStripEditor
          photos={photoStripPhotos}
          canWrite={canWrite}
          onFilesPicked={(files) => {
            if (id === null) { void addLocalPhotos(files) } else { void uploadPhotosToSavedItem(id, files) }
          }}
          onMakeCover={(photoId) => {
            if (id === null) { makeCoverLocal(photoId) } else { void makeCoverSaved(photoId) }
          }}
          onRemove={(photoId) => {
            if (id === null) { removeLocalPhoto(photoId) } else { void removeSavedPhoto(photoId) }
          }}
        />
        {photoNotice && (
          <div role="alert" className="flex items-center justify-between gap-2 rounded-md border border-border bg-muted px-3 py-2 text-sm">
            <span>{photoNotice}</span>
            <Button type="button" variant="outline" size="sm" disabled={retryingPhotos} onClick={() => { void retryFailedPhotoUploads() }}>
              {retryingPhotos ? 'Retrying...' : 'Retry'}
            </Button>
          </div>
        )}
      </FieldSet>

      {/* formatAppLocation (not raw interpolation) - same reasoning as
          bin-page.tsx's own Tag row, so this path is encoded exactly the
          way the app's own /inventory/equipment/<id> URLs are. */}
      {id !== null && <TagRow path={formatAppLocation({ panel: 'inventory', inventorySection: 'equipment', equipmentEditId: id }, { firstPageId: null })} />}

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
        // ADR 0127: this item's OWN photos are excluded too, not just its
        // already-linked non-photo documents - "the photo row shows them"
        // (the plan's own words), so offering one here would let the
        // operator link it a second time through the wrong control.
        excludeIds={[...docEntries.map((d) => d.document_id), ...(item?.photo_ids ?? [])]}
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
