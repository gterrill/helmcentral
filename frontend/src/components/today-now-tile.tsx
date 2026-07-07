import { CloudSun } from 'lucide-react'
import type { WeatherToday } from '@/hooks/use-weather-today'
import type { DistanceUnits } from '@/config/app-config'
import { Tile } from '@/components/ui/tile'

export interface TodayNowTileProps {
  weather: WeatherToday
  distanceUnits: DistanceUnits
  onOpen?: () => void
}

function fahrenheitToCelsius(temp: number) {
  return (temp - 32) * (5 / 9)
}

export function TodayNowTile({ weather, distanceUnits, onOpen }: TodayNowTileProps) {
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
              {weather.high_temp_f >= 0 ? `↑${Math.round(distanceUnits === 'metric' ? fahrenheitToCelsius(weather.high_temp_f) : weather.high_temp_f)}°` : '↑—°'} {weather.low_temp_f >= 0 ? `↓${Math.round(distanceUnits === 'metric' ? fahrenheitToCelsius(weather.low_temp_f) : weather.low_temp_f)}°` : '↓—°'}
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
              Sea {weather.sea_temperature_f >= 0 ? `${Math.round(distanceUnits === 'metric' ? fahrenheitToCelsius(weather.sea_temperature_f) : weather.sea_temperature_f)}°` : '—°'}
            </p>
          </div>
        </div>

      </Tile>
    </div>
  )
}
