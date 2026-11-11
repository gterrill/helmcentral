import { documentIsImage } from '@/lib/document-display'
import { errorMessage } from '@/lib/errors'
import { readExifGps } from '@/lib/exif-gps'
import { captureUnix, readExifCapture, type ExifCapture } from '@/lib/exif-time'
import { photoFilename } from '@/lib/image-downscale'
import type { InboxUploadMeta, InboxUploadResult } from '@/lib/inbox-api'
import { processInboxImage, type InboxImage } from '@/lib/inbox-image'
import { sha256HexOfBlob } from '@/lib/sha256'
import { zoneKnown, zonedWallInstant } from '@/lib/vessel-time'

// Getting files ready to share to the Inbox, on the phone, and sending a batch
// one file at a time. The order matters. Each photo's original is hashed first
// and the box is asked which hashes it already has, so a photo it has is never
// shrunk or sent. For the rest, the EXIF block (capture time, the phone's
// position and facing) is read from the original, because the re-encode that
// shrinks the photo drops it.

const HEAD_BYTES = 256 * 1024
/** The most hashes the box answers for in one call. */
const KNOWN_BATCH = 500
/** The badge count is refreshed after this many stored files, so a long batch shows progress. */
const CHECKPOINT_EVERY = 10

export interface PreparedFile {
  blob: Blob
  /** A small preview of a photo; null for a PDF. */
  thumb: Blob | null
  filename: string
  meta: InboxUploadMeta
  /** Things the operator should know about this file that did not stop it going. */
  warnings: string[]
}

export interface PrepareDeps {
  /** The vessel's IANA zone, for a photo that carries no zone of its own. */
  boatTimeZone?: string
  /** Shrinks, previews and scores a photo. A seam because jsdom has no canvas. */
  process?: (file: Blob) => Promise<InboxImage>
}

const isPdf = (f: File) => f.type === 'application/pdf' || /\.pdf$/i.test(f.name)
const isImage = (f: File) => documentIsImage({ mime: f.type.startsWith('image/') ? f.type : undefined, filename: f.name })

const NO_META: InboxUploadMeta = { captureMs: null, bearingDeg: null, phoneLat: null, phoneLon: null, fingerprint: '', sharpness: 0, sourceSha256: '' }

/**
 * The hash of a photo's original bytes, which the box knows it by however the
 * photo was shrunk. Empty for a file that is not hashed (a PDF goes as it is,
 * so the box recognises its bytes directly). The pure-TypeScript hash works on
 * a plain http page, where Web Crypto is missing.
 */
export async function hashFile(file: File): Promise<string> {
  return isImage(file) ? sha256HexOfBlob(file) : ''
}

/**
 * When a photo was taken, in Unix ms. The photo's own offset wins; without one
 * the camera's wall clock is read in the boat's zone; with neither the time is
 * left out, with a warning to say so.
 */
export function resolveCaptureMs(capture: ExifCapture | null, boatZone: string | undefined): { ms: number | null; warning?: string } {
  if (!capture) return { ms: null }
  if (capture.offsetMinutes !== null) return { ms: captureUnix(capture, 0).unix * 1000 }
  if (!boatZone || !zoneKnown(boatZone)) {
    return { ms: null, warning: "The photo has no time zone and the boat's is not known, so its capture time was not used" }
  }
  const wall = Date.UTC(capture.year, capture.month - 1, capture.day, capture.hour, capture.minute, capture.second)
  return { ms: zonedWallInstant(wall, boatZone).getTime() }
}

/**
 * A photo read, shrunk and scored; a PDF as it is. Throws on a file that is
 * neither, and when the browser cannot decode or draw a photo, so that file is
 * reported rather than uploaded half-prepared. A capture time that cannot be
 * read or placed does not stop the photo: it goes with no time and a warning.
 * `sourceSha256` is hashFile's answer for this file.
 */
export async function prepareFile(file: File, sourceSha256: string, deps: PrepareDeps = {}): Promise<PreparedFile> {
  if (isPdf(file)) return { blob: file, thumb: null, filename: file.name, meta: { ...NO_META }, warnings: [] }
  if (!isImage(file)) throw new Error(`${file.name} is not a photo or a PDF`)

  const warnings: string[] = []
  const head = await file.slice(0, HEAD_BYTES).arrayBuffer()

  let captureMs: number | null = null
  try {
    const resolved = resolveCaptureMs(readExifCapture(head), deps.boatTimeZone)
    captureMs = resolved.ms
    if (resolved.warning) warnings.push(resolved.warning)
  } catch (err) {
    warnings.push(`Could not read when the photo was taken: ${errorMessage(err)}`)
  }

  let gps = { lat: null as number | null, lon: null as number | null, bearingDeg: null as number | null }
  try {
    gps = readExifGps(head)
  } catch (err) {
    warnings.push(`Could not read the photo's position: ${errorMessage(err)}`)
  }

  const { blob, thumb, fingerprint, sharpness } = await (deps.process ?? processInboxImage)(file)

  return {
    blob,
    thumb,
    filename: photoFilename(file.name),
    meta: { captureMs, bearingDeg: gps.bearingDeg, phoneLat: gps.lat, phoneLon: gps.lon, fingerprint, sharpness, sourceSha256 },
    warnings,
  }
}

export interface UploadTally {
  /** `checking` while the files are hashed and the box is asked, then `sending`. */
  stage: 'checking' | 'sending'
  total: number
  /** Files hashed so far, in the checking stage. */
  checked: number
  /** Files finished with, whatever the outcome. */
  done: number
  uploaded: number
  /** Files whose bytes were already in the Inbox or Documents. */
  skipped: number
  failed: { name: string; error: string }[]
  /** Stored files that carried no capture time. */
  noTime: number
  warnings: { name: string; warning: string }[]
}

/** `already_held`: the box already had the bytes, in the Inbox, in Documents or filed. */
export type FileOutcome = { kind: 'stored' } | { kind: 'already_held' } | { kind: 'failed'; error: string }

export interface UploadDeps {
  hash: (file: File) => Promise<string>
  /** Which of these hashes the box already has. */
  known: (hashes: string[]) => Promise<string[]>
  prepare: (file: File, sourceSha256: string) => Promise<PreparedFile>
  upload: (prepared: PreparedFile) => Promise<InboxUploadResult>
  onProgress?: (tally: UploadTally) => void
  /**
   * Called once for each file as it ends, after the tally counts it, and awaited:
   * a waiting list removes the files the box answered for. A file never started
   * (the batch was stopped) is not reported. If this throws, the batch stops with
   * that error, because a list that cannot be kept true must not go on.
   */
  onFileDone?: (file: File, outcome: FileOutcome) => void | Promise<void>
  /** Called after every tenth stored file. */
  onCheckpoint?: () => void
  /** Stops starting new files; the one in flight finishes. */
  signal?: AbortSignal
}

/**
 * Shares the files: hash them all, ask the box once which it has, then prepare
 * and send the rest one at a time, in order, so a poor connection is working
 * on a single photo and the count is true. A file that fails is recorded and
 * the rest go on. If the box cannot be asked, the batch fails: sending
 * everything blind would defeat the check.
 */
export async function uploadFiles(files: readonly File[], deps: UploadDeps): Promise<UploadTally> {
  const tally: UploadTally = { stage: 'checking', total: files.length, checked: 0, done: 0, uploaded: 0, skipped: 0, failed: [], noTime: 0, warnings: [] }
  const report = () => deps.onProgress?.({ ...tally, failed: [...tally.failed], warnings: [...tally.warnings] })
  report()

  const hashes: string[] = []
  const unreadable = new Set<number>()
  for (const [i, file] of files.entries()) {
    if (deps.signal?.aborted) return tally
    try {
      hashes[i] = await deps.hash(file)
    } catch (err) {
      hashes[i] = ''
      unreadable.add(i)
      const error = errorMessage(err)
      tally.failed.push({ name: file.name, error })
      tally.done += 1
      await deps.onFileDone?.(file, { kind: 'failed', error })
    }
    tally.checked += 1
    report()
  }

  const known = new Set<string>()
  const toAsk = [...new Set(hashes.filter((h) => h !== ''))]
  for (let at = 0; at < toAsk.length; at += KNOWN_BATCH) {
    if (deps.signal?.aborted) return tally
    for (const h of await deps.known(toAsk.slice(at, at + KNOWN_BATCH))) known.add(h)
  }

  tally.stage = 'sending'
  report()
  for (const [i, file] of files.entries()) {
    if (deps.signal?.aborted) break
    if (unreadable.has(i)) continue
    if (hashes[i] !== '' && known.has(hashes[i])) {
      tally.skipped += 1
      tally.done += 1
      await deps.onFileDone?.(file, { kind: 'already_held' })
      report()
      continue
    }
    let outcome: FileOutcome
    try {
      const prepared = await deps.prepare(file, hashes[i])
      for (const warning of prepared.warnings) tally.warnings.push({ name: file.name, warning })
      const result = await deps.upload(prepared)
      if (result.status === 'stored') {
        outcome = { kind: 'stored' }
        tally.uploaded += 1
        if (prepared.meta.captureMs === null) tally.noTime += 1
        if (tally.uploaded % CHECKPOINT_EVERY === 0) deps.onCheckpoint?.()
      } else {
        outcome = { kind: 'already_held' }
        tally.skipped += 1
      }
    } catch (err) {
      const error = errorMessage(err)
      outcome = { kind: 'failed', error }
      tally.failed.push({ name: file.name, error })
    }
    tally.done += 1
    await deps.onFileDone?.(file, outcome)
    report()
  }
  return tally
}

/** "12 uploaded", "12 uploaded, 3 already in Helmcentral" and, if any failed, "; 2 failed". */
export function summariseTally(t: Pick<UploadTally, 'uploaded' | 'skipped' | 'failed'>): string {
  const base = t.skipped > 0 ? `${t.uploaded} uploaded, ${t.skipped} already in Helmcentral` : `${t.uploaded} uploaded`
  return t.failed.length > 0 ? `${base}; ${t.failed.length} failed` : base
}
