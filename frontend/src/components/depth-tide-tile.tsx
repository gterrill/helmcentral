import { ArrowDown, ArrowUp } from 'lucide-react'
import { memo } from 'react'
import type { DepthTrendData } from '@/hooks/use-depth-trend'
import type { TideToday } from '@/hooks/use-tide-today'
import { DepthSparkline } from '@/components/depth-sparkline'
import { Tile } from '@/components/ui/tile'
import { formatDataAge, isStale } from '@/lib/staleness'
import { estimateDepthAtNextTurn, tideExtremesByTime } from '@/lib/tide-estimate'

export interface DepthTideTileProps {
  depth: number | null
  isImperialDistance: boolean
  navigationState: string | null
  depthTrend: DepthTrendData
  tide: TideToday
  onOpen?: () => void
  /**
   * Seconds since the depth feed last reported, or null if the upstream
   * source publishes no age for it. Depth is the number you run aground on:
   * a dead transducer must not go on showing a confident reading forever.
   * `null` reads as "unknown," not "stale" (see isStale) — there is no
   * timestamp on environment.depth in the SignalK payload today, so this is
   * always null until a source actually publishes one.
   */
  lastUpdateAgeS: number | null
}

export const DepthTideTile = memo(function DepthTideTile({
  depth,
  isImperialDistance,
  navigationState,
  depthTrend,
  tide,
  onOpen,
  lastUpdateAgeS,
}: DepthTideTileProps) {
  const feedStale = isStale(lastUpdateAgeS)
  const depthValue =
    depth !== null
      ? isImperialDistance
        ? (depth * 3.28084).toFixed(1)
        : depth.toFixed(1)
      : '—'
  const depthUnitLabel = isImperialDistance ? 'feet' : 'm'
  const tideUnit = isImperialDistance ? 'ft' : 'm'
  const tideFtToDisplay = (ft: number) => (isImperialDistance ? ft : ft / 3.28084)
  const tideExtremes = tideExtremesByTime(tide)
  const isRising = tide.tide_direction === 'Rising'
  const estimatedExtremeDepth = estimateDepthAtNextTurn(depth, tide)
  const estimatedExtremeLabel = isRising ? 'Est. high' : 'Est. low'

  // A bare clickable div has no keyboard access and no focus ring — real
  // button semantics when there's something to open, an inert div otherwise,
  // so the non-interactive case (no onOpen) renders exactly as before.
  const Wrapper = onOpen ? 'button' : 'div'

  return (
    <Wrapper
      type={onOpen ? 'button' : undefined}
      onClick={onOpen}
      className={
        onOpen
          ? 'block w-full rounded-xl text-left transition-opacity hover:opacity-80 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2'
          : undefined
      }
    >
      <Tile title="Depth & Tide" stale={feedStale} staleLabel={formatDataAge(lastUpdateAgeS)}>
        <div className="mt-1 rounded-md border bg-background/60 px-3 py-3">
          <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Depth</p>
          <div className="mt-1 flex items-center gap-4">
            <div className="shrink-0">
              <p className="font-display text-4xl text-gauge-secondary">
                {depthValue}
                <span className="ml-2 align-baseline text-xl text-muted-foreground">{depth !== null ? depthUnitLabel : 'unavailable'}</span>
              </p>
              {estimatedExtremeDepth !== null && (
                <p className="mt-1 text-xs text-foreground">
                  <span className="text-muted-foreground">{estimatedExtremeLabel}</span>{' '}
                  <span className="font-semibold text-gauge-secondary">
                    {(isImperialDistance ? estimatedExtremeDepth * 3.28084 : estimatedExtremeDepth).toFixed(1)} {tideUnit}
                  </span>
                </p>
              )}
            </div>
            {(navigationState === 'anchored' || navigationState === 'moored') && (
              <DepthSparkline
                points={depthTrend.points}
                isImperial={isImperialDistance}
                since={depthTrend.since}
                tideType={depthTrend.tideType}
                tideDepthM={depthTrend.tideDepthM}
                className="min-w-0 flex-1"
              />
            )}
          </div>
        </div>
        <div className="mt-2 rounded-md border bg-background/60 px-3 py-2">
          <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
            Tide{tide.station_name ? ` — ${tide.station_name}` : ''}
          </p>
          <div className="mt-1 flex items-baseline gap-2">
            <span className="font-display text-4xl leading-none text-gauge-secondary">
              {tide.current_tide_height_ft >= 0 ? tideFtToDisplay(tide.current_tide_height_ft).toFixed(isImperialDistance ? 1 : 2) : '—'}
            </span>
            <span className="text-lg text-muted-foreground">{tideUnit}</span>
            <span className="ml-1 inline-flex items-center gap-1 text-xs font-semibold text-gauge-secondary">
              {tide.tide_direction === 'Falling' ? <ArrowDown className="h-3 w-3" /> : <ArrowUp className="h-3 w-3" />}
              {tide.tide_direction}
            </span>
          </div>
          {/* The High/Low pair fits on one line everywhere except md–lg, where the
              fixed sidebar leaves the two-column dashboard ~240px per tile and the
              second entry was running past the edge. Stacking them there keeps both
              readings whole rather than clipping a tide time. */}
          <div className="mt-2 flex flex-row items-center justify-between gap-x-2 md:flex-col md:items-start md:gap-y-1 lg:flex-row lg:items-center">
            {tideExtremes.map((extreme) => (
              <p key={extreme.isHigh ? 'high' : 'low'} className="inline-flex shrink-0 items-center gap-1.5 text-xs text-foreground">
                {extreme.isHigh ? (
                  <ArrowUp className="h-3.5 w-3.5 text-gauge-secondary" />
                ) : (
                  <ArrowDown className="h-3.5 w-3.5 text-amber-600" />
                )}
                {extreme.isHigh ? 'High' : 'Low'}
                {extreme.heightFt >= 0 && (
                  <span className="text-muted-foreground">({tideFtToDisplay(extreme.heightFt).toFixed(isImperialDistance ? 1 : 2)} {tideUnit})</span>
                )}
                {' '}{new Date(extreme.time).toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' })}
              </p>
            ))}
          </div>
        </div>
      </Tile>
    </Wrapper>
  )
})
