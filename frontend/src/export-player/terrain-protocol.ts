// Terrain in the reader's browser. The card asks AWS for Terrarium elevation
// tiles itself and flattens the sea here, the way the boat's proxy does for the
// app (lib/terrain-sea.ts has the shared arithmetic). MapLibre reaches this
// through a custom protocol, 'hcterrain-v1://{z}/{x}/{y}'. A tile that cannot be
// had is an error: nothing stands in for it. The PNG is decoded by hand
// (png.ts) because reading elevation back from a canvas is scrambled by
// browsers' privacy modes.

import { TERRARIUM_UPSTREAM_TILES } from '@/lib/terrain'
import { clampSeaPixels } from '@/lib/terrain-sea'

import { warnCsp } from './csp'
import { decodePng, type Pixels } from './png'

export type { Pixels }

/** Bump when the clamp or the upstream address changes. Cards from different
 * Helmcentral versions on one page then register different protocols, and none
 * clobbers another. */
export const TERRAIN_PROTOCOL_VERSION = 1
export const TERRAIN_PROTOCOL = `hcterrain-v${TERRAIN_PROTOCOL_VERSION}`
export const TERRAIN_PROTOCOL_TILES = `${TERRAIN_PROTOCOL}://{z}/{x}/{y}`

/** Decoding a PNG into pixels and turning pixels back into an image MapLibre
 * can read. Split out so the rest can be tested without a browser. */
export interface PixelIO {
  decode: (png: Uint8Array) => Promise<Pixels>
  encode: (pixels: Pixels) => Promise<ImageBitmap>
}

export const browserPixels: PixelIO = {
  decode: decodePng,
  // Straight from ImageData: no canvas is drawn on, so nothing is read back
  // through one here. MapLibre's own elevation path does its readback itself.
  encode: (pixels) => createImageBitmap(
    new ImageData(pixels.data as Uint8ClampedArray<ArrayBuffer>, pixels.width, pixels.height),
    { premultiplyAlpha: 'none', colorSpaceConversion: 'none' },
  ),
}

/** The z/x/y a protocol URL names. Anything else is a fault in the style. */
export function tileOfUrl(url: string): { z: number; x: number; y: number } {
  const m = new RegExp(`^${TERRAIN_PROTOCOL}://(\\d+)/(\\d+)/(\\d+)$`).exec(url)
  if (!m) throw new Error(`Not a terrain tile address: ${url}`)
  return { z: Number(m[1]), x: Number(m[2]), y: Number(m[3]) }
}

export const upstreamUrl = ({ z, x, y }: { z: number; x: number; y: number }) =>
  TERRARIUM_UPSTREAM_TILES.replace('{z}', String(z)).replace('{x}', String(x)).replace('{y}', String(y))

/** The loader to register with `maplibregl.addProtocol`. */
export function terrainLoader(io: PixelIO = browserPixels, fetchFn: typeof fetch = (...a) => fetch(...a)) {
  return async (params: { url: string }, abort: AbortController): Promise<{ data: ImageBitmap }> => {
    const tile = tileOfUrl(params.url)
    let res: Response
    try {
      res = await fetchFn(upstreamUrl(tile), { signal: abort.signal })
    } catch (err) {
      if (!abort.signal.aborted) warnCsp(err, 'connect-src s3.amazonaws.com')
      throw err
    }
    if (!res.ok) throw new Error(`Terrain tile ${tile.z}/${tile.x}/${tile.y}: the elevation server answered ${res.status}`)
    const pixels = await io.decode(new Uint8Array(await res.arrayBuffer()))
    clampSeaPixels(pixels.data)
    return { data: await io.encode(pixels) }
  }
}
