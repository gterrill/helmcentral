// The replay player that ships inside a blog export (ADR 0174, 0205, 0250). It
// runs in a reader's browser on a blog page, with MapLibre already loaded as a
// page global, and knows nothing about Helmcentral. It holds no clock, camera,
// heading or sign timing of its own: the replay is the app's. The card's
// passages, shots and signs go to the app's planner (./plan), the replay
// controller works out each frame (the boat, its heading, the sailed track
// coloured by speed, the light, the cards, the photo moment, the stays and the
// passage labels), and this file puts the frame on a plain MapLibre map, the
// way the video's map does. The player is built from those modules by
// vite-plugin-passage-player.ts and pasted into the snippet, so it imports no
// React, no react-map-gl and nothing that touches the boat. Every visible
// string is set with textContent.
//
// It runs on a stranger's phone, so a frame reads nothing from the layout: the
// map's size and the readout's box are measured when they change, and the page
// is written to only where a value has changed.

import type * as MapLibre from 'maplibre-gl'

import { createBoatLayer, type BoatLayer } from '@/lib/boat-layer'
import { boatMarkerKind } from '@/lib/boat-model'
import { MAP_LABEL, labelHasRoom, mapLabelStyle, type MapLabelBox, type ReservedArea } from '@/lib/map-label'
import type { ExportData } from '@/lib/passage-export'
import { exportColour } from '@/lib/passage-export-style'
import { isMultiPassage } from '@/lib/passages'
import { CameraGate, outsideCentral, zoomBucket, type Camera } from '@/lib/replay-camera'
import { ReplayController, ReplayPlayer, type ReplayFrame } from '@/lib/replay-controller'
import { FIT_PADDING, cruiseFramingFor, introLanding, type CruiseFraming } from '@/lib/replay-framing'
import { INTRO_MS, introStartCamera, MERCATOR, type MapProjection } from '@/lib/replay-intro'
import { hillshadeLayer, lightController, seamarkLayer, tintLayer } from '@/lib/replay-map-style'
import { addSpecLayer, addWholeTrack, createFramePainter } from '@/lib/replay-overlays'
import { passageLabelBox } from '@/lib/replay-passage-label'
import { stayLabelBox } from '@/lib/replay-stay'
import { playedIds } from '@/lib/replay-time'
import { formatHourMinute } from '@/lib/vessel-time'
import {
  HILLSHADE_LAYER_ID,
  REPLAY_3D_MAX_PITCH,
  REPLAY_3D_PITCH,
  TERRAIN_SOURCE_ID,
  terrainSetting,
  terrainSourceSpec,
} from '@/lib/terrain'

import { buildReplay, hullOf } from './plan'
import { createSignLayer, type SignLayer } from './signs'
import { TERRAIN_PROTOCOL, TERRAIN_PROTOCOL_TILES, terrainLoader } from './terrain-protocol'
import { loadThree } from './three-loader'

type Lib = typeof MapLibre

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
const MAP_ERROR = 'The map could not load. Check your connection and reload the page.'
const LEVEL_MS = 600
/** How far a mouse moves while down before it counts as a drag. */
const DRAG_YIELD_PX = 4
/** How far a finger may move and still be a tap. Further, it is the page being scrolled. */
const TAP_PX = 10
/** The properties of a label's style that are pixels. */
const PX = new Set(['left', 'top', 'fontSize', 'borderRadius', 'maxWidth'])


/** Writes text only where it has changed. */
function setText(e: HTMLElement, text: string) {
  if (e.textContent !== text) e.textContent = text
}

export function initCard(id: string, maplibregl: Lib | null | undefined, doc: Document) {
  const root = doc.getElementById(id) as HTMLElement
  const data = JSON.parse((doc.getElementById(`${id}-data`) as HTMLElement).textContent as string) as ExportData
  const part = (name: string) => root.querySelector(`.hcp-${name}`) as HTMLElement
  const els = {
    title: part('title'), dates: part('dates'), stats: part('stats'), map: part('map'),
    inset: part('inset'), readout: part('readout'),
    passage: part('passage'), time: part('time'), btn: part('btn'), label: part('btn-label'),
    error: part('error'), notice: part('notice'),
  }

  els.title.textContent = data.title
  els.dates.textContent = data.dates
  for (const s of data.stats) {
    const item = doc.createElement('div')
    item.className = 'hcp-stat'
    const k = doc.createElement('span')
    k.className = 'hcp-k'
    k.textContent = s.l
    const v = doc.createElement('span')
    v.className = 'hcp-v'
    v.textContent = s.v
    item.appendChild(k)
    item.appendChild(v)
    els.stats.appendChild(item)
  }
  if (data.stats.length === 0) els.stats.hidden = true

  const fmtLocal = (unix: number) => {
    const d = new Date((unix + data.zone.o * 3600) * 1000)
    return `${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]} ${formatHourMinute(d.getUTCHours(), d.getUTCMinutes())} ${data.zone.l}`
  }

  const { plan, config } = buildReplay(data)
  const controller = new ReplayController()
  const player = new ReplayPlayer(config.time)
  const api = { data, plan, config, player, controller, fmtLocal, map: undefined as MapLibre.Map | undefined }

  if (!maplibregl) {
    els.error.textContent = MAP_ERROR
    els.error.hidden = false
    els.btn.hidden = true
    return api
  }
  const lib = maplibregl

  // ── the map ─────────────────────────────────────────────────────────────

  const bounds: [[number, number], [number, number]] = [[data.bounds[0], data.bounds[1]], [data.bounds[2], data.bounds[3]]]
  const win = root.ownerDocument.defaultView
  const reduced = !!(win && win.matchMedia && win.matchMedia('(prefers-reduced-motion: reduce)').matches)

  const map = new lib.Map({
    container: els.map,
    style: data.map.style,
    bounds,
    fitBoundsOptions: { padding: FIT_PADDING, maxZoom: 14 },
    attributionControl: { compact: true },
    cooperativeGestures: true,
    // Rotating and tilting belong to the 3D card only; the flat card is as it
    // always was.
    dragRotate: data.d3,
    pitchWithRotate: data.d3,
    ...(data.d3 ? { maxPitch: REPLAY_3D_MAX_PITCH } : {}),
  })
  api.map = map
  map.addControl(new lib.NavigationControl({ showCompass: false }), 'top-left')
  let loaded = false
  let wantStart = false
  let terrain: 'ok' | 'failed' = 'ok'
  const flat = () => !data.d3 || terrain === 'failed'

  // The map's size, measured when it changes (MapLibre says so with 'resize').
  let size = { w: 0, h: 0 }
  const measureMap = () => {
    const c = map.getContainer()
    size = { w: c.clientWidth, h: c.clientHeight }
  }
  measureMap()
  // The sign layer places its cards again once the size is read: it is told here, not by its own listener, so the order of listeners does not matter.
  map.on('resize', () => {
    measureMap()
    measureInset()
    signLayer?.resized()
  })

  // MapLibre opens its compact credit as a long strip the first time it fills,
  // which the readout would sit on. Keep it as the small icon until the reader
  // opens it themselves, as the app's own maps do.
  let creditOpenedByReader = false
  els.map.addEventListener('click', (e) => {
    if ((e.target as Element | null)?.closest?.('.maplibregl-ctrl-attrib-button')) creditOpenedByReader = true
  })
  map.on('idle', () => {
    if (creditOpenedByReader) return
    const credit = map.getContainer().querySelector('.maplibregl-ctrl-attrib')
    if (!credit || !credit.classList.contains('maplibregl-compact-show')) return
    credit.classList.remove('maplibregl-compact-show')
    credit.removeAttribute('open')
  })

  // The sky and light follow the sun over the boat, as the replay's do. The
  // card has no theme of its own: it is the day sky, and no satellite imagery
  // (the blog map is always the chart).
  const scene = { isDarkTheme: false, satellite: false, terrain: data.d3, camera3d: data.d3 }
  const light = lightController(map, scene)

  // ── terrain ─────────────────────────────────────────────────────────────

  // Registered once per MapLibre and per protocol version: a second card on the
  // same MapLibre, or a card from another Helmcentral release, neither repeats
  // nor replaces it. It is per MapLibre because each pasted card loads its own
  // copy of the library, and a later copy replaces the page's `maplibregl`; a
  // card handed that copy has to register on it.
  function registerTerrainProtocol() {
    const page = globalThis as { __hcpProtocols?: WeakMap<object, Set<string>> }
    const all = (page.__hcpProtocols ??= new WeakMap())
    const have = all.get(lib) ?? new Set<string>()
    all.set(lib, have)
    if (have.has(TERRAIN_PROTOCOL)) return
    have.add(TERRAIN_PROTOCOL)
    lib.addProtocol(TERRAIN_PROTOCOL, terrainLoader())
  }

  function addTerrain() {
    registerTerrainProtocol()
    // The tiles come from the card's own protocol: a public page cannot reach the boat's.
    map.addSource(TERRAIN_SOURCE_ID, { ...terrainSourceSpec, tiles: [TERRAIN_PROTOCOL_TILES] })
    addSpecLayer(map, { ...hillshadeLayer(), beforeId: data.map.seamarks.layerId }, TERRAIN_SOURCE_ID)
    map.setTerrain(terrainSetting)
  }

  // Fail loud: a terrain tile that cannot be had drops the card back to flat
  // and says so. Nothing stands in for it.
  function terrainFailed(err: unknown) {
    if (terrain === 'failed') return
    terrain = 'failed'
    console.error('Terrain unavailable', err)
    els.notice.textContent = 'Terrain unavailable'
    els.notice.hidden = false
    scene.terrain = false
    scene.camera3d = false
    try {
      map.setTerrain(null)
      map.setSky(undefined as never)
      if (map.getLayer(HILLSHADE_LAYER_ID)) map.removeLayer(HILLSHADE_LAYER_ID)
    } catch (e) {
      console.error('Terrain could not be taken off the map', e)
    }
    dropModel()
    if (controller.introActive) {
      // The fly-in is heading for a tilted view that will not be drawn: turn
      // it round to the flat framing, and let it begin the replay from there.
      const f = framing()
      if (f) controller.startIntro(readCamera(), introLanding(f, config.timeline), performance.now(), INTRO_MS / 2)
    } else if (!reduced) {
      map.easeTo({ pitch: 0, bearing: 0, duration: LEVEL_MS })
    } else {
      map.jumpTo({ pitch: 0, bearing: 0 })
    }
  }

  // ── signs ───────────────────────────────────────────────────────────────

  // Pins at rest, before Play and when nothing is up; cards, and the photo
  // moment, from the controller's frame while it plays. A photo opens only when
  // the replay is not running.
  function showProblem(message: string) {
    els.error.textContent = message
    els.error.hidden = false
  }
  const signLayer: SignLayer | null = plan.labelled.length > 0
    ? createSignLayer({
      doc, root, stage: part('stage'), map, lib, id, signs: plan.labelled, still: reduced, onProblem: showProblem,
      container: () => size,
    })
    : null
  signLayer?.update({ cards: [], pins: true, canOpenPhoto: true })

  // ── labels ──────────────────────────────────────────────────────────────

  // The stay's label and the passage's, placed by the rule the replay places
  // them by, clear of the overview map in a bottom corner and of the readout.
  const makeLabel = () => {
    const e = doc.createElement('span')
    e.className = 'hcp-maplabel'
    e.style.background = `rgba(255, 255, 255, ${MAP_LABEL.bgAlpha})`
    e.hidden = true
    part('stage').appendChild(e)
    return { e, drawn: '' }
  }
  const stayLabel = makeLabel()
  const passageLabel = makeLabel()
  function showLabel(label: { e: HTMLElement; drawn: string }, text: string | null, box: MapLabelBox | null) {
    // No room between what it keeps clear of: no label, not an empty pill (a video skips it too).
    if (text === null || box === null || !labelHasRoom(box)) {
      if (!label.e.hidden) { label.e.hidden = true; label.drawn = '' }
      return
    }
    const style = mapLabelStyle(box)
    const drawn = `${text}|${JSON.stringify(style)}`
    if (drawn === label.drawn) return
    label.drawn = drawn
    label.e.hidden = false
    setText(label.e, text)
    for (const [k, v] of Object.entries(style)) {
      ;(label.e.style as unknown as Record<string, string>)[k] = v === undefined ? '' : PX.has(k) ? `${v}px` : String(v)
    }
  }

  // ── the boat ────────────────────────────────────────────────────────────

  const boatEl = doc.createElement('div')
  boatEl.className = 'hcp-boat'
  const boat = new lib.Marker({ element: boatEl })
  let boatShown = false
  let modelLayer: BoatLayer | null = null
  let modelReady = false
  let lastPose: { lat: number; lon: number; headingDeg: number | null } | null = null
  const hull = hullOf(data)

  function showMarker() {
    const kind = boatMarkerKind({ threeD: data.d3, terrain: terrain === 'ok' ? 'ok' : 'failed', hull, layerReady: modelReady })
    boatEl.style.display = kind === 'model' ? 'none' : ''
  }

  function dropModel() {
    if (modelLayer && map.getLayer(modelLayer.id)) map.removeLayer(modelLayer.id)
    modelLayer = null
    modelReady = false
    showMarker()
  }

  // The model is fetched only when 3D is on and the vessel has a hull type.
  // Whatever goes wrong, the glowing dot stays and the console says why.
  function addModel() {
    const source = data.map.three
    if (!data.d3 || !hull || !source) return
    loadThree(source).then((three) => {
      if (terrain === 'failed') return
      const layer = createBoatLayer(hull, { three, MercatorCoordinate: lib.MercatorCoordinate })
      map.addLayer(layer)
      modelLayer = layer
      modelReady = true
      if (lastPose) layer.setPose(lastPose)
      showMarker()
    }).catch((err) => {
      console.error('Boat model unavailable', err)
    })
  }

  // ── what is on the map ──────────────────────────────────────────────────

  map.on('style.load', () => {
    map.addSource(data.map.seamarks.id, {
      type: 'raster', tiles: [data.map.seamarks.tiles], tileSize: 256, attribution: data.map.seamarks.attribution,
    })
    addSpecLayer(map, { ...seamarkLayer(), id: data.map.seamarks.layerId }, data.map.seamarks.id)
    if (data.d3) addTerrain()
    // The time of day goes over the chart; the style's own place names go back
    // over the time of day, so the names stay as readable at night as by day.
    addSpecLayer(map, tintLayer(null, false))
    for (const layer of map.getStyle().layers) if (layer.type === 'symbol') map.moveLayer(layer.id)
    // The whole track, faint, with the sailed part drawn over it as it is sailed.
    addWholeTrack(map, config.pieces, (p) => exportColour(p.colourIndex), false)
    loaded = true
    addModel()
    if (wantStart) begin()
  })
  map.on('error', (e: { error?: Error; sourceId?: string }) => {
    if (e && e.sourceId === TERRAIN_SOURCE_ID) { terrainFailed(e.error); return }
    if (e && e.error && !loaded) {
      els.error.textContent = MAP_ERROR
      els.error.hidden = false
    }
  })

  const paint = createFramePainter({
    map, light, pieces: config.pieces, colourOf: (p) => exportColour(p.colourIndex), satellite: false, boatLayerId: () => modelLayer?.id ?? null,
  })

  function setMode(mode: string, label: string) {
    root.className = `hcp hcp-${mode}`
    els.label.textContent = label
  }

  // The part of the map the readout covers, and how far up it a bottom-corner
  // overview map sits; measured when the readout changes, not at every frame.
  let readoutBox: ReservedArea | null = null
  // The overview map's real box, measured with the readout (it sits above it) and when the map resizes, never at a frame.
  let insetBox: ReservedArea | null = null
  // Both boxes, built where they are measured and read at every frame.
  let reserved: ReservedArea[] = []
  function showFrame(frame: ReplayFrame) {
    paint(frame, playedIds(config.timeline, player.clock))

    if (frame.boat) {
      boat.setLngLat([frame.boat.lon, frame.boat.lat])
      if (!boatShown) { boat.addTo(map); boatShown = true }
      lastPose = { lat: frame.boat.lat, lon: frame.boat.lon, headingDeg: frame.heading }
    } else {
      lastPose = null
    }
    if (modelReady && modelLayer) modelLayer.setPose(lastPose)

    if (frame.here) {
      setText(els.passage, data.passages[Number(frame.here.entry.id)].n)
      setText(els.time, fmtLocal(frame.stay ? frame.stay.sunAt : frame.here.at))
    }

    // The overview map is smaller on this page than in the app and sits above
    // the readout, so the labels keep clear of the boxes measured for them
    // (`reserved`), not of the app's corner rule.
    showLabel(stayLabel, frame.stay ? frame.stay.stay.label : null, frame.stay ? stayLabelBox(size, frame.stay.elapsed, null, reserved) : null)
    showLabel(passageLabel, frame.passageLabel ? frame.passageLabel.label.text : null, frame.passageLabel ? passageLabelBox(size, frame.passageLabel.elapsed, null, reserved) : null)

    // Where the replay has run to its end the signs are pins again, as they were before it, so a reader can open a photo.
    const rest = player.ended
    signLayer?.update({ cards: rest ? [] : frame.cards, pins: rest || frame.pins.length > 0, canOpenPhoto: !player.playing, moment: rest ? null : frame.moment })
  }

  // ── the overview map ────────────────────────────────────────────────────

  const centre: [number, number] = [(data.bounds[0] + data.bounds[2]) / 2, (data.bounds[1] + data.bounds[3]) / 2]
  let insetMap: MapLibre.Map | null = null
  function makeInset() {
    if (!data.inset || insetMap) return
    els.inset.hidden = false
    measureInset()
    insetMap = new lib.Map({
      container: els.inset, style: data.map.style, center: centre, zoom: data.insetZoom,
      interactive: false, attributionControl: false,
    })
    const inset = insetMap
    inset.on('style.load', () => {
      const b = data.bounds
      inset.addSource('hcp-box', {
        type: 'geojson',
        data: { type: 'Feature', properties: {}, geometry: { type: 'Polygon', coordinates: [[[b[0], b[1]], [b[2], b[1]], [b[2], b[3]], [b[0], b[3]], [b[0], b[1]]]] } },
      })
      if (data.insetDot) {
        inset.addSource('hcp-dot', { type: 'geojson', data: { type: 'Feature', properties: {}, geometry: { type: 'Point', coordinates: centre } } })
        inset.addLayer({ id: 'hcp-dot', type: 'circle', source: 'hcp-dot', paint: { 'circle-radius': 4, 'circle-color': '#e11d48', 'circle-stroke-color': '#ffffff', 'circle-stroke-width': 2 } })
      } else {
        inset.addLayer({ id: 'hcp-box-fill', type: 'fill', source: 'hcp-box', paint: { 'fill-color': '#e11d48', 'fill-opacity': 0.2 } })
        inset.addLayer({ id: 'hcp-box-line', type: 'line', source: 'hcp-box', paint: { 'line-color': '#e11d48', 'line-width': 2 } })
      }
    })
  }
  // A bottom-corner overview sits just above the readout, whatever height the
  // readout has. The gap is the distance from the stage's bottom edge to the
  // readout's top, plus a margin; unset while the readout is hidden, so the
  // stylesheet's own offset applies. The readout's box is what the labels keep
  // clear of.
  function measureReadout() {
    if (els.readout.hidden) {
      readoutBox = null
      root.style.removeProperty('--hcp-ro-gap')
    } else {
      readoutBox = { x: els.readout.offsetLeft, y: els.readout.offsetTop, w: els.readout.offsetWidth, h: els.readout.offsetHeight }
      root.style.setProperty('--hcp-ro-gap', `${part('stage').clientHeight - readoutBox.y + 8}px`)
    }
    measureInset()
  }
  // Read after the gap above is set: the overview map's place depends on it. Not shown yet, or with no size, it reserves nothing.
  function measureInset() {
    insetBox = data.inset && !els.inset.hidden && els.inset.offsetWidth > 0
      ? { x: els.inset.offsetLeft, y: els.inset.offsetTop, w: els.inset.offsetWidth, h: els.inset.offsetHeight }
      : null
    reserved = [readoutBox, insetBox].filter((a): a is ReservedArea => a !== null)
  }
  doc.defaultView?.addEventListener('resize', measureReadout)
  if (typeof ResizeObserver !== 'undefined') new ResizeObserver(measureReadout).observe(els.readout)

  // ── the camera ──────────────────────────────────────────────────────────

  const readCamera = (): Camera => {
    const c = map.getCenter()
    return { center: [c.lng, c.lat], zoom: map.getZoom(), pitch: map.getPitch(), bearing: map.getBearing() }
  }

  // The whole track, and each passage of a cruise, framed in this map's size
  // and shape; worked out again when either changes.
  let framingKey = ''
  let framed: CruiseFraming | null = null
  function framing(): CruiseFraming | null {
    const key = `${size.w}x${size.h}|${flat() ? 0 : 1}`
    if (framed && key === framingKey) return framed
    const next = cruiseFramingFor(map, data.bounds, plan.passageBounds, flat() ? 0 : REPLAY_3D_PITCH)
    if (!next) {
      console.error('The map could not work out where to look')
      return null
    }
    framingKey = key
    framed = next
    return next
  }

  // A person taking hold of the map (a drag, a wheel turn, a pinch) takes the
  // camera until the next shot; a tap does not (it ends the opening move or a
  // photo moment instead). The camera's own jumps fire movestart and interrupt
  // a drag in progress, so a drag is read from the pointer itself, as the app's
  // map does: a mouse or pen moving past a few pixels, a second finger down, or
  // the wheel (with Ctrl or Command, which is what zooms a card with
  // cooperative gestures). A single finger is left to the page: it scrolls
  // rather than moves the map, so it is a tap only when it lifts having hardly
  // moved; a mouse press is a tap at once.
  let gate = new CameraGate()
  const down = new Map<number, { x: number; y: number; touch: boolean; moved: boolean }>()
  const surface = map.getCanvasContainer()
  surface.addEventListener('pointerdown', (e) => {
    const touch = e.pointerType === 'touch'
    down.set(e.pointerId ?? 0, { x: e.clientX, y: e.clientY, touch, moved: false })
    if (down.size > 1) {
      gate.yield()
      for (const d of down.values()) d.moved = true
    }
    if (!touch) interrupt(e)
  })
  surface.addEventListener('pointermove', (e) => {
    const start = down.get(e.pointerId ?? 0)
    if (!start) return
    const far = Math.hypot(e.clientX - start.x, e.clientY - start.y)
    if (!start.touch && far > DRAG_YIELD_PX) gate.yield()
    if (far > TAP_PX) start.moved = true
  })
  surface.addEventListener('pointerup', (e) => {
    const start = down.get(e.pointerId ?? 0)
    down.delete(e.pointerId ?? 0)
    if (start && start.touch && !start.moved) interrupt(e)
  })
  surface.addEventListener('pointercancel', (e) => { down.delete(e.pointerId ?? 0) })
  surface.addEventListener('wheel', (e) => { if (e.ctrlKey || e.metaKey) { gate.yield(); interrupt(e) } }, { passive: true })
  map.on('movestart', (e: { originalEvent?: unknown }) => {
    // The camera's own jumps carry no originalEvent.
    if (e.originalEvent) { gate.yield(); interrupt() }
  })

  function placeCamera(frame: ReplayFrame, nowMs: number) {
    // A single passage with no shots leaves the camera where the opening move put it.
    if (config.resolved.length === 0 && !isMultiPassage(config.timeline)) return
    const f = framing()
    if (!f || !frame.boat) return
    const current = readCamera()
    const result = controller.cameraAt({ flat: flat(), overview: f.overview, passages: f.passages, current }, nowMs)
    if (!result) return
    const place = gate.place({
      result, resume, reduced, current,
      boatOutside: () => (frame.boat ? outsideCentral(map.project([frame.boat.lon, frame.boat.lat]), size.w, size.h) : false),
    })
    if (place) map.jumpTo(place)
  }

  // ── playing ─────────────────────────────────────────────────────────────

  let resume = 0
  let raf = 0
  let lastFrame: ReplayFrame | null = null
  let introFlatAt: number | null | undefined

  /** The frame at the place the replay is at, with the camera for it placed before the cards are, so each card is put once against the camera it will be seen with. */
  function render(nowMs: number, placing: boolean) {
    const frame = controller.step(config, {
      wall: player.wall, nowMs, playing: player.playing, resume, reduced, wantHeading: plan.wantHeading, signsOpen: false,
      view: { zoom: zoomBucket(map.getZoom()) },
    })
    lastFrame = frame
    if (placing) placeCamera(frame, nowMs)
    showFrame(frame)
  }

  function loop(now: number) {
    raf = 0
    if (controller.introActive) {
      const at = controller.introAt(now)
      if (!at) return
      placeIntro(at)
      if (at.done) land(now)
      else raf = requestAnimationFrame(loop)
      return
    }
    if (!player.playing) return
    player.tick(now)
    render(now, true)
    if (player.ended) { finish(); return }
    raf = requestAnimationFrame(loop)
  }
  const schedule = () => { if (!raf) raf = requestAnimationFrame(loop) }

  function finish() {
    player.pause()
    setMode('done', 'Replay')
  }

  /** The camera and the projection (the globe, flat by the time it lands) go to the map; the projection when the zoom it is flat from changes. */
  function placeIntro(at: { camera: Camera; projection: MapProjection; flatAtZoom: number | null }) {
    map.jumpTo(at.camera)
    if (at.flatAtZoom !== introFlatAt) {
      introFlatAt = at.flatAtZoom
      map.setProjection(at.projection)
    }
  }

  /** The opening move is over, or a hand took the map out of its hands: the replay plays from the start. */
  function land(now: number) {
    if (!controller.introActive) return
    controller.endIntro()
    // Left on the globe by a hand before it landed: the map goes flat at once.
    if (introFlatAt !== null) { map.setProjection(MERCATOR); introFlatAt = null }
    // The camera comes down on its framing, unless a hand has the map.
    const f = framing()
    if (f && !gate.taken) map.jumpTo(introLanding(f, config.timeline))
    setMode('playing', 'Pause')
    els.readout.hidden = false
    measureReadout()
    makeInset()
    player.rewind()
    player.play(now)
    render(now, false)
    schedule()
  }

  /** A tap on the map ends the opening move, or a photo moment. A tap on a small card on the map is not a hand on the map; on the grown card it is, as in the app, whose card lets touches through. */
  function interrupt(e?: Event) {
    const moment = player.playing && lastFrame?.moment ? lastFrame.moment : null
    if (!moment && e?.target instanceof Element && e.target.closest('.maplibregl-marker')) return
    const now = performance.now()
    if (controller.introActive) { land(now); return }
    if (moment) {
      player.skipMoment()
      render(now, false)
    }
  }

  // Start high over the globe so a reader who does not know the coast sees
  // where in the world this is, then come down onto the track (a cruise comes
  // down on its first passage).
  function begin() {
    wantStart = false
    measureMap()
    const f = framing()
    if (!f) {
      showProblem(MAP_ERROR)
      return
    }
    player.rewind()
    gate = new CameraGate()
    resume++
    const now = performance.now()
    introFlatAt = undefined
    root.classList.remove('hcp-paused')
    setMode('flying', 'Play')
    controller.startIntro(introStartCamera(data.bounds, f.overview.bearing), introLanding(f, config.timeline), now, reduced ? 0 : INTRO_MS)
    render(now, false)
    const first = controller.introAt(now)
    if (first) placeIntro(first)
    if (first?.done) land(now)
    else schedule()
  }

  els.btn.addEventListener('click', () => {
    // Pressed again while the opening move flies: it carries on, once.
    if (controller.introActive) return
    const now = performance.now()
    if (player.playing) {
      player.pause()
      els.label.textContent = 'Resume'
      root.classList.add('hcp-paused')
      render(now, false)
      return
    }
    // Paused part way: play on from there, with no opening move.
    if (player.wall > 0 && !player.ended) {
      resume++
      player.play(now)
      els.label.textContent = 'Pause'
      root.classList.remove('hcp-paused')
      render(now, false)
      schedule()
      return
    }
    if (loaded) begin()
    else { wantStart = true; setMode('flying', 'Play') }
  })

  return api
}
