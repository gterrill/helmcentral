// The signs' photos, fetched once as bitmaps before the render starts, so no
// frame waits on a picture.

import { apiBaseUrl } from '@/config/api'
import { cancelled } from '@/lib/passage-video/errors'
import type { ReplaySign } from '@/lib/replay-signs'
import { MOMENT_FRAME_FRACTION, MOMENT_KEN_BURNS, PHOTO_ASPECT } from '@/lib/replay-time'

/** The widest a photo is ever drawn in a video of this size: its moment at full size, zoomed to the end of its slow zoom. A bitmap wider than this is memory for nothing. */
export function photoDrawWidth(size: { width: number; height: number }): number {
  return Math.ceil(Math.min(size.width * MOMENT_FRAME_FRACTION, size.height * MOMENT_FRAME_FRACTION * PHOTO_ASPECT) * MOMENT_KEN_BURNS)
}

export interface PhotoSet {
  bitmaps: Map<string, ImageBitmap>
  /** The signs whose photo would not load. Their cards say so and they get no moment. */
  failed: Set<string>
  /** Lets go of the bitmaps. */
  close(): void
}

export interface PhotoDeps {
  fetchPhoto: (url: string, signal?: AbortSignal) => Promise<Blob>
  /** Decodes the photo no wider than `width`. */
  makeBitmap: (blob: Blob, width: number) => Promise<ImageBitmap>
}

const browserDeps: PhotoDeps = {
  async fetchPhoto(url, signal) {
    const res = await fetch(url, { signal })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.blob()
  },
  // Given only the width, the height follows the photo's own shape.
  makeBitmap: (blob, width) => createImageBitmap(blob, { resizeWidth: width, resizeQuality: 'high' }),
}

/** Fetches and decodes the signs' photos, each no larger than the video can draw it (`width`, see photoDrawWidth). */
export async function loadPhotos(signs: readonly ReplaySign[], width: number, signal?: AbortSignal, deps: PhotoDeps = browserDeps): Promise<PhotoSet> {
  const set: PhotoSet = {
    bitmaps: new Map(),
    failed: new Set(),
    close() {
      for (const b of set.bitmaps.values()) b.close()
      set.bitmaps.clear()
    },
  }
  await Promise.all(signs.filter((s) => s.photo_url != null).map(async (s) => {
    try {
      set.bitmaps.set(s.id, await deps.makeBitmap(await deps.fetchPhoto(`${apiBaseUrl}${s.photo_url}`, signal), width))
    } catch (err) {
      if (signal?.aborted) return
      set.failed.add(s.id)
      console.warn(`Sign "${s.title}" (${s.id}): its photo did not load for the video (${err instanceof Error ? err.message : String(err)})`)
    }
  }))
  if (signal?.aborted) {
    set.close()
    throw cancelled()
  }
  return set
}
