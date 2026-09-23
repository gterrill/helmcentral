import { lazy, memo, Suspense, useEffect, useMemo, useRef, useState } from 'react'

import { Tile } from '@/components/ui/tile'
import { ChartUnavailableMessage } from '@/components/chart-tooltip'
import { WeatherConditionIcon } from '@/components/forecast/weather-condition-icon'
import { SEA_STATE_PLOT_INSET } from '@/lib/sea-state-geometry'
import { buildSeaStateSeries } from '@/lib/sea-state-series'
import type { WeatherForecastDay } from '@/hooks/use-weather-forecast'
import type { WaveForecastDay } from '@/hooks/use-wave-forecast'
import type { DistanceUnits } from '@/config/app-config'
import { fahrenheitToCelsius } from '@/lib/units'

export interface ForecastConditionsTileProps {
  forecast: WeatherForecastDay[]
  waveForecastDays: WaveForecastDay[]
  waveLoading: boolean
  waveError: string | null
  units: DistanceUnits
}

const CARD_COUNT = 5
// Tomorrow through five days out (forecast index 1..5): today itself is
// already the current-conditions tile's job, matching the split HelmCast's
// own main page already made between "now" and "the week" (ADR 0092). Both
// the card row and buildSeaStateSeries below are keyed off this same
// constant so the two halves of this tile can never drift onto different
// days from each other.
const START_DAY = 1

// Fallback box for the one frame before ResizeObserver reports a real size
// (or in an environment, such as jsdom, that never fires it at all).
// gridPixelHeight(WIDGET_CONSTRAINTS['forecast-conditions'].minH) is 272px
// (6 rows at GRID_ROW_HEIGHT=32, GRID_MARGIN=16 - dashboard-bento-grid.tsx);
// this box only covers the chart's own flex-1 slot below the card row, so
// the fallback height is that budget minus the Tile chrome (~40px, see
// TILE_CHROME_H in dashboard-bento-grid.tsx) and the card row above it
// (~96px: an mt-2, then each card's own padding, label, icon and temp
// line) - comfortably legible without ever claiming more height than this
// tile is allowed at its own minH. Not imported directly from
// dashboard-bento-grid.tsx to avoid pulling react-grid-layout into a
// presentational tile just for one constant; keep these numbers in sync by
// hand if that constraint ever changes.
const FALLBACK_WIDTH = 800
const FALLBACK_HEIGHT = 130

// recharts (~395 KB raw) is otherwise the only thing pulling dashboard-vendor
// into every route's startup bundle, including /kiosk pages that never
// render this tile at all. Same lazy-split pattern as
// assistant-markdown.tsx/help-markdown.tsx: only the chart itself is
// behind the boundary — the Tile chrome, the card row, the measured box and
// the "Loading wave data…" text below all stay eager, so the tile's own
// shape never jumps while the chart chunk loads.
const SeaStateChart = lazy(() =>
  import('@/components/forecast/sea-state-chart').then((m) => ({ default: m.SeaStateChart })),
)

/**
 * Measures its own content box with a ref + ResizeObserver, the same
 * technique useFitScale (lib/cluster-canvas.ts) uses to fit the engine
 * cluster canvas - except this tile needs both dimensions, not just a scale
 * factor, since the sea-state chart is drawn at the tile's actual pixel size
 * rather than a fixed design size scaled down.
 */
function useMeasuredBox() {
  const ref = useRef<HTMLDivElement>(null)
  const [box, setBox] = useState({ width: 0, height: 0 })

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const observer = new ResizeObserver(([entry]) => {
      setBox({ width: entry.contentRect.width, height: entry.contentRect.height })
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  return [ref, box] as const
}

/**
 * The wall display's forecast-conditions tile (ADR 0092, ADR 0125): five
 * day cards (condition, high/low) stacked above a wind-and-wave chart for
 * the same five days, replacing what used to be two separate tiles
 * (forecast-days, sea-state). Merged because they always showed the same
 * five days side by side on the wall anyway, and a shared tile is what lets
 * their day boundaries be made to align by construction instead of by
 * eyeballing two independently-sized widgets.
 *
 * ALIGNMENT: the card row is inset horizontally by SEA_STATE_PLOT_INSET
 * (the chart's own real left/right plot bounds - see that constant's doc
 * comment in lib/sea-state-geometry.ts for why it isn't simply the chart's
 * margin object) via inline paddingLeft/paddingRight, and uses a gapless
 * `grid-cols-5` with each cell's own `px-1` supplying the visual gap
 * instead of a `gap-*` on the grid itself - that's what puts the gap
 * between card 1 and card 2 at the exact pixel `xForDayBoundary(1, ...)`
 * the chart draws its first dashed day-boundary line at, for any tile
 * width, with no hand-tuned number to drift out of sync. The chart is
 * always given 5 day columns (buildSeaStateSeries' own dayCount, and
 * SeaStateChart's own dayCount prop, both default to 5) regardless of how
 * many real forecast days exist, so a short forecast still divides the
 * width into 5 columns rather than stretching fewer days across the whole
 * tile - the fewer-than-5-real-days case this tile and forecast-days-tile's
 * old dash-card behaviour both already handled on the card side.
 *
 * The chart itself keeps sea-state-tile.tsx's old measured-box technique: a
 * ref + ResizeObserver sized child, with the chart drawn in an
 * absolutely-positioned grandchild so its own rendered SVG size can never
 * feed back into the box the observer is watching (see useMeasuredBox's
 * doc comment). A wave-feed error shows the same unavailable message the
 * forecast drawer uses for its own wave chart, with no chart drawn
 * underneath it - a failed fetch is not the same thing as an empty series.
 *
 * Wrapped in memo() (item D's per-tick-render pattern - see
 * app-tile-memoization.test.tsx) and buildSeaStateSeries kept in a useMemo
 * keyed on its own inputs: App re-renders every gauge tick, and without
 * these two this tile would rebuild the full 5-day hourly series and
 * re-diff the sea-state chart on every one of those ticks even though
 * forecast/waveForecastDays themselves change far less often.
 */
export const ForecastConditionsTile = memo(function ForecastConditionsTile({
  forecast,
  waveForecastDays,
  waveLoading,
  waveError,
  units,
}: ForecastConditionsTileProps) {
  const [ref, box] = useMeasuredBox()
  const width = box.width > 0 ? box.width : FALLBACK_WIDTH
  const height = box.height > 0 ? box.height : FALLBACK_HEIGHT
  const waveUnit = units === 'imperial' ? 'ft' : 'm'
  const isImperial = units === 'imperial'
  const displayTemp = (tempF: number) => (isImperial ? tempF : fahrenheitToCelsius(tempF))

  const upcoming = forecast.slice(START_DAY, START_DAY + CARD_COUNT)
  const cards = Array.from({ length: CARD_COUNT }, (_, i) => upcoming[i] ?? null)

  const series = useMemo(
    () => buildSeaStateSeries(forecast, waveForecastDays, CARD_COUNT, START_DAY),
    [forecast, waveForecastDays],
  )

  return (
    <Tile title="Forecast Conditions">
      <div className="flex h-full min-h-0 flex-col">
        <div
          className="mt-2 grid grid-cols-5"
          style={{ paddingLeft: SEA_STATE_PLOT_INSET.left, paddingRight: SEA_STATE_PLOT_INSET.right }}
        >
          {cards.map((day, i) => {
            const hasTemps = day !== null && day.high !== -1 && day.low !== -1
            return (
              <div key={day?.dayKey ?? i} className="min-w-0 px-1">
                <div
                  data-testid="forecast-days-card"
                  className="flex min-w-0 flex-col items-center gap-1 rounded-md border border-border/60 px-1 py-2 text-center"
                >
                  <span className="text-[10px] uppercase tracking-[0.1em] text-muted-foreground">
                    {day ? day.dayName.slice(0, 3).toUpperCase() : '--'}
                  </span>
                  {day ? <WeatherConditionIcon condition={day.condition} size={22} /> : <span className="h-[22px]" />}
                  {hasTemps ? (
                    <div className="flex items-baseline gap-1">
                      <span className="font-display text-xl tabular-nums text-gauge-primary">
                        {Math.round(displayTemp(day.high))}°
                      </span>
                      <span className="text-[11px] text-muted-foreground">{Math.round(displayTemp(day.low))}°</span>
                    </div>
                  ) : (
                    <span className="font-display text-xl text-muted-foreground">--</span>
                  )}
                </div>
              </div>
            )
          })}
        </div>

        {/* Tile's CardContent isn't itself a flex column (tile.tsx), so this
            wrapper gives the measured box below a flex parent to take flex-1
            from - the same technique anchor-watch-tile.tsx uses for its map. */}
        <div ref={ref} className="relative mt-2 min-h-[120px] w-full lg:min-h-0 lg:flex-1">
          {waveError ? (
            <ChartUnavailableMessage testId="forecast-wave-error" message="Wave data unavailable" />
          ) : (
            <div className="absolute inset-0">
              {/* Fallback sized to fill the same absolutely-positioned slot
                  the chart renders into, so the tile's shape doesn't jump
                  while the recharts chunk loads. */}
              <Suspense fallback={<div className="h-full w-full" data-testid="sea-state-chart-loading" />}>
                <SeaStateChart series={series} width={width} height={height} waveUnit={waveUnit} />
              </Suspense>
            </div>
          )}
        </div>
        {waveLoading && waveForecastDays.length === 0 && !waveError && (
          <p className="mt-1 text-[11px] text-muted-foreground">Loading wave data…</p>
        )}
      </div>
    </Tile>
  )
})
