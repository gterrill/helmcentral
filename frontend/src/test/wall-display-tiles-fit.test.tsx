import { render } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { ClockTile } from '@/components/clock-tile'
import { CurrentConditionsTile } from '@/components/current-conditions-tile'
import { ForecastDaysTile } from '@/components/forecast-days-tile'
import { SeaStateTile } from '@/components/sea-state-tile'
import { WIDGET_CONSTRAINTS, gridPixelHeight } from '@/components/dashboard-bento-grid'
import type { WeatherForecastDay } from '@/hooks/use-weather-forecast'
import type { WaveForecastDay } from '@/hooks/use-wave-forecast'
import type { WeatherToday } from '@/hooks/use-weather-today'

/**
 * ADR 0092 verification step: none of the four wall-display tiles may need
 * more vertical room than the grid row budget they were registered with
 * (dashboard-bento-grid.tsx's WIDGET_CONSTRAINTS), since a tile that
 * overflows its own minH can never actually fit inside the kiosk's 344px
 * fold - the whole reason these tiles exist.
 *
 * jsdom has no layout engine: every element's getBoundingClientRect and
 * scrollHeight read 0 regardless of its content or CSS, so a literal pixel
 * overflow measurement is not available in this environment (the same
 * reason forecast-drawer.tsx's own charts pass an explicit pixel width down
 * rather than relying on a browser-measured one in tests). What IS
 * checkable here, and is checked below: the tile renders at the exact pixel
 * box its own registered constraint promises, at column widths taken from
 * the real 1920px kiosk strip (12 columns, 16px margins - the same
 * GRID_ROW_HEIGHT/GRID_MARGIN math dashboard-bento-grid-constraints.test.ts
 * already uses), without throwing, and with no element declaring an inline
 * height/min-height that already exceeds that box on its face. A true
 * pixel-perfect visual check needs a real browser against this worktree's
 * own build, which the verification step in this session's brief explicitly
 * deferred in favour of this DOM-level check.
 */

const KIOSK_WIDTH_PX = 1920
const GRID_COLUMNS = 12
const GRID_MARGIN_PX = 16
const COLUMN_WIDTH_PX = (KIOSK_WIDTH_PX - GRID_MARGIN_PX * (GRID_COLUMNS + 1)) / GRID_COLUMNS

function columnsToPx(cols: number): number {
  return cols * COLUMN_WIDTH_PX + (cols - 1) * GRID_MARGIN_PX
}

function weatherDay(overrides: Partial<WeatherForecastDay> = {}): WeatherForecastDay {
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
    precipitation: 20,
    humidityPct: 60,
    visibilityNm: 10,
    sunriseTime: '6:02AM',
    sunsetTime: '7:41PM',
    moonPhase: 'waxingGibbous',
    hourlyWind: Array.from({ length: 24 }, (_, h) => ({
      label: `${h}:00`, hourOfDay: h, windSpeed: 10 + h * 0.2, windGust: 15 + h * 0.2, windDirection: 'E', windDirectionDeg: 90,
    })),
    hourlyPrecip: Array.from({ length: 24 }, (_, h) => ({ label: `${h}:00`, hourOfDay: h, precipChancePct: 20, precipIntensityMm: 0 })),
    hourlyUV: [],
    hourlyCloud: [],
    ...overrides,
  }
}

function waveDay(): WaveForecastDay {
  return {
    dayKey: '2026-06-14',
    date: 'Jun 14',
    dayName: 'Sunday',
    waveSummary: null,
    hourlyWave: Array.from({ length: 24 }, (_, h) => ({
      label: `${h}:00`, hourOfDay: h, waveHeightM: 1 + h * 0.02, wavePeriodS: 6, waveDirectionDeg: 180,
      windWaveHeightM: 0.5, swellWaveHeightM: 0.5, windWaveDirectionDeg: 0, windWavePeriodS: 5,
      swellWaveDirectionDeg: 0, swellWavePeriodS: 8, steepnessRatio: 0.02, steepnessBand: 'rolling' as const,
    })),
    indicators: { waveFront: false, rapidBuild: false, periodStep: false, crossSea: false },
  }
}

const WEATHER_TODAY: WeatherToday = {
  datetime: '2026-06-14T14:00:00Z', temperature_f: 82, condition: 'Clear', wind_speed_kts: 14,
  wind_gust_kts: 19, wind_direction: 'ENE', precipitation_pct: 20, provider: 'weatherkit',
  cached: false, updated_at: '2026-06-14T14:00:00Z', ttl_seconds: 900,
}

/**
 * Renders `ui` inside a div whose fixed inline width/height match the box
 * react-grid-layout would actually give this widget, then asserts nothing
 * inside it declares an inline height/min-height in px larger than the box
 * itself - the one overflow signal jsdom can actually see, since it has no
 * layout engine to measure real overflow with.
 */
function renderAtGridBox(ui: React.ReactElement, widthPx: number, heightPx: number) {
  const { container } = render(
    <div style={{ width: `${widthPx}px`, height: `${heightPx}px` }} data-testid="grid-box">
      {ui}
    </div>,
  )

  const offenders: string[] = []
  container.querySelectorAll<HTMLElement>('[style]').forEach((el) => {
    const h = el.style.height
    const minH = el.style.minHeight
    for (const [label, raw] of [['height', h], ['min-height', minH]] as const) {
      if (raw && raw.endsWith('px')) {
        const px = parseFloat(raw)
        if (Number.isFinite(px) && px > heightPx) {
          offenders.push(`${el.tagName.toLowerCase()}.${el.className || '(no class)'} declares inline ${label}:${raw}, over the ${heightPx}px box`)
        }
      }
    }
  })

  expect(offenders, `Found element(s) with an inline height exceeding the tile's own grid box:\n${offenders.join('\n')}`).toEqual([])
  return container
}

describe('wall-display tiles fit their registered grid constraint', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-06-14T14:00:00Z'))
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, json: async () => ({}) }))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  test('clock-tile', () => {
    const { minW, minH } = WIDGET_CONSTRAINTS.clock!
    renderAtGridBox(
      <ClockTile
        sunriseTime="6:02AM"
        sunsetTime="7:41PM"
        moonPhase="waxingGibbous"
        placeName="Airlie Beach, Queensland"
        nextWaypoint={{ label: 'Mooloolaba Marina', etaAt: new Date('2026-06-15T04:00:00Z'), basis: 'plan' }}
      />,
      columnsToPx(minW!),
      gridPixelHeight(minH!),
    )
  })

  test('current-conditions-tile', () => {
    const { minW, minH } = WIDGET_CONSTRAINTS['current-conditions']!
    renderAtGridBox(
      <CurrentConditionsTile
        depth={12.4}
        depthLastUpdateAgeS={5}
        windSpeedApparentKts={14}
        maxGustKts={{ '10m': 16, '30m': 17, '1h': 19, '24h': 22 }}
        weather={WEATHER_TODAY}
        forecast={[weatherDay()]}
        distanceUnits="metric"
      />,
      columnsToPx(minW!),
      gridPixelHeight(minH!),
    )
  })

  test('forecast-days-tile', () => {
    const { minW, minH } = WIDGET_CONSTRAINTS['forecast-days']!
    const days = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'].map((n) => weatherDay({ dayName: n }))
    renderAtGridBox(<ForecastDaysTile days={days} units="imperial" />, columnsToPx(minW!), gridPixelHeight(minH!))
  })

  test('sea-state-tile', () => {
    const { minW, minH } = WIDGET_CONSTRAINTS['sea-state']!
    renderAtGridBox(
      <SeaStateTile forecast={[weatherDay()]} waveForecastDays={[waveDay()]} waveLoading={false} waveError={null} units="metric" />,
      columnsToPx(minW!),
      gridPixelHeight(minH!),
    )
  })
})
