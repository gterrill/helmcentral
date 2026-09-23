import { act, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { ForecastConditionsTile } from '@/components/forecast-conditions-tile'
import { SEA_STATE_PLOT_INSET, xForDayBoundary } from '@/lib/sea-state-geometry'
import type { WeatherForecastDay } from '@/hooks/use-weather-forecast'
import type { WaveForecastDay } from '@/hooks/use-wave-forecast'

/**
 * Captures the observed target and callback instead of firing on its own
 * (jsdom's global ResizeObserver stub - src/test/setup.ts - never fires),
 * so a test can trigger a resize with a specific contentRect and inspect
 * exactly which element was observed.
 */
class MockResizeObserver implements ResizeObserver {
  static instances: MockResizeObserver[] = []
  callback: ResizeObserverCallback
  target: Element | null = null

  constructor(callback: ResizeObserverCallback) {
    this.callback = callback
    MockResizeObserver.instances.push(this)
  }

  observe(target: Element) {
    this.target = target
  }

  unobserve() {
    this.target = null
  }

  disconnect() {
    this.target = null
  }

  fire(contentRect: { width: number; height: number }) {
    act(() => {
      this.callback(
        [{ contentRect } as ResizeObserverEntry],
        this as unknown as ResizeObserver,
      )
    })
  }
}

function day(dayName: string, overrides: Partial<WeatherForecastDay> = {}): WeatherForecastDay {
  return {
    dayKey: dayName,
    date: dayName,
    dayName,
    condition: 'Clear',
    high: 82,
    low: 68,
    windSpeed: 10,
    windGust: 14,
    windDirection: 'E',
    windSummary: null,
    precipitationSummary: null,
    precipitation: null,
    humidityPct: null,
    visibilityNm: null,
    sunriseTime: null,
    sunsetTime: null,
    moonPhase: null,
    hourlyWind: Array.from({ length: 24 }, (_, h) => ({
      label: `${h}:00`,
      hourOfDay: h,
      windSpeed: 10,
      windGust: 15,
      windDirection: 'E',
      windDirectionDeg: 90,
    })),
    hourlyPrecip: [],
    hourlyUV: [],
    hourlyCloud: [],
    ...overrides,
  }
}

const SEVEN_DAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'].map((n, i) =>
  day(n, { high: 80 + i, low: 60 + i }),
)

function waveDay(dayKey: string): WaveForecastDay {
  return {
    dayKey,
    date: dayKey,
    dayName: 'Sunday',
    waveSummary: null,
    hourlyWave: Array.from({ length: 24 }, (_, h) => ({
      label: `${h}:00`,
      hourOfDay: h,
      waveHeightM: 1,
      wavePeriodS: 6,
      waveDirectionDeg: 180,
      windWaveHeightM: 0.5,
      swellWaveHeightM: 0.5,
      windWaveDirectionDeg: 0,
      windWavePeriodS: 5,
      swellWaveDirectionDeg: 0,
      swellWavePeriodS: 8,
      steepnessRatio: 0.02,
      steepnessBand: 'rolling' as const,
    })),
    indicators: { waveFront: false, rapidBuild: false, periodStep: false, crossSea: false },
  }
}

const defaultProps = {
  forecast: SEVEN_DAYS,
  waveForecastDays: [] as WaveForecastDay[],
  waveLoading: false,
  waveError: null as string | null,
  units: 'imperial' as const,
}

describe('ForecastConditionsTile', () => {
  // ── carried over from forecast-days-tile.test.tsx ──────────────────────

  test('renders tomorrow onward, skipping today', () => {
    render(<ForecastConditionsTile {...defaultProps} />)

    expect(screen.queryByText('SUN')).not.toBeInTheDocument()
    expect(screen.getByText('MON')).toBeInTheDocument()
    expect(screen.getByText('FRI')).toBeInTheDocument()
  })

  test('renders at most five days', () => {
    render(<ForecastConditionsTile {...defaultProps} />)

    // tomorrow(Mon) through 5 days out = Mon..Fri, Saturday excluded.
    expect(screen.queryByText('SAT')).not.toBeInTheDocument()
  })

  test('shows imperial highs and lows in Fahrenheit', () => {
    render(<ForecastConditionsTile {...defaultProps} forecast={[day('Sunday'), day('Monday', { high: 82, low: 68 })]} units="imperial" />)

    expect(screen.getByText('82°')).toBeInTheDocument()
    expect(screen.getByText('68°')).toBeInTheDocument()
  })

  test('converts highs and lows to Celsius when metric', () => {
    render(<ForecastConditionsTile {...defaultProps} forecast={[day('Sunday'), day('Monday', { high: 82, low: 68 })]} units="metric" />)

    // 82F -> 28C, 68F -> 20C
    expect(screen.getByText('28°')).toBeInTheDocument()
    expect(screen.getByText('20°')).toBeInTheDocument()
  })

  test('renders dash cards for missing days rather than hiding them, always showing five slots', () => {
    render(<ForecastConditionsTile {...defaultProps} forecast={[day('Sunday'), day('Monday')]} />)

    expect(screen.getAllByText('--').length).toBeGreaterThan(0)
    expect(screen.getAllByTestId('forecast-days-card')).toHaveLength(5)
  })

  test('shows a dash when a day has the -1 sentinel for high or low', () => {
    render(<ForecastConditionsTile {...defaultProps} forecast={[day('Sunday'), day('Monday', { high: -1, low: -1 })]} />)

    const card = screen.getAllByTestId('forecast-days-card')[0]
    expect(card).toHaveTextContent('--')
  })

  // ── carried over from sea-state-tile.test.tsx ───────────────────────────

  // SeaStateChart (and the recharts it pulls in) is lazy-loaded behind a
  // Suspense boundary (kiosk bundle-split follow-up: recharts used to load
  // at startup on every route, including /kiosk, purely because this tile
  // imported it eagerly). The Tile chrome and card row render synchronously
  // either way; only the chart itself needs a beat to resolve.
  test('renders the chart when wave data is available', async () => {
    render(<ForecastConditionsTile {...defaultProps} forecast={[day('2026-06-14')]} waveForecastDays={[waveDay('2026-06-14')]} />)

    expect(screen.getByText('Forecast Conditions')).toBeInTheDocument()
    await waitFor(() => {
      expect(document.querySelector('svg.recharts-surface')).toBeInTheDocument()
    })
    expect(screen.queryByTestId('forecast-wave-error')).not.toBeInTheDocument()
  })

  test('shows the unavailable message and no chart when the wave feed errors', () => {
    render(<ForecastConditionsTile {...defaultProps} forecast={[day('2026-06-14')]} waveError="fetch failed" />)

    expect(screen.getByTestId('forecast-wave-error')).toHaveTextContent(/wave data unavailable/i)
    expect(document.querySelector('svg.recharts-surface')).not.toBeInTheDocument()
  })

  test('renders without crashing when the wave feed has not answered yet', () => {
    render(<ForecastConditionsTile {...defaultProps} forecast={[day('2026-06-14')]} waveLoading />)

    expect(screen.getByText('Forecast Conditions')).toBeInTheDocument()
  })

  describe('measured box sizing', () => {
    afterEach(() => {
      vi.unstubAllGlobals()
      MockResizeObserver.instances = []
    })

    test('observes a relative, height-constrained container and sizes the chart from a resize entry, not its own content', async () => {
      MockResizeObserver.instances = []
      vi.stubGlobal('ResizeObserver', MockResizeObserver)

      render(<ForecastConditionsTile {...defaultProps} forecast={[day('2026-06-14')]} waveForecastDays={[waveDay('2026-06-14')]} />)

      expect(MockResizeObserver.instances).toHaveLength(1)
      const observer = MockResizeObserver.instances[0]!
      const measuredBox = observer.target as HTMLElement
      expect(measuredBox).not.toBeNull()
      expect(measuredBox.classList.contains('relative')).toBe(true)

      const chartWrapper = measuredBox.querySelector(':scope > .absolute.inset-0')
      expect(chartWrapper).not.toBeNull()
      await waitFor(() => {
        expect(chartWrapper?.querySelector('svg.recharts-surface')).toBeInTheDocument()
      })

      observer.fire({ width: 582, height: 250 })

      const svg = document.querySelector('svg.recharts-surface')
      expect(svg).toHaveAttribute('width', '582')
      expect(svg).toHaveAttribute('height', '250')
    })
  })

  // ── new: days must match between the card row and the chart ────────────

  test('the card row and the chart series cover the same five days (tomorrow..+5)', () => {
    const forecast = SEVEN_DAYS
    const waveForecastDays = forecast.map((d) => waveDay(d.dayKey))
    render(<ForecastConditionsTile {...defaultProps} forecast={forecast} waveForecastDays={waveForecastDays} />)

    // Cards show Monday..Friday (index 1..5); Sunday (today) and Saturday
    // (day 6) must not appear.
    for (const label of ['MON', 'TUE', 'WED', 'THU', 'FRI']) {
      expect(screen.getByText(label)).toBeInTheDocument()
    }
    expect(screen.queryByText('SUN')).not.toBeInTheDocument()
    expect(screen.queryByText('SAT')).not.toBeInTheDocument()
  })

  // ── new: alignment is the point (ADR 0125) ──────────────────────────────

  test('insets the card row by exactly the chart own plot bounds, so the dashed day-boundary lines sit under the card gaps', () => {
    const { container } = render(<ForecastConditionsTile {...defaultProps} />)

    const cardRow = container.querySelector('.grid.grid-cols-5') as HTMLElement
    expect(cardRow).not.toBeNull()
    expect(cardRow.style.paddingLeft).toBe(`${SEA_STATE_PLOT_INSET.left}px`)
    expect(cardRow.style.paddingRight).toBe(`${SEA_STATE_PLOT_INSET.right}px`)
  })

  test('the card row has no explicit gap - each cell supplies its own visual gap so cell boundaries fall at exact fifths', () => {
    const { container } = render(<ForecastConditionsTile {...defaultProps} />)

    const cardRow = container.querySelector('.grid.grid-cols-5') as HTMLElement
    expect(cardRow.className).not.toMatch(/\bgap-/)
  })

  test('renders exactly five card cells regardless of how many real forecast days exist', () => {
    render(<ForecastConditionsTile {...defaultProps} forecast={[day('Sunday'), day('Monday')]} />)
    expect(screen.getAllByTestId('forecast-days-card')).toHaveLength(5)
  })

  // The fewer-than-5-real-days case (ADR 0125): the chart must still divide
  // its width into 5 day columns rather than stretching 2 real days across
  // the whole tile, so the 5 card cells above keep lining up with the
  // chart's day boundaries below even when the forecast is short.
  test('still shows 4 day-boundary lines (5 columns) in the chart when the forecast has only 2 real days', async () => {
    const shortForecast = [day('Sunday'), day('Monday'), day('Tuesday')]
    const shortWaves = [waveDay('Monday'), waveDay('Tuesday')]
    render(<ForecastConditionsTile {...defaultProps} forecast={shortForecast} waveForecastDays={shortWaves} />)

    await waitFor(() => {
      expect(screen.getAllByTestId('sea-state-day-boundary')).toHaveLength(4)
    })
  })

  test('a day boundary in the padded (fewer-than-5-real-days) chart still lands at the same pixel xForDayBoundary predicts', async () => {
    const shortForecast = [day('Sunday'), day('Monday'), day('Tuesday')]
    const shortWaves = [waveDay('Monday'), waveDay('Tuesday')]
    const { container } = render(<ForecastConditionsTile {...defaultProps} forecast={shortForecast} waveForecastDays={shortWaves} />)

    await waitFor(() => {
      expect(container.querySelectorAll('.recharts-reference-line line').length).toBe(4)
    })
    const lines = Array.from(container.querySelectorAll('.recharts-reference-line line'))
    const firstLineX = Number(lines[0]!.getAttribute('x1'))
    // width falls back to the tile's FALLBACK_WIDTH (800) in jsdom, since
    // ResizeObserver never fires there (src/test/setup.ts's stub).
    expect(firstLineX).toBeCloseTo(xForDayBoundary(1, 800), 0)
  })
})
