import { AnchorWatchMap } from '@/components/anchor-watch-map'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { TrailPoint } from '@/hooks/use-server-trails'

interface AnchorWatchDrawerProps {
  vesselLat: number
  vesselLon: number
  vesselHeadingDeg: number | null
  anchorLat: number
  anchorLon: number
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
}: AnchorWatchDrawerProps) {
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
      <div className="flex-1 rounded-xl border bg-background/70">
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
      </div>
    </div>
  )
}

