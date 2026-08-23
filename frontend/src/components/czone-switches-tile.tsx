import { Zap } from 'lucide-react'
import { memo } from 'react'

import { Tile } from '@/components/ui/tile'
import type { CZoneSwitch } from '@/hooks/use-czone-switches'

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

export const CZoneSwitchesTile = memo(function CZoneSwitchesTile({ switches, loading, pending, onToggle, error = null, readOnly = false }: CZoneSwitchesTileProps) {
  const hasSwitches = switches.length > 0

  return (
    <Tile title="CZone" icon={<Zap className="h-3.5 w-3.5 text-gauge-secondary" />}>
      <div className="mt-3 space-y-1.5">
        {error && hasSwitches ? (
          <div
            role="alert"
            className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-500"
          >
            Could not refresh — showing last known switch state.
          </div>
        ) : null}

        {switches.map((sw) => {
          const isOn = sw.state === 1
          const isPending = pending.has(sw.id)
          // A switch is interactive only when SignalK's meta.supportsPut says
          // so and the user isn't role-gated to readonly. Most bank circuits
          // are status indicators, not controllable outputs (Finding 2) — the
          // control is disabled but the state stays visible either way.
          const interactive = sw.writable && !readOnly
          const isDisabled = isPending || !interactive
          const cursorClass = isPending ? 'cursor-wait' : interactive ? 'cursor-pointer' : 'cursor-not-allowed'

          return (
            <div key={sw.id} className="flex items-center gap-3 rounded-md px-1 py-0.5">
              <p className="min-w-0 flex-1 truncate font-display text-sm uppercase leading-tight tracking-[0.08em] text-foreground">
                {sw.display_name}
              </p>
              <button
                type="button"
                disabled={isDisabled}
                onClick={() => onToggle(sw.id, isOn ? 0 : 1)}
                aria-label={`${sw.display_name}: ${isOn ? 'on' : 'off'}, ${interactive ? 'tap to toggle' : 'read-only'}`}
                aria-pressed={isOn}
                className={[
                  'relative inline-flex h-6 w-11 shrink-0 items-center rounded-full',
                  'transition-colors duration-200',
                  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2',
                  'disabled:opacity-50',
                  cursorClass,
                  isOn ? 'bg-emerald-600' : 'bg-muted',
                ].join(' ')}
              >
                <span
                  className={[
                    'pointer-events-none inline-block h-4 w-4 rounded-full bg-white shadow-md',
                    'transition-transform duration-200',
                    isOn ? 'translate-x-6' : 'translate-x-1',
                  ].join(' ')}
                />
              </button>
              <span
                className={[
                  'w-6 shrink-0 text-right font-display text-xs uppercase leading-none tabular-nums',
                  isOn ? 'text-emerald-600' : 'text-muted-foreground',
                ].join(' ')}
                aria-hidden="true"
              >
                {isOn ? 'ON' : 'OFF'}
              </span>
            </div>
          )
        })}

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
