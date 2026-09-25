import { forwardRef, useCallback, useEffect, useImperativeHandle, useLayoutEffect, useMemo, useState } from 'react'
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
import { Checkbox } from '@/components/ui/checkbox'
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
import { photoFilename, usePhotoStaging, type FailedPhotoUpload, type LocalPhoto } from '@/hooks/use-photo-staging'
import { useSignalKPaths } from '@/hooks/use-signalk-paths'
import { formatAppLocation } from '@/lib/app-location'
import {
  BLANK_DRAFT,
  EQUIPMENT_SYSTEMS,
  EQUIPMENT_SYSTEM_LABELS,
  InventoryValidationError,
  createEquipment,
  deleteEquipmentPhoto,
  setEquipmentPhotoOrder,
  useEquipmentItem,
  useInventoryZones,
  type EquipmentCategory,
  type EquipmentInput,
  type EquipmentItem,
  type EquipmentStatus,
  type EquipmentSystem,
  type InventoryFieldError,
} from '@/hooks/use-inventory'
import { downscaleAll, downscaleImage } from '@/lib/image-downscale'

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

/** One item's photos waiting for Retry, and the notice shown for them. */
interface PhotoStatus {
  failures: FailedPhotoUpload[]
  notice: string
}

interface EquipmentEditorProps {
  /** null is the "New item" draft - see this file's header comment. */
  id: string | null
  onBack: () => void
  /** Fired once, right after a successful create POST - App.tsx uses this to
   * navigate to `/inventory/equipment/<id>` (plan: "Create flow: POST, then
   * navigate to the new id; do not leave the operator on a blank form"). */
  onCreated: (id: string) => void
  /** Fired once the item is gone. `message`, when given (2026-09-25
   * amendment), is the server's own word that a photo file or document
   * could not be removed AFTER the item itself was already deleted
   * (deleteEquipmentHandler's own "the item was deleted, but ..." case) -
   * the item is still gone either way, so this still fires; App.tsx shows
   * the message on the destination rather than leaving the editor open on
   * a record that no longer exists. */
  onDeleted: (message?: string) => void
  onDirtyChange?: (dirty: boolean) => void
  /** 2026-09-25 amendment (finding 8): photos still waiting for Retry on
   * the open item are unsaved work for the SAME leave guard bin-quick-add's
   * identical prop drives - Leave/Stay wording with a detail, not the
   * Save-and-Continue wording onDirtyChange's own dirty-draft case gets. */
  onHasWorkChange?: (hasWork: boolean, detail?: string) => void
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
  { id, onBack, onCreated, onDeleted, onDirtyChange, onHasWorkChange, canWrite = true, initialZoneId = null, initialBinId = null },
  ref,
) {
  const { item, documents, loading, error, refresh, update, remove, patchLinkedDocuments, setItem, refreshDocuments } = useEquipmentItem(id)
  const { zones } = useInventoryZones()
  const { profiles } = useEquipmentProfiles(true)
  const { paths } = useSignalKPaths(true)

  const [draft, setDraft] = useState<EquipmentInput>(BLANK_DRAFT)
  // 2026-09-25 refactor: the Documents tab's own PENDING diff - what the
  // operator has picked to add or removed, not yet saved. Replaces the
  // whole-set mirror (docEntries) the whole-set PUT used to require: now
  // that the write is a diff (patchLinkedDocuments), the tab only ever has
  // to say what IT changed, never restate `documents` itself (the server's
  // current truth, including a link some other code path - a photo upload -
  // just added, which this tab never has to know or care about).
  const [pendingAdds, setPendingAdds] = useState<DocEntry[]>([])
  const [pendingRemoves, setPendingRemoves] = useState<string[]>([])
  // Resets pendingAdds/pendingRemoves the moment `id` itself changes - in
  // the SAME render as the change, via React's own "adjust state while
  // rendering" pattern, not a passive effect. A passive effect can still
  // run after some OTHER effect has already read the stale pending state
  // for the previous id (the exact ordering trap the whole-set mirror this
  // replaces needed extra machinery to paper over) - this instead forces an
  // immediate re-render with the reset already applied, before anything
  // else (a photo write's own setItem, say) can observe the old id's
  // pending state under the new id.
  const [pendingForId, setPendingForId] = useState(id)
  if (pendingForId !== id) {
    setPendingForId(id)
    setPendingAdds([])
    setPendingRemoves([])
  }
  const [aliasInput, setAliasInput] = useState('')
  const [pickerOpen, setPickerOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [fieldErrors, setFieldErrors] = useState<InventoryFieldError[]>([])
  const [pendingDelete, setPendingDelete] = useState(false)
  const [deleting, setDeleting] = useState(false)
  // 2026-09-25 amendment: the delete confirm dialog's own "Also delete N
  // photo(s) only this item uses" checkbox - off by default (the operator's
  // own decision: a delete must never destroy a document without being
  // explicitly asked), reset every time the dialog is (re)opened.
  const [deletePhotosOnDelete, setDeletePhotosOnDelete] = useState(false)

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
  const {
    localPhotos,
    setLocalPhotos,
    addLocalPhotos,
    makeCoverLocal,
    removeLocalPhoto,
    uploadPhotosInOrder,
  } = usePhotoStaging({ onDownscaleError: setSaveError, setItem })
  // Failed photo uploads and their notice, per item. The editor stays
  // mounted across Back/Forward, so a single slot let another item show
  // them and Retry file their photos on it, and a failure on one item
  // replaced another item's and lost its photos (final pre-release review
  // findings). Each item's entry shows, and Retry sends, only for that item.
  const [photoStatus, setPhotoStatus] = useState<Record<string, PhotoStatus>>({})
  const [retryingPhotos, setRetryingPhotos] = useState(false)
  const currentPhotoStatus = id !== null ? photoStatus[id] : undefined
  const failedPhotoUploads = currentPhotoStatus?.failures ?? []
  const photoNotice = currentPhotoStatus?.notice ?? null

  // Records one upload batch's outcome against its own item. New failures
  // join any already waiting for that item.
  const recordPhotoOutcome = useCallback((itemId: string, failures: FailedPhotoUpload[], failureNotice: string) => {
    if (failures.length === 0) return
    setPhotoStatus((prev) => {
      const existing = prev[itemId]?.failures ?? []
      return { ...prev, [itemId]: { failures: [...existing, ...failures], notice: failureNotice } }
    })
  }, [])

  // finding 9 (pre-release review): a "Full item" draft's baseline used to
  // stay BLANK_DRAFT even when initialZoneId/initialBinId seeded the draft
  // itself with a real zone/bin - draftDirty compared the seeded draft
  // against a baseline that never saw the seed, so the form read dirty the
  // instant it opened. newDraftBaseline is the SAME seeded value the effect
  // below writes into `draft`, kept alongside it so id===null's own
  // baseline (below) is what was actually seeded, not the unseeded default.
  const [newDraftBaseline, setNewDraftBaseline] = useState<EquipmentInput>(BLANK_DRAFT)

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
      const seeded = { ...BLANK_DRAFT, zone_id: initialZoneId, bin_id: initialBinId }
      setDraft(seeded)
      setNewDraftBaseline(seeded)
      return
    }
    if (item) setDraft(draftFromItem(item))
    // eslint-disable-next-line react-hooks/exhaustive-deps -- keyed on id/item id only, see comment above.
  }, [id, item?.id])

  // finding 9: id===null's own baseline is the SEEDED draft (newDraftBaseline,
  // set by the effect above the moment this draft was born), not BLANK_DRAFT
  // itself - a "Full item" draft that pre-set zone_id/bin_id must compare
  // against a baseline that already carries them, or it reads dirty on
  // arrival with nothing yet typed.
  const baseline = id === null ? newDraftBaseline : (item ? draftFromItem(item) : null)
  // 2026-09-25 refactor: the Documents tab's own list is `documents` (the
  // server's current truth) with pendingRemoves filtered out and pendingAdds
  // appended - never a stored mirror of the whole set. A photo linked by
  // some other code path (an upload) shows up here the instant `documents`
  // itself picks it up (refreshDocuments, below) - this tab never has to be
  // told about it separately.
  const displayedDocuments = useMemo(
    () => [
      ...documents.filter((d) => !pendingRemoves.includes(d.document_id)).map((d) => ({ document_id: d.document_id, title: d.title, filename: d.filename })),
      ...pendingAdds,
    ],
    [documents, pendingRemoves, pendingAdds],
  )
  const draftDirty = baseline !== null && !sameDraft(draft, baseline)
  // Any pending change at all means dirty - no comparison against a moving
  // baseline needed, unlike the whole-set mirror this replaces.
  const linksDirty = pendingAdds.length > 0 || pendingRemoves.length > 0
  const dirty = draftDirty || linksDirty

  useEffect(() => { onDirtyChange?.(dirty) }, [dirty, onDirtyChange])

  // finding 8 (pre-release review): photos still waiting for Retry on the
  // OPEN item are unsaved work for the leave guard, the same way
  // bin-quick-add's own pendingRetries drive its onHasWorkChange - a
  // navigation away used to be free to drop them with no prompt at all,
  // because only draftDirty/linksDirty above ever fed onDirtyChange, and a
  // Retry failure by itself changes neither. Routed through onHasWorkChange
  // (not onDirtyChange) so App.tsx shows the Leave/Stay wording this
  // failure actually needs, not the dirty-draft Save-and-Continue prompt.
  const photoWorkDetail = useMemo(() => {
    const n = failedPhotoUploads.length
    if (n === 0) return undefined
    const who = (item?.name || draft.name || '').trim() || 'this item'
    return `${n} photo${n === 1 ? '' : 's'} for ${who} ${n === 1 ? "hasn't" : "haven't"} uploaded yet.`
  }, [failedPhotoUploads.length, item?.name, draft.name])
  const hasPhotoWork = photoWorkDetail !== undefined
  // useLayoutEffect - see bin-quick-add.tsx's own onHasWorkChange effect for
  // why: App.tsx's guard can read inventoryHasWork right after a state
  // update this same effect is meant to report, with no render in between
  // for an ordinary passive effect to be guaranteed to have caught up.
  useLayoutEffect(() => {
    if (photoWorkDetail !== undefined) onHasWorkChange?.(hasPhotoWork, photoWorkDetail)
    else onHasWorkChange?.(hasPhotoWork)
  }, [hasPhotoWork, photoWorkDetail, onHasWorkChange])

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

  // Picking a document already linked server-side but staged for removal
  // (the operator removed it, then picked it again before Save) just
  // un-stages the removal - it's already linked, there's nothing to add.
  // Otherwise it's a genuinely new pick, staged in pendingAdds (a no-op if
  // already staged - a duplicate pick from the picker).
  const addDocument = (doc: DocumentLinkPickerResult) => {
    const alreadyLinked = documents.some((d) => d.document_id === doc.document_id)
    if (alreadyLinked && pendingRemoves.includes(doc.document_id)) {
      setPendingRemoves((prev) => prev.filter((docId) => docId !== doc.document_id))
      return
    }
    setPendingAdds((prev) => (prev.some((d) => d.document_id === doc.document_id) ? prev : [...prev, doc]))
  }

  // Removing a document that's only a pending add (never actually saved)
  // just un-stages the add - there's nothing server-side to unlink yet.
  // Otherwise it's one of `documents` (the server's current set), staged
  // for removal on the next Save.
  const removeDocument = (documentId: string) => {
    if (pendingAdds.some((d) => d.document_id === documentId)) {
      setPendingAdds((prev) => prev.filter((d) => d.document_id !== documentId))
      return
    }
    setPendingRemoves((prev) => (prev.includes(documentId) ? prev : [...prev, documentId]))
  }

  // Trap: SignalK publishes runTime under all sorts of prefixes
  // (electrical.generator.0.runTime, propulsion.port.runTime...) - the one
  // constant is the last segment, matched case-insensitively since some
  // plugins spell it runtime.
  const hourMeterPaths = useMemo(
    () => paths.filter((p) => /^runtime$/i.test(p.path.split('.').pop() ?? '')),
    [paths],
  )

  // ── photo row (ADR 0127) ────────────────────────────────────────────────
  // Two independent sets of handlers below - one for a brand new draft
  // (id === null, acting on localPhotos via usePhotoStaging), one for a
  // saved record (acting on the server immediately, like the document links
  // already do) - composed into the single onFilesPicked/onMakeCover/
  // onRemove PhotoStripEditor actually receives further down, which
  // branches on `id` itself. usePhotoStaging itself owns addLocalPhotos/
  // makeCoverLocal/removeLocalPhoto and the object-URL cleanup on unmount.

  // ADR 0127 review: this used to `break` on the first failed file, so the
  // remaining picks were never even tried and never offered for Retry -
  // every file gets its own attempt regardless of an earlier one failing. A
  // downscale failure queues for Retry using the ORIGINAL file, flagged
  // needsDownscale so Retry re-runs downscaling before it ever tries to
  // upload again (finding 10 below).
  //
  // uploadPhotosInOrder (usePhotoStaging) applies each successful upload's
  // own returned item via setItem as it lands (not a refresh() afterward) -
  // a write already gets the updated record back, so a second GET is
  // redundant, and - for the create-then-upload transition in particular
  // (performSave's id===null branch) - actively wrong, since
  // useEquipmentItem's own GET for a freshly created id can land before
  // these uploads finish and nothing else would ever catch the item up.
  const uploadPhotosToSavedItem = async (targetId: string, files: File[]) => {
    // Downscaling runs through downscaleAll's worker pool (at most 3 at
    // once, see usePhotoStaging's addLocalPhotos) - the network uploads
    // that follow stay strictly sequential and in the ORIGINAL file order
    // (not completion order), because the server assigns sort_index as
    // each one arrives. A downscale failure never reaches
    // uploadPhotosInOrder at all (there is no Blob to upload) - it is
    // recorded directly, in the same shape uploadPhotosInOrder's own
    // failures are, so Retry treats it identically (finding 10: except it
    // is also flagged needsDownscale, since the blob it holds is still the
    // full-size original).
    const downscaled = await downscaleAll(files)
    const downscaleFailures: FailedPhotoUpload[] = []
    const toUpload: LocalPhoto[] = []
    for (const { file, result } of downscaled) {
      if (result.ok) {
        // previewUrl '' - nothing displays this Blob before it uploads, so
        // there is no object URL for uploadPhotosInOrder to revoke.
        toUpload.push({ id: crypto.randomUUID(), blob: result.blob, filename: photoFilename(file.name), previewUrl: '' })
      } else {
        downscaleFailures.push({ blob: file, filename: photoFilename(file.name), error: result.error, needsDownscale: true })
      }
    }

    const { failures: uploadFailures } = await uploadPhotosInOrder(targetId, toUpload)
    const failures = [...downscaleFailures, ...uploadFailures]
    recordPhotoOutcome(targetId, failures,
      `${failures.length} of ${files.length} photo${files.length === 1 ? '' : 's'} didn't upload: ${failures[0]?.error ?? ''}`)
    // 2026-09-25 refactor: a successful upload already links the photo
    // server-side the instant it returns (item.photo_ids, applied above via
    // setItem inside uploadPhotosInOrder) - this documents-only refetch just
    // catches `documents` up to it, so the Documents tab shows it too. The
    // Documents tab's own pendingAdds/pendingRemoves are untouched - they're
    // only for what THIS tab has staged, orthogonal to what `documents`
    // itself contains.
    void refreshDocuments(targetId)
  }

  const makeCoverSaved = async (photoId: string) => {
    if (id === null || !item) return
    try {
      const updated = await setEquipmentPhotoOrder(id, [photoId, ...item.photo_ids.filter((p) => p !== photoId)])
      setItem(updated)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err))
    }
  }

  // 2026-09-25 amendment: removing a photo from the strip is unlink-only by
  // default - `deletePhoto` (PhotoStripEditor's own "Remove and delete"
  // choice, offered only when item.exclusive_photo_ids names this photo)
  // additionally deletes the underlying document once nothing else links it.
  const removeSavedPhoto = async (photoId: string, deletePhoto: boolean) => {
    if (id === null) return
    try {
      const updated = await deleteEquipmentPhoto(id, photoId, deletePhoto)
      setItem(updated)
      // Review finding: without this, the removed photo's still-stale entry
      // in `documents` starts passing the Documents tab's own list the
      // instant item.photo_ids above stops naming it. 2026-09-25 refactor:
      // a documents-only refetch (replacing pruneDocument) catches
      // `documents` up to the removal - the Documents tab is a view over
      // `documents` now, not a stored mirror this write has to patch
      // directly.
      void refreshDocuments(id)
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      setSaveError(message)
      // deleteEquipmentPhotoHandler only ever fails AFTER the unlink itself
      // has already committed (its own "the photo was removed from the
      // item, but ..." wording) - re-syncing from the server here is what
      // keeps this item's local state from still claiming the photo is
      // linked when the server no longer agrees.
      if (message.includes('was removed from the item')) void refresh()
    }
  }

  // Retry re-sends ONLY the photos that failed - the ones that uploaded
  // fine on the first pass are already linked server-side and are never
  // touched again.
  const retryFailedPhotoUploads = async () => {
    if (id === null || failedPhotoUploads.length === 0) return
    const targetId = id
    setRetryingPhotos(true)
    // finding 10 (pre-release review): a photo that failed to DOWNSCALE was
    // queued holding the original, full-size File (needsDownscale: true) -
    // re-running downscaleImage here, before this photo is ever handed to
    // uploadPhotosInOrder, is what actually retries the failure that
    // happened. Simply re-sending photo.blob as-is (the old behaviour)
    // uploaded the full-size original straight past the size limit
    // downscaling exists to enforce. A photo that still fails to downscale
    // stays queued with that error and is never uploaded.
    const stillNeedsDownscale: FailedPhotoUpload[] = []
    const readyToUpload: LocalPhoto[] = []
    for (const photo of failedPhotoUploads) {
      if (photo.needsDownscale) {
        try {
          const blob = await downscaleImage(photo.blob)
          readyToUpload.push({ id: crypto.randomUUID(), blob, filename: photo.filename, previewUrl: '' })
        } catch (err) {
          stillNeedsDownscale.push({ ...photo, error: err instanceof Error ? err.message : String(err) })
        }
      } else {
        // No previewUrl to revoke for an already-downscaled retry (none was
        // ever created - failedPhotoUploads holds only blob/filename/error)
        // - uploadPhotosInOrder skips the revoke for an empty one.
        readyToUpload.push({ id: crypto.randomUUID(), blob: photo.blob, filename: photo.filename, previewUrl: '' })
      }
    }
    const { failures: stillFailingUpload } = await uploadPhotosInOrder(targetId, readyToUpload)
    // 2026-09-25 refactor: same documents-only refetch as a fresh upload -
    // a retry that lands a photo links it server-side immediately too.
    void refreshDocuments(targetId)
    const stillFailing = [...stillNeedsDownscale, ...stillFailingUpload]
    setPhotoStatus((prev) => {
      const next = { ...prev }
      if (stillFailing.length > 0) {
        next[targetId] = { failures: stillFailing, notice: `Saved, but ${stillFailing.length} photo${stillFailing.length === 1 ? '' : 's'} didn't upload: ${stillFailing[0].error}` }
      } else {
        delete next[targetId]
      }
      return next
    })
    setRetryingPhotos(false)
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
          // finding 7 (pre-release review): this used to clear the WHOLE
          // localPhotos list once the upload settled - the same gap
          // bin-quick-add's own handleSave had, and the same fix: snapshot
          // exactly what this save is about to send, and afterward remove
          // only those ids, by id, leaving anything staged after this save
          // started untouched.
          const toUpload = localPhotos
          // Review finding: each upload's own returned item is applied via
          // setItem (usePhotoStaging's uploadPhotosInOrder) the moment it
          // lands, not a refresh() (or nothing at all, which is what this
          // did before) afterward - useEquipmentItem's own GET for
          // `created.id` (fired the instant onCreated above flips the `id`
          // prop) routinely lands before this loop finishes, and without
          // this the photo row stayed empty: nothing was ever going to
          // re-fetch it again once that GET's stale response was in.
          // adopt: this upload can land before the re-render that brings
          // created.id in as the hook's `id` (see setItem's own comment).
          const { failures } = await uploadPhotosInOrder(created.id, toUpload, { adopt: true })
          recordPhotoOutcome(created.id, failures,
            `Saved, but ${failures.length} of ${toUpload.length} photo${toUpload.length === 1 ? '' : 's'} didn't upload: ${failures[0]?.error ?? ''}`)
          const uploadedIds = new Set(toUpload.map((p) => p.id))
          setLocalPhotos((prev) => prev.filter((p) => !uploadedIds.has(p.id)))
          // 2026-09-25 refactor: catches `documents` up to the newly created
          // item's own just-uploaded photos, the same documents-only
          // refetch every other photo write site now does.
          void refreshDocuments(created.id)
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
      // PATCH-based diff (backend replaced the whole-set PUT): this tab only
      // ever sends what IT staged - a link it doesn't know about (a photo
      // some other code path just uploaded) is never named in either list,
      // so it can never be silently unlinked. patchLinkedDocuments' own
      // refresh() afterward catches `documents` up; pendingAdds/pendingRemoves
      // are cleared here rather than left for the id-change reset, since
      // nothing about id changed - this Save just succeeded.
      if (pendingAdds.length > 0 || pendingRemoves.length > 0) {
        await patchLinkedDocuments(pendingAdds.map((d) => d.document_id), pendingRemoves)
        setPendingAdds([])
        setPendingRemoves([])
      }
    } catch (err) {
      if (err instanceof InventoryValidationError) setFieldErrors(err.fields)
      throw err
    }
  }, [id, draft, pendingAdds, pendingRemoves, localPhotos, setLocalPhotos, uploadPhotosInOrder, recordPhotoOutcome, update, patchLinkedDocuments, refreshDocuments, onCreated])

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
      await remove(deletePhotosOnDelete)
      onDeleted()
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      // deleteEquipmentHandler only ever fails AFTER the item row itself is
      // already gone - its one post-delete failure path is a photo file
      // that could not be removed, worded "the item was deleted, but ..."
      // (inventory_handlers.go). onDeleted still fires either way; the
      // message travels with it so App.tsx can show it on the destination
      // instead of leaving this editor open on a record that no longer
      // exists (2026-09-25 amendment).
      if (message.includes('the item was deleted')) {
        onDeleted(message)
      } else {
        setSaveError(message)
      }
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
          onRemove={(photoId, deletePhoto) => {
            // A draft's local photos have no exclusivity to speak of yet -
            // nothing is linked or shared until Save - so deletePhoto is
            // never offered (exclusivePhotoIds is omitted below) and this
            // branch never receives true for one.
            if (id === null) { removeLocalPhoto(photoId) } else { void removeSavedPhoto(photoId, deletePhoto) }
          }}
          exclusivePhotoIds={id === null ? undefined : item?.exclusive_photo_ids}
        />
        {photoNotice && (
          <div role="alert" className="flex items-center justify-between gap-2 rounded-md border border-border bg-muted px-3 py-2 text-sm">
            <span>{photoNotice}</span>
            {/* Review finding: a 409 refusal never populates
                failedPhotoUploads (it's dropped, not queued) - Retry has
                nothing to re-send for a notice that's ONLY a 409, so the
                button is withheld rather than offering a retry that can
                only ever repeat the same refusal. */}
            {failedPhotoUploads.length > 0 && (
              <Button type="button" variant="outline" size="sm" disabled={retryingPhotos} onClick={() => { void retryFailedPhotoUploads() }}>
                {retryingPhotos ? 'Retrying...' : 'Retry'}
              </Button>
            )}
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
              onClick={() => { setDeletePhotosOnDelete(false); setPendingDelete(true) }}
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
            {displayedDocuments.length === 0 && <p className="text-sm text-muted-foreground">No documents linked yet.</p>}
            {displayedDocuments.map((doc) => (
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
        // Excludes whatever the Documents tab is CURRENTLY showing -
        // displayedDocuments, not just `documents` - so a document already
        // staged as a pending add (not yet saved) is excluded too, the same
        // "no offering the same document twice" rule as an already-linked
        // one.
        excludeIds={displayedDocuments.map((d) => d.document_id)}
      />

      <AlertDialog open={pendingDelete} onOpenChange={(isOpen) => { if (!isOpen) setPendingDelete(false) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete &quot;{item?.name}&quot;?</AlertDialogTitle>
            <AlertDialogDescription>This removes the equipment record. It can&apos;t be undone.</AlertDialogDescription>
          </AlertDialogHeader>
          {/* 2026-09-25 amendment: only shown when N > 0 - an item with no
              exclusive photos has nothing this checkbox could offer to
              delete, and showing it anyway would ask about a choice that
              doesn't exist. Off by default: the operator's own decision
              that a delete never destroys a document without being asked. */}
          {(item?.exclusive_photo_ids.length ?? 0) > 0 && (
            <div className="flex flex-col gap-2">
              <label className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={deletePhotosOnDelete}
                  onCheckedChange={(checked) => setDeletePhotosOnDelete(checked === true)}
                />
                Also delete {item?.exclusive_photo_ids.length} photo{item?.exclusive_photo_ids.length === 1 ? '' : 's'} only this item uses
              </label>
              {/* Finding 3 (review): small thumbnails of exactly the photos
                  this checkbox would delete - a bare count gives the
                  operator nothing to actually recognise before confirming a
                  delete that takes them along with the item. */}
              {deletePhotosOnDelete && (
                <div className="flex flex-wrap gap-1.5 pl-6">
                  {(item?.exclusive_photo_ids ?? []).map((photoId) => (
                    <img
                      key={photoId}
                      src={`${apiBaseUrl}/api/documents/${encodeURIComponent(photoId)}/content`}
                      alt="Photo to delete"
                      className="h-12 w-12 shrink-0 rounded-md border border-border object-cover"
                    />
                  ))}
                </div>
              )}
            </div>
          )}
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
