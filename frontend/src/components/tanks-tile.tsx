import { faucet } from '@lucide/lab'
import { Droplet, Droplets, Fuel, Icon } from 'lucide-react'
import { memo } from 'react'

import { Tile } from '@/components/ui/tile'
import type { TankLevel } from '@/hooks/use-tanks-state'
import { formatQuantity } from '@/lib/quantities'
import { worstZoneState, type ZoneState } from '@/lib/severity'
import { formatDataAge, isStale } from '@/lib/staleness'
import { cn } from '@/lib/utils'

type TanksTileProps = {
  tanks: TankLevel[]
  loading: boolean
  /**
   * Seconds since the tanks feed last reported, or null if the upstream
   * source carries no age at all. `useTanksState`'s payload does not
   * currently publish one (see hooks/use-tanks-state.ts) — a null age reads
   * as "not stale" rather than being defaulted to a number, per the
   * fail-fast/no-masking policy: this tile cannot claim staleness it has no
   * evidence for.
   */
  lastUpdateAgeS: number | null
  /**
   * The three derived fuel figures the footer reads (ADR 0084), plus each
   * figure's own age. Optional and defaulted to null so a caller exercising
   * the tank rows in isolation (most of this file's existing tests) does not
   * have to know the footer exists.
   */
  fuelVolumeM3?: number | null
  fuelVolumeAgeS?: number | null
  fuelTimeToEmptyS?: number | null
  fuelRangeM?: number | null
  fuelDerivedAgeS?: number | null
}

const NO_VALUE = '—'

function clampPercent(value: number) {
  return Math.max(0, Math.min(100, value))
}

type TankTone = 'critical' | 'warn' | 'normal'

// Waste inverts the rest of the fleet: a FULL waste tank is the bad state,
// not an empty one, so its "getting worse" direction is HIGH rather than LOW.
function isLowBadKind(kind: TankLevel['kind']) {
  return kind !== 'waste'
}

function isWarn(kind: TankLevel['kind'], percent: number) {
  if (kind === 'water') {
    return percent > 10 && percent <= 20
  }

  if (kind === 'waste') {
    return percent > 80 && percent <= 90
  }

  return percent >= 10 && percent < 30
}

function isCritical(kind: TankLevel['kind'], percent: number) {
  if (kind === 'water') {
    return percent <= 10
  }

  if (kind === 'waste') {
    return percent > 90
  }

  return percent < 10
}

function tankTone(kind: TankLevel['kind'], percent: number): TankTone {
  if (isCritical(kind, percent)) {
    return 'critical'
  }

  if (isWarn(kind, percent)) {
    return 'warn'
  }

  return 'normal'
}

/** TankTone in the tile-edge vocabulary (ADR 0081). */
const TONE_STATE: Record<TankTone, ZoneState> = { critical: 'alarm', warn: 'warn', normal: 'normal' }

/**
 * The non-colour channel a red/green-colour-deficient operator (or anyone
 * reading a washed-out screen in direct sun) gets instead of the tone colour
 * alone: OK for a healthy tank, LOW/HIGH for whichever direction is bad for
 * this tank kind once it crosses the warn threshold, CRIT past that. This is
 * the accessible signal; the colour below is reinforcement, not the message.
 */
function tankToneWord(kind: TankLevel['kind'], tone: TankTone): string {
  if (tone === 'normal') {
    return 'OK'
  }

  if (tone === 'critical') {
    return 'CRIT'
  }

  return isLowBadKind(kind) ? 'LOW' : 'HIGH'
}

// A healthy tank is visually neutral (Rationed Colour Rule): colour is spent
// only at warn and critical, never on the normal reading.
function toneTextClass(tone: TankTone): string {
  if (tone === 'critical') return 'text-red-600'
  if (tone === 'warn') return 'text-amber-600'
  return 'text-foreground'
}

function toneBarClass(tone: TankTone): string {
  if (tone === 'critical') return 'bg-red-600'
  if (tone === 'warn') return 'bg-amber-600'
  return 'bg-muted-foreground/70'
}

function toneWordClass(tone: TankTone): string {
  return tone === 'normal' ? 'text-muted-foreground' : toneTextClass(tone)
}

function tankKindIcon(kind: TankLevel['kind']) {
  if (kind === 'fuel') {
    return <Fuel className="h-4 w-4 text-amber-600" aria-hidden="true" />
  }

  if (kind === 'waste') {
    return <Droplet className="h-4 w-4 text-amber-900" aria-hidden="true" />
  }

  return <Icon iconNode={faucet} className="h-4 w-4 text-emerald-700" aria-hidden="true" />
}

type FuelFooterStatProps = {
  label: string
  value: string
  unit: string
  stale: boolean
  staleAgeLabel: string
}

/**
 * One footer KPI stack: a muted uppercase label (with a small amber stale
 * badge beside it, the same `Tile`-badge look, when this stat's own age has
 * gone stale) over a mono readout and its unit suffix, in the same idiom
 * `AlternatorColumn` and `GaugeGroupTile`'s per-member rows already use.
 * Scoped to this file — not a shared primitive, used three times below.
 */
function FuelFooterStat({ label, value, unit, stale, staleAgeLabel }: FuelFooterStatProps) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="flex min-w-0 items-center gap-1 text-[10px] uppercase tracking-wider text-muted-foreground">
        <span className="truncate">{label}</span>
        {stale && (
          <span
            data-testid="fuel-footer-stale-badge"
            className="shrink-0 rounded-sm border border-amber-500/40 bg-amber-500/10 px-1.5 py-0.5 text-[10px] leading-none text-amber-600 dark:text-amber-400"
          >
            Stale {staleAgeLabel}
          </span>
        )}
      </span>
      <div className="flex min-w-0 items-baseline gap-1">
        <span className="truncate font-display text-lg tabular-nums leading-none text-gauge-secondary">{value}</span>
        <span className="shrink-0 text-[11px] leading-none text-muted-foreground">{unit}</span>
      </div>
    </div>
  )
}

export const TanksTile = memo(function TanksTile({
  tanks,
  loading,
  lastUpdateAgeS,
  fuelVolumeM3 = null,
  fuelVolumeAgeS = null,
  fuelTimeToEmptyS = null,
  fuelRangeM = null,
  fuelDerivedAgeS = null,
}: TanksTileProps) {
  const feedStale = isStale(lastUpdateAgeS)
  const visibleTanks = tanks.slice(0, 8)
  // Worst tone across the visible tanks, in the tile-edge vocabulary. A stale
  // feed's tanks all read null here (mirroring the JSX below), which is moot
  // anyway: Tile suppresses the state whenever `stale` is set.
  const state = worstZoneState(
    visibleTanks.map((tank) => {
      if (feedStale) return null
      return TONE_STATE[tankTone(tank.kind, clampPercent(tank.level_percent))]
    }),
  )

  // The footer only makes sense once there is a fuel tank to be aboard for —
  // a boat with none configured has nothing for "fuel aboard" to mean.
  const hasFuelTank = tanks.some((tank) => tank.kind === 'fuel')

  // Fuel aboard goes stale on its own age; range and time both go stale on
  // fuel_derived_age_s, the oldest input behind whichever of burn or speed
  // they need (ADR 0084) — independent of each other and of the tank feed's
  // own staleness above, so an engine's dead fuel-rate sensor blanks range
  // and time without also blanking a live tank reading.
  const volumeStale = isStale(fuelVolumeAgeS)
  const derivedStale = isStale(fuelDerivedAgeS)

  const volumeLabel = formatQuantity(volumeStale ? null : fuelVolumeM3, 'volume', 'L') ?? NO_VALUE
  const rangeLabel = formatQuantity(derivedStale ? null : fuelRangeM, 'length', 'nm') ?? NO_VALUE
  // One decimal for time to empty specifically: the shared duration/h unit
  // rounds to whole hours for gauges like engine hours, but a boat rarely
  // has anywhere near an hour of margin worth truncating away here.
  const timeToEmptyLabel = formatQuantity(derivedStale ? null : fuelTimeToEmptyS, 'duration', 'h', 1) ?? NO_VALUE

  return (
    <Tile
      title="Tanks"
      state={state}
      icon={<Droplets className="h-3.5 w-3.5 text-gauge-secondary" />}
      stale={feedStale}
      staleLabel={formatDataAge(lastUpdateAgeS)}
    >
      <div className="mt-3 space-y-2">
        {visibleTanks.map((tank) => {
          // A stale feed is showing a historical artefact, not a live level —
          // blanked rather than tinted by a tone it can no longer vouch for.
          const percent = feedStale ? null : clampPercent(tank.level_percent)
          const tone: TankTone = percent === null ? 'normal' : tankTone(tank.kind, percent)
          const percentLabel = percent === null ? NO_VALUE : `${Math.round(percent)}%`
          const toneWord = percent === null ? NO_VALUE : tankToneWord(tank.kind, tone)

          return (
            <div key={tank.id} className="flex items-center gap-3">
              <span className="grid h-6 w-6 shrink-0 place-items-center">{tankKindIcon(tank.kind)}</span>
              <p className={cn('w-28 shrink-0 font-display text-sm uppercase leading-tight tracking-[0.08em] sm:w-32', toneTextClass(tone))}>
                {tank.label}
              </p>
              <div className="h-5 min-w-0 flex-1 overflow-hidden rounded-md border bg-muted/30">
                <div className={cn('h-full transition-[width] duration-500', toneBarClass(tone))} style={{ width: `${percent ?? 0}%` }} />
              </div>
              <div className="flex w-20 shrink-0 flex-col items-end">
                <p className={cn('font-display text-2xl leading-none tabular-nums', toneTextClass(tone))}>{percentLabel}</p>
                <p className={cn('font-display text-[10px] uppercase leading-none tracking-[0.12em]', toneWordClass(tone))}>{toneWord}</p>
              </div>
            </div>
          )
        })}

        {!loading && visibleTanks.length === 0 ? (
          <div className="rounded-md border border-dashed bg-muted/25 px-3 py-4 text-center text-sm text-muted-foreground">No tank data available</div>
        ) : null}

        {loading ? (
          <div className="rounded-md border border-dashed bg-muted/25 px-3 py-4 text-center text-sm text-muted-foreground">Loading tank levels...</div>
        ) : null}
      </div>

      {hasFuelTank && (
        <div className="mt-3 grid grid-cols-2 gap-3 border-t pt-3 sm:grid-cols-3">
          <FuelFooterStat
            label="Fuel aboard"
            value={volumeLabel}
            unit="L"
            stale={volumeStale}
            staleAgeLabel={formatDataAge(fuelVolumeAgeS)}
          />
          <FuelFooterStat
            label="Range at current burn"
            value={rangeLabel}
            unit="nm"
            stale={derivedStale}
            staleAgeLabel={formatDataAge(fuelDerivedAgeS)}
          />
          <FuelFooterStat
            label="Time to empty"
            value={timeToEmptyLabel}
            unit="h"
            stale={derivedStale}
            staleAgeLabel={formatDataAge(fuelDerivedAgeS)}
          />
        </div>
      )}
    </Tile>
  )
})
