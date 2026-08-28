import { Anchor, Volume2 } from 'lucide-react'
import { memo, useCallback, useMemo } from 'react'
import { Button } from '@/components/ui/button'
import { Tile } from '@/components/ui/tile'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import { AnchorDropRaiseButton } from '@/components/anchor-drop-raise-button'
import { findAnchorDragAlarm, useAlarms } from '@/hooks/use-alarms'
import { useAnchorAlarm } from '@/hooks/use-anchor-alarm'
import type { AnchorWatchResult } from '@/hooks/use-anchor-watch'
import type { AnchorPlacemark } from '@/hooks/use-anchor-placemarks'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { TrailPoint } from '@/hooks/use-server-trails'
import type { TideToday } from '@/hooks/use-tide-today'
import type { GustWindow } from '@/lib/gust-windows'
import type { AnchorConfig } from '@/config/app-config'
import { computeScopeRecommendation, tideHeightFtOrNull } from '@/lib/rode-plan'

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
  isDarkTheme: boolean
  showImageryLayer: boolean
  onImageryToggle: (enabled: boolean) => void
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
  isDarkTheme,
  showImageryLayer,
  onImageryToggle,
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
}: AnchorWatchTileProps) {
  const {
    anchorState,
    gnssCritical,
    anchorLat,
    anchorLon,
    radiusMeters,
    distanceMeters,
    bearingDeg,
    suggestSet,
    setAnchorHere,
    updatePosition,
    updateRadius,
    clearAnchor,
    seaState,
    seabedType,
  } = watch

  // Drag detection is server-side (ADR 0038); this renders the audible half and
  // silences by acknowledging, so every screen agrees.
  const { alarms, acknowledge } = useAlarms()
  const { isAlarming, isSilenced, silence } = useAnchorAlarm(findAnchorDragAlarm(alarms), acknowledge)

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
    <Tile title="Anchor Watch" icon={<Anchor className="h-3.5 w-3.5" />}>
      {/* At lg+ the dashboard grid hands this tile a fixed height (RGL wraps
          widgets in h-full), so the content is a flex column and the map is the
          one row that gives — otherwise the rode readout and Drop/Raise button
          overflow the card. Below lg the persisted height is only a floor
          (dashboard-bento-grid.tsx) and the map keeps its fixed h-64. */}
      <div className="flex h-full min-h-0 flex-col">
      {anchorState === 'none' && suggestSet && (
        <div className="mb-3 rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-500">
          SignalK reports <span className="font-semibold">anchored</span> — set anchor watch?
        </div>
      )}
      {anchorState !== 'none' && gnssCritical && (
        <div className="mb-3 rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-500">
          GPS signal degraded — position may be inaccurate
        </div>
      )}
      {isAlarming && !isSilenced && (
        <div className="mb-3 flex items-center justify-between rounded-md border border-red-500/60 bg-red-500/20 px-3 py-2">
          <div className="flex items-center gap-2">
            <div className="h-2 w-2 animate-pulse rounded-full bg-red-500" />
            <span className="text-xs font-semibold text-red-400">ALARM</span>
          </div>
          <Button
            size="sm"
            variant="outline"
            className="h-8 gap-1 rounded px-2 text-xs text-red-400 hover:bg-red-500/20 hover:text-red-300"
            onClick={silence}
          >
            <Volume2 className="h-3.5 w-3.5" />
            Silence
          </Button>
        </div>
      )}

      <div className="mt-2 rounded-xl border bg-background/70 lg:min-h-0 lg:flex-1">
        {vesselLat !== null && vesselLon !== null ? (
          <AnchorWatchMap
            vesselLat={vesselLat}
            vesselLon={vesselLon}
            vesselHeadingDeg={vesselHeadingDeg}
            anchorLat={anchorLat}
            anchorLon={anchorLon}
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
            isDarkTheme={isDarkTheme}
            showImageryLayer={showImageryLayer}
            onImageryToggle={onImageryToggle}
            onAnchorReposition={updatePosition}
            onRadiusChange={updateRadius}
            onFullscreen={onFullscreen}
            placemarks={placemarks}
            onPlacemarkCreate={onPlacemarkCreate}
            onPlacemarkRemove={onPlacemarkRemove}
            className="h-64 w-full rounded-lg lg:h-full"
          />
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
