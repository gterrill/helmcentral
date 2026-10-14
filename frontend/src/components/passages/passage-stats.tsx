import type { ReactNode } from 'react'

import type { DistanceUnits } from '@/config/app-config'
import { Button } from '@/components/ui/button'
import {
  ABSENT,
  fmtDuration,
  fmtHours,
  fmtKn,
  fmtVolume,
  fmtNM,
  fmtPct,
  formatLocalDate,
  formatLocalDateTime,
  type CruiseStats,
  type PassageStats,
  type WaterRise,
  type WaterRiseLabel,
} from '@/lib/passages'
import { cn } from '@/lib/utils'

// KPI stack per the dashboard spec: muted uppercase label over a large
// tabular readout. An absent reading is `--`, never an estimate.

export function Kpi({ label, value, sub, className }: { label: string; value: ReactNode; sub?: ReactNode; className?: string }) {
  return (
    <div className={cn('flex min-w-0 flex-col gap-1 rounded-md border border-border bg-card p-3', className)}>
      <span className="truncate text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{label}</span>
      <span className="truncate font-display text-2xl font-bold tabular-nums tracking-tight text-gauge-primary">{value}</span>
      {sub !== undefined && <span className="truncate text-xs tabular-nums text-muted-foreground">{sub}</span>}
    </div>
  )
}

function Grid({ children, testId }: { children: ReactNode; testId: string }) {
  return (
    <div data-testid={testId} className="grid grid-cols-[repeat(auto-fill,minmax(9rem,1fr))] gap-2">
      {children}
    </div>
  )
}

export function PassageStatsStrip({ stats, units }: { stats: PassageStats; units: DistanceUnits }) {
  return (
    <Grid testId="passage-stats">
      <Kpi label="Distance" value={fmtNM(stats.distance_nm)} />
      <Kpi label="Underway" value={fmtDuration(stats.underway_seconds)} />
      <Kpi label="Mean speed" value={fmtKn(stats.mean_sog_kts)} sub={`Max ${fmtKn(stats.max_sog_kts)}`} />
      <Kpi label="Fuel burned" value={fmtVolume(stats.fuel_burned_l, units)} />
      <Kpi label="Wind" value={fmtKn(stats.tws_mean_kts)} sub={`Gust ${fmtKn(stats.max_gust_kts)}`} />
      <Kpi
        label="House battery"
        value={fmtPct(stats.soc_end_pct)}
        sub={`Start ${fmtPct(stats.soc_start_pct)} · Low ${fmtPct(stats.soc_min_pct)}`}
      />
    </Grid>
  )
}

function windSub(maxGust: number | null) {
  return `Gust ${fmtKn(maxGust)}`
}

function nightLabel(night: string): string {
  const m = /^\d{4}-(\d{2})-(\d{2})$/.exec(night)
  if (!m) return night
  const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
  return `${Number(m[2])} ${months[Number(m[1]) - 1] ?? ''}`.trim()
}

interface WaterRiseRowProps {
  rise: WaterRise
  lon: number
  units: DistanceUnits
  canWrite: boolean
  onLabel: (rise: WaterRise, label: WaterRiseLabel | '') => void
}

const RISE_LABELS: WaterRiseLabel[] = ['Watermaker', 'Dock fill']

export function WaterRiseRow({ rise, lon, units, canWrite, onLabel }: WaterRiseRowProps) {
  const confirmed = rise.label as WaterRiseLabel | null
  const suggested = confirmed || rise.suggestion_status !== 'found' ? null : (rise.suggested_label as WaterRiseLabel | null)
  const checkFailed = !confirmed && rise.suggestion_status === 'failed'
  return (
    <li className="flex flex-wrap items-center gap-2 text-xs" data-testid="water-rise-row">
      <span className="min-w-0 flex-1 truncate tabular-nums text-foreground">
        {formatLocalDateTime(Date.parse(rise.start) / 1000, lon)} · +{fmtVolume(rise.litres, units)}
      </span>
      <span className="text-muted-foreground">
        {confirmed ? 'Confirmed' : suggested ? `Suggested: ${suggested}` : checkFailed ? "Couldn't check for a marina" : 'Not confirmed'}
      </span>
      <span className="flex gap-1" role="group" aria-label="Water source">
        {RISE_LABELS.map((label) => (
          <Button
            key={label}
            type="button"
            size="sm"
            variant={confirmed === label ? 'default' : suggested === label ? 'secondary' : 'outline'}
            aria-pressed={confirmed === label}
            disabled={!canWrite}
            onClick={() => onLabel(rise, confirmed === label ? '' : label)}
          >
            {label}
          </Button>
        ))}
      </span>
    </li>
  )
}

export interface CruiseStatsPanelProps {
  stats: CruiseStats
  /** Longitude of the cruise, for boat-local dates. */
  lon: number
  units: DistanceUnits
  canWrite: boolean
  onLabelRise: (rise: WaterRise, label: WaterRiseLabel | '') => void
}

export function CruiseStatsPanel({ stats, units, lon, canWrite, onLabelRise }: CruiseStatsPanelProps) {
  const span = stats.departure && stats.arrival
    ? `${formatLocalDate(stats.departure, lon)} to ${formatLocalDate(stats.arrival, lon)}`
    : ABSENT
  return (
    <div className="flex flex-col gap-3" data-testid="cruise-stats">
      <Grid testId="cruise-stats-grid">
        <Kpi label="Departure to arrival" value={span} sub={`${stats.passage_count} ${stats.passage_count === 1 ? 'passage' : 'passages'}`} />
        <Kpi label="Underway" value={fmtDuration(stats.underway_seconds)} />
        <Kpi label="Nights" value={String(stats.nights)} />
        <Kpi label="Distance" value={fmtNM(stats.distance_nm)} />
        <Kpi label="Fuel burned" value={fmtVolume(stats.fuel_burned_l, units)} />
        <Kpi label="Wind underway" value={fmtKn(stats.underway_tws_mean_kts)} sub={windSub(stats.underway_max_gust_kts)} />
        <Kpi label="Wind at anchor" value={fmtKn(stats.anchor_tws_mean_kts)} sub={windSub(stats.anchor_max_gust_kts)} />
        <Kpi label="Generator" value={fmtHours(stats.generator_hours)} />
        <Kpi label="Lowest battery" value={fmtPct(stats.lowest_soc_pct)} />
        <Kpi label="Water used" value={fmtVolume(stats.water_used_l, units)} />
        <Kpi label="Water taken on" value={fmtVolume(stats.water_taken_on_l, units)} />
      </Grid>

      <section aria-label="Battery at dawn" className="flex flex-col gap-2 rounded-md border border-border bg-card p-3">
        <h3 className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Battery at dawn</h3>
        {stats.night_soc.length === 0 ? (
          <p className="text-xs text-muted-foreground">No nights at anchor in this cruise.</p>
        ) : (
          <ul className="flex flex-wrap gap-2" data-testid="night-soc">
            {stats.night_soc.map((n) => (
              <li key={n.night} className="flex flex-col rounded-sm border border-border px-2 py-1">
                <span className="text-[10px] uppercase tracking-wider text-muted-foreground">{nightLabel(n.night)}</span>
                <span className="font-display text-base font-bold tabular-nums text-gauge-primary">{fmtPct(n.dawn_soc_pct)}</span>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section aria-label="Water taken on" className="flex flex-col gap-2 rounded-md border border-border bg-card p-3">
        <h3 className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Water taken on</h3>
        {stats.water_rises.length === 0 ? (
          <p className="text-xs text-muted-foreground">No rise in the fresh water tanks was recorded.</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {stats.water_rises.map((r) => (
              <WaterRiseRow key={r.start} rise={r} lon={lon} units={units} canWrite={canWrite} onLabel={onLabelRise} />
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}
