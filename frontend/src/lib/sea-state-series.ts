import type { WeatherForecastDay } from '@/hooks/use-weather-forecast'
import type { WaveForecastDay, WaveSteepnessBand } from '@/hooks/use-wave-forecast'

export interface SeaStatePoint {
  /** 0..(dayCount*24 - 1) for a 5-day, 24h/day series. */
  index: number
  dayKey: string
  hourOfDay: number
  windKts: number | null
  gustKts: number | null
  windDirDeg: number | null
  waveM: number | null
  waveDirDeg: number | null
  steepnessBand: WaveSteepnessBand | null
}

const HOURS_PER_DAY = 24
const SENTINEL = -1

function sentinelOrNull(value: number | undefined): number | null {
  return value === undefined || value === SENTINEL ? null : value
}

/**
 * A pure join of the weather and wave forecast hooks into one hourly series
 * for the sea-state chart. Every day in range always emits exactly 24
 * points, even when a feed is entirely dead for that day (no matching
 * waveDays entry by dayKey, or a sparse/missing hourlyWind array) - a dead
 * feed must read as null fields, never as a shortened series or a
 * fabricated calm reading.
 *
 * When `forecastDays` itself has fewer than `dayCount` entries, the series
 * stops at however many real days exist rather than padding the remainder
 * with fabricated all-null days: a day that hasn't arrived from the
 * forecast yet is a different thing from a day whose feed went dead, and
 * only the latter gets the null-fields treatment above.
 */
export function buildSeaStateSeries(
  forecastDays: WeatherForecastDay[],
  waveDays: WaveForecastDay[],
  dayCount = 5,
): SeaStatePoint[] {
  const days = forecastDays.slice(0, dayCount)
  const points: SeaStatePoint[] = []

  days.forEach((day, dayIndex) => {
    const matchedWaveDay = waveDays.find((w) => w.dayKey === day.dayKey)

    for (let hourOfDay = 0; hourOfDay < HOURS_PER_DAY; hourOfDay++) {
      const windPoint = day.hourlyWind.find((p) => p.hourOfDay === hourOfDay)
      const wavePoint = matchedWaveDay?.hourlyWave.find((p) => p.hourOfDay === hourOfDay)

      points.push({
        index: dayIndex * HOURS_PER_DAY + hourOfDay,
        dayKey: day.dayKey,
        hourOfDay,
        windKts: sentinelOrNull(windPoint?.windSpeed),
        gustKts: sentinelOrNull(windPoint?.windGust),
        windDirDeg: sentinelOrNull(windPoint?.windDirectionDeg),
        waveM: sentinelOrNull(wavePoint?.waveHeightM),
        waveDirDeg: sentinelOrNull(wavePoint?.waveDirectionDeg),
        steepnessBand: wavePoint?.steepnessBand ?? null,
      })
    }
  })

  return points
}
