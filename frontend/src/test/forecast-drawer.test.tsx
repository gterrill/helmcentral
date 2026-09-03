import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { fireEvent, render, screen, within } from '@testing-library/react'

// Kept 100% fetch-free per this file's existing convention: ForecastTideSection
// owns its own data-fetching (tide settings + tide chart), so it's stubbed here
// rather than exercised end-to-end - that's covered by forecast-tide-section.test.tsx.
vi.mock('@/components/forecast-tide-section', () => ({
  ForecastTideSection: ({ dayOffset }: { isImperial: boolean; dayOffset: number }) => (
    <div data-testid="mock-forecast-tide-section" data-day-offset={dayOffset} />
  ),
}))

import { ForecastDrawer, formatRefreshAge } from '@/components/forecast-drawer'

const HOUR_LABELS = [
  '12AM', '1AM', '2AM', '3AM', '4AM', '5AM', '6AM', '7AM', '8AM', '9AM', '10AM', '11AM',
  '12PM', '1PM', '2PM', '3PM', '4PM', '5PM', '6PM', '7PM', '8PM', '9PM', '10PM', '11PM',
]

function buildHourlyWind(count = 24) {
  return Array.from({ length: count }, (_, idx) => ({
    label: HOUR_LABELS[idx % HOUR_LABELS.length],
    hourOfDay: idx % 24,
    windSpeed: 10 + idx,
    windGust: 15 + idx,
    windDirection: 'NE',
    windDirectionDeg: 45,
  }))
}

// Mirrors the backend's waveSteepness so fixtures stay internally consistent
// rather than carrying a ratio that contradicts their own height and period.
function steepnessFor(heightM: number, periodS: number) {
  if (periodS <= 0 || heightM < 0) return { steepnessRatio: null, steepnessBand: null }
  const ratio = heightM / ((9.80665 * periodS * periodS) / (2 * Math.PI))
  const band = ratio >= 0.1 ? 'breaking' : ratio >= 0.07 ? 'steep' : ratio >= 0.04 ? 'building' : 'rolling'
  return { steepnessRatio: ratio, steepnessBand: band as 'rolling' | 'building' | 'steep' | 'breaking' }
}

function buildHourlyWave(count = 24, periodS = 6, heightAt = (idx: number) => 1 + idx * 0.05) {
  return Array.from({ length: count }, (_, idx) => ({
    label: HOUR_LABELS[idx % HOUR_LABELS.length],
    hourOfDay: idx % 24,
    waveHeightM: heightAt(idx),
    wavePeriodS: periodS,
    waveDirectionDeg: 90,
    windWaveHeightM: 0.4 + idx * 0.02,
    swellWaveHeightM: 0.8 + idx * 0.03,
    windWaveDirectionDeg: 90,
    windWavePeriodS: periodS,
    swellWaveDirectionDeg: 95,
    swellWavePeriodS: periodS,
    ...steepnessFor(heightAt(idx), periodS),
  }))
}

function buildHourlyPrecip(count = 24) {
  return Array.from({ length: count }, (_, idx) => ({
    label: HOUR_LABELS[idx % HOUR_LABELS.length],
    hourOfDay: idx % 24,
    precipChancePct: (idx * 4) % 101,
    precipIntensityMm: idx % 6 === 0 ? 1.5 + idx * 0.1 : 0,
  }))
}

function buildHourlyUV(count = 24) {
  return Array.from({ length: count }, (_, idx) => ({
    label: HOUR_LABELS[idx % HOUR_LABELS.length],
    uvIndex: Math.max(0, Math.round(6 - Math.abs(idx - 12) * 0.6)),
  }))
}

function buildHourlyCloud(count = 24) {
  return Array.from({ length: count }, (_, idx) => ({
    label: HOUR_LABELS[idx % HOUR_LABELS.length],
    hourOfDay: idx % 24,
    condition: idx === 12 ? 'Clear' : idx % 6 === 0 ? 'Partly Cloudy' : 'Cloudy',
    // Smooth hump peaking at noon (idx 12) and lowest at midnight (idx 0) -
    // gives deterministic, easy-to-assert L/H marker positions.
    temperatureF: 60 + Math.round(10 * Math.sin((idx / 24) * Math.PI)),
    isDaylight: idx >= 6 && idx <= 18,
  }))
}

function buildDay(overrides: Record<string, unknown> = {}) {
  return {
    dayKey: '2026-06-14',
    date: 'Jun 14',
    dayName: 'Sunday',
    condition: 'Clear',
    high: 76,
    low: 62,
    windSpeed: 10,
    windGust: 14,
    windDirection: 'NE',
    windSummary: 'Winds 10 to 19 kts, with gusts up to 24 kts.',
    precipitationSummary: 'Slight chance of rain after 5PM.',
    precipitation: 5,
    sunriseTime: '6:32AM',
    sunsetTime: '5:47PM',
    moonPhase: 'waningCrescent',
    hourlyWind: buildHourlyWind(),
    hourlyPrecip: buildHourlyPrecip(),
    hourlyUV: buildHourlyUV(),
    hourlyCloud: buildHourlyCloud(),
    ...overrides,
  }
}

function buildWaveDay(overrides: Record<string, unknown> = {}) {
  return {
    dayKey: '2026-06-14',
    date: 'Jun 14',
    dayName: 'Sunday',
    waveSummary: 'Significant wave height 1.0 to 1.2 m from the E, with a period around 6 sec.',
    hourlyWave: buildHourlyWave(),
    indicators: { waveFront: false, rapidBuild: false, periodStep: false, crossSea: false },
    ...overrides,
  }
}

// Stage 1 of the design-token refactor (see docs/adr): every colour in this
// component must come from a CSS custom-property token (hsl(var(--...))) so
// the .dark and [data-skin="instrument"] themes can actually repaint it - a
// hand-typed rgb()/rgba()/hex literal always paints the same colour no matter
// which theme is active. This is a guard against regressions, not a one-time
// cleanup: it reads the component source directly off disk so any literal
// that creeps back in fails the suite immediately.
describe('ForecastDrawer design tokens', () => {
  it('contains no raw colour literals - only hsl(var(--...)) tokens', () => {
    const testDir = dirname(fileURLToPath(import.meta.url))
    const sourcePath = resolve(testDir, '../components/forecast-drawer.tsx')
    const source = readFileSync(sourcePath, 'utf8')
    const lines = source.split('\n')

    // hsl(var(--...)) is explicitly allowed and never matched by this pattern.
    const colorLiteralPattern = /rgb\(|rgba\(|#[0-9a-fA-F]{3,8}\b/g

    const offenders: string[] = []
    lines.forEach((line, idx) => {
      const matches = line.match(colorLiteralPattern)
      if (matches) {
        offenders.push(`  line ${idx + 1} (${matches.length}x): ${line.trim()}`)
      }
    })

    expect(
      offenders,
      `Found raw colour literal(s) in forecast-drawer.tsx - replace with hsl(var(--token)):\n${offenders.join('\n')}`,
    ).toEqual([])
  })
})

describe('ForecastDrawer refresh age', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-06-14T12:30:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('formats relative refresh age labels', () => {
    expect(formatRefreshAge('2026-06-14T12:30:00Z', new Date('2026-06-14T12:30:20Z').getTime())).toBe('just now')
    expect(formatRefreshAge('2026-06-14T12:29:00Z', new Date('2026-06-14T12:30:00Z').getTime())).toBe('1 min ago')
    expect(formatRefreshAge('2026-06-14T10:00:00Z', new Date('2026-06-14T12:30:00Z').getTime())).toBe('2 hours ago')
  })

  // isCached/updatedAt/ttlSeconds were declared on ForecastDrawerProps and
  // passed from App.tsx, but never destructured or rendered - so a forecast
  // served from the provider's stale-on-error cache looked identical to a
  // live one. The tide section already shows this; weather must too.
  it('shows the provider, cache state and refresh age for the forecast', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        loading={false}
        error={null}
        unit="metric"
        provider="weatherkit"
        isCached
        updatedAt="2026-06-14T10:00:00Z"
        ttlSeconds={900}
      />,
    )

    const meta = screen.getByTestId('forecast-refresh-meta')
    expect(meta).toHaveTextContent('cached')
    expect(meta).toHaveTextContent('2 hours ago')
    expect(meta).toHaveTextContent('weatherkit')
  })

  it('labels a freshly fetched forecast as live', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        loading={false}
        error={null}
        unit="metric"
        isCached={false}
        updatedAt="2026-06-14T12:30:00Z"
      />,
    )

    const meta = screen.getByTestId('forecast-refresh-meta')
    expect(meta).toHaveTextContent('live')
    expect(meta).toHaveTextContent('just now')
  })

  it('omits the refresh line entirely when no cache metadata is available', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    expect(screen.queryByTestId('forecast-refresh-meta')).not.toBeInTheDocument()
  })

  it('updates summary metrics when a day card is selected', () => {
    render(
      <ForecastDrawer
        forecast={[
          buildDay(),
          buildDay({
            date: 'Jun 15',
            dayName: 'Monday',
            condition: 'Cloudy',
            high: 68,
            low: 58,
            windSpeed: 22,
            windGust: 30,
            windDirection: 'SW',
            precipitation: 85,
          }),
        ]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getByTestId('forecast-selected-wind')).toHaveTextContent('10.0 kts')

    fireEvent.click(screen.getByRole('button', { name: /Select forecast day Monday Jun 15/i }))

    expect(screen.getByTestId('forecast-selected-wind')).toHaveTextContent('22.0 kts')
    expect(screen.getByTestId('forecast-selected-gust')).toHaveTextContent('30.0 kts')
  })

  it('renders the hourly strip with summary and sunset marker', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay({ condition: 'Mostly Sunny' })]}
        hourlyToday={[
          { label: 'Now', condition: 'Mostly Sunny', temperatureF: 72, windSpeedKts: 11, windGustKts: 18, windDirection: 'NE', windDirectionDeg: 45, kind: 'forecast' },
          { label: '11AM', condition: 'Mostly Sunny', temperatureF: 72, windSpeedKts: 12, windGustKts: 19, windDirection: 'NE', windDirectionDeg: 50, kind: 'forecast' },
          { label: '5:09PM', condition: 'Sunset', temperatureF: -1, windSpeedKts: -1, windGustKts: -1, windDirection: '—', windDirectionDeg: -1, kind: 'sunset' },
        ]}
        summary="Sunny conditions will continue all day."
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getByText('Sunny conditions will continue all day.')).toBeInTheDocument()
    expect(screen.getByText('Now')).toBeInTheDocument()
    expect(screen.getByText('11AM')).toBeInTheDocument()
    expect(screen.getByText('5:09PM')).toBeInTheDocument()
    expect(screen.getAllByText('Sunset').length).toBeGreaterThan(0)
    expect(screen.getByText('11kts NE')).toBeInTheDocument()
    expect(screen.getByText('12kts NE')).toBeInTheDocument()
  })

  it('renders up to 10 day tabs', () => {
    const days = Array.from({ length: 12 }, (_, idx) =>
      buildDay({
        date: `Jun ${14 + idx}`,
        dayName: 'Sunday',
        precipitation: idx,
      }),
    )

    render(<ForecastDrawer forecast={days} loading={false} error={null} unit="metric" />)

    const tabs = screen.getAllByRole('button', { name: /Select forecast day/i })
    expect(tabs).toHaveLength(10)
  })

  it('renders wind and wave graphs for the selected day', () => {
    render(<ForecastDrawer forecast={[buildDay()]} waveDays={[buildWaveDay()]} loading={false} error={null} unit="metric" />)

    expect(screen.getByTestId('forecast-wind-chart')).toBeInTheDocument()
    expect(screen.getByTestId('forecast-wave-chart')).toBeInTheDocument()
    expect(screen.queryByTestId('forecast-wave-unavailable')).not.toBeInTheDocument()
  })

  it('shows the wind summary sentence and direction barbs for the selected day', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    expect(screen.getByText('Winds 10 to 19 kts, with gusts up to 24 kts.')).toBeInTheDocument()
    expect(screen.getAllByTestId('forecast-wind-barb').length).toBeGreaterThan(0)
  })

  it('uses 6-hour-block labels on the wind chart', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    const chart = screen.getByTestId('forecast-wind-chart')
    expect(within(chart).getByText('12AM')).toBeInTheDocument()
    expect(within(chart).getByText('6AM')).toBeInTheDocument()
    expect(within(chart).getByText('12PM')).toBeInTheDocument()
    expect(within(chart).getByText('6PM')).toBeInTheDocument()
  })

  // Pins the recharts-backed wind series to the same solid-speed /
  // dashed-gust visual distinction the hand-rolled <polyline> pair had -
  // both are now <path class="recharts-curve"> elements (recharts never
  // renders <polyline>), distinguished by stroke color and dasharray.
  it('draws the wind speed line solid and the gust line dashed, in their existing colors', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    const chart = screen.getByTestId('forecast-wind-chart')
    const curves = Array.from(chart.querySelectorAll('path.recharts-curve'))
    const speedLine = curves.find((path) => path.getAttribute('stroke') === 'hsl(var(--chart-wind) / 0.95)')
    const gustLine = curves.find((path) => path.getAttribute('stroke') === 'hsl(var(--chart-gust) / 0.75)')
    expect(speedLine).toBeTruthy()
    expect(gustLine).toBeTruthy()
    expect(gustLine).toHaveAttribute('stroke-dasharray', '4 3')
  })

  // Regression test for a real bug found via browser-level verification: the
  // visible tick-label <XAxis> on every hourly chart reserves its own
  // default `height` (30px) via recharts' offset calculation IN ADDITION TO
  // the chart's own `margin.bottom` - shrinking the actual plot rectangle
  // below what windYFor (and therefore the tooltip marker and every other
  // manually-positioned overlay element) assumes, unless that reserved
  // height is subtracted back out of the margin. This can't be caught by
  // checking recharts' outer <svg> width/height (unaffected) - it has to
  // check the actual rendered curve geometry, parsed straight out of its
  // `d` attribute, against the same pixel math the manual overlay uses.
  it('renders the windSpeed curve at the exact pixel windYFor computes for the first hour', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    const chart = screen.getByTestId('forecast-wind-chart')
    const curve = chart.querySelector('path.recharts-curve[stroke="hsl(var(--chart-wind) / 0.95)"]')
    expect(curve).toBeTruthy()
    const d = curve!.getAttribute('d') ?? ''
    const firstPoint = /^M(-?[\d.]+),(-?[\d.]+)/.exec(d)
    expect(firstPoint).toBeTruthy()
    const [, xStr, yStr] = firstPoint!

    // Reproduces forecast-drawer.tsx's own margin-matching math: jsdom's
    // ResizeObserver stub never fires, so forecastChartWidth falls back to
    // 1000, giving hourlyChartLeft=30. buildHourlyWind(24) gives windSpeed
    // 10+idx (10 at idx 0) and windGust 15+idx (max 38 at idx 23), so
    // windDataMax=38 > 30, meaning windMax rounds up to 40.
    const hourlyChartLeft = 30
    const windChartTop = 35
    const windChartBottom = 125
    const windMax = 40
    const windYFor = (value: number) => windChartTop + (1 - value / windMax) * (windChartBottom - windChartTop)

    expect(Number(xStr)).toBeCloseTo(hourlyChartLeft, 0)
    expect(Number(yStr)).toBeCloseTo(windYFor(10), 0)
  })

  // Task 1 (visual polish): every forecast-drawer series should render as a
  // smooth monotone curve, not a linear/straight-segment polyline - d3-shape's
  // monotone interpolator (what recharts' type="monotone" uses) emits cubic
  // bezier "C" commands between points, whereas type="linear" only ever emits
  // "M"/"L" commands. Checking for a "C" command in the rendered `d` is a
  // reliable way to distinguish the two without depending on recharts'
  // internals - this fails under type="linear" and passes under "monotone".
  it('renders the windSpeed curve as a smooth (monotone) curve, not straight segments', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    const chart = screen.getByTestId('forecast-wind-chart')
    const curve = chart.querySelector('path.recharts-curve[stroke="hsl(var(--chart-wind) / 0.95)"]')
    expect(curve).toBeTruthy()
    const d = curve!.getAttribute('d') ?? ''
    expect(d).toMatch(/C/)
  })

  // Regression test for hourlyXFor's index/count-based bug: it computed pixel-x
  // from array INDEX and LENGTH, silently assuming the hourly array is always an
  // evenly-spaced 24-entry 0..23 sequence. But the chart's XAxis plots by the
  // entry's real hourOfDay value on a continuous domain={[0,23]} scale, not by
  // index/count — these only coincide today because the API always returns a
  // full 24-entry array. A deliberately non-uniform (3-entry, non-contiguous
  // hourOfDay) array exposes the bug: the old formula placed hourOfDay=12 (the
  // 3rd of 3 entries) at index 2 of 3 -> hourlyChartLeft + hourlyChartWidth (the
  // chart's far right edge), a clearly different pixel from the correct
  // hourOfDay-based position.
  it('positions a WindBarb by its real hourOfDay, not its array index, when the hourly array is non-uniform', () => {
    const sparseWind = [
      { label: '12AM', hourOfDay: 0, windSpeed: 1, windGust: 2, windDirection: 'NE', windDirectionDeg: 45 },
      { label: '6AM', hourOfDay: 6, windSpeed: 1, windGust: 2, windDirection: 'NE', windDirectionDeg: 45 },
      { label: '12PM', hourOfDay: 12, windSpeed: 1, windGust: 2, windDirection: 'NE', windDirectionDeg: 45 },
    ]

    render(<ForecastDrawer forecast={[buildDay({ hourlyWind: sparseWind })]} loading={false} error={null} unit="metric" />)

    const chart = screen.getByTestId('forecast-wind-chart')
    const barbs = within(chart).getAllByTestId('forecast-wind-barb')
    expect(barbs).toHaveLength(3)

    // hourlyChartLeft=30; jsdom's ResizeObserver stub never fires so
    // forecastChartWidth falls back to 1000, giving hourlyChartRight=980 and
    // hourlyChartWidth=950 (matches this file's existing windYFor test above).
    const hourlyChartLeft = 30
    const hourlyChartWidth = 950
    const expectedX = hourlyChartLeft + (12 / 23) * hourlyChartWidth
    // The old index-based hourlyXFor(idx, count) formula would have placed
    // hourOfDay=12 (the 3rd of 3 array entries) at idx=2, count=3 ->
    // hourlyChartLeft + (2*950)/(3-1) = hourlyChartLeft + hourlyChartWidth = 980,
    // the chart's far right edge - a clearly different pixel.
    const buggyIndexBasedX = hourlyChartLeft + hourlyChartWidth

    const thirdBarb = barbs[2]
    const actualX = Number(thirdBarb.getAttribute('cx'))
    expect(actualX).toBeCloseTo(expectedX, 0)
    expect(Math.abs(actualX - buggyIndexBasedX)).toBeGreaterThan(50)
  })

  it('shows the wave summary sentence, direction arrows and period for the selected day', () => {
    render(<ForecastDrawer forecast={[buildDay()]} waveDays={[buildWaveDay()]} loading={false} error={null} unit="metric" />)

    expect(screen.getByText('Significant wave height 1.0 to 1.2 m from the E, with a period around 6 sec.')).toBeInTheDocument()
    expect(screen.getAllByTestId('forecast-wave-arrow').length).toBeGreaterThan(0)
    expect(screen.getAllByText('6.0s').length).toBeGreaterThan(0)
  })

  it('includes sea surface temperature in the wave summary when available', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        waveDays={[buildWaveDay()]}
        waveSeaTemperatureF={70} // 70°F -> 21°C
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(
      screen.getByText('Significant wave height 1.0 to 1.2 m from the E, with a period around 6 sec and sea surface temperature of 21°C.'),
    ).toBeInTheDocument()
  })

  it('shows wave provider, cache state and refresh age for the wave card', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        waveDays={[buildWaveDay()]}
        loading={false}
        error={null}
        unit="metric"
        waveProvider="open-meteo-marine"
        waveIsCached
        waveUpdatedAt="2026-06-14T10:00:00Z"
        waveTtlSeconds={3600}
      />,
    )

    const meta = screen.getByTestId('forecast-wave-refresh-meta')
    expect(meta).toHaveTextContent('open-meteo-marine')
    expect(meta).toHaveTextContent('cached')
    expect(meta).toHaveTextContent('2 hours ago')
    expect(meta).toHaveTextContent('refreshes every 1h')
  })

  it('uses 6-hour-block labels on the wave chart', () => {
    render(<ForecastDrawer forecast={[buildDay()]} waveDays={[buildWaveDay()]} loading={false} error={null} unit="metric" />)

    const chart = screen.getByTestId('forecast-wave-chart')
    expect(within(chart).getByText('12AM')).toBeInTheDocument()
    expect(within(chart).getByText('6AM')).toBeInTheDocument()
    expect(within(chart).getByText('12PM')).toBeInTheDocument()
    expect(within(chart).getByText('6PM')).toBeInTheDocument()
  })

  // Pins the recharts-backed wave series (total/wind-wave/swell) to the same
  // three-way solid/dashed/dotted-dash visual distinction the hand-rolled
  // <polyline> trio had, now as <path class="recharts-curve"> elements.
  it('draws the total wave height solid and the wind-wave/swell lines dashed, in their existing colors', () => {
    render(<ForecastDrawer forecast={[buildDay()]} waveDays={[buildWaveDay()]} loading={false} error={null} unit="metric" />)

    const chart = screen.getByTestId('forecast-wave-chart')
    const curves = Array.from(chart.querySelectorAll('path.recharts-curve'))
    // The total-wave line is stroked with the steepness gradient rather than a
    // flat colour now, so it is identified by being the solid one.
    const waveLine = curves.find(
      (path) => path.getAttribute('stroke')?.startsWith('url(#') && !path.getAttribute('stroke-dasharray'),
    )
    const windWaveLine = curves.find((path) => path.getAttribute('stroke') === 'hsl(var(--chart-gust) / 0.85)')
    const swellLine = curves.find((path) => path.getAttribute('stroke') === 'hsl(var(--chart-swell) / 0.85)')
    expect(waveLine).toBeTruthy()
    expect(windWaveLine).toBeTruthy()
    expect(swellLine).toBeTruthy()
    expect(windWaveLine).toHaveAttribute('stroke-dasharray', '4 3')
    expect(swellLine).toHaveAttribute('stroke-dasharray', '2 3')
  })

  it('shows sunrise, sunset and moon phase for the selected day', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    expect(screen.getByText('6:32AM')).toBeInTheDocument()
    expect(screen.getByText('5:47PM')).toBeInTheDocument()
    expect(screen.getByText('Waning Crescent')).toBeInTheDocument()
  })

  it('omits the wind summary when not provided', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay({ windSummary: null })]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.queryByText(/winds will be/)).not.toBeInTheDocument()
  })

  it('shows an unavailable message when a day has no wave forecast (real no-data state, not an error)', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        waveDays={[buildWaveDay({ hourlyWave: [] })]}
        loading={false}
        error={null}
        waveLoading={false}
        waveError={null}
        unit="metric"
      />,
    )

    expect(screen.getByTestId('forecast-wind-chart')).toBeInTheDocument()
    expect(screen.queryByTestId('forecast-wave-chart')).not.toBeInTheDocument()
    expect(screen.getByTestId('forecast-wave-unavailable')).toBeInTheDocument()
    expect(screen.queryByTestId('forecast-wave-error')).not.toBeInTheDocument()
  })

  it('shows a loading indicator in the wave section while waves are still loading', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        waveDays={[]}
        loading={false}
        error={null}
        waveLoading
        waveError={null}
        unit="metric"
      />,
    )

    expect(screen.getByTestId('forecast-wave-loading')).toBeInTheDocument()
    expect(screen.queryByTestId('forecast-wave-chart')).not.toBeInTheDocument()
    expect(screen.queryByTestId('forecast-wave-unavailable')).not.toBeInTheDocument()
    expect(screen.queryByTestId('forecast-wave-error')).not.toBeInTheDocument()
  })

  it('shows a visible "Wave data unavailable" message when the wave fetch fails outright, distinct from the no-data state', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        waveDays={[]}
        loading={false}
        error={null}
        waveLoading={false}
        waveError="HTTP error! status: 502"
        unit="metric"
      />,
    )

    expect(screen.getByTestId('forecast-wave-error')).toBeInTheDocument()
    expect(screen.getByText('Wave data unavailable')).toBeInTheDocument()
    expect(screen.queryByTestId('forecast-wave-chart')).not.toBeInTheDocument()
    expect(screen.queryByTestId('forecast-wave-unavailable')).not.toBeInTheDocument()
  })

  it('still renders real wave data for the selected day even when waveError is set, as long as a matching day was found (stale-but-present data)', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        waveDays={[buildWaveDay()]}
        loading={false}
        error={null}
        waveLoading={false}
        waveError="HTTP error! status: 502"
        unit="metric"
      />,
    )

    expect(screen.getByTestId('forecast-wave-chart')).toBeInTheDocument()
    expect(screen.queryByTestId('forecast-wave-error')).not.toBeInTheDocument()
  })

  it('renders the cloud, temperature, and precipitation graph with bars for the selected day', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    expect(screen.getByTestId('forecast-cloud-chart')).toBeInTheDocument()
    expect(screen.getAllByTestId('forecast-precip-bar').length).toBeGreaterThan(0)
  })

  // -1 from the backend means "the provider reported no precipitation data",
  // which the hook maps to null. Rendering that as "0%" is what told the crew
  // there was no chance of rain while it was drizzling on deck - it must read
  // as unavailable instead.
  it('shows a dash instead of 0% when the precipitation chance is unavailable', () => {
    render(<ForecastDrawer forecast={[buildDay({ precipitation: null })]} loading={false} error={null} unit="metric" />)

    const precip = screen.getByTestId('forecast-selected-precip')
    expect(precip).toHaveTextContent('—')
    expect(precip).not.toHaveTextContent('0%')
  })

  it('still shows 0% when the provider genuinely forecasts no rain', () => {
    render(<ForecastDrawer forecast={[buildDay({ precipitation: 0 })]} loading={false} error={null} unit="metric" />)

    expect(screen.getByTestId('forecast-selected-precip')).toHaveTextContent('0%')
  })

  it('does not derive humidity or visibility from an unavailable precipitation chance', () => {
    render(<ForecastDrawer forecast={[buildDay({ precipitation: null })]} loading={false} error={null} unit="metric" />)

    // Both are computed from precipitation; with no precipitation reading
    // they would otherwise render a fabricated but plausible number.
    expect(screen.getByTestId('forecast-selected-humidity')).toHaveTextContent('—')
    expect(screen.getByTestId('forecast-selected-visibility')).toHaveTextContent('—')
  })

  it('shows the precipitation summary sentence for the selected day', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    expect(screen.getByText('Slight chance of rain after 5PM.')).toBeInTheDocument()
  })

  // Pins the recharts-backed bar rendering: each bar is colored by its probability gradient
  it('renders one precipitation bar per non-zero hourly reading, colored by probability gradient', () => {
    const hourlyPrecip = buildHourlyPrecip().map((entry, idx) => (idx === 18 ? { ...entry, precipIntensityMm: 8.2, precipChancePct: 90 } : entry))

    render(
      <ForecastDrawer
        forecast={[buildDay({ hourlyPrecip })]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const bars = screen.getAllByTestId('forecast-precip-bar')
    // buildHourlyPrecip gives non-zero intensity every 6th hour (4 of 24: idx 0, 6, 12, 18).
    expect(bars).toHaveLength(4)
    // idx 0 -> chance = 0% -> hsl(var(--chart-precip) / 0.2)
    expect(bars[0]).toHaveAttribute('fill', 'hsl(var(--chart-precip) / 0.2)')
    // idx 18 -> chance = 90% -> hsl(var(--chart-precip) / 0.92)
    expect(bars[3]).toHaveAttribute('fill', 'hsl(var(--chart-precip) / 0.92)')
  })

  it('shows UV as a background gradient and displays sun protection recommendation', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    const chart = screen.getByTestId('forecast-cloud-chart')
    expect(screen.getByText('Sun protection recommended from 7AM to 5PM.')).toBeInTheDocument()
    const uvAreaFills = chart.querySelectorAll('path.recharts-area-area')
    // One for UV area background and one for temperature area
    expect(uvAreaFills.length).toBe(2)
    expect(Array.from(uvAreaFills).every((fill) => fill.getAttribute('fill')?.startsWith('url(#'))).toBe(true)
  })

  // Task 3 (visual polish): the Wind/Wave/Temperature Area fills should fade
  // from a visible tint at the curve down to nearly transparent at the
  // baseline, instead of a single flat semi-transparent fill - implemented as
  // a <linearGradient>, so the rendered <Area>'s fill path should reference
  // one via url(#...) rather than a literal color.
  it('fills the wind Area with a vertical gradient instead of a flat color', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    const chart = screen.getByTestId('forecast-wind-chart')
    const areaFill = chart.querySelector('path.recharts-area-area')
    expect(areaFill).toBeTruthy()
    expect(areaFill!.getAttribute('fill')).toMatch(/^url\(#/)
  })

  it('fills the wave Area with a vertical gradient instead of a flat color', () => {
    render(<ForecastDrawer forecast={[buildDay()]} waveDays={[buildWaveDay()]} loading={false} error={null} unit="metric" />)

    const chart = screen.getByTestId('forecast-wave-chart')
    const areaFill = chart.querySelector('path.recharts-area-area')
    expect(areaFill).toBeTruthy()
    expect(areaFill!.getAttribute('fill')).toMatch(/^url\(#/)
  })

  it('fills the temperature Area with a vertical gradient instead of a flat color', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    const chart = screen.getByTestId('forecast-cloud-chart')
    const areaFill = chart.querySelector('path.recharts-area-area')
    expect(areaFill).toBeTruthy()
    expect(areaFill!.getAttribute('fill')).toMatch(/^url\(#/)
  })

  // Task 4 (visual polish): all five forecast charts should share the same
  // faint horizontal-gridline convention (rather than CartesianGrid appearing
  // on only the Cloud & Temperature chart, and only as vertical dividers).
  it.each([
    ['forecast-wind-chart'],
    ['forecast-wave-chart'],
    ['forecast-cloud-chart'],
  ])('renders faint horizontal gridlines using the chart-grid token on %s', (testId) => {
    render(<ForecastDrawer forecast={[buildDay()]} waveDays={[buildWaveDay()]} loading={false} error={null} unit="metric" />)

    const chart = screen.getByTestId(testId)
    const horizontalLines = Array.from(chart.querySelectorAll('.recharts-cartesian-grid-horizontal line'))
    expect(horizontalLines.length).toBeGreaterThan(0)
    expect(horizontalLines.every((line) => line.getAttribute('stroke') === 'hsl(var(--chart-grid) / 0.12)')).toBe(true)

    // Cloud & Temperature used to render vertical dividers instead - confirm
    // it (and every other chart) no longer renders any vertical grid lines,
    // now that the whole family shares the horizontal-only convention.
    const verticalLines = Array.from(chart.querySelectorAll('.recharts-cartesian-grid-vertical line'))
    expect(verticalLines.length).toBe(0)
  })

  // Task 5 (visual polish): the wind/wave/cloud legends used to fake a solid
  // vs. dashed line with text characters (an em-dash / hyphens), which don't
  // track the real series' color or strokeDasharray. They should now draw a
  // real inline <svg><line> swatch instead.
  it('draws real solid/dashed line swatches in the wind legend, not text-character fakes', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    const legend = screen.getByTestId('forecast-wind-legend')
    expect(legend.textContent).not.toMatch(/[—-] ?- ?Gusts|— Wind/)

    const swatchLines = Array.from(legend.querySelectorAll('svg line'))
    const windSwatch = swatchLines.find((line) => line.getAttribute('stroke') === 'hsl(var(--chart-wind) / 0.95)')
    const gustSwatch = swatchLines.find((line) => line.getAttribute('stroke') === 'hsl(var(--chart-gust) / 0.75)')
    expect(windSwatch).toBeTruthy()
    expect(windSwatch).not.toHaveAttribute('stroke-dasharray')
    expect(gustSwatch).toBeTruthy()
    expect(gustSwatch).toHaveAttribute('stroke-dasharray', '4 3')
    expect(legend).toHaveTextContent('barbs show direction the wind is coming from')
  })

  it('draws real solid/dashed line swatches in the wave legend, not text-character fakes', () => {
    render(<ForecastDrawer forecast={[buildDay()]} waveDays={[buildWaveDay()]} loading={false} error={null} unit="metric" />)

    const legend = screen.getByTestId('forecast-wave-legend')
    const swatchLines = Array.from(legend.querySelectorAll('svg line'))
    const waveSwatch = swatchLines.find((line) => line.getAttribute('stroke') === 'hsl(var(--chart-wave) / 0.9)')
    const chopSwatch = swatchLines.find((line) => line.getAttribute('stroke') === 'hsl(var(--chart-gust) / 0.85)')
    const swellSwatch = swatchLines.find((line) => line.getAttribute('stroke') === 'hsl(var(--chart-swell) / 0.85)')
    expect(waveSwatch).toBeTruthy()
    expect(waveSwatch).not.toHaveAttribute('stroke-dasharray')
    expect(chopSwatch).toBeTruthy()
    expect(chopSwatch).toHaveAttribute('stroke-dasharray', '4 3')
    expect(swellSwatch).toBeTruthy()
    expect(swellSwatch).toHaveAttribute('stroke-dasharray', '2 3')
  })

  it('draws a real line swatch and bar swatch in the cloud & temperature legend', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    const legend = screen.getByTestId('forecast-cloud-legend')
    expect(legend.querySelector('svg rect')).toBeTruthy()
    const swatchLines = Array.from(legend.querySelectorAll('svg line'))
    const tempSwatch = swatchLines.find((line) => line.getAttribute('stroke') === 'hsl(var(--chart-temp) / 0.9)')
    expect(tempSwatch).toBeTruthy()
  })

  // Task 6 (visual polish): the topmost Y-axis tick on the Wind/Wave/
  // charts used to append its unit only on that one tick (e.g.
  // "40 kts"), which can run past the plot's left gutter at fontSize 10. The
  // unit now lives in the chart's <h4> header instead, and every tick
  // (including the topmost) renders as a bare number.
  it('moves the wind chart max-tick unit into the header instead of the tick label', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    expect(screen.getByText('Wind (kts)')).toBeInTheDocument()
    const chart = screen.getByTestId('forecast-wind-chart')
    // windDataMax=38 > 30 -> windMax rounds up to 40 (same fixture math as
    // the pixel-exactness test above).
    const topTick = Array.from(chart.querySelectorAll('text')).find((el) => el.textContent === '40')
    expect(topTick).toBeTruthy()
    expect(Array.from(chart.querySelectorAll('text')).some((el) => /kts/.test(el.textContent ?? ''))).toBe(false)
  })

  it('moves the wave chart max-tick unit into the header instead of the tick label', () => {
    render(<ForecastDrawer forecast={[buildDay()]} waveDays={[buildWaveDay()]} loading={false} error={null} unit="metric" />)

    expect(screen.getByText('Wave (m)')).toBeInTheDocument()
    const chart = screen.getByTestId('forecast-wave-chart')
    expect(Array.from(chart.querySelectorAll('text')).some((el) => /\bm\b/.test(el.textContent ?? ''))).toBe(false)
  })

  it('still renders the cloud & temperature chart when a day has no UV forecast', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay({ hourlyUV: [] })]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getByTestId('forecast-cloud-chart')).toBeInTheDocument()
    expect(screen.getByText('No sun protection needed.')).toBeInTheDocument()
  })

  it('shows no sun protection needed when UV stays low all day', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay({ hourlyUV: buildHourlyUV().map((entry) => ({ ...entry, uvIndex: 1 })) })]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getByText('No sun protection needed.')).toBeInTheDocument()
  })

  it('renders the cloud & temperature graph with 6-hour-block labels and condition icons', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    const chart = screen.getByTestId('forecast-cloud-chart')
    // 6-hour-block ticks per the requested layout, not the dynamic spacing
    // the other hourly charts use.
    expect(within(chart).getByText('12AM')).toBeInTheDocument()
    expect(within(chart).getByText('6AM')).toBeInTheDocument()
    expect(within(chart).getByText('12PM')).toBeInTheDocument()
    expect(within(chart).getByText('6PM')).toBeInTheDocument()
    // L/H markers at the coldest (midnight) and warmest (noon) points.
    expect(within(chart).getByText('L')).toBeInTheDocument()
    expect(within(chart).getByText('H')).toBeInTheDocument()
  })

  it('does not throw when transitioning from loading (early return) to a loaded forecast', () => {
    const { rerender } = render(
      <ForecastDrawer forecast={[]} loading unit="metric" />,
    )

    expect(screen.getByText('Loading latest marine forecast...')).toBeInTheDocument()

    expect(() => {
      rerender(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)
    }).not.toThrow()

    expect(screen.getByTestId('forecast-wind-chart')).toBeInTheDocument()
  })

  it('shows an unavailable message when a day has no cloud/temperature forecast', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay({ hourlyCloud: [] })]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getByTestId('forecast-wind-chart')).toBeInTheDocument()
    expect(screen.queryByTestId('forecast-cloud-chart')).not.toBeInTheDocument()
    expect(screen.getByTestId('forecast-cloud-unavailable')).toBeInTheDocument()
  })

  it('renders the tide section as the last card, passing the currently selected day index', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay(), buildDay({ date: 'Jun 15', dayName: 'Monday' })]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getByTestId('mock-forecast-tide-section')).toHaveAttribute('data-day-offset', '0')

    fireEvent.click(screen.getByRole('button', { name: /Select forecast day Monday Jun 15/i }))

    expect(screen.getByTestId('mock-forecast-tide-section')).toHaveAttribute('data-day-offset', '1')
  })
})


describe('ForecastDrawer wave steepness', () => {
  // A day that never leaves the rolling band paints as the ordinary wave
  // colour. Escalation colour is reserved for seas worth escalating about.
  it('paints no alert colour on a rolling day', () => {
    render(<ForecastDrawer forecast={[buildDay()]} waveDays={[buildWaveDay()]} loading={false} error={null} unit="metric" />)

    const bands = screen.getAllByTestId('forecast-wave-steepness-stop').map((el) => el.getAttribute('data-band'))
    expect(bands.length).toBeGreaterThan(0)
    expect(new Set(bands)).toEqual(new Set(['rolling']))
  })

  it('escalates the wave line colour once seas reach the breaking band', () => {
    // 4m at 5s is 1:9, past the book's real-world 1-in-10 breaking slope.
    const breaking = buildWaveDay({ hourlyWave: buildHourlyWave(24, 5, () => 4) })
    render(<ForecastDrawer forecast={[buildDay()]} waveDays={[breaking]} loading={false} error={null} unit="metric" />)

    const bands = screen.getAllByTestId('forecast-wave-steepness-stop').map((el) => el.getAttribute('data-band'))
    expect(bands).toContain('breaking')
  })

  // The light-theme wave and gust colours sit under 3:1 against the card, so
  // the band must never be carried by colour alone - the ratio is written out
  // beside every direction arrow.
  it('writes the steepness ratio as text, not colour alone', () => {
    const breaking = buildWaveDay({ hourlyWave: buildHourlyWave(24, 5, () => 4) })
    render(<ForecastDrawer forecast={[buildDay()]} waveDays={[breaking]} loading={false} error={null} unit="metric" />)

    expect(screen.getAllByText('1:9').length).toBeGreaterThan(0)
  })
})

describe('ForecastDrawer wave leading indicators', () => {
  it('shows nothing when the day trips no indicator', () => {
    render(<ForecastDrawer forecast={[buildDay()]} waveDays={[buildWaveDay()]} loading={false} error={null} unit="metric" />)
    expect(screen.queryByTestId('forecast-wave-indicators')).not.toBeInTheDocument()
  })

  it('names each tripped indicator in words', () => {
    const day = buildWaveDay({ indicators: { waveFront: true, rapidBuild: false, periodStep: true, crossSea: false } })
    render(<ForecastDrawer forecast={[buildDay()]} waveDays={[day]} loading={false} error={null} unit="metric" />)

    const panel = screen.getByTestId('forecast-wave-indicators')
    expect(within(panel).getByText(/Sea building/i)).toBeInTheDocument()
    expect(within(panel).getByText(/Period lengthening/i)).toBeInTheDocument()
    expect(within(panel).queryByText(/danger signal/i)).not.toBeInTheDocument()
  })

  // Page 243: the highest wave runs 1.87 times the significant height, so a
  // forecast read as its headline number understates what you will meet.
  it('shows the largest likely wave alongside the significant height', () => {
    const day = buildWaveDay({ hourlyWave: buildHourlyWave(24, 8, () => 3) })
    render(<ForecastDrawer forecast={[buildDay()]} waveDays={[day]} loading={false} error={null} unit="metric" />)

    expect(screen.getByTestId('forecast-wave-largest')).toHaveTextContent('5.6 m')
  })

  // Page 233: wave heights do not normally exceed 0.8 times the wind in knots.
  // Past that the sea is not the local wind's doing.
  it('flags a sea that outruns the wind that could have built it', () => {
    // 4m is 13.1ft; 0.8 x 10kt of wind allows 8ft. Well past the ceiling.
    // The ceiling uses the day's peak hourly wind, so that is what to pin.
    const flatWind = buildHourlyWind().map((entry) => ({ ...entry, windSpeed: 10, windGust: 12 }))
    const day = buildWaveDay({ hourlyWave: buildHourlyWave(24, 8, () => 4) })
    render(<ForecastDrawer forecast={[buildDay({ hourlyWind: flatWind })]} waveDays={[day]} loading={false} error={null} unit="metric" />)

    expect(screen.getByTestId('forecast-wave-indicators')).toHaveTextContent(/outrunning the wind/i)
  })

  it('does not flag a sea the wind accounts for', () => {
    // 1.2m is 3.9ft, comfortably under 0.8 x 30kt.
    const day = buildWaveDay({ hourlyWave: buildHourlyWave(24, 8, () => 1.2) })
    render(<ForecastDrawer forecast={[buildDay({ windSpeed: 30, windGust: 35 })]} waveDays={[day]} loading={false} error={null} unit="metric" />)

    expect(screen.queryByText(/outrunning the wind/i)).not.toBeInTheDocument()
  })

  // Page 310: "Small changes, sometimes as little as 3 or 4 degrees Fahrenheit
  // (1 or 2 degrees Celsius), can have a major impact."
  it('flags cold air arriving over warmer water', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay({ low: 50 })]}
        waveDays={[buildWaveDay()]}
        waveSeaTemperatureF={72}
        loading={false}
        error={null}
        unit="metric"
      />,
    )
    expect(screen.getByTestId('forecast-wave-indicators')).toHaveTextContent(/cold air over warmer water/i)
  })
})

function buildUpperAirDay(dayKey: string, overrides: Record<string, unknown> = {}) {
  return {
    dayKey,
    date: 'Sep 12',
    dayName: 'Saturday',
    outlook: {
      present: true,
      height500M: 5835,
      thicknessM: 5645,
      peakWind500Kts: 33.9,
      heightPercentile: 0.07,
      tendency24hM: -23.1,
      troughSupport: false,
      ...overrides,
    },
  }
}

describe('ForecastDrawer upper air', () => {
  it('shows nothing at all when no upper-air provider is installed', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    expect(screen.queryByTestId('forecast-upper-air-marker')).not.toBeInTheDocument()
    expect(screen.queryByTestId('forecast-upper-air-detail')).not.toBeInTheDocument()
  })

  // An upper-air section that renders "clear" when there is no data would be
  // a reassurance nobody measured.
  it('shows nothing for a day the provider had no data for', () => {
    const day = buildUpperAirDay('2026-06-14', { present: false })
    render(<ForecastDrawer forecast={[buildDay()]} upperAirDays={[day]} loading={false} error={null} unit="metric" />)

    expect(screen.queryByTestId('forecast-upper-air-marker')).not.toBeInTheDocument()
    expect(screen.queryByTestId('forecast-upper-air-detail')).not.toBeInTheDocument()
  })

  it('marks only the day whose upper air supports development', () => {
    const days = [
      buildUpperAirDay('2026-06-14', { troughSupport: false }),
      buildUpperAirDay('2026-06-15', { troughSupport: true }),
    ]
    render(
      <ForecastDrawer
        forecast={[buildDay(), buildDay({ dayKey: '2026-06-15', date: 'Jun 15', dayName: 'Monday' })]}
        upperAirDays={days}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getAllByTestId('forecast-upper-air-marker')).toHaveLength(1)
  })

  // The card is 150px and already carries five lines, so the reasoning goes
  // in the expanded panel where there is room to say it in words.
  it('explains the finding in the expanded panel', () => {
    const day = buildUpperAirDay('2026-06-14', { troughSupport: true })
    render(<ForecastDrawer forecast={[buildDay()]} upperAirDays={[day]} loading={false} error={null} unit="metric" />)

    const detail = screen.getByTestId('forecast-upper-air-detail')
    expect(detail).toHaveTextContent(/500mb/i)
    expect(detail).toHaveTextContent(/lowest/i)
  })

  // A quiet day still gets its numbers in the panel, just no marker and no
  // claim about development.
  it('reports the numbers on a quiet day without claiming a trough', () => {
    const day = buildUpperAirDay('2026-06-14', { troughSupport: false, heightPercentile: 0.8, tendency24hM: 5 })
    render(<ForecastDrawer forecast={[buildDay()]} upperAirDays={[day]} loading={false} error={null} unit="metric" />)

    const detail = screen.getByTestId('forecast-upper-air-detail')
    expect(detail).toHaveTextContent('5835')
    expect(detail).not.toHaveTextContent(/support/i)
  })
})

function buildUpperAirSeries(dayKeys: string[], heightAt: (idx: number) => number) {
  const samples: Array<{
    time: string
    dayKey: string
    localHour: number
    height500M: number
    thicknessM: number
    wind500Kts: number
    temperature500C: number
  }> = []
  dayKeys.forEach((dayKey, dayIdx) => {
    for (let block = 0; block < 4; block += 1) {
      const idx = dayIdx * 4 + block
      samples.push({
        time: `${dayKey}T${String(block * 6).padStart(2, '0')}:00:00Z`,
        dayKey,
        localHour: block * 6,
        height500M: heightAt(idx),
        thicknessM: 5645,
        wind500Kts: 30,
        temperature500C: -8,
      })
    }
  })
  return samples
}

const UPPER_AIR_WINDOW = { present: true, lowM: 5835, highM: 5907, lowQuintileM: 5851 }

describe('ForecastDrawer upper-air trace', () => {
  // The complaint this chart answers: a single day's "500mb 5899 m" cannot be
  // correlated against anything. The book's method is reading the shape across
  // successive charts, so the window has to be drawn as a window.
  it('draws the trace when a series is present', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        upperAirDays={[buildUpperAirDay('2026-06-14')]}
        upperAirSeries={buildUpperAirSeries(['2026-06-14', '2026-06-15'], (idx) => 5900 - idx * 4)}
        upperAirWindow={UPPER_AIR_WINDOW}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getByTestId('forecast-upper-air-chart')).toBeInTheDocument()
  })

  // Upper air is optional. A boat with no plugin must not get an empty chart
  // frame that reads as "nothing happening up there".
  it('draws no chart at all without a series', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        upperAirDays={[buildUpperAirDay('2026-06-14')]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.queryByTestId('forecast-upper-air-chart')).not.toBeInTheDocument()
  })

  // The band is the whole point of the percentile: a day is judged against the
  // rest of the window, so the window's range has to be visible on the chart
  // rather than asserted in a sentence.
  it('labels the range the window is judged against', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        upperAirDays={[buildUpperAirDay('2026-06-14')]}
        upperAirSeries={buildUpperAirSeries(['2026-06-14', '2026-06-15'], (idx) => 5900 - idx * 4)}
        upperAirWindow={UPPER_AIR_WINDOW}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const legend = screen.getByTestId('forecast-upper-air-legend')
    expect(legend).toHaveTextContent('5835')
    expect(legend).toHaveTextContent('5907')
  })

  // Surviving the Storm's claim is causal and lagged: the upper trough vents
  // the surface low. Putting surface wind on the same axis is what makes that
  // lead-lag readable instead of asserted.
  it('carries the surface wind alongside the upper trace', () => {
    render(
      <ForecastDrawer
        forecast={[
          buildDay({ windGust: 33 }),
          buildDay({ dayKey: '2026-06-15', date: 'Jun 15', dayName: 'Monday', windGust: 41 }),
        ]}
        upperAirDays={[buildUpperAirDay('2026-06-14'), buildUpperAirDay('2026-06-15')]}
        upperAirSeries={buildUpperAirSeries(['2026-06-14', '2026-06-15'], (idx) => 5900 - idx * 4)}
        upperAirWindow={UPPER_AIR_WINDOW}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getByTestId('forecast-upper-air-legend')).toHaveTextContent(/surface gust/i)
  })

  // Days the outlook marked have to read as an extent in time (approach,
  // bottom, recovery) rather than as a badge on one card.
  it('shades the days whose upper air supports development', () => {
    render(
      <ForecastDrawer
        forecast={[
          buildDay(),
          buildDay({ dayKey: '2026-06-15', date: 'Jun 15', dayName: 'Monday' }),
        ]}
        upperAirDays={[
          buildUpperAirDay('2026-06-14', { troughSupport: false }),
          buildUpperAirDay('2026-06-15', { troughSupport: true }),
        ]}
        upperAirSeries={buildUpperAirSeries(['2026-06-14', '2026-06-15'], (idx) => 5900 - idx * 4)}
        upperAirWindow={UPPER_AIR_WINDOW}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getAllByTestId('forecast-upper-air-trough-band')).toHaveLength(1)
  })
})

describe('ForecastDrawer upper-air trough bands', () => {
  // Two flagged days in a row are one stretch of weather, not two. Drawing
  // each band only as far as its own last sample leaves a visible gap between
  // them, which reads as the trough letting up in the middle.
  it('joins consecutive flagged days into one continuous band', () => {
    render(
      <ForecastDrawer
        forecast={[
          buildDay(),
          buildDay({ dayKey: '2026-06-15', date: 'Jun 15', dayName: 'Monday' }),
          buildDay({ dayKey: '2026-06-16', date: 'Jun 16', dayName: 'Tuesday' }),
        ]}
        upperAirDays={[
          buildUpperAirDay('2026-06-14', { troughSupport: false }),
          buildUpperAirDay('2026-06-15', { troughSupport: true }),
          buildUpperAirDay('2026-06-16', { troughSupport: true }),
        ]}
        upperAirSeries={buildUpperAirSeries(['2026-06-14', '2026-06-15', '2026-06-16'], (idx) => 5900 - idx * 4)}
        upperAirWindow={UPPER_AIR_WINDOW}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const bands = screen.getAllByTestId('forecast-upper-air-trough-band')
    expect(bands).toHaveLength(2)

    const left = bands[0]
    const right = bands[1]
    const leftEnd = Number(left.getAttribute('x')) + Number(left.getAttribute('width'))
    expect(leftEnd).toBeCloseTo(Number(right.getAttribute('x')), 5)
  })
})

function buildHourlyToday() {
  return [
    { label: 'Now', condition: 'Clear', temperatureF: 72, windSpeedKts: 11, windGustKts: 18, windDirection: 'NE', windDirectionDeg: 45, kind: 'forecast' as const },
    { label: '11AM', condition: 'Clear', temperatureF: 73, windSpeedKts: 12, windGustKts: 19, windDirection: 'NE', windDirectionDeg: 50, kind: 'forecast' as const },
  ]
}

describe('ForecastDrawer panel hierarchy', () => {
  // The page answers the same question at three ranges. They are peers, so
  // they are three sibling panels rather than a 16-day chart buried inside the
  // card for one selected day.
  it('renders today, the extended forecast and upper air as sibling panels', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay(), buildDay({ dayKey: '2026-06-15', date: 'Jun 15', dayName: 'Monday' })]}
        hourlyToday={buildHourlyToday()}
        upperAirDays={[buildUpperAirDay('2026-06-14'), buildUpperAirDay('2026-06-15')]}
        upperAirSeries={buildUpperAirSeries(['2026-06-14', '2026-06-15'], (idx) => 5900 - idx * 4)}
        upperAirWindow={UPPER_AIR_WINDOW}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const today = screen.getByTestId('forecast-panel-today')
    const extended = screen.getByTestId('forecast-panel-extended')
    const upperAir = screen.getByTestId('forecast-panel-upper-air')

    for (const [a, b] of [[today, extended], [extended, upperAir]] as const) {
      expect(a.contains(b)).toBe(false)
      expect(b.contains(a)).toBe(false)
      expect(a.parentElement).toBe(b.parentElement)
    }
  })

  // The upper-air panel must not appear at all without a trace, rather than
  // leaving an empty frame that reads as "nothing happening up there".
  it('omits the upper-air panel when there is no trace', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        hourlyToday={buildHourlyToday()}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getByTestId('forecast-panel-extended')).toBeInTheDocument()
    expect(screen.queryByTestId('forecast-panel-upper-air')).not.toBeInTheDocument()
  })

  // The span meter is the only thing separating the three panels structurally,
  // so it has to actually track the horizon each one covers.
  it('scales each span meter to the horizon its panel covers', () => {
    const dayKeys = Array.from({ length: 16 }, (_, i) => `2026-06-${String(14 + i).padStart(2, '0')}`)
    render(
      <ForecastDrawer
        forecast={dayKeys.slice(0, 10).map((dayKey) => buildDay({ dayKey, date: dayKey.slice(5), dayName: 'Sunday' }))}
        hourlyToday={buildHourlyToday()}
        upperAirDays={dayKeys.map((dayKey) => buildUpperAirDay(dayKey))}
        upperAirSeries={buildUpperAirSeries(dayKeys, (idx) => 5900 - idx)}
        upperAirWindow={UPPER_AIR_WINDOW}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const pct = (testId: string) =>
      Number.parseFloat(screen.getByTestId(`${testId}-span-meter`).style.width)

    expect(pct('forecast-panel-upper-air')).toBe(100)
    expect(pct('forecast-panel-extended')).toBeCloseTo(63, 0)
    // Floored rather than 6%, so a single day still reads as a mark.
    expect(pct('forecast-panel-today')).toBe(7)
    expect(pct('forecast-panel-today')).toBeLessThan(pct('forecast-panel-extended'))
  })
})

// The chart frames encode consequence: shape is read before axis numbers, so
// a variable whose frame is fixed too high flattens real weather into a
// baseline scribble, and one that auto-scales to a single day magnifies a
// degree of drift into a dramatic curve. These pin the framing policy:
// wind and wave scale to the WHOLE visible window (deliberately constant
// across day tabs), temperature carries a unit-aware minimum span.
describe('ForecastDrawer chart y-axis framing', () => {
  const PLOT_TOP = 35
  const PLOT_BOTTOM = 125
  const PLOT_HEIGHT = PLOT_BOTTOM - PLOT_TOP

  // The left-hand axis ticks are plain numbers in the manual overlay <svg>;
  // the recharts XAxis hour labels ("12AM") never match this.
  const numericTicks = (chart: HTMLElement) =>
    Array.from(chart.querySelectorAll('text'))
      .map((el) => el.textContent ?? '')
      .filter((text) => /^\d+(\.\d+)?$/.test(text))

  const flatWind = (windSpeed: number, windGust: number) =>
    buildHourlyWind().map((entry) => ({ ...entry, windSpeed, windGust }))

  const windowDays = (hourlyWindFor: (dayIdx: number) => ReturnType<typeof buildHourlyWind>) =>
    Array.from({ length: 10 }, (_, idx) =>
      buildDay({
        dayKey: `2026-06-${String(14 + idx).padStart(2, '0')}`,
        date: `Jun ${14 + idx}`,
        hourlyWind: hourlyWindFor(idx),
      }),
    )

  const waveWindow = (heightM: number) =>
    Array.from({ length: 10 }, (_, idx) =>
      buildWaveDay({
        dayKey: `2026-06-${String(14 + idx).padStart(2, '0')}`,
        date: `Jun ${14 + idx}`,
        hourlyWave: buildHourlyWave(24, 6, () => heightM),
      }),
    )

  const rampCloud = (baseF: number, spreadF: number) =>
    buildHourlyCloud().map((entry, idx) => ({ ...entry, temperatureF: baseF + (idx / 23) * spreadF }))

  const tempAxisLabels = (chart: HTMLElement) =>
    Array.from(chart.querySelectorAll('text')).filter((el) => /°[CF]$/.test(el.textContent ?? ''))

  it('frames a calm ten-day wind window well below the old fixed 30kt floor', () => {
    render(<ForecastDrawer forecast={windowDays(() => flatWind(8, 11))} loading={false} error={null} unit="metric" />)

    // Window peak 11kt -> 5kt tick step, frame tops out at 15.
    expect(numericTicks(screen.getByTestId('forecast-wind-chart'))).toEqual(['0', '5', '10', '15'])
  })

  // The invariant most likely to regress: someone "fixes" the frame to the
  // selected day and the calm days stop reading as calm relative to what is
  // coming. One rough day late in the window must raise the frame for every
  // tab, including the ones before it.
  it('keeps the wind frame constant across day tabs when one day late in the window is rough', () => {
    render(
      <ForecastDrawer
        forecast={windowDays((idx) => (idx === 8 ? flatWind(30, 38) : flatWind(6, 9)))}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const tabs = screen.getAllByRole('button', { name: /Select forecast day/i })

    fireEvent.click(tabs[0])
    const dayZeroTicks = numericTicks(screen.getByTestId('forecast-wind-chart'))
    fireEvent.click(tabs[8])
    const dayEightTicks = numericTicks(screen.getByTestId('forecast-wind-chart'))

    expect(dayZeroTicks).toEqual(['0', '10', '20', '30', '40'])
    expect(dayEightTicks).toEqual(dayZeroTicks)
  })

  // Guards the divide-by-zero: a frame max of 0 would make windYFor emit
  // NaN into every y attribute in the chart.
  it('does not emit NaN or Infinity geometry when the whole wind window is dead calm', () => {
    render(<ForecastDrawer forecast={windowDays(() => flatWind(0, 0))} loading={false} error={null} unit="metric" />)

    const chart = screen.getByTestId('forecast-wind-chart')
    const badAttributes = Array.from(chart.querySelectorAll('*')).flatMap((el) =>
      Array.from(el.attributes)
        .filter((attr) => /NaN|Infinity/.test(attr.value))
        .map((attr) => `${el.nodeName}.${attr.name}=${attr.value}`),
    )

    expect(badAttributes).toEqual([])
    expect(numericTicks(chart)).toEqual(['0', '5'])
  })

  it('labels half-metre ticks on a wave window under 2m', () => {
    render(
      <ForecastDrawer
        forecast={windowDays(() => flatWind(6, 9))}
        waveDays={waveWindow(1.5)}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(numericTicks(screen.getByTestId('forecast-wave-chart'))).toEqual(['0', '0.5', '1', '1.5'])
  })

  it('labels whole-metre ticks on a wave window over 2m', () => {
    render(
      <ForecastDrawer
        forecast={windowDays(() => flatWind(6, 9))}
        waveDays={waveWindow(2.5)}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(numericTicks(screen.getByTestId('forecast-wave-chart'))).toEqual(['0', '1', '2', '3'])
  })

  it('does not stretch a one-degree day across the whole temperature frame', () => {
    // 1.8°F of drift across the day is exactly 1.0°C, against an 8°C floor.
    render(<ForecastDrawer forecast={[buildDay({ hourlyCloud: rampCloud(60, 1.8) })]} loading={false} error={null} unit="metric" />)

    const chart = screen.getByTestId('forecast-cloud-chart')
    const labels = tempAxisLabels(chart)
    expect(labels.map((el) => el.textContent)).toEqual(['17°C', '16°C'])

    const highY = Number(labels[0].getAttribute('y'))
    const lowY = Number(labels[1].getAttribute('y'))
    const spanPx = Math.abs(lowY - highY)

    expect(spanPx).toBeGreaterThan(0)
    expect(spanPx).toBeCloseTo((1 / 8) * PLOT_HEIGHT, 1)
    expect(spanPx).toBeLessThan(PLOT_HEIGHT * 0.25)

    // The frame is centred, not bottom-anchored: the flat run sits in the
    // middle of the plot rather than pinned to the baseline.
    const midpoint = (highY + lowY) / 2
    expect(midpoint).toBeCloseTo((PLOT_TOP + PLOT_BOTTOM) / 2 + 3, 1)
  })

  // The recharts YAxis domain and the manual cloudYFor overlay must agree,
  // or the axis labels and the scrub marker drift off the plotted curve.
  it('keeps the plotted temperature curve on the same scale as the axis labels', () => {
    render(<ForecastDrawer forecast={[buildDay({ hourlyCloud: rampCloud(60, 1.8) })]} loading={false} error={null} unit="metric" />)

    const chart = screen.getByTestId('forecast-cloud-chart')
    const curve = chart.querySelector('path.recharts-curve[stroke="hsl(var(--chart-temp) / 0.9)"]')
    expect(curve).toBeTruthy()

    const ys = Array.from((curve!.getAttribute('d') ?? '').matchAll(/[ML,C]\s*-?[\d.]+,(-?[\d.]+)/g)).map((m) =>
      Number(m[1]),
    )
    expect(ys.length).toBeGreaterThan(0)

    const labels = tempAxisLabels(chart)
    // axisTickLabelY nudges the text 3px below the value it marks.
    expect(Math.min(...ys)).toBeCloseTo(Number(labels[0].getAttribute('y')) - 3, 0)
    expect(Math.max(...ys)).toBeCloseTo(Number(labels[1].getAttribute('y')) - 3, 0)
  })

  it('uses a unit-aware minimum temperature span', () => {
    // 7.2°F of drift is 4.0°C: half of the 8°C metric floor, a little under
    // half of the 15°F imperial one.
    const hourlyCloud = rampCloud(60, 7.2)

    const spanPxFor = (unit: 'metric' | 'imperial') => {
      const view = render(<ForecastDrawer forecast={[buildDay({ hourlyCloud })]} loading={false} error={null} unit={unit} />)
      const labels = tempAxisLabels(screen.getByTestId('forecast-cloud-chart'))
      const span = Math.abs(Number(labels[1].getAttribute('y')) - Number(labels[0].getAttribute('y')))
      view.unmount()
      return span
    }

    expect(spanPxFor('metric')).toBeCloseTo((4.0 / 8) * PLOT_HEIGHT, 1)
    expect(spanPxFor('imperial')).toBeCloseTo((7.2 / 15) * PLOT_HEIGHT, 1)
    expect(spanPxFor('metric')).toBeGreaterThan(spanPxFor('imperial'))
  })
})
