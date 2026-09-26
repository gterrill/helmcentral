import 'maplibre-gl/dist/maplibre-gl.css'
import maplibregl from 'maplibre-gl'
import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from 'react'
import type { MapRef } from 'react-map-gl/maplibre'
import { Map, Marker, Source, Layer } from 'react-map-gl/maplibre'
import { Anchor, ArrowUp, Crosshair, Expand, MapPin, Minus, Move, Plus, Radar, Satellite, Ship, X } from 'lucide-react'
import type { AnchorPlacemark } from '@/hooks/use-anchor-placemarks'
import { cn } from '@/lib/utils'
import { haversineMeters, bearingDeg, destinationPoint } from '@/lib/geo'
import { ALARM_STATES, type AlarmState } from '@/hooks/use-alarms'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { RadarInfo, RadarSource, RadarTarget } from '@/hooks/use-radar-targets'
import type { TrailPoint } from '@/hooks/use-server-trails'
import type { RodeMethodResult } from '@/lib/rode-plan'
import { MapPlaceLabels, warnIfBaseVectorSourceMissing } from '@/components/map-place-labels'
import { useCollapsedMapAttribution } from '@/hooks/use-collapsed-map-attribution'
import { useRadarCapabilities } from '@/hooks/use-radar-capabilities'
import { useRadarEchoLayer } from '@/hooks/use-radar-echo-layer'
import type { RadarEchoStatus } from '@/hooks/use-radar-echo-stream'
import { STYLE_LIGHT, STYLE_DARK, OPENSEAMAP_TILES } from '@/lib/basemap'
import { hasWebGL2 } from '@/lib/webgl'
import { ANCHOR_VIEW_MAX_ZOOM, ANCHOR_VIEW_MIN_ZOOM, fitRadiusZoom } from '@/lib/anchor-view'
import { VesselArrow } from '@/components/vessel-arrow-marker'
import {
  ADJUST_MAX_ZOOM,
  adjustZoomBounds,
  anchorMoveOffset,
  clampRadiusM,
  formatMovedLabel,
  MIN_ALARM_RADIUS_M,
  metersPerPixel,
  radiusForZoom,
  radiusStepM,
  ringRadiusPx,
  snapRadiusM,
  zoomForRingRadius,
  type AlarmRadiusBounds,
} from '@/lib/anchor-adjust'
import {
  resolveMarkerLabelSuppression,
  markerScaleForZoom,
  LABEL_COLLISION_RADIUS_PX,
  type MarkerLabelPoint,
  type ScreenRect,
} from '@/lib/marker-labels'

const WORLD_IMAGERY_TILES = '/api/world-imagery/{z}/{x}/{y}'
const SAT_HANDOFF_START_ZOOM = 9
const SAT_HANDOFF_END_ZOOM = 10
const WORLD_IMAGERY_MAX_ZOOM = 18
const ANCHOR_WATCH_ZOOM_STORAGE_KEY = 'anchor-watch-map-zoom'
const ANCHOR_WATCH_CENTER_STORAGE_KEY = 'anchor-watch-map-center'
const AIS_TRAIL_MAX_AGE_MS = 24 * 60 * 60 * 1000
// Threshold above which the current is judged strong enough to visually call out.
const HIGH_DRIFT_IMPACT_KTS = 1.5
// Provisional zoom for the one frame before the fit-to-container effect can
// measure the map's real rendered size (fitRadiusZoom needs it, and it isn't
// known until after mount) — matches the zoom most existing anchor radii
// already settled near under the old zoomForRadius, so a container that
// can't be measured at all (e.g. no real layout under jsdom in tests) is no
// worse off than before. A stored zoom that belongs to the current anchor
// session overrides this outright.
const DEFAULT_ZOOM_BEFORE_FIT = 14

// World Imagery (Esri/Maxar aerial photography) fades in between zoom 9-10
// rather than appearing abruptly, since its low-zoom tiles are lower quality.
export function computeWorldImageryOpacity(currentZoom: number, enabled: boolean): number {
  if (!enabled) return 0
  return Math.max(
    0,
    Math.min(1, (currentZoom - SAT_HANDOFF_START_ZOOM) / (SAT_HANDOFF_END_ZOOM - SAT_HANDOFF_START_ZOOM)),
  )
}

export interface RadarEchoAvailability {
  available: boolean
  // Always present alongside `available: false`, so the toggle never
  // silently does nothing (the plan's own requirement) -- a disabled
  // control carries the reason in its title rather than just being greyed
  // out with no explanation.
  reason: string | null
}

/**
 * The radar-echo toggle's precedence chain, checked in the stated order:
 * no mayara address configured, no radar present, radar not transmitting
 * (STANDBY), capabilities unreadable, stream disconnected. Each reason is
 * checked only once the ones before it have already passed, so e.g. an
 * unconfigured integration reports "no mayara radar configured" rather than
 * a confusing "no radar found".
 *
 * The stream-disconnected check is gated on `echoEnabled`: `echoStatus` is
 * a module-level value (use-radar-echo-stream.ts) that settles to
 * 'disconnected' the moment the *last* subscriber releases -- including an
 * ordinary, intentional toggle-off. Without the `echoEnabled` gate, turning
 * the overlay off would leave it permanently unable to be turned back on,
 * since the very act of disabling it sets the status this function would
 * otherwise read as a reason to stay disabled.
 */
export function resolveRadarEchoAvailability(params: {
  radarSource: RadarSource
  radars: RadarInfo[]
  capabilitiesError: string | null
  capabilitiesLoading: boolean
  echoEnabled: boolean
  echoStatus: RadarEchoStatus
}): RadarEchoAvailability {
  if (params.radarSource === 'disabled') {
    return { available: false, reason: 'No mayara radar configured' }
  }
  if (params.radars.length === 0) {
    return { available: false, reason: 'No radar found' }
  }
  if (!params.radars[0].transmitting) {
    return { available: false, reason: 'Radar in STANDBY' }
  }
  if (params.capabilitiesLoading) {
    return { available: false, reason: 'Loading radar capabilities…' }
  }
  if (params.capabilitiesError !== null) {
    return { available: false, reason: params.capabilitiesError }
  }
  if (params.echoEnabled && (params.echoStatus === 'disconnected' || params.echoStatus === 'reconnecting')) {
    return { available: false, reason: 'Radar stream disconnected' }
  }
  return { available: true, reason: null }
}

// AIS label collision avoidance (design critique item 4) now lives in
// lib/marker-labels.ts, shared with the poi-map widget's own marker labels
// (ADR 0091 phase 3b). Re-exported here so existing imports from this module
// keep working unchanged.
export { resolveMarkerLabelSuppression, LABEL_COLLISION_RADIUS_PX }
export type { MarkerLabelPoint, ScreenRect }

// The zoom the operator last left the map at, tagged with the anchor session
// it was made in — same shape and the same reason as StoredCenter below: a
// zoom left over from a different anchorage (a much bigger or smaller swing
// circle) is not a pan worth restoring, it's a stale view of the wrong scale.
interface StoredZoom {
  zoom: number
  sessionId: string | null
}

// Keyed per host (code-review finding): the dashboard tile and the
// fullscreen drawer are very different sizes, so a zoom fit for one is wrong
// for the other. Sharing one key meant whichever host fit first "poisoned"
// the stored zoom for the other, which then read it back as already
// belonging to the session and skipped its own fit entirely. viewKey is an
// opaque host id the caller chooses (anchor-watch-tile.tsx passes "tile",
// anchor-watch-drawer.tsx passes "drawer"); the empty string keeps today's
// unscoped key for any caller/test that doesn't pass one.
function zoomStorageKey(viewKey: string): string {
  return viewKey ? `${ANCHOR_WATCH_ZOOM_STORAGE_KEY}.${viewKey}` : ANCHOR_WATCH_ZOOM_STORAGE_KEY
}

function readStoredZoom(viewKey: string): StoredZoom | null {
  if (typeof window === 'undefined') return null
  const raw = window.localStorage.getItem(zoomStorageKey(viewKey))
  if (!raw) return null
  try {
    const parsed = JSON.parse(raw) as { zoom?: unknown; sessionId?: unknown }
    if (typeof parsed.zoom === 'number' && Number.isFinite(parsed.zoom)) {
      return {
        zoom: Math.max(ANCHOR_VIEW_MIN_ZOOM, Math.min(ANCHOR_VIEW_MAX_ZOOM, parsed.zoom)),
        sessionId: typeof parsed.sessionId === 'string' ? parsed.sessionId : null,
      }
    }
  } catch {
    // Ignore JSON parse errors and return null
  }
  return null
}

function writeStoredZoom(zoom: number, sessionId: string | null, viewKey: string): void {
  if (typeof window === 'undefined') return
  window.localStorage.setItem(zoomStorageKey(viewKey), JSON.stringify({ zoom, sessionId }))
}

// The centre the operator last left the map at, tagged with the anchor
// session it was made in. An untagged record (or one written with no watch
// running) carries a null session, which matches no live session and so
// gets re-centred the moment one arrives.
interface StoredCenter {
  latitude: number
  longitude: number
  sessionId: string | null
}

function readStoredCenter(): StoredCenter | null {
  if (typeof window === 'undefined') return null
  const raw = window.localStorage.getItem(ANCHOR_WATCH_CENTER_STORAGE_KEY)
  if (!raw) return null
  try {
    const parsed = JSON.parse(raw) as { latitude?: unknown; longitude?: unknown; sessionId?: unknown }
    if (
      typeof parsed === 'object' &&
      parsed !== null &&
      typeof parsed.latitude === 'number' &&
      Number.isFinite(parsed.latitude) &&
      typeof parsed.longitude === 'number' &&
      Number.isFinite(parsed.longitude) &&
      parsed.latitude >= -90 &&
      parsed.latitude <= 90 &&
      parsed.longitude >= -180 &&
      parsed.longitude <= 180
    ) {
      return {
        latitude: parsed.latitude,
        longitude: parsed.longitude,
        sessionId: typeof parsed.sessionId === 'string' ? parsed.sessionId : null,
      }
    }
  } catch {
    // Ignore JSON parse errors and return null
  }
  return null
}

function writeStoredCenter(latitude: number, longitude: number, sessionId: string | null): void {
  if (typeof window === 'undefined') return
  window.localStorage.setItem(
    ANCHOR_WATCH_CENTER_STORAGE_KEY,
    JSON.stringify({ latitude, longitude, sessionId }),
  )
}

// ── Generate a circle GeoJSON polygon ──────────────────────────────────────
function generateCircleGeoJSON(anchorLon: number, anchorLat: number, radiusM: number): GeoJSON.Feature<GeoJSON.Polygon> {
  const steps = 64
  const coordinates: [number, number][] = []
  for (let i = 0; i <= steps; i++) {
    const bearing = (i / steps) * 2 * Math.PI
    const [lat, lon] = destinationPoint(anchorLat, anchorLon, bearing, radiusM)
    coordinates.push([lon, lat])
  }
  return {
    type: 'Feature',
    geometry: { type: 'Polygon', coordinates: [coordinates] },
    properties: {},
  }
}

// ── Trail to GeoJSON LineString ─────────────────────────────────────────────
function trailToGeoJSON(points: TrailPoint[]): GeoJSON.Feature<GeoJSON.LineString> {
  return {
    type: 'Feature',
    geometry: {
      type: 'LineString',
      coordinates: points.map((p) => [p.lon, p.lat]),
    },
    properties: {},
  }
}

// ── Change detection for the trail GeoJSON refresh tick ────────────────────
// react-map-gl re-uploads a Source's `data` to maplibre whenever the prop's
// *identity* changes, even when the GeoJSON it describes is unchanged - and
// both trail sources below use lineMetrics with line-gradient, one of the
// more expensive things to re-upload for nothing. vesselTrail()/aisTrails()
// read ring buffers behind a stable getter (use-server-trails.ts, not owned
// here), so there's no cheaper signal available than point count plus the
// last point's own timestamp per trail - enough to tell "actually changed"
// from "nothing new arrived this tick" without diffing every point.
function trailSignature(points: TrailPoint[]): string {
  if (points.length === 0) return '0'
  return `${points.length}:${points[points.length - 1].timestampMs}`
}

function aisTrailsSignature(trails: Map<string, TrailPoint[]>): string {
  let signature = ''
  for (const [name, points] of trails) {
    signature += `${name}=${trailSignature(points)};`
  }
  return signature
}

// Two label-suppression sets are equal when their contents match,
// regardless of identity - used so the AIS-label effect below only calls
// setState (and forces a second full render of this 1,748-line map) when
// the result actually changed.
function suppressedIdsEqual(a: Set<string>, b: Set<string>): boolean {
  if (a.size !== b.size) return false
  for (const id of a) {
    if (!b.has(id)) return false
  }
  return true
}

// A map click that hasn't been committed to a placemark yet — the tooltip
// offering range, bearing and a Pin button. Unlike the AIS selection
// highlight it has no expiry: it must sit still long enough to be clicked.
interface PinCandidate {
  lat: number
  lon: number
}

export interface AnchorWatchMapProps {
  vesselLat: number
  vesselLon: number
  vesselHeadingDeg: number | null
  // Null before an anchor is set — groundwork for an always-on map. The
  // hosts still gate rendering on anchor-set for now, so the no-anchor path
  // below only runs under test until the tile/drawer rework lands.
  anchorLat: number | null
  anchorLon: number | null
  // The watch's set_at: the anchoring session's identity, minted at the drop
  // and held for the life of the watch (a reposition keeps it). Null while
  // the first /api/anchor-watch poll is in flight and whenever no watch is
  // running. The map centres itself on the anchor whenever this changes, so
  // every client swings to the new anchorage instead of sitting over the
  // water it was left looking at.
  anchorSetAt?: string | null
  // Whether the host has actually resolved anchor state — useAnchorWatch's
  // own `loaded` (true only once the first GET /api/anchor-watch has
  // succeeded). `anchorSetAt: null` alone is ambiguous between "no watch is
  // running" and "still waiting to hear back", and a stored map centre from
  // a past anchorage must only be discarded once "no watch" is actually
  // confirmed — discarding it on the ambiguous case would flash the vessel
  // position and then jump back to the stored centre the moment the poll
  // resolves active. Defaults to false (not known) per the repo's fail-fast
  // policy: a caller that forgets to wire this up gets the safe "still
  // waiting to hear back" behaviour (trust a stored centre, don't discard it
  // yet) rather than the default silently asserting a confirmed "no anchor"
  // it has no basis for (code-review finding, PR #30).
  anchorStateKnown?: boolean
  radiusMeters: number
  // No longer read inside this component — the map's own Depth row is gone
  // (ADR 0133's amendment: Depth is the Anchor Watch page's own hero KPI
  // now, in the header above the map). Kept in the prop contract since both
  // existing hosts (anchor-watch-tile.tsx, anchor-watch-drawer.tsx) still
  // pass it; dropping the field would be a breaking API change for no
  // behavioural gain — the same treatment distanceMeters got when it made
  // the opposite trip out of this panel.
  depthMeters: number | null
  currentDriftKts: number | null
  currentSetDeg: number | null
  currentDriftImpactKts?: number | null
  // The boat's live distance from the anchor — this panel's own top row
  // (ADR 0133's amendment), same figure and formatting the old promoted
  // Distance KPI showed before Depth took over as the page's hero.
  distanceMeters: number | null
  bearingDeg: number | null
  // Rendered as the Scope row in the metric overlay, under Current (ADR 0059
  // §3). Null when the host has nothing to say yet (e.g. no map at all) —
  // distinct from a RodeMethodResult carrying its own unavailableReason,
  // which still renders the row with the reason visible.
  scopeRecommendation: RodeMethodResult | null
  isImperial: boolean
  vesselTrail: () => TrailPoint[]
  aisVessels: NearbyVessel[]
  aisTrails: () => Map<string, TrailPoint[]>
  // Vessel id -> worst live collision-alarm state (ADR 0088), keyed the same
  // way collisionAlarmStatesByVessel keys it. Optional and left undefined by
  // every existing caller/test with no alarm feed wired up — the AIS marker
  // block below reads it with optional chaining and stays amber throughout.
  aisCollisionAlarms?: ReadonlyMap<string, AlarmState>
  // Optional, and defaulted below, so every existing caller and test that
  // mounts this map without a mayara integration keeps compiling untouched
  // — the same treatment `placemarks` already gets.
  radarTargets?: RadarTarget[]
  // The radar *units* mayara knows about (distinct from radarTargets, its
  // ARPA contacts) -- optional and defaulted below, same treatment as
  // radarTargets, so every existing caller/test with no mayara integration
  // wired up keeps compiling untouched.
  radars?: RadarInfo[]
  radarSource?: RadarSource
  isDarkTheme: boolean
  showImageryLayer?: boolean
  onImageryToggle?: (enabled: boolean) => void
  showRadarEcho?: boolean
  onRadarEchoToggle?: (enabled: boolean) => void
  onFullscreen?: () => void
  // Design critique item 3: six controls stacked in-tile clipped the bottom
  // two at tile height. Defaults to the collapsed three (fullscreen + zoom)
  // every existing host (the tile) already gets for free with no prop
  // change; the fullscreen drawer opts into the full set explicitly, since
  // it has the room the tile doesn't.
  expandedControls?: boolean
  // Session-bound pins shared across every client watching this anchorage.
  placemarks?: AnchorPlacemark[]
  onPlacemarkCreate?: (lat: number, lon: number) => void
  onPlacemarkRemove?: (id: string) => void
  className?: string
  // False on the wall kiosk (ADR: kiosk maps are display-only): passes
  // interactive={false} straight through to the underlying maplibre map,
  // which detaches every mouse/touch/keyboard handler (no zoom, pan, rotate
  // or gesture handling), and hides every on-map control (zoom, fullscreen,
  // satellite, radar echo, recentre) - there is nothing for a display with
  // no touchscreen to drive them with. The map still swings to a new anchor
  // session and markers/trails still update, since those are driven
  // imperatively (easeTo) rather than through one of the handlers this
  // disables. Defaults to true so every existing host keeps today's
  // behaviour.
  interactive?: boolean
  // An opaque id for the surface this instance renders into — namespaces the
  // persisted fitted-zoom key (see zoomStorageKey above) so the dashboard
  // tile and the fullscreen drawer, very different sizes, each keep their
  // own fit instead of clobbering each other's. Defaults to '' (today's
  // unscoped key) for any caller/test that doesn't pass one; the pan centre
  // is intentionally still shared across hosts (ADR 0064) — only the zoom is
  // size-dependent.
  viewKey?: string
  // The Adjust mode entry point (ADR 0133's amendment, ADR 0136) — an icon
  // in the map's right-hand control stack, rendered only when the map is
  // interactive, an anchor is down, and this prop is actually given.
  // Undefined (not passed at all) is how the dashboard tile and the kiosk
  // opt out: neither host passes it, so neither ever shows the icon,
  // regardless of `interactive`.
  onAdjust?: () => void
  // Whether Adjust mode (ADR 0136) is open. Owned by the host
  // (anchor-watch-drawer.tsx), not this component: the drawer decides entry/
  // exit (the Move icon, Escape, Cancel, a successful Set, raising the
  // anchor, the watch disappearing) and this component only reacts to the
  // prop — camera lock, the fixed crosshair, the screen-space ring, the
  // reference layers, and pan/zoom capture.
  adjustActive?: boolean
  // The radius ceiling/floor (lib/anchor-adjust.ts's alarmRadiusBounds) —
  // required whenever adjustActive is true, so pinch/scroll-wheel zoom can
  // be locked to the exact range the operator is allowed to set. Optional in
  // the type only because every non-Adjust render omits it.
  adjustRadiusBounds?: AlarmRadiusBounds
  // Fires once on Adjust entry (with the committed position/radius as the
  // starting draft) and again on every pan/zoom while it stays open — the
  // host's only view into the live draft, since nothing is written until
  // Set. Never fires while adjustActive is false.
  onAdjustDraftChange?: (draft: AnchorAdjustDraft) => void
  // Enter/Escape inside the map container (never window — the keyboard
  // scoping the plan asks for) delegate to the host's own Set/Cancel
  // handlers rather than this component owning any commit logic itself.
  onAdjustSetKey?: () => void
  onAdjustCancelKey?: () => void
}

/** The Adjust mode's live draft — nothing more than what Set would PATCH, reported up so the host's bottom bar and warning banner can read it. */
export interface AnchorAdjustDraft {
  lat: number
  lon: number
  radiusM: number
}

/** Imperative surface Adjust mode's bottom bar (owned by the host) drives the camera through — setting an absolute target radius by easing zoom so the fixed-size ring keeps representing it. */
export interface AnchorWatchMapHandle {
  setAdjustRadius: (targetRadiusM: number) => void
  /**
   * The last radius setAdjustRadius (or the map's own keyboard +/-) actually
   * commanded, or null before Adjust has ever set one this session — the
   * host's own +/- step handler bases its next target on this, not the
   * draft it last received, for the same reason the map's own keyboard
   * handler does (see pendingTargetRadiusMRef's doc comment): the reported
   * draft only updates once the 150ms ease reports back, which can lag
   * behind a rapid hold's 80ms repeat interval.
   */
  getAdjustRadiusTarget: () => number | null
}

export const AnchorWatchMap = forwardRef<AnchorWatchMapHandle, AnchorWatchMapProps>(function AnchorWatchMap({
  vesselLat,
  vesselLon,
  vesselHeadingDeg,
  anchorLat,
  anchorLon,
  anchorSetAt = null,
  anchorStateKnown = false,
  radiusMeters,
  // depthMeters intentionally not destructured — see the prop doc above.
  currentDriftKts,
  currentSetDeg,
  currentDriftImpactKts = null,
  distanceMeters,
  bearingDeg: bearingDegProp,
  scopeRecommendation,
  isImperial,
  vesselTrail,
  aisVessels,
  aisTrails,
  aisCollisionAlarms,
  radarTargets = [],
  radars = [],
  radarSource = 'disabled',
  isDarkTheme,
  showImageryLayer = false,
  onImageryToggle,
  showRadarEcho = false,
  onRadarEchoToggle,
  onFullscreen,
  expandedControls = false,
  placemarks = [],
  onPlacemarkCreate,
  onPlacemarkRemove,
  className,
  interactive = true,
  viewKey = '',
  onAdjust,
  adjustActive = false,
  adjustRadiusBounds,
  onAdjustDraftChange,
  onAdjustSetKey,
  onAdjustCancelKey,
}: AnchorWatchMapProps, ref) {
  const hasAnchor = anchorLat !== null && anchorLon !== null
  // WPE WebKit 2.38 (the wall-display kiosk browser) has no WebGL2, and
  // MapLibre 5 throws synchronously when it can't get a context — mounting
  // the map anyway would blank the whole app (ADR 0089 §11). The metric
  // overlay and the zoom/expand controls below are unaffected: they read
  // from props and from an optionally-chained mapRef that is simply never
  // populated when the map itself doesn't mount.
  const canRenderMap = hasWebGL2()
  const mapWrapperRef = useRef<HTMLDivElement | null>(null)
  const metricsPanelRef = useRef<HTMLDivElement | null>(null)
  const mapControlsRef = useRef<HTMLDivElement | null>(null)
  const mapRef = useRef<MapRef | null>(null)

  // Resolved once, at mount. A stored centre is the operator's pan, but only
  // for the anchorage it was made in: against a different session it is a
  // view of water the boat has left, so the anchor wins. `hasAnchor` false
  // is ambiguous until anchorStateKnown is true — it means either "no watch
  // is running" or "we haven't heard back from the first poll yet", and only
  // the former should discard a stored centre. Once anchorStateKnown
  // confirms there is genuinely no anchor, a stored centre from any session
  // must not win, or the operator opens on last night's bay instead of the
  // boat while about to drop a new anchor. While it's still unknown, trust
  // the stored centre for now and let the effects below correct it the
  // moment a session id lands, or the moment "no watch" is confirmed. Keyed
  // on hasAnchor rather than anchorSetAt: a legacy watch record with no
  // recorded set_at can still have an anchor, and that anchor must win over
  // a stale stored centre exactly as it always has.
  const [mountView] = useState(() => {
    const stored = readStoredCenter()
    if (anchorStateKnown && !hasAnchor) {
      return { center: null, sessionId: null }
    }
    const belongsToCurrentSession =
      stored !== null && (anchorSetAt === null || stored.sessionId === anchorSetAt)
    return {
      center: belongsToCurrentSession ? stored : null,
      sessionId: belongsToCurrentSession ? stored.sessionId : anchorSetAt,
    }
  })
  // The anchor session the view on screen is currently following. Declared
  // early (ahead of the fit-to-anchor effect below, which reads it) rather
  // than beside handleMoveEnd/handleRecenter further down, where it used to
  // sit before the fit effect needed to see it too.
  const viewSessionRef = useRef<string | null>(mountView.sessionId)
  // Whether the view on screen at mount came from a stored centre rather
  // than the live vessel/anchor fallback — the one case the effect below
  // (confirming "no watch" while already mounted) has anything to correct.
  const mountedFromStoredCenterRef = useRef(mountView.center !== null)
  // Whether the operator has panned or zoomed since mount — set from
  // onDragStart/onZoomStart below (user gestures only; programmatic
  // easeTo/jumpTo never fire them). The unknown-anchor-state-resolves
  // transition effect checks this before snapping the view back to the
  // vessel: a pan made while the first poll was still in flight is a
  // deliberate look at the chart, not a mistake to undo the moment the poll
  // answers (code-review finding).
  const hasUserPannedRef = useRef(false)
  const handleUserGestureStart = useCallback(() => {
    hasUserPannedRef.current = true
  }, [])

  // Forward reference to the AIS-label suppression recompute (declared
  // further down, after the state it closes over) so handleMoveEnd below
  // can trigger it on pan/zoom without needing to be redeclared every time
  // aisVessels changes — see the effect that keeps this current, near the
  // suppression logic itself.
  const recomputeAisLabelSuppressionRef = useRef<() => void>(() => {})
  const collapseAttribution = useCollapsedMapAttribution(mapRef)
  // Which AIS marker is drawn enlarged. Selection is by id, not name: two
  // vessels sharing a name previously both lit up when either was clicked.
  const [selectedVesselId, setSelectedVesselId] = useState<string | null>(null)
  const selectionTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [pinCandidate, setPinCandidate] = useState<PinCandidate | null>(null)
  const [selectedPlacemarkId, setSelectedPlacemarkId] = useState<string | null>(null)
  // Every marker on this map (self vessel, AIS, placemarks, the anchor, the
  // pin-candidate tooltip buttons) sets this so its own click doesn't fall
  // through to handleMapClick's "place a pin here" handling below. Each of
  // those markers' React onClick already calls e.stopPropagation() too, and
  // — confirmed against both maplibre-gl's own source (Marker.addTo appends
  // the marker's element into map.getCanvasContainer(), the exact element
  // HandlerManager binds its native 'click' listener to) and React's event
  // system (a portaled child's stopPropagation() halts the underlying native
  // event during React's root-level dispatch, before the browser's own
  // bubble phase ever reaches canvasContainer) — that alone already keeps
  // maplibre from ever seeing a marker tap as a map click in the first
  // place. This ref exists for the input types or embedding quirks where
  // that isn't reliable (code-review finding: a flag set but never consumed
  // by a map click that never arrives just sits there and swallows the
  // *next*, unrelated, genuine tap instead). suppressNextMapClick below is
  // the only way it's ever set, so it can never outlive a stray tick.
  const suppressNextMapClickRef = useRef(false)
  const suppressNextMapClickTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const suppressNextMapClick = useCallback(() => {
    suppressNextMapClickRef.current = true
    if (suppressNextMapClickTimerRef.current !== null) {
      clearTimeout(suppressNextMapClickTimerRef.current)
    }
    // A genuine map click for the same tap, if maplibre ever does see one,
    // fires synchronously within the same event — well inside this window.
    // Anything arriving after it is a separate, later tap that deserves to
    // be treated normally.
    suppressNextMapClickTimerRef.current = setTimeout(() => {
      suppressNextMapClickRef.current = false
      suppressNextMapClickTimerRef.current = null
    }, 300)
  }, [])
  useEffect(() => () => {
    if (suppressNextMapClickTimerRef.current !== null) clearTimeout(suppressNextMapClickTimerRef.current)
  }, [])
  const [renderKey, setRenderKey] = useState(0) // bumped each poll cycle to re-render trails
  // Track zoom for marker scaling. No onZoom handler: that used to fire
  // setCurrentZoom on every animation frame of a zoom gesture, and a
  // persistence effect wrote it to localStorage on each of those renders
  // too. Neither marker scale nor the satellite-imagery fade need that
  // granularity - both only have to be right once the gesture settles - so
  // currentZoom is updated from handleMoveEnd below instead, which fires
  // once per gesture (moveend also fires at the end of a zoom, not just a
  // pan) rather than once per frame.
  const [currentZoom, setCurrentZoom] = useState(() => {
    const stored = readStoredZoom(viewKey)
    const zoomBelongsToCurrentSession = stored !== null && (anchorSetAt === null || stored.sessionId === anchorSetAt)
    return zoomBelongsToCurrentSession ? stored.zoom : DEFAULT_ZOOM_BEFORE_FIT
  })
  // Scale markers: full size at zoom 14+, shrink linearly down to 0.45× at zoom 10
  const markerScale = markerScaleForZoom(currentZoom)
  const worldImageryOpacity = computeWorldImageryOpacity(currentZoom, showImageryLayer)

  // ── Fitting and following the anchor (session change, fresh mount with no
  // matching stored zoom, or a container that couldn't be measured yet) —
  // all funnelled through one fitToAnchor so a single event (e.g. a drop
  // while mounted with no prior anchor) only ever moves the camera once
  // (code-review finding: the old separate mount-time fit effect and
  // session-change effect could both fire for that same drop, one jumping
  // the zoom and the other immediately easing center+zoom on top of it).
  //
  // react-map-gl constructs the underlying maplibre.Map instance
  // asynchronously (mapRef.current can still be null the first time this
  // runs, right at mount) and the wrapper can measure 0×0 (a hidden page,
  // kiosk rotation, or the drawer opening) — either way jumpTo/easeTo would
  // silently no-op or fit against a bogus size. Whatever a fit couldn't
  // finish is kept in pendingFitRef and retried from handleMapLoad (the map
  // becoming ready) and from the ResizeObserver further down (the container
  // getting a real size), rather than giving up on it for good. Marking the
  // fit "done" (didFitInitialZoomRef) only happens once the zoom half has
  // actually landed on a real map at a real size — never merely once it was
  // computed — so a premature reload can't read back a fit that never
  // actually applied.
  const didFitInitialZoomRef = useRef(false)

  interface PendingFit {
    lat: number
    lon: number
    radiusM: number
    sessionId: string | null
    // Whether this fit still owes the map an easeTo to a new centre (a
    // session change) or is a zoom-only catch-up for the session this
    // component has already been following (a fresh mount/host with no
    // matching stored zoom yet). Flips to false once the centre half lands,
    // so an unmeasurable retry doesn't re-ease a centre that already moved.
    animateCenter: boolean
  }
  const pendingFitRef = useRef<PendingFit | null>(null)

  const measureShortSidePx = useCallback(() => {
    const container = mapRef.current?.getMap?.()?.getContainer?.() ?? mapWrapperRef.current
    const rect = container?.getBoundingClientRect()
    return rect ? Math.min(rect.width, rect.height) : 0
  }, [])

  // Applies as much of `pending` as the map/container currently allow, and
  // returns what's left to retry (null once fully applied).
  const attemptFit = useCallback((pending: PendingFit): PendingFit | null => {
    const map = mapRef.current
    if (!map) return pending // nothing done yet — retry once handleMapLoad fires

    const shortSidePx = measureShortSidePx()
    if (shortSidePx > 0) {
      const zoom = fitRadiusZoom(pending.radiusM, pending.lat, shortSidePx)
      didFitInitialZoomRef.current = true
      setCurrentZoom(zoom)
      writeStoredZoom(zoom, pending.sessionId, viewKey)
      if (pending.animateCenter) {
        writeStoredCenter(pending.lat, pending.lon, pending.sessionId)
        map.easeTo({ center: [pending.lon, pending.lat], zoom, duration: 600 })
      } else {
        map.jumpTo({ zoom })
      }
      return null
    }

    // Unmeasurable: a session change still moves the centre now (today's
    // shipped fallback — nothing truthful to fit a zoom against, but the
    // anchorage itself is known), and only the zoom half is left pending,
    // retried once the container reports a real size.
    if (pending.animateCenter) {
      writeStoredCenter(pending.lat, pending.lon, pending.sessionId)
      map.easeTo({ center: [pending.lon, pending.lat], duration: 600 })
    }
    return { ...pending, animateCenter: false }
  }, [viewKey, measureShortSidePx])

  const fitToAnchor = useCallback((
    lat: number,
    lon: number,
    radiusM: number,
    sessionId: string | null,
    animateCenter: boolean,
  ) => {
    pendingFitRef.current = attemptFit({ lat, lon, radiusM, sessionId, animateCenter })
  }, [attemptFit])

  const retryPendingFit = useCallback(() => {
    if (!pendingFitRef.current) return
    pendingFitRef.current = attemptFit(pendingFitRef.current)
  }, [attemptFit])

  useEffect(() => {
    if (!hasAnchor || anchorLat === null || anchorLon === null) {
      // Raised: any fit still owed to the previous anchor no longer applies
      // to anything. Left uncleared, a later unrelated resize or style
      // reload (retryPendingFit, below) would apply that stale geometry to
      // a map that now has no anchor at all (code review finding 6).
      pendingFitRef.current = null
      return
    }
    const sessionChanged = viewSessionRef.current !== anchorSetAt
    if (sessionChanged) {
      // A new anchorage pulls every client's view to it — the alternative is
      // a chart still centred on last night's bay, boat and swing circle
      // off-screen. A reposition drag keeps the same set_at (the backend
      // carries it forward), so nobody's view is yanked while the hook is
      // being nudged around.
      viewSessionRef.current = anchorSetAt
      fitToAnchor(anchorLat, anchorLon, radiusMeters, anchorSetAt, true)
      return
    }
    // Same session this component has already been following (including
    // "always has, since mount") — only a catch-up fit is owed, and only if
    // this host has no matching persisted zoom of its own yet.
    if (didFitInitialZoomRef.current) return
    if (pendingFitRef.current) {
      // A fit for this same session is already deferred (map not ready, or
      // container unmeasurable) — keep its radius current rather than
      // silently dropping a radius change (the stepper, or the Rode
      // Planner's "Apply as alarm radius") that lands before it resolves;
      // otherwise the eventual retry fits the STALE radius this effect was
      // first deferred with (code review finding 6).
      pendingFitRef.current = { ...pendingFitRef.current, radiusM: radiusMeters }
      return
    }
    const stored = readStoredZoom(viewKey)
    const zoomBelongsToCurrentSession = stored !== null && (anchorSetAt === null || stored.sessionId === anchorSetAt)
    if (zoomBelongsToCurrentSession) {
      didFitInitialZoomRef.current = true
      return
    }
    fitToAnchor(anchorLat, anchorLon, radiusMeters, anchorSetAt, false)
  }, [hasAnchor, anchorLat, anchorLon, radiusMeters, anchorSetAt, viewKey, fitToAnchor])

  // Fail-fast per the repo fallback policy: MapPlaceLabels' <Layer>
  // elements (mounted below, after the alarm-circle Source) attach to a
  // source this component never creates. If the loaded style doesn't carry
  // one named BASE_VECTOR_SOURCE_ID, react-map-gl's createLayer silently
  // skips addLayer and every place name just never shows up, with nothing
  // else saying why. styledata fires many times per style load (tiles
  // arriving, etc.), so this is guarded by a ref to log once per load
  // rather than once per event - reset below when isDarkTheme flips,
  // which is a real style reload (STYLE_LIGHT/STYLE_DARK swap).
  const missingSourceWarnedRef = useRef(false)
  const isDarkThemeRef = useRef(isDarkTheme)
  useEffect(() => {
    if (isDarkThemeRef.current !== isDarkTheme) {
      isDarkThemeRef.current = isDarkTheme
      missingSourceWarnedRef.current = false
    }
  }, [isDarkTheme])
  const handleStyleData = useCallback(() => {
    const map = mapRef.current?.getMap()
    if (!map || !map.isStyleLoaded() || missingSourceWarnedRef.current) return
    missingSourceWarnedRef.current = true
    warnIfBaseVectorSourceMissing(map)
  }, [])

  // Dual-range radars (this boat's Furuno) present two radar keys off one
  // antenna; radars[0] speaks for the pair, same convention
  // radarHeaderLabel (radar-targets-tile.tsx) already uses.
  const primaryRadar = radars[0] ?? null
  // Capabilities are only worth fetching once a radar exists and is
  // actually transmitting -- otherwise resolveRadarEchoAvailability below
  // has already explained why there's nothing to show, and a GET here
  // would just be wasted backend/mayara round-trips for a picture that
  // can't be drawn regardless of what it returns.
  const radarEchoRadarId = primaryRadar && primaryRadar.transmitting && radarSource !== 'disabled' ? primaryRadar.id : null
  const {
    capabilities: radarEchoCapabilities,
    palette: radarEchoPalette,
    error: radarEchoCapabilitiesError,
    loading: radarEchoCapabilitiesLoading,
  } = useRadarCapabilities(radarEchoRadarId)

  const {
    status: radarEchoStatus,
    handleMapLoad: handleRadarEchoMapLoad,
    handleStyleData: handleRadarEchoStyleData,
  } = useRadarEchoLayer({
    mapRef,
    // Own ship, not the radar's own reported fix: mayara captures Spoke.lat/lon
    // once and does not refresh it (resolveEchoCentre).
    vesselLat,
    vesselLon,
    radarId: radarEchoRadarId,
    enabled: showRadarEcho,
    capabilities: radarEchoCapabilities,
    palette: radarEchoPalette,
  })

  const radarEchoAvailability = useMemo(
    () =>
      resolveRadarEchoAvailability({
        radarSource,
        radars,
        capabilitiesError: radarEchoCapabilitiesError,
        capabilitiesLoading: radarEchoCapabilitiesLoading,
        echoEnabled: showRadarEcho,
        echoStatus: radarEchoStatus,
      }),
    [radarSource, radars, radarEchoCapabilitiesError, radarEchoCapabilitiesLoading, showRadarEcho, radarEchoStatus],
  )

  const handleMapLoad = useCallback(() => {
    handleRadarEchoMapLoad()
    // The map instance is guaranteed to exist by the time 'load' fires —
    // this is the fallback path for fitToAnchor above, for the common case
    // where it ran before react-map-gl had finished constructing the
    // underlying map (or session change followed by a not-yet-ready map).
    retryPendingFit()
  }, [handleRadarEchoMapLoad, retryPendingFit])

  const handleMapStyleData = useCallback(() => {
    handleStyleData()
    handleRadarEchoStyleData()
  }, [handleStyleData, handleRadarEchoStyleData])

  // Re-render trails on each poll cycle (trails are stored in refs, not
  // state) - but only bump renderKey when a trail's signature (point count
  // plus its last point's own timestamp) actually changed since the last
  // tick. postAnchorTrailGeoJSON/aisTrailsData below are keyed on renderKey,
  // so an unconditional bump every 5s used to hand react-map-gl a brand new
  // GeoJSON object - and force it to re-upload both lineMetrics sources -
  // even on a tick where not a single point had arrived.
  const trailSignatureRef = useRef<string>('')
  useEffect(() => {
    trailSignatureRef.current = `${trailSignature(vesselTrail())}|${aisTrailsSignature(aisTrails())}`
    const timer = setInterval(() => {
      const next = `${trailSignature(vesselTrail())}|${aisTrailsSignature(aisTrails())}`
      if (next !== trailSignatureRef.current) {
        trailSignatureRef.current = next
        setRenderKey((k) => k + 1)
      }
    }, 5000)
    return () => clearInterval(timer)
  }, [vesselTrail, aisTrails])

  // Collapse the enlarged marker after 3 seconds. Only the highlight is
  // transient — every marker's range stays on show permanently.
  const selectVessel = useCallback((id: string) => {
    if (selectionTimerRef.current) clearTimeout(selectionTimerRef.current)
    setSelectedVesselId(id)
    selectionTimerRef.current = setTimeout(() => setSelectedVesselId(null), 3000)
  }, [])

  const highDriftImpact = currentDriftImpactKts !== null && Math.abs(currentDriftImpactKts) >= HIGH_DRIFT_IMPACT_KTS
  // Scope row (ADR 0059 §3): unavailable covers both a null prop (host has
  // nothing to say) and a RodeMethodResult carrying its own unavailableReason
  // — either way the row shows the reason, never a bare dash.
  const scopeUnit = isImperial ? 'ft' : 'm'
  const scopeAvailable = scopeRecommendation !== null && !scopeRecommendation.unavailableReason
  const scopeReason = scopeRecommendation?.unavailableReason ?? null
  const scopeRodeDisplay = scopeAvailable
    ? Math.round(isImperial ? scopeRecommendation!.recommendedRodeM * 3.28084 : scopeRecommendation!.recommendedRodeM)
    : null
  const formatRange = useCallback(
    (distanceM: number) => {
      if (isImperial) {
        return `${Math.round(distanceM * 3.28084)} ft`
      }
      return distanceM < 1000 ? `${distanceM} m` : `${(distanceM / 1000).toFixed(1)} km`
    },
    [isImperial],
  )

  // GeoJSON data
  // Empty FeatureCollection with no anchor, rather than unmounting the
  // Source/Layers below — see the raster-layer comment further down for why
  // they have to stay mounted regardless.
  const circleGeoJSON = useMemo<GeoJSON.Feature<GeoJSON.Polygon> | GeoJSON.FeatureCollection>(
    () =>
      hasAnchor
        ? generateCircleGeoJSON(anchorLon, anchorLat, radiusMeters)
        : { type: 'FeatureCollection', features: [] },
    [hasAnchor, anchorLon, anchorLat, radiusMeters],
  )

  // Trail data (re-derived each render triggered by renderKey bump)
  const postAnchorTrailGeoJSON = useMemo(
    () => trailToGeoJSON(vesselTrail()),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [renderKey, vesselTrail],
  )

  const aisTrailsData = useMemo(() => {
    const trails = aisTrails()
    const activeNames = new Set(aisVessels.map(v => v.name))
    const cutoffMs = Date.now() - AIS_TRAIL_MAX_AGE_MS
    const features: GeoJSON.Feature<GeoJSON.LineString>[] = []
    for (const [name, points] of trails) {
      if (!activeNames.has(name)) continue
      const recentPoints = points.filter((p) => p.timestampMs >= cutoffMs)
      if (recentPoints.length >= 2) features.push(trailToGeoJSON(recentPoints))
    }
    return { type: 'FeatureCollection' as const, features }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [renderKey, aisTrails, aisVessels])

  // ── Map click handler ────────────────────────────────────────────────────
  // Editing (reposition, radius drag) is gone entirely — every branch that
  // used to commit one of those on a map click went with it. What's left is
  // exactly the placemark-pinning flow: unoccupied water raises a pin-it
  // tooltip, and every marker (AIS, anchor, self vessel, placemarks) sets
  // suppressNextMapClickRef so a click that reaches here always landed on
  // open chart.
  const handleMapClick = useCallback(
    (e: maplibregl.MapMouseEvent) => {
      if (suppressNextMapClickRef.current) {
        suppressNextMapClickRef.current = false
        return
      }
      // No active watch: POST /api/anchor-watch/placemarks would 409 (see
      // backend/anchor_placemarks.go), so don't even raise the tooltip.
      if (!hasAnchor) return
      // Adjust mode owns every tap/drag on the map for positioning the
      // draft anchor — a click landing here mid-pan (MapLibre can fire a
      // synthetic click alongside a drag that started as one) must not also
      // raise the unrelated "pin a hazard" tooltip on top of it.
      if (adjustActive) return
      const { lat, lng } = e.lngLat
      setSelectedPlacemarkId(null)
      setPinCandidate({ lat, lon: lng })
    },
    [hasAnchor, adjustActive],
  )

  // Both tooltip buttons sit inside the map, so their clicks also reach
  // maplibre's own handler — which would immediately reopen the tooltip
  // under the button just pressed. Suppress that follow-on click the same
  // way the AIS and placemark markers do.
  const handleConfirmPin = useCallback((e: React.MouseEvent) => {
    e.stopPropagation()
    suppressNextMapClick()
    if (!pinCandidate) return
    onPlacemarkCreate?.(pinCandidate.lat, pinCandidate.lon)
    setPinCandidate(null)
  }, [pinCandidate, onPlacemarkCreate, suppressNextMapClick])

  const handleDismissPin = useCallback((e: React.MouseEvent) => {
    e.stopPropagation()
    suppressNextMapClick()
    setPinCandidate(null)
  }, [suppressNextMapClick])

  const handleSelfVesselClick = useCallback((e: React.MouseEvent) => {
    e.stopPropagation()
    suppressNextMapClick()
  }, [suppressNextMapClick])

  const handlePlacemarkClick = useCallback((e: React.MouseEvent, id: string) => {
    e.stopPropagation()
    suppressNextMapClick()
    setPinCandidate(null)
    setSelectedPlacemarkId((current) => (current === id ? null : id))
  }, [suppressNextMapClick])

  const handleRemovePlacemark = useCallback((e: React.MouseEvent, id: string) => {
    e.stopPropagation()
    suppressNextMapClick()
    onPlacemarkRemove?.(id)
    setSelectedPlacemarkId(null)
  }, [onPlacemarkRemove, suppressNextMapClick])

  // ── AIS vessel click ─────────────────────────────────────────────────────
  const handleAisClick = useCallback(
    (e: React.MouseEvent, vessel: NearbyVessel) => {
      e.stopPropagation()
      suppressNextMapClick()
      setPinCandidate(null)
      setSelectedPlacemarkId(null)
      selectVessel(vessel.id)
    },
    [selectVessel, suppressNextMapClick],
  )

  // ── Anchor marker click ──────────────────────────────────────────────────
  // The marker itself is a non-interactive "Anchor position" div now (no
  // edit mode exists to enter) — this only swallows the click so it doesn't
  // fall through to the map's own "place a pin here" handler, same as every
  // other marker on this map (self vessel, AIS, placemarks).
  const handleAnchorMarkerClick = useCallback((e: React.MouseEvent) => {
    e.stopPropagation()
    suppressNextMapClick()
  }, [suppressNextMapClick])

  // ── Zoom / Recenter controls ────────────────────────────────────────────
  const handleZoomIn = useCallback(() => {
    mapRef.current?.easeTo({ zoom: (mapRef.current.getZoom() ?? 14) + 1, duration: 250 })
  }, [])

  const handleZoomOut = useCallback(() => {
    mapRef.current?.easeTo({ zoom: (mapRef.current.getZoom() ?? 14) - 1, duration: 250 })
  }, [])

  const handleRecenter = useCallback(() => {
    // No removeItem here (code-review finding): the easeTo below fires its
    // own moveend the instant it settles, and handleMoveEnd unconditionally
    // rewrites the stored centre to the new position anyway — a preceding
    // removeItem was dead code, immediately undone.
    // Nothing to swing around without an anchor — recentre on the vessel.
    const center: [number, number] = hasAnchor ? [anchorLon, anchorLat] : [vesselLon, vesselLat]
    mapRef.current?.easeTo({ center, duration: 600 })
  }, [hasAnchor, anchorLat, anchorLon, vesselLat, vesselLon])

  const handleMoveEnd = useCallback((e: { viewState: { latitude: number; longitude: number; zoom: number } }) => {
    if (typeof window === 'undefined') return
    // Adjust mode's camera is following a DRAFT, not the operator's own pan
    // of the ordinary chart — persisting it as the stored centre/zoom would
    // leave every other view (and this one, after Cancel) reopening on
    // wherever Adjust happened to leave the map, including a session that
    // never actually got Set. handleAdjustMove (below) is the one place
    // that reads the camera while Adjust is open.
    if (adjustActive) return
    const { latitude, longitude, zoom } = e.viewState
    if (Number.isFinite(latitude) && Number.isFinite(longitude)) {
      // Tagged with the session the view is following (a ref, so this stays
      // a stable handler) — a pan is only worth restoring for the anchorage
      // it was made in.
      writeStoredCenter(latitude, longitude, viewSessionRef.current)
    }
    // Not interactive (kiosk): no user gesture can change the zoom, so
    // there's nothing to track — see the currentZoom state's own comment.
    if (interactive && Number.isFinite(zoom)) {
      setCurrentZoom(zoom)
      writeStoredZoom(zoom, viewSessionRef.current, viewKey)
    }
    // A pan/zoom changes every AIS vessel's projected screen position, so
    // the label-declutter result can change even with no new AIS poll.
    recomputeAisLabelSuppressionRef.current()
  }, [adjustActive, interactive, viewKey])

  // ── Adjust mode (ADR 0136) ───────────────────────────────────────────────
  // Nothing below ever writes to the server — Adjust mode's only write is
  // the host's own Set button (anchor-watch-drawer.tsx's
  // useAnchorAdjustCommit), well after this component has reported a draft
  // up. Everything here is camera control and screen-space rendering: the
  // fixed crosshair, the screen-space swing ring, the faded reference
  // layers, and pan/zoom/keyboard capture.

  // Focus returns here on exit (the plan's own keyboard requirement).
  const moveButtonRef = useRef<HTMLButtonElement | null>(null)

  const [adjustDraft, setAdjustDraft] = useState<AnchorAdjustDraft | null>(null)
  // Mirrors adjustDraft synchronously, same reasoning as adjustRingPxRef
  // below: the resize handler further down needs the latest draft without
  // adding adjustDraft itself to that effect's own dependency list, which
  // would tear down and re-subscribe its ResizeObserver on every pan/pinch.
  const adjustDraftRef = useRef<AnchorAdjustDraft | null>(null)
  // The last radius applyAdjustRadius was actually asked to reach — distinct
  // from adjustDraft.radiusM, which only updates once the camera's own
  // moveend/move event reports back (code-review finding). easeTo's 150ms
  // transition is slower than usePressRepeat's 80ms repeat interval, so a
  // held +/- (or rapid native key-repeat on the keyboard's own +/-) can fire
  // its next step before that report lands; deriving the next target from
  // adjustDraft.radiusM in that window recomputes from a stale, mid-ease
  // value instead of continuing from where the previous step was actually
  // headed, landing off the exact step grid (74/78/83 ft instead of
  // 75/80/85). Every step now bases itself on this instead.
  const pendingTargetRadiusMRef = useRef<number | null>(null)
  // The container's short side in CSS pixels, remeasured on entry and on
  // resize — drives the fixed ring's own diameter and every zoom<->radius
  // conversion below, so a resized window or a rotated kiosk-sized drawer
  // keeps the ring's ground radius accurate rather than a stale measurement
  // from whenever Adjust happened to open.
  //
  // Mirrored into a ref (updated synchronously, not just via the state
  // setter) because map.jumpTo() below fires MapLibre's 'move' event
  // *synchronously*, within the very same effect that just measured this
  // value — well before React has re-rendered and handed handleAdjustMove a
  // closure that actually sees the new state. A handler reading the state
  // value here would process that first synchronous move against whatever
  // adjustRingPx held on the PREVIOUS render (0, on entry), computing a
  // radius of 0 and clamping straight to the floor. The ref is always
  // current at the moment it's read, regardless of which render's closure
  // is doing the reading.
  const [adjustRingPx, setAdjustRingPxState] = useState(0)
  const adjustRingPxRef = useRef(0)
  const setAdjustRingPx = useCallback((px: number) => {
    adjustRingPxRef.current = px
    setAdjustRingPxState(px)
  }, [])

  const reportAdjustDraft = useCallback((draft: AnchorAdjustDraft) => {
    adjustDraftRef.current = draft
    setAdjustDraft(draft)
    onAdjustDraftChange?.(draft)
  }, [onAdjustDraftChange])

  // Guards the exit branch below from running its restore/focus-return on
  // the component's very first mount (adjustActive starts false, and the
  // entry/exit effect fires once on mount regardless) — without this, every
  // render of this map would steal focus onto the Move icon the instant it
  // mounts, for no user action at all.
  const everEnteredAdjustRef = useRef(false)

  // Entry/exit transition — deliberately keyed on adjustActive alone (see
  // the eslint-disable below): re-running this for every tick of
  // anchorLat/anchorLon/radiusMeters while Adjust stays open would reset the
  // camera and the draft on every 5s anchor-watch poll, which is exactly
  // what the plan's "the poll must not reset the draft or the camera"
  // requirement rules out. Safe by construction — nothing writes to the
  // server (and so nothing changes these props) until Set, so they are
  // frozen at their entry values for the life of one Adjust session.
  useEffect(() => {
    const map = mapRef.current?.getMap?.()
    if (adjustActive) {
      everEnteredAdjustRef.current = true
      if (anchorLat === null || anchorLon === null) return
      const shortSidePx = measureShortSidePx()
      const ringPx = ringRadiusPx(shortSidePx)
      setAdjustRingPx(ringPx)
      const bounds: AlarmRadiusBounds = adjustRadiusBounds ?? { minM: MIN_ALARM_RADIUS_M, maxM: radiusMeters, maxReason: null }
      // Clamped, not the raw committed radiusMeters (code-review finding):
      // the drawer's own openAdjust already seeds ITS draft with
      // clampRadiusM(radiusMeters, bounds) so a radius saved before the
      // current chain+LOA ceiling existed starts inside it — this entry
      // effect used to report the raw value right after, silently
      // overwriting that clamped seed with one Set would PATCH straight
      // back out to. MapLibre's own setMinZoom/setMaxZoom below already
      // clamp the CAMERA to whatever the raw radius's zoom would have been,
      // so the ring was already showing the clamped ground radius while this
      // reported the wrong number as the draft.
      const clampedRadiusM = clampRadiusM(radiusMeters, bounds)
      const initialZoom = zoomForRingRadius(clampedRadiusM, anchorLat, ringPx)
      const zoomBounds = adjustZoomBounds(bounds, anchorLat, ringPx)
      if (map) {
        map.setMinZoom(zoomBounds.minZoom)
        map.setMaxZoom(zoomBounds.maxZoom)
        // Touch-pinch rotate is the one rotation gesture the Map's own
        // dragRotate={false} prop (always on, not Adjust-specific) doesn't
        // already cover.
        map.touchZoomRotate?.disableRotation?.()
        map.jumpTo({ center: [anchorLon, anchorLat], zoom: initialZoom })
        // CSS alone (the full-screen phone layout) doesn't tell MapLibre its
        // container resized — belt-and-suspenders alongside MapLibre's own
        // trackResize default.
        map.resize()
      }
      setCurrentZoom(initialZoom)
      pendingTargetRadiusMRef.current = clampedRadiusM
      reportAdjustDraft({ lat: anchorLat, lon: anchorLon, radiusM: clampedRadiusM })
      mapWrapperRef.current?.focus()
    } else if (everEnteredAdjustRef.current) {
      everEnteredAdjustRef.current = false
      if (map) {
        map.setMinZoom(ANCHOR_VIEW_MIN_ZOOM)
        map.setMaxZoom(ADJUST_MAX_ZOOM)
        map.touchZoomRotate?.enableRotation?.()
      }
      adjustDraftRef.current = null
      pendingTargetRadiusMRef.current = null
      setAdjustDraft(null)
      // "Return focus to the Move icon on exit" — a no-op (optional
      // chaining) if the icon isn't currently rendered, e.g. the anchor was
      // raised as part of this same exit and hasAnchor has already gone
      // false.
      moveButtonRef.current?.focus()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [adjustActive])

  // Fires on every pan/zoom while Adjust is open (react-map-gl's onMove,
  // distinct from onMoveEnd — this needs the live value while a gesture is
  // still in progress, not just once it settles). The draft radius is
  // clamped to bounds as a safety net (minZoom/maxZoom already stop pinch
  // at the limits) and snapped to a whole display unit — "snap the
  // displayed/committed value, not the zoom" avoids the readout jittering
  // by fractions of a metre as a gesture settles at an off zoom.
  const handleAdjustMove = useCallback((e: { viewState: { latitude: number; longitude: number; zoom: number }; originalEvent?: unknown }) => {
    if (!adjustActive) return
    const { latitude, longitude, zoom } = e.viewState
    if (!Number.isFinite(latitude) || !Number.isFinite(longitude) || !Number.isFinite(zoom)) return
    const bounds: AlarmRadiusBounds = adjustRadiusBounds ?? { minM: MIN_ALARM_RADIUS_M, maxM: radiusMeters, maxReason: null }
    // Reads the ref, not the closed-over adjustRingPx state — see its own
    // doc comment above for why: map.jumpTo() on entry fires this
    // synchronously, before this render's state update has reached a new
    // closure.
    const rawRadiusM = radiusForZoom(zoom, latitude, adjustRingPxRef.current)
    const snappedRadiusM = snapRadiusM(clampRadiusM(rawRadiusM, bounds), isImperial)
    // A genuine pan/pinch gesture is just as authoritative as a commanded
    // step for what the NEXT step should continue from (pendingTargetRadiusMRef's
    // own doc comment) — without this, stepping once, then pinching by hand,
    // then stepping again would resume from the first step's stale target
    // instead of wherever the pinch actually left the ring.
    //
    // Gated on e.originalEvent (code-review finding, round 2): this handler
    // fires on EVERY animation frame of applyAdjustRadius's own 150ms
    // easeTo, not only on a genuine gesture — MapLibre only attaches
    // originalEvent to a move event it fired for real user input
    // (drag/wheel/touch); a programmatic easeTo/jumpTo call with no
    // eventData argument (every call this file makes) fires 'move' with
    // originalEvent undefined. Writing the ref unconditionally meant a
    // still-in-flight frame of a step's own ease — not yet at the target,
    // since the ease takes longer than usePressRepeat's 80ms repeat or a
    // keyboard's own auto-repeat — would overwrite the correct target that
    // same step had just recorded, so the NEXT step read back a stale,
    // not-yet-settled value instead of continuing from where the previous
    // one was actually headed.
    if (e.originalEvent !== undefined) {
      pendingTargetRadiusMRef.current = snappedRadiusM
    }
    reportAdjustDraft({ lat: latitude, lon: longitude, radiusM: snappedRadiusM })
  }, [adjustActive, adjustRadiusBounds, radiusMeters, isImperial, reportAdjustDraft])

  // Eases the camera's zoom to whatever represents `targetRadiusM` on the
  // fixed-size ring — the ring's own screen size never changes, only the
  // ground scale under it. The draft itself is updated by the moveend/move
  // events this triggers, not written here directly, so there is exactly
  // one place (handleAdjustMove) that derives it from the camera. Takes the
  // absolute target, not a delta, so the same function serves the keyboard's
  // relative +/- (which computes its own target first) and the host's chip
  // taps (which already have an absolute value in hand) without either
  // needing a second copy.
  const applyAdjustRadius = useCallback((targetRadiusMRaw: number) => {
    if (!adjustActive || !adjustDraft) return
    const bounds: AlarmRadiusBounds = adjustRadiusBounds ?? { minM: MIN_ALARM_RADIUS_M, maxM: adjustDraft.radiusM, maxReason: null }
    const targetRadiusM = clampRadiusM(targetRadiusMRaw, bounds)
    pendingTargetRadiusMRef.current = targetRadiusM
    const zoom = zoomForRingRadius(targetRadiusM, adjustDraft.lat, adjustRingPxRef.current)
    mapRef.current?.easeTo({ zoom, duration: 150 })
  }, [adjustActive, adjustDraft, adjustRadiusBounds])

  // Keyboard pan (arrows) — converts a metre offset to a pixel offset at the
  // current zoom/latitude and pans by it, same mechanism a drag gesture
  // drives, just without one.
  const panAdjustByMeters = useCallback((eastM: number, northM: number) => {
    if (!adjustActive) return
    const map = mapRef.current?.getMap?.()
    const zoom = map?.getZoom() ?? currentZoom
    const lat = adjustDraft?.lat ?? anchorLat
    if (lat === null || lat === undefined) return
    const mPerPx = metersPerPixel(lat, zoom)
    if (!(mPerPx > 0)) return
    // Screen y grows downward; north is -y.
    map?.panBy([eastM / mPerPx, -northM / mPerPx], { duration: 0 })
  }, [adjustActive, adjustDraft, anchorLat, currentZoom])

  // Scoped to the map container's own onKeyDown, never window — a keydown
  // anywhere else on the page (the bottom bar's own inputs, the rest of the
  // drawer) must not pan or resize the ring.
  const handleAdjustKeyDown = useCallback((e: React.KeyboardEvent<HTMLDivElement>) => {
    if (!adjustActive) return
    const panStepM = e.shiftKey ? 5 : 1
    const step = radiusStepM(isImperial)
    switch (e.key) {
      case 'ArrowUp': e.preventDefault(); panAdjustByMeters(0, panStepM); break
      case 'ArrowDown': e.preventDefault(); panAdjustByMeters(0, -panStepM); break
      case 'ArrowLeft': e.preventDefault(); panAdjustByMeters(-panStepM, 0); break
      case 'ArrowRight': e.preventDefault(); panAdjustByMeters(panStepM, 0); break
      // Steps from the last commanded target (pendingTargetRadiusMRef), not
      // adjustDraft.radiusM directly — see that ref's own doc comment for
      // why: a native key-repeat firing faster than the 150ms ease would
      // otherwise recompute from a stale, mid-ease draft.
      case '+':
      case '=': e.preventDefault(); if (adjustDraft) applyAdjustRadius((pendingTargetRadiusMRef.current ?? adjustDraft.radiusM) + step); break
      case '-':
      case '_': e.preventDefault(); if (adjustDraft) applyAdjustRadius((pendingTargetRadiusMRef.current ?? adjustDraft.radiusM) - step); break
      case 'Enter': e.preventDefault(); onAdjustSetKey?.(); break
      case 'Escape': e.preventDefault(); onAdjustCancelKey?.(); break
      default: break
    }
  }, [adjustActive, isImperial, panAdjustByMeters, applyAdjustRadius, adjustDraft, onAdjustSetKey, onAdjustCancelKey])

  // The bottom bar (owned by the host, anchor-watch-drawer.tsx) drives the
  // camera's radius through this — its own +/- buttons and chip taps need to
  // ease the SAME zoom this component's keyboard handler above eases,
  // rather than a second copy of the conversion.
  useImperativeHandle(ref, () => ({
    setAdjustRadius: applyAdjustRadius,
    getAdjustRadiusTarget: () => pendingTargetRadiusMRef.current,
  }), [applyAdjustRadius])

  // The reference line + faded original circle's label ("moved 8 m ·
  // 045°") — null (and so not rendered) under 1 m of movement, and null
  // outright with no draft yet (the one render before the entry effect
  // above has run).
  const adjustMovedLabel = adjustActive && adjustDraft && anchorLat !== null && anchorLon !== null
    ? formatMovedLabel(anchorMoveOffset(anchorLat, anchorLon, adjustDraft.lat, adjustDraft.lon), isImperial)
    : null

  // ── Initial map view ─────────────────────────────────────────────────────
  // mountView, resolved above, has already decided whether the stored centre
  // belongs to the anchorage now under the boat.
  const initialViewState = useMemo(() => {
    // No anchor to open on yet — fall back to the vessel.
    const fallback = hasAnchor
      ? { latitude: anchorLat, longitude: anchorLon }
      : { latitude: vesselLat, longitude: vesselLon }
    return {
      longitude: mountView.center ? mountView.center.longitude : fallback.longitude,
      latitude: mountView.center ? mountView.center.latitude : fallback.latitude,
      zoom: currentZoom,
    }
  },
    // Only used as initial value — no deps to avoid re-centering on every update
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  )

  // Tracks the previous anchorStateKnown so the effect below fires on
  // exactly one transition: false -> true. Ordinary Raise (an already-known
  // active watch going inactive) never touches this, since anchorStateKnown
  // was already true well before the Raise — this only fires the first time
  // ambiguity resolves.
  const anchorStateWasKnownRef = useRef(anchorStateKnown)

  // Read fresh off a ref rather than closed over directly (same reasoning as
  // recomputeAisLabelSuppression's own vesselPositionRef further down, which
  // this ref now also backs): the transition below only cares about
  // anchorStateKnown/hasAnchor, but vesselLat/vesselLon used to sit in its
  // dependency array too, which meant this effect tore down and re-created
  // on every GPS tick even though it returns immediately on all but the one
  // render where the transition actually happens (code-review finding).
  // Updated unconditionally every render, so it's always current by the time
  // the transition effect reads it.
  const vesselPositionRef = useRef({ lat: vesselLat, lon: vesselLon })
  vesselPositionRef.current = { lat: vesselLat, lon: vesselLon }

  // The mount-time gate above only helps a fresh mount. A map that was
  // already on screen while the first poll was in flight — trusting a
  // stored centre because the ambiguous null hadn't resolved yet — needs
  // its own correction once "no watch" is confirmed: swing to the vessel,
  // the same way a fresh mount would have opened. Fires at most once (the
  // ref above), and only when there's actually a stored centre to override —
  // if the view was already tracking the vessel/anchor fallback, easing to
  // the vessel again is a no-op anyway, but there's nothing to correct.
  //
  // Skipped entirely if the operator has panned or zoomed since mount
  // (hasUserPannedRef, set from onDragStart/onZoomStart below) — code-review
  // finding: snapping back to the vessel the instant "no watch" is confirmed
  // would otherwise throw away a deliberate look at the chart made while the
  // first poll was still in flight. viewSessionRef still gets cleared either
  // way, so a pan made from here on is correctly tagged "no session" rather
  // than whatever stale session id mount happened to resolve.
  useEffect(() => {
    const wasKnown = anchorStateWasKnownRef.current
    anchorStateWasKnownRef.current = anchorStateKnown
    if (wasKnown || !anchorStateKnown) return
    if (hasAnchor) return // resolved active - fitToAnchor above handles centring on the anchor
    if (!mountedFromStoredCenterRef.current) return
    mountedFromStoredCenterRef.current = false
    viewSessionRef.current = null
    // No removeItem here (code-review finding): same reasoning as
    // handleRecenter above — whichever branch below runs, either the easeTo's
    // own moveend or the operator's own prior pan has already left a correct,
    // current entry in storage; there is nothing left to clear.
    if (hasUserPannedRef.current) return
    const { lat, lon } = vesselPositionRef.current
    mapRef.current?.easeTo({ center: [lon, lat], duration: 600 })
  }, [anchorStateKnown, hasAnchor])

  const mapStyle = isDarkTheme ? STYLE_DARK : STYLE_LIGHT

  const handleImageryToggle = useCallback(() => {
    onImageryToggle?.(!showImageryLayer)
  }, [showImageryLayer, onImageryToggle])

  const handleRadarEchoToggle = useCallback(() => {
    onRadarEchoToggle?.(!showRadarEcho)
  }, [showRadarEcho, onRadarEchoToggle])

  // AIS labels (9px map annotation, DESIGN.md's legibility floor) whose
  // projected screen position lands under the metric overlay, under the
  // control stack, or within LABEL_COLLISION_RADIUS_PX of a higher-priority
  // vessel's label — see resolveMarkerLabelSuppression above. `project`
  // isn't part of every test double for MapRef (only a live maplibre map
  // provides it), so this degrades to "suppress nothing" — today's
  // behaviour — wherever it's unavailable, rather than throwing.
  //
  // This used to recompute on every own-ship position tick (vesselLat/
  // vesselLon in the trigger deps), which called getBoundingClientRect
  // three times (a forced synchronous layout) and always handed React a
  // brand-new Set identity, forcing a second full render of this map on
  // every GPS fix. Own-ship position does feed `priority` (the tie-break
  // between two colliding AIS labels), but only changes which of two
  // already-colliding vessels wins — it does not, on its own, justify
  // re-measuring the overlay panels or re-rendering on every tick, so it's
  // read fresh off vesselPositionRef (declared above, kept current every
  // render, no effect needed) rather than triggering the recompute itself.

  const [suppressedAisLabelIds, setSuppressedAisLabelIds] = useState<Set<string>>(new Set())

  // Avoid-zone rects (the metrics panel, the control stack), in
  // wrapper-relative coordinates. Cached here rather than re-measured by
  // the recompute below: getBoundingClientRect forces a synchronous layout,
  // and these panels only actually move when the wrapper itself resizes or
  // the metrics panel's own row count changes (hasAnchor) — not on every
  // AIS poll or pan/zoom.
  const avoidZonesRef = useRef<ScreenRect[]>([])
  const recomputeAvoidZones = useCallback(() => {
    const wrapper = mapWrapperRef.current
    if (!wrapper) return
    const wrapperRect = wrapper.getBoundingClientRect()
    const zones: ScreenRect[] = []
    for (const ref of [metricsPanelRef, mapControlsRef]) {
      const el = ref.current
      if (!el) continue
      const rect = el.getBoundingClientRect()
      zones.push({
        left: rect.left - wrapperRect.left,
        top: rect.top - wrapperRect.top,
        right: rect.right - wrapperRect.left,
        bottom: rect.bottom - wrapperRect.top,
      })
    }
    avoidZonesRef.current = zones
  }, [])

  const recomputeAisLabelSuppression = useCallback(() => {
    const map = mapRef.current
    if (!map || typeof map.project !== 'function') return
    const { lat, lon } = vesselPositionRef.current
    const points: MarkerLabelPoint[] = []
    for (const vessel of aisVessels) {
      if (vessel.lat === undefined || vessel.lon === undefined) continue
      const projected = map.project([vessel.lon, vessel.lat])
      points.push({
        id: vessel.id,
        x: projected.x,
        y: projected.y,
        priority: haversineMeters(lat, lon, vessel.lat, vessel.lon),
      })
    }
    const next = resolveMarkerLabelSuppression(points, avoidZonesRef.current)
    setSuppressedAisLabelIds((current) => (suppressedIdsEqual(current, next) ? current : next))
  }, [aisVessels])

  // recomputeAisLabelSuppression's identity changes with aisVessels, so
  // handleMoveEnd (declared earlier, well before aisVessels is known to
  // change) reads it through this ref rather than depending on it directly
  // — otherwise every AIS poll would also mean re-subscribing onMoveEnd.
  useEffect(() => {
    recomputeAisLabelSuppressionRef.current = recomputeAisLabelSuppression
  }, [recomputeAisLabelSuppression])

  // Mount + whenever the metrics panel's own row count changes (hasAnchor)
  // + whenever aisVessels changes (a new/departed contact, or ranks
  // reshuffling): both the avoid zones and the suppression result need a
  // fresh look. Neither of these fires per GPS tick.
  useEffect(() => {
    recomputeAvoidZones()
    recomputeAisLabelSuppression()
  }, [hasAnchor, recomputeAvoidZones, recomputeAisLabelSuppression])

  // Container resize (a tile being resized, the browser window changing, a
  // hidden page becoming visible, kiosk rotation, the drawer opening): the
  // other thing that can move the overlay panels, and — via retryPendingFit
  // (code-review finding) — the only chance a zoom fit that had to be
  // deferred for an unmeasurable 0×0 container ever gets to actually land,
  // rather than being silently skipped for the rest of the session.
  useEffect(() => {
    const wrapper = mapWrapperRef.current
    if (!wrapper || typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(() => {
      recomputeAvoidZones()
      recomputeAisLabelSuppressionRef.current()
      retryPendingFit()
      // The full-screen phone layout (className flips to fixed inset-0 on
      // Adjust entry) resizes this exact container, and so does an ordinary
      // browser window resize or a kiosk rotation while Adjust is already
      // open. The ring's pixel size has to track the new short side — but a
      // resize is not a gesture, so the ground radius it represents (what
      // the bar shows and Set would commit) must not drift just because the
      // container changed shape (code-review finding: this used to update
      // only adjustRingPx, leaving the zoom bounds and the reported draft
      // radius stale until the next pan/pinch silently snapped them onto
      // the new ring). Recompute the ring, recompute the zoom bounds
      // against it, and re-aim the camera's zoom so the operator's own
      // chosen radius stays exactly what it was.
      const draft = adjustDraftRef.current
      if (adjustActive && draft) {
        const newRingPx = ringRadiusPx(measureShortSidePx())
        setAdjustRingPx(newRingPx)
        const bounds: AlarmRadiusBounds = adjustRadiusBounds ?? { minM: MIN_ALARM_RADIUS_M, maxM: draft.radiusM, maxReason: null }
        // Clamped, not the existing draft.radiusM as-is (code-review
        // finding, same reasoning as the entry effect above): if the bounds
        // have narrowed since the draft was last set (a settings change to
        // chain onboard/LOA landing mid-session), a resize must pull the
        // reported radius back inside them like every other path already
        // does, not just keep re-asserting whatever it already was.
        const clampedRadiusM = clampRadiusM(draft.radiusM, bounds)
        // Keeps the pending-target tracking (see its own doc comment) in
        // sync with a resize too, so a keyboard/bar step immediately after
        // one continues from the radius the resize just settled on, not a
        // stale value from before it.
        pendingTargetRadiusMRef.current = clampedRadiusM
        const zoomBounds = adjustZoomBounds(bounds, draft.lat, newRingPx)
        const map = mapRef.current?.getMap?.()
        if (map) {
          map.setMinZoom(zoomBounds.minZoom)
          map.setMaxZoom(zoomBounds.maxZoom)
          map.jumpTo({ zoom: zoomForRingRadius(clampedRadiusM, draft.lat, newRingPx) })
        }
        // Restated explicitly rather than left to jumpTo's own synchronous
        // 'move' event (which handleAdjustMove would otherwise re-derive
        // from the new ringPx/zoom pair): the radius has to read back as
        // exactly what it was, not a value that merely round-trips close to
        // it through a second floating-point conversion.
        reportAdjustDraft({ ...draft, radiusM: clampedRadiusM })
      }
    })
    observer.observe(wrapper)
    return () => observer.disconnect()
  }, [recomputeAvoidZones, retryPendingFit, adjustActive, measureShortSidePx, setAdjustRingPx, adjustRadiusBounds, reportAdjustDraft])

  return (
    <div
      ref={mapWrapperRef}
      data-testid="anchor-watch-map-wrapper"
      className={cn('relative isolate overflow-hidden rounded-lg', className)}
      // Keyboard scoping (the plan's own requirement): Adjust's arrows/+-/
      // Enter/Escape only fire from a keydown on this container, never
      // window — tabIndex makes it focusable so the entry effect above can
      // actually put focus here. Not focusable outside Adjust: nothing else
      // in this component wants keyboard capture.
      tabIndex={adjustActive ? 0 : undefined}
      onKeyDown={adjustActive ? handleAdjustKeyDown : undefined}
    >
      {!canRenderMap ? (
        <div
          data-testid="anchor-watch-map-webgl2-fallback"
          className="flex h-full items-center justify-center px-3 text-center text-[10px] uppercase tracking-[0.14em] text-muted-foreground"
        >
          Map needs WebGL2, which this browser does not provide
        </div>
      ) : (
      <Map
        ref={mapRef}
        mapLib={maplibregl}
        initialViewState={initialViewState}
        style={{ width: '100%', height: '100%' }}
        mapStyle={mapStyle}
        minZoom={10}
        interactive={interactive}
        onLoad={handleMapLoad}
        onMove={handleAdjustMove}
        onMoveEnd={handleMoveEnd}
        // User-gesture-only events (maplibre never fires these for a
        // programmatic easeTo/jumpTo) — the only signal hasUserPannedRef
        // needs to tell "the operator moved the view" from "the code did".
        onDragStart={handleUserGestureStart}
        onZoomStart={handleUserGestureStart}
        onStyleData={handleMapStyleData}
        onClick={handleMapClick}
        // Carto and OpenStreetMap both require attribution, and since the
        // backend now proxies and caches their tiles (ADR 0067) rather than
        // the browser fetching them from CARTO directly, showing that credit
        // is our obligation, not the CDN's. MapLibre's own control collects
        // the attribution string every active source declares - Carto/OSM
        // through the proxied TileJSON, plus OpenSeaMap, Esri and any
        // uploaded chart - so it stays correct as layers come and go.
        // Compact keeps it to a single small "i" until tapped, which suits a
        // dense helm display better than a permanent strip of credits.
        attributionControl={{ compact: true }}
        onIdle={collapseAttribution}
        dragRotate={false}
        touchPitch={false}
        // Always on — there is no edit mode left that ever needs to borrow
        // this gesture for something else (radius/reposition editing used to
        // disable it imperatively while a drag was in progress).
        dragPan
      >
        {/* Alarm circle fill — during Adjust this is the ORIGINAL circle
            (nothing is written until Set, so circleGeoJSON/radiusMeters
            stay frozen at the committed watch for the whole session),
            faded down so it reads as reference rather than as the live
            alarm boundary — that role belongs to the screen-space ring
            below, which tracks the draft. */}
        <Source id="alarm-circle" type="geojson" data={circleGeoJSON}>
          <Layer
            id="alarm-circle-fill"
            type="fill"
            paint={{
              'fill-color': isDarkTheme ? '#38bdf8' : '#0ea5e9',
              'fill-opacity': adjustActive ? 0.04 : 0.1,
            }}
          />
          <Layer
            id="alarm-circle-stroke"
            type="line"
            paint={{
              'line-color': isDarkTheme ? '#38bdf8' : '#0284c7',
              'line-width': 2,
              'line-dasharray': [4, 3],
              'line-opacity': adjustActive ? 0.35 : 0.8,
            }}
          />
        </Source>

        {/* Adjust mode's reference line: original anchor -> the draft
            (the crosshair, always the map's own centre) — a real map layer
            rather than a screen-space overlay, so it projects correctly
            through every pan/zoom with no extra project() bookkeeping. */}
        {adjustActive && adjustDraft && anchorLat !== null && anchorLon !== null && (
          <Source
            id="adjust-reference-line"
            type="geojson"
            data={{
              type: 'Feature',
              properties: {},
              geometry: { type: 'LineString', coordinates: [[anchorLon, anchorLat], [adjustDraft.lon, adjustDraft.lat]] },
            }}
          >
            <Layer
              id="adjust-reference-line-layer"
              type="line"
              paint={{
                'line-color': isDarkTheme ? '#38bdf8' : '#0284c7',
                'line-width': 1.5,
                'line-dasharray': [2, 2],
                'line-opacity': 0.9,
              }}
            />
          </Source>
        )}

        {/*
          Place-name labels (bays, islands, marinas, peaks, town top-up -
          see docs/adr/0066-basemap-place-name-labels.md) mount here,
          unconditionally and with no beforeId. This map pins its rasters
          beforeId="alarm-circle-fill" - i.e. above the whole base style -
          so with imagery on, Carto's own labels (never drawn anyway, which
          is the whole reason this component exists) would be buried under
          the raster regardless. Mounting these label layers right after
          alarm-circle, with no beforeId, is what keeps names visible above
          the satellite raster on this map; the overImagery white/black-halo
          paint below is load-bearing here, not cosmetic.
        */}
        <MapPlaceLabels isDarkTheme={isDarkTheme} overImagery={showImageryLayer} />

        {/* AIS vessel trails */}
      {aisTrailsData.features.length > 0 && (
        <Source id="ais-trails" type="geojson" lineMetrics data={aisTrailsData}>
          <Layer
            id="ais-trails-layer"
            type="line"
            paint={{
              'line-width': 2.5,
              'line-gradient': [
                'interpolate',
                ['linear'],
                ['line-progress'],
                0, 'rgba(245, 158, 11, 0)',
                0.5, 'rgba(245, 158, 11, 0.4)',
                1, 'rgba(245, 158, 11, 1)',
              ],
              'line-opacity': 0.9,
            }}
            layout={{ 'line-join': 'round', 'line-cap': 'round' }}
          />
        </Source>
      )}

        {/* Post-anchor tracking trail (ring buffer) */}
        {postAnchorTrailGeoJSON.geometry.coordinates.length >= 2 && (
          <Source id="vessel-trail-anchor" type="geojson" lineMetrics data={postAnchorTrailGeoJSON}>
            <Layer
              id="vessel-trail-anchor-layer"
              type="line"
              paint={{
                'line-width': 3,
                'line-gradient': [
                  'interpolate',
                  ['linear'],
                  ['line-progress'],
                  0, '#fde047',
                  0.7, '#facc15',
                  1, '#f59e0b',
                ],
                'line-opacity': 0.9,
              }}
              layout={{ 'line-join': 'round', 'line-cap': 'round' }}
            />
          </Source>
        )}

        {/*
          Raster layers (imagery, seamarks) are declared last and explicitly
          anchored with beforeId="alarm-circle-fill" - react-map-gl's addLayer
          call has no awareness of JSX sibling order, so a layer that mounts
          later (e.g. world-imagery toggled on mid-session, well after the
          map's initial load) is otherwise appended on top of the whole
          stack, covering the alarm circle and trails outright. Anchoring to
          alarm-circle-fill, which is unconditional and always mounts first,
          keeps these rasters below the vector layers regardless of when
          they're toggled on.

          This is exactly why the alarm-circle Source and its two layers
          above stay mounted even with no anchor set, rather than being
          unmounted along with the anchor marker: if they weren't there,
          beforeId="alarm-circle-fill" would have no target to attach to, the
          rasters would never mount at all, and layer order would scramble
          the moment an anchor later appeared mid-session. Instead,
          circleGeoJSON collapses to an empty FeatureCollection with no
          anchor (see above), so the layers stay put and simply draw nothing.
        */}
        {showImageryLayer && worldImageryOpacity > 0 && (
          <Source
            id="world-imagery"
            type="raster"
            tiles={[WORLD_IMAGERY_TILES]}
            tileSize={256}
            maxzoom={WORLD_IMAGERY_MAX_ZOOM}
            attribution="Source: Esri, Maxar, Earthstar Geographics"
          >
            <Layer
              id="world-imagery-layer"
              type="raster"
              beforeId="alarm-circle-fill"
              paint={{
                'raster-opacity': worldImageryOpacity,
                'raster-fade-duration': 250,
              }}
            />
          </Source>
        )}

        <Source
          id="openseamap"
          type="raster"
          tiles={[OPENSEAMAP_TILES]}
          tileSize={256}
          attribution="© OpenSeaMap contributors"
        >
          <Layer id="openseamap-layer" type="raster" beforeId="alarm-circle-fill" paint={{ 'raster-opacity': 0.85 }} />
        </Source>

        {/* AIS vessel markers (ADR 0088 colours them). Shape carries
            identity, colour carries state, the same split the radar block
            below uses: a circle with the Ship glyph is always an AIS
            contact, never a triangle. Red is the one shared danger colour
            with radar's dangerous triangle below, added once a collision
            alarm is in force for this target; a ring marks alarm severity
            and above. Amber is AIS's own identity colour, so a warn tier
            can't borrow it and an intermediate orange next to amber isn't
            legible at chart scale — hence one red for every live state,
            with the state itself spelled out in the accessible label. */}
        {aisVessels.map((vessel) => {
          if (vessel.lat === undefined || vessel.lon === undefined) return null
          const isSelected = selectedVesselId === vessel.id
          // Recomputed from the live fix on every render, like a placemark's
          // — vessel.range_m is only as fresh as the last AIS poll, and it
          // ignores our own movement between polls.
          const distanceM = Math.round(haversineMeters(vesselLat, vesselLon, vessel.lat, vessel.lon))
          const bearing = Math.round(bearingDeg(vesselLat, vesselLon, vessel.lat, vessel.lon))
          // A live collision alarm for this target (ADR 0088). Read off the
          // alarm list rather than the plugin's raw collision_alarm_state so
          // the marker agrees with the alarm card: same freshness gate, same
          // self-context skip.
          const collisionState = aisCollisionAlarms?.get(vessel.id)
          const inCollisionAlarm = collisionState !== undefined
          const collisionAlarmTier = collisionState !== undefined
            && ALARM_STATES.indexOf(collisionState) >= ALARM_STATES.indexOf('alarm')
          return (
            <Marker
              key={vessel.id}
              latitude={vessel.lat}
              longitude={vessel.lon}
              style={{ zIndex: isSelected ? 30 : inCollisionAlarm ? 25 : 10 }}
            >
              <button
                className="flex flex-col items-center"
                style={{ minWidth: 40, minHeight: 40 }}
                aria-label={
                  inCollisionAlarm
                    ? `AIS vessel: ${vessel.name}, collision ${collisionState === 'warn' ? 'warning' : collisionState}`
                    : `AIS vessel: ${vessel.name}`
                }
                onClick={(e) => handleAisClick(e, vessel)}
              >
                <div
                  style={{
                    transform: `scale(${markerScale * (isSelected ? 1.35 : 1)})`,
                    transformOrigin: 'center',
                    transition: 'transform 150ms ease-out',
                  }}
                >
                  <div
                    className={cn(
                      'flex items-center justify-center rounded-full shadow-lg',
                      inCollisionAlarm ? 'bg-red-600' : 'bg-amber-500/90',
                      isSelected
                        ? 'h-11 w-11 ring-2 ring-white/70'
                        : cn('h-9 w-9', collisionAlarmTier && 'ring-2 ring-red-600/50'),
                    )}
                  >
                    <Ship className={cn('text-white', isSelected ? 'h-6 w-6' : 'h-5 w-5')} />
                  </div>
                  {/* Suppressed when it would sit under the metric overlay,
                      the control stack, or a closer vessel's own label (see
                      resolveMarkerLabelSuppression) — but never for the
                      vessel the operator just tapped; that one is expected
                      to answer, not disappear. The marker dot itself always
                      stays put either way. */}
                  {(isSelected || !suppressedAisLabelIds.has(vessel.id)) && (
                    <div className="mt-0.5 max-w-28 text-center font-mono text-[9px] font-semibold uppercase tracking-wider text-white drop-shadow-[0_1px_2px_rgba(0,0,0,0.8)]">
                      <div className="truncate">{vessel.name}</div>
                      <div className="whitespace-nowrap text-[9px] font-medium tracking-normal">
                        {formatRange(distanceM)} · {bearing}°
                      </div>
                    </div>
                  )}
                </div>
              </button>
            </Marker>
          )
        })}

        {/* Radar target markers (ADR 0062). Shape carries identity, colour
            carries state: AIS keeps its filled circle with the Ship glyph
            above, radar draws a filled triangle oriented along course_rad —
            the ARPA convention, readable without relying on hue. A target
            with no lat/lon (no mayara fix and no own-ship position to
            project from) renders nothing, exactly as the AIS block above. */}
        {radarTargets.map((target) => {
          if (target.lat === undefined || target.lon === undefined) return null
          const distanceM = Math.round(haversineMeters(vesselLat, vesselLon, target.lat, target.lon))
          const bearing = Math.round(bearingDeg(vesselLat, vesselLon, target.lat, target.lon))
          const acquiring = target.status === 'acquiring'
          // course_rad is true course in radians, same convention as the
          // vessel heading rotation just below. Unknown motion (course_rad
          // absent) leaves the triangle pointing north rather than guessing.
          const rotationDeg = target.course_rad !== undefined ? (target.course_rad * 180) / Math.PI : 0
          return (
            <Marker
              key={target.id}
              latitude={target.lat}
              longitude={target.lon}
              style={{ zIndex: target.is_dangerous ? 25 : 8 }}
            >
              <div
                className="flex flex-col items-center"
                style={{ minWidth: 40, minHeight: 40 }}
                aria-label={`Radar target: ${target.id}`}
                onClick={(e) => e.stopPropagation()}
              >
                <div
                  style={{
                    transform: `scale(${markerScale}) rotate(${rotationDeg}deg)`,
                    transformOrigin: 'center',
                    transition: 'transform 150ms ease-out',
                  }}
                >
                  {/*
                    bg-foreground, not bg-secondary. In the day theme
                    --secondary is 0 0% 92%, near-white, and the map tiles
                    underneath are near-white too: the first live run put 20
                    targets on the chart and only the 3 dangerous ones were
                    visible, the other 17 showing as a floating RDR label with
                    no marker above it. --foreground is 0% on day and 92% on
                    night, so it is legible in both by construction.
                  */}
                  <div
                    className={cn(
                      'h-4 w-4 shadow-lg',
                      acquiring
                        ? 'border-2 border-foreground bg-transparent'
                        : target.is_dangerous
                          ? 'bg-red-600 ring-2 ring-red-600/50'
                          : 'bg-foreground',
                    )}
                    style={{ clipPath: 'polygon(50% 0%, 0% 100%, 100% 100%)' }}
                  />
                </div>
                <div className="mt-0.5 max-w-24 text-center font-mono text-[9px] font-semibold uppercase tracking-wider text-white drop-shadow-[0_1px_2px_rgba(0,0,0,0.8)]">
                  <div className="whitespace-nowrap">
                    RDR {formatRange(distanceM)} · {bearing}°
                  </div>
                </div>
              </div>
            </Marker>
          )
        })}

        {/* Vessel marker */}
        <Marker latitude={vesselLat} longitude={vesselLon}>
          <div
            className="flex items-center justify-center"
            style={{ width: 40, height: 40 }}
            // Marker elements sit inside the canvas container, so a click
            // here still reaches the map handler. Swallow it: the boat's own
            // icon is not a place you'd pin, and the range would read 0.
            onClick={handleSelfVesselClick}
          >
            <VesselArrow headingDeg={vesselHeadingDeg} scale={markerScale} />
          </div>
        </Marker>

        {/* Anchor marker — non-interactive: dragging/tapping it can no longer
            move the anchor or change the radius (impeccable P0s). onClick
            only swallows the event so it doesn't fall through to the map's
            own "place a pin here" handler, same as every other marker here.
            Hidden during Adjust: the fixed centre crosshair (a screen-space
            DOM overlay below, not a Marker) replaces it for the length of
            the session. */}
        {hasAnchor && !adjustActive && (
          <Marker latitude={anchorLat} longitude={anchorLon} style={{ zIndex: 1000 }}>
            <div
              onClick={handleAnchorMarkerClick}
              className="flex items-center justify-center"
              style={{ width: 40, height: 40 }}
              aria-label="Anchor position"
            >
              <div
                className="flex h-8 w-8 items-center justify-center rounded-full bg-sky-600/90 shadow-lg"
                style={{ transform: `scale(${markerScale})`, transformOrigin: 'center', transition: 'transform 150ms ease-out' }}
              >
                <Anchor className="h-4 w-4 text-white" />
              </div>
            </div>
          </Marker>
        )}

        {/* The ORIGINAL anchor position, faded — the reference line above
            runs from here to the draft. Frozen for the session (see the
            alarm-circle comment above: nothing writes to the server until
            Set), so this is exactly anchorLat/anchorLon, not a separate
            captured value. */}
        {hasAnchor && adjustActive && (
          <Marker latitude={anchorLat} longitude={anchorLon} style={{ zIndex: 990 }}>
            <div
              className="flex items-center justify-center opacity-40"
              style={{ width: 40, height: 40 }}
              aria-label="Original anchor position"
            >
              <div className="flex h-6 w-6 items-center justify-center rounded-full bg-sky-600/90 shadow-sm">
                <Anchor className="h-3 w-3 text-white" />
              </div>
            </div>
          </Marker>
        )}

        {/* Session placemarks — bombies, shoreline, anything worth watching
            the swing against. Range and bearing are recomputed from the live
            vessel fix on every render, so they close up as the boat drifts
            towards the hazard. */}
        {placemarks.map((pm) => {
          const distanceM = Math.round(haversineMeters(vesselLat, vesselLon, pm.lat, pm.lon))
          const bearing = Math.round(bearingDeg(vesselLat, vesselLon, pm.lat, pm.lon))
          const isSelected = selectedPlacemarkId === pm.id
          return (
            <Marker key={pm.id} latitude={pm.lat} longitude={pm.lon} style={{ zIndex: isSelected ? 1100 : 900 }}>
              <div className="flex flex-col items-center" data-testid={`placemark-${pm.id}`}>
                <button
                  className="flex flex-col items-center"
                  style={{ minWidth: 40, minHeight: 40 }}
                  aria-label={pm.label ? `Placemark: ${pm.label}` : 'Placemark'}
                  onClick={(e) => handlePlacemarkClick(e, pm.id)}
                >
                  <div
                    style={{
                      transform: `scale(${markerScale * (isSelected ? 1.25 : 1)})`,
                      transformOrigin: 'bottom center',
                      transition: 'transform 150ms ease-out',
                    }}
                  >
                    <MapPin
                      className={cn(
                        'h-6 w-6 drop-shadow-[0_1px_2px_rgba(0,0,0,0.8)]',
                        isSelected ? 'text-fuchsia-300' : 'text-fuchsia-400',
                      )}
                    />
                  </div>
                  {/* Same treatment as an AIS vessel's label — shadowed
                      text, no plate. Persistent map labels are drawn this
                      way so a crowded anchorage doesn't fill up with opaque
                      boxes; only interactive controls (the Pin tooltip, the
                      Remove button) get a solid background. */}
                  <div className="mt-0.5 max-w-28 text-center font-mono text-[9px] font-semibold uppercase tracking-wider text-fuchsia-300 drop-shadow-[0_1px_2px_rgba(0,0,0,0.8)]">
                    {pm.label && <div className="truncate">{pm.label}</div>}
                    <div className="whitespace-nowrap text-[9px] font-medium tracking-normal text-white">
                      {formatRange(distanceM)} · {bearing}°
                    </div>
                  </div>
                </button>
                {isSelected && (
                  <button
                    onClick={(e) => handleRemovePlacemark(e, pm.id)}
                    className="mt-1 rounded-full bg-white/15 px-2 py-0.5 text-[10px] font-medium text-white backdrop-blur-sm hover:bg-white/25 active:scale-95"
                    style={{ transition: 'background-color 150ms ease-out' }}
                  >
                    Remove
                  </button>
                )}
              </div>
            </Marker>
          )
        })}

        {/* Pin candidate — the tooltip raised by clicking open water. It has
            no expiry: it must survive long enough to be clicked. */}
        {pinCandidate && (
          <Marker latitude={pinCandidate.lat} longitude={pinCandidate.lon} style={{ zIndex: 1200 }}>
            <div className="flex flex-col items-center" data-testid="pin-candidate">
              <MapPin className="h-6 w-6 text-white drop-shadow-[0_1px_2px_rgba(0,0,0,0.8)]" />
              <div className="mt-1 flex items-center gap-1.5 rounded-full bg-black/80 py-1 pl-2.5 pr-1 backdrop-blur-sm">
                <span className="whitespace-nowrap font-mono text-[11px] text-white">
                  {formatRange(Math.round(haversineMeters(vesselLat, vesselLon, pinCandidate.lat, pinCandidate.lon)))}
                  {' · '}
                  {Math.round(bearingDeg(vesselLat, vesselLon, pinCandidate.lat, pinCandidate.lon))}°
                </span>
                <button
                  onClick={handleConfirmPin}
                  className="rounded-full bg-fuchsia-500/90 px-2.5 py-0.5 text-[11px] font-semibold text-white hover:bg-fuchsia-400 active:scale-95"
                  style={{ transition: 'background-color 150ms ease-out' }}
                >
                  Pin
                </button>
                <button
                  onClick={handleDismissPin}
                  aria-label="Dismiss"
                  className="flex h-5 w-5 items-center justify-center rounded-full text-white/60 hover:bg-white/15 hover:text-white"
                  style={{ transition: 'background-color 150ms ease-out' }}
                >
                  <X className="h-3 w-3" />
                </button>
              </div>
            </div>
          </Marker>
        )}
      </Map>
      )}

      {/* Metric overlay — top of map. Depth is gone from here entirely (ADR
          0133's amendment): it's the Anchor Watch page's own hero KPI now,
          in the header above the map (anchor-watch-drawer.tsx), so this
          panel doesn't repeat it at a second visual weight. Distance is
          this panel's own top row instead — the boat's live distance from
          the anchor, same figure and formatting the old promoted Distance
          KPI showed. Distance, Bearing and Radius are dropped from the row
          list outright with no anchor set (none of the three mean anything
          without one) rather than rendering a dashed placeholder at full
          visual weight; Current/Scope can be genuinely absent for reasons
          that have nothing to do with anchor state, so they keep rendering
          (and dashing) as before.
          The background is a flat, near-opaque scrim rather than the old
          bg-black/50 (bg-black/35 while editing): measured contrast against
          real satellite imagery came in at 4.1:1, short of the 4.5:1 floor
          AGENTS.md sets for text this small — a translucent ground can't
          promise 4.5:1 against arbitrary imagery underneath it, so the fix
          is a ground dark enough that it doesn't have to. */}
      {/* Hidden during Adjust: Distance/Bearing/Radius here describe the
          COMMITTED watch, and would read as live numbers contradicting the
          draft the crosshair/ring and the bottom bar are actually showing.
          The moved-distance/bearing label near the crosshair and the bar's
          own RADIUS readout are Adjust's replacement for this panel. */}
      {!adjustActive && (
      <div
        ref={metricsPanelRef}
        className="pointer-events-none absolute left-3 top-3 overflow-hidden rounded-lg bg-black/90 backdrop-blur-sm"
        style={{ zIndex: 2000 }}
        data-testid="anchor-watch-metrics"
      >
        {[
          ...(hasAnchor
            ? [
                {
                  label: 'Distance',
                  value: distanceMeters !== null
                    ? isImperial
                      ? `${Math.round(distanceMeters * 3.28084)}`
                      : `${Math.round(distanceMeters)}`
                    : '—',
                  unit: isImperial ? 'ft' : 'm',
                },
                {
                  label: 'Bearing',
                  value: bearingDegProp !== null ? `${bearingDegProp}` : '—',
                  unit: '°',
                },
                {
                  label: 'Radius',
                  value: isImperial
                    ? `${Math.round(radiusMeters * 3.28084)}`
                    : `${Math.round(radiusMeters)}`,
                  unit: isImperial ? 'ft' : 'm',
                },
              ]
            : []),
          {
            label: 'Current',
            value: currentDriftKts !== null ? currentDriftKts.toFixed(1) : '—',
            unit: 'kts',
            setDeg: currentSetDeg,
          },
          {
            label: 'Scope',
            value: scopeRodeDisplay !== null ? `${scopeRodeDisplay}` : '—',
            // Just the recommended rode and its unit, same as Radius/Depth
            // above. The ratio and the MIN_SCOPE_RATIO floor marker used to
            // ride in this suffix too; they're only reachable via the row's
            // tooltip now (rowTitle below), which already carried the full
            // note regardless.
            unit: scopeAvailable ? scopeUnit : '',
            // The reason (not the unit) renders in the unit slot when
            // unavailable — see the render branch below — so the fallback
            // policy's "surface the reason, never a bare dash" holds here too.
            reason: scopeReason,
            // The full note (depth source, wind, ratio, and the
            // MIN_SCOPE_RATIO floor marker when it binds) rides along as a
            // row tooltip rather than crowding the overlay — only set when
            // there's a result to describe.
            rowTitle: scopeAvailable ? scopeRecommendation!.note : undefined,
          },
        ].map(({ label, value, unit, setDeg, reason, rowTitle }, index) => (
          <div
            key={label}
            title={rowTitle}
            className={cn(
              'flex items-center justify-between gap-4 px-3 py-1.5',
              index > 0 && 'border-t border-white/10',
            )}
          >
            <p className="text-[9px] uppercase tracking-[0.16em] text-white/80">
              {label}
            </p>
            {label === 'Current' ? (
              <div className="flex items-center justify-end gap-2">
                <ArrowUp
                  className={cn(
                    'shrink-0 text-white',
                    highDriftImpact ? 'h-5 w-5' : 'h-4 w-4',
                    (value === '0.0' || value === '—') && 'invisible',
                  )}
                  strokeWidth={highDriftImpact ? 2.75 : 2}
                  style={{ transform: `rotate(${setDeg ?? 0}deg)` }}
                  aria-hidden="true"
                />
                <p className="font-display tabular-nums leading-tight text-white" style={{ fontSize: '1.1rem' }}>
                  {value}
                  <span className="ml-0.5 text-[11px] text-white/80">{unit}</span>
                </p>
              </div>
            ) : (
              <p className="font-display tabular-nums leading-tight text-white" style={{ fontSize: '1.1rem' }}>
                {value}
                {reason ? (
                  <span
                    className="ml-1 inline-block max-w-[9rem] truncate align-bottom text-[11px] text-white/80"
                    title={reason}
                  >
                    {reason}
                  </span>
                ) : (
                  <span className="ml-0.5 text-[11px] text-white/80">{unit}</span>
                )}
              </p>
            )}
          </div>
        ))}
      </div>
      )}

      {/* Zoom + Recenter controls. Design critique item 3: six buttons
          stacked in-tile clipped the bottom two at tile height. The
          in-tile default (expandedControls unset) keeps only fullscreen and
          zoom; satellite, radar and recentre move into this same stack
          under expandedControls, which the fullscreen drawer opts into —
          same control, same code, just more room to show all of it.

          Kiosk maps are display-only: every button here is an interaction
          control (zoom, fullscreen, satellite/radar-echo toggle, recentre) —
          nothing informational — so the whole stack is dropped rather than
          picked apart one button at a time when `interactive` is false. */}
      {interactive && !adjustActive && (
      <div
        ref={mapControlsRef}
        className="pointer-events-auto absolute right-3 top-3 flex flex-col gap-1"
        style={{ zIndex: 2100 }}
        data-testid="anchor-watch-controls"
      >
        {onFullscreen && (
          <button
            onClick={onFullscreen}
            aria-label="Full screen"
            className="flex h-10 w-10 items-center justify-center rounded-lg bg-black/65 text-white shadow-sm backdrop-blur-sm hover:bg-black/80 active:scale-95"
            style={{ transition: 'background-color 150ms ease-out' }}
          >
            <Expand className="h-4 w-4" />
          </button>
        )}
        <button
          onClick={handleZoomIn}
          aria-label="Zoom in"
          className="flex h-10 w-10 items-center justify-center rounded-lg bg-black/65 text-white shadow-sm backdrop-blur-sm hover:bg-black/80 active:scale-95"
          style={{ transition: 'background-color 150ms ease-out' }}
        >
          <Plus className="h-4 w-4" />
        </button>
        <button
          onClick={handleZoomOut}
          aria-label="Zoom out"
          className="flex h-10 w-10 items-center justify-center rounded-lg bg-black/65 text-white shadow-sm backdrop-blur-sm hover:bg-black/80 active:scale-95"
          style={{ transition: 'background-color 150ms ease-out' }}
        >
          <Minus className="h-4 w-4" />
        </button>
        {expandedControls && (
          <>
            <button
              onClick={handleImageryToggle}
              aria-label="Toggle satellite imagery"
              className={cn(
                'flex h-10 w-10 items-center justify-center rounded-lg text-white shadow-sm backdrop-blur-sm active:scale-95',
                showImageryLayer ? 'bg-sky-600/90 hover:bg-sky-500/90' : 'bg-black/65 hover:bg-black/80',
              )}
              style={{ transition: 'background-color 150ms ease-out' }}
            >
              <Satellite className="h-4 w-4" />
            </button>
            <button
              onClick={handleRadarEchoToggle}
              aria-label="Toggle radar echo overlay"
              disabled={!radarEchoAvailability.available}
              title={radarEchoAvailability.available ? undefined : (radarEchoAvailability.reason ?? undefined)}
              className={cn(
                'flex h-10 w-10 items-center justify-center rounded-lg text-white shadow-sm backdrop-blur-sm active:scale-95',
                showRadarEcho ? 'bg-sky-600/90 hover:bg-sky-500/90' : 'bg-black/65 hover:bg-black/80',
                !radarEchoAvailability.available && 'cursor-not-allowed opacity-50',
              )}
              style={{ transition: 'background-color 150ms ease-out' }}
            >
              <Radar className="h-4 w-4" />
            </button>
            <button
              onClick={handleRecenter}
              aria-label="Re-centre on anchor"
              className="flex h-10 w-10 items-center justify-center rounded-lg bg-black/65 text-white shadow-sm backdrop-blur-sm hover:bg-black/80 active:scale-95"
              style={{ transition: 'background-color 150ms ease-out' }}
            >
              <Crosshair className="h-4 w-4" />
            </button>
          </>
        )}
        {/* Adjust mode entry point (ADR 0133, ADR 0136): only when there is
            a map to open it on (canRenderMap — no WebGL2 means no map, and
            the header's own text Adjust button is that state's entry point
            instead), an anchor to adjust, and a caller that actually wants
            it (onAdjust) — the dashboard tile and the kiosk never pass this
            prop, so they never get the icon regardless of `interactive`.
            Not gated on expandedControls: it belongs in every interactive
            host's stack, not only the fullscreen drawer's expanded set.
            Move, not Crosshair — Crosshair is already this same stack's
            recentre icon a few buttons up (expandedControls), and reusing
            it here would give two different actions the same glyph. */}
        {canRenderMap && hasAnchor && onAdjust && (
          <button
            ref={moveButtonRef}
            onClick={onAdjust}
            aria-label="Adjust anchor"
            title="Adjust anchor"
            className="flex h-10 w-10 items-center justify-center rounded-lg bg-black/65 text-white shadow-sm backdrop-blur-sm hover:bg-black/80 active:scale-95"
            style={{ transition: 'background-color 150ms ease-out' }}
          >
            <Move className="h-4 w-4" />
          </button>
        )}
        {/* No stop/clear control here — both hosts are gaining a labeled
            Raise button with its own confirm dialog (anchor-watch-tile.tsx,
            anchor-watch-drawer.tsx); a one-tap unlabeled destructive icon
            next to that would be inconsistent. */}
      </div>
      )}

      {/* Adjust mode's fixed centre crosshair (replaces the anchor marker)
          and its screen-space swing ring — DOM/SVG overlays, not map
          layers, per the plan: the ring's own pixel size never changes as
          the operator pinches/scrolls, only the ground scale under it does,
          which is what makes stepping the radius read as "the ring stays
          fixed" rather than visibly resizing. */}
      {adjustActive && canRenderMap && (
        <>
          <div
            className="pointer-events-none absolute inset-0 flex items-center justify-center"
            style={{ zIndex: 1600 }}
            data-testid="anchor-adjust-ring"
          >
            <div
              className="rounded-full border-2 border-dashed border-primary/80"
              style={{ width: adjustRingPx * 2, height: adjustRingPx * 2 }}
            />
          </div>
          <div
            className="pointer-events-none absolute inset-0 flex items-center justify-center"
            style={{ zIndex: 1650 }}
          >
            <div className="relative flex flex-col items-center" data-testid="anchor-adjust-crosshair">
              <div className="absolute h-9 w-px bg-primary" />
              <div className="absolute h-px w-9 bg-primary" />
              <div className="h-4 w-4 rounded-full border-2 border-primary bg-primary/25" />
              {adjustMovedLabel && (
                <div
                  data-testid="anchor-adjust-moved-label"
                  className="absolute top-6 whitespace-nowrap rounded bg-black/85 px-2 py-0.5 text-[10px] font-medium tracking-wide text-white"
                >
                  {adjustMovedLabel}
                </div>
              )}
            </div>
          </div>
        </>
      )}

    </div>
  )
})
