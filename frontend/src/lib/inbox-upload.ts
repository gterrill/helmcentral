import { errorMessage } from '@/lib/errors'
import { readExifGps } from '@/lib/exif-gps'
import { captureUnix, readExifCapture } from '@/lib/exif-time'
import { downscaleImage, photoFilename } from '@/lib/image-downscale'
import { sha256HexOfBlob } from '@/lib/sha256'
import { fingerprintImage, type ImageFingerprint } from '@/lib/image-fingerprint'
import type { InboxUploadMeta, InboxUploadResult } from '@/lib/inbox-api'
import { zoneKnown, zoneOffsetMinutes } from '@/lib/vessel-time'

// Getting a file ready to share to the Inbox, on the phone, and sending a
// batch one file at a time. The order matters: the EXIF block (capture time,
// the phone's position and facing) is read from the original, because the
// re-encode that shrinks the photo drops it.

/** Inbox photos keep more detail than sign photos do: they may become a document. */
export const INBOX_MAX_DIMENSION = 2560
const HEAD_BYTES = 256 * 1024

export interface PreparedFile {
  blob: Blob
  filename: string
  meta: InboxUploadMeta
  /** Things the operator should know about this file that did not stop it going. */
  warnings: string[]
}

export interface PrepareDeps {
  /** The vessel's IANA zone, for a photo that carries no zone of its own. */
  boatTimeZone?: string
  downscale?: (file: Blob, maxDimension: number) => Promise<Blob>
  fingerprint?: (file: Blob) => Promise<ImageFingerprint>
  /** Hex SHA-256 of a file. Defaults to the pure-TypeScript hash, which works on a plain http page. */
  sha256?: (file: Blob) => Promise<string>
}

/**
 * Hours east of UTC in `zone` for a camera's wall-clock time. The offset is
 * asked of the instant the first guess gives, then of the instant that offset
 * gives, so a time either side of a daylight saving change lands on the right side.
 */
function zoneOffsetHoursForWall(zone: string, wallMs: number): number {
  const first = zoneOffsetMinutes(zone, wallMs)
  return zoneOffsetMinutes(zone, wallMs - first * 60_000) / 60
}

const isPdf = (f: File) => f.type === 'application/pdf' || /\.pdf$/i.test(f.name)
const isImage = (f: File) => f.type.startsWith('image/') || /\.(jpe?g|png|gif|webp|heic|heif)$/i.test(f.name)

const NO_META: InboxUploadMeta = { captureMs: null, captureOffsetMin: null, bearingDeg: null, phoneLat: null, phoneLon: null, fingerprint: '', sharpness: 0, sourceSha256: '' }

/**
 * A photo read, shrunk and scored; a PDF as it is. Throws on a file that is
 * neither, and when the browser cannot decode or draw a photo, so that file is
 * reported rather than uploaded half-prepared. A capture time that cannot be
 * read or placed does not stop the photo: it goes with no time and a warning.
 */
export async function prepareFile(file: File, deps: PrepareDeps = {}): Promise<PreparedFile> {
  if (isPdf(file)) return { blob: file, filename: file.name, meta: { ...NO_META }, warnings: [] }
  if (!isImage(file)) throw new Error(`${file.name} is not a photo or a PDF`)

  const warnings: string[] = []
  const head = await file.slice(0, HEAD_BYTES).arrayBuffer()

  let captureMs: number | null = null
  let captureOffsetMin: number | null = null
  try {
    const cap = readExifCapture(head)
    if (cap) {
      captureOffsetMin = cap.offsetMinutes
      if (cap.offsetMinutes !== null) {
        captureMs = captureUnix(cap, 0).unix * 1000
      } else if (deps.boatTimeZone && zoneKnown(deps.boatTimeZone)) {
        const wall = Date.UTC(cap.year, cap.month - 1, cap.day, cap.hour, cap.minute, cap.second)
        captureMs = captureUnix(cap, zoneOffsetHoursForWall(deps.boatTimeZone, wall)).unix * 1000
      } else {
        warnings.push("The photo has no time zone and the boat's is not known, so its capture time was not used")
      }
    }
  } catch (err) {
    warnings.push(`Could not read when the photo was taken: ${errorMessage(err)}`)
  }

  let gps = { lat: null as number | null, lon: null as number | null, bearingDeg: null as number | null }
  try {
    gps = readExifGps(head)
  } catch (err) {
    warnings.push(`Could not read the photo's position: ${errorMessage(err)}`)
  }

  const downscale = deps.downscale ?? ((blob: Blob, max: number) => downscaleImage(blob, max, { reduceDecode: true }))
  const fingerprint = deps.fingerprint ?? fingerprintImage
  // The hash is of the original, so the same photo is recognised however it
  // is shrunk.
  const sourceSha256 = await (deps.sha256 ?? sha256HexOfBlob)(file)
  const blob = await downscale(file, INBOX_MAX_DIMENSION)
  const { fingerprint: hash, sharpness } = await fingerprint(blob)

  return {
    blob,
    filename: photoFilename(file.name),
    meta: {
      captureMs,
      captureOffsetMin,
      bearingDeg: gps.bearingDeg,
      phoneLat: gps.lat,
      phoneLon: gps.lon,
      fingerprint: hash,
      sharpness,
      sourceSha256,
    },
    warnings,
  }
}

export interface UploadTally {
  total: number
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

export interface UploadDeps {
  prepare: (file: File) => Promise<PreparedFile>
  upload: (prepared: PreparedFile) => Promise<InboxUploadResult>
  onProgress?: (tally: UploadTally) => void
  /** Stops starting new files; the one in flight finishes. */
  signal?: AbortSignal
}

/**
 * Sends the files one at a time, in order, so a poor connection is working on
 * a single photo and the count is true. A file that fails is recorded and the
 * rest go on.
 */
export async function uploadFiles(files: readonly File[], deps: UploadDeps): Promise<UploadTally> {
  const tally: UploadTally = { total: files.length, done: 0, uploaded: 0, skipped: 0, failed: [], noTime: 0, warnings: [] }
  const report = () => deps.onProgress?.({ ...tally, failed: [...tally.failed], warnings: [...tally.warnings] })
  report()
  for (const file of files) {
    if (deps.signal?.aborted) break
    try {
      const prepared = await deps.prepare(file)
      for (const warning of prepared.warnings) tally.warnings.push({ name: file.name, warning })
      const result = await deps.upload(prepared)
      if (result.status === 'stored') {
        tally.uploaded += 1
        if (prepared.meta.captureMs === null) tally.noTime += 1
      } else {
        tally.skipped += 1
      }
    } catch (err) {
      tally.failed.push({ name: file.name, error: errorMessage(err) })
    }
    tally.done += 1
    report()
  }
  return tally
}

/** "12 uploaded", "12 uploaded, 3 already in Helmcentral" and, if any failed, "; 2 failed". */
export function summariseTally(t: Pick<UploadTally, 'uploaded' | 'skipped' | 'failed'>): string {
  const base = t.skipped > 0 ? `${t.uploaded} uploaded, ${t.skipped} already in Helmcentral` : `${t.uploaded} uploaded`
  return t.failed.length > 0 ? `${base}; ${t.failed.length} failed` : base
}
