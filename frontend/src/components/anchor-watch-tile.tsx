import { Anchor, Map as MapIcon } from 'lucide-react'
import { useCallback, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Tile } from '@/components/ui/tile'
import type { AnchorWatchResult } from '@/hooks/use-anchor-watch'

const METERS_TO_FEET = 3.28084
const DEFAULT_RADIUS_FT = 150
const DEFAULT_RADIUS_M = 15

interface AnchorWatchTileProps {
  watch: AnchorWatchResult
  lat: number | null
  lon: number | null
  isImperial: boolean
  showMap: boolean
  onToggleMap: () => void
}

function formatCoord(value: number, isLat: boolean): string {
  const abs = Math.abs(value)
  const deg = Math.floor(abs)
  const minFloat = (abs - deg) * 60
  const min = minFloat.toFixed(3)
  const hemi = isLat ? (value >= 0 ? 'N' : 'S') : value >= 0 ? 'E' : 'W'
  return `${hemi} ${deg} ${min}\u2032`
}

export function AnchorWatchTile({ watch, lat, lon, isImperial, showMap, onToggleMap }: AnchorWatchTileProps) {
  const {
    anchorState,
    anchorLat,
    anchorLon,
    radiusMeters,
    distanceMeters,
    bearingDeg,
    suggestSet,
    setAnchorHere,
    updateRadius,
    clearAnchor,
  } = watch

  const [pendingRadius, setPendingRadius] = useState<number | null>(null)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const displayRadius = pendingRadius !== null
    ? pendingRadius
    : isImperial
      ? Math.round(radiusMeters * METERS_TO_FEET)
      : Math.round(radiusMeters)

  const handleRadiusChange = useCallback((rawValue: number) => {
    setPendingRadius(rawValue)
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => {
      const meters = isImperial ? rawValue / METERS_TO_FEET : rawValue
      void updateRadius(meters)
      setPendingRadius(null)
    }, 800)
  }, [isImperial, updateRadius])

  const handleDropHere = useCallback(() => {
    if (lat === null || lon === null) return
    const defaultMeters = isImperial ? DEFAULT_RADIUS_FT / METERS_TO_FEET : DEFAULT_RADIUS_M
    void setAnchorHere(lat, lon, defaultMeters)
  }, [lat, lon, isImperial, setAnchorHere])

  const handleClear = useCallback(() => {
    void clearAnchor()
  }, [clearAnchor])

  const displayDist = distanceMeters !== null
    ? isImperial
      ? `${Math.round(distanceMeters * METERS_TO_FEET)}`
      : `${Math.round(distanceMeters)}`
    : '—'

  const distUnit = isImperial ? 'ft' : 'm'
  const radiusUnit = isImperial ? 'ft' : 'm'
  const draggingBeyond = distanceMeters !== null
    ? isImperial
      ? Math.round((distanceMeters - radiusMeters) * METERS_TO_FEET)
      : Math.round(distanceMeters - radiusMeters)
    : 0

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
  const isDragging = anchorState === 'dragging'

  return (
    <Tile
      title="Anchor Watch"
      icon={<Anchor className="h-3.5 w-3.5" />}
      titleExtra={
        <button
          onClick={onToggleMap}
          aria-label={showMap ? 'Show list view' : 'Show map'}
          className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-primary"
        >
          <MapIcon className="h-4 w-4" />
        </button>
      }
    >
      {isDragging && (
        <div className="mb-3 rounded-md border border-red-500/40 bg-red-500/10 px-3 py-2 text-xs text-red-400">
          ⚠ Dragging — <span className="font-semibold">{draggingBeyond}{distUnit} beyond radius</span>
        </div>
      )}

      <div className="mt-2 grid grid-cols-3 gap-2 text-center">
        <div className={`rounded-md px-2 py-2 ${isDragging ? 'bg-red-500/15' : 'bg-muted/50'}`}>
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Distance</p>
          <p className={`font-display text-2xl ${isDragging ? 'text-red-400' : 'text-primary'}`}>
            {displayDist}
            <span className="ml-1 text-base text-muted-foreground">{distUnit}</span>
          </p>
        </div>
        <div className="rounded-md bg-muted/50 px-2 py-2">
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Bearing</p>
          <p className="font-display text-2xl text-secondary">
            {bearingDeg ?? '—'}
            <span className="ml-1 text-base text-muted-foreground">°</span>
          </p>
        </div>
        <div className="rounded-md bg-muted/50 px-2 py-2">
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Radius</p>
          <p className="font-display text-2xl text-secondary">
            {isImperial ? Math.round(radiusMeters * METERS_TO_FEET) : Math.round(radiusMeters)}
            <span className="ml-1 text-base text-muted-foreground">{radiusUnit}</span>
          </p>
        </div>
      </div>

      {anchorLat !== null && anchorLon !== null && (
        <div className="mt-3 rounded-md border bg-background/60 px-3 py-2 text-sm">
          <div className="flex items-center gap-2">
            <span className={`h-2 w-2 rounded-full ${isDragging ? 'bg-red-400' : 'bg-emerald-400'}`} />
            <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Anchor Position</p>
          </div>
          <p className="mt-1 font-mono text-xs">{formatCoord(anchorLat, true)}&nbsp;&nbsp;{formatCoord(anchorLon, false)}</p>
        </div>
      )}

      <div className="mt-3">
        <label className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
          Radius ({radiusUnit})
        </label>
        <div className="mt-1 flex items-center gap-2">
          <input
            type="number"
            min={isImperial ? 50 : 15}
            max={isImperial ? 1000 : 305}
            step={isImperial ? 10 : 5}
            value={displayRadius}
            onChange={(e) => handleRadiusChange(Number(e.target.value))}
            className="w-24 rounded-md border bg-background/60 px-2 py-1.5 text-sm font-display tabular-nums focus:outline-none focus:ring-1 focus:ring-ring"
          />
          <span className="text-sm text-muted-foreground">{radiusUnit}</span>
        </div>
      </div>

      <div className="mt-3">
        <Button
          variant="outline"
          className="h-10 w-full text-muted-foreground"
          onClick={handleClear}
        >
          Clear Anchor Watch
        </Button>
      </div>
    </Tile>
  )
}
