// Why a video stopped. Each is said to the operator in the modal as it is,
// and none is covered by a lesser video (AGENTS.md: fail fast). A video with
// a hole in its chart is worse than none, so the render stops and says which
// part failed.

import { SEAMARK_SOURCE_ID } from '@/lib/basemap'
import { TERRAIN_SOURCE_ID } from '@/lib/terrain'
import { IMAGERY_SOURCE_ID } from '@/lib/world-imagery'

export type PassageVideoErrorKind =
  /** The browser cannot encode H.264 at this size. */
  | 'unsupported'
  /** A tile, terrain or imagery request failed, or the map could not be made. */
  | 'source'
  /** A frame never finished drawing. */
  | 'timeout'
  /** The encoder stopped. */
  | 'encode'
  /** The operator cancelled, or left the page. Not a fault. */
  | 'cancelled'

export class PassageVideoError extends Error {
  readonly kind: PassageVideoErrorKind

  constructor(kind: PassageVideoErrorKind, message: string) {
    super(message)
    this.name = 'PassageVideoError'
    this.kind = kind
  }
}

export const UNSUPPORTED_MESSAGE = "This browser can't make H.264 video."

/** The operator cancelled, or left the page. */
export const cancelled = () => new PassageVideoError('cancelled', 'The video was cancelled.')

/** How long a frame may take to finish drawing before the render gives up. */
export const FRAME_TIMEOUT_MS = 20_000

/** The one kind of map error that is not a fault: the sea marks have no tile
 * where there are no marks to draw. */
export function ignoredMapError(sourceId: string | undefined, status: number | undefined): boolean {
  return sourceId === SEAMARK_SOURCE_ID && status === 404
}

/** Which part of the map failed to arrive, in the operator's words. */
export function sourceFailureMessage(sourceId: string | undefined, detail: string): string {
  const stop = "The video wasn't finished."
  switch (sourceId) {
    case IMAGERY_SOURCE_ID:
      return `The satellite imagery didn't load, so the boat may be offline (${detail}). ${stop}`
    case TERRAIN_SOURCE_ID:
      return `The 3D terrain didn't load, so the boat may be offline (${detail}). ${stop}`
    case SEAMARK_SOURCE_ID:
      return `The sea marks didn't load. They come from the internet, so the boat may be offline (${detail}). ${stop}`
    case 'protomaps':
      return `The chart didn't load fully, so the boat may be offline (${detail}). ${stop}`
    default:
      return `The map hit a problem (${detail}). ${stop}`
  }
}

/** A frame that never finished drawing. */
export function timeoutMessage(frame: number, total: number): string {
  return `The map didn't finish drawing within ${FRAME_TIMEOUT_MS / 1000} seconds, at frame ${frame} of ${total}. The boat may be offline, or this tab may not be in front: keep it in front while the video is made. The video wasn't finished.`
}
