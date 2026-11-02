// The signs' photos, fetched once as bitmaps before the render starts, so no
// frame waits on a picture.

import { apiBaseUrl } from '@/config/api'
import { cancelled } from '@/lib/passage-video/errors'
import type { ReplaySign } from '@/lib/replay-signs'
import { UI_SCALE } from '@/lib/passage-video/overlay'
import { SIGN_CARD_PAD_PX, SIGN_CARD_PHOTO_W_PX } from '@/lib/replay-signs'
import { MOMENT_KEN_BURNS, momentGrowth } from '@/lib/replay-time'

/** The widest a photo is ever drawn in a video of this size: in its card, made
 * as big as a photo moment makes it, and zoomed to the end of its slow zoom.
 * A bitmap wider than this is memory for nothing. The card is the shortest a
 * card with a photo can be (no note), which grows the most. */
export function photoDrawWidth(size: { width: number; height: number }): number {
  const card = { w: SIGN_CARD_PHOTO_W_PX, h: 2 * SIGN_CARD_PAD_PX + (SIGN_CARD_PHOTO_W_PX - 2 * SIGN_CARD_PAD_PX) * 0.75 + SIGN_CARD_PAD_PX + 20 }
  const k = momentGrowth(card, { w: size.width / UI_SCALE, h: size.height / UI_SCALE })
  return Math.ceil((SIGN_CARD_PHOTO_W_PX - 2 * SIGN_CARD_PAD_PX) * k * UI_SCALE * MOMENT_KEN_BURNS)
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
