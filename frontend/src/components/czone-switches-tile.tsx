import { Zap } from 'lucide-react'

import { Tile } from '@/components/ui/tile'
import type { CZoneSwitch } from '@/hooks/use-czone-switches'

type CZoneSwitchesTileProps = {
  switches: CZoneSwitch[]
  loading: boolean
  pending: Set<string>
  onToggle: (id: string, newState: 0 | 1) => void
}

export function CZoneSwitchesTile({ switches, loading, pending, onToggle }: CZoneSwitchesTileProps) {
  return (
    <Tile title="CZone" icon={<Zap className="h-3.5 w-3.5 text-gauge-secondary" />}>
      <div className="mt-3 space-y-1.5">
        {switches.map((sw) => {
          const isOn = sw.state === 1
          const isPending = pending.has(sw.id)

          return (
            <div key={sw.id} className="flex items-center gap-3 rounded-md px-1 py-0.5">
              <p className="min-w-0 flex-1 truncate font-display text-sm uppercase leading-tight tracking-[0.08em] text-foreground">
                {sw.display_name}
              </p>
              <button
                type="button"
                disabled={isPending}
                onClick={() => onToggle(sw.id, isOn ? 0 : 1)}
                aria-label={`${sw.display_name}: ${isOn ? 'on' : 'off'}, tap to toggle`}
                aria-pressed={isOn}
                className={[
                  'relative inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full',
                  'transition-colors duration-200',
                  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2',
                  'disabled:cursor-wait disabled:opacity-50',
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

        {!loading && switches.length === 0 ? (
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
}
