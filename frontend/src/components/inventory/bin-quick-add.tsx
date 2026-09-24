import { useEffect, useMemo, useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { PhotoStripEditor, type PhotoStripPhoto } from '@/components/inventory/photo-strip-editor'
import { createEquipment, uploadEquipmentPhoto, type EquipmentInput } from '@/hooks/use-inventory'
import { downscaleImage } from '@/lib/image-downscale'

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
   * when this is true. */
  onHasWorkChange?: (hasWork: boolean) => void
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
  const [retrying, setRetrying] = useState(false)
  // Review finding: Enter in the Name field calls handleSave directly, with
  // no in-flight guard - the `saving` state above is too slow to catch a
  // second Enter (or a stray Enter-then-click) pressed before React has
  // re-rendered it into either handler's own closure. A ref updates
  // synchronously, so it is what actually blocks a second call that starts
  // before the first one's first await ever yields.
  const savingRef = useRef(false)

  const hasWork = useMemo(() => name.trim() !== '' || photos.length > 0, [name, photos])
  useEffect(() => { onHasWorkChange?.(hasWork) }, [hasWork, onHasWorkChange])

  // Downscaling every picked file runs concurrently (Promise.all) - see
  // equipment-editor.tsx's own addLocalPhotos for the identical reasoning.
  // Results are still applied in the ORIGINAL file order, not completion
  // order.
  const addFiles = async (files: File[]) => {
    const results = await Promise.all(files.map(async (file) => {
      try {
        return { ok: true as const, downscaled: await downscaleImage(file), filename: photoFilename(file.name) }
      } catch (err) {
        return { ok: false as const, error: err instanceof Error ? err.message : String(err) }
      }
    }))
    let lastError: string | null = null
    for (const result of results) {
      if (result.ok) {
        const previewUrl = URL.createObjectURL(result.downscaled)
        setPhotos((prev) => [...prev, { id: crypto.randomUUID(), blob: result.downscaled, filename: result.filename, previewUrl }])
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
      // system is left out (set to its own server default 'other' here
      // rather than omitted from the request body, which lands on the
      // identical stored value) - "that is the current contract, not a new
      // fallback" (the plan's own words).
      const input: EquipmentInput = {
        name: trimmedName,
        category: 'general',
        system: 'other',
        manufacturer: '',
        model: '',
        serial: '',
        quantity,
        status: 'stored',
        zone_id: zoneId,
        bin_id: binId,
        location_detail: '',
        install_date: '',
        hour_meter_path: '',
        profile_id: '',
        aliases: [],
        verified_aboard: false,
        notes: '',
      }
      const created = await createEquipment(input)

      const toUpload = photos
      const failures: FailedUpload[] = []
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
        setSavedItemId(created.id)
        setFailedUploads(failures)
        setNotice(`Saved ${trimmedName}, but ${failures.length} photo${failures.length === 1 ? '' : 's'} didn't upload: ${failures[0].error}`)
      } else {
        setSavedItemId(null)
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
    for (const photo of failedUploads) {
      try {
        await uploadEquipmentPhoto(savedItemId, photo.blob, photo.filename)
      } catch (err) {
        stillFailing.push({ ...photo, error: err instanceof Error ? err.message : String(err) })
      }
    }
    setFailedUploads(stillFailing)
    setNotice(stillFailing.length === 0
      ? null
      : `Saved, but ${stillFailing.length} photo${stillFailing.length === 1 ? '' : 's'} didn't upload: ${stillFailing[0].error}`)
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
          <Button type="button" variant="outline" size="sm" disabled={retrying} onClick={() => { void handleRetry() }}>
            {retrying ? 'Retrying...' : 'Retry'}
          </Button>
        </div>
      )}
    </div>
  )
}
