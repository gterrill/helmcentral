import { Anchor, Volume2 } from 'lucide-react'
import { lazy, memo, Suspense, useCallback, type ComponentProps } from 'react'
import { Button } from '@/components/ui/button'
import { Tile } from '@/components/ui/tile'
import { AnchorDropRaiseButton } from '@/components/anchor-drop-raise-button'
import { acknowledgeAlarm, useAnchorDragAlarm, type AlarmState, useAisCollisionAlarms } from '@/hooks/use-alarms'
import { useAnchorAlarm } from '@/hooks/use-anchor-alarm'
import { tideHeightFtOrNull } from '@/lib/rode-plan'
import { formatDataAge, isStale } from '@/lib/staleness'
import { FORECAST_REFRESH_SECONDS, TRAIL_POLL_MS } from '@/config/app-config'
import type { AnchorPlacemark } from '@/hooks/use-anchor-placemarks'
import { useAnchorTrail } from '@/hooks/use-anchor-trail'
import { useAnchorWatch, type AnchorWatchResult } from '@/hooks/use-anchor-watch'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { RadarInfo, RadarSource, RadarTarget } from '@/hooks/use-radar-targets'
import { useAisTrails, type TrailPoint } from '@/hooks/use-server-trails'
import { useTideToday, type TideToday } from '@/hooks/use-tide-today'
import { useMapContacts, usePins, useWatchAtFix } from '@/hooks/use-anchor-map-inputs'
import { useVesselFields } from '@/hooks/use-vessel-state'

// anchor-watch-map.tsx imports maplibre-gl and react-map-gl at module scope
// (map-vendor: 1048 KB raw / 280 KB gzip), and this tile is mounted eagerly
// by App.tsx (it's the dashboard's 'anchor-watch' case, not behind any
// React.lazy panel) - so that whole chunk used to load at startup on every
// route, including /kiosk pages with no anchor-watch tile on them. Same lazy
// -split pattern as assistant-markdown.tsx/help-markdown.tsx, just with
// the boundary drawn inside this component instead of a matching *-impl
// file: the tile's own chrome (alarm strips, distance KPI, Drop) stays
// eager, and only the map slot below waits on the dynamic import.
const AnchorWatchMap = lazy(() =>
  import('@/components/anchor-watch-map').then((m) => ({ default: m.AnchorWatchMap })),
)

/** A distance in the host's chosen unit, rounded for display. */
function formatDistanceValue(meters: number, isImperial: boolean): { value: string; unit: string } {
  return isImperial
    ? { value: `${Math.round(meters * 3.28084)}`, unit: 'ft' }
    : { value: `${Math.round(meters)}`, unit: 'm' }
}

/**
 * "38 m of 30 m" — the figure the drag-alarm strips below need: how far the vessel actually is, against the radius
 * it is meant to stay inside. Falls back to "past {radius}" on the rare
 * frame where an alarm is live but the tile has no live distance to hand
 * (e.g. the GPS fix just dropped) — never a bare dash on what is, by
 * definition, an active alarm condition.
 */
function formatDragDistance(distanceMeters: number | null, radiusMeters: number, isImperial: boolean): string {
  const radius = formatDistanceValue(radiusMeters, isImperial)
  if (distanceMeters === null) {
    return `past ${radius.value} ${radius.unit}`
  }
  const distance = formatDistanceValue(distanceMeters, isImperial)
  return `${distance.value} ${distance.unit} of ${radius.value} ${radius.unit}`
}

interface AnchorWatchTileProps {
  watch: AnchorWatchResult
  lat: number | null
  lon: number | null
  depthMeters: number | null
  currentDriftKts: number | null
  currentSetDeg: number | null
  currentDriftImpactKts?: number | null
  isImperial: boolean
  vesselHeadingDeg: number | null
  vesselTrail: TrailPoint[]
  aisVessels: NearbyVessel[]
  aisTrails: Map<string, TrailPoint[]>
  // Optional, mirroring AnchorWatchMapProps — passed straight through to the
  // map, which colours an AIS marker red while a collision alarm is in force
  // for that vessel id (ADR 0088).
  aisCollisionAlarms?: ReadonlyMap<string, AlarmState>
  // Optional, mirroring AnchorWatchMapProps — a caller with no mayara
  // integration wired up simply omits it.
  radarTargets?: RadarTarget[]
  // Optional, mirroring AnchorWatchMapProps — a caller with no mayara
  // integration wired up simply omits it.
  radars?: RadarInfo[]
  radarSource?: RadarSource
  isDarkTheme: boolean
  showImageryLayer: boolean
  onImageryToggle: (enabled: boolean) => void
  showRadarEcho: boolean
  onRadarEchoToggle: (enabled: boolean) => void
  onFullscreen: () => void
  placemarks?: AnchorPlacemark[]
  onPlacemarkCreate?: (lat: number, lon: number) => void
  onPlacemarkRemove?: (id: string) => void
  tide: TideToday | null
  /**
   * False on the wall kiosk (ADR: kiosk maps are display-only) - passed
   * straight through to AnchorWatchMap, which then hides its own on-map
   * controls and drops maplibre's interactive handlers. Defaults to true so
   * every existing host keeps today's behaviour.
   */
  interactive?: boolean
  /**
   * Seconds since the position/anchor-watch feed last updated, or null when
   * the host has no age to report. There is currently no upstream source
   * for this — the anchor-watch API (backend/anchor.go) carries no
   * last-update timestamp, and neither does the live GPS fix this tile
   * otherwise renders from — so every caller today passes null, which
   * `isStale`/`formatDataAge` already treat as "not stale" rather than a
   * fabricated freshness. Typed and wired through now so the tile can go
   * stale the moment a real age is published, without inventing one here.
   */
  lastUpdateAgeS: number | null
}

export const AnchorWatchTile = memo(function AnchorWatchTile({
  watch,
  lat,
  lon,
  depthMeters,
  currentDriftKts,
  currentSetDeg,
  currentDriftImpactKts = null,
  isImperial,
  vesselHeadingDeg,
  vesselTrail,
  aisVessels,
  aisTrails,
  aisCollisionAlarms,
  radarTargets,
  radars,
  radarSource,
  isDarkTheme,
  showImageryLayer,
  onImageryToggle,
  showRadarEcho,
  onRadarEchoToggle,
  onFullscreen,
  placemarks,
  onPlacemarkCreate,
  onPlacemarkRemove,
  tide,
  interactive = true,
  lastUpdateAgeS,
}: AnchorWatchTileProps) {
  const {
    anchorState,
    gnssCritical,
    anchorLat,
    anchorLon,
    setAt,
    loaded,
    error: watchError,
    radiusMeters,
    distanceMeters,
    bearingDeg,
    setAnchorHere,
  } = watch

  // Drag detection is server-side (ADR 0038); this renders the audible half and
  // silences by acknowledging, so every screen agrees. isAlarming/isSilenced
  // are mutually exclusive — audible-and-visible vs. active-but-silenced —
  // and neither implies the other cleared: a silenced drag is still a live
  // condition and must keep rendering something (see the strips below).
  const dragAlarm = useAnchorDragAlarm()
  const { isAlarming, isSilenced, silence, unsilence } = useAnchorAlarm(dragAlarm, acknowledgeAlarm)

  // "38 m of 30 m" — how far the vessel actually is against the radius it's
  // meant to stay inside. Shared by both drag-alarm strips below.
  const dragDistanceLabel = formatDragDistance(distanceMeters, radiusMeters, isImperial)

  const handleDropHere = useCallback(() => {
    if (lat === null || lon === null) return
    void setAnchorHere(lat, lon, {
      planningDepthM: depthMeters,
      planningTideHeightFt: tideHeightFtOrNull(tide),
    })
  }, [lat, lon, setAnchorHere, depthMeters, tide])

  // Nothing truthful to center a map on without either a live fix or an
  // anchor point — falls back from the live fix to the anchor (e.g. GPS lost
  // after the anchor was already set) rather than fabricating 0,0.
  const vesselLat = lat ?? anchorLat
  const vesselLon = lon ?? anchorLon

  return (
    <Tile
      title="Anchor Watch"
      icon={<Anchor className="h-3.5 w-3.5" />}
      stale={isStale(lastUpdateAgeS)}
      staleLabel={formatDataAge(lastUpdateAgeS)}
    >
      {/* At lg+ the dashboard grid hands this tile a fixed height (RGL wraps
          widgets in h-full), so the content is a flex column and the map is the
          one row that gives — otherwise the rode readout and Drop button
          overflow the card. Below lg the persisted height is only a floor
          (dashboard-bento-grid.tsx) and the map keeps its fixed h-64. */}
      <div className="flex h-full min-h-0 flex-col">
      {/* The backend puts a damaged anchor_watch.json into an explicit error
          state rather than an invented or empty watch — never silently
          rendered as the ordinary "no watch set" tile. title carries the
          full message (including the file path) for a hover/long-press, the
          line itself truncates so a long path never breaks the tile's
          fixed-height layout. */}
      {watchError && (
        <div
          role="alert"
          data-testid="anchor-watch-error-banner"
          title={watchError}
          className="mb-3 rounded-md border border-red-500/60 bg-red-500/10 px-3 py-2 text-xs text-red-400"
        >
          <p className="font-semibold">Saved anchor watch unreadable</p>
          <p className="truncate">{watchError}</p>
        </div>
      )}
      {anchorState !== 'none' && gnssCritical && (
        <div className="mb-3 rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-500">
          GPS signal degraded — position may be inaccurate
        </div>
      )}
      {/* Audible and visible: a live drag, not yet silenced. */}
      {isAlarming && (
        <div
          role="alert"
          data-testid="drag-alarm-strip"
          className="mb-3 flex items-center justify-between gap-2 rounded-md border border-red-500/60 bg-red-500/20 px-3 py-2"
        >
          <div className="flex min-w-0 items-center gap-2">
            <div className="h-2 w-2 shrink-0 animate-pulse rounded-full bg-red-500" />
            <span className="truncate text-xs font-semibold text-red-400">
              Dragging — {dragDistanceLabel}
            </span>
          </div>
          <Button
            size="sm"
            variant="outline"
            className="shrink-0 gap-1 text-xs text-red-400 hover:bg-red-500/20 hover:text-red-300"
            onClick={silence}
          >
            <Volume2 className="h-3.5 w-3.5" />
            Silence
          </Button>
        </div>
      )}

      {/* P0: the same live drag, acknowledged. Silencing stops the klaxon,
          not the condition, so this must keep the board from looking calm —
          it is the direct replacement for the red strip disappearing with
          nothing standing in for it. */}
      {isSilenced && (
        <div
          role="alert"
          data-testid="drag-silenced-strip"
          className="mb-3 flex items-center justify-between gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2"
        >
          <div className="flex min-w-0 items-center gap-2">
            <div className="h-2 w-2 shrink-0 rounded-full bg-amber-500" />
            <span className="truncate text-xs font-semibold text-amber-500">
              Dragging, silenced — {dragDistanceLabel}
            </span>
          </div>
          <Button
            size="sm"
            variant="outline"
            className="shrink-0 gap-1 text-xs text-amber-500 hover:bg-amber-500/20"
            onClick={unsilence}
          >
            <Volume2 className="h-3.5 w-3.5" />
            Unsilence
          </Button>
        </div>
      )}

      {/* No distance KPI here (the map's own overlay already carries
          distance) and no low-water clearance line, which lives on the
          Anchor Watch page (the drawer). The tile stays a map. */}
      <div className="mt-2 rounded-xl border bg-background/70 lg:min-h-0 lg:flex-1">
        {vesselLat !== null && vesselLon !== null ? (
          // Fallback matches AnchorWatchMap's own className exactly, so the
          // map slot holds its size while the map-vendor chunk loads and the
          // layout never jumps once it resolves.
          <Suspense
            fallback={
              <div
                className="h-64 w-full animate-pulse rounded-lg bg-muted/40 lg:h-full"
                data-testid="anchor-watch-map-loading"
              />
            }
          >
            <AnchorWatchMap
              vesselLat={vesselLat}
              vesselLon={vesselLon}
              vesselHeadingDeg={vesselHeadingDeg}
              anchorLat={anchorLat}
              anchorLon={anchorLon}
              anchorSetAt={setAt}
              anchorStateKnown={loaded}
              radiusMeters={radiusMeters}
              depthMeters={depthMeters}
              currentDriftKts={currentDriftKts}
              currentSetDeg={currentSetDeg}
              currentDriftImpactKts={currentDriftImpactKts}
              distanceMeters={distanceMeters}
              bearingDeg={bearingDeg}
              isImperial={isImperial}
              vesselTrail={vesselTrail}
              aisVessels={aisVessels}
              aisTrails={aisTrails}
              aisCollisionAlarms={aisCollisionAlarms}
              radarTargets={radarTargets}
              radars={radars}
              radarSource={radarSource}
              isDarkTheme={isDarkTheme}
              showImageryLayer={showImageryLayer}
              onImageryToggle={onImageryToggle}
              showRadarEcho={showRadarEcho}
              onRadarEchoToggle={onRadarEchoToggle}
              onFullscreen={onFullscreen}
              placemarks={placemarks}
              onPlacemarkCreate={onPlacemarkCreate}
              onPlacemarkRemove={onPlacemarkRemove}
              interactive={interactive}
              // Namespaces the persisted fitted-zoom key (code-review
              // finding) so this tile's own fit for its own size never
              // clobbers, or gets clobbered by, the fullscreen drawer's.
              viewKey="tile"
              className="h-64 w-full rounded-lg lg:h-full"
            />
          </Suspense>
        ) : (
          <div className="flex h-64 items-center justify-center text-sm text-muted-foreground lg:h-full">
            No GPS fix
          </div>
        )}
      </div>

      {/* Idle, the footer holds Drop: full width, at the moment it matters.
          With a watch active the tile is a monitor only. Raise lives on the
          Anchor Watch page, so a stray touch on a dashboard tile can't end
          the watch. */}
      {anchorState === 'none' && (
        <div className="mt-2 shrink-0">
          <AnchorDropRaiseButton
            className="w-full"
            anchorActive={false}
            canDrop={lat !== null && lon !== null}
            onDrop={handleDropHere}
          />
        </div>
      )}
      </div>
    </Tile>
  )
})

const ANCHOR_FIELDS = [
  'positionLastUpdateAgeS', 'latitude', 'longitude', 'gnssCriticalAlert', 'depth',
  'currentDriftKts', 'currentSetDeg', 'currentDriftImpactKts', 'headingTrue',
] as const

/**
 * The anchor watch tile. It reads the watch record, applies the live fix to it
 * (distance, bearing, drag state), and adds depth, current, heading, the tide
 * a drop records, the collision colours, the pins, the trails and the AIS and
 * radar contacts. The trails poll while the tile is mounted.
 */
export const LiveAnchorWatchTile = memo(function LiveAnchorWatchTile(props: Omit<ComponentProps<typeof AnchorWatchTile>,
  'watch' | 'aisCollisionAlarms' | 'lastUpdateAgeS' | 'lat' | 'lon' | 'depthMeters' | 'currentDriftKts' | 'currentSetDeg' | 'currentDriftImpactKts' | 'vesselHeadingDeg'
  | 'vesselTrail' | 'aisTrails' | 'placemarks' | 'onPlacemarkCreate' | 'onPlacemarkRemove'
  | 'aisVessels' | 'radarTargets' | 'radars' | 'radarSource' | 'tide'>) {
  const watch = useAnchorWatch()
  const contacts = useMapContacts()
  const v = useVesselFields(ANCHOR_FIELDS)
  const { tide } = useTideToday(FORECAST_REFRESH_SECONDS)
  const aisCollisionAlarms = useAisCollisionAlarms()
  const atFix = useWatchAtFix(watch, v.latitude, v.longitude, v.gnssCriticalAlert)
  const anchorTrail = useAnchorTrail(TRAIL_POLL_MS)
  const aisTrails = useAisTrails(TRAIL_POLL_MS)
  const pins = usePins(watch.active)
  return (
    <AnchorWatchTile
      {...props}
      {...pins}
      {...contacts}
      tide={tide}
      watch={atFix}
      vesselTrail={anchorTrail}
      aisTrails={aisTrails}
      lastUpdateAgeS={v.positionLastUpdateAgeS}
      lat={v.latitude}
      lon={v.longitude}
      depthMeters={v.depth}
      currentDriftKts={v.currentDriftKts}
      currentSetDeg={v.currentSetDeg}
      currentDriftImpactKts={v.currentDriftImpactKts}
      vesselHeadingDeg={v.headingTrue}
      aisCollisionAlarms={aisCollisionAlarms}
    />
  )
})
