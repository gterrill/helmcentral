import { Radar } from 'lucide-react'
import { memo } from 'react'

import { Tile } from '@/components/ui/tile'
import type { DistanceUnits } from '@/config/app-config'
import type { RadarInfo, RadarSource, RadarTarget } from '@/hooks/use-radar-targets'
import { cn } from '@/lib/utils'

interface RadarTargetsTileProps {
  targets: RadarTarget[]
  radars: RadarInfo[]
  source: RadarSource
  loading: boolean
  distanceUnits: DistanceUnits
}

function formatRange(rangeMeters: number, distanceUnits: DistanceUnits) {
  if (distanceUnits === 'imperial') {
    return `${Math.round(rangeMeters * 3.28084)} ft`
  }
  return `${Math.round(rangeMeters)} m`
}

function radiansToDeg(rad: number): number {
  return Math.round((((rad * 180) / Math.PI) % 360 + 360) % 360)
}

// mm:ss for a positive TCPA. A negative value means the target has already
// passed its closest point (mayara still reports it, ADR 0062) — that reads
// as "passed" rather than a confusing negative countdown.
function formatTcpa(seconds: number): string {
  if (seconds < 0) return 'passed'
  const mins = Math.floor(seconds / 60)
  const secs = Math.round(seconds % 60)
  return `${mins}:${String(secs).padStart(2, '0')}`
}

// One line naming the radar and, when unhealthy, its state — never one line
// per physical radar. This boat's Furuno already presents two radar keys off
// one antenna (dual range), so a literal per-radar list would duplicate the
// same physical unit on screen.
function radarHeaderLabel(radars: RadarInfo[]): string {
  if (radars.length === 0) return 'Radar'
  const name = radars.length === 1 ? radars[0].name : `${radars[0].name} +${radars.length - 1}`
  // Dual range is one antenna, so its power state is shared; the first radar
  // speaks for both.
  return radars[0].transmitting ? name : `${name} · STANDBY`
}

// Mirrors tempClass() in alternator-tile.tsx: alert colour reserved strictly
// for the condition it signals, applied consistently across the value and
// its row.
function dangerClasses(isDangerous: boolean) {
  return {
    row: isDangerous ? 'border-red-500/60 bg-red-500/15' : 'bg-muted/45',
    value: isDangerous ? 'text-red-500' : 'text-foreground',
    accent: isDangerous ? 'text-red-500' : 'text-gauge-secondary',
  }
}

function TargetRow({ target, distanceUnits }: { target: RadarTarget; distanceUnits: DistanceUnits }) {
  const { row, value, accent } = dangerClasses(target.is_dangerous)
  const hasDanger = target.cpa_m !== undefined && target.tcpa_seconds !== undefined

  return (
    <div className={cn('flex items-center justify-between gap-2 rounded-md border px-3 py-2', row)}>
      <div className="min-w-0">
        <p className="text-[10px] uppercase tracking-wider text-muted-foreground">
          RDR · {radiansToDeg(target.bearing_rad)}°
        </p>
        <p className={cn('font-display text-lg uppercase leading-none', value)}>
          {formatRange(target.range_m, distanceUnits)}
        </p>
      </div>
      {hasDanger ? (
        <div className="shrink-0 text-right">
          <p className={cn('text-xs', accent)}>{formatRange(target.cpa_m!, distanceUnits)} CPA</p>
          <p className="mt-1 text-xs text-muted-foreground">{formatTcpa(target.tcpa_seconds!)} TCPA</p>
        </div>
      ) : null}
    </div>
  )
}

export const RadarTargetsTile = memo(function RadarTargetsTile({
  targets,
  radars,
  source,
  loading,
  distanceUnits,
}: RadarTargetsTileProps) {
  return (
    <Tile title="Radar Targets" icon={<Radar className="h-3.5 w-3.5 text-gauge-secondary" />}>
      <div className="mt-3 space-y-2">
        {source === 'disabled' ? (
          <div className="rounded-md border border-dashed bg-muted/25 px-3 py-4 text-center text-sm text-muted-foreground">--</div>
        ) : (
          <>
            <p
              className={cn(
                'text-[10px] uppercase tracking-wider',
                source === 'mayara-unreachable' ? 'text-red-500' : 'text-muted-foreground',
              )}
            >
              {source === 'mayara-unreachable' ? 'RADAR OFFLINE' : radarHeaderLabel(radars)}
            </p>

            {/* A stream flagged unreachable may still carry targets fading
                out of the eviction window (ADR 0062 §5) — up to 30 s of data
                the operator can no longer trust. Show the state, not the
                stale rows. */}
            {source === 'mayara' && targets.map((target) => (
              <TargetRow key={target.id} target={target} distanceUnits={distanceUnits} />
            ))}

            {source === 'mayara' && !loading && targets.length === 0 ? (
              <div className="rounded-md border border-dashed bg-muted/25 px-3 py-4 text-center text-sm text-muted-foreground">--</div>
            ) : null}

            {source === 'mayara' && loading ? (
              <div className="rounded-md border border-dashed bg-muted/25 px-3 py-4 text-center text-sm text-muted-foreground">
                Loading radar targets...
              </div>
            ) : null}
          </>
        )}
      </div>
    </Tile>
  )
})
