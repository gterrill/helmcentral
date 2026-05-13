import { Anchor } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import type { AnchorConfig } from '@/config/app-config'
import { calculateCatenary, type SeabedType, type SeaState } from '@/lib/catenary'
import type { AnchorWatchState } from '@/hooks/use-anchor-watch'
import { Tile } from '@/components/ui/tile'

const METERS_TO_FEET = 3.28084

interface RodeScopeTileProps {
  anchorState: AnchorWatchState
  rodeDeployedM: number
  seaState: SeaState
  seabedType: SeabedType
  depthM: number | null
  windKts: number | null
  isImperial: boolean
  anchorConfig: AnchorConfig
  onUpdate: (rodeDeployedM: number, seaState: SeaState, seabedType: SeabedType) => Promise<void>
}

function toDisplayDistance(meters: number, isImperial: boolean): number {
  return isImperial ? meters * METERS_TO_FEET : meters
}

export function RodeScopeTile({
  anchorState,
  rodeDeployedM,
  seaState,
  seabedType,
  depthM,
  windKts,
  isImperial,
  anchorConfig,
  onUpdate,
}: RodeScopeTileProps) {
  const [pendingRode, setPendingRode] = useState<number>(0)
  const [pendingSeaState, setPendingSeaState] = useState<SeaState>(seaState)
  const [pendingSeabedType, setPendingSeabedType] = useState<SeabedType>(seabedType)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    setPendingRode(Math.max(0, toDisplayDistance(rodeDeployedM, isImperial)))
  }, [rodeDeployedM, isImperial])

  useEffect(() => {
    setPendingSeaState(seaState)
  }, [seaState])

  useEffect(() => {
    setPendingSeabedType(seabedType)
  }, [seabedType])

  const depthFromHawseM = depthM !== null ? depthM + anchorConfig.bowRollerHeightM : null
  const currentScope = depthFromHawseM && depthFromHawseM > 0 ? rodeDeployedM / depthFromHawseM : null

  const catenary = (depthM !== null && windKts !== null)
    ? calculateCatenary({
      depthM,
      bowRollerHeightM: anchorConfig.bowRollerHeightM,
      windKts,
      seaState: pendingSeaState,
      seabedType: pendingSeabedType,
      chainSizeMm: anchorConfig.chainSizeMm,
      windageAreaM2: anchorConfig.windageAreaM2,
      hullType: anchorConfig.hullType,
    })
    : null

  const recommendedScope = catenary?.minScopeRatio ?? null
  const recommendedRodeM = catenary?.recommendedRodeM ?? null

  let scopeStatus: 'ok' | 'low' | 'insufficient' | 'unknown' = 'unknown'
  if (currentScope !== null && recommendedScope !== null) {
    if (currentScope >= recommendedScope) {
      scopeStatus = 'ok'
    } else if (currentScope >= recommendedScope * 0.85) {
      scopeStatus = 'low'
    } else {
      scopeStatus = 'insufficient'
    }
  }

  const handlePersist = useCallback((rodeDisplay: number, nextSeaState: SeaState, nextSeabedType: SeabedType) => {
    if (debounceRef.current) {
      clearTimeout(debounceRef.current)
    }
    debounceRef.current = setTimeout(() => {
      const normalizedDisplay = Number.isFinite(rodeDisplay) ? Math.max(0, rodeDisplay) : 0
      const rodeMeters = isImperial ? normalizedDisplay / METERS_TO_FEET : normalizedDisplay
      void onUpdate(rodeMeters, nextSeaState, nextSeabedType)
    }, 800)
  }, [isImperial, onUpdate])

  const handleRodeChange = useCallback((raw: string) => {
    const parsed = Number(raw)
    const nextDisplay = Number.isFinite(parsed) ? Math.max(0, parsed) : 0
    setPendingRode(nextDisplay)
    handlePersist(nextDisplay, pendingSeaState, pendingSeabedType)
  }, [handlePersist, pendingSeaState, pendingSeabedType])

  const handleSeaStateChange = useCallback((value: SeaState) => {
    setPendingSeaState(value)
    handlePersist(pendingRode, value, pendingSeabedType)
  }, [handlePersist, pendingRode, pendingSeabedType])

  const handleSeabedChange = useCallback((value: SeabedType) => {
    setPendingSeabedType(value)
    handlePersist(pendingRode, pendingSeaState, value)
  }, [handlePersist, pendingRode, pendingSeaState])

  const unit = isImperial ? 'ft' : 'm'
  const displayDepthFromHawse = depthFromHawseM !== null ? toDisplayDistance(depthFromHawseM, isImperial).toFixed(1) : '—'
  const displayRode = Math.round(pendingRode)
  const displayRecommendedRode = recommendedRodeM !== null
    ? Math.round(toDisplayDistance(recommendedRodeM, isImperial)).toString()
    : '—'

  const isDragging = anchorState === 'dragging'
  const isInactive = anchorState === 'none'

  const scopeBadgeClass = scopeStatus === 'ok'
    ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-400'
    : scopeStatus === 'low'
      ? 'border-amber-500/40 bg-amber-500/10 text-amber-400'
      : scopeStatus === 'insufficient'
        ? 'border-red-500/40 bg-red-500/10 text-red-400'
        : 'border-muted bg-muted/40 text-muted-foreground'

  const scopeBadgeLabel = scopeStatus === 'ok'
    ? 'Scope OK'
    : scopeStatus === 'low'
      ? 'Scope Low'
      : scopeStatus === 'insufficient'
        ? 'Scope Insufficient'
        : 'Scope Unknown'

  return (
    <Tile title="Rode & Scope" icon={<Anchor className="h-3.5 w-3.5" />}>
      {isDragging && (
        <div className="mb-3 rounded-md border border-red-500/40 bg-red-500/10 px-3 py-2 text-xs text-red-400">
          ⚠ Dragging — check scope now
        </div>
      )}

      <div className={isInactive ? 'opacity-60' : ''}>
        <div className="mt-2 grid grid-cols-3 gap-2 text-center">
          <div className={`rounded-md border bg-background/60 px-2 py-2 ${isDragging ? 'border-red-500/30 bg-red-500/10' : ''}`}>
            <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Rode Deployed</p>
            <p className="font-display text-2xl text-primary tabular-nums">
              {displayRode}
              <span className="ml-1 text-base text-muted-foreground">{unit}</span>
            </p>
          </div>
          <div className="rounded-md border bg-background/60 px-2 py-2">
            <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Current Scope</p>
            <p className="font-display text-2xl text-secondary tabular-nums">
              {currentScope !== null && Number.isFinite(currentScope) ? currentScope.toFixed(1) : '—'}
              <span className="ml-1 text-base text-muted-foreground">:1</span>
            </p>
          </div>
          <div className="rounded-md border bg-background/60 px-2 py-2">
            <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Recommended</p>
            <p className="font-display text-2xl text-secondary tabular-nums">
              {recommendedScope !== null ? recommendedScope.toFixed(1) : '—'}
              <span className="ml-1 text-base text-muted-foreground">:1</span>
            </p>
          </div>
        </div>

        <div className="mt-3 flex items-center justify-between gap-2">
          <div className={`rounded-md border px-2 py-1 text-xs font-semibold uppercase tracking-[0.12em] ${scopeBadgeClass}`}>
            {scopeBadgeLabel}
          </div>
          <p className="text-xs text-muted-foreground">
            Hawse depth {displayDepthFromHawse} {unit}
          </p>
        </div>

        <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-3">
          <label className="rounded-md border bg-background/60 px-3 py-2 text-left">
            <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Rode ({unit})</p>
            <input
              type="number"
              min={0}
              max={Math.round(toDisplayDistance(anchorConfig.chainOnboardM, isImperial))}
              step={isImperial ? 5 : 1}
              value={Number.isFinite(pendingRode) ? pendingRode : 0}
              onChange={(e) => handleRodeChange(e.target.value)}
              disabled={isInactive}
              className="mt-1 w-full rounded-md border bg-background/70 px-2 py-1.5 text-sm font-display tabular-nums focus:outline-none focus:ring-1 focus:ring-ring"
            />
          </label>

          <label className="rounded-md border bg-background/60 px-3 py-2 text-left">
            <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Sea State</p>
            <select
              className="mt-1 w-full rounded-md border bg-background/70 px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
              value={pendingSeaState}
              onChange={(e) => handleSeaStateChange(e.target.value as SeaState)}
              disabled={isInactive}
            >
              <option value="calm">Calm</option>
              <option value="choppy">Choppy</option>
              <option value="rough">Rough</option>
              <option value="storm">Storm</option>
            </select>
          </label>

          <label className="rounded-md border bg-background/60 px-3 py-2 text-left">
            <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Seabed</p>
            <select
              className="mt-1 w-full rounded-md border bg-background/70 px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
              value={pendingSeabedType}
              onChange={(e) => handleSeabedChange(e.target.value as SeabedType)}
              disabled={isInactive}
            >
              <option value="sand">Sand</option>
              <option value="mud">Mud</option>
              <option value="rock">Rock</option>
              <option value="grass">Grass</option>
            </select>
          </label>
        </div>

        <div className="mt-3 rounded-md border bg-background/60 px-3 py-2 text-sm">
          <div className="flex items-center justify-between gap-3">
            <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Catenary Recommendation</p>
            <p className="font-display text-xl leading-none text-secondary tabular-nums">
              {displayRecommendedRode}
              <span className="ml-1 text-base text-muted-foreground">{unit}</span>
            </p>
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            Wind {windKts !== null ? `${Math.round(windKts)} kts` : '—'} · Chain {anchorConfig.chainSizeMm}mm · Hull {anchorConfig.hullType.replace('_', ' ')}
          </p>
        </div>
      </div>

      {isInactive && (
        <p className="mt-3 text-xs text-muted-foreground">Set anchor watch to activate monitoring state.</p>
      )}
    </Tile>
  )
}
