import type { ReactNode } from 'react'

import type { DistanceUnits } from '@/config/app-config'
import { Button } from '@/components/ui/button'
import { PASSAGE_RATING_LABELS, type PassageRating } from '@/lib/passage-plans'
import {
  ABSENT,
  fmtDuration,
  fmtHours,
  fmtKn,
  fmtVolume,
  fmtVolumePerNM,
  fmtNM,
  fmtPct,
  formatLocalDate,
  formatLocalDateTime,
  feltHeadline,
  joinSeaParts,
  type CruiseStats,
  type PassageFelt,
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

/** A maximum that moves the replay to the moment it happened. Plain text when
 * there is no replay to move or the time is not known. */
export function MaxButton({
  label, shown, what, at, onSeek, className,
}: { label: string; shown: string; what: string; at: number | null | undefined; onSeek?: (at: number) => void; className?: string }) {
  if (!onSeek || at === null || at === undefined) return <>{label}</>
  return (
    <button
      type="button"
      aria-label={`Show max ${what} ${shown} on the replay`}
      className={cn('-mx-1 rounded-sm px-1 underline decoration-dotted underline-offset-2 hover:text-foreground focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring', className)}
      onClick={() => onSeek(at)}
    >
      {label}
    </button>
  )
}

/** The roll, pitch and heave the boat peaked at, each a button to its moment.
 * A figure that was not recorded is left out; with none, it says so. */
export function PassageMotion({ stats, onSeek }: { stats: PassageStats; onSeek?: (at: number) => void }) {
  const figures = [
    { key: 'roll', value: stats.max_roll_deg, at: stats.max_roll_at, text: (v: number) => `${Math.round(v)}°` },
    { key: 'pitch', value: stats.max_pitch_deg, at: stats.max_pitch_at, text: (v: number) => `${Math.round(v)}°` },
    { key: 'heave', value: stats.max_heave_m, at: stats.max_heave_at, text: (v: number) => `${v.toFixed(1)} m` },
  ].flatMap((f) => (typeof f.value === 'number' && Number.isFinite(f.value) ? [{ ...f, shown: f.text(f.value) }] : []))
  if (figures.length === 0) {
    return <p className="text-xs text-muted-foreground">Motion not recorded for this passage</p>
  }
  return (
    <p data-testid="passage-motion" className="text-sm tabular-nums text-foreground">
      Max{' '}
      {figures.map((f, i) => (
        <span key={f.key}>
          {i > 0 && ', '}
          <MaxButton label={`${f.key} ${f.shown}`} shown={f.shown} what={f.key} at={f.at} onSeek={onSeek} />
        </span>
      ))}
    </p>
  )
}

function snapText(snap: PassageFelt['snap']): string | null {
  if (snap.value === null || !Number.isFinite(snap.value)) return null
  return snap.kind === 'sideways_g' ? `${snap.value.toFixed(2)} g at the helm` : `${Math.round(snap.value)}°/s² roll acceleration`
}

/** How the passage felt: the band word beside the operator's own rating, the
 * felt figure, and the snap and pounding peaks, each a button to its moment.
 * A figure that was not recorded says why. The rating is never changed here. */
export function PassageFeltRow({ felt, rating, onSeek, timeZone = null, comfortModel }: { felt: PassageFelt | undefined; rating: PassageRating; onSeek?: (at: number) => void; timeZone?: string | null; comfortModel?: string }) {
  if (!felt) return null
  const snap = snapText(felt.snap)
  const pounding = felt.pounding.value_g !== null && Number.isFinite(felt.pounding.value_g) ? `${felt.pounding.value_g.toFixed(2)} g` : null
  // With no band word the headline already shows the degrees; the line below
  // leaves its own "felt" figure out rather than say the same number twice.
  const showDeg = felt.deg !== null && felt.band !== null
  return (
    <div data-testid="passage-felt" className="flex flex-col gap-1">
      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
        <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Felt</span>
        <span data-testid="felt-band" className="font-display text-base font-bold text-foreground">
          {feltHeadline(felt.band, felt.deg)}
        </span>
        {felt.band_note && <span className="text-xs text-muted-foreground">{felt.band_note}</span>}
        <span data-testid="felt-rating" className="text-xs text-muted-foreground">
          {rating ? `You rated it ${PASSAGE_RATING_LABELS[rating]}` : 'Not rated'}
        </span>
      </div>
      {felt.deg === null && <p className="text-xs text-muted-foreground">{felt.reason}</p>}
      {(showDeg || snap || pounding) && (
        <p className="text-sm tabular-nums text-foreground">
          {showDeg && `${felt.deg!.toFixed(1)}° felt`}
          {snap && (
            <>
              {showDeg && ' · '}
              {felt.snap.at !== null ? (
                <MaxButton label={`Snap ${snap}`} shown={snap} what="snap" at={felt.snap.at} onSeek={onSeek} />
              ) : (
                `Snap ${snap}`
              )}
            </>
          )}
          {pounding && (
            <>
              {(showDeg || snap) && ' · '}
              {felt.pounding.at !== null ? (
                <MaxButton label={`Pounding ${pounding}`} shown={pounding} what="pounding" at={felt.pounding.at} onSeek={onSeek} />
              ) : (
                `Pounding ${pounding}`
              )}
            </>
          )}
        </p>
      )}
      {!snap && <p className="text-xs text-muted-foreground">Snap: {felt.snap.reason}</p>}
      {!pounding && <p className="text-xs text-muted-foreground">Pounding: {felt.pounding.reason}</p>}
      {snap && felt.snap.note && <p className="text-xs text-muted-foreground">{felt.snap.note}</p>}
      {felt.sea && <p data-testid="felt-sea" className="text-xs text-muted-foreground">Sea: {seaSummaryText(felt.sea, timeZone)}</p>}
      {felt.deg !== null && <p className="text-xs text-muted-foreground">{felt.bands.summary ?? felt.bands.reason}</p>}
      {comfortModel && <p data-testid="felt-comfort-model" className="text-xs text-muted-foreground">{comfortModel}</p>}
    </div>
  )
}

/** One line on the sea over the passage: the largest swell and wind wave and
 * when each was, a flat sea in words, or the server's reason there is no sea state. */
function seaSummaryText(sea: NonNullable<PassageFelt['sea']>, timeZone: string | null): string {
  if (sea.reason) return sea.reason
  const peak = (kind: string, p: NonNullable<NonNullable<PassageFelt['sea']>['swell']>) => {
    const when = formatLocalDateTime(p.at, timeZone)
    return `largest ${kind} ${p.height_m.toFixed(1)} m ${Math.round(p.period_s)} s${when === ABSENT ? '' : `, ${when}`}`
  }
  const text = joinSeaParts([sea.swell && peak('swell', sea.swell), sea.wind_wave && peak('wind wave', sea.wind_wave)], '; ')
  return sea.partial ? `${text} (${sea.partial})` : text
}

/** A bank that stayed within five points, down or up, is not worth a tile.
 * Without the readings the tile stays and says `--`. */
function batteryMoved(stats: PassageStats): boolean {
  const { soc_start_pct: start, soc_min_pct: low, soc_end_pct: end } = stats
  if (start === null || low === null) return true
  return Math.max(start, end ?? start) - low >= 5
}

// One row when it fits; otherwise a tile never sits alone: a last odd tile
// takes the whole row. Classes are spelled out so Tailwind sees them.
const WIDE_COLUMNS: Record<number, string> = {
  5: '@3xl:grid-cols-5',
  6: '@3xl:grid-cols-6',
}

export function PassageStatsStrip({ stats, units, onSeek }: { stats: PassageStats; units: DistanceUnits; onSeek?: (at: number) => void }) {
  const tiles: ReactNode[] = [
    <Kpi key="distance" label="Distance" value={fmtNM(stats.distance_nm)} />,
    <Kpi key="underway" label="Underway" value={fmtDuration(stats.underway_seconds)} />,
    <Kpi
      key="speed"
      label="Mean speed"
      value={fmtKn(stats.mean_sog_kts)}
      sub={<MaxButton label={`Max ${fmtKn(stats.max_sog_kts)}`} shown={fmtKn(stats.max_sog_kts)} what="speed" at={stats.max_sog_at} onSeek={stats.max_sog_kts === null ? undefined : onSeek} />}
    />,
    <Kpi key="fuel" label="Fuel burned" value={fmtVolume(stats.fuel_burned_l, units)} sub={fmtVolumePerNM(stats.fuel_burned_l, stats.distance_nm, units) ?? undefined} />,
    <Kpi
      key="wind"
      label="Mean wind"
      value={fmtKn(stats.tws_mean_kts)}
      sub={<MaxButton label={`Gust ${fmtKn(stats.max_gust_kts)}`} shown={fmtKn(stats.max_gust_kts)} what="gust" at={stats.max_gust_at} onSeek={stats.max_gust_kts === null ? undefined : onSeek} />}
    />,
  ]
  if (batteryMoved(stats)) {
    tiles.push(
      <Kpi
        key="battery"
        label="House battery"
        value={fmtPct(stats.soc_end_pct)}
        sub={`Start ${fmtPct(stats.soc_start_pct)} · Low ${fmtPct(stats.soc_min_pct)}`}
      />,
    )
  }
  return (
    <div className="@container">
      <div
        data-testid="passage-stats"
        data-tiles={tiles.length}
        className={cn('grid grid-cols-2 gap-2 @md:grid-cols-3 [&>*:last-child:nth-child(odd)]:col-span-2 @md:[&>*:last-child:nth-child(odd)]:col-span-1', WIDE_COLUMNS[tiles.length])}
      >
        {tiles}
      </div>
    </div>
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
  timeZone: string | null
  units: DistanceUnits
  canWrite: boolean
  onLabel: (rise: WaterRise, label: WaterRiseLabel | '') => void
}

const RISE_LABELS: WaterRiseLabel[] = ['Watermaker', 'Dock fill']

export function WaterRiseRow({ rise, timeZone, units, canWrite, onLabel }: WaterRiseRowProps) {
  const confirmed = rise.label as WaterRiseLabel | null
  const suggested = confirmed || rise.suggestion_status !== 'found' ? null : (rise.suggested_label as WaterRiseLabel | null)
  const checkFailed = !confirmed && rise.suggestion_status === 'failed'
  return (
    <li className="flex flex-wrap items-center gap-2 text-xs" data-testid="water-rise-row">
      <span className="min-w-0 flex-1 truncate tabular-nums text-foreground">
        {formatLocalDateTime(Date.parse(rise.start) / 1000, timeZone)} · +{fmtVolume(rise.litres, units)}
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
  timeZone: string | null
  units: DistanceUnits
  canWrite: boolean
  onLabelRise: (rise: WaterRise, label: WaterRiseLabel | '') => void
}

export function CruiseStatsPanel({ stats, units, timeZone, canWrite, onLabelRise }: CruiseStatsPanelProps) {
  const span = stats.departure && stats.arrival
    ? `${formatLocalDate(stats.departure, timeZone)} to ${formatLocalDate(stats.arrival, timeZone)}`
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
              <WaterRiseRow key={r.start} rise={r} timeZone={timeZone} units={units} canWrite={canWrite} onLabel={onLabelRise} />
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}
