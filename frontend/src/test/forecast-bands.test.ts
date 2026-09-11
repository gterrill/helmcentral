import { describe, it, expect } from 'vitest'

import { next24hWindBand, todayTempBand, nextRain } from '@/lib/forecast-bands'
import type {
  WeatherForecastDay,
  WeatherHourlyWindPoint,
  WeatherHourlyPrecipPoint,
} from '@/hooks/use-weather-forecast'

function windPoint(hourOfDay: number, windSpeed: number, windGust: number, label?: string): WeatherHourlyWindPoint {
  return {
    label: label ?? `${hourOfDay}:00`,
    hourOfDay,
    windSpeed,
    windGust,
    windDirection: 'N',
    windDirectionDeg: 0,
  }
}

function precipPoint(hourOfDay: number, precipChancePct: number | null, label?: string): WeatherHourlyPrecipPoint {
  return {
    label: label ?? `${hourOfDay}:00`,
    hourOfDay,
    precipChancePct,
    precipIntensityMm: 0,
  }
}

function day(overrides: Partial<WeatherForecastDay> = {}): WeatherForecastDay {
  return {
    dayKey: '2026-01-01',
    date: 'Jan 1',
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
    hourlyWind: [],
    hourlyPrecip: [],
    hourlyUV: [],
    hourlyCloud: [],
    ...overrides,
  }
}

describe('next24hWindBand', () => {
  it('spans two days from nowHour, excluding nothing when all speeds are valid', () => {
    const day0Wind = Array.from({ length: 24 }, (_, h) => windPoint(h, 10 + (h % 5), 15 + (h % 5)))
    const day1Wind = Array.from({ length: 24 }, (_, h) => windPoint(h, 5 + (h % 3), 8 + (h % 3)))
    const days = [
      day({ dayKey: 'd0', hourlyWind: day0Wind }),
      day({ dayKey: 'd1', hourlyWind: day1Wind }),
    ]
    // nowHour 14 -> hours 14-23 of day0 (10 points) plus hours 0-13 of day1 (14 points)
    const result = next24hWindBand(days, 14)
    expect(result).not.toBeNull()

    const included = [
      ...day0Wind.slice(14),
      ...day1Wind.slice(0, 14),
    ]
    const expectedMin = Math.min(...included.map((p) => p.windSpeed))
    const expectedMax = Math.max(...included.map((p) => p.windSpeed))
    const expectedGustMax = Math.max(...included.map((p) => p.windGust))
    expect(result).toEqual({ min: expectedMin, max: expectedMax, gustMax: expectedGustMax })
  })

  it('returns null when every point in the window is the -1 sentinel', () => {
    const day0Wind = Array.from({ length: 24 }, (_, h) => windPoint(h, -1, -1))
    const days = [day({ dayKey: 'd0', hourlyWind: day0Wind })]
    expect(next24hWindBand(days, 0)).toBeNull()
  })

  it('excludes only the -1 hours when speeds are a mix of valid and sentinel', () => {
    const day0Wind = [
      windPoint(0, -1, -1),
      windPoint(1, 12, 18),
      windPoint(2, -1, -1),
      windPoint(3, 8, 14),
    ]
    // Use a narrow window: nowHour 0, but supply only 4 hours in day0 and none in day1.
    const days = [day({ dayKey: 'd0', hourlyWind: day0Wind })]
    const result = next24hWindBand(days, 0)
    expect(result).toEqual({ min: 8, max: 12, gustMax: 18 })
  })

  it('excludes a -1 gust on an hour where speed is otherwise valid, without dropping that hour\'s speed', () => {
    const day0Wind = [
      windPoint(0, 10, -1), // valid speed, absent gust
      windPoint(1, 20, 25), // valid speed, valid gust
    ]
    const days = [day({ dayKey: 'd0', hourlyWind: day0Wind })]
    const result = next24hWindBand(days, 0)
    expect(result).toEqual({ min: 10, max: 20, gustMax: 25 })
  })

  it('returns null for an empty forecastDays array', () => {
    expect(next24hWindBand([], 5)).toBeNull()
  })

  it('draws almost the whole window from day 1 when nowHour is 23', () => {
    const day0Wind = [windPoint(23, 100, 100)] // only hour 23 populated, would be the single day0 point used
    const day1Wind = Array.from({ length: 24 }, (_, h) => windPoint(h, 1, 2))
    const days = [
      day({ dayKey: 'd0', hourlyWind: day0Wind }),
      day({ dayKey: 'd1', hourlyWind: day1Wind }),
    ]
    const result = next24hWindBand(days, 23)
    // Included: day0 hour 23 (speed 100) + day1 hours 0-22 (speed 1) - 23 points from day1.
    expect(result).toEqual({ min: 1, max: 100, gustMax: 100 })
  })

  it('still returns a value using only day 0 when forecastDays[1] is missing entirely', () => {
    const day0Wind = Array.from({ length: 24 }, (_, h) => windPoint(h, h, h + 1))
    const days = [day({ dayKey: 'd0', hourlyWind: day0Wind })]
    const result = next24hWindBand(days, 14)
    // Only hours 14-23 of day0 are available (10 points).
    const included = day0Wind.slice(14)
    expect(result).toEqual({
      min: Math.min(...included.map((p) => p.windSpeed)),
      max: Math.max(...included.map((p) => p.windSpeed)),
      gustMax: Math.max(...included.map((p) => p.windGust)),
    })
  })
})

describe('todayTempBand', () => {
  it('returns low/high for a normal day', () => {
    expect(todayTempBand(day({ low: 55, high: 78 }))).toEqual({ low: 55, high: 78 })
  })

  it('returns null when day is undefined', () => {
    expect(todayTempBand(undefined)).toBeNull()
  })

  it('returns null when low is the -1 sentinel', () => {
    expect(todayTempBand(day({ low: -1, high: 78 }))).toBeNull()
  })

  it('returns null when high is the -1 sentinel', () => {
    expect(todayTempBand(day({ low: 55, high: -1 }))).toBeNull()
  })
})

describe('nextRain', () => {
  it('returns null when every point has a null precipChancePct', () => {
    const days = [day({ dayKey: 'd0', hourlyPrecip: [precipPoint(0, null), precipPoint(1, null)] })]
    expect(nextRain(days, 0)).toBeNull()
  })

  it("returns 'none' when data is present but nothing reaches the threshold", () => {
    const days = [day({ dayKey: 'd0', hourlyPrecip: [precipPoint(0, 10), precipPoint(1, 30)] })]
    expect(nextRain(days, 0)).toBe('none')
  })

  it('returns the first hour in window order to cross the threshold, even when a later hour is higher', () => {
    const days = [
      day({
        dayKey: 'd0',
        hourlyPrecip: [
          precipPoint(0, 20, 'noon'),
          precipPoint(1, 50, 'first-over'),
          precipPoint(2, 90, 'higher-later'),
        ],
      }),
    ]
    const result = nextRain(days, 0)
    expect(result).toEqual({ label: 'first-over', chancePct: 50, isNow: false })
  })

  it('respects a custom threshold parameter', () => {
    const days = [day({ dayKey: 'd0', hourlyPrecip: [precipPoint(0, 60, 'sixty')] })]
    expect(nextRain(days, 0, 70)).toBe('none')
    expect(nextRain(days, 0, 50)).toEqual({ label: 'sixty', chancePct: 60, isNow: true })
  })

  it('marks isNow true when the first over-threshold hour is the current hour', () => {
    const days = [day({ dayKey: 'd0', hourlyPrecip: [precipPoint(11, 50, '11AM')] })]
    const result = nextRain(days, 11)
    expect(result).toEqual({ label: '11AM', chancePct: 50, isNow: true })
  })

  it('marks isNow false when the first over-threshold hour is later than the current hour', () => {
    const days = [
      day({
        dayKey: 'd0',
        hourlyPrecip: [precipPoint(11, 20, '11AM'), precipPoint(14, 50, '2PM')],
      }),
    ]
    const result = nextRain(days, 11)
    expect(result).toEqual({ label: '2PM', chancePct: 50, isNow: false })
  })

  it("returns 'none' when real data is mixed with nulls but none cross threshold", () => {
    const days = [
      day({
        dayKey: 'd0',
        hourlyPrecip: [precipPoint(0, null), precipPoint(1, 15), precipPoint(2, null), precipPoint(3, 25)],
      }),
    ]
    expect(nextRain(days, 0)).toBe('none')
  })
})
