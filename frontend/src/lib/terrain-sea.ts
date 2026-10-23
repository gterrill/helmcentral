// Flattening the sea in Terrarium elevation tiles. The boat's own proxy does
// this for the app (backend/terrain_proxy.go); the blog card does it in the
// reader's browser, so both must agree to the pixel. Pure: no canvas here.

/** Terrarium encodes elevation as R*256 + G + B/256 - 32768, so 0 m is
 * R=128 G=0 B=0 and any pixel whose 24-bit value R<<16|G<<8|B is below
 * 128<<16 lies below sea level. */
export const TERRAIN_SEA_FLOOR = 128 << 16

/** Sets every RGBA pixel below sea level to exactly 0 m (128, 0, 0, 255), in
 * place, and returns how many it changed. */
export function clampSeaPixels(rgba: Uint8ClampedArray | Uint8Array): number {
  if (rgba.length % 4 !== 0) throw new Error('Terrain tile data is not whole pixels')
  let changed = 0
  for (let i = 0; i < rgba.length; i += 4) {
    if (((rgba[i] << 16) | (rgba[i + 1] << 8) | rgba[i + 2]) < TERRAIN_SEA_FLOOR) {
      rgba[i] = 128
      rgba[i + 1] = 0
      rgba[i + 2] = 0
      rgba[i + 3] = 255
      changed++
    }
  }
  return changed
}
