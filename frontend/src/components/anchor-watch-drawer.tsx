import { AnchorWatchMap } from '@/components/anchor-watch-map'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { TrailPoint } from '@/hooks/use-vessel-trail'

interface AnchorWatchDrawerProps {
  vesselLat: number
  vesselLon: number
  vesselHeadingDeg: number | null
  anchorLat: number
  anchorLon: number
  radiusMeters: number
  distanceMeters: number | null
  bearingDeg: number | null
  vesselTrail: () => TrailPoint[]
  aisVessels: NearbyVessel[]
  aisTrails: () => Map<string, TrailPoint[]>
  isDarkTheme: boolean
  onAnchorReposition: (lat: number, lon: number) => void
  onRadiusChange: (radiusMeters: number) => void
  isImperial: boolean
}

export function AnchorWatchDrawer({
  vesselLat,
  vesselLon,
  vesselHeadingDeg,
  anchorLat,
  anchorLon,
  radiusMeters,
  distanceMeters,
  bearingDeg,
  vesselTrail,
  aisVessels,
  aisTrails,
  isDarkTheme,
  onAnchorReposition,
  onRadiusChange,
  isImperial,
}: AnchorWatchDrawerProps) {
  const distLabel = distanceMeters !== null
    ? isImperial
      ? `${Math.round(distanceMeters * 3.28084)} ft`
      : `${Math.round(distanceMeters)} m`
    : '—'
  const radiusLabel = isImperial
    ? `${Math.round(radiusMeters * 3.28084)} ft`
    : `${Math.round(radiusMeters)} m`

  return (
    <div className="flex h-full flex-col gap-0">
      {/* Compact metrics bar */}
      <div className="flex shrink-0 items-center gap-6 border-b bg-background/80 px-4 py-2 backdrop-blur">
        <div className="text-center">
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Distance</p>
          <p className="font-display text-lg tabular-nums leading-tight text-primary">{distLabel}</p>
        </div>
        <div className="text-center">
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Bearing</p>
          <p className="font-display text-lg tabular-nums leading-tight text-secondary">
            {bearingDeg !== null ? `${bearingDeg}°` : '—'}
          </p>
        </div>
        <div className="text-center">
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Alarm Radius</p>
          <p className="font-display text-lg tabular-nums leading-tight text-secondary">{radiusLabel}</p>
        </div>
      </div>

      {/* Full-width map */}
      <AnchorWatchMap
        vesselLat={vesselLat}
        vesselLon={vesselLon}
        vesselHeadingDeg={vesselHeadingDeg}
        anchorLat={anchorLat}
        anchorLon={anchorLon}
        radiusMeters={radiusMeters}
        vesselTrail={vesselTrail}
        aisVessels={aisVessels}
        aisTrails={aisTrails}
        isDarkTheme={isDarkTheme}
        onAnchorReposition={onAnchorReposition}
        onRadiusChange={onRadiusChange}
        className="flex-1"
      />
    </div>
  )
}
