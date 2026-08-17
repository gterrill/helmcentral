import { Compass } from 'lucide-react'
import { memo, useCallback, useRef, useState } from 'react'

import { Tile } from '@/components/ui/tile'
import type { AutopilotSide, AutopilotState } from '@/hooks/use-autopilot'
import { cn } from '@/lib/utils'

// Change who is steering the boat, so a slip of the finger must not trigger
// them (ADR 0041). The heading nudges below are deliberately NOT held to —
// they're the controls used constantly, and a confirm on them would make the
// widget useless.
const HOLD_TO_CONFIRM_MS = 800

/** How far one dodge nudge steers off the target course. */
const DODGE_STEP_DEG = 5

interface AutopilotTileProps {
  state: AutopilotState
  pending: Set<string>
  error: string | null
  /** Modes this pilot supports, from the hook's capability probe — empty if it failed. */
  availableModes: string[]
  capabilityError: string | null
  onEngage: () => void
  onDisengage: () => void
  onTack: (side: AutopilotSide) => void
  onGybe: (side: AutopilotSide) => void
  onAdjustHeading: (degrees: number) => void
  onSetMode: (mode: string) => void
  onDodge: (degrees: number) => void
  onClearDodge: () => void
  /** Current true heading in degrees, from the vessel-state stream (not the autopilot's own data). */
  headingTrueDeg?: number | null
  /**
   * Cosmetic-only role gate (ADR 0040 §frontend, backend `write` tier): the
   * server is the actual enforcement point for every /api/autopilot/*
   * command, this just avoids offering a control below readwrite that would
   * only 403. Deliberately does not reuse `stale`'s greyscale/banner
   * treatment — this is a permission gate, not a data-quality one, and the
   * two must stay visually distinguishable.
   */
  readOnly?: boolean
}

function formatDegrees(value: number | null | undefined): string {
  if (value === null || value === undefined || value < 0) return '--'
  return `${Math.round(value)}°`
}

interface HoldToConfirmButtonProps {
  label: string
  ariaLabel: string
  disabled: boolean
  onConfirm: () => void
  className?: string
}

/**
 * A button that only fires after being held for HOLD_TO_CONFIRM_MS. Releasing
 * early — a short tap — cancels it silently rather than firing on release,
 * so an accidental brush of the tile can't engage/disengage/tack/gybe.
 */
function HoldToConfirmButton({ label, ariaLabel, disabled, onConfirm, className }: HoldToConfirmButtonProps) {
  const [holding, setHolding] = useState(false)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const clear = useCallback(() => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current)
      timerRef.current = null
    }
    setHolding(false)
  }, [])

  const start = useCallback(() => {
    if (disabled) return
    setHolding(true)
    timerRef.current = setTimeout(() => {
      timerRef.current = null
      setHolding(false)
      onConfirm()
    }, HOLD_TO_CONFIRM_MS)
  }, [disabled, onConfirm])

  return (
    <button
      type="button"
      disabled={disabled}
      aria-label={ariaLabel}
      onPointerDown={start}
      onPointerUp={clear}
      onPointerLeave={clear}
      onPointerCancel={clear}
      className={cn(
        'relative overflow-hidden rounded-md px-3 py-2 text-center font-display text-xs font-semibold uppercase tracking-[0.14em]',
        'transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2',
        'disabled:cursor-not-allowed disabled:opacity-40',
        className,
      )}
    >
      <span
        aria-hidden="true"
        className={cn(
          'pointer-events-none absolute inset-0 bg-white/25 transition-transform ease-linear',
          holding ? 'translate-x-0' : '-translate-x-full',
        )}
        style={{ transitionDuration: holding ? `${HOLD_TO_CONFIRM_MS}ms` : '0ms' }}
      />
      <span className="relative">{label}</span>
    </button>
  )
}

interface ImmediateButtonProps {
  label: string
  ariaLabel?: string
  disabled: boolean
  onClick: () => void
  className?: string
}

function ImmediateButton({ label, ariaLabel, disabled, onClick, className }: ImmediateButtonProps) {
  return (
    <button
      type="button"
      disabled={disabled}
      aria-label={ariaLabel}
      onClick={onClick}
      className={cn(
        'rounded-md border border-border bg-card px-2 py-2 text-center font-display text-sm font-semibold tabular-nums text-foreground',
        'transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2',
        'disabled:cursor-not-allowed disabled:opacity-40',
        className,
      )}
    >
      {label}
    </button>
  )
}

export const AutopilotTile = memo(function AutopilotTile({
  state,
  pending,
  error,
  availableModes,
  capabilityError,
  onEngage,
  onDisengage,
  onTack,
  onGybe,
  onAdjustHeading,
  onSetMode,
  onDodge,
  onClearDodge,
  headingTrueDeg,
  readOnly = false,
}: AutopilotTileProps) {
  const modeBadge = state.present ? (state.mode ?? state.state ?? '').toUpperCase() : ''

  const titleExtra = state.present && modeBadge ? (
    <span className="truncate text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">
      {modeBadge}
    </span>
  ) : null

  return (
    <Tile title="Autopilot" icon={<Compass className="h-3.5 w-3.5 text-gauge-secondary" />} titleExtra={titleExtra}>
      {!state.present ? (
        <div className="mt-3 rounded-md border border-dashed bg-muted/25 px-3 py-6 text-center text-sm text-muted-foreground">
          No autopilot detected on this SignalK server.
        </div>
      ) : (
        <AutopilotControls
          state={state}
          pending={pending}
          error={error}
          availableModes={availableModes}
          capabilityError={capabilityError}
          onEngage={onEngage}
          onDisengage={onDisengage}
          onTack={onTack}
          onGybe={onGybe}
          onAdjustHeading={onAdjustHeading}
          onSetMode={onSetMode}
          onDodge={onDodge}
          onClearDodge={onClearDodge}
          headingTrueDeg={headingTrueDeg}
          readOnly={readOnly}
        />
      )}
    </Tile>
  )
})

function AutopilotControls({
  state,
  pending,
  error,
  availableModes,
  capabilityError,
  onEngage,
  onDisengage,
  onTack,
  onGybe,
  onAdjustHeading,
  onSetMode,
  onDodge,
  onClearDodge,
  headingTrueDeg,
  readOnly,
}: Required<Pick<AutopilotTileProps, 'state' | 'pending' | 'error' | 'availableModes' | 'capabilityError' | 'onEngage' | 'onDisengage' | 'onTack' | 'onGybe' | 'onAdjustHeading' | 'onSetMode' | 'onDodge' | 'onClearDodge' | 'readOnly'>> & { headingTrueDeg?: number | null }) {
  const { engaged, target, stale, availableActions, mode } = state

  // Stale steering data can't be trusted: no command should go out while the
  // tile doesn't actually know the pilot's current state (ADR 0041).
  const locked = stale
  // Every command is additionally disabled below readwrite (ADR 0040), but
  // deliberately kept out of `locked` — that drives the stale-specific
  // greyscale/banner treatment below, which must not fire for a permission
  // gate that has nothing to do with data quality.
  const commandsDisabled = locked || readOnly
  const has = (action: string) => availableActions.includes(action)

  const engageDisabled = commandsDisabled || pending.has(engaged ? 'disengage' : 'engage') || !has(engaged ? 'disengage' : 'engage')
  const adjustDisabled = commandsDisabled || !has('adjustHeading')
  const dodgeDisabled = commandsDisabled || !has('dodge')

  return (
    <div className={cn('mt-2 flex flex-col gap-3', stale && 'opacity-60 grayscale')}>
      {stale && (
        <div className="rounded-md border border-amber-600/40 bg-amber-950/20 px-2 py-1 text-center text-[10px] font-semibold uppercase tracking-[0.14em] text-amber-500">
          Stale — steering data lost
        </div>
      )}

      <div className="flex items-end justify-center gap-3 px-1">
        <div className="flex min-w-0 flex-col items-center">
          <span className="text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground">Heading</span>
          <span className="font-display text-2xl font-bold tabular-nums tracking-tight text-gauge-secondary">
            {formatDegrees(headingTrueDeg)}
          </span>
        </div>
        <span aria-hidden="true" className="pb-2 text-muted-foreground">→</span>
        <div className="flex min-w-0 flex-col items-center">
          <span className="text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground">Target</span>
          <span className="font-display text-2xl font-bold tabular-nums tracking-tight text-gauge-primary">
            {formatDegrees(target)}
          </span>
        </div>
      </div>

      {/*
        Which modes exist comes from the capability probe, not the delta
        stream. If that probe failed the selector is omitted entirely and the
        reason shown — rendering a guessed compass/wind/gps set would offer
        modes this pilot may not have.
      */}
      {availableModes.length > 0 ? (
        <div className="flex flex-wrap gap-1.5">
          {availableModes.map((m) => {
            const active = m === mode
            return (
              <HoldToConfirmButton
                key={m}
                label={m}
                ariaLabel={active ? `${m} mode (active)` : `Hold to switch to ${m} mode`}
                disabled={active || commandsDisabled || pending.has(`mode-${m}`)}
                onConfirm={() => onSetMode(m)}
                className={cn(
                  'flex-1 px-2 py-1.5 text-[10px]',
                  active
                    ? 'bg-gauge-primary/20 text-gauge-primary ring-1 ring-inset ring-gauge-primary/40 disabled:opacity-100'
                    : 'border border-border bg-card text-muted-foreground hover:bg-muted',
                )}
              />
            )
          })}
        </div>
      ) : capabilityError ? (
        <p className="text-center text-[10px] text-muted-foreground" role="status">
          Mode switching unavailable — {capabilityError}
        </p>
      ) : null}

      <div className="grid grid-cols-4 gap-2">
        <ImmediateButton label="-10°" disabled={adjustDisabled} onClick={() => onAdjustHeading(-10)} />
        <ImmediateButton label="-1°" disabled={adjustDisabled} onClick={() => onAdjustHeading(-1)} />
        <ImmediateButton label="+1°" disabled={adjustDisabled} onClick={() => onAdjustHeading(1)} />
        <ImmediateButton label="+10°" disabled={adjustDisabled} onClick={() => onAdjustHeading(10)} />
      </div>

      {/*
        Dodge steers around an obstacle without giving up the target course,
        so it is an immediate nudge like the heading buttons rather than a
        held one — needing to hold a button for 800ms with a pot buoy coming
        up is the wrong trade.
      */}
      <div className="grid grid-cols-3 gap-2">
        <ImmediateButton
          label="◄ Dodge"
          ariaLabel={`Dodge ${DODGE_STEP_DEG}° to port`}
          disabled={dodgeDisabled}
          onClick={() => onDodge(-DODGE_STEP_DEG)}
        />
        <ImmediateButton
          label="Cancel"
          ariaLabel="Cancel dodge"
          disabled={dodgeDisabled}
          onClick={onClearDodge}
        />
        <ImmediateButton
          label="Dodge ►"
          ariaLabel={`Dodge ${DODGE_STEP_DEG}° to starboard`}
          disabled={dodgeDisabled}
          onClick={() => onDodge(DODGE_STEP_DEG)}
        />
      </div>

      {/*
        Tack and gybe are offered separately and gated on their own advertised
        actions, rather than one button relabelled from the steering mode: a
        pilot may support one and not the other, and inferring which the crew
        meant from `mode` is a guess about a manoeuvre that swings the boom.
      */}
      <div className="grid grid-cols-2 gap-2">
        {(['tack', 'gybe'] as const).map((maneuver) =>
          (['port', 'starboard'] as const).map((side) => (
            <HoldToConfirmButton
              key={`${maneuver}-${side}`}
              label={side === 'port' ? `◄ ${maneuver}` : `${maneuver} ►`}
              ariaLabel={`Hold to ${maneuver} to ${side}`}
              disabled={commandsDisabled || !has(maneuver) || pending.has(`${maneuver}-${side}`)}
              onConfirm={() => (maneuver === 'tack' ? onTack : onGybe)(side)}
              className="border border-border bg-card capitalize text-foreground hover:bg-muted"
            />
          )),
        )}
      </div>

      <HoldToConfirmButton
        label={engaged ? 'Hold to disengage' : 'Hold to engage'}
        ariaLabel={engaged ? 'Hold to disengage autopilot' : 'Hold to engage autopilot'}
        disabled={engageDisabled}
        onConfirm={engaged ? onDisengage : onEngage}
        className={cn(
          'py-3 text-sm',
          engaged
            ? 'bg-red-600 text-white hover:bg-red-500 dark:bg-red-700 dark:hover:bg-red-600'
            : 'bg-emerald-600 text-white hover:bg-emerald-500 dark:bg-emerald-700 dark:hover:bg-emerald-600',
        )}
      />

      {error && (
        <p className="text-center text-[11px] font-medium text-red-500" role="alert">
          {error}
        </p>
      )}
    </div>
  )
}
