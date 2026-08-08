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

function buildHourlyWave(count = 24) {
  return Array.from({ length: count }, (_, idx) => ({
    label: HOUR_LABELS[idx % HOUR_LABELS.length],
    hourOfDay: idx % 24,
    waveHeightM: 1 + idx * 0.05,
    wavePeriodS: 6,
    waveDirectionDeg: 90,
    windWaveHeightM: 0.4 + idx * 0.02,
    swellWaveHeightM: 0.8 + idx * 0.03,
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
    ...overrides,
  }
}

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
    const speedLine = curves.find((path) => path.getAttribute('stroke') === 'rgba(37,99,235,0.95)')
    const gustLine = curves.find((path) => path.getAttribute('stroke') === 'rgba(245,158,11,0.75)')
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
    const curve = chart.querySelector('path.recharts-curve[stroke="rgba(37,99,235,0.95)"]')
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
    const waveLine = curves.find((path) => path.getAttribute('stroke') === 'rgba(20,184,166,0.9)')
    const windWaveLine = curves.find((path) => path.getAttribute('stroke') === 'rgba(245,158,11,0.85)')
    const swellLine = curves.find((path) => path.getAttribute('stroke') === 'rgba(139,92,246,0.85)')
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

  it('renders the precipitation graph with bars for the selected day', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    expect(screen.getByTestId('forecast-precip-chart')).toBeInTheDocument()
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

  it('uses 6-hour-block labels on the precipitation chart', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    const chart = screen.getByTestId('forecast-precip-chart')
    expect(within(chart).getByText('12AM')).toBeInTheDocument()
    expect(within(chart).getByText('6AM')).toBeInTheDocument()
    expect(within(chart).getByText('12PM')).toBeInTheDocument()
    expect(within(chart).getByText('6PM')).toBeInTheDocument()
  })

  // Pins the recharts-backed bar rendering to the exact same visual output as
  // the hand-rolled <rect> loop it replaces: one bar per hour with a
  // non-zero intensity (zero-intensity hours render nothing, same as
  // today), each colored independently by precipBarColor's Light/Moderate/
  // Heavy bands - not a single shared fill for the whole series.
  it('renders one precipitation bar per non-zero hourly reading, each colored by its own intensity band', () => {
    const hourlyPrecip = buildHourlyPrecip().map((entry, idx) => (idx === 18 ? { ...entry, precipIntensityMm: 8.2 } : entry))

    render(
      <ForecastDrawer
        forecast={[buildDay({ hourlyPrecip })]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const bars = screen.getAllByTestId('forecast-precip-bar')
    // buildHourlyPrecip only gives non-zero intensity every 6th hour (4 of 24).
    expect(bars).toHaveLength(4)
    // idx 0 -> 1.5mm/hr (Light band), idx 18 -> overridden to 8.2mm/hr (Heavy band).
    expect(bars[0]).toHaveAttribute('fill', 'rgba(147,197,253,0.85)')
    expect(bars[3]).toHaveAttribute('fill', 'rgba(29,78,216,0.9)')
  })

  it('shows an unavailable message when a day has no precipitation forecast', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay({ hourlyPrecip: [] })]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getByTestId('forecast-wind-chart')).toBeInTheDocument()
    expect(screen.queryByTestId('forecast-precip-chart')).not.toBeInTheDocument()
    expect(screen.getByTestId('forecast-precip-unavailable')).toBeInTheDocument()
  })

  it('plots UV as a second series on the cloud & temperature chart, with a sun protection recommendation', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    const chart = screen.getByTestId('forecast-cloud-chart')
    // Temperature + UV curves share the same chart now, instead of UV having
    // its own standalone chart. Both are recharts <Area>/<Line> series, which
    // (unlike the hand-rolled <polyline> this replaces) always render as SVG
    // <path> elements - see recharts' shared Curve component.
    expect(chart.querySelectorAll('path.recharts-curve').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText('Sun protection recommended from 7AM to 5PM.')).toBeInTheDocument()
  })

  it('strokes the UV series with the multi-stop UV gradient', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    const chart = screen.getByTestId('forecast-cloud-chart')
    const curves = Array.from(chart.querySelectorAll('path.recharts-curve'))
    const gradientStroked = curves.some((path) => (path.getAttribute('stroke') ?? '').startsWith('url(#'))
    expect(gradientStroked).toBe(true)
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
