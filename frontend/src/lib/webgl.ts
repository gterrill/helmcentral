/**
 * Whether this browser can create a WebGL2 rendering context.
 *
 * MapLibre GL JS 5 requires WebGL2 and throws synchronously out of its own
 * constructor when it cannot get one. React 19 unmounts the whole root on an
 * uncaught render error, so a map-bearing tile that just tries anyway can
 * blank the entire app on a browser that lacks it — the wall display's WPE
 * WebKit 2.38 kiosk browser among them (ADR 0089 §11). Every map-bearing
 * tile calls this before mounting a map, and renders a quiet fallback
 * instead when it comes back false.
 *
 * Probed once and memoised: creating a canvas and asking it for a context is
 * cheap but not free, and the answer cannot change while the page stays
 * open. Returns false, rather than throwing, when the probe itself throws
 * (some WebKit builds throw instead of returning null) or when there is no
 * `document` to probe at all (server-side rendering, a non-browser test).
 */
let cachedResult: boolean | null = null

export function hasWebGL2(): boolean {
  if (cachedResult === null) {
    cachedResult = probeWebGL2()
  }
  return cachedResult
}

function probeWebGL2(): boolean {
  if (typeof document === 'undefined') return false
  try {
    const canvas = document.createElement('canvas')
    const ctx = canvas.getContext('webgl2')
    // The probe context is otherwise never released: it's created on a
    // throwaway canvas that's immediately discarded, but the GPU-backed
    // context itself would sit alive for the life of the page (or until GC
    // gets to it) with nothing ever using it. WEBGL_lose_context is the
    // standard way to force an unused context to free its resources right
    // away. Optional chaining throughout: some contexts (and every stub a
    // test hands back) may not implement getExtension at all.
    ctx?.getExtension?.('WEBGL_lose_context')?.loseContext?.()
    return ctx !== null
  } catch {
    return false
  }
}
