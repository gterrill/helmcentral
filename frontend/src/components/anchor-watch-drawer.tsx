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
  depthMeters: number | null
  currentDriftKts: number | null
  currentSetDeg: number | null
  distanceMeters: number | null
  bearingDeg: number | null
  vesselTrail: () => TrailPoint[]
  aisVessels: NearbyVessel[]
  aisTrails: () => Map<string, TrailPoint[]>
  isDarkTheme: boolean
  onAnchorReposition: (lat: number, lon: number) => void
  onRadiusChange: (radiusMeters: number) => void
  onClearAnchor: () => void
  isImperial: boolean
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
  distanceMeters,
  bearingDeg,
  vesselTrail,
  aisVessels,
  aisTrails,
  isDarkTheme,
  onAnchorReposition,
  onRadiusChange,
  onClearAnchor,
  isImperial,
}: AnchorWatchDrawerProps) {
  return (
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
      distanceMeters={distanceMeters}
      bearingDeg={bearingDeg}
      isImperial={isImperial}
      vesselTrail={vesselTrail}
      aisVessels={aisVessels}
      aisTrails={aisTrails}
      isDarkTheme={isDarkTheme}
      onAnchorReposition={onAnchorReposition}
      onRadiusChange={onRadiusChange}
      onClearAnchor={onClearAnchor}
      className="h-full w-full"
    />
  )
}

