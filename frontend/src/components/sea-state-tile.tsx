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
// enough that the chart is legible rather than collapsed to nothing.
const FALLBACK_WIDTH = 800
const FALLBACK_HEIGHT = 260

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
      <div ref={ref} className="mt-2 h-full min-h-[160px] w-full">
        {waveError ? (
          <ChartUnavailableMessage testId="forecast-wave-error" message="Wave data unavailable" />
        ) : (
          <SeaStateChart series={series} width={width} height={height} waveUnit={waveUnit} />
        )}
      </div>
      {waveLoading && waveForecastDays.length === 0 && !waveError && (
        <p className="mt-1 text-[11px] text-muted-foreground">Loading wave data…</p>
      )}
    </Tile>
  )
}
