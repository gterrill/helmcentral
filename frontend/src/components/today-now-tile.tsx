import { CloudSun } from 'lucide-react'
import { memo } from 'react'
import type { WeatherToday } from '@/hooks/use-weather-today'
import type { DistanceUnits } from '@/config/app-config'
import { Tile } from '@/components/ui/tile'

export interface TodayNowTileProps {
  weather: WeatherToday
  highTempF: number
  lowTempF: number
  seaTemperatureF: number | null
  distanceUnits: DistanceUnits
  onOpen?: () => void
}

function fahrenheitToCelsius(temp: number) {
  return (temp - 32) * (5 / 9)
}

export const TodayNowTile = memo(function TodayNowTile({ weather, highTempF, lowTempF, seaTemperatureF, distanceUnits, onOpen }: TodayNowTileProps) {
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

      </Tile>
    </div>
  )
})
