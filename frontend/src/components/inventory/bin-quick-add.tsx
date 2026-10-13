import { useMemo, useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { PhotoStripEditor, type PhotoStripPhoto } from '@/components/inventory/photo-strip-editor'
import { BLANK_DRAFT, createEquipment, type EquipmentInput } from '@/hooks/use-inventory'
import { useNavigationGuard } from '@/hooks/use-navigation-guard'
import { inventoryWorkClosedBy } from '@/lib/inventory-navigation-guard'
import { usePhotoStaging, type FailedPhotoUpload } from '@/hooks/use-photo-staging'

// ADR 0127 (the plan's A5b): the video's own workflow - stand at the open
// bin, photograph each thing, name it. Reuses PhotoStripEditor's local
// photo list (photo-strip-editor.tsx) and usePhotoStaging's own
// create-then-upload sequence, the same one equipment-editor.tsx's own
// draft Save runs, but as its OWN small form rather than the full
// Specifications editor - Full item (bin-page.tsx) is the escape hatch to
// that when a quick record isn't enough.

interface BinQuickAddProps {
  zoneId: string
  binId: string
  /** Fired once the item is created (whether or not every photo uploaded) -
   * the bin page's own contents list refresh. */
  onCreated: () => void
  canWrite?: boolean
  /** The bin's code, to tell a move that stays on this bin from one that leaves it. */
  binCode: string
}

/** One saved item's photos still waiting for Retry. */
interface PendingRetry {
  itemId: string
  name: string
  failures: FailedPhotoUpload[]
  notice: string
}

export function BinQuickAdd({ zoneId, binId, onCreated, canWrite = true, binCode }: BinQuickAddProps) {
  const [name, setName] = useState('')
  const [quantity, setQuantity] = useState(1)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  // Photos still waiting for Retry, one entry per saved item. The form
  // clears after every save and moves on to the next item, so a single
  // slot here used to be overwritten by the next save and the earlier
  // item's photos were silently lost (final pre-release review finding).
  // `name` is kept because the form's own Name field is already blank by
  // the time anything reads an entry.
  const [pendingRetries, setPendingRetries] = useState<PendingRetry[]>([])
  const [retryingItemId, setRetryingItemId] = useState<string | null>(null)
  // Review finding: Enter in the Name field calls handleSave directly, with
  // no in-flight guard - the `saving` state above is too slow to catch a
  // second Enter (or a stray Enter-then-click) pressed before React has
  // re-rendered it into either handler's own closure. A ref updates
  // synchronously, so it is what actually blocks a second call that starts
  // before the first one's first await ever yields.
  const savingRef = useRef(false)

  const {
    localPhotos: photos,
    setLocalPhotos: setPhotos,
    addLocalPhotos: addFiles,
    makeCoverLocal: makeCover,
    removeLocalPhoto: removePhoto,
    uploadPhotosInOrder,
  } = usePhotoStaging({ onDownscaleError: setSaveError })

  // Release-fixes code-review finding: a partial-failure save clears name
  // and photos (the form's own "ready for the next item" contract, handleSave
  // below) but leaves failedUploads/savedItemId holding photos nothing has
  // actually sent - those are exactly as much unsent work as a staged name or
  // photo, and leaving the bin used to drop them with no prompt because this
  // check never looked at them.
  const hasWork = useMemo(
    () => name.trim() !== '' || photos.length > 0 || pendingRetries.length > 0,
    [name, photos, pendingRetries],
  )
  // Wording for the failedUploads case specifically - the fallback "name and
  // photos you have added" copy App.tsx's dialog otherwise shows would be
  // wrong here (the form's own name/photo fields are empty; nothing was "just
  // added"). null when hasWork is true for the ordinary staged-draft reason
  // instead, so App.tsx's own generic copy still applies there.
  const detail = useMemo(() => {
    if (pendingRetries.length === 0) return undefined
    const n = pendingRetries.reduce((total, entry) => total + entry.failures.length, 0)
    const who = pendingRetries.length === 1 ? pendingRetries[0].name : `${pendingRetries.length} items`
    return `${n} photo${n === 1 ? '' : 's'} for ${who} ${n === 1 ? "hasn't" : "haven't"} uploaded yet.`
  }, [pendingRetries])
  // ADR 0171: the form owns the guard for what it holds. Only a move off this
  // bin is held; `detail` replaces the dialog's generic copy when photos are
  // still queued for Retry. Registered in a layout effect, so it is live the
  // instant Full item is pressed after a staged change.
  useNavigationGuard(hasWork
    ? {
        owner: 'inventory',
        label: 'Inventory',
        blocks: (_from, to) => (
          inventoryWorkClosedBy({ view: 'bin', code: binCode }, to) ? { kind: 'quick-add-work', detail: detail ?? null } : null
        ),
      }
    : null)

  const handleSave = async () => {
    const trimmedName = name.trim()
    if (trimmedName === '') return
    if (savingRef.current) return
    savingRef.current = true
    setSaving(true)
    setSaveError(null)
    // Captured before the first await: finding 7 (pre-release review) - a
    // save clearing `name`/`photos` unconditionally at the end wiped a name
    // typed or a photo taken WHILE the create/upload round trip was still in
    // flight. Only what THIS save actually captured gets cleared below - the
    // exact snapshot taken here, nothing added afterward.
    const nameAtSaveStart = name
    const photosSnapshot = photos
    try {
      // BLANK_DRAFT already carries system's own server default 'other' -
      // "that is the current contract, not a new fallback" (the plan's own
      // words) - so only the fields this form actually collects override it.
      const input: EquipmentInput = {
        ...BLANK_DRAFT,
        name: trimmedName,
        quantity,
        status: 'stored',
        zone_id: zoneId,
        bin_id: binId,
      }
      const created = await createEquipment(input)

      const { failures } = await uploadPhotosInOrder(created.id, photosSnapshot)

      // Only this item's own outcome changes here - an earlier item's
      // photos still waiting for Retry are left exactly where they are.
      if (failures.length > 0) {
        const notice = `Saved ${trimmedName}, but ${failures.length} photo${failures.length === 1 ? '' : 's'} didn't upload: ${failures[0].error}`
        setPendingRetries((prev) => [...prev, { itemId: created.id, name: trimmedName, failures, notice }])
      }

      // The camera is ready for the next item - "there is no silent
      // rollback" (the plan's own words) means a partial photo failure
      // still leaves the item saved, not the form reopened on it for
      // correction. Remove exactly the photos THIS save captured, by id -
      // every one of them is now accounted for, uploaded or moved into
      // pendingRetries above with its own blob copy - leaving any photo
      // added to the strip while this save was still running. The name
      // resets only if it still equals what this save sent; an operator who
      // has since typed a new name for the next item keeps it.
      const uploadedIds = new Set(photosSnapshot.map((p) => p.id))
      setPhotos((prev) => prev.filter((p) => !uploadedIds.has(p.id)))
      setName((prev) => (prev === nameAtSaveStart ? '' : prev))
      setQuantity(1)
      onCreated()
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err))
    } finally {
      savingRef.current = false
      setSaving(false)
    }
  }

  const handleRetry = async (entry: PendingRetry) => {
    setRetryingItemId(entry.itemId)
    // No previewUrl to revoke for a retried photo (none was ever created -
    // an entry holds only blob/filename/error) - uploadPhotosInOrder skips
    // the revoke for an empty one.
    const toRetry = entry.failures.map((photo) => ({ id: crypto.randomUUID(), blob: photo.blob, filename: photo.filename, previewUrl: '' }))
    const { failures: stillFailing } = await uploadPhotosInOrder(entry.itemId, toRetry)
    setPendingRetries((prev) => {
      const others = prev.filter((p) => p.itemId !== entry.itemId)
      if (stillFailing.length === 0) return others
      const notice = `Saved, but ${stillFailing.length} photo${stillFailing.length === 1 ? '' : 's'} didn't upload: ${stillFailing[0].error}`
      return prev.map((p) => (p.itemId === entry.itemId ? { ...p, failures: stillFailing, notice } : p))
    })
    setRetryingItemId(null)
    onCreated()
  }

  const photoStripPhotos: PhotoStripPhoto[] = photos.map((p, i) => ({ id: p.id, previewUrl: p.previewUrl, isCover: i === 0 }))

  if (!canWrite) return null

  return (
    <div className="flex flex-col gap-2 rounded-md border border-border bg-card p-3">
      <PhotoStripEditor
        photos={photoStripPhotos}
        onFilesPicked={(files) => { void addFiles(files) }}
        onMakeCover={makeCover}
        onRemove={(photoId) => removePhoto(photoId)}
      />
      <div className="flex flex-wrap items-end gap-2">
        <Field className="min-w-0 flex-1">
          <FieldLabel htmlFor="quick-add-name">Name</FieldLabel>
          <Input
            id="quick-add-name"
            value={name}
            placeholder="Gaffer tape"
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); void handleSave() } }}
          />
        </Field>
        <Field className="w-20 shrink-0">
          <FieldLabel htmlFor="quick-add-qty">Qty</FieldLabel>
          <Input
            id="quick-add-qty"
            type="number"
            min={1}
            value={quantity}
            onChange={(e) => setQuantity(Math.max(1, Number(e.target.value) || 1))}
          />
        </Field>
        <Button type="button" onClick={() => { void handleSave() }} disabled={saving || name.trim() === ''}>
          {saving ? 'Saving...' : 'Save'}
        </Button>
      </div>
      {saveError && (
        <p role="alert" className="text-sm text-destructive">{saveError}</p>
      )}
      {pendingRetries.map((entry) => (
        <div key={entry.itemId} role="alert" className="flex items-center justify-between gap-2 rounded-md border border-border bg-muted px-3 py-2 text-sm">
          <span className="min-w-0">{entry.notice}</span>
          <Button type="button" variant="outline" size="sm" disabled={retryingItemId === entry.itemId} onClick={() => { void handleRetry(entry) }}>
            {retryingItemId === entry.itemId ? 'Retrying...' : 'Retry'}
          </Button>
        </div>
      ))}
    </div>
  )
}
