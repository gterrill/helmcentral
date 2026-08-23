import { LampCeiling, Settings2 } from 'lucide-react'
import { memo } from 'react'

import { Button } from '@/components/ui/button'
import { Tile } from '@/components/ui/tile'
import type { LampConfig, LampStripWidgetConfig } from '@/lib/dashboard-widgets'

/**
 * Three states, not two. An absent path is not a zero — a lamp lit or darkened
 * because nothing has reported is the same failure the gauges' structural dash
 * exists to prevent.
 */
type LampState = 'on' | 'off' | 'no data'

function lampState(lamp: LampConfig, value: number | null | undefined): LampState {
  if (value === null || value === undefined) return 'no data'
  const lit = value !== 0
  return (lamp.invert ? !lit : lit) ? 'on' : 'off'
}

function lampFill(state: LampState): string {
  switch (state) {
    case 'on':
      return 'hsl(var(--primary))'
    case 'off':
      return 'hsl(var(--muted-foreground))'
    default:
      return 'hsl(var(--border))'
  }
}

/** The CHK rollup reuses the alarm severity vocabulary (ADR 0038). */
function checkFill(state: string): string {
  switch (state) {
    case 'emergency':
      return 'hsl(0 72% 42%)'
    case 'alarm':
      return 'hsl(0 72% 51%)'
    case 'warn':
      return 'hsl(38 92% 50%)'
    case 'alert':
      return 'hsl(43 96% 56%)'
    default:
      return 'hsl(var(--muted-foreground))'
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
  config, values, worstAlarmState, editing, onConfigure, onOpenAlarms,
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
          const state = lampState(lamp, values[lamp.path])
          return (
            <Lamp
              key={index}
              label={lamp.label.trim() || lamp.path.split('.').slice(-1)[0]}
              state={state}
              fill={lampFill(state)}
              opacity={state === 'on' ? 1 : 0.3}
              ariaLabel={`${lamp.label.trim() || lamp.path}: ${state}`}
            />
          )
        })}

        {config.showCheck && (
          <Lamp
            label="CHK"
            state={worstAlarmState}
            fill={checkFill(worstAlarmState)}
            opacity={worstAlarmState === 'normal' ? 0.3 : 1}
            onClick={onOpenAlarms}
            ariaLabel={`CHK: ${worstAlarmState}`}
          />
        )}
      </div>
    </Tile>
  )
})
