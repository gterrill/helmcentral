import { Anchor } from 'lucide-react'
import type { AnchorConfig } from '@/config/app-config'
import type { AnchorWatchState } from '@/hooks/use-anchor-watch'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { TrailPoint } from '@/hooks/use-server-trails'
import type { TideToday } from '@/hooks/use-tide-today'
import type { GustWindow } from '@/lib/gust-windows'
import type { SeabedType, SeaState } from '@/lib/catenary'
import { AnchorRodePlanner } from '@/components/anchor-rode-planner'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import { Button } from '@/components/ui/button'

interface AnchorWatchDrawerProps {
  vesselLat: number
  vesselLon: number
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
  onClearAnchor: () => void
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
  onUpdateRodeAndConditions,
}: AnchorWatchDrawerProps) {
  const isSet = anchorLat !== null && anchorLon !== null

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
        <div className="min-w-0 flex-1 rounded-xl border bg-background/70">
          {isSet ? (
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
              isImperial={isImperial}
              vesselTrail={vesselTrail}
              aisVessels={aisVessels}
              aisTrails={aisTrails}
              isDarkTheme={isDarkTheme}
              showImageryLayer={showImageryLayer}
              onImageryToggle={onImageryToggle}
              onAnchorReposition={onAnchorReposition}
              onRadiusChange={onRadiusChange}
              onClearAnchor={onClearAnchor}
              className="h-full w-full"
            />
          ) : (
            <div className="flex h-full flex-col items-center justify-center px-6 py-8 text-center text-muted-foreground">
              <div className="mb-4">Not monitoring</div>
              <Button
                className="h-11 bg-teal-600 text-teal-50 hover:bg-teal-700"
                disabled={!canDrop}
                onClick={onDropAnchor}
              >
                <Anchor className="h-4 w-4" />
                Drop
              </Button>
            </div>
          )}
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
          onUpdateRodeAndConditions={onUpdateRodeAndConditions}
          onApplyAlarmRadius={async (radius) => onRadiusChange(radius)}
        />
      </div>
    </div>
  )
}
