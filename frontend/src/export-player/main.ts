// The replay player that ships inside a blog export (ADR 0174, 0205). It runs
// in a reader's browser on a blog page, with MapLibre already loaded as a page
// global, and knows nothing about Helmcentral. It is built from the same
// modules as the app's replay (the clock, the camera director, the pitched fit,
// the boat model) by vite-plugin-passage-player.ts and pasted into the snippet,
// so it imports no React, no react-map-gl and nothing that touches the boat.
// Every visible string is set with textContent.

import type * as MapLibre from 'maplibre-gl'

import { createBoatLayer, type BoatLayer } from '@/lib/boat-layer'
import { boatMarkerKind, HeadingSmoother, nextHeading, type HeadingState } from '@/lib/boat-model'
import { fitZoomForPitch } from '@/lib/pitch-fit'
import type { ExportData } from '@/lib/passage-export'
import { exportLegSegments } from '@/lib/passage-legs'
import { createSailedCache, type TimedSegment } from '@/lib/passages'
import { visibleSigns } from '@/lib/replay-signs'
import { CameraDirector, CameraGate, outsideCentral, shotStartState, type Camera } from '@/lib/replay-camera'
import {
  hillshadePaint,
  REPLAY_3D_MAX_PITCH,
  REPLAY_3D_PITCH,
  skyFor,
  TERRAIN_ATTRIBUTION,
  TERRAIN_EXAGGERATION,
  TERRAIN_MAX_ZOOM,
  TERRAIN_SOURCE_ID,
} from '@/lib/terrain'

import { buildPlan, type CardState } from './plan'
import { createSignLayer, type SignLayer } from './signs'
import { TERRAIN_PROTOCOL, TERRAIN_PROTOCOL_TILES, terrainLoader } from './terrain-protocol'
import { loadThree } from './three-loader'

type Lib = typeof MapLibre

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
const HILLSHADE_LAYER_ID = 'hcp-hillshade'
const MAP_ERROR = 'The map could not load. Check your connection and reload the page.'
const FLY_MS = 3200
const LEVEL_MS = 600
/** How far a mouse moves while down before it counts as a drag. */
const DRAG_YIELD_PX = 4

const pad = (n: number) => (n < 10 ? '0' : '') + n

export function initCard(id: string, maplibregl: Lib | null | undefined, doc: Document) {
  const root = doc.getElementById(id) as HTMLElement
  const data = JSON.parse((doc.getElementById(`${id}-data`) as HTMLElement).textContent as string) as ExportData
  const part = (name: string) => root.querySelector(`.hcp-${name}`) as HTMLElement
  const els = {
    title: part('title'), dates: part('dates'), stats: part('stats'), map: part('map'),
    inset: part('inset'), readout: part('readout'), caption: part('caption'),
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
    return `${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]} ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())} ${data.zone.l}`
  }

  const plan = buildPlan(data)
  const api = { data, steps: plan.steps, total: plan.total, stateAt: plan.stateAt, fmtLocal, plan, map: undefined as MapLibre.Map | undefined }

  if (!maplibregl) {
    els.error.textContent = MAP_ERROR
    els.error.hidden = false
    els.btn.hidden = true
    return api
  }
  const lib = maplibregl

  // ── the map ─────────────────────────────────────────────────────────────

  const bounds: [[number, number], [number, number]] = [[data.bounds[0], data.bounds[1]], [data.bounds[2], data.bounds[3]]]
  const centre: [number, number] = [(data.bounds[0] + data.bounds[2]) / 2, (data.bounds[1] + data.bounds[3]) / 2]
  const win = root.ownerDocument.defaultView
  const reduced = !!(win && win.matchMedia && win.matchMedia('(prefers-reduced-motion: reduce)').matches)
  const padding = { top: 56, bottom: 56, left: 40, right: 40 }

  const map = new lib.Map({
    container: els.map,
    style: data.map.style,
    bounds,
    fitBoundsOptions: { padding, maxZoom: 14 },
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
    map.addSource(TERRAIN_SOURCE_ID, {
      type: 'raster-dem', tiles: [TERRAIN_PROTOCOL_TILES], encoding: 'terrarium', tileSize: 256,
      maxzoom: TERRAIN_MAX_ZOOM, attribution: TERRAIN_ATTRIBUTION,
    })
    map.addLayer({ id: HILLSHADE_LAYER_ID, type: 'hillshade', source: TERRAIN_SOURCE_ID, paint: hillshadePaint }, data.map.seamarks.layerId)
    map.setTerrain({ source: TERRAIN_SOURCE_ID, exaggeration: TERRAIN_EXAGGERATION })
    // The card has no theme of its own: it is always the day sky.
    map.setSky(skyFor(false) as never)
  }

  // Fail loud: a terrain tile that cannot be had drops the card back to flat
  // and says so. Nothing stands in for it.
  function terrainFailed(err: unknown) {
    if (terrain === 'failed') return
    terrain = 'failed'
    console.error('Terrain unavailable', err)
    els.notice.textContent = 'Terrain unavailable'
    els.notice.hidden = false
    try {
      map.setTerrain(null)
      map.setSky(undefined as never)
      if (map.getLayer(HILLSHADE_LAYER_ID)) map.removeLayer(HILLSHADE_LAYER_ID)
    } catch (e) {
      console.error('Terrain could not be taken off the map', e)
    }
    dropModel()
    if (flying) {
      // The fly-in is heading for a tilted view that will not be drawn: turn
      // it round to the flat framing, and let it begin the replay from there.
      overviewCache = null
      const flatView = overviewCamera()
      if (flatView) arrive(flatView, FLY_MS / 2)
    } else if (!reduced) {
      map.easeTo({ pitch: 0, bearing: 0, duration: LEVEL_MS })
    } else {
      map.jumpTo({ pitch: 0, bearing: 0 })
    }
  }

  // ── signs ───────────────────────────────────────────────────────────────

  // Pins at rest, before Play and when the replay ends; cards from the app's
  // own visibility rules while it plays. A photo opens only when the replay
  // is not running.
  function showProblem(message: string) {
    els.error.textContent = message
    els.error.hidden = false
  }
  const signLayer: SignLayer | null = plan.signs.length > 0
    ? createSignLayer({ doc, root, stage: part('stage'), map, lib, id, signs: plan.signs, still: reduced, onProblem: showProblem })
    : null
  signLayer?.update({ cards: [], pins: true, canOpenPhoto: true })
  let lastRun = 0
  function showSigns(cardsOn: boolean) {
    if (!signLayer) return
    signLayer.update({
      cards: cardsOn ? visibleSigns(plan.signs, lastRun, plan.wallAt) : [],
      pins: !cardsOn,
      canOpenPhoto: !(cardsOn && !paused),
    })
  }

  // ── the boat ────────────────────────────────────────────────────────────

  const boatEl = doc.createElement('div')
  boatEl.className = 'hcp-boat'
  const boat = new lib.Marker({ element: boatEl })
  let boatShown = false
  let modelLayer: BoatLayer | null = null
  let modelReady = false
  let lastPose: { lat: number; lon: number; headingDeg: number | null } | null = null
  const hull = data.hull ? { type: data.hull.type, loaM: data.hull.loa, beamM: data.hull.beam } : null

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

  map.on('style.load', () => {
    map.setProjection({ type: 'globe' })
    map.addSource(data.map.seamarks.id, {
      type: 'raster', tiles: [data.map.seamarks.tiles], tileSize: 256, attribution: data.map.seamarks.attribution,
    })
    map.addLayer({ id: data.map.seamarks.layerId, type: 'raster', source: data.map.seamarks.id, paint: { 'raster-opacity': 0.85 } })
    if (data.d3) addTerrain()
    map.addSource(data.map.trackSource, { type: 'geojson', data: fullGeo() })
    map.addSource(data.map.doneSource, { type: 'geojson', data: { type: 'FeatureCollection', features: [] } })
    for (const layer of data.map.layers) map.addLayer(layer as MapLibre.LayerSpecification)
    loaded = true
    addModel()
    if (wantStart) intro()
  })
  map.on('error', (e: { error?: Error; sourceId?: string }) => {
    if (e && e.sourceId === TERRAIN_SOURCE_ID) { terrainFailed(e.error); return }
    if (e && e.error && !loaded) {
      els.error.textContent = MAP_ERROR
      els.error.hidden = false
    }
  })

  // ── what is drawn ───────────────────────────────────────────────────────

  function fullGeo(): GeoJSON.FeatureCollection {
    const features: GeoJSON.Feature[] = []
    data.passages.forEach((p, i) => {
      p.l.forEach((leg, k) => {
        for (const seg of exportLegSegments(plan.segments[i], p.l, k)) {
          if (seg.length < 2) continue
          features.push({ type: 'Feature', properties: { c: leg.c }, geometry: { type: 'LineString', coordinates: seg.map((pt) => [pt.lon, pt.lat]) } })
        }
      })
    })
    return { type: 'FeatureCollection', features }
  }

  // A leg's track is cut once and kept, so the sailed cache can tell it is unchanged.
  const legCache = new Map<string, TimedSegment[]>()
  function legSegs(i: number, k: number): TimedSegment[] {
    const key = `${i}-${k}`
    let hit = legCache.get(key)
    if (!hit) { hit = exportLegSegments(plan.segments[i], data.passages[i].l, k); legCache.set(key, hit) }
    return hit
  }

  // The part of every passage the boat has covered, one MultiLineString each.
  const sailed = createSailedCache()
  function geoFor(s: CardState): GeoJSON.FeatureCollection {
    const features: GeoJSON.Feature[] = []
    for (let i = 0; i <= s.index; i++) {
      const p = data.passages[i]
      const through = i < s.index || s.night || s.done ? p.b : s.t
      // Leg by leg, so each part is drawn in its own colour; the legs not yet
      // reached add nothing.
      p.l.forEach((leg, k) => {
        if (p.l.length > 1 && through <= leg.a) return
        const part = sailed.get(`${i}-${k}`, legSegs(i, k), through)
        if (part.geometry.coordinates.length > 0) features.push({ type: 'Feature', properties: { c: leg.c }, geometry: part.geometry })
      })
    }
    return { type: 'FeatureCollection', features }
  }

  function dimTrack(on: boolean) {
    if (!loaded) return
    map.setPaintProperty('hcp-track-casing', 'line-opacity', on ? 0.3 : 0.95)
    map.setPaintProperty('hcp-track-line', 'line-opacity', on ? 0.3 : 1)
  }

  function setMode(mode: string, label: string) {
    root.className = `hcp hcp-${mode}`
    els.label.textContent = label
  }

  const wantHeading = () => (data.d3 && hull !== null) || plan.shots.length > 0
  let lastHeading: HeadingState | null = null
  // The raw track heading twitches as its chord crosses fixes; the model and
  // the camera both take this eased one, stepped in watched seconds.
  const smoother = new HeadingSmoother()
  let smoothedHeading: number | null = null

  function showState(s: CardState) {
    if (s.pos) {
      boat.setLngLat(s.pos)
      if (!boatShown) { boat.addTo(map); boatShown = true }
    }
    if (wantHeading() && s.pos) {
      const at = s.night ? data.passages[s.index].b : s.t
      const sameLeg = lastHeading !== null && lastHeading.id === String(s.index)
      lastHeading = nextHeading(lastHeading, { id: String(s.index), at }, plan.segments[s.index])
      smoothedHeading = smoother.step(lastHeading.heading, performance.now(), { snap: reduced || !sameLeg })
      lastPose = { lat: s.pos[1], lon: s.pos[0], headingDeg: smoothedHeading }
      if (modelReady && modelLayer) modelLayer.setPose(lastPose)
    }
    els.passage.textContent = s.passage
    els.time.textContent = fmtLocal(s.t)
    const wasNight = !els.caption.hidden
    els.caption.hidden = !s.night
    els.caption.textContent = s.night ? `Night ${s.night.n}${s.night.place ? ` · ${s.night.place}` : ''}` : ''
    if (wasNight !== !!s.night) liftInset()
    const src = map.getSource(data.map.doneSource) as MapLibre.GeoJSONSource | undefined
    if (src) src.setData(geoFor(s))
    lastRun = s.run
    showSigns(true)
  }

  function makeInset() {
    if (!data.inset || insetMap) return
    els.inset.hidden = false
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
  // readout has (a night caption makes it taller). The gap is the distance
  // from the stage's bottom edge to the readout's top, plus a margin; unset
  // while the readout is hidden, so the stylesheet's own offset applies.
  function liftInset() {
    if (els.readout.hidden) { root.style.removeProperty('--hcp-ro-gap'); return }
    root.style.setProperty('--hcp-ro-gap', `${part('stage').clientHeight - els.readout.offsetTop + 8}px`)
  }
  doc.defaultView?.addEventListener('resize', liftInset)
  if (typeof ResizeObserver !== 'undefined') new ResizeObserver(liftInset).observe(els.readout)

  let insetMap: MapLibre.Map | null = null

  // ── the camera ──────────────────────────────────────────────────────────

  const readCamera = (): Camera => {
    const c = map.getCenter()
    return { center: [c.lng, c.lat], zoom: map.getZoom(), pitch: map.getPitch(), bearing: map.getBearing() }
  }

  // The whole track framed: MapLibre's own fit when flat; pitched, the zoom is
  // backed off until every corner is inside the padding.
  let overviewKey = ''
  let overviewCache: Camera | null = null
  function overviewCamera(): Camera | null {
    const c = map.getContainer()
    const key = `${c.clientWidth}x${c.clientHeight}|${flat() ? 0 : 1}`
    if (overviewCache && key === overviewKey) return overviewCache
    const fit = map.cameraForBounds(bounds, { padding, maxZoom: 14 })
    if (!fit || !fit.center) return null
    const fc = fit.center as MapLibre.LngLat | [number, number]
    const [lon, lat] = Array.isArray(fc) ? fc : [fc.lng, fc.lat]
    const zoom = fit.zoom ?? 0
    const camera: Camera = flat()
      ? { center: [lon, lat], zoom, pitch: 0, bearing: 0 }
      : {
        center: [lon, lat],
        zoom: fitZoomForPitch({ bounds: data.bounds, centre: { lon, lat }, zoom, pitch: REPLAY_3D_PITCH, width: c.clientWidth, height: c.clientHeight, padding }),
        pitch: REPLAY_3D_PITCH,
        bearing: 0,
      }
    overviewKey = key
    overviewCache = camera
    return camera
  }

  // Shots. A person taking hold of the map (a drag, a wheel turn, a pinch)
  // takes the camera until the next shot; a tap does not.
  const director = new CameraDirector()
  const gate = new CameraGate()
  let resume = 0
  // The camera's own jumps fire movestart and interrupt a drag in progress,
  // so a drag is read from the pointer itself, as the app's map does: a mouse
  // or pen moving past a few pixels, a second finger down, or the wheel (with
  // Ctrl or Command, which is what zooms a card with cooperative gestures). A
  // single finger is left to the page: it scrolls rather than moves the map.
  const down = new Map<number, { x: number; y: number; touch: boolean }>()
  const surface = map.getCanvasContainer()
  surface.addEventListener('pointerdown', (e) => {
    down.set(e.pointerId ?? 0, { x: e.clientX, y: e.clientY, touch: e.pointerType === 'touch' })
    if (down.size > 1) gate.yield()
  })
  surface.addEventListener('pointermove', (e) => {
    const start = down.get(e.pointerId ?? 0)
    if (start && !start.touch && Math.hypot(e.clientX - start.x, e.clientY - start.y) > DRAG_YIELD_PX) gate.yield()
  })
  const lift = (e: PointerEvent) => { down.delete(e.pointerId ?? 0) }
  surface.addEventListener('pointerup', lift)
  surface.addEventListener('pointercancel', lift)
  surface.addEventListener('wheel', (e) => { if (e.ctrlKey || e.metaKey) gate.yield() }, { passive: true })
  map.on('movestart', (e: { originalEvent?: unknown }) => {
    // The camera's own jumps carry no originalEvent.
    if (e.originalEvent) gate.yield()
  })

  function placeCamera(s: CardState, nowMs: number) {
    if (plan.shots.length === 0 || !s.pos) return
    const overview = overviewCamera()
    if (!overview) return
    const current = readCamera()
    const heading = wantHeading() && lastHeading ? smoothedHeading : null
    const result = director.frame(
      {
        overview,
        flat: flat(),
        shotStart: (rs) => shotStartState(plan.timeline, rs),
        elapsedAt: (rs, clock) => plan.wallAt(clock) - plan.wallAt(rs.clock),
      },
      { resolved: plan.shots, clock: s.clock, playing: true, nowMs, reduced, resume, boat: { lat: s.pos[1], lon: s.pos[0], heading }, current },
    )
    if (!result) return
    const place = gate.place({
      result, resume, reduced, current,
      boatOutside: () => {
        const c = map.getContainer()
        return s.pos ? outsideCentral(map.project(s.pos), c.clientWidth, c.clientHeight) : false
      },
    })
    if (place) map.jumpTo(place)
  }

  // ── playing ─────────────────────────────────────────────────────────────

  let clock = 0
  let playing = false
  let paused = false
  let started = false
  let raf = 0
  let introTimer: ReturnType<typeof setTimeout> | undefined
  let lastTick = 0

  function frame(now: number) {
    if (!playing) return
    // A stalled frame (a background tab) is not eased across.
    if (now - lastTick > 250) smoother.hold(performance.now())
    if (!paused) {
      clock = Math.min(plan.total, clock + (now - lastTick) / 1000)
      const s = plan.stateAt(clock)
      showState(s)
      placeCamera(s, now)
      if (s.done) { finish(); return }
    }
    lastTick = now
    raf = requestAnimationFrame(frame)
  }

  function finish() {
    playing = false
    paused = false
    dimTrack(false)
    setMode('done', 'Replay')
    showSigns(false)
  }

  // Each press of Play starts a run with its own token. The fly-in's listener
  // and timer belong to one run, are removed when a new one starts, and cannot
  // start a newer run if one slips through.
  let run = 0
  let flying = false
  let pendingMoveEnd: (() => void) | null = null

  function begin(token: number) {
    if (token !== run || started) return
    flying = false
    started = true
    clock = 0
    lastHeading = null
    smoother.hold(performance.now())
    playing = true
    paused = false
    setMode('playing', 'Pause')
    els.readout.hidden = false
    liftInset()
    makeInset()
    showState(plan.stateAt(0))
    lastTick = performance.now()
    raf = requestAnimationFrame(frame)
  }

  function clearPending() {
    if (pendingMoveEnd) map.off('moveend', pendingMoveEnd)
    pendingMoveEnd = null
    clearTimeout(introTimer)
  }

  // Comes down onto a framing (null: MapLibre's own flat fit) and begins the
  // replay when the camera lands, or after five seconds whatever happens.
  function arrive(view: Camera | null, duration: number) {
    clearPending()
    const token = run
    const land = () => { clearPending(); begin(token) }
    pendingMoveEnd = land
    map.on('moveend', land)
    if (view) map.easeTo({ ...view, duration })
    else map.fitBounds(bounds, { padding, maxZoom: 14, duration })
    introTimer = setTimeout(() => { clearPending(); begin(token) }, 5000)
  }

  // Start high over the globe so a reader who does not know the coast sees
  // where in the world this is, then come down onto the track.
  function intro() {
    clearPending()
    run++
    started = false
    flying = true
    wantStart = false
    dimTrack(true)
    const done = map.getSource(data.map.doneSource) as MapLibre.GeoJSONSource
    done.setData({ type: 'FeatureCollection', features: [] })
    setMode('flying', 'Play')
    showSigns(false)
    const overview = flat() ? null : overviewCamera()
    if (reduced) {
      if (overview) map.jumpTo(overview)
      else map.fitBounds(bounds, { padding, maxZoom: 14, duration: 0 })
      begin(run)
      return
    }
    map.jumpTo({ center: centre, zoom: 1.5, bearing: 0, pitch: 0 })
    arrive(overview, FLY_MS)
  }

  els.btn.addEventListener('click', () => {
    if (playing) {
      paused = !paused
      els.label.textContent = paused ? 'Resume' : 'Pause'
      root.classList.toggle('hcp-paused', paused)
      showSigns(true)
      lastTick = performance.now()
      smoother.hold(lastTick)
      if (!paused) resume++
      return
    }
    resume++
    cancelAnimationFrame(raf)
    if (loaded) intro()
    else { wantStart = true; setMode('flying', 'Play') }
  })

  return api
}
