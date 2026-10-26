import { captureUnix, type ExifCapture } from '@/lib/exif-time'
import type { Timeline } from '@/lib/passages'
import { placeAt } from '@/lib/replay-signs'

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
const two = (n: number) => String(n).padStart(2, '0')

export interface SignPlacement {
  /** Unix seconds to pin the sign at. */
  at: number
  placedByPhoto: boolean
  /** Plain words for the editor: what time was used and why. */
  message: string
}

/** The answer for a sign that is already placed: a time only when the photo gave one. */
export type PhotoMove = { at: number; placedByPhoto: true; message: string } | { at: null; placedByPhoto: false; message: string }

/** What the photo says about when it was taken, in the boat's terms. */
type Reading = { kind: 'time'; at: number; message: string } | { kind: 'none'; why: string }

function readCapture(capture: ExifCapture | Error | null, boatOffsetHours: number, timeline: Timeline): Reading {
  if (capture instanceof Error) return { kind: 'none', why: `Could not read when the photo was taken (${capture.message})` }
  if (capture === null) return { kind: 'none', why: 'The photo has no capture time' }
  const { unix, zone, fromPhoto } = captureUnix(capture, boatOffsetHours)
  const shown = `${capture.day} ${MONTHS[capture.month - 1]} ${two(capture.hour)}:${two(capture.minute)} ${zone}`
  if (!placeAt(unix, timeline)) return { kind: 'none', why: `The photo was taken ${shown}, outside this replay` }
  return {
    kind: 'time',
    at: unix,
    message: `Placed at the time the photo was taken: ${shown}${fromPhoto ? '' : ' (the photo has no time zone, so boat time is used)'}.`,
  }
}

/**
 * Where a photo's sign goes: at the moment the photo was taken if that falls
 * inside a replayed passage, otherwise at the replay cursor. `capture` is what
 * the photo said (null when it carries no time), or the error from reading it.
 * The photo's GPS is not consulted: the track is the reliable source.
 */
export function placeByCapture(capture: ExifCapture | Error | null, boatOffsetHours: number, timeline: Timeline, cursorAt: number): SignPlacement {
  const r = readCapture(capture, boatOffsetHours, timeline)
  if (r.kind === 'time') return { at: r.at, placedByPhoto: true, message: r.message }
  return { at: cursorAt, placedByPhoto: false, message: `${r.why}, so the sign is at the current position.` }
}

/** The same reading for a sign already placed: when the photo cannot place it,
 * the answer carries no time and the sign is left where it is. */
export function moveByCapture(capture: ExifCapture | Error | null, boatOffsetHours: number, timeline: Timeline): PhotoMove {
  const r = readCapture(capture, boatOffsetHours, timeline)
  if (r.kind === 'time') return { at: r.at, placedByPhoto: true, message: r.message }
  return { at: null, placedByPhoto: false, message: `${r.why}, so the sign was not moved.` }
}
