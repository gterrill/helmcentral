import { describe, it, expect } from 'vitest'

import { buildSeaStateSeries } from '@/lib/sea-state-series'
import type { WeatherForecastDay, WeatherHourlyWindPoint } from '@/hooks/use-weather-forecast'
import type { WaveForecastDay, WaveHourlyPoint } from '@/hooks/use-wave-forecast'

function windPoint(hourOfDay: number, overrides: Partial<WeatherHourlyWindPoint> = {}): WeatherHourlyWindPoint {
  return {
    label: `${hourOfDay}:00`,
    hourOfDay,
    windSpeed: 10 + hourOfDay,
    windGust: 15 + hourOfDay,
    windDirection: 'N',
    windDirectionDeg: hourOfDay * 10,
    ...overrides,
  }
}

function wavePoint(hourOfDay: number, overrides: Partial<WaveHourlyPoint> = {}): WaveHourlyPoint {
  return {
    label: `${hourOfDay}:00`,
    hourOfDay,
    waveHeightM: 1 + hourOfDay * 0.1,
    wavePeriodS: 6,
    waveDirectionDeg: hourOfDay * 5,
    windWaveHeightM: 0.5,
    swellWaveHeightM: 0.5,
    windWaveDirectionDeg: 0,
    windWavePeriodS: 5,
    swellWaveDirectionDeg: 0,
    swellWavePeriodS: 8,
    steepnessRatio: 0.02,
    steepnessBand: 'rolling',
    ...overrides,
  }
}

function weatherDay(dayKey: string, hourlyWind: WeatherHourlyWindPoint[]): WeatherForecastDay {
  return {
    dayKey,
    date: dayKey,
    dayName: 'Thursday',
    condition: 'Clear',
    high: 75,
    low: 60,
    windSpeed: 10,
    windGust: 15,
    windDirection: 'N',
    windSummary: null,
    precipitationSummary: null,
    precipitation: null,
    humidityPct: null,
    visibilityNm: null,
    sunriseTime: null,
    sunsetTime: null,
    moonPhase: null,
    hourlyWind,
    hourlyPrecip: [],
    hourlyUV: [],
    hourlyCloud: [],
  }
}

function waveDay(dayKey: string, hourlyWave: WaveHourlyPoint[]): WaveForecastDay {
  return {
    dayKey,
    date: dayKey,
    dayName: 'Thursday',
    waveSummary: null,
    hourlyWave,
    indicators: { waveFront: false, rapidBuild: false, periodStep: false, crossSea: false },
  }
}

function fullDayWind(dayKey: string): WeatherForecastDay {
  return weatherDay(dayKey, Array.from({ length: 24 }, (_, h) => windPoint(h)))
}

function fullDayWave(dayKey: string): WaveForecastDay {
  return waveDay(dayKey, Array.from({ length: 24 }, (_, h) => wavePoint(h)))
}

describe('buildSeaStateSeries', () => {
  it('produces 120 points for 5 full days of matching wind+wave data with correct progression', () => {
    const forecastDays = Array.from({ length: 5 }, (_, d) => fullDayWind(`d${d}`))
    const waveDays = Array.from({ length: 5 }, (_, d) => fullDayWave(`d${d}`))

    const series = buildSeaStateSeries(forecastDays, waveDays, 5)
    expect(series).toHaveLength(120)

    // Spot-check progression and values at a few points.
    expect(series[0]).toEqual({
      index: 0,
      dayKey: 'd0',
      hourOfDay: 0,
      windKts: 10,
      gustKts: 15,
      windDirDeg: 0,
      waveM: 1,
      waveDirDeg: 0,
      steepnessBand: 'rolling',
    })

    // Absolute index 24 is day 1, hour 0.
    expect(series[24]).toEqual({
      index: 24,
      dayKey: 'd1',
      hourOfDay: 0,
      windKts: 10,
      gustKts: 15,
      windDirDeg: 0,
      waveM: 1,
      waveDirDeg: 0,
      steepnessBand: 'rolling',
    })

    // Absolute index 119 is day 4, hour 23.
    const last = series[119]
    expect(last.index).toBe(119)
    expect(last.dayKey).toBe('d4')
    expect(last.hourOfDay).toBe(23)
    expect(last.windKts).toBe(33)
    expect(last.gustKts).toBe(38)
    expect(last.windDirDeg).toBe(230)
    expect(last.waveM).toBeCloseTo(3.3, 10)
    expect(last.waveDirDeg).toBe(115)
    expect(last.steepnessBand).toBe('rolling')
  })

  it('emits 24 points with populated wind and all-null wave fields when a day has no matching waveDays entry', () => {
    const forecastDays = [fullDayWind('d0')]
    const waveDays: WaveForecastDay[] = [] // no matching dayKey at all

    const series = buildSeaStateSeries(forecastDays, waveDays, 1)
    expect(series).toHaveLength(24)
    for (const point of series) {
      expect(point.windKts).not.toBeNull()
      expect(point.gustKts).not.toBeNull()
      expect(point.windDirDeg).not.toBeNull()
      expect(point.waveM).toBe(null)
      expect(point.waveDirDeg).toBe(null)
      expect(point.steepnessBand).toBe(null)
    }
  })

  it('produces null wind fields for just the missing hour when hourlyWind is sparse', () => {
    // Hour 5 is missing entirely from the array.
    const sparseWind = Array.from({ length: 24 }, (_, h) => h).filter((h) => h !== 5).map((h) => windPoint(h))
    const forecastDays = [weatherDay('d0', sparseWind)]
    const waveDays = [fullDayWave('d0')]

    const series = buildSeaStateSeries(forecastDays, waveDays, 1)
    expect(series).toHaveLength(24)

    const missingHourPoint = series.find((p) => p.hourOfDay === 5)!
    expect(missingHourPoint.windKts).toBe(null)
    expect(missingHourPoint.gustKts).toBe(null)
    expect(missingHourPoint.windDirDeg).toBe(null)
    // Wave data for that same hour is still present - independent feeds.
    expect(missingHourPoint.waveM).not.toBeNull()

    // Every other hour still has wind data.
    const presentHourPoint = series.find((p) => p.hourOfDay === 6)!
    expect(presentHourPoint.windKts).not.toBeNull()
  })

  it('turns -1 sentinels on wind and wave hourly fields into null, never 0', () => {
    const forecastDays = [
      weatherDay('d0', [windPoint(0, { windSpeed: -1, windGust: -1, windDirectionDeg: -1 })]),
    ]
    const waveDays = [
      waveDay('d0', [wavePoint(0, { waveHeightM: -1, waveDirectionDeg: -1 })]),
    ]

    const series = buildSeaStateSeries(forecastDays, waveDays, 1)
    const point = series.find((p) => p.hourOfDay === 0)!

    expect(point.windKts).toBe(null)
    expect(point.gustKts).toBe(null)
    expect(point.windDirDeg).toBe(null)
    expect(point.waveM).toBe(null)
    expect(point.waveDirDeg).toBe(null)
  })

  it('only uses dayCount days when forecastDays has more than dayCount', () => {
    const forecastDays = Array.from({ length: 5 }, (_, d) => fullDayWind(`d${d}`))
    const waveDays = Array.from({ length: 5 }, (_, d) => fullDayWave(`d${d}`))

    const series = buildSeaStateSeries(forecastDays, waveDays, 2)
    expect(series).toHaveLength(48)
    expect(series[47].dayKey).toBe('d1')
  })

  it('stops at the number of real days available when forecastDays is shorter than dayCount, rather than padding fabricated days', () => {
    // Only 2 real forecast days exist, but dayCount asks for 5. A day that
    // doesn't exist yet (no dayKey, no data at all) is not the same thing as
    // a day whose feed is dead - padding it in would fabricate a day's worth
    // of structure, so the series simply stops at what's real.
    const forecastDays = [fullDayWind('d0'), fullDayWind('d1')]
    const waveDays = [fullDayWave('d0'), fullDayWave('d1')]

    const series = buildSeaStateSeries(forecastDays, waveDays, 5)
    expect(series).toHaveLength(48)
    expect(series[47].dayKey).toBe('d1')
  })
})
