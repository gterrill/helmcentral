import { captureUnix, type ExifCapture } from '@/lib/exif-time'
import type { Timeline } from '@/lib/passages'

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
const two = (n: number) => String(n).padStart(2, '0')

export interface SignPlacement {
  /** Unix seconds to pin the sign at. */
  at: number
  placedByPhoto: boolean
  /** Plain words for the editor: what time was used and why. */
  message: string
}

/**
 * Where a photo's sign goes: at the moment the photo was taken if that falls
 * inside a replayed passage, otherwise at the replay cursor. `capture` is what
 * the photo said (null when it carries no time), or the error from reading it.
 * The photo's GPS is not consulted: the track is the reliable source.
 */
export function placeByCapture(capture: ExifCapture | Error | null, boatOffsetHours: number, timeline: Timeline, cursorAt: number): SignPlacement {
  const atCursor = (why: string): SignPlacement => ({ at: cursorAt, placedByPhoto: false, message: `${why}, so the sign is at the current position.` })
  if (capture instanceof Error) return atCursor(`Could not read when the photo was taken (${capture.message})`)
  if (capture === null) return atCursor('The photo has no capture time')
  const { unix, zone, fromPhoto } = captureUnix(capture, boatOffsetHours)
  const shown = `${capture.day} ${MONTHS[capture.month - 1]} ${two(capture.hour)}:${two(capture.minute)} ${zone}`
  if (!timeline.entries.some((e) => unix >= e.start && unix <= e.end)) return atCursor(`The photo was taken ${shown}, outside this replay`)
  return {
    at: unix,
    placedByPhoto: true,
    message: `Placed at the time the photo was taken: ${shown}${fromPhoto ? '' : ' (the photo has no time zone, so boat time is used)'}.`,
  }
}
