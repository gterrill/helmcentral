import { Anchor } from 'lucide-react'
import { useCallback } from 'react'
import { Button } from '@/components/ui/button'
import { Tile } from '@/components/ui/tile'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import type { AnchorWatchResult } from '@/hooks/use-anchor-watch'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { TrailPoint } from '@/hooks/use-server-trails'

interface AnchorWatchTileProps {
  watch: AnchorWatchResult
  lat: number | null
  lon: number | null
  depthMeters: number | null
  currentDriftKts: number | null
  currentSetDeg: number | null
  isImperial: boolean
  vesselHeadingDeg: number | null
  vesselTrail: () => TrailPoint[]
  aisVessels: NearbyVessel[]
  aisTrails: () => Map<string, TrailPoint[]>
  isDarkTheme: boolean
  onFullscreen: () => void
}

export function AnchorWatchTile({ watch, lat, lon, depthMeters, currentDriftKts, currentSetDeg, isImperial, vesselHeadingDeg, vesselTrail, aisVessels, aisTrails, isDarkTheme, onFullscreen }: AnchorWatchTileProps) {
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
    void setAnchorHere(lat, lon)
  }, [lat, lon, setAnchorHere])

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
            Drop
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
          onAnchorReposition={updatePosition}
          onRadiusChange={updateRadius}
          onClearAnchor={clearAnchor}
          onFullscreen={onFullscreen}
          className="h-64 rounded-lg"
        />
      </div>
    </Tile>
  )
}
