import { Anchor, Volume2 } from 'lucide-react'
import { memo, useCallback } from 'react'
import { Button } from '@/components/ui/button'
import { Tile } from '@/components/ui/tile'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import { findAnchorDragAlarm, useAlarms } from '@/hooks/use-alarms'
import { useAnchorAlarm } from '@/hooks/use-anchor-alarm'
import { primeAudioContextForAlarm } from '@/lib/audio-utils'
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
  } = watch

  // Drag detection is server-side (ADR 0038); this renders the audible half and
  // silences by acknowledging, so every screen agrees.
  const { alarms, acknowledge } = useAlarms()
  const { isAlarming, isSilenced, silence } = useAnchorAlarm(findAnchorDragAlarm(alarms), acknowledge)

  const handleDropHere = useCallback(() => {
    if (lat === null || lon === null) return
    void primeAudioContextForAlarm() // Unlock audio context for alarm
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
      {gnssCritical && (
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
          onAnchorReposition={updatePosition}
          onRadiusChange={updateRadius}
          onClearAnchor={clearAnchor}
          onFullscreen={onFullscreen}
          className="h-64 rounded-lg"
        />
      </div>
    </Tile>
  )
})
