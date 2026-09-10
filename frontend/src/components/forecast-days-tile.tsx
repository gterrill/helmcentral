import { memo } from 'react'

import { Tile } from '@/components/ui/tile'
import { WeatherConditionIcon } from '@/components/forecast/weather-condition-icon'
import type { WeatherForecastDay } from '@/hooks/use-weather-forecast'
import type { DistanceUnits } from '@/config/app-config'
import { fahrenheitToCelsius } from '@/lib/units'

export interface ForecastDaysTileProps {
  days: WeatherForecastDay[]
  units: DistanceUnits
}

const CARD_COUNT = 5

/**
 * Five days ahead for the wall display (ADR 0092): tomorrow through five
 * days out, since today itself is already covered by the current-conditions
 * tile - matching HelmCast's own split between "now" and "the week". Always
 * renders exactly five card slots; a day the forecast hasn't supplied yet
 * (or hasn't reported a high/low for) shows a dash card rather than
 * collapsing the row, so a short forecast never reads as a narrower tile
 * than it is.
 */
export const ForecastDaysTile = memo(function ForecastDaysTile({ days, units }: ForecastDaysTileProps) {
  const upcoming = days.slice(1, 1 + CARD_COUNT)
  const isImperial = units === 'imperial'
  const displayTemp = (tempF: number) => (isImperial ? tempF : fahrenheitToCelsius(tempF))

  const cards = Array.from({ length: CARD_COUNT }, (_, i) => upcoming[i] ?? null)

  return (
    <Tile title="Forecast">
      <div className="mt-2 grid grid-cols-5 gap-2">
        {cards.map((day, i) => {
          const hasTemps = day !== null && day.high !== -1 && day.low !== -1
          return (
            <div
              key={day?.dayKey ?? i}
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
          )
        })}
      </div>
    </Tile>
  )
})
