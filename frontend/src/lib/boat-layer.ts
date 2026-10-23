// The MapLibre custom layer that draws the boat, with three.js on the map's own
// WebGL context. Loaded with import() only when the 3D replay needs it, so
// three.js stays out of the main bundle. The three.js namespace is handed in:
// the app imports it from its own chunk, the blog card from a pinned CDN copy.

import type { CustomLayerInterface, MercatorCoordinate as MercatorCoordinateClass, Map as MapLibreMap } from 'maplibre-gl'
import type { WebGLRenderer } from 'three'

import { boatScaleFactor, resolveBoatDimensions, type BoatHull } from '@/lib/boat-model'
import { buildBoat, disposeBoat, type BoatModel, type Three } from '@/lib/boat-geometry'
import { TERRAIN_SOURCE_ID } from '@/lib/terrain'

export const BOAT_LAYER_ID = 'passage-boat-model'

export interface BoatPose {
  lat: number
  lon: number
  /** Bow bearing in degrees clockwise from north; null keeps the last one. */
  headingDeg: number | null
}

export interface BoatLayer extends CustomLayerInterface {
  /** Moves the boat and asks the map for a frame. No React involved. Null
   * takes the boat off the map. */
  setPose: (pose: BoatPose | null) => void
}

const missingLogged = new Set<string>()

/** Says once, where the operator can find it, that the boat is not to scale. */
function logMissing(hull: BoatHull, missing: ('length' | 'beam')[]) {
  if (missing.length === 0) return
  const key = `${hull.type}:${missing.join('+')}`
  if (missingLogged.has(key)) return
  missingLogged.add(key)
  const what = missing.includes('length')
    ? 'no length overall: the boat model is drawn at a fixed size on screen and is not to scale'
    : 'no beam: the beam of the boat model is estimated from a typical proportion for the hull type'
  console.info(`Boat model (${hull.type}): ${what}. Enter it under vessel details.`)
}

/** What the layer draws with, handed in by the caller. MapLibre's own class is
 * passed too, so the blog card, where MapLibre is a page global, needs no
 * import of it. */
export interface BoatLayerLibs {
  three: Three
  MercatorCoordinate: typeof MercatorCoordinateClass
}

export function createBoatLayer(hull: BoatHull, { three: T, MercatorCoordinate }: BoatLayerLibs): BoatLayer {
  const dims = resolveBoatDimensions(hull.type, hull.loaM, hull.beamM)
  logMissing(hull, dims.missing)
  let map: MapLibreMap | null = null
  let renderer: WebGLRenderer | null = null
  let model: BoatModel | null = null
  let pose: BoatPose | null = null
  let heading = 0
  let elevation = 0
  let elevationStale = true
  const camera = new T.Camera()
  const scene = new T.Scene()
  // Scratch objects for the per-frame matrix, so a frame allocates nothing.
  const placement = new T.Matrix4()
  const mvp = new T.Matrix4()
  const scale = new T.Vector3()

  const onSourceData = (e: { sourceId?: string }) => {
    if (e.sourceId === TERRAIN_SOURCE_ID) elevationStale = true
  }

  const layer: BoatLayer = {
    id: BOAT_LAYER_ID,
    type: 'custom',
    renderingMode: '3d',

    // MapLibre adds the layer again after a style rebuild without calling
    // onRemove, so everything is built once per instance and reused.
    onAdd(m, gl) {
      if (map && map !== m) map.off('sourcedata', onSourceData)
      map = m
      map.on('sourcedata', onSourceData)
      if (renderer) return
      model = buildBoat(hull.type, { loaM: dims.loaM ?? 1, beamM: dims.beamM }, T)
      scene.add(model.group)
      scene.add(new T.AmbientLight(0xffffff, 1.6))
      const sun = new T.DirectionalLight(0xffffff, 1.8)
      sun.position.set(-0.4, -0.6, 1).normalize()
      scene.add(sun)
      renderer = new T.WebGLRenderer({ canvas: m.getCanvas(), context: gl, antialias: true })
      renderer.autoClear = false
      // Creating the renderer changed GL state outside MapLibre's pass: clear
      // three.js's view of it and tell MapLibre's state cache to re-apply its
      // own. `painter` is internal to MapLibre, hence the loose type.
      renderer.resetState()
      const painter = (m as unknown as { painter?: { context?: { setDirty?: () => void } } }).painter
      if (painter?.context?.setDirty) painter.context.setDirty()
      else console.warn('Boat model: MapLibre state cache not reachable; drawing may glitch')
    },

    render(gl, options) {
      if (!map || !renderer || !model || !pose) return
      if (pose.headingDeg !== null) heading = pose.headingDeg
      if (elevationStale) {
        // The proxy clamps the sea to exactly 0 m, so null (no terrain tile
        // yet) and 0 are the same height for a boat afloat: sea level.
        elevation = map.queryTerrainElevation([pose.lon, pose.lat]) ?? 0
        elevationStale = false
      }
      const origin = MercatorCoordinate.fromLngLat([pose.lon, pose.lat], elevation)
      const s = origin.meterInMercatorCoordinateUnits() * boatScaleFactor(map.getZoom(), pose.lat, dims.loaM)
      model.group.rotation.z = -(heading * Math.PI) / 180
      // Mercator y runs south; the model's y runs north.
      placement.makeTranslation(origin.x, origin.y, origin.z).scale(scale.set(s, -s, s))
      camera.projectionMatrix.copy(mvp.fromArray(options.defaultProjectionData.mainMatrix).multiply(placement))
      // three.js draws into the viewport it was created with, which is stale
      // once the canvas has been resized; give it the one MapLibre is using.
      // MapLibre puts its own state back after a custom layer (drawCustom marks
      // its cache dirty and resets the viewport), so nothing is restored here.
      const viewport = gl.getParameter(gl.VIEWPORT) as Int32Array
      renderer.resetState()
      renderer.setViewport(viewport[0], viewport[1], viewport[2], viewport[3])
      renderer.render(scene, camera)
      if (import.meta.env.DEV) {
        // Dev only, for browser checks: where the boat is on screen and its bow.
        const at = map.project([pose.lon, pose.lat])
        ;(window as unknown as { __boatDebug?: unknown }).__boatDebug = { x: at.x, y: at.y, headingDeg: heading }
      }
    },

    onRemove() {
      if (import.meta.env.DEV) delete (window as unknown as { __boatDebug?: unknown }).__boatDebug
      map?.off('sourcedata', onSourceData)
      if (model) disposeBoat(model)
      scene.clear()
      renderer?.dispose()
      model = null
      renderer = null
      map = null
    },

    setPose(next) {
      pose = next
      elevationStale = true
      map?.triggerRepaint()
    },
  }
  return layer
}
