import { useCallback, useEffect, useRef, useState, type RefObject } from 'react'
import type { MapRef } from 'react-map-gl/maplibre'

import { subscribeRadarEchoStatus, subscribeRadarSpokes, type RadarEchoStatus } from '@/hooks/use-radar-echo-stream'
import {
  clearEchoBuffer,
  createEchoBuffer,
  expireStaleSpokes,
  ingestSpoke,
  RADAR_ECHO_SPOKE_MAX_AGE_MS,
  type EchoBuffer,
} from '@/lib/radar-echo/echo-buffer'
import { createEchoCanvas, RADAR_ECHO_CANVAS_PX, type EchoCanvas } from '@/lib/radar-echo/echo-canvas'
import { haversineMeters } from '@/lib/geo'
import { echoCornerCoordinates, getEchoLut } from '@/lib/radar-echo/geometry'
import type { RadarCapabilities } from '@/lib/radar-echo/legend'
import { decodeRadarMessage } from '@/lib/radar-echo/spoke-message'

/**
 * Owns every imperative piece of the radar picture overlay: the offscreen
 * canvas, the echo buffer, the spoke subscription, the throttled paint
 * loop, and the raw MapLibre CanvasSource/raster layer. react-map-gl's
 * declarative <Source>/<Layer> can't express a canvas source at all -- its
 * SourceSpecification union excludes CanvasSourceSpecification, so that
 * would fail at typecheck -- and the paint loop has to run well above
 * React's render cadence without ever going through setState (a picture
 * updates 2.4 frames/s at 256 spokes/frame in steady state; routing that
 * through React state would mean a re-render per frame for a layer that
 * has nothing declarative about it). Every mutable piece here -- the
 * canvas, the buffer, the decode state, the dirty flag -- lives in a ref.
 *
 * The only two things this hook ever calls setState for are `status`,
 * which changes on the order of seconds (a connection opening or
 * dropping), not frames -- exactly the kind of external-store signal
 * subscribeRadarEchoStatus was built to drive a status readout from -- and
 * nothing else. The paint path (handleFrame, tick) touches only refs.
 */

// Measured (backend/testdata/mayara/spokes-fur6424A.md): every captured
// spoke is exactly 1024 bytes despite hasSparseSpokes:true in the
// capabilities, so there is no resampling and this is a hard constant, not
// derived from anything in `capabilities`.
const RADAR_ECHO_BIN_COUNT = 1024

export const RADAR_ECHO_SOURCE_ID = 'radar-echo'
export const RADAR_ECHO_LAYER_ID = 'radar-echo-layer'

// Seamark symbols (buoys, lights, beacons -- the openseamap-layer raster)
// stay legible over the live sweep, which is why this anchors immediately
// below it rather than above. openseamap-layer itself anchors below
// alarm-circle-fill (anchor-watch-map.tsx), so transitively the echo
// picture also sits below every vector overlay -- the alarm circle,
// trails, AIS and radar-target markers -- without this layer needing to
// chase that anchor directly. It still mounts above world-imagery, which
// shares openseamap's alarm-circle-fill anchor but was added earlier in
// JSX mount order, so the live radar return is visible over satellite
// imagery instead of buried beneath it.
export const RADAR_ECHO_BEFORE_ID = 'openseamap-layer'

export const RADAR_ECHO_MAX_FPS = 8
const RADAR_ECHO_FRAME_INTERVAL_MS = 1000 / RADAR_ECHO_MAX_FPS

/**
 * The largest power-of-two `step` such that no observed value in `orAcc`
 * (an OR-reduction of every bearing seen so far) has ever set one of its
 * low `log2(step)` bits. Every bearing on this wire is a slot in a
 * power-of-two-sized circular counter (spokesPerRevolution, 8192 on the
 * boat's Furuno), so "is bit K always clear across everything observed" is
 * exactly "is the true step a multiple of 2^(K+1)" -- a plain OR
 * accumulator answers it in O(1) per spoke, no history retained.
 *
 * This is the mechanism behind "derive the spoke count from the observed
 * angle step, never from the advertised number"
 * (backend/testdata/mayara/spokes-fur6424A.md): capabilities.spokesPerRevolution
 * reads 8192 on this radar but only 4096 distinct bearings are ever sent,
 * every one even. Seeded from a single spoke this can overestimate the
 * step (e.g. a first bearing of 4 looks like step 4 until a bearing with
 * bit 1 set arrives), but it can never underestimate one -- more evidence
 * only clears more bits, so the buffer/LUT resize this drives in
 * handleFrame below is self-correcting downward, never wrong in the
 * direction that would misplace a spoke.
 */
function derivedAngleStep(orAcc: number, maxStep: number): number {
  if (orAcc === 0) return 1
  let step = 1
  while (step < maxStep && (orAcc & step) === 0) step <<= 1
  return step
}

export interface EchoFix {
  lat: number
  lon: number
}

export interface EchoCentre {
  lat: number
  lon: number
  // How far the radar's own reported fix sits from own ship, or null when the
  // radar reported none. Surfaced rather than silently ignored: a large,
  // persistent value means the radar's fix has gone stale and is worth seeing.
  radarFixDivergenceM: number | null
}

// Where to centre the radar picture.
//
// Own ship, not the radar's own reported fix, and that is a correction.
// Phase 0 measured Spoke.lat/lon populated on every spoke and concluded the
// antenna offset was solved for free. Populated turned out not to mean live:
// sampled three times over 36 s while making way, the radar's fix was
// byte-identical every time while own ship moved, because mayara captures it
// once and never refreshes it. Centring on it put the overlay 12.5 km away
// and off the visible map, with the layer, source, palette and 26k drawn
// pixels all perfectly correct.
//
// The antenna offset is metres. Trusting a frozen fix is kilometres and grows
// without bound. So own ship wins, and the divergence is reported rather than
// buried, following logRadarProjectionMismatch in backend/radar_targets.go:
// surface the disagreement, never silently pick one and hope.
export function resolveEchoCentre(ownShip: EchoFix | null, radarFix: EchoFix | null): EchoCentre | null {
  if (!ownShip) return null
  const divergence =
    radarFix === null
      ? null
      : haversineMeters(ownShip.lat, ownShip.lon, radarFix.lat, radarFix.lon)
  return { lat: ownShip.lat, lon: ownShip.lon, radarFixDivergenceM: divergence }
}

// Beyond this the radar's reported fix is not plausibly an antenna offset and
// is reported once. Ten metres of offset is expected; kilometres is a frozen
// or wrong fix.
export const RADAR_ECHO_FIX_DIVERGENCE_WARN_M = 500

export interface UseRadarEchoLayerParams {
  mapRef: RefObject<MapRef | null>
  // Own ship, which is what the picture is centred on. See resolveEchoCentre.
  vesselLat: number | null
  vesselLon: number | null
  // Null when there is nothing to show (no primary radar, or it's not
  // transmitting) -- see resolveRadarEchoAvailability in anchor-watch-map.tsx,
  // which gates this before it ever reaches here.
  radarId: string | null
  enabled: boolean
  capabilities: RadarCapabilities | null
  palette: Uint32Array | null
}

export interface UseRadarEchoLayerResult {
  /** For the toggle's title text -- see resolveRadarEchoAvailability. */
  status: RadarEchoStatus
  /** Wire into <Map onLoad>. */
  handleMapLoad: () => void
  /** Wire into <Map onStyleData>, alongside any other onStyleData handler. */
  handleStyleData: () => void
}

export function useRadarEchoLayer({
  mapRef,
  vesselLat,
  vesselLon,
  radarId,
  enabled,
  capabilities,
  palette,
}: UseRadarEchoLayerParams): UseRadarEchoLayerResult {
  const [status, setStatus] = useState<RadarEchoStatus>('idle')

  // Everything the paint loop and the frame listener touch lives here, not
  // in React state -- see the module doc above. Survives across renders;
  // reset to a fresh set of values by the setup effect below whenever
  // enabled/radarId/capabilities/palette actually change.
  const canvasRef = useRef<EchoCanvas | null>(null)
  const bufferRef = useRef<EchoBuffer | null>(null)
  const spokeCountRef = useRef(0)
  const angleOrAccRef = useRef(0)
  const angleMaskRef = useRef(0)
  const dirtyRef = useRef(false)
  const lastPaintAtRef = useRef(-Infinity)
  const rafIdRef = useRef<number | null>(null)
  // The radar's own reported fix, kept only to cross-check against own ship.
  // It is NOT what the canvas is centred on: see resolveEchoCentre above for
  // the measurement that ruled that out.
  const lastRadarLatRef = useRef<number | null>(null)
  const lastRadarLonRef = useRef<number | null>(null)
  // Own ship, mirrored into a ref so the rAF paint loop can read it without
  // being torn down and rebuilt on every position update.
  const vesselLatRef = useRef<number | null>(vesselLat)
  const vesselLonRef = useRef<number | null>(vesselLon)
  vesselLatRef.current = vesselLat
  vesselLonRef.current = vesselLon
  // One warning per enable, not per frame, when the radar's own fix has
  // drifted implausibly far from own ship.
  const warnedFixDivergenceRef = useRef(false)
  // What the map source's coordinates were last set to, so the paint loop
  // only calls setCoordinates on an actual change rather than every tick.
  const lastAppliedLatRef = useRef<number | null>(null)
  const lastAppliedLonRef = useRef<number | null>(null)
  const lastAppliedRangeRef = useRef<number | null>(null)
  // Mirrors `enabled`, readable from handleMapLoad/handleStyleData without
  // making them depend on (and be recreated by) the enabled prop directly.
  const enabledRef = useRef(enabled)
  useEffect(() => {
    enabledRef.current = enabled
  }, [enabled])

  // Status is a module-level signal (use-radar-echo-stream.ts: "at most one
  // radar is ever overlaid at a time"), independent of whether *this*
  // mounted map instance is the one holding the subscription -- tile and
  // drawer can both want it (the plan's "Two mounted maps"). Always
  // listening, never itself opening a connection.
  useEffect(() => subscribeRadarEchoStatus(setStatus), [])

  // The most recent maplibregl.Map instance seen through mapRef, kept
  // independent of mapRef.current itself: React detaches a child's ref
  // (the <Map ref={mapRef}> below) while unmounting that child fiber,
  // which happens *before* this hook's own effect cleanup runs on the
  // parent fiber -- by teardown time mapRef.current is already null even
  // though the real Map instance it pointed to is still perfectly valid to
  // call removeLayer/removeSource on. Updated by every read through
  // getCurrentMap() below, so teardown always has a usable reference
  // regardless of exactly when the ref itself got cleared.
  const lastMapRef = useRef<ReturnType<NonNullable<MapRef['getMap']>> | undefined>(undefined)
  const getCurrentMap = useCallback(() => {
    const map = mapRef.current?.getMap()
    if (map) lastMapRef.current = map
    return map
  }, [mapRef])

  const addOrMoveLayer = useCallback((map: ReturnType<NonNullable<MapRef['getMap']>>) => {
    const canvas = canvasRef.current
    const buffer = bufferRef.current
    if (!canvas || !buffer) return
    const radarFix =
      lastRadarLatRef.current !== null && lastRadarLonRef.current !== null
        ? { lat: lastRadarLatRef.current, lon: lastRadarLonRef.current }
        : null
    const ownShip =
      vesselLatRef.current !== null && vesselLonRef.current !== null
        ? { lat: vesselLatRef.current, lon: vesselLonRef.current }
        : null
    const centre = resolveEchoCentre(ownShip, radarFix)
    const rangeM = buffer.rangeM
    // Nothing truthful to place the canvas at yet: no own-ship fix, or no
    // spoke carrying a range. Wait rather than guess at coordinates.
    if (centre === null || rangeM <= 0) return
    const lat = centre.lat
    const lon = centre.lon

    if (
      !warnedFixDivergenceRef.current &&
      centre.radarFixDivergenceM !== null &&
      centre.radarFixDivergenceM > RADAR_ECHO_FIX_DIVERGENCE_WARN_M
    ) {
      warnedFixDivergenceRef.current = true
      console.warn(
        `radar echo: the radar's reported fix is ${Math.round(centre.radarFixDivergenceM)}m from own ship, ` +
          `far beyond any antenna offset. Centring on own ship. mayara captures Spoke.lat/lon once and may ` +
          `not refresh it; see backend/testdata/mayara/spokes-fur6424A.md.`,
      )
    }

    if (!map.getSource(RADAR_ECHO_SOURCE_ID)) {
      if (!map.isStyleLoaded()) return
      map.addSource(RADAR_ECHO_SOURCE_ID, {
        type: 'canvas',
        canvas: canvas.element,
        coordinates: echoCornerCoordinates(lat, lon, rangeM),
        animate: true,
      })
      const beforeId = map.getLayer(RADAR_ECHO_BEFORE_ID) ? RADAR_ECHO_BEFORE_ID : undefined
      map.addLayer(
        {
          id: RADAR_ECHO_LAYER_ID,
          type: 'raster',
          source: RADAR_ECHO_SOURCE_ID,
          paint: { 'raster-opacity': 0.75, 'raster-fade-duration': 0 },
        },
        beforeId,
      )
      lastAppliedLatRef.current = lat
      lastAppliedLonRef.current = lon
      lastAppliedRangeRef.current = rangeM
    } else if (lat !== lastAppliedLatRef.current || lon !== lastAppliedLonRef.current || rangeM !== lastAppliedRangeRef.current) {
      const source = map.getSource(RADAR_ECHO_SOURCE_ID)
      // CanvasSource per maplibre-gl's own type -- getSource's return type
      // is the generic Source base, which doesn't carry setCoordinates.
      ;(source as unknown as { setCoordinates: (c: ReturnType<typeof echoCornerCoordinates>) => void }).setCoordinates(
        echoCornerCoordinates(lat, lon, rangeM),
      )
      lastAppliedLatRef.current = lat
      lastAppliedLonRef.current = lon
      lastAppliedRangeRef.current = rangeM
    }

    // Re-assert order every time this runs, not only on first mount: a
    // layer added mid-session (e.g. right after the map first loads, or
    // right after a style reload re-created openseamap-layer) otherwise
    // lands on top of everything react-map-gl's addLayer calls have no
    // awareness of. See the comment at RADAR_ECHO_BEFORE_ID's declaration
    // and the identical pattern this mirrors in anchor-watch-map.tsx.
    if (map.getLayer(RADAR_ECHO_LAYER_ID) && map.getLayer(RADAR_ECHO_BEFORE_ID)) {
      map.moveLayer(RADAR_ECHO_LAYER_ID, RADAR_ECHO_BEFORE_ID)
    }
  }, [])

  const removeLayer = useCallback((map: ReturnType<NonNullable<MapRef['getMap']>> | undefined) => {
    if (!map) return
    // Layer before source: removing a source out from under a live layer
    // throws in maplibre-gl.
    if (map.getLayer(RADAR_ECHO_LAYER_ID)) map.removeLayer(RADAR_ECHO_LAYER_ID)
    if (map.getSource(RADAR_ECHO_SOURCE_ID)) map.removeSource(RADAR_ECHO_SOURCE_ID)
  }, [])

  const handleMapLoad = useCallback(() => {
    if (!enabledRef.current) return
    const map = getCurrentMap()
    if (map) addOrMoveLayer(map)
  }, [getCurrentMap, addOrMoveLayer])

  const handleStyleData = useCallback(() => {
    if (!enabledRef.current) return
    const map = getCurrentMap()
    if (map && map.isStyleLoaded()) addOrMoveLayer(map)
  }, [getCurrentMap, addOrMoveLayer])

  useEffect(() => {
    if (!enabled || radarId === null || capabilities === null || palette === null) {
      return
    }

    // Fresh state for this radar/capabilities pair. A prior overlay (a
    // different radar, or the same one after capabilities re-resolved)
    // must not leak its buffer, its discovered spoke count, or its last
    // painted position into this one.
    canvasRef.current = createEchoCanvas(RADAR_ECHO_CANVAS_PX)
    spokeCountRef.current = capabilities.spokesPerRevolution
    angleOrAccRef.current = 0
    angleMaskRef.current = 0
    bufferRef.current = createEchoBuffer(spokeCountRef.current, RADAR_ECHO_BIN_COUNT)
    lastRadarLatRef.current = null
    lastRadarLonRef.current = null
    lastAppliedLatRef.current = null
    lastAppliedLonRef.current = null
    lastAppliedRangeRef.current = null
    warnedFixDivergenceRef.current = false
    dirtyRef.current = false
    lastPaintAtRef.current = -Infinity

    const ensureBufferSize = (spokeCount: number): EchoBuffer => {
      if (bufferRef.current === null || spokeCountRef.current !== spokeCount) {
        const previousRangeM = bufferRef.current?.rangeM ?? 0
        spokeCountRef.current = spokeCount
        bufferRef.current = createEchoBuffer(spokeCount, RADAR_ECHO_BIN_COUNT)
        bufferRef.current.rangeM = previousRangeM
      }
      return bufferRef.current
    }

    const handleFrame = (frame: ArrayBuffer) => {
      const spokes = decodeRadarMessage(frame, angleMaskRef.current)
      if (spokes.length === 0) return
      const nowMs = Date.now()

      for (const spoke of spokes) {
        // North-up rendering is drawn from `bearing` (true north), never
        // `angle` (bow-relative) -- see geometry.ts's LUT and the measured
        // facts doc. A spoke with no bearing carries nothing this overlay
        // can place, so it's skipped rather than assigned a fabricated row.
        if (spoke.bearing === null) continue

        angleOrAccRef.current |= spoke.bearing
        const step = derivedAngleStep(angleOrAccRef.current, capabilities.spokesPerRevolution)
        const spokeCount = Math.max(1, Math.round(capabilities.spokesPerRevolution / step))
        angleMaskRef.current = step - 1
        const buffer = ensureBufferSize(spokeCount)

        if (spoke.rangeM !== buffer.rangeM) {
          // Spoke.range is the range to this spoke's last pixel, not the
          // radar's range control (measured: range=825 while the range
          // control read 463) -- the canvas is sized from it, so a change
          // means every existing pixel's metres-per-bin mapping is now
          // wrong. Clearing immediately, rather than waiting for the next
          // revolution to overwrite it, is the same discipline
          // RADAR_ECHO_SPOKE_MAX_AGE_MS documents: a frozen/stale picture
          // reads as live returns and is worse than no picture.
          clearEchoBuffer(buffer)
          buffer.rangeM = spoke.rangeM
        }

        const row = Math.floor(spoke.bearing / step) % spokeCount
        ingestSpoke(buffer, spoke, row, nowMs)

        if (spoke.lat !== null && spoke.lon !== null) {
          lastRadarLatRef.current = spoke.lat
          lastRadarLonRef.current = spoke.lon
        }
      }
      dirtyRef.current = true
    }

    const unsubscribeFrame = subscribeRadarSpokes(radarId, handleFrame)

    // Socket close/reconnect clears immediately, same reasoning as a range
    // change above: a still-displayed picture from before the drop reads as
    // live returns. Independent of the module-level `status` state above --
    // this always fires for a real transition, that's merely what a status
    // readout observes.
    const unsubscribeStatus = subscribeRadarEchoStatus((s) => {
      if (s === 'reconnecting' || s === 'disconnected') {
        const buffer = bufferRef.current
        if (buffer) {
          clearEchoBuffer(buffer)
          dirtyRef.current = true
        }
      }
    })

    const tick = (nowMs: number) => {
      rafIdRef.current = requestAnimationFrame(tick)
      if (nowMs - lastPaintAtRef.current < RADAR_ECHO_FRAME_INTERVAL_MS) return

      const buffer = bufferRef.current
      const canvas = canvasRef.current
      if (!buffer || !canvas) return

      const expired = expireStaleSpokes(buffer, Date.now(), RADAR_ECHO_SPOKE_MAX_AGE_MS)
      if (!dirtyRef.current && expired === 0) return // nothing arrived, nothing expired: nothing to do

      lastPaintAtRef.current = nowMs
      const lut = getEchoLut(RADAR_ECHO_CANVAS_PX, spokeCountRef.current, RADAR_ECHO_BIN_COUNT)
      canvas.render(buffer, lut, palette)
      dirtyRef.current = false

      const map = getCurrentMap()
      if (map) addOrMoveLayer(map)
    }
    rafIdRef.current = requestAnimationFrame(tick)

    return () => {
      if (rafIdRef.current !== null) cancelAnimationFrame(rafIdRef.current)
      rafIdRef.current = null
      unsubscribeFrame()
      unsubscribeStatus()
      // Not getCurrentMap()/mapRef.current here on purpose -- see
      // lastMapRef's own comment above. By the time this cleanup runs
      // (parent-fiber effect cleanup, which React runs after detaching a
      // child fiber's ref such as <Map ref={mapRef}>) mapRef.current may
      // already be null even though the underlying Map instance is still
      // live and still needs its layer/source removed.
      removeLayer(lastMapRef.current)
      canvasRef.current = null
      bufferRef.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- getCurrentMap/addOrMoveLayer/removeLayer are stable (refs and useCallback with fixed deps)
  }, [enabled, radarId, capabilities, palette])

  return { status, handleMapLoad, handleStyleData }
}
