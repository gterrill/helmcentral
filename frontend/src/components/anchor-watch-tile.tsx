import { Anchor, Volume2 } from 'lucide-react'
import { lazy, memo, Suspense, useCallback, useMemo } from 'react'
import { Button } from '@/components/ui/button'
import { Tile } from '@/components/ui/tile'
import { AnchorDropRaiseButton } from '@/components/anchor-drop-raise-button'
import { findAnchorDragAlarm, useAlarms, type AlarmState } from '@/hooks/use-alarms'
import { useAnchorAlarm } from '@/hooks/use-anchor-alarm'
import type { AnchorWatchResult } from '@/hooks/use-anchor-watch'
import type { AnchorPlacemark } from '@/hooks/use-anchor-placemarks'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { RadarInfo, RadarSource, RadarTarget } from '@/hooks/use-radar-targets'
import type { TrailPoint } from '@/hooks/use-server-trails'
import type { TideToday } from '@/hooks/use-tide-today'
import type { GustWindow } from '@/lib/gust-windows'
import type { AnchorConfig } from '@/config/app-config'
import { computeScopeRecommendation, tideHeightFtOrNull } from '@/lib/rode-plan'
import { formatDataAge, isStale } from '@/lib/staleness'

// anchor-watch-map.tsx imports maplibre-gl and react-map-gl at module scope
// (map-vendor: 1048 KB raw / 280 KB gzip), and this tile is mounted eagerly
// by App.tsx (it's the dashboard's 'anchor-watch' case, not behind any
// React.lazy panel) - so that whole chunk used to load at startup on every
// route, including /kiosk pages with no anchor-watch tile on them. Same lazy
// -split pattern as assistant-markdown.tsx/help-markdown.tsx, just with
// the boundary drawn inside this component instead of a matching *-impl
// file: the tile's own chrome (alarm strips, distance KPI, Drop/Raise) stays
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
 * "38 m of 30 m" — the figure both the drag-alarm strips and the promoted
 * KPI stack below need: how far the vessel actually is, against the radius
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
  vesselTrail: () => TrailPoint[]
  aisVessels: NearbyVessel[]
  aisTrails: () => Map<string, TrailPoint[]>
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
  windSpeedApparentKts: number | null
  maxGustKts: Record<GustWindow, number | null>
  anchorConfig: AnchorConfig
  // The band the operator picked in the Rode Planner (App.tsx owns it), so
  // this tile's Scope row plans against the same band the planner shows
  // rather than seeding its own raw wind. Null when there's no explicit pick
  // — computeScopeRecommendation falls back to the live seed.
  selectedWindBandId: string | null
  // The resolved planning depth (App.tsx owns it — ADR 0063): the persisted
  // watch record while anchored, the not-anchored session what-if otherwise.
  // This tile has no control to set it, only to plan against it — the same
  // resolved figure reaching the drawer and the planner is what keeps all
  // three surfaces in agreement.
  planningDepthM: number | null
  planningTideHeightFt: number | null
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
  windSpeedApparentKts,
  maxGustKts,
  anchorConfig,
  selectedWindBandId,
  planningDepthM,
  planningTideHeightFt,
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
    clearAnchor,
    seaState,
    seabedType,
  } = watch

  // Drag detection is server-side (ADR 0038); this renders the audible half and
  // silences by acknowledging, so every screen agrees. isAlarming/isSilenced
  // are mutually exclusive — audible-and-visible vs. active-but-silenced —
  // and neither implies the other cleared: a silenced drag is still a live
  // condition and must keep rendering something (see the strips below).
  const { alarms, acknowledge } = useAlarms()
  const { isAlarming, isSilenced, silence, unsilence } = useAnchorAlarm(findAnchorDragAlarm(alarms), acknowledge)

  // "38 m of 30 m" — how far the vessel actually is against the radius it's
  // meant to stay inside. Shared by both drag-alarm strips below and, once
  // anchored, the promoted distance KPI above the map.
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

  const isAnchored = anchorState !== 'none'

  // Rendered by the map's metric overlay as the Scope row, under Current
  // (ADR 0059 §3) — this tile no longer renders the readout itself.
  const rodeResult = useMemo(
    () =>
      computeScopeRecommendation({
        isAnchored,
        liveDepthM: depthMeters,
        planningDepthM,
        planningTideHeightFt,
        tide,
        maxGustKts,
        windSpeedApparentKts,
        seaState,
        seabedType,
        anchorConfig,
        selectedWindBandId,
      }),
    [
      isAnchored,
      depthMeters,
      planningDepthM,
      planningTideHeightFt,
      tide,
      maxGustKts,
      windSpeedApparentKts,
      seaState,
      seabedType,
      anchorConfig,
      selectedWindBandId,
    ],
  )

  return (
    <Tile
      title="Anchor Watch"
      icon={<Anchor className="h-3.5 w-3.5" />}
      stale={isStale(lastUpdateAgeS)}
      staleLabel={formatDataAge(lastUpdateAgeS)}
    >
      {/* At lg+ the dashboard grid hands this tile a fixed height (RGL wraps
          widgets in h-full), so the content is a flex column and the map is the
          one row that gives — otherwise the rode readout and Drop/Raise button
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

      {/* Promoted out of the map (design critique item 4): the operator
          answers "is the boat where I left it" from this hero readout, not
          by parsing the map's own overlay panel. Suppressed with no anchor
          set rather than rendering a dash at full visual weight — there is
          nothing to promote yet. */}
      {isAnchored && (
        <div
          data-testid="anchor-distance-kpi"
          className="mt-2 flex items-baseline gap-3 rounded-md border bg-background/60 px-3 py-2"
        >
          <div className="min-w-0">
            <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Distance</p>
            <p className="font-display text-5xl leading-none tabular-nums text-gauge-primary">
              {distanceMeters !== null ? formatDistanceValue(distanceMeters, isImperial).value : '—'}
              <span className="ml-1 text-lg text-muted-foreground">
                {distanceMeters !== null ? formatDistanceValue(distanceMeters, isImperial).unit : ''}
              </span>
            </p>
          </div>
          <p className="text-sm text-muted-foreground">
            of {formatDistanceValue(radiusMeters, isImperial).value} {formatDistanceValue(radiusMeters, isImperial).unit}
          </p>
        </div>
      )}

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
              scopeRecommendation={rodeResult}
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
              className="h-64 w-full rounded-lg lg:h-full"
            />
          </Suspense>
        ) : (
          <div className="flex h-64 items-center justify-center text-sm text-muted-foreground lg:h-full">
            No GPS fix
          </div>
        )}
      </div>

      {/* The footer holds the current state's primary action. Idle, that is
          Drop: full width, at the moment it matters. With a watch active the
          tile is a monitor and has no primary action, so Raise — a once-per-
          anchorage departure chore whose accidental press the confirm dialog
          exists to guard — sits compact at the trailing edge instead of
          dominating the tile for the life of the watch (matching the
          drawer's footer). */}
      {anchorState === 'none' ? (
        <div className="mt-2 shrink-0">
          <AnchorDropRaiseButton
            className="w-full"
            anchorActive={false}
            canDrop={lat !== null && lon !== null}
            onDrop={handleDropHere}
            onRaise={clearAnchor}
          />
        </div>
      ) : (
        <div className="mt-2 flex shrink-0 justify-end">
          <AnchorDropRaiseButton
            anchorActive
            canDrop={lat !== null && lon !== null}
            onDrop={handleDropHere}
            onRaise={clearAnchor}
          />
        </div>
      )}
      </div>
    </Tile>
  )
})
