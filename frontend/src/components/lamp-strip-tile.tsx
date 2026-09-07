import { LampCeiling, Settings2 } from 'lucide-react'
import { memo } from 'react'

import { Button } from '@/components/ui/button'
import { Tile } from '@/components/ui/tile'
import type { LampConfig, LampStripWidgetConfig } from '@/lib/dashboard-widgets'
import { severityFill } from '@/lib/severity'
import { formatDataAge, isStale } from '@/lib/staleness'

/**
 * Four states. An absent path is not a zero — a lamp lit or darkened because
 * nothing has reported is the same failure the gauges' structural dash
 * exists to prevent. `stale` (ADR 0083) is a fifth reason a lamp goes dark:
 * a source that stopped reporting must never be left showing its last "on".
 */
type LampState = 'on' | 'off' | 'no data' | 'stale'

function lampState(lamp: LampConfig, value: number | null | undefined, stale: boolean): LampState {
  if (stale) return 'stale'
  if (value === null || value === undefined) return 'no data'
  const lit = value !== 0
  return (lamp.invert ? !lit : lit) ? 'on' : 'off'
}

/**
 * "on" is the healthy green, not Signal Blue (ADR 0080). Signal Blue is
 * interactive chrome (DESIGN.md) — buttons, toggles, the active state of a
 * control — and a lamp is not a control; it is a reading with two states.
 * severityFill('normal') is the same green a gauge zone in its normal band
 * draws, so "everything is fine" reads the same way across the dashboard.
 *
 * "no data" and "stale" share the same unlit border colour: a frozen source
 * reads exactly like one that has never reported at all.
 */
function lampFill(state: LampState): string {
  switch (state) {
    case 'on':
      return severityFill('normal')
    case 'off':
      return 'hsl(var(--muted-foreground))'
    default:
      return 'hsl(var(--border))'
  }
}

function Lamp({ label, state, fill, opacity, onClick, ariaLabel }: {
  label: string
  state: string
  fill: string
  opacity: number
  onClick?: () => void
  ariaLabel: string
}) {
  const body = (
    <>
      <svg viewBox="0 0 24 24" className="h-5 w-5" aria-hidden="true">
        <circle cx="12" cy="12" r="9" fill={fill} opacity={opacity} />
      </svg>
      <span className="max-w-[6ch] truncate text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
        {label}
      </span>
    </>
  )

  const className = 'flex min-w-0 shrink-0 flex-col items-center gap-1'
  if (!onClick) {
    return <div className={className} aria-label={ariaLabel} data-state={state}>{body}</div>
  }
  return (
    <button type="button" onClick={onClick} className={`${className} rounded-sm hover:opacity-80`} aria-label={ariaLabel} data-state={state}>
      {body}
    </button>
  )
}

interface LampStripTileProps {
  config: LampStripWidgetConfig
  values: Record<string, number | null>
  /** Age in seconds behind each lamp's bound path (ADR 0083); absent is unknown. */
  ages?: Record<string, number | null>
  /** The worst currently-active alarm severity, driving the CHK rollup. */
  worstAlarmState: string
  editing: boolean
  onConfigure: () => void
  onOpenAlarms: () => void
}

/**
 * The indicator ribbon (ADR 0052): one glance tells you whether anything wants
 * attention, and the CHK lamp says whether to go looking.
 */
export const LampStripTile = memo(function LampStripTile({
  config, values, ages, worstAlarmState, editing, onConfigure, onOpenAlarms,
}: LampStripTileProps) {
  const title = config.title.trim() || 'Indicators'

  return (
    <Tile
      title={title}
      icon={<LampCeiling className="h-3.5 w-3.5 text-gauge-secondary" />}
      titleExtra={
        editing ? (
          <Button size="sm" variant="ghost" onClick={onConfigure} aria-label={`Configure ${title}`}>
            <Settings2 className="size-3.5" />
          </Button>
        ) : undefined
      }
    >
      {/* Scrolls rather than stretching: sixteen lamps must never widen the
          tile and break the grid. */}
      <div data-testid="lamp-strip-row" className="flex items-start gap-3 overflow-x-auto pb-1">
        {config.lamps.map((lamp, index) => {
          const age = ages?.[lamp.path] ?? null
          const stale = isStale(age)
          const state = lampState(lamp, values[lamp.path], stale)
          const label = lamp.label.trim() || lamp.path.split('.').slice(-1)[0]
          return (
            <Lamp
              key={index}
              label={label}
              state={state}
              fill={lampFill(state)}
              opacity={state === 'on' ? 1 : 0.3}
              ariaLabel={stale ? `${label}: stale ${formatDataAge(age)}` : `${label}: ${state}`}
            />
          )
        })}

        {config.showCheck && (
          <Lamp
            label="CHK"
            state={worstAlarmState}
            // normal stays this tile's own muted grey rather than
            // severityFill's green: the lamp already drops to 0.3 opacity for
            // it, and a dim grey dot reads as "nothing to report" more
            // plainly than a dim green one does.
            fill={worstAlarmState === 'normal' ? 'hsl(var(--muted-foreground))' : severityFill(worstAlarmState)}
            opacity={worstAlarmState === 'normal' ? 0.3 : 1}
            onClick={onOpenAlarms}
            ariaLabel={`CHK: ${worstAlarmState}`}
          />
        )}
      </div>
    </Tile>
  )
})
