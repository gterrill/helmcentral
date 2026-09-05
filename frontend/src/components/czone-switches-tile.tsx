import { Zap } from 'lucide-react'
import { memo } from 'react'

import { Tile } from '@/components/ui/tile'
import type { CZoneSwitch } from '@/hooks/use-czone-switches'
import { cn } from '@/lib/utils'

type CZoneSwitchesTileProps = {
  switches: CZoneSwitch[]
  loading: boolean
  pending: Set<string>
  onToggle: (id: string, newState: 0 | 1) => void
  /**
   * Set when the last fetch/poll failed (backend 502, or a network error).
   * Distinguishes "this vessel has no switches" from "the switch panel could
   * not be read" — the two must not look the same in the UI.
   */
  error?: string | null
  /**
   * Cosmetic-only role gate (ADR 0040 §frontend, backend `write` tier): the
   * server is the actual enforcement point for a PUT to
   * /api/czone/switches/:id/state, this just avoids offering a control below
   * readonly that would only 403.
   */
  readOnly?: boolean
}

/**
 * A single toggleable circuit. `sw.writable` (SignalK's meta.supportsPut) is
 * always true in this group — `readOnly` is a separate, cosmetic role gate
 * that can still disable it (ADR 0040 §frontend).
 */
function ControlRow({ sw, isPending, readOnly, onToggle }: { sw: CZoneSwitch; isPending: boolean; readOnly: boolean; onToggle: (id: string, newState: 0 | 1) => void }) {
  const isOn = sw.state === 1
  const interactive = !readOnly
  const isDisabled = isPending || !interactive
  const cursorClass = isPending ? 'cursor-wait' : interactive ? 'cursor-pointer' : 'cursor-not-allowed'

  return (
    <div className="flex items-center gap-3 rounded-md px-1 py-0.5">
      <p className="min-w-0 flex-1 truncate font-display text-sm uppercase leading-tight tracking-[0.08em] text-foreground">
        {sw.display_name}
      </p>
      <button
        type="button"
        disabled={isDisabled}
        onClick={() => onToggle(sw.id, isOn ? 0 : 1)}
        aria-label={`${sw.display_name}: ${isOn ? 'on' : 'off'}, ${interactive ? 'tap to toggle' : 'read-only'}`}
        aria-pressed={isOn}
        className={cn(
          'relative inline-flex h-6 w-11 shrink-0 items-center rounded-full',
          'transition-colors duration-200',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2',
          'disabled:opacity-50',
          cursorClass,
          // Switches spec (DESIGN.md): --input unchecked, Signal Blue
          // checked. "On" is a control state, not a reading — it belongs on
          // the primary token, never on an alert-semantics colour.
          isOn ? 'bg-primary' : 'bg-input',
        )}
      >
        <span
          className={cn(
            'pointer-events-none inline-block h-4 w-4 rounded-full bg-white shadow-md',
            'transition-transform duration-200',
            isOn ? 'translate-x-6' : 'translate-x-1',
          )}
        />
      </button>
      <span
        className={cn(
          'w-6 shrink-0 text-right font-display text-xs uppercase leading-none tabular-nums',
          isOn ? 'text-primary' : 'text-muted-foreground',
        )}
        aria-hidden="true"
      >
        {isOn ? 'ON' : 'OFF'}
      </span>
    </div>
  )
}

/**
 * A status-only circuit (SignalK's meta.supportsPut is false — most bank
 * circuits are PGN 127501 status reports, not controllable outputs). No
 * toggle affordance at all: the old disabled-button rendering was
 * indistinguishable from a real control until tapped and nothing happened.
 */
function IndicatorRow({ sw }: { sw: CZoneSwitch }) {
  const isOn = sw.state === 1

  return (
    <div
      className="flex items-center gap-3 rounded-md px-1 py-0.5"
      role="group"
      aria-label={`${sw.display_name}: ${isOn ? 'on' : 'off'}, read-only`}
    >
      <p className="min-w-0 flex-1 truncate font-display text-sm uppercase leading-tight tracking-[0.08em] text-foreground">
        {sw.display_name}
      </p>
      <span
        className={cn(
          'w-10 shrink-0 rounded-sm border px-1.5 py-0.5 text-center font-display text-[10px] uppercase leading-none tracking-[0.08em]',
          isOn ? 'border-primary/40 bg-primary/10 text-primary' : 'border-border text-muted-foreground',
        )}
      >
        {isOn ? 'ON' : 'OFF'}
      </span>
    </div>
  )
}

export const CZoneSwitchesTile = memo(function CZoneSwitchesTile({ switches, loading, pending, onToggle, error = null, readOnly = false }: CZoneSwitchesTileProps) {
  const hasSwitches = switches.length > 0
  // Split by the device's own capability (SignalK's meta.supportsPut), not
  // by whether this session happens to be allowed to use it right now — a
  // circuit that is genuinely a status report stays an indicator even for a
  // readwrite user, and a genuinely writable one stays a (disabled) control
  // even for a readonly user (Finding 4).
  const controls = switches.filter((sw) => sw.writable)
  const indicators = switches.filter((sw) => !sw.writable)

  return (
    <Tile title="CZone" icon={<Zap className="h-3.5 w-3.5 text-gauge-secondary" />}>
      <div className="mt-3 space-y-3">
        {error && hasSwitches ? (
          <div
            role="alert"
            className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-500"
          >
            Could not refresh — showing last known switch state.
          </div>
        ) : null}

        {controls.length > 0 ? (
          <div className="space-y-1.5">
            <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Controls</p>
            {controls.map((sw) => (
              <ControlRow key={sw.id} sw={sw} isPending={pending.has(sw.id)} readOnly={readOnly} onToggle={onToggle} />
            ))}
          </div>
        ) : null}

        {indicators.length > 0 ? (
          <div className="space-y-1.5">
            <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Indicators</p>
            {indicators.map((sw) => (
              <IndicatorRow key={sw.id} sw={sw} />
            ))}
          </div>
        ) : null}

        {error && !hasSwitches ? (
          <div
            role="alert"
            className="rounded-md border border-dashed border-amber-500/40 bg-amber-500/10 px-3 py-4 text-center text-sm text-amber-600"
          >
            {error}
          </div>
        ) : null}

        {!error && !loading && !hasSwitches ? (
          <div className="rounded-md border border-dashed bg-muted/25 px-3 py-4 text-center text-sm text-muted-foreground">
            No CZone switches found
          </div>
        ) : null}

        {loading ? (
          <div className="rounded-md border border-dashed bg-muted/25 px-3 py-4 text-center text-sm text-muted-foreground">
            Loading switches…
          </div>
        ) : null}
      </div>
    </Tile>
  )
})
