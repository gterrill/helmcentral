import { errorMessage } from '@/lib/errors'
import { apiBaseUrl } from '@/config/api'
import { downscaleImage } from '@/lib/image-downscale'
import { EXPORT_PHOTO_PREFIX, MOMENT_PHOTO_EDGE_PX, PHOTO_EDGE_PX, PHOTO_QUALITY, cleanJpeg } from '@/lib/passage-export-signs'
import { photoMomentOn, type ReplaySign } from '@/lib/replay-signs'

// Sign photos for the blog card (ADR 0205), prepared in the operator's own
// browser: fetched from the document, shrunk to 960 px on the long side and
// re-encoded as JPEG by the app's shared downscaler. The encoder's file is then
// rewritten with only the plain header segments, so no Exif (and with it no GPS
// position), XMP or comment can come along whatever the browser wrote.

function toBase64(bytes: Uint8Array): string {
  let binary = ''
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  return btoa(binary)
}

const browserDownscale = (blob: Blob, edge: number) => downscaleImage(blob, edge, { quality: PHOTO_QUALITY, opaque: true, reduceDecode: true })

/** The photos already prepared, by the address they were fetched from and the size asked for. */
const prepared = new Map<string, string>()

export interface PrepareOptions {
  /** Replaceable in tests. Shrinks to this long edge, and never enlarges. */
  downscale?: (blob: Blob, edge: number) => Promise<Blob>
  fetchFn?: typeof fetch
  cache?: Map<string, string>
  onProgress?: (done: number, total: number) => void
}

/** The long edge a sign's photo is shrunk to: the larger size when its card grows for a moment in the replay (see photoMomentOn), since that one photo serves the small card and the moment, otherwise the card's own size. */
export const photoEdgeFor = (sign: Pick<ReplaySign, 'photo_url' | 'photo_moment'>) => (photoMomentOn(sign) ? MOMENT_PHOTO_EDGE_PX : PHOTO_EDGE_PX)

/** Prepares every sign's photo, one after another: sign id to JPEG data URI.
 * Signs with no photo link are skipped. A photo that cannot be fetched, read
 * or cleaned stops the whole export and names its sign. */
export async function prepareSignPhotos(signs: readonly ReplaySign[], options: PrepareOptions = {}): Promise<Map<string, string>> {
  const downscale = options.downscale ?? browserDownscale
  const fetchFn = options.fetchFn ?? fetch
  const cache = options.cache ?? prepared
  const withPhotos = signs.filter((s) => s.photo_url)
  const out = new Map<string, string>()
  options.onProgress?.(0, withPhotos.length)
  let done = 0
  for (const sign of withPhotos) {
    const url = `${apiBaseUrl}${sign.photo_url}`
    const edge = photoEdgeFor(sign)
    const key = `${url}@${edge}`
    let uri = cache.get(key)
    if (uri === undefined) {
      const fail = (what: string, err?: unknown) => new Error(`The photo for the sign "${sign.title}" ${what}${err === undefined ? '' : `: ${errorMessage(err)}`}`)
      let res: Response
      try {
        res = await fetchFn(url)
      } catch (err) {
        throw fail('could not be fetched', err)
      }
      if (!res.ok) throw fail(`could not be read (HTTP ${res.status})`)
      let blob: Blob
      try {
        blob = await downscale(await res.blob(), edge)
      } catch (err) {
        throw fail('could not be prepared', err)
      }
      if (blob.type !== 'image/jpeg') throw fail('did not come out as a JPEG')
      let clean: Uint8Array
      try {
        clean = cleanJpeg(new Uint8Array(await blob.arrayBuffer()))
      } catch (err) {
        throw fail('could not be cleaned', err)
      }
      uri = `${EXPORT_PHOTO_PREFIX}${toBase64(clean)}`
      cache.set(key, uri)
    }
    out.set(sign.id, uri)
    options.onProgress?.(++done, withPhotos.length)
  }
  return out
}
