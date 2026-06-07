import { Anchor } from 'lucide-react'
import { useCallback } from 'react'
import { Button } from '@/components/ui/button'
import { Tile } from '@/components/ui/tile'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import type { AnchorWatchResult } from '@/hooks/use-anchor-watch'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { TrailPoint } from '@/hooks/use-vessel-trail'

const METERS_TO_FEET = 3.28084
const DEFAULT_RADIUS_FT = 150
const DEFAULT_RADIUS_M = 15

interface AnchorWatchTileProps {
  watch: AnchorWatchResult
  lat: number | null
  lon: number | null
  isImperial: boolean
  vesselHeadingDeg: number | null
  vesselTrail: () => TrailPoint[]
  aisVessels: NearbyVessel[]
  aisTrails: () => Map<string, TrailPoint[]>
  isDarkTheme: boolean
}

export function AnchorWatchTile({ watch, lat, lon, isImperial, vesselHeadingDeg, vesselTrail, aisVessels, aisTrails, isDarkTheme }: AnchorWatchTileProps) {
  const {
    anchorState,
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
  } = watch

  const handleDropHere = useCallback(() => {
    if (lat === null || lon === null) return
    const defaultMeters = isImperial ? DEFAULT_RADIUS_FT / METERS_TO_FEET : DEFAULT_RADIUS_M
    void setAnchorHere(lat, lon, defaultMeters)
  }, [lat, lon, isImperial, setAnchorHere])

  // ── No anchor set ─────────────────────────────────────────────
  if (anchorState === 'none') {
    return (
      <Tile title="Anchor Watch" icon={<Anchor className="h-3.5 w-3.5" />}>
        {suggestSet && (
          <div className="mb-3 rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-500">
            SignalK reports <span className="font-semibold">anchored</span> — set anchor watch?
          </div>
        )}
        <div className="grid place-items-center py-4 text-center">
          <Anchor className="mb-2 h-8 w-8 text-muted-foreground/30" />
          <p className="text-sm text-muted-foreground">Not monitoring</p>
        </div>
        <div className="mt-2 grid gap-2">
          <Button
            className="h-11 bg-teal-600 text-teal-50 hover:bg-teal-700"
            disabled={lat === null || lon === null}
            onClick={handleDropHere}
          >
            <Anchor className="h-4 w-4" />
            Drop Here ({isImperial ? `${DEFAULT_RADIUS_FT} ft` : `${DEFAULT_RADIUS_M} m`} radius)
          </Button>
        </div>
      </Tile>
    )
  }

  // ── Set or Dragging ───────────────────────────────────────────
  return (
    <Tile title="Anchor Watch" icon={<Anchor className="h-3.5 w-3.5" />}>
      <div className="mt-2 rounded-xl border bg-background/70">
        <AnchorWatchMap
          vesselLat={lat ?? anchorLat!}
          vesselLon={lon ?? anchorLon!}
          vesselHeadingDeg={vesselHeadingDeg}
          anchorLat={anchorLat!}
          anchorLon={anchorLon!}
          radiusMeters={radiusMeters}
          distanceMeters={distanceMeters}
          bearingDeg={bearingDeg}
          isImperial={isImperial}
          vesselTrail={vesselTrail}
          aisVessels={aisVessels}
          aisTrails={aisTrails}
          isDarkTheme={isDarkTheme}
          onAnchorReposition={updatePosition}
          onRadiusChange={updateRadius}
          onClearAnchor={clearAnchor}
          className="h-64 rounded-lg"
        />
      </div>
    </Tile>
  )
}
