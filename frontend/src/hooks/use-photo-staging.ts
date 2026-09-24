import { useCallback, useEffect, useRef, useState } from 'react'

import { uploadEquipmentPhoto, type EquipmentItem } from '@/hooks/use-inventory'
import { downscaleAll } from '@/lib/image-downscale'

// ADR 0127: the "pick photos before the record exists yet" staging area -
// shared by bin-quick-add.tsx's own quick-add draft and equipment-editor.tsx's
// "New item" draft, which used to carry near-identical copies of this whole
// module: the LocalPhoto/FailedPhotoUpload shapes, photoFilename, the
// add/make-cover/remove operations, and the "upload in order, collect
// failures" loop. Pulled out once both copies needed the same fixes twice.

/** ADR 0127: a picked-but-not-yet-uploaded photo on a brand new draft -
 * downscaled already (lib/image-downscale.ts runs the moment a file is
 * picked, never deferred to Save), previewed through an object URL that
 * MUST be revoked once it's no longer needed (a successful upload, a
 * Remove, or the owning component unmounting with the draft abandoned).
 * previewUrl is '' for a photo synthesized purely to pass through
 * uploadPhotosInOrder without ever being shown in a strip (see
 * equipment-editor.tsx's uploadPhotosToSavedItem) - uploadPhotosInOrder
 * skips revoking an empty one. */
export interface LocalPhoto {
  id: string
  blob: Blob
  filename: string
  previewUrl: string
}

/** A photo whose upload failed - holds the Blob itself (not just an id) so
 * Retry can re-send the exact same bytes without asking the operator to
 * pick the file again. needsDownscale (finding 10, pre-release review)
 * marks a failure that happened at the DOWNSCALE step, before there was
 * ever a Blob to upload - `blob` here is still the original, full-size
 * File in that case, and Retry must downscale it again before it can be
 * uploaded at all; re-sending it as-is would upload the full-size original
 * straight past the size limit downscaling exists to enforce. Omitted (or
 * false) for an ordinary upload failure, whose blob is already downscaled. */
export interface FailedPhotoUpload {
  blob: Blob
  filename: string
  error: string
  needsDownscale?: boolean
}

/** ADR 0127: JPEG/PNG re-encoding always renames to .jpg (downscaleImage's
 * own output format) - a camera capture's filename is often meaningless
 * anyway ("image.heic", "blob"), and giving every upload a real extension
 * that matches its actual bytes is one less thing to get wrong. */
export function photoFilename(original: string): string {
  const base = original.replace(/\.[^.]+$/, '').trim()
  return `${base || 'photo'}.jpg`
}

export interface UsePhotoStagingOptions {
  /** Reported when downscaling a picked file fails - the caller's own
   * saveError/setSaveError slot. */
  onDownscaleError?: (message: string) => void
  /** The live item hook's own setItem (useEquipmentItem), applied after
   * each successful upload so a saved record's photo row reflects it
   * immediately rather than waiting on a second GET. Omitted by a caller
   * with no live item to update (bin-quick-add.tsx's own draft, which
   * clears and moves on rather than keeping one open). */
  setItem?: (item: EquipmentItem, options?: { adopt?: boolean }) => void
}

/** ADR 0127: local-photo staging shared by a brand new draft's photo row -
 * add (downscale + stage), make cover (move to the front), remove (revoke
 * its object URL) - plus uploadPhotosInOrder, the "send each one in order,
 * apply the item it comes back with" sequence every upload site runs once
 * the target record actually exists. */
export function usePhotoStaging(options: UsePhotoStagingOptions = {}) {
  const { onDownscaleError, setItem } = options
  const [localPhotos, setLocalPhotos] = useState<LocalPhoto[]>([])

  // Downscaling runs through downscaleAll's own worker pool (at most 3 at
  // once - a plain Promise.all(files.map(downscaleImage)) would decode
  // every picked file into memory at once, risking the tab running out of
  // memory on a phone). Results are still applied in the ORIGINAL file
  // order, not completion order. A failure keeps its real reason (AGENTS.md
  // fallback policy) rather than being silently dropped.
  const addLocalPhotos = useCallback(async (files: File[]) => {
    const outcomes = await downscaleAll(files)
    let lastError: string | null = null
    for (const { file, result } of outcomes) {
      if (result.ok) {
        const previewUrl = URL.createObjectURL(result.blob)
        setLocalPhotos((prev) => [...prev, { id: crypto.randomUUID(), blob: result.blob, filename: photoFilename(file.name), previewUrl }])
      } else {
        lastError = result.error
      }
    }
    if (lastError) onDownscaleError?.(lastError)
  }, [onDownscaleError])

  const makeCoverLocal = useCallback((photoId: string) => {
    setLocalPhotos((prev) => {
      const idx = prev.findIndex((p) => p.id === photoId)
      if (idx <= 0) return prev
      const next = [...prev]
      const [moved] = next.splice(idx, 1)
      next.unshift(moved)
      return next
    })
  }, [])

  const removeLocalPhoto = useCallback((photoId: string) => {
    setLocalPhotos((prev) => {
      const target = prev.find((p) => p.id === photoId)
      if (target) URL.revokeObjectURL(target.previewUrl)
      return prev.filter((p) => p.id !== photoId)
    })
  }, [])

  // Object URLs are a browser-level resource, not a React one - abandoning
  // a draft (Back, or navigating away) without ever pressing Save must
  // still release them, or every unfinished draft leaks one blob URL per
  // photo picked for the life of the tab. Deliberately an EMPTY dependency
  // array with a ref-mirrored read at cleanup time (not `[localPhotos]`,
  // which would revoke and immediately re-grant new object URLs on every
  // single photo pick) - this only ever runs once, on unmount, over
  // whatever the list holds at that moment.
  const localPhotosRef = useRef<LocalPhoto[]>(localPhotos)
  useEffect(() => { localPhotosRef.current = localPhotos }, [localPhotos])
  useEffect(() => () => {
    for (const photo of localPhotosRef.current) URL.revokeObjectURL(photo.previewUrl)
  }, [])

  // ADR 0127: "create the item; upload the photos one at a time, in order;
  // release the object URLs." A failed upload does NOT stop the remaining
  // photos from being tried - "there is no silent rollback" (the plan's own
  // words) means every photo gets its own attempt regardless of an earlier
  // one failing. 2026-09-25 amendment: a byte-identical upload is no longer
  // refused with a 409 - it links the existing document and succeeds like
  // any other upload, so there is no longer a separate "refused, never
  // worth retrying" outcome to track here.
  const uploadPhotosInOrder = useCallback(async (
    targetId: string,
    photos: LocalPhoto[],
    opts: { adopt?: boolean } = {},
  ): Promise<{ failures: FailedPhotoUpload[] }> => {
    const failures: FailedPhotoUpload[] = []
    for (const photo of photos) {
      try {
        const updated = await uploadEquipmentPhoto(targetId, photo.blob, photo.filename)
        if (photo.previewUrl) URL.revokeObjectURL(photo.previewUrl)
        setItem?.(updated, opts)
      } catch (err) {
        if (photo.previewUrl) URL.revokeObjectURL(photo.previewUrl)
        failures.push({ blob: photo.blob, filename: photo.filename, error: err instanceof Error ? err.message : String(err) })
      }
    }
    return { failures }
  }, [setItem])

  return { localPhotos, setLocalPhotos, addLocalPhotos, makeCoverLocal, removeLocalPhoto, uploadPhotosInOrder }
}
