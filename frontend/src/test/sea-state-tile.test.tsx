import { act, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { SeaStateTile } from '@/components/sea-state-tile'
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

function day(dayKey: string): WeatherForecastDay {
  return {
    dayKey,
    date: dayKey,
    dayName: 'Sunday',
    condition: 'Clear',
    high: 80,
    low: 65,
    windSpeed: 10,
    windGust: 15,
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
  }
}

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

describe('SeaStateTile', () => {
  test('renders the chart when wave data is available', () => {
    render(
      <SeaStateTile
        forecast={[day('2026-06-14')]}
        waveForecastDays={[waveDay('2026-06-14')]}
        waveLoading={false}
        waveError={null}
        units="metric"
      />,
    )

    expect(screen.getByText('Sea State')).toBeInTheDocument()
    expect(document.querySelector('svg.recharts-surface')).toBeInTheDocument()
    expect(screen.queryByTestId('forecast-wave-error')).not.toBeInTheDocument()
  })

  test('shows the unavailable message and no chart when the wave feed errors', () => {
    render(
      <SeaStateTile
        forecast={[day('2026-06-14')]}
        waveForecastDays={[]}
        waveLoading={false}
        waveError="fetch failed"
        units="metric"
      />,
    )

    expect(screen.getByTestId('forecast-wave-error')).toHaveTextContent(/wave data unavailable/i)
    expect(document.querySelector('svg.recharts-surface')).not.toBeInTheDocument()
  })

  test('renders without crashing when the wave feed has not answered yet', () => {
    render(
      <SeaStateTile forecast={[day('2026-06-14')]} waveForecastDays={[]} waveLoading waveError={null} units="metric" />,
    )

    expect(screen.getByText('Sea State')).toBeInTheDocument()
  })

  // Regression test for the ResizeObserver feedback loop: the measured box
  // must take its size from the grid (relative + flex-1/min-h-0, so it never
  // grows to fit its own content), and the chart must live inside an
  // `absolute inset-0` wrapper so the SVG's own rendered size can never feed
  // back into the box the observer is watching. jsdom has no layout engine,
  // so this can't assert real pixels the way a browser would - the
  // structural checks below (which element is observed, and how it and its
  // chart wrapper are positioned) are what rule the loop out.
  describe('measured box sizing', () => {
    afterEach(() => {
      vi.unstubAllGlobals()
      MockResizeObserver.instances = []
    })

    test('observes a relative, height-constrained container and sizes the chart from a resize entry, not its own content', () => {
      MockResizeObserver.instances = []
      vi.stubGlobal('ResizeObserver', MockResizeObserver)

      render(
        <SeaStateTile
          forecast={[day('2026-06-14')]}
          waveForecastDays={[waveDay('2026-06-14')]}
          waveLoading={false}
          waveError={null}
          units="metric"
        />,
      )

      expect(MockResizeObserver.instances).toHaveLength(1)
      const observer = MockResizeObserver.instances[0]!
      const measuredBox = observer.target as HTMLElement
      expect(measuredBox).not.toBeNull()

      // relative + flex-1/min-h-0 (at lg, gated by min-h-[160px] below it):
      // the box takes its height from the flex parent the grid sizes, never
      // from its own content.
      expect(measuredBox.classList.contains('relative')).toBe(true)
      expect(measuredBox.classList.contains('min-h-[160px]')).toBe(true)

      // The chart sits in its own absolutely-positioned wrapper inside the
      // measured box, so the SVG's own size has nothing left to push against.
      const chartWrapper = measuredBox.querySelector(':scope > .absolute.inset-0')
      expect(chartWrapper).not.toBeNull()
      expect(chartWrapper?.querySelector('svg.recharts-surface')).toBeInTheDocument()

      observer.fire({ width: 582, height: 250 })

      const svg = document.querySelector('svg.recharts-surface')
      expect(svg).toHaveAttribute('width', '582')
      expect(svg).toHaveAttribute('height', '250')
    })
  })
})
