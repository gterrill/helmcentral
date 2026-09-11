import { useEffect, useRef, useState } from 'react'

import { Tile } from '@/components/ui/tile'
import { ChartUnavailableMessage } from '@/components/chart-tooltip'
import { SeaStateChart } from '@/components/forecast/sea-state-chart'
import { buildSeaStateSeries } from '@/lib/sea-state-series'
import type { WeatherForecastDay } from '@/hooks/use-weather-forecast'
import type { WaveForecastDay } from '@/hooks/use-wave-forecast'
import type { DistanceUnits } from '@/config/app-config'

export interface SeaStateTileProps {
  forecast: WeatherForecastDay[]
  waveForecastDays: WaveForecastDay[]
  waveLoading: boolean
  waveError: string | null
  units: DistanceUnits
}

// Fallback box for the one frame before ResizeObserver reports a real size
// (or in an environment, such as jsdom, that never fires it at all) - large
// enough that the chart is legible rather than collapsed to nothing, and no
// taller than gridPixelHeight(WIDGET_CONSTRAINTS['sea-state'].minH) (224px:
// 5 rows at GRID_ROW_HEIGHT=32, GRID_MARGIN=16 - dashboard-bento-grid.tsx),
// so a test or a browser that never fires the observer doesn't render a
// chart taller than this tile is ever allowed to be. Not imported directly
// from dashboard-bento-grid.tsx to avoid pulling react-grid-layout into a
// presentational tile just for one constant; keep the two numbers in sync
// by hand if that constraint ever changes.
const FALLBACK_WIDTH = 800
const FALLBACK_HEIGHT = 220

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
 * The wall display's sea-state tile (ADR 0092): five days of wind and wave
 * in one chart, sized to whatever the grid actually gave this widget rather
 * than a fixed design width. A wave-feed error shows the same unavailable
 * message the forecast drawer uses for its own wave chart, with no chart
 * drawn underneath it - a failed fetch is not the same thing as an empty
 * series, and showing a flat empty chart in its place would read as "no
 * waves today" rather than "the wave feed failed".
 */
export function SeaStateTile({ forecast, waveForecastDays, waveLoading, waveError, units }: SeaStateTileProps) {
  const [ref, box] = useMeasuredBox()
  const width = box.width > 0 ? box.width : FALLBACK_WIDTH
  const height = box.height > 0 ? box.height : FALLBACK_HEIGHT
  const waveUnit = units === 'imperial' ? 'ft' : 'm'

  const series = buildSeaStateSeries(forecast, waveForecastDays)

  return (
    <Tile title="Sea State">
      {/* Tile's CardContent isn't itself a flex column (tile.tsx), so this
          wrapper gives the measured box below a flex parent to take flex-1
          from - the same technique anchor-watch-tile.tsx uses for its map. */}
      <div className="flex h-full min-h-0 flex-col">
        {/* relative + lg:flex-1/lg:min-h-0: at lg+ the dashboard grid hands
            this tile a fixed height (RGL wraps widgets in h-full), so this
            box takes its height from that flex parent. Below lg the grid
            only gives a floor (dashboard-bento-grid.tsx), so min-h-[160px]
            is the height there instead. Either way the box's height comes
            from the grid, never from its own content - the chart is rendered
            in an absolutely-positioned child below, so the SVG's rendered
            size has no box left to grow. Without this, in a real browser
            (which, unlike jsdom, actually applies layout) the div's height
            would be its content height, i.e. the chart's height, which is
            set from the measured height: every render grows the box and the
            observer fires again. */}
        <div ref={ref} className="relative mt-2 min-h-[160px] w-full lg:min-h-0 lg:flex-1">
          {waveError ? (
            <ChartUnavailableMessage testId="forecast-wave-error" message="Wave data unavailable" />
          ) : (
            <div className="absolute inset-0">
              <SeaStateChart series={series} width={width} height={height} waveUnit={waveUnit} />
            </div>
          )}
        </div>
        {waveLoading && waveForecastDays.length === 0 && !waveError && (
          <p className="mt-1 text-[11px] text-muted-foreground">Loading wave data…</p>
        )}
      </div>
    </Tile>
  )
}
