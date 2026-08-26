import { useMemo } from 'react'
import type { AnchorConfig } from '@/config/app-config'
import type { AnchorWatchState } from '@/hooks/use-anchor-watch'
import type { AnchorPlacemark } from '@/hooks/use-anchor-placemarks'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { TrailPoint } from '@/hooks/use-server-trails'
import type { TideToday } from '@/hooks/use-tide-today'
import type { GustWindow } from '@/lib/gust-windows'
import type { SeabedType, SeaState } from '@/lib/catenary'
import { AnchorDropRaiseButton } from '@/components/anchor-drop-raise-button'
import { AnchorRodePlanner } from '@/components/anchor-rode-planner'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import { computeScopeRecommendation } from '@/lib/rode-plan'

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
  anchorSetAt?: never  // removed — motoring track fetched by map on reposition
  vesselTrail: () => TrailPoint[]
  aisVessels: NearbyVessel[]
  aisTrails: () => Map<string, TrailPoint[]>
  isDarkTheme: boolean
  showImageryLayer: boolean
  onImageryToggle: (enabled: boolean) => void
  onAnchorReposition: (lat: number, lon: number) => void
  onRadiusChange: (radiusMeters: number) => void
  onClearAnchor: () => Promise<void> | void
  placemarks?: AnchorPlacemark[]
  onPlacemarkCreate?: (lat: number, lon: number) => void
  onPlacemarkRemove?: (id: string) => void
  isImperial: boolean
  isAutoCloseArmed: boolean
  motoringSecondsElapsed: number
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
  vesselTrail,
  aisVessels,
  aisTrails,
  isDarkTheme,
  showImageryLayer,
  onImageryToggle,
  onAnchorReposition,
  onRadiusChange,
  onClearAnchor,
  placemarks,
  onPlacemarkCreate,
  onPlacemarkRemove,
  isImperial,
  isAutoCloseArmed,
  motoringSecondsElapsed,
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
}: AnchorWatchDrawerProps) {
  // Rendered by the map's metric overlay as the Scope row, under Current
  // (ADR 0059 §3) — shared with the tile via computeScopeRecommendation, and
  // with the Rode Planner below via windBandId, so all three plan against
  // the same forecast band.
  const scopeRecommendation = useMemo(
    () =>
      computeScopeRecommendation({
        depthMeters,
        tide,
        maxGustKts,
        windSpeedApparentKts,
        seaState,
        seabedType,
        anchorConfig,
        selectedWindBandId: windBandId,
      }),
    [depthMeters, tide, maxGustKts, windSpeedApparentKts, seaState, seabedType, anchorConfig, windBandId],
  )

  return (
    <div className="flex h-full flex-col gap-3">
      {isAutoCloseArmed && (
        <div className="rounded-md border border-yellow-500/40 bg-yellow-500/10 px-3 py-2 text-xs text-yellow-600">
          <div className="flex items-center justify-between">
            <span className="font-semibold">Auto-close armed</span>
            <span className="font-mono">{5 - motoringSecondsElapsed}s</span>
          </div>
          <p className="mt-1 text-yellow-600/80">Engines running • Outside circle • Will clear shortly</p>
        </div>
      )}
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
          <div className="min-h-0 flex-1 rounded-xl border bg-background/70">
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
                scopeRecommendation={scopeRecommendation}
                isImperial={isImperial}
                vesselTrail={vesselTrail}
                aisVessels={aisVessels}
                aisTrails={aisTrails}
                isDarkTheme={isDarkTheme}
                showImageryLayer={showImageryLayer}
                onImageryToggle={onImageryToggle}
                onAnchorReposition={onAnchorReposition}
                onRadiusChange={onRadiusChange}
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
