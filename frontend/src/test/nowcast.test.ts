import { describe, it, expect } from 'vitest'

import {
  buildNowcastBars,
  hasNowcastRain,
  nowcastIntensityLabel,
  computeNowcastStatus,
  NOWCAST_WINDOW_MINUTES,
  type NowcastPoint,
} from '@/lib/nowcast'
import type { WeatherForecastDay, WeatherHourlyPrecipPoint } from '@/hooks/use-weather-forecast'

const NOW = new Date('2026-06-14T14:00:00Z')

function point(minutesFromNow: number, chancePct: number | null, mmPerH: number): NowcastPoint {
  return { time: new Date(NOW.getTime() + minutesFromNow * 60000), chancePct, mmPerH }
}

function precipPoint(hourOfDay: number, precipChancePct: number | null, label?: string): WeatherHourlyPrecipPoint {
  return { label: label ?? `${hourOfDay}:00`, hourOfDay, precipChancePct, precipIntensityMm: 0 }
}

function day(overrides: Partial<WeatherForecastDay> = {}): WeatherForecastDay {
  return {
    dayKey: '2026-06-14',
    date: 'Jun 14',
    dayName: 'Sunday',
    condition: 'Clear',
    high: 82,
    low: 68,
    windSpeed: 12,
    windGust: 18,
    windDirection: 'ENE',
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

describe('buildNowcastBars', () => {
  it('clips a partially-elapsed current bucket to start at offset 0', () => {
    // A 15-minute bucket that started 5 minutes ago still covers "now" for
    // another 10 minutes - it must appear as a 10-minute-wide bar starting
    // at 0, not be dropped or shown starting before "now".
    const bars = buildNowcastBars([point(-5, 40, 1.2)], NOW, 15)
    expect(bars).toHaveLength(1)
    expect(bars[0].offsetMinutes).toBe(0)
    expect(bars[0].widthMinutes).toBe(10)
  })

  it('drops points that ended before now (stale data)', () => {
    // The whole window is in the past relative to "now" - e.g. a cached
    // bundle whose nowcast has fully aged out. Every point must be dropped,
    // not clamped into the window as if it were still current.
    const stalePoints = [point(-120, 80, 3), point(-105, 60, 2), point(-90, 20, 0.5)]
    const bars = buildNowcastBars(stalePoints, NOW, 15)
    expect(bars).toHaveLength(0)
  })

  it('drops points that start at or beyond the window end', () => {
    const bars = buildNowcastBars([point(60, 50, 1), point(75, 50, 1)], NOW, 15)
    expect(bars).toHaveLength(0)
  })

  it('clips the last bar to the window boundary', () => {
    const bars = buildNowcastBars([point(50, 30, 0.8)], NOW, 15)
    expect(bars).toHaveLength(1)
    expect(bars[0].offsetMinutes).toBe(50)
    expect(bars[0].widthMinutes).toBe(10) // 50 -> 60, clipped from a 15-minute bucket
  })

  it('keeps one-minute cadence bars as one-minute-wide, in order', () => {
    const points = [point(0, 10, 0), point(1, 10, 0), point(2, 35, 0.5)]
    const bars = buildNowcastBars(points, NOW, 1)
    expect(bars.map((b) => b.offsetMinutes)).toEqual([0, 1, 2])
    expect(bars.every((b) => b.widthMinutes === 1)).toBe(true)
  })
})

describe('hasNowcastRain', () => {
  it('is false for an empty bar list', () => {
    expect(hasNowcastRain([])).toBe(false)
  })

  it('is false when every bar is dry (0 chance, 0 intensity)', () => {
    const bars = buildNowcastBars([point(0, 0, 0), point(15, 0, 0)], NOW, 15)
    expect(hasNowcastRain(bars)).toBe(false)
  })

  it('is true when a bar has a positive chance even with 0 intensity', () => {
    const bars = buildNowcastBars([point(0, 40, 0)], NOW, 15)
    expect(hasNowcastRain(bars)).toBe(true)
  })

  it('is true when a bar has intensity but a null (not supplied) chance', () => {
    const bars = buildNowcastBars([point(0, null, 1.4)], NOW, 15)
    expect(hasNowcastRain(bars)).toBe(true)
  })

  it('is false when chance is null and intensity is 0 - null chance is not evidence of dry, but it is not evidence of rain either', () => {
    const bars = buildNowcastBars([point(0, null, 0)], NOW, 15)
    expect(hasNowcastRain(bars)).toBe(false)
  })
})

describe('nowcastIntensityLabel', () => {
  it('bands mm/h into light/moderate/heavy', () => {
    expect(nowcastIntensityLabel(0)).toBe('light')
    expect(nowcastIntensityLabel(2.4)).toBe('light')
    expect(nowcastIntensityLabel(2.5)).toBe('moderate')
    expect(nowcastIntensityLabel(7.5)).toBe('moderate')
    expect(nowcastIntensityLabel(7.6)).toBe('heavy')
    expect(nowcastIntensityLabel(20)).toBe('heavy')
  })
})

describe('computeNowcastStatus', () => {
  const nowHour = NOW.getUTCHours()

  it('provider has no nowcast at all: falls back to hourly/daily, never draws the chart', () => {
    const result = computeNowcastStatus({
      now: NOW,
      nextHour: null,
      forecastDays: [day({ hourlyPrecip: [] })],
      nowHour,
    })
    expect(result.showChart).toBe(false)
    expect(result.bars).toHaveLength(0)
    // No hourly/daily data either - never a fabricated "no rain" claim.
    expect(result.line).toBe('—')
  })

  it('nowcast supplied but entirely dry: does not draw the chart, falls back to hourly/daily', () => {
    const points = Array.from({ length: 4 }, (_, i) => point(i * 15, 5, 0)) // some chance, but under any reasonable signal? use 0 to mean genuinely dry
    const dryPoints = Array.from({ length: 4 }, (_, i) => point(i * 15, 0, 0))
    const result = computeNowcastStatus({
      now: NOW,
      nextHour: { stepMinutes: 15, points: dryPoints },
      forecastDays: [day()],
      nowHour,
    })
    expect(result.showChart).toBe(false)
    expect(result.bars.length).toBeGreaterThan(0) // the points exist, just carry no rain signal
    void points
  })

  it('rain now: the bucket covering "now" already has signal', () => {
    const points = [point(0, 60, 2.1), point(15, 70, 3)]
    const result = computeNowcastStatus({
      now: NOW,
      nextHour: { stepMinutes: 15, points },
      forecastDays: [day()],
      nowHour,
    })
    expect(result.showChart).toBe(true)
    expect(result.line).toBe('Moderate rain expected now')
  })

  it('rain in N minutes: the first signal is a later bucket', () => {
    const points = [point(0, 0, 0), point(15, 0, 0), point(30, 55, 1.8)]
    const result = computeNowcastStatus({
      now: NOW,
      nextHour: { stepMinutes: 15, points },
      forecastDays: [day()],
      nowHour,
    })
    expect(result.showChart).toBe(true)
    expect(result.line).toBe('Light rain expected in 30 minutes')
  })

  it('a small chance with no rainfall does not count as rain starting', () => {
    const points = [point(0, 10, 0), point(15, 10, 0), point(30, 55, 1.8)]
    const result = computeNowcastStatus({
      now: NOW,
      nextHour: { stepMinutes: 15, points },
      forecastDays: [day()],
      nowHour,
    })
    expect(result.showChart).toBe(true)
    expect(result.line).toBe('Light rain expected in 30 minutes')
  })

  it('names the heaviest rain in the hour, not the first bucket', () => {
    const points = [point(0, 60, 1), point(15, 80, 9)]
    const result = computeNowcastStatus({
      now: NOW,
      nextHour: { stepMinutes: 15, points },
      forecastDays: [day()],
      nowHour,
    })
    expect(result.line).toBe('Heavy rain expected now')
  })

  it('window fully in the past (stale data): treated the same as no nowcast, never shown as current', () => {
    const points = [point(-180, 80, 3), point(-165, 60, 2)]
    const result = computeNowcastStatus({
      now: NOW,
      nextHour: { stepMinutes: 15, points },
      forecastDays: [day({ hourlyPrecip: [] })],
      nowHour,
    })
    expect(result.bars).toHaveLength(0)
    expect(result.showChart).toBe(false)
    expect(result.line).toBe('—')
  })

  it('chance supplied: a positive chance alone is enough to show the chart even with 0 intensity', () => {
    const result = computeNowcastStatus({
      now: NOW,
      nextHour: { stepMinutes: 15, points: [point(0, 45, 0), point(15, 20, 0)] },
      forecastDays: [day()],
      nowHour,
    })
    expect(result.showChart).toBe(true)
    expect(result.line).toBe('45% chance of rain in the next hour')
  })

  it('chance not supplied: falls back to intensity alone to decide whether to show the chart', () => {
    const result = computeNowcastStatus({
      now: NOW,
      nextHour: { stepMinutes: 15, points: [point(0, null, 1.2)] },
      forecastDays: [day()],
      nowHour,
    })
    expect(result.showChart).toBe(true)
    expect(result.line).toBe('Light rain expected now')
  })

  it('falls back to the hourly nextRain signal when the nowcast is dry', () => {
    const result = computeNowcastStatus({
      now: NOW,
      nextHour: { stepMinutes: 15, points: [point(0, 0, 0), point(45, 0, 0)] },
      forecastDays: [
        day({
          hourlyPrecip: [precipPoint(nowHour + 2, 80, '4PM')],
        }),
      ],
      nowHour,
    })
    expect(result.showChart).toBe(false)
    expect(result.line).toBe('Rain likely from 4PM (80%)')
  })

  // The hourly fallback restores nextRain's own 40% "likely" threshold
  // (not a bare positive-chance one) - a 2% chance is not "rain expected",
  // and claiming otherwise is the false positive the fallback policy exists
  // to prevent.
  it('a real but unlikely hourly chance (below the 40% threshold) never reads as rain coming', () => {
    const result = computeNowcastStatus({
      now: NOW,
      nextHour: null,
      forecastDays: [
        day({
          hourlyPrecip: [precipPoint(nowHour + 1, 2, '3PM')],
          precipitation: null,
        }),
      ],
      nowHour,
    })
    expect(result.line).not.toMatch(/rain/i)
  })

  it('reports the chance alongside the label when the hourly tier finds a likely hour, matching the old tile\'s wording', () => {
    const result = computeNowcastStatus({
      now: NOW,
      nextHour: null,
      forecastDays: [day({ hourlyPrecip: [precipPoint(nowHour + 1, 45, '3PM')] })],
      nowHour,
    })
    expect(result.line).toBe('Rain likely from 3PM (45%)')
  })

  it('reports "now" when the hourly tier\'s own likely match is the current hour, with no real nowcast to defer to', () => {
    const result = computeNowcastStatus({
      now: NOW,
      nextHour: null,
      forecastDays: [day({ hourlyPrecip: [precipPoint(nowHour, 55, 'Now')] })],
      nowHour,
    })
    expect(result.line).toBe('Rain likely now (55%)')
  })

  // A dry-but-real nowcast (bars drawn, just carrying no signal) already
  // spoke for the current hour more precisely than the hourly forecast's
  // own blunt per-hour bucket can - the hourly fallback must not then
  // contradict it by reporting "now" off that same hour's coarser chance.
  it('a dry real nowcast defers the hourly fallback past the hour it already covered, never reporting "now" off that hour', () => {
    const result = computeNowcastStatus({
      now: NOW,
      nextHour: { stepMinutes: 15, points: [point(0, 0, 0), point(15, 0, 0), point(30, 0, 0), point(45, 0, 0)] },
      forecastDays: [
        day({
          hourlyPrecip: [
            precipPoint(nowHour, 45, 'Now'), // would have matched "now" before this fix
            precipPoint(nowHour + 1, 50, '3PM'),
          ],
        }),
      ],
      nowHour,
    })
    expect(result.showChart).toBe(false)
    expect(result.line).toBe('Rain likely from 3PM (50%)')
  })

  // Same scenario, but with no hour past the one the nowcast covered
  // clearing the threshold either - the hourly tier must report nothing
  // usable (falling further down the cascade) rather than reaching back
  // into the hour the nowcast already spoke for.
  it('a dry real nowcast never falls back onto its own covered hour even when nothing later qualifies', () => {
    const result = computeNowcastStatus({
      now: NOW,
      nextHour: { stepMinutes: 15, points: [point(0, 0, 0), point(15, 0, 0), point(30, 0, 0), point(45, 0, 0)] },
      forecastDays: [
        day({
          hourlyPrecip: [precipPoint(nowHour, 90, 'Now')],
          precipitation: 0,
        }),
      ],
      nowHour,
    })
    expect(result.showChart).toBe(false)
    expect(result.line).not.toMatch(/90%/)
    expect(result.line).not.toMatch(/now/i)
  })

  it('falls all the way back to a dash when there is no nowcast and no hourly/daily data at all', () => {
    const result = computeNowcastStatus({
      now: NOW,
      nextHour: null,
      forecastDays: [],
      nowHour,
    })
    expect(result.line).toBe('—')
  })

  describe('next_hour_source captioning', () => {
    it('source "nowcast": no caption suffix, isHourlySourced is false', () => {
      const result = computeNowcastStatus({
        now: NOW,
        nextHour: { stepMinutes: 15, points: [point(0, 60, 2.1)], source: 'nowcast' },
        forecastDays: [day()],
        nowHour,
      })
      expect(result.showChart).toBe(true)
      expect(result.isHourlySourced).toBe(false)
      expect(result.line).toBe('Light rain expected now')
    })

    it('source omitted: defaults to uncaptioned behaviour (existing callers/tests)', () => {
      const result = computeNowcastStatus({
        now: NOW,
        nextHour: { stepMinutes: 15, points: [point(0, 60, 2.1)] },
        forecastDays: [day()],
        nowHour,
      })
      expect(result.isHourlySourced).toBe(false)
      expect(result.line).toBe('Light rain expected now')
    })

    it('source "hourly": the rain-now/rain-in-N-minutes line names it honestly', () => {
      const points = [point(0, 0, 0), point(15, 0, 0), point(30, 20, 1.8)]
      const result = computeNowcastStatus({
        now: NOW,
        nextHour: { stepMinutes: 15, points, source: 'hourly' },
        forecastDays: [day()],
        nowHour,
      })
      expect(result.showChart).toBe(true)
      expect(result.isHourlySourced).toBe(true)
      expect(result.line).toBe('Light rain expected in 30 minutes (hourly forecast)')
    })

    it('source "hourly": the chance-only line also names it honestly', () => {
      const result = computeNowcastStatus({
        now: NOW,
        nextHour: { stepMinutes: 15, points: [point(0, 80, 0), point(15, 20, 0)], source: 'hourly' },
        forecastDays: [day()],
        nowHour,
      })
      expect(result.isHourlySourced).toBe(true)
      expect(result.line).toBe('80% chance of rain in the next hour (hourly forecast)')
    })

    it('source "hourly" but the nowcast is dry: falls back to the hourly-fallback tier with no caption - that tier is not misrepresenting its own resolution', () => {
      const result = computeNowcastStatus({
        now: NOW,
        nextHour: { stepMinutes: 15, points: [point(0, 0, 0), point(45, 0, 0)], source: 'hourly' },
        forecastDays: [day({ hourlyPrecip: [precipPoint(nowHour + 2, 80, '4PM')] })],
        nowHour,
      })
      expect(result.showChart).toBe(false)
      expect(result.isHourlySourced).toBe(false)
      expect(result.line).not.toMatch(/hourly forecast/)
    })
  })
})

describe('NOWCAST_WINDOW_MINUTES', () => {
  it('is 60', () => {
    expect(NOWCAST_WINDOW_MINUTES).toBe(60)
  })
})
