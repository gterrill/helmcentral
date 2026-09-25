import { useMemo } from 'react'
import type { AnchorConfig } from '@/config/app-config'
import type { AnchorWatchState } from '@/hooks/use-anchor-watch'
import type { AlarmState } from '@/hooks/use-alarms'
import type { AnchorPlacemark } from '@/hooks/use-anchor-placemarks'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { RadarInfo, RadarSource, RadarTarget } from '@/hooks/use-radar-targets'
import type { TrailPoint } from '@/hooks/use-server-trails'
import type { TideToday } from '@/hooks/use-tide-today'
import type { GustWindow } from '@/lib/gust-windows'
import type { SeabedType, SeaState } from '@/lib/catenary'
import { AnchorDropRaiseButton } from '@/components/anchor-drop-raise-button'
import { AnchorRodePlanner } from '@/components/anchor-rode-planner'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import { computeScopeRecommendation } from '@/lib/rode-plan'

/**
 * A distance in the host's chosen unit, rounded for display — same shape as
 * anchor-watch-tile.tsx's helper of the same name. Duplicated locally rather
 * than imported: that file isn't exported from and isn't owned here.
 */
function formatDistanceValue(meters: number, isImperial: boolean): { value: string; unit: string } {
  return isImperial
    ? { value: `${Math.round(meters * 3.28084)}`, unit: 'ft' }
    : { value: `${Math.round(meters)}`, unit: 'm' }
}

interface AnchorWatchDrawerProps {
  // Resolved by the caller (live fix, falling back to the anchor point), and
  // left null only when neither is available — see the no-fix placeholder
  // below rather than being handed a fabricated 0,0.
  vesselLat: number | null
  vesselLon: number | null
  vesselHeadingDeg: number | null
  anchorLat: number | null
  anchorLon: number | null
  radiusMeters: number
  depthMeters: number | null
  currentDriftKts: number | null
  currentSetDeg: number | null
  currentDriftImpactKts?: number | null
  distanceMeters: number | null
  bearingDeg: number | null
  bowOffsetM?: number
  bowOffsetApplied?: boolean
  bowOffsetReason?: string
  // The watch's set_at, passed straight through to the map: it centres on
  // the anchor whenever the session changes.
  anchorSetAt: string | null
  // useAnchorWatch's own `loaded` — whether the first GET /api/anchor-watch
  // has actually resolved, passed straight through to the map so it can
  // tell "no watch" from "haven't heard back yet" (anchorSetAt alone can't).
  anchorStateKnown: boolean
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
  onAnchorReposition: (lat: number, lon: number) => void
  onRadiusChange: (radiusMeters: number) => void
  onClearAnchor: () => Promise<void> | void
  placemarks?: AnchorPlacemark[]
  onPlacemarkCreate?: (lat: number, lon: number) => void
  onPlacemarkRemove?: (id: string) => void
  isImperial: boolean
  onDropAnchor: () => void
  canDrop: boolean
  anchorState: AnchorWatchState
  rodeDeployedM: number
  seaState: SeaState
  seabedType: SeabedType
  windSpeedApparentKts: number | null
  maxGustKts: Record<GustWindow, number | null>
  tide: TideToday | null
  anchorConfig: AnchorConfig
  vesselLengthOverallM: number | null
  // Shared with the tile and the Rode Planner (App.tsx owns the state) so
  // all three plan against the same forecast band. Null when there's no
  // explicit pick — computeScopeRecommendation falls back to the live seed.
  windBandId: string | null
  onWindBandChange: (bandId: string) => void
  onUpdateRodeAndConditions: (rodeDeployedM: number, seaState: SeaState, seabedType: SeabedType) => Promise<void>
  // The resolved planning depth (App.tsx owns it — ADR 0063): the persisted
  // watch record while anchored, the not-anchored session what-if otherwise.
  // Feeds both the map's Scope row here and the Rode Planner below it, so the
  // two can't disagree.
  planningDepthM: number | null
  planningTideHeightFt: number | null
  onPlanningDepthChange: (depthM: number, tideHeightFt: number | null) => void
}

export function AnchorWatchDrawer({
  vesselLat,
  vesselLon,
  vesselHeadingDeg,
  anchorLat,
  anchorLon,
  radiusMeters,
  depthMeters,
  currentDriftKts,
  currentSetDeg,
  currentDriftImpactKts = null,
  distanceMeters,
  bearingDeg,
  bowOffsetM = 0,
  bowOffsetApplied = false,
  bowOffsetReason = '',
  anchorSetAt,
  anchorStateKnown,
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
  onAnchorReposition,
  onRadiusChange,
  onClearAnchor,
  placemarks,
  onPlacemarkCreate,
  onPlacemarkRemove,
  isImperial,
  onDropAnchor,
  canDrop,
  anchorState,
  rodeDeployedM,
  seaState,
  seabedType,
  windSpeedApparentKts,
  maxGustKts,
  tide,
  anchorConfig,
  vesselLengthOverallM,
  windBandId,
  onWindBandChange,
  onUpdateRodeAndConditions,
  planningDepthM,
  planningTideHeightFt,
  onPlanningDepthChange,
}: AnchorWatchDrawerProps) {
  const isAnchored = anchorState !== 'none'

  // Rendered by the map's metric overlay as the Scope row, under Current
  // (ADR 0059 §3) — shared with the tile via computeScopeRecommendation, and
  // with the Rode Planner below via windBandId, so all three plan against
  // the same forecast band and the same resolved depth (ADR 0063).
  const scopeRecommendation = useMemo(
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
        selectedWindBandId: windBandId,
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
      windBandId,
    ],
  )

  return (
    <div className="flex h-full flex-col gap-3">
      {bowOffsetApplied && (
        <div className="rounded-md border border-border bg-background/60 px-3 py-2 text-xs text-muted-foreground">
          Anchor point corrected {Math.round(bowOffsetM)}m forward of GPS — radius should cover rode + {Math.round(bowOffsetM)}m.
        </div>
      )}
      {!bowOffsetApplied && bowOffsetReason === 'heading unavailable' && (
        <div className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-500">
          Anchor point not bow-corrected — no heading from SignalK.
        </div>
      )}

      <div className="flex min-h-0 flex-1 flex-col gap-3 lg:flex-row">
        <div className="flex min-w-0 flex-1 flex-col gap-3">
          {/* Same promotion as anchor-watch-tile.tsx's KPI stack, and for
              the same reason: the map's own metric overlay dropped its
              Distance row (design critique item 1, it duplicated this),
              which would otherwise leave the drawer with no distance
              readout at all. */}
          {isAnchored && (
            <div
              data-testid="anchor-distance-kpi"
              className="flex items-baseline gap-3 rounded-md border bg-background/60 px-3 py-2"
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
          <div className="min-h-0 flex-1 rounded-xl border bg-background/70">
            {vesselLat !== null && vesselLon !== null ? (
              <AnchorWatchMap
                vesselLat={vesselLat}
                vesselLon={vesselLon}
                vesselHeadingDeg={vesselHeadingDeg}
                anchorLat={anchorLat}
                anchorLon={anchorLon}
                anchorSetAt={anchorSetAt}
                anchorStateKnown={anchorStateKnown}
                radiusMeters={radiusMeters}
                depthMeters={depthMeters}
                currentDriftKts={currentDriftKts}
                currentSetDeg={currentSetDeg}
                currentDriftImpactKts={currentDriftImpactKts}
                distanceMeters={distanceMeters}
                bearingDeg={bearingDeg}
                scopeRecommendation={scopeRecommendation}
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
                onAnchorReposition={onAnchorReposition}
                onRadiusChange={onRadiusChange}
                // The tile trims its own in-map stack to fullscreen+zoom
                // (design critique item 3); this drawer IS the fullscreen
                // view, so it gets satellite/radar/recentre back — every
                // control stays reachable, just relocated.
                expandedControls
                placemarks={placemarks}
                onPlacemarkCreate={onPlacemarkCreate}
                onPlacemarkRemove={onPlacemarkRemove}
                className="h-full w-full"
              />
            ) : (
              <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
                No GPS fix
              </div>
            )}
          </div>

          {/* Prominence follows state (see anchor-watch-tile.tsx): Drop is the
              idle state's one action and gets a generous centered target; Raise
              is a confirmed departure chore and sits compact at the trailing
              edge rather than spanning the whole map column. */}
          <div className={anchorState === 'none' ? 'flex justify-center' : 'flex justify-end'}>
            <AnchorDropRaiseButton
              className={anchorState === 'none' ? 'w-full max-w-xs' : undefined}
              anchorActive={anchorState !== 'none'}
              canDrop={canDrop}
              onDrop={onDropAnchor}
              onRaise={onClearAnchor}
            />
          </div>
        </div>

        <AnchorRodePlanner
          anchorState={anchorState}
          rodeDeployedM={rodeDeployedM}
          seaState={seaState}
          seabedType={seabedType}
          depthM={depthMeters}
          planningDepthM={planningDepthM}
          planningTideHeightFt={planningTideHeightFt}
          onPlanningDepthChange={onPlanningDepthChange}
          windSpeedApparentKts={windSpeedApparentKts}
          maxGustKts={maxGustKts}
          tide={tide}
          isImperial={isImperial}
          anchorConfig={anchorConfig}
          bowOffsetM={bowOffsetM}
          vesselLengthOverallM={vesselLengthOverallM}
          windBandId={windBandId}
          onWindBandChange={onWindBandChange}
          onUpdateRodeAndConditions={onUpdateRodeAndConditions}
          onApplyAlarmRadius={async (radius) => onRadiusChange(radius)}
        />
      </div>
    </div>
  )
}
