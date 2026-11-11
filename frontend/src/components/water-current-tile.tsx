import { memo } from 'react'

import { Readout } from '@/components/readout'
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
 * S35 is 35 degrees off the bow on the starboard side. Dead ahead and dead
 * astern name no side, so they read as plain 0 and 180.
 */
function formatComesFrom(setDeg: number, headingTrue: number): string {
  const fromRel = Math.round(norm360(setDeg + 180 - headingTrue)) % 360
  if (fromRel === 0 || fromRel === 180) {
    return String(fromRel)
  }
  return fromRel < 180 ? `S${fromRel}` : `P${360 - fromRel}`
}

interface Effect {
  label: string
  figure: string
  unit?: string
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
    return { label: 'No current', figure: '0.0', unit: 'kt', className: MUTED }
  }
  if (impactKts === null) {
    return { label: 'Effect', figure: NO_VALUE, className: MUTED }
  }
  const rounded = roundTenth(impactKts)
  const className = currentEffectColorClass(classifyCurrentEffect(impactKts)) || MUTED
  switch (classifyCurrentEffect(impactKts)) {
    case 'helping':
      return { label: 'Helping', figure: `+${rounded.toFixed(1)}`, unit: 'kt', className }
    case 'hindering':
      return { label: 'Hindering', figure: `${MINUS}${Math.abs(rounded).toFixed(1)}`, unit: 'kt', className }
    default:
      return { label: 'No effect', figure: '0.0', unit: 'kt', className }
  }
}

/**
 * One row of the tile: a small label over a reading the size of a Gauge tile's.
 * The value box is its own size container, so the reading scales with the room
 * the row has, as it does on a gauge.
 */
function Row({ label, labelWraps, labelClassName, value, unit, className, rowTestId, labelTestId, valueTestId }: {
  label: string
  /** A reason in the label wraps rather than truncating; a plain label stays on one line. */
  labelWraps?: boolean
  labelClassName?: string
  value: string
  unit?: string
  className: string
  rowTestId?: string
  labelTestId: string
  valueTestId: string
}) {
  return (
    <div data-testid={rowTestId} className={cn('flex min-h-0 min-w-0 flex-1 flex-col', className)}>
      <p
        data-testid={labelTestId}
        className={cn('text-[10px] uppercase leading-none tracking-[0.16em]', !labelWraps && 'truncate', labelClassName)}
      >
        {label}
      </p>
      <div className="flex min-h-0 min-w-0 flex-1 items-center [container-type:size]">
        <Readout
          text={value}
          unitLabel={unit ?? ''}
          zone={null}
          size=""
          fallbackTextClass={className}
          fluid
          valueTestId={valueTestId}
        />
      </div>
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
  // Why a row is blank is said in that row's label, so no extra line is needed.
  // A blocked tile blanks every row and says why on the effect row; a missing
  // heading blanks only From.
  const effectLabel = gate.reason ?? effect.label
  const noHeading = readings !== null && !still && headingTrue === null
  const fromLabel = noHeading ? 'No heading' : 'From'

  return (
    <Tile title="Current" stale={feedStale} staleLabel={formatDataAge(lastUpdateAgeS)} fill>
      <div className="flex min-h-0 flex-1 flex-col gap-1">
        <Row
          label="Speed"
          labelClassName={MUTED}
          value={speed}
          unit={readings ? 'kt' : undefined}
          className="text-gauge-primary"
          labelTestId="current-speed-label"
          valueTestId="current-speed"
        />
        <Row
          label={fromLabel}
          labelWraps={noHeading}
          labelClassName={MUTED}
          value={angle}
          className="text-gauge-primary"
          labelTestId="current-angle-label"
          valueTestId="current-angle"
        />
        <Row
          label={effectLabel}
          labelWraps={gate.reason !== null}
          value={effect.figure}
          unit={effect.unit}
          className={effect.className}
          rowTestId="current-effect"
          labelTestId="current-effect-label"
          valueTestId="current-effect-figure"
        />
      </div>
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
