import type { ReplaySign, SignVisibility } from '@/lib/replay-signs'

// Signs in the blog card (ADR 0205). The card carries each sign's time, title,
// note and visibility, and a photo as a small JPEG data URI the export itself
// made. The pure parts live here so the card's player can share them: nothing
// in this file touches the network, the DOM or React.

/** The only prefix an image source in the card may start with. */
export const EXPORT_PHOTO_PREFIX = 'data:image/jpeg;base64,'

/** The card, player and data together, must stay under this many bytes. */
export const CARD_LIMIT_BYTES = 3 * 1024 * 1024

/** Photos are shrunk so their longest side is this many pixels, never enlarged. */
export const PHOTO_EDGE_PX = 960
export const PHOTO_QUALITY = 0.82

/** A sign as the card's player reads it. Short keys keep the snippet small;
 * the sign's id and its document are never sent. */
export interface ExportSign {
  /** Real time, Unix milliseconds. */
  t: number
  ti: string
  no: string
  v: SignVisibility
  /** Seconds a passing sign stays up. */
  s: number
  /** The photo, a JPEG data URI; absent for a note-only sign. */
  ph?: string
}

/** True for a JPEG data URI of the kind the export writes, and nothing else. */
export function isExportPhoto(value: unknown): value is string {
  return typeof value === 'string' && value.startsWith(EXPORT_PHOTO_PREFIX) && /^[A-Za-z0-9+/]+={0,2}$/.test(value.slice(EXPORT_PHOTO_PREFIX.length))
}

/** The signs whose time falls inside an exported passage, oldest first. A sign
 * between passages or outside the selection would never be shown. */
export function signsInRange(signs: readonly ReplaySign[], passages: readonly { a: number; b: number }[]): ReplaySign[] {
  return signs
    .filter((s) => passages.some((p) => s.t_ms / 1000 >= p.a && s.t_ms / 1000 <= p.b))
    .sort((a, b) => a.t_ms - b.t_ms || a.id.localeCompare(b.id))
}

/** What goes into the card for each sign in range. `photos` maps a sign id to
 * its prepared data URI. A sign with a photo that was not prepared is an error:
 * it is never sent as a note without a word. A sign whose photo document has
 * been deleted has no photo link and goes out as a note, as the app shows it. */
export function exportSigns(signs: readonly ReplaySign[], passages: readonly { a: number; b: number }[], photos?: ReadonlyMap<string, string>): ExportSign[] {
  return signsInRange(signs, passages).map((s) => {
    const out: ExportSign = { t: Math.round(s.t_ms), ti: s.title, no: s.note, v: s.visibility, s: s.show_s }
    if (s.photo_url) {
      const uri = photos?.get(s.id)
      if (uri === undefined) throw new Error(`The photo for the sign "${s.title}" was not prepared`)
      if (!isExportPhoto(uri)) throw new Error(`The photo for the sign "${s.title}" is not a JPEG the export made`)
      out.ph = uri
    }
    return out
  })
}

/** The size a photo is shrunk to: the long side at most `edge`, the shape kept. */
export function fitWithin(width: number, height: number, edge: number): { width: number; height: number } {
  const scale = Math.min(1, edge / Math.max(width, height))
  return { width: Math.max(1, Math.round(width * scale)), height: Math.max(1, Math.round(height * scale)) }
}

const MARKER_NAMES: Record<number, string> = { 0xfe: 'COM' }
for (let n = 1; n <= 15; n++) MARKER_NAMES[0xe0 + n] = `APP${n}`

const text = (b: Uint8Array, at: number, s: string) => s.split('').every((c, k) => b[at + k] === c.charCodeAt(0))

/** Header segments the cleaned file keeps: the quantisation, frame, Huffman
 * and restart tables a decoder needs, a JFIF header and an ICC profile. */
function keeps(marker: number, b: Uint8Array, payload: number): boolean {
  if (marker === 0xdb || marker === 0xdd) return true
  if (marker >= 0xc0 && marker <= 0xcf && marker !== 0xc8) return true
  if (marker === 0xe0) return text(b, payload, 'JFIF\0')
  if (marker === 0xe2) return text(b, payload, 'ICC_PROFILE\0')
  return false
}

/** Walks a JPEG's header segments: [marker, start, end] for each, up to and
 * including SOS. Throws when the file is not a JPEG or ends before the picture
 * data starts, so a truncated file is never taken for a clean one. */
function segments(bytes: Uint8Array): { marker: number; start: number; end: number }[] {
  if (bytes.length < 4 || bytes[0] !== 0xff || bytes[1] !== 0xd8) throw new Error('These bytes are not a JPEG')
  const out: { marker: number; start: number; end: number }[] = []
  let i = 2
  while (i + 3 < bytes.length) {
    if (bytes[i] !== 0xff) throw new Error('These bytes are not a valid JPEG')
    const marker = bytes[i + 1]
    if (marker === 0xff) { i++; continue }
    const end = i + 2 + ((bytes[i + 2] << 8) | bytes[i + 3])
    if (end > bytes.length) throw new Error('This JPEG is cut short')
    out.push({ marker, start: i, end })
    if (marker === 0xda) return out
    if (marker === 0xd9) break
    i = end
  }
  throw new Error('This JPEG is cut short: it ends before the picture data')
}

/** The metadata segments in a JPEG's header that can carry a position or a
 * description: Exif and XMP (APP1), IPTC (APP13), other APPn and comments. A
 * JFIF header (APP0) and an ICC profile (APP2) are not listed. Throws if the
 * picture data is never reached. */
export function jpegMetadataMarkers(bytes: Uint8Array): string[] {
  return segments(bytes)
    .filter((s) => MARKER_NAMES[s.marker] && !keeps(s.marker, bytes, s.start + 4))
    .map((s) => MARKER_NAMES[s.marker])
}

/** The same JPEG with every header segment but the plain ones dropped: Exif and
 * XMP, IPTC, every other APPn and every comment go, so nothing about where or
 * with what the photo was taken is left whatever the browser's encoder wrote.
 * The scan data is copied as it is, up to the end-of-image marker. Throws when
 * the file is cut short, or when anything that should be gone is still there. */
export function cleanJpeg(bytes: Uint8Array): Uint8Array {
  const segs = segments(bytes)
  const sos = segs[segs.length - 1]
  const parts: Uint8Array[] = [bytes.subarray(0, 2)]
  for (const s of segs) {
    if (s.marker === 0xda || keeps(s.marker, bytes, s.start + 4)) parts.push(bytes.subarray(s.start, s.end))
  }
  let eoi = -1
  for (let i = sos.end; i + 1 < bytes.length; i++) {
    if (bytes[i] === 0xff && bytes[i + 1] === 0xd9) { eoi = i + 2; break }
  }
  if (eoi < 0) throw new Error('This JPEG is cut short: it has no end marker')
  parts.push(bytes.subarray(sos.end, eoi))
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0))
  let at = 0
  for (const p of parts) { out.set(p, at); at += p.length }
  const left = jpegMetadataMarkers(out)
  if (left.length > 0) throw new Error(`Picture metadata is still in the file (${left.join(', ')})`)
  return out
}

const MB = 1024 * 1024
export const fmtMB = (bytes: number) => (bytes < MB ? `${Math.max(1, Math.round(bytes / 1024))} KB` : `${(bytes / MB).toFixed(1)} MB`)

/** The plain error for a card over the limit, naming the biggest photos. */
export function oversizeMessage(totalBytes: number, signs: readonly ExportSign[]): string {
  const photos = signs.filter((s) => s.ph).map((s) => ({ title: s.ti, bytes: (s.ph as string).length })).sort((a, b) => b.bytes - a.bytes)
  const largest = photos.slice(0, 3).map((p) => `"${p.title}" ${fmtMB(p.bytes)}`).join(', ')
  const head = `This card is ${fmtMB(totalBytes)}, over the ${CARD_LIMIT_BYTES / MB} MB limit.`
  if (photos.length === 0) return head
  return `${head} Largest photos: ${largest}. Turn off Include signs, or remove photos from signs, and export again.`
}

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? '' : 's'}`

/** The menu's count line: `5 signs, 3 photos`. */
export function signSummary(signs: readonly Pick<ReplaySign, 'photo_url'>[]): string {
  if (signs.length === 0) return 'No signs in this range'
  return `${plural(signs.length, 'sign')}, ${plural(signs.filter((s) => s.photo_url).length, 'photo')}`
}

/** What a photo not yet prepared is guessed to add to the card, in bytes. */
export const ESTIMATED_PHOTO_BYTES = 300_000
