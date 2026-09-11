import { memo } from 'react'

import { Tile } from '@/components/ui/tile'
import { BulletGauge } from '@/components/ui/bullet-gauge'
import type { WeatherToday } from '@/hooks/use-weather-today'
import type { WeatherForecastDay } from '@/hooks/use-weather-forecast'
import type { GustWindow } from '@/lib/gust-windows'
import type { DistanceUnits } from '@/config/app-config'
import { next24hWindBand, todayTempBand, nextRain } from '@/lib/forecast-bands'
import { fahrenheitToCelsius } from '@/lib/units'
import { formatDataAge, isStale } from '@/lib/staleness'

export interface CurrentConditionsTileProps {
  depth: number | null
  /** Seconds since the depth feed last reported; null reads as unknown, not stale (see isStale). */
  depthLastUpdateAgeS: number | null
  windSpeedApparentKts: number | null
  maxGustKts: Record<GustWindow, number | null>
  weather: WeatherToday
  forecast: WeatherForecastDay[]
  distanceUnits: DistanceUnits
}

const METRES_TO_FEET = 3.28084

function niceMax(...values: (number | null | undefined)[]): number {
  const usable = values.filter((v): v is number => typeof v === 'number' && Number.isFinite(v))
  const max = usable.length > 0 ? Math.max(...usable) : 10
  return Math.max(10, Math.ceil((max + 5) / 5) * 5)
}

/**
 * Depth, wind and temperature at a glance for the wall display (ADR 0092):
 * three readouts, each with a bullet gauge showing today's forecast range
 * where one applies, plus a line naming the next rain the forecast expects.
 * Every field is a structural dash when its source has nothing to say - a
 * missing forecast hour is never read as calm, and a feed with no
 * precipitation data at all never gets coerced into "no rain expected".
 */
export const CurrentConditionsTile = memo(function CurrentConditionsTile({
  depth,
  depthLastUpdateAgeS,
  windSpeedApparentKts,
  maxGustKts,
  weather,
  forecast,
  distanceUnits,
}: CurrentConditionsTileProps) {
  const isImperial = distanceUnits === 'imperial'
  const depthStale = isStale(depthLastUpdateAgeS)
  const depthDisplay = depth !== null ? (isImperial ? depth * METRES_TO_FEET : depth).toFixed(1) : '—'
  const depthUnit = isImperial ? 'ft' : 'm'

  const nowHour = new Date().getHours()
  const windBand = next24hWindBand(forecast, nowHour)
  const tempBandF = todayTempBand(forecast[0])
  const rain = nextRain(forecast, nowHour)

  const obsGust1h = maxGustKts['1h']
  const windMax = niceMax(windBand?.max, windBand?.gustMax, obsGust1h, windSpeedApparentKts)
  const windMarkers = [
    ...(windBand ? [{ value: windBand.gustMax, label: 'fcst gust' }] : []),
    ...(obsGust1h !== null ? [{ value: obsGust1h, label: 'obs' }] : []),
  ]

  const displayTemp = (tempF: number) => (isImperial ? tempF : fahrenheitToCelsius(tempF))
  const tempUnit = isImperial ? '°F' : '°C'
  const tempValue = weather.temperature_f >= 0 ? Math.round(displayTemp(weather.temperature_f)) : null
  const tempBand = tempBandF ? { low: displayTemp(tempBandF.low), high: displayTemp(tempBandF.high) } : null
  const tempMin = tempBand ? Math.floor(tempBand.low) - 5 : 0
  const tempMax = tempBand ? Math.ceil(tempBand.high) + 5 : 40

  const rainLine =
    rain === null
      ? '—'
      : rain === 'none'
        ? 'No rain expected next 24 h'
        : rain.isNow
          ? `Rain likely now (${Math.round(rain.chancePct)}%)`
          : `Rain likely from ${rain.label} (${Math.round(rain.chancePct)}%)`

  return (
    <Tile title="Current Conditions" stale={depthStale} staleLabel={formatDataAge(depthLastUpdateAgeS)}>
      <div className="mt-2 grid grid-cols-3 gap-3">
        <div className="flex min-w-0 flex-col gap-1">
          <span className="text-[10px] uppercase tracking-[0.1em] text-muted-foreground">Depth</span>
          <span className="font-display text-4xl leading-none tabular-nums text-gauge-secondary">
            {depthDisplay}
            <span className="ml-1 text-[11px] text-muted-foreground">{depthUnit}</span>
          </span>
        </div>

        <div className="flex min-w-0 flex-col gap-1">
          <span className="text-[10px] uppercase tracking-[0.1em] text-muted-foreground">Wind</span>
          <span className="font-display text-4xl leading-none tabular-nums text-gauge-secondary">
            {windSpeedApparentKts !== null ? Math.round(windSpeedApparentKts) : '—'}
            <span className="ml-1 text-[11px] text-muted-foreground">kts</span>
          </span>
          <BulletGauge
            value={windSpeedApparentKts}
            min={0}
            max={windMax}
            bandLow={windBand?.min ?? null}
            bandHigh={windBand?.max ?? null}
            markers={windMarkers}
            unit="kt"
          />
        </div>

        <div className="flex min-w-0 flex-col gap-1">
          <span className="text-[10px] uppercase tracking-[0.1em] text-muted-foreground">Temp</span>
          <span className="font-display text-4xl leading-none tabular-nums text-gauge-secondary">
            {tempValue !== null ? tempValue : '—'}
            <span className="ml-1 text-[11px] text-muted-foreground">{tempUnit}</span>
          </span>
          <BulletGauge
            value={tempValue}
            min={tempMin}
            max={tempMax}
            bandLow={tempBand?.low ?? null}
            bandHigh={tempBand?.high ?? null}
            unit={tempUnit}
          />
        </div>
      </div>

      <p className="mt-3 truncate text-[11px] text-muted-foreground">{rainLine}</p>
    </Tile>
  )
})
