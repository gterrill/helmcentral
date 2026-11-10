import { memo } from 'react'

import { Tile } from '@/components/ui/tile'
import { useVesselFields } from '@/hooks/use-vessel-state'
import { normalizeDisplayState } from '@/lib/displays'
import { classifyCurrentEffect, currentEffectColorClass, roundTenth } from '@/lib/current-effect'
import { norm360 } from '@/lib/geo'
import { formatDataAge, isStale } from '@/lib/staleness'
import { cn } from '@/lib/utils'

const NO_VALUE = '—'
const MINUS = '−'

interface WaterCurrentTileProps {
  navigationState: string | null
  /** Compass bearing (true) the current flows towards. */
  currentSetDeg: number | null
  currentDriftKts: number | null
  /** Current along the course; negative runs against the boat. */
  currentDriftImpactKts: number | null
  headingTrue: number | null
  /** Seconds since the current feed last reported; null when unknown. */
  lastUpdateAgeS: number | null
}

type CurrentReading =
  | { reason: string }
  | { reason: null; setDeg: number; driftKts: number }

/**
 * The one gate on the tile. Set and drift are noise at anchor or on a
 * mooring, so they read only under way. A missing navigation state is a
 * missing source, and says so rather than claiming the boat is not moving.
 */
function readCurrent(
  navigationState: string | null,
  setDeg: number | null,
  driftKts: number | null,
): CurrentReading {
  const state = normalizeDisplayState(navigationState)
  if (state === null) return { reason: 'No navigation state' }
  if (state !== 'motoring' && state !== 'sailing') return { reason: 'Shows under way' }
  if (setDeg === null || driftKts === null) return { reason: 'No current reading' }
  return { reason: null, setDeg, driftKts }
}

/**
 * Where the current comes from relative to the bow, in the wind's convention:
 * S35 is 35 degrees off the bow on the starboard side.
 */
function formatComesFrom(setDeg: number, headingTrue: number): string {
  const fromRel = Math.round(norm360(setDeg + 180 - headingTrue)) % 360
  if (fromRel === 0) {
    return 'Ahead'
  }
  if (fromRel === 180) {
    return 'Astern'
  }
  return fromRel < 180 ? `S${fromRel}` : `P${360 - fromRel}`
}

interface Effect {
  label: string
  figure: string
  className: string
}

const MUTED = 'text-muted-foreground'

/** The word and figure for what the current does to progress along the course. */
function describeEffect(driftKts: number | null, impactKts: number | null): Effect {
  if (driftKts === null) {
    return { label: 'Effect', figure: NO_VALUE, className: MUTED }
  }
  // Drift that rounds to 0.0 kt is still water, whatever the impact says.
  if (roundTenth(driftKts) === 0) {
    return { label: 'No current', figure: '0.0 kt', className: MUTED }
  }
  if (impactKts === null) {
    return { label: 'Effect', figure: NO_VALUE, className: MUTED }
  }
  const rounded = roundTenth(impactKts)
  const className = currentEffectColorClass(classifyCurrentEffect(impactKts)) || MUTED
  switch (classifyCurrentEffect(impactKts)) {
    case 'helping':
      return { label: 'Helping', figure: `+${rounded.toFixed(1)} kt`, className }
    case 'hindering':
      return { label: 'Hindering', figure: `${MINUS}${Math.abs(rounded).toFixed(1)} kt`, className }
    default:
      return { label: 'No effect', figure: '0.0 kt', className }
  }
}

function Stat({ label, value, unit, testId }: { label: string; value: string; unit?: string; testId: string }) {
  return (
    <div className="min-w-0 rounded-md border bg-background/60 px-3 py-2">
      <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">{label}</p>
      <p className="whitespace-nowrap font-display text-2xl leading-none text-gauge-primary tabular-nums">
        <span data-testid={testId}>{value}</span>
        {unit && <span className="ml-1 text-xs text-muted-foreground">{unit}</span>}
      </p>
    </div>
  )
}

export const WaterCurrentTile = memo(function WaterCurrentTile({
  navigationState,
  currentSetDeg,
  currentDriftKts,
  currentDriftImpactKts,
  headingTrue,
  lastUpdateAgeS,
}: WaterCurrentTileProps) {
  const gate = readCurrent(navigationState, currentSetDeg, currentDriftKts)
  const readings = gate.reason === null ? gate : null
  // Stale applies only when readings are shown: a deliberately blank tile has
  // nothing a dead feed could have frozen.
  const feedStale = readings !== null && isStale(lastUpdateAgeS)

  // Still water (drift rounds to 0.0) has no direction worth naming.
  const still = readings !== null && roundTenth(readings.driftKts) === 0
  const showAngle = readings !== null && !still && headingTrue !== null

  const speed = readings ? readings.driftKts.toFixed(1) : NO_VALUE
  const angle = showAngle ? formatComesFrom(readings.setDeg, headingTrue) : NO_VALUE
  const effect = describeEffect(readings?.driftKts ?? null, readings ? currentDriftImpactKts : null)
  const reason = gate.reason ?? (!still && headingTrue === null ? 'No heading' : null)

  return (
    <Tile title="Current" stale={feedStale} staleLabel={formatDataAge(lastUpdateAgeS)}>
      <div className="grid grid-cols-[repeat(2,minmax(0,1fr))] gap-2">
        <Stat label="Speed" value={speed} unit={readings ? 'kt' : undefined} testId="current-speed" />
        <Stat label="From" value={angle} testId="current-angle" />
      </div>
      <div data-testid="current-effect" className={cn('mt-2 min-w-0', effect.className)}>
        <p data-testid="current-effect-label" className="text-xs uppercase tracking-[0.16em]">{effect.label}</p>
        <p
          data-testid="current-effect-figure"
          className="whitespace-nowrap font-display text-3xl leading-none tabular-nums"
        >
          {effect.figure}
        </p>
      </div>
      {reason !== null && <p className="mt-1 text-xs text-muted-foreground">{reason}</p>}
    </Tile>
  )
})

const CURRENT_FIELDS = [
  'navigationState', 'currentSetDeg', 'currentDriftKts', 'currentDriftImpactKts', 'headingTrue', 'currentLastUpdateAgeS',
] as const

/** The Current tile: set and drift against the bow, from vessel state. */
export const LiveWaterCurrentTile = memo(function LiveWaterCurrentTile() {
  const { currentLastUpdateAgeS, ...current } = useVesselFields(CURRENT_FIELDS)
  return <WaterCurrentTile lastUpdateAgeS={currentLastUpdateAgeS} {...current} />
})
