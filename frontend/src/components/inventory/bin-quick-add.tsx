import { useLayoutEffect, useMemo, useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { PhotoStripEditor, type PhotoStripPhoto } from '@/components/inventory/photo-strip-editor'
import { BLANK_DRAFT, PhotoAlreadyLinkedError, createEquipment, uploadEquipmentPhoto, type EquipmentInput } from '@/hooks/use-inventory'
import { downscaleAll } from '@/lib/image-downscale'

// ADR 0127 (the plan's A5b): the video's own workflow - stand at the open
// bin, photograph each thing, name it. Reuses PhotoStripEditor's local
// photo list (photo-strip-editor.tsx) and the same create-then-upload
// sequence equipment-editor.tsx's own draft Save runs, but as its OWN small
// form rather than the full Specifications editor - Full item (bin-page.tsx)
// is the escape hatch to that when a quick record isn't enough.

interface LocalPhoto {
  id: string
  blob: Blob
  filename: string
  previewUrl: string
}

interface FailedUpload {
  blob: Blob
  filename: string
  error: string
}

function photoFilename(original: string): string {
  const base = original.replace(/\.[^.]+$/, '').trim()
  return `${base || 'photo'}.jpg`
}

interface BinQuickAddProps {
  zoneId: string
  binId: string
  /** Fired once the item is created (whether or not every photo uploaded) -
   * the bin page's own contents list refresh. */
  onCreated: () => void
  canWrite?: boolean
  /** Release-fixes code-review finding: reports whether this form holds a
   * staged name or photos a navigation away would silently clear -
   * App.tsx routes the bin page's "Full item" button through the same
   * unsaved-work guard equipment-editor.tsx's onDirtyChange already gets
   * when this is true. `detail`, when given, is wording for what is
   * actually staged (see the second review finding on `hasWork` below) -
   * App.tsx's dialog uses it in place of its own generic copy when present. */
  onHasWorkChange?: (hasWork: boolean, detail?: string) => void
}

export function BinQuickAdd({ zoneId, binId, onCreated, canWrite = true, onHasWorkChange }: BinQuickAddProps) {
  const [name, setName] = useState('')
  const [quantity, setQuantity] = useState(1)
  const [photos, setPhotos] = useState<LocalPhoto[]>([])
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [failedUploads, setFailedUploads] = useState<FailedUpload[]>([])
  const [savedItemId, setSavedItemId] = useState<string | null>(null)
  // The item's name, for failedUploads' own onHasWorkChange detail below -
  // `name` itself is blanked as soon as the save that produced these
  // failures completes (see handleSave's own comment on why), so by the
  // time anything reads failedUploads there is nothing else left to name
  // the still-queued photos after.
  const [savedItemName, setSavedItemName] = useState<string | null>(null)
  const [retrying, setRetrying] = useState(false)
  // Review finding: Enter in the Name field calls handleSave directly, with
  // no in-flight guard - the `saving` state above is too slow to catch a
  // second Enter (or a stray Enter-then-click) pressed before React has
  // re-rendered it into either handler's own closure. A ref updates
  // synchronously, so it is what actually blocks a second call that starts
  // before the first one's first await ever yields.
  const savingRef = useRef(false)

  // Release-fixes code-review finding: a partial-failure save clears name
  // and photos (the form's own "ready for the next item" contract, handleSave
  // below) but leaves failedUploads/savedItemId holding photos nothing has
  // actually sent - those are exactly as much unsent work as a staged name or
  // photo, and leaving the bin used to drop them with no prompt because this
  // check never looked at them.
  const hasWork = useMemo(
    () => name.trim() !== '' || photos.length > 0 || failedUploads.length > 0,
    [name, photos, failedUploads],
  )
  // Wording for the failedUploads case specifically - the fallback "name and
  // photos you have added" copy App.tsx's dialog otherwise shows would be
  // wrong here (the form's own name/photo fields are empty; nothing was "just
  // added"). null when hasWork is true for the ordinary staged-draft reason
  // instead, so App.tsx's own generic copy still applies there.
  const detail = useMemo(() => {
    if (failedUploads.length === 0) return undefined
    const n = failedUploads.length
    const who = savedItemName ?? 'this item'
    return `${n} photo${n === 1 ? '' : 's'} for ${who} ${n === 1 ? "hasn't" : "haven't"} uploaded yet.`
  }, [failedUploads, savedItemName])
  // useLayoutEffect - see stocktake-section.tsx's own onHasWorkChange effect
  // for why: App.tsx's guard can read inventoryHasWork right after a state
  // update this same effect is meant to report, with no render in between
  // for an ordinary passive effect to be guaranteed to have caught up.
  useLayoutEffect(() => {
    if (detail !== undefined) onHasWorkChange?.(hasWork, detail)
    else onHasWorkChange?.(hasWork)
  }, [hasWork, detail, onHasWorkChange])

  // Downscaling runs through downscaleAll's own worker pool (at most 3 at
  // once - review finding: Promise.all(files.map(downscaleImage)) decoded
  // every picked file into memory at the same time, risking the tab running
  // out of memory on a phone) - see equipment-editor.tsx's own
  // addLocalPhotos for the identical reasoning. Results are still applied
  // in the ORIGINAL file order, not completion order.
  const addFiles = async (files: File[]) => {
    const outcomes = await downscaleAll(files)
    let lastError: string | null = null
    for (const { file, result } of outcomes) {
      if (result.ok) {
        const previewUrl = URL.createObjectURL(result.blob)
        setPhotos((prev) => [...prev, { id: crypto.randomUUID(), blob: result.blob, filename: photoFilename(file.name), previewUrl }])
      } else {
        lastError = result.error
      }
    }
    if (lastError) setSaveError(lastError)
  }

  const makeCover = (photoId: string) => {
    setPhotos((prev) => {
      const idx = prev.findIndex((p) => p.id === photoId)
      if (idx <= 0) return prev
      const next = [...prev]
      const [moved] = next.splice(idx, 1)
      next.unshift(moved)
      return next
    })
  }

  const removePhoto = (photoId: string) => {
    setPhotos((prev) => {
      const target = prev.find((p) => p.id === photoId)
      if (target) URL.revokeObjectURL(target.previewUrl)
      return prev.filter((p) => p.id !== photoId)
    })
  }

  const handleSave = async () => {
    const trimmedName = name.trim()
    if (trimmedName === '') return
    if (savingRef.current) return
    savingRef.current = true
    setSaving(true)
    setSaveError(null)
    try {
      // BLANK_DRAFT already carries system's own server default 'other' -
      // "that is the current contract, not a new fallback" (the plan's own
      // words) - so only the fields this form actually collects override it.
      const input: EquipmentInput = {
        ...BLANK_DRAFT,
        name: trimmedName,
        quantity,
        category: 'general',
        status: 'stored',
        zone_id: zoneId,
        bin_id: binId,
      }
      const created = await createEquipment(input)

      const toUpload = photos
      const failures: FailedUpload[] = []
      // Review finding: a 409 ("already in Documents as ...") means the
      // exact same bytes can never be linked as a NEW photo - re-sending
      // them on Retry can only get the identical refusal, so it is never
      // queued in `failures` the way a genuine (transient) failure is.
      const refused: string[] = []
      for (const photo of toUpload) {
        try {
          await uploadEquipmentPhoto(created.id, photo.blob, photo.filename)
          URL.revokeObjectURL(photo.previewUrl)
        } catch (err) {
          URL.revokeObjectURL(photo.previewUrl)
          if (err instanceof PhotoAlreadyLinkedError) {
            refused.push(err.message)
            continue
          }
          failures.push({ blob: photo.blob, filename: photo.filename, error: err instanceof Error ? err.message : String(err) })
        }
      }

      if (failures.length > 0) {
        setSavedItemId(created.id)
        setSavedItemName(trimmedName)
        setFailedUploads(failures)
        const notUploaded = failures.length + refused.length
        setNotice(`Saved ${trimmedName}, but ${notUploaded} photo${notUploaded === 1 ? '' : 's'} didn't upload: ${failures[0].error}`)
      } else if (refused.length > 0) {
        setSavedItemId(null)
        setSavedItemName(null)
        setFailedUploads([])
        setNotice(refused[0])
      } else {
        setSavedItemId(null)
        setSavedItemName(null)
        setFailedUploads([])
        setNotice(null)
      }

      // The form clears and the camera is ready for the next item -
      // "there is no silent rollback" (the plan's own words) means a
      // partial photo failure still leaves the item saved, not the form
      // reopened on it for correction.
      setName('')
      setQuantity(1)
      setPhotos([])
      onCreated()
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err))
    } finally {
      savingRef.current = false
      setSaving(false)
    }
  }

  const handleRetry = async () => {
    if (savedItemId === null || failedUploads.length === 0) return
    setRetrying(true)
    const stillFailing: FailedUpload[] = []
    const refused: string[] = []
    for (const photo of failedUploads) {
      try {
        await uploadEquipmentPhoto(savedItemId, photo.blob, photo.filename)
      } catch (err) {
        if (err instanceof PhotoAlreadyLinkedError) {
          refused.push(err.message)
          continue
        }
        stillFailing.push({ ...photo, error: err instanceof Error ? err.message : String(err) })
      }
    }
    setFailedUploads(stillFailing)
    if (stillFailing.length > 0) {
      setNotice(`Saved, but ${stillFailing.length} photo${stillFailing.length === 1 ? '' : 's'} didn't upload: ${stillFailing[0].error}`)
    } else {
      // Nothing left queued - savedItemName's only reader (the `detail`
      // memo above) is gated on failedUploads.length, so this isn't load-
      // bearing for the guard, but leaving a stale name behind here is its
      // own kind of confusing state to carry.
      setSavedItemName(null)
      if (refused.length > 0) {
        setNotice(refused[0])
      } else {
        setNotice(null)
      }
    }
    setRetrying(false)
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
        onRemove={removePhoto}
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
      {notice && (
        <div role="alert" className="flex items-center justify-between gap-2 rounded-md border border-border bg-muted px-3 py-2 text-sm">
          <span>{notice}</span>
          {/* A 409 refusal never populates failedUploads (it's dropped, not
              queued) - Retry has nothing to re-send for a notice that's
              only a 409, see handleSave/handleRetry's own comments. */}
          {failedUploads.length > 0 && (
            <Button type="button" variant="outline" size="sm" disabled={retrying} onClick={() => { void handleRetry() }}>
              {retrying ? 'Retrying...' : 'Retry'}
            </Button>
          )}
        </div>
      )}
    </div>
  )
}
