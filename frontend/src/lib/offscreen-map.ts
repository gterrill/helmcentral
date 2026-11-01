// A MapLibre map drawn in an element nobody sees, for the pictures and videos
// made from a passage. The still images (passage-export-image) and the video
// (passage-video/scene) both start here: make the map, wait for its style to
// load, and later wait for it to finish drawing, failing instead of handing
// back a half-drawn map. What counts as a failure is the caller's to say.

import maplibregl from 'maplibre-gl'
import type { MapOptions, TransformStyleFunction } from 'maplibre-gl'

export interface OffscreenMapOptions {
  width: number
  height: number
  pixelRatio: number
  /** How the element is placed. It must be laid out for the map to draw into it. */
  hostStyle: Partial<CSSStyleDeclaration>
  /** The rest of the map's options: the style, and where it looks. */
  map: Omit<MapOptions, 'container' | 'pixelRatio' | 'interactive' | 'attributionControl' | 'fadeDuration' | 'canvasContextAttributes'>
  /** Changes the style after it is fetched and before the map has it. */
  transformStyle?: TransformStyleFunction
  /** How long the style may take to load. */
  loadTimeoutMs: number
  loadTimeoutError: () => Error
  /** What a wait is rejected with if the map is taken down while it is waiting. */
  disposedError: () => Error
  /** What a map error is: null if it is no fault (a tile that does not exist), else the error to stop with. */
  onError: (e: { sourceId?: string; error?: Error & { status?: number } }) => Error | null
}

export interface OffscreenMap {
  map: maplibregl.Map
  /** Resolves once the map has drawn everything asked of it. Rejects on a map error, or after `timeoutMs`. */
  settle: (timeoutMs: number, timeoutError: () => Error) => Promise<void>
  /** Takes the map and its element down. Safe to call twice. */
  dispose: () => void
}

/** The map, once its style has loaded. Throws if the style does not load in time or the map reports a fault. */
export async function openOffscreenMap(o: OffscreenMapOptions): Promise<OffscreenMap> {
  const host = document.createElement('div')
  Object.assign(host.style, o.hostStyle, { width: `${o.width}px`, height: `${o.height}px` })
  document.body.appendChild(host)

  // A style that is to be changed on its way in is set after the map is made, since only setStyle takes the change.
  const { style, ...rest } = o.map
  const map = new maplibregl.Map({
    ...rest,
    ...(o.transformStyle ? {} : { style }),
    container: host,
    interactive: false,
    attributionControl: false,
    fadeDuration: 0,
    pixelRatio: o.pixelRatio,
    canvasContextAttributes: { preserveDrawingBuffer: true },
  })
  let disposed = false
  // Every wait that is still going, so that taking the map down ends them.
  const pending = new Set<(e: Error) => void>()
  const dispose = () => {
    if (disposed) return
    disposed = true
    pending.forEach((end) => end(o.disposedError()))
    map.remove()
    host.remove()
  }

  // The first fault is kept, and stops whatever is being waited on.
  let failure: Error | null = null
  let fail: ((e: Error) => void) | null = null
  map.on('error', (e) => {
    const err = o.onError(e as { sourceId?: string; error?: Error & { status?: number } })
    if (!err) return
    failure ??= err
    fail?.(failure)
  })

  const until = (event: 'load' | 'idle', timeoutMs: number, timeoutError: () => Error) => new Promise<void>((resolve, reject) => {
    if (failure) { reject(failure); return }
    if (disposed) { reject(o.disposedError()); return }
    const finish = (err?: Error) => {
      clearTimeout(timer)
      map.off(event, on)
      pending.delete(finish)
      if (fail === finish) fail = null
      if (err) reject(err)
      else if (failure) reject(failure)
      else resolve()
    }
    const on = () => finish()
    const timer = setTimeout(() => finish(timeoutError()), timeoutMs)
    fail = finish
    pending.add(finish)
    map.on(event, on)
  })

  try {
    if (o.transformStyle && style) map.setStyle(style, { transformStyle: o.transformStyle })
    await until('load', o.loadTimeoutMs, o.loadTimeoutError)
  } catch (err) {
    dispose()
    throw err
  }
  return {
    map,
    settle: (timeoutMs, timeoutError) => {
      const done = until('idle', timeoutMs, timeoutError)
      map.triggerRepaint()
      return done
    },
    dispose,
  }
}
