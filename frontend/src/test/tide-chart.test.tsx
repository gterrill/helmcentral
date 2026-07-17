import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen } from '@testing-library/react'

import { TideChart } from '@/components/tide-chart'
import type { TideChart as TideChartData } from '@/hooks/use-tide-chart'

const STATION = {
  stationId: 'station-1',
  name: 'Test Harbor',
  state: 'CA',
  lat: 37.0,
  lon: -122.0,
  timezone: 'America/Los_Angeles',
}

function buildChart(overrides: Partial<TideChartData> = {}): TideChartData {
  return {
    station: STATION,
    extremes: [],
    currentHeightM: 0.9,
    direction: 'Rising',
    ...overrides,
  }
}

// All bounds constructed via the local-time Date constructor (not ISO 'Z'
// strings) so the window math and the "12AM/6AM/..." tick labels line up
// regardless of the machine's timezone the tests run under - exactly how
// ForecastTideSection will derive windowStart/windowEnd from the local clock.
function todayWindow() {
  return {
    windowStart: new Date(2026, 5, 14, 0, 0, 0),
    windowEnd: new Date(2026, 5, 15, 0, 0, 0),
  }
}

function tomorrowWindow() {
  return {
    windowStart: new Date(2026, 5, 15, 0, 0, 0),
    windowEnd: new Date(2026, 5, 16, 0, 0, 0),
  }
}

describe('TideChart', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 5, 14, 12, 0, 0))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows the unavailable message when there are no extremes at all', () => {
    const { windowStart, windowEnd } = todayWindow()
    render(
      <TideChart
        chart={buildChart({ extremes: [] })}
        isImperial={false}
        windowStart={windowStart}
        windowEnd={windowEnd}
      />,
    )

    expect(screen.getByTestId('forecast-tide-unavailable')).toBeInTheDocument()
    expect(screen.getByText('Tide forecast unavailable for this day')).toBeInTheDocument()
  })

  it('shows the unavailable message when extremes exist but none fall inside the given window', () => {
    const { windowStart, windowEnd } = todayWindow()
    render(
      <TideChart
        chart={buildChart({
          extremes: [
            { time: new Date(2026, 5, 20, 4, 0, 0).toISOString(), heightM: 1.8, high: true },
            { time: new Date(2026, 5, 20, 10, 0, 0).toISOString(), heightM: 0.3, high: false },
          ],
        })}
        isImperial={false}
        windowStart={windowStart}
        windowEnd={windowEnd}
      />,
    )

    expect(screen.getByTestId('forecast-tide-unavailable')).toBeInTheDocument()
  })

  it('does not throw when transitioning from no-data (early return) to a loaded chart', () => {
    const { windowStart, windowEnd } = todayWindow()
    const { rerender } = render(
      <TideChart chart={buildChart({ extremes: [] })} isImperial={false} windowStart={windowStart} windowEnd={windowEnd} />,
    )

    expect(screen.getByTestId('forecast-tide-unavailable')).toBeInTheDocument()

    expect(() => {
      rerender(
        <TideChart
          chart={buildChart({
            extremes: [
              { time: new Date(2026, 5, 14, 5, 0, 0).toISOString(), heightM: 1.8, high: true },
              { time: new Date(2026, 5, 14, 18, 0, 0).toISOString(), heightM: 0.3, high: false },
            ],
          })}
          isImperial={false}
          windowStart={windowStart}
          windowEnd={windowEnd}
        />,
      )
    }).not.toThrow()

    expect(screen.queryByTestId('forecast-tide-unavailable')).not.toBeInTheDocument()
  })

  it('shows the Now marker when the window contains the current time', () => {
    const { windowStart, windowEnd } = todayWindow()
    render(
      <TideChart
        chart={buildChart({
          extremes: [
            { time: new Date(2026, 5, 14, 5, 0, 0).toISOString(), heightM: 1.8, high: true },
            { time: new Date(2026, 5, 14, 18, 0, 0).toISOString(), heightM: 0.3, high: false },
          ],
        })}
        isImperial={false}
        windowStart={windowStart}
        windowEnd={windowEnd}
      />,
    )

    expect(screen.getByText('Now')).toBeInTheDocument()
  })

  it('hides the Now marker when the window does not contain the current time', () => {
    const { windowStart, windowEnd } = tomorrowWindow()
    render(
      <TideChart
        chart={buildChart({
          extremes: [
            { time: new Date(2026, 5, 15, 5, 0, 0).toISOString(), heightM: 1.8, high: true },
            { time: new Date(2026, 5, 15, 18, 0, 0).toISOString(), heightM: 0.3, high: false },
          ],
        })}
        isImperial={false}
        windowStart={windowStart}
        windowEnd={windowEnd}
      />,
    )

    expect(screen.queryByText('Now')).not.toBeInTheDocument()
  })

  it('shows 12AM/6AM/12PM/6PM hour ticks across the window', () => {
    const { windowStart, windowEnd } = todayWindow()
    render(
      <TideChart
        chart={buildChart({
          extremes: [
            { time: new Date(2026, 5, 14, 5, 0, 0).toISOString(), heightM: 1.8, high: true },
            { time: new Date(2026, 5, 14, 18, 0, 0).toISOString(), heightM: 0.3, high: false },
          ],
        })}
        isImperial={false}
        windowStart={windowStart}
        windowEnd={windowEnd}
      />,
    )

    // Ticks run 0/6/12/18/24h from window start, so "12AM" legitimately
    // appears twice (the window's start and end midnights).
    expect(screen.getAllByText('12AM').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText('6AM')).toBeInTheDocument()
    expect(screen.getByText('12PM')).toBeInTheDocument()
    expect(screen.getByText('6PM')).toBeInTheDocument()
  })

  // Pins the migration's trickiest piece: curvePoints/extreme markers are
  // precomputed in PIXEL space (not a real data domain) and handed to
  // recharts on an identity domain, so a recharts-rendered <ReferenceDot>
  // must land at the exact same literal pixel position this test computes
  // via the component's own xFor/yFor formulas - not some recharts-rescaled
  // position.
  it('places a high-tide ReferenceDot at its exact precomputed pixel position', () => {
    const { windowStart, windowEnd } = todayWindow()
    const highTime = new Date(2026, 5, 14, 5, 0, 0)
    const extremes = [
      { time: highTime.toISOString(), heightM: 1.8, high: true },
      { time: new Date(2026, 5, 14, 18, 0, 0).toISOString(), heightM: 0.3, high: false },
    ]

    const { container } = render(
      <TideChart
        chart={buildChart({ extremes })}
        isImperial={false}
        windowStart={windowStart}
        windowEnd={windowEnd}
      />,
    )

    // Reproduces the component's own xFor/yFor math exactly (same formula,
    // same inputs) so this compares against the true expected pixel, not a
    // hand-rounded approximation. jsdom's ResizeObserver stub never fires,
    // so viewportWidth falls back to DEFAULT_VIEWPORT_WIDTH (1000).
    const CHART_LEFT = 36
    const CHART_RIGHT = 1000 - 20
    const CHART_TOP = 16
    const CHART_BOTTOM = 125
    const chartStartMs = windowStart.getTime()
    const chartEndMs = windowEnd.getTime()
    const xFor = (t: number) => CHART_LEFT + ((t - chartStartMs) / (chartEndMs - chartStartMs)) * (CHART_RIGHT - CHART_LEFT)
    const heights = [1.8, 0.3, 0.9] // extremes' heights + buildChart's default currentHeightM
    const minHeight = Math.min(...heights)
    const maxHeight = Math.max(...heights)
    const padding = Math.max((maxHeight - minHeight) * 0.1, 0.1)
    const yMin = minHeight - padding
    const yMax = maxHeight + padding
    const yRange = yMax - yMin || 1
    const yFor = (v: number) => CHART_TOP + (1 - (v - yMin) / yRange) * (CHART_BOTTOM - CHART_TOP)

    const expectedX = xFor(highTime.getTime())
    const expectedY = yFor(1.8)

    const dots = Array.from(container.querySelectorAll('.recharts-reference-dot circle'))
    expect(dots.length).toBeGreaterThan(0)
    const highDot = dots.find((dot) => Math.abs(Number(dot.getAttribute('cx')) - expectedX) < 0.5)
    expect(highDot).toBeTruthy()
    expect(Number(highDot!.getAttribute('cy'))).toBeCloseTo(expectedY, 1)
  })

  // Regression test for a real bug found via browser-level verification:
  // curvePoints intentionally includes samples slightly outside
  // [0, viewportWidth]/[0, 175] whenever a curve segment spans a window
  // boundary (a raised-cosine segment between an out-of-window extreme and
  // an in-window one), for a smooth day-boundary - exactly like the original
  // hand-rolled <svg> did. Without `allowDataOverflow` on the identity-domain
  // XAxis/YAxis, recharts silently *expands* its domain to fit that overflow
  // (see parseSpecifiedDomain in recharts' ChartUtils.js) while the pixel
  // range stays fixed, compressing the scale for EVERY point - including
  // ones that were always safely inside the window. The previous test above
  // doesn't exercise this: both its extremes sit inside the window, so no
  // overflow ever occurs and it would pass even without the fix. This one
  // adds an extreme before the window and one after it specifically to
  // trigger the overflow, then checks the same in-window high tide still
  // lands at the exact same pixel as it would without those extra points.
  it('keeps an in-window ReferenceDot pixel-accurate even when other extremes fall outside the window', () => {
    const { windowStart, windowEnd } = todayWindow()
    const highTime = new Date(2026, 5, 14, 5, 0, 0)
    const extremes = [
      { time: new Date(2026, 5, 13, 22, 0, 0).toISOString(), heightM: 1.0, high: false }, // before window
      { time: highTime.toISOString(), heightM: 1.8, high: true }, // inside window - the one we check
      { time: new Date(2026, 5, 14, 18, 0, 0).toISOString(), heightM: 0.3, high: false }, // inside window
      { time: new Date(2026, 5, 15, 2, 0, 0).toISOString(), heightM: 1.2, high: true }, // after window
    ]

    const { container } = render(
      <TideChart
        chart={buildChart({ extremes })}
        isImperial={false}
        windowStart={windowStart}
        windowEnd={windowEnd}
      />,
    )

    const CHART_LEFT = 36
    const CHART_RIGHT = 1000 - 20
    const CHART_TOP = 16
    const CHART_BOTTOM = 125
    const chartStartMs = windowStart.getTime()
    const chartEndMs = windowEnd.getTime()
    const xFor = (t: number) => CHART_LEFT + ((t - chartStartMs) / (chartEndMs - chartStartMs)) * (CHART_RIGHT - CHART_LEFT)
    const heights = [1.0, 1.8, 0.3, 1.2, 0.9] // all 4 extremes' heights + buildChart's default currentHeightM
    const minHeight = Math.min(...heights)
    const maxHeight = Math.max(...heights)
    const padding = Math.max((maxHeight - minHeight) * 0.1, 0.1)
    const yMin = minHeight - padding
    const yMax = maxHeight + padding
    const yRange = yMax - yMin || 1
    const yFor = (v: number) => CHART_TOP + (1 - (v - yMin) / yRange) * (CHART_BOTTOM - CHART_TOP)

    const expectedX = xFor(highTime.getTime())
    const expectedY = yFor(1.8)

    const dots = Array.from(container.querySelectorAll('.recharts-reference-dot circle'))
    expect(dots.length).toBeGreaterThan(0)
    const highDot = dots.find((dot) => Math.abs(Number(dot.getAttribute('cx')) - expectedX) < 0.5)
    expect(highDot).toBeTruthy()
    expect(Number(highDot!.getAttribute('cy'))).toBeCloseTo(expectedY, 1)
  })

  // New assertion enabled by the real-data-domain refactor (previously the
  // curve was pre-baked entirely in pixel space, so there was no independent
  // "domain" to reason about at all): confirms the curve actually renders a
  // substantial, non-degenerate set of points across the window even when
  // extremes span both before AND after it (the boundary-straddling segment
  // scenario from the test above) - regardless of whether allowDataOverflow
  // ended up being needed for the boundary segments' out-of-domain samples.
  it('renders a continuous, non-degenerate tide curve when extremes span both before and after the window', () => {
    const { windowStart, windowEnd } = todayWindow()
    const extremes = [
      { time: new Date(2026, 5, 13, 22, 0, 0).toISOString(), heightM: 1.0, high: false }, // before window
      { time: new Date(2026, 5, 14, 5, 0, 0).toISOString(), heightM: 1.8, high: true }, // inside window
      { time: new Date(2026, 5, 14, 18, 0, 0).toISOString(), heightM: 0.3, high: false }, // inside window
      { time: new Date(2026, 5, 15, 2, 0, 0).toISOString(), heightM: 1.2, high: true }, // after window
    ]

    const { container } = render(
      <TideChart
        chart={buildChart({ extremes })}
        isImperial={false}
        windowStart={windowStart}
        windowEnd={windowEnd}
      />,
    )

    const curve = container.querySelector('path.recharts-curve[stroke="rgba(20,184,166,0.9)"]')
    expect(curve).toBeTruthy()
    const d = curve!.getAttribute('d') ?? ''
    expect(d).not.toBe('')
    expect(d).not.toMatch(/NaN/)
    const numbers = d.match(/-?\d+(\.\d+)?/g)?.map(Number) ?? []
    // 3 curve segments each producing up to CURVE_STEPS+1=13 points (the two
    // boundary segments get partially clipped by the chartStartMs/chartEndMs
    // guard) - well over 20 numbers confirms this is a real, non-empty path.
    expect(numbers.length).toBeGreaterThan(20)
  })

  // Only 2 extremes -> a single consecutive-pair -> the local
  // classifyTidePhase() heuristic always bails out to null (max range ==
  // min range with just one pair), so no badge would render from the
  // heuristic alone. Setting chart.tidalPhase proves it's preferred instead.
  it('prefers the backend tidal_phase field over the local heuristic when present', () => {
    const { windowStart, windowEnd } = todayWindow()
    const extremes = [
      { time: new Date(2026, 5, 14, 5, 0, 0).toISOString(), heightM: 1.8, high: true },
      { time: new Date(2026, 5, 14, 18, 0, 0).toISOString(), heightM: 0.3, high: false },
    ]

    const { rerender } = render(
      <TideChart
        chart={buildChart({ extremes })}
        isImperial={false}
        windowStart={windowStart}
        windowEnd={windowEnd}
      />,
    )
    // Without tidalPhase, the heuristic bails out - no badge.
    expect(screen.queryByText('Spring Tide')).not.toBeInTheDocument()
    expect(screen.queryByText('Neap Tide')).not.toBeInTheDocument()

    rerender(
      <TideChart
        chart={buildChart({ extremes, tidalPhase: 'springs+2' })}
        isImperial={false}
        windowStart={windowStart}
        windowEnd={windowEnd}
      />,
    )
    expect(screen.getByText('Spring Tide')).toBeInTheDocument()
  })

  // Extremes chosen so the local classifyTidePhase() heuristic (see
  // frontend/src/test/tide-phase.test.ts for the same math) resolves 'now'
  // (fixed to 2026-06-14T12:00:00 by beforeEach) into the widest-range pair
  // (t1 -> t2, range 1.9) relative to the narrowest (t0 -> t1, range 0.1).
  it('falls back to the local heuristic when tidal_phase is absent', () => {
    const { windowStart, windowEnd } = todayWindow()
    const extremes = [
      { time: new Date(2026, 5, 14, 0, 0, 0).toISOString(), heightM: 1.0, high: false },
      { time: new Date(2026, 5, 14, 5, 0, 0).toISOString(), heightM: 1.1, high: true },
      { time: new Date(2026, 5, 14, 18, 0, 0).toISOString(), heightM: 3.0, high: false },
    ]

    render(
      <TideChart
        chart={buildChart({ extremes })}
        isImperial={false}
        windowStart={windowStart}
        windowEnd={windowEnd}
      />,
    )

    expect(screen.getByText('Spring Tide')).toBeInTheDocument()
  })
})
