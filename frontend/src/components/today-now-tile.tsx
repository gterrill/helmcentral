import { CloudSun } from 'lucide-react'
import { memo, type ComponentProps, type ReactNode } from 'react'
import { formatDataAge } from '@/lib/staleness'
import { useWeatherToday, type WeatherToday } from '@/hooks/use-weather-today'
import { FORECAST_REFRESH_SECONDS, type DistanceUnits } from '@/config/app-config'
import { Tile } from '@/components/ui/tile'
import { fahrenheitToCelsius } from '@/lib/units'
import { useWaveForecast } from '@/hooks/use-wave-forecast'
import { useWeatherForecast } from '@/hooks/use-weather-forecast'
import { useBarometerTrend, type BarometerTrendData } from '@/hooks/use-barometer-trend'
import { BAROMETER_UNIT, BarometerSparkline } from '@/components/barometer-sparkline'
import { convertFromSI, formatQuantity } from '@/lib/quantities'

export interface TodayNowTileProps {
  weather: WeatherToday
  highTempF: number
  lowTempF: number
  seaTemperatureF: number | null
  distanceUnits: DistanceUnits
  /** The barometer row. Live data comes from LiveBarometerRow, which polls on its own. */
  barometer: ReactNode
  onOpen?: () => void
}

/** A change smaller than this reads as steady. */
const STEADY_BELOW_MB = 1

/**
 * How much history a change covers: "40m", "1h 30m", "3h". Minutes round
 * down to five, so the label never claims more history than there is, and
 * anything from 2h45m up reads as the full 3h.
 */
export function changeSpanLabel(seconds: number): string {
  if (seconds >= 165 * 60) return '3h'
  return formatDataAge(Math.floor(seconds / 60 / 5) * 5 * 60)
}

/**
 * The boat's barometer: the reading, its change over the last three hours and a sparkline.
 * With no barometer the row stays, reading "—" with no chart, so the tile
 * does not change height when a reading arrives and never shows a value the
 * boat did not measure. A failed request says "Barometer unavailable" in the
 * chart's place, so a broken feed isn't mistaken for a boat without one.
 */
export function BarometerRow({ barometer }: { barometer: BarometerTrendData }) {
  const current = formatQuantity(barometer.currentPa, 'pressure', BAROMETER_UNIT)
  const changeMb = barometer.changePa === null || barometer.changeSpanS === null ? null : convertFromSI(barometer.changePa, 'pressure', BAROMETER_UNIT)

  // The server sends the change and the span it covers together, so a change
  // always has a span.
  let changeText = '—'
  let changeClass = 'text-muted-foreground'
  if (changeMb !== null) {
    if (Math.abs(changeMb) < STEADY_BELOW_MB) {
      changeText = 'steady'
    } else {
      changeText = `${changeMb > 0 ? '▲ +' : '▼ −'}${Math.abs(changeMb).toFixed(1)} ${BAROMETER_UNIT}`
      changeClass = 'tabular-nums text-gauge-secondary'
    }
  }

  return (
    <div data-testid="today-barometer" className="mt-2 flex items-center gap-3 border-t border-border pt-2">
      <div className="shrink-0">
        <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Barometer</p>
        <p className="font-display text-xl leading-none tabular-nums text-gauge-secondary">
          <span>{current ?? '—'}</span>
          {current !== null && <span className="ml-1 text-xs font-semibold text-muted-foreground">{BAROMETER_UNIT}</span>}
        </p>
      </div>
      <div className="min-w-0 flex-1">
        {barometer.failed ? (
          <p data-testid="barometer-error" className="text-xs text-muted-foreground">Barometer unavailable</p>
        ) : (
          <BarometerSparkline points={barometer.points} />
        )}
      </div>
      <div data-testid="barometer-change" className="w-16 shrink-0 text-right">
        <p className={`text-xs font-medium ${changeClass}`}>{changeText}</p>
        {barometer.changeSpanS !== null && changeMb !== null && (
          <p className="text-[10px] tracking-[0.16em] text-muted-foreground">{changeSpanLabel(barometer.changeSpanS)}</p>
        )}
      </div>
    </div>
  )
}

export const TodayNowTile = memo(function TodayNowTile({ weather, highTempF, lowTempF, seaTemperatureF, distanceUnits, barometer, onOpen }: TodayNowTileProps) {
  return (
    <div onClick={onOpen} className={onOpen ? 'cursor-pointer transition-opacity hover:opacity-80' : undefined}>
      <Tile title="Today & Now">
        <div className="mt-2 grid grid-cols-[auto_1fr_auto] items-center gap-3">
          <div className="grid h-12 w-12 place-items-center rounded-full bg-amber-100 text-amber-600">
            <CloudSun className="h-7 w-7" />
          </div>
          <div>
            <div className="flex items-end gap-1">
              <p className="font-display text-5xl leading-none text-amber-700">
                {weather.temperature_f >= 0 ? Math.round(distanceUnits === 'metric' ? fahrenheitToCelsius(weather.temperature_f) : weather.temperature_f) : '—'}
              </p>
              <p className="pb-1 text-xl font-semibold text-amber-700">{distanceUnits === 'metric' ? '°C' : '°F'}</p>
            </div>
            <p className="text-sm font-semibold uppercase tracking-[0.1em] text-foreground">
              {weather.condition}
            </p>
            <p className="text-xs text-muted-foreground">
              {highTempF >= 0 ? `↑${Math.round(distanceUnits === 'metric' ? fahrenheitToCelsius(highTempF) : highTempF)}°` : '↑—°'} {lowTempF >= 0 ? `↓${Math.round(distanceUnits === 'metric' ? fahrenheitToCelsius(lowTempF) : lowTempF)}°` : '↓—°'}
            </p>
          </div>
          <div className="text-right">
            <p className="font-display text-xl leading-none text-gauge-secondary">
              {weather.wind_speed_kts >= 0 ? `${Math.round(weather.wind_speed_kts)} kts` : '— kts'} {weather.wind_direction}
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              Gust {weather.wind_gust_kts >= 0 ? `${Math.round(weather.wind_gust_kts)} kts` : '— kts'}
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              {weather.precipitation_pct >= 0 ? `${Math.round(weather.precipitation_pct)}% precip` : '—% precip'}
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              Sea {seaTemperatureF !== null ? `${Math.round(distanceUnits === 'metric' ? fahrenheitToCelsius(seaTemperatureF) : seaTemperatureF)}°` : '—°'}
            </p>
          </div>
        </div>

        {barometer}
      </Tile>
    </div>
  )
})

/** Polls the barometer itself, so a poll re-renders this row and not the weather above it. */
const LiveBarometerRow = memo(function LiveBarometerRow() {
  return <BarometerRow barometer={useBarometerTrend()} />
})

const LIVE_BAROMETER = <LiveBarometerRow />

/** Today & Now: the weather now, today's high and low from the forecast, the sea temperature from the wave forecast, and the boat's barometer. */
export const LiveTodayNowTile = memo(function LiveTodayNowTile(props: Pick<ComponentProps<typeof TodayNowTile>, 'distanceUnits' | 'onOpen'>) {
  const { weather } = useWeatherToday(FORECAST_REFRESH_SECONDS)
  const { forecast } = useWeatherForecast(FORECAST_REFRESH_SECONDS)
  const { seaTemperatureF } = useWaveForecast(FORECAST_REFRESH_SECONDS)
  return (
    <TodayNowTile
      {...props}
      barometer={LIVE_BAROMETER}
      weather={weather}
      highTempF={forecast[0]?.high ?? -1}
      lowTempF={forecast[0]?.low ?? -1}
      seaTemperatureF={seaTemperatureF ?? null}
    />
  )
})
