import { faucet } from '@lucide/lab'
import { Droplet, Droplets, Fuel, Icon } from 'lucide-react'
import { memo } from 'react'

import { Tile } from '@/components/ui/tile'
import type { TankLevel } from '@/hooks/use-tanks-state'
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

export const TanksTile = memo(function TanksTile({ tanks, loading, lastUpdateAgeS }: TanksTileProps) {
  const feedStale = isStale(lastUpdateAgeS)
  const visibleTanks = tanks.slice(0, 8)

  return (
    <Tile
      title="Tanks"
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
    </Tile>
  )
})
