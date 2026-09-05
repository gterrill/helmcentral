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

import { ForecastDrawer, formatRefreshAge, smoothUpperAirHeights } from '@/components/forecast-drawer'

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
    humidityPct: 50 + (idx % 10),
    visibilityNm: 10 - (idx % 4) * 0.5,
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
    humidityPct: 58,
    visibilityNm: 9.5,
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

  // Type-scale sweep guard: the component should only ever reach for named
  // Tailwind font-size steps (text-2xs, text-xs, text-sm, ...), not one-off
  // text-[Npx] bracket values that drift a pixel or two off a scale step
  // that already exists. Reads the component source directly off disk so any
  // arbitrary font-size utility that creeps back in fails the suite immediately.
  it('contains no arbitrary font-size utilities - only named text- scale steps', () => {
    const testDir = dirname(fileURLToPath(import.meta.url))
    const sourcePath = resolve(testDir, '../components/forecast-drawer.tsx')
    const source = readFileSync(sourcePath, 'utf8')
    const lines = source.split('\n')

    const arbitraryFontSizePattern = /text-\[[^\]]*\]/g

    const offenders: string[] = []
    lines.forEach((line, idx) => {
      const matches = line.match(arbitraryFontSizePattern)
      if (matches) {
        offenders.push(`  line ${idx + 1} (${matches.length}x): ${line.trim()}`)
      }
    })

    expect(
      offenders,
      `Found arbitrary text-[...] font-size utilit(y/ies) in forecast-drawer.tsx - replace with a named scale step (text-2xs/text-xs/text-sm/...):\n${offenders.join('\n')}`,
    ).toEqual([])
  })

  /*
   * Axis labels are text, so they owe 4.5:1 against the card they sit on -
   * the 3:1 graphics bar the plotted lines and areas answer to is not enough.
   * Measured against the light card (--card: 0 0% 100%), the four series
   * tokens the labels used to borrow ran 2.14:1 to 3.63:1, two of them below
   * even the large-text bar. So the labels get their own tokens: darkened in
   * :root, and aliased straight back to the series colour in the two dark
   * themes, which already clear 4.5:1 there.
   *
   * The alias in [data-skin="instrument"] is the one that is easy to miss.
   * That block defines no --chart-* of its own and inherits them from :root,
   * so without an explicit alias it would inherit the DARKENED label values
   * onto its dark card and end up worse than before.
   */
  it('defines a label variant of every series-coloured axis token in all three themes', () => {
    const testDir = dirname(fileURLToPath(import.meta.url))
    const css = readFileSync(resolve(testDir, '../index.css'), 'utf8')
      // Strip comments, or a token named in prose reads as a declaration.
      .replace(/\/\*[\s\S]*?\*\//g, '')

    const block = (selector: string) => {
      const start = css.indexOf(selector)
      expect(start, `${selector} not found in index.css`).toBeGreaterThan(-1)
      return css.slice(start, css.indexOf('}', start))
    }

    const scopes = {
      ':root {': block(':root {'),
      '.dark {': block('.dark {'),
      '[data-skin="instrument"] {': block('[data-skin="instrument"] {'),
    }

    const missing: string[] = []
    for (const series of ['temp', 'precip', 'wave', 'gust']) {
      for (const [selector, source] of Object.entries(scopes)) {
        if (!new RegExp(`--chart-${series}-label:\\s*[^;]+;`).test(source)) {
          missing.push(`--chart-${series}-label in ${selector}`)
        }
      }
    }

    expect(missing, `axis label token(s) not defined:\n${missing.join('\n')}`).toEqual([])
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
    // Each forecast hour still carries its own wind speed and direction. Wind
    // now holds the tile's display slot, so the number and its "kts DIR"
    // suffix are separate spans and no longer one text node.
    const headlines = screen
      .getAllByTestId('forecast-hour-headline')
      .map((el) => (el.textContent ?? '').replace(/\s+/g, ' ').trim())
    expect(headlines).toEqual(['11kts NE', '12kts NE', 'Sunset'])
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

  // Humidity and visibility are now real provider fields (hourly mean/min,
  // reduced host-side), not arithmetic on the precipitation chance - see
  // backend/weather_providers.go's sentinelHumidityPct/sentinelVisibilityNm.
  // The backend sends null (mapped from its own -1 sentinel) when a provider
  // never reported either field for the day; that must render as the dash,
  // never as a fabricated-but-plausible number.
  it('shows a dash for humidity and visibility when the provider has no reading', () => {
    render(<ForecastDrawer forecast={[buildDay({ humidityPct: null, visibilityNm: null })]} loading={false} error={null} unit="metric" />)

    expect(screen.getByTestId('forecast-selected-humidity')).toHaveTextContent('—')
    expect(screen.getByTestId('forecast-selected-visibility')).toHaveTextContent('—')
  })

  it('shows the real humidity and visibility values the provider reported', () => {
    render(<ForecastDrawer forecast={[buildDay({ humidityPct: 62, visibilityNm: 8.5 })]} loading={false} error={null} unit="metric" />)

    expect(screen.getByTestId('forecast-selected-humidity')).toHaveTextContent('62%')
    expect(screen.getByTestId('forecast-selected-visibility')).toHaveTextContent('8.5 nm')
  })

  // The most important assertion in this suite: a genuine 0.0nm visibility
  // reading (real fog thick enough to hide the bow) must render as "0.0 nm",
  // not be mistaken for "no reading" and collapse into the dash. This is
  // exactly the "0% precip during actual rainfall" failure mode one level
  // worse - see the sentinelVisibilityNm doc comment in weather_providers.go.
  it('renders a genuine 0.0 nm visibility as a real reading, not the absent dash', () => {
    render(<ForecastDrawer forecast={[buildDay({ visibilityNm: 0 })]} loading={false} error={null} unit="metric" />)

    const visibility = screen.getByTestId('forecast-selected-visibility')
    expect(visibility).toHaveTextContent('0.0 nm')
    expect(visibility).not.toHaveTextContent('—')
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
    // The barb decoding instruction still has to be on the page, but it is no
    // longer part of the swatch row: it moved above the chart as the wind key
    // (see 'ForecastDrawer chart decoder keys' below).
    expect(screen.getByTestId('forecast-wind-key')).toHaveTextContent('Barbs show the direction the wind is coming from')
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

    // The bounds are no longer restated in a sentence. The height axis now
    // carries the drawn range, top and bottom, and the key carries the one
    // number in the window that is a threshold rather than an extent - the
    // edge of the lowest quintile, which is what the shading means. What
    // matters to this test is unchanged: the range a day is judged against is
    // readable off the chart, not asserted in prose.
    const heightTicks = screen.getAllByTestId('forecast-upper-air-height-tick')
    expect(heightTicks).toHaveLength(2)
    const top = Number(heightTicks[0].textContent!.replace(/[^0-9.]/g, ''))
    const bottom = Number(heightTicks[1].textContent!.replace(/[^0-9.]/g, ''))
    expect(top).toBeGreaterThan(bottom)
    // The series runs 5872-5900, so the padded frame has to contain it.
    expect(top).toBeGreaterThanOrEqual(5900)
    expect(bottom).toBeLessThanOrEqual(5872)

    expect(screen.getByTestId('forecast-upper-air-key')).toHaveTextContent('5851')
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

    // The swatch row that used to name this series is gone. The amber trace is
    // now identified by its own right-hand axis, drawn in the gust colour -
    // which is also what gives it a magnitude, the job the legend's "full
    // scale 40 kts" was doing.
    const chart = screen.getByTestId('forecast-upper-air-chart')
    expect(chart.querySelector('path.recharts-area-area')).toBeTruthy()

    // Gusts run to 41 kts across this window, so the axis tops out at 50.
    const gustTicks = screen.getAllByTestId('forecast-upper-air-gust-tick')
    expect(gustTicks.map((tick) => tick.textContent)).toEqual(['0', '10', '20', '30', '40', '50 kt'])
  })

  // Days the outlook marked have to read as an extent in time (approach,
  // bottom, recovery) rather than as a badge on one card. That extent is now
  // carried by tinting the height trace itself over those samples instead of
  // by a full-height band behind it.
  it('tints the trace over the days whose upper air supports development', () => {
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

    const stops = screen.getAllByTestId('forecast-upper-air-trough-stop')
    const trough = stops.filter((stop) => stop.getAttribute('data-band') === 'trough')
    // Paired stops at the same offset, so the colour change is a step rather
    // than a blend - a day either has upper support or it does not.
    expect(trough).toHaveLength(2)
    // Red, not amber: amber is already the surface-gust series on this chart.
    expect(trough.every((stop) => stop.getAttribute('stop-color') === 'hsl(var(--destructive))')).toBe(true)
    expect(stops.some((stop) => stop.getAttribute('stop-color') === 'hsl(var(--chart-wave) / 0.95)')).toBe(true)

    // The grey quintile band stays: it is a level, not a span, so it cannot be
    // carried by the trace's colour.
    expect(screen.getByTestId('forecast-upper-air-quintile-band')).toBeInTheDocument()
    expect(screen.queryByTestId('forecast-upper-air-trough-band')).not.toBeInTheDocument()
  })

  // The tint is an escalation, so an unremarkable window must not carry it.
  it('leaves the trace its plain colour when no day is flagged', () => {
    render(
      <ForecastDrawer
        forecast={[
          buildDay(),
          buildDay({ dayKey: '2026-06-15', date: 'Jun 15', dayName: 'Monday' }),
        ]}
        upperAirDays={[
          buildUpperAirDay('2026-06-14', { troughSupport: false }),
          buildUpperAirDay('2026-06-15', { troughSupport: false }),
        ]}
        upperAirSeries={buildUpperAirSeries(['2026-06-14', '2026-06-15'], (idx) => 5900 - idx * 4)}
        upperAirWindow={UPPER_AIR_WINDOW}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.queryAllByTestId('forecast-upper-air-trough-stop')).toHaveLength(0)
    const curve = screen.getByTestId('forecast-upper-air-chart').querySelector('path.recharts-line-curve')
    expect(curve!.getAttribute('stroke')).toBe('hsl(var(--chart-wave) / 0.95)')
  })
})

describe('ForecastDrawer upper-air trough bands', () => {
  // Two flagged days in a row are one stretch of weather, not two. Tinting
  // each day only as far as its own last sample leaves an untinted notch
  // between them, which reads as the trough letting up in the middle.
  it('joins consecutive flagged days into one continuous tint', () => {
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

    const trough = screen
      .getAllByTestId('forecast-upper-air-trough-stop')
      .filter((stop) => stop.getAttribute('data-band') === 'trough')
    expect(trough).toHaveLength(4)

    const offsets = trough.map((stop) => Number(stop.getAttribute('offset')!.replace('%', '')))
    // The first span's end offset lands exactly on the second's start, so no
    // base-coloured stop can slip between the two flagged days.
    expect(offsets[1]).toBeCloseTo(offsets[2], 5)
    // And the whole run is one colour.
    expect(new Set(trough.map((stop) => stop.getAttribute('stop-color')))).toEqual(
      new Set(['hsl(var(--destructive))']),
    )
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

  // The decorative span meter that used to live here encoded nothing the
  // adjacent spanLabel text didn't already say, and it sat in the exact slot
  // the panel-level stale badge needed - see 'ForecastDrawer panel staleness'
  // below for its replacement.
})

// The product's stated defining risk is "a frozen dashboard looks exactly
// like a calm night." Every gauge Tile grayscales and badges itself when its
// feed dies; ForecastPanel is a bespoke card that split off from Tile and
// had to grow the same machinery back rather than silently having none.
describe('ForecastDrawer panel staleness', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-06-14T12:30:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows no stale badge and no grayscale for a fresh forecast', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        hourlyToday={buildHourlyToday()}
        loading={false}
        error={null}
        unit="metric"
        updatedAt="2026-06-14T12:29:00Z"
        ttlSeconds={900}
      />,
    )

    expect(screen.queryByTestId('forecast-panel-today-stale-badge')).not.toBeInTheDocument()
    expect(screen.queryByTestId('forecast-panel-extended-stale-badge')).not.toBeInTheDocument()
    expect(screen.getByTestId('forecast-panel-today-body')).not.toHaveClass('grayscale')
    expect(screen.getByTestId('forecast-panel-extended-body')).not.toHaveClass('grayscale')
  })

  // 900s TTL -> 2700s (45min) threshold. 50 minutes old clears it.
  it('shows the stale badge and grayscales the body once the forecast outlives 3x its TTL', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        hourlyToday={buildHourlyToday()}
        loading={false}
        error={null}
        unit="metric"
        updatedAt="2026-06-14T11:40:00Z"
        ttlSeconds={900}
      />,
    )

    expect(screen.getByTestId('forecast-panel-today-stale-badge')).toBeInTheDocument()
    expect(screen.getByTestId('forecast-panel-today-body')).toHaveClass('grayscale')
    expect(screen.getByTestId('forecast-panel-extended-stale-badge')).toBeInTheDocument()
    expect(screen.getByTestId('forecast-panel-extended-body')).toHaveClass('grayscale')

    // Grayscale, never opacity - a faded panel reads as "dim screen in the
    // sun," not "this feed is dead."
    expect(screen.getByTestId('forecast-panel-today-body')).not.toHaveClass('opacity-50')
  })

  // A missing TTL is an absence of evidence, not evidence of staleness -
  // isStale's own documented reasoning, which this must not override with a
  // borrowed SignalK default.
  it('does not mark a feed stale when ttlSeconds is absent, no matter its age', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        hourlyToday={buildHourlyToday()}
        loading={false}
        error={null}
        unit="metric"
        updatedAt="2026-06-10T00:00:00Z"
        ttlSeconds={null}
      />,
    )

    expect(screen.queryByTestId('forecast-panel-today-stale-badge')).not.toBeInTheDocument()
    expect(screen.getByTestId('forecast-panel-today-body')).not.toHaveClass('grayscale')
  })

  it('carries the age in the badge, formatted the same compact way every other stale Tile uses', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        hourlyToday={buildHourlyToday()}
        loading={false}
        error={null}
        unit="metric"
        updatedAt="2026-06-14T11:40:00Z"
        ttlSeconds={900}
      />,
    )

    const badge = screen.getByTestId('forecast-panel-today-stale-badge')
    expect(badge).toHaveTextContent('Stale 50m')
    expect(badge).toHaveAttribute('title', 'No update for 50m')
  })

  // The wave card carries its own TTL and can die on a schedule independent
  // of the weather feed the panel-level badge above tracks.
  it('flags the wave section on its own TTL, independently of a fresh weather feed', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        waveDays={[buildWaveDay()]}
        loading={false}
        error={null}
        unit="metric"
        updatedAt="2026-06-14T12:29:00Z"
        ttlSeconds={900}
        waveUpdatedAt="2026-06-14T00:00:00Z"
        waveTtlSeconds={3600}
      />,
    )

    expect(screen.queryByTestId('forecast-panel-extended-stale-badge')).not.toBeInTheDocument()
    expect(screen.getByTestId('forecast-panel-extended-body')).not.toHaveClass('grayscale')

    expect(screen.getByTestId('forecast-wave-stale-badge')).toBeInTheDocument()
    expect(screen.getByTestId('forecast-wave-body')).toHaveClass('grayscale')
  })
})

// Task 2: the day strip is the only control in the panel and drives four
// charts 800-2000px below it - it has to actually stick, mark its selection
// with something other than a 5% colour tint, and be usable non-visually.
describe('ForecastDrawer day selector', () => {
  it('exposes the selected day via aria-pressed and updates it on click', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay(), buildDay({ dayKey: '2026-06-15', date: 'Jun 15', dayName: 'Monday' })]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const tabs = screen.getAllByRole('button', { name: /Select forecast day/i })
    expect(tabs[0]).toHaveAttribute('aria-pressed', 'true')
    expect(tabs[1]).toHaveAttribute('aria-pressed', 'false')

    fireEvent.click(tabs[1])

    expect(tabs[0]).toHaveAttribute('aria-pressed', 'false')
    expect(tabs[1]).toHaveAttribute('aria-pressed', 'true')
  })

  it('carries wind, condition and precipitation in the accessible name', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay({ windSpeed: 22, windDirection: 'SW', condition: 'Cloudy', precipitation: 85 })]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const tab = screen.getByRole('button', { name: /Select forecast day/i })
    expect(tab).toHaveAccessibleName(/22 kts SW/)
    expect(tab).toHaveAccessibleName(/Cloudy/)
    expect(tab).toHaveAccessibleName(/85% chance of precipitation/)
    expect(tab).toHaveAccessibleName(/high \d+.*low \d+/)
  })

  // precipitation is `number | null` - announcing a fabricated 0% would be
  // worse than saying nothing.
  it('announces missing precipitation data rather than a fabricated number', () => {
    render(<ForecastDrawer forecast={[buildDay({ precipitation: null })]} loading={false} error={null} unit="metric" />)

    const tab = screen.getByRole('button', { name: /Select forecast day/i })
    expect(tab).toHaveAccessibleName(/no precipitation data/)
    expect(tab).not.toHaveAccessibleName(/null/)
  })

  it('exposes the trough marker to assistive tech via an image role, and names it in the day label', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay(), buildDay({ dayKey: '2026-06-15', date: 'Jun 15', dayName: 'Monday' })]}
        upperAirDays={[
          buildUpperAirDay('2026-06-14', { troughSupport: false }),
          buildUpperAirDay('2026-06-15', { troughSupport: true }),
        ]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getByTestId('forecast-upper-air-marker')).toHaveAttribute('role', 'img')

    const flaggedTab = screen.getByRole('button', { name: /Select forecast day Monday Jun 15/i })
    expect(flaggedTab).toHaveAccessibleName(/upper air trough/)
  })

  it('marks the selected day with a non-colour indicator, not colour alone', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay(), buildDay({ dayKey: '2026-06-15', date: 'Jun 15', dayName: 'Monday' })]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const tabs = screen.getAllByRole('button', { name: /Select forecast day/i })

    // Selection is carried by a tonal surface step and an un-muted day name,
    // not by a coloured bar: the board is flat and near-monochrome, and type
    // weight is the cue that survives direct sun where a tint does not.
    expect(tabs[0].className).toMatch(/bg-card/)
    expect(tabs[1].className).not.toMatch(/bg-card/)

    const selectedName = within(tabs[0]).getByText('Today')
    const unselectedName = within(tabs[1]).getByText('Mon')
    expect(selectedName.className).toMatch(/text-foreground/)
    expect(unselectedName.className).toMatch(/text-muted-foreground/)
  })

  // overflow-hidden on the panel section made it a scroll container of its
  // own, so the day row's `sticky top-0` stuck to a box that never scrolls
  // instead of the real scrollport - it never visibly stuck at all.
  it('keeps the extended panel free of overflow-hidden so its sticky day row can actually engage', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    const panel = screen.getByTestId('forecast-panel-extended')
    expect(panel.className).not.toMatch(/\boverflow-hidden\b/)
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

  // Selected by testid rather than by "text ending in a degree unit": the
  // Cloud chart is dual-axis, so its unit is now carried once per axis, on the
  // top tick only ("17°C" over "16"), and a suffix filter would silently drop
  // the bottom label and leave these span assertions reading one element.
  const tempAxisLabels = (chart: HTMLElement) =>
    Array.from(chart.querySelectorAll('[data-testid="forecast-cloud-temp-tick"]'))

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
    // Unit on the top tick only - this chart has two axes, so each states its
    // own unit once rather than on every tick.
    expect(labels.map((el) => el.textContent)).toEqual(['17°C', '16'])

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

// --- Layer-4 resolution: the ten-day strip has to say what it adds up to ---

const RUN_DAY_NAMES = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

function runDayKey(idx: number) {
  return `2026-06-${String(14 + idx).padStart(2, '0')}`
}

function buildDayRun(count: number) {
  return Array.from({ length: count }, (_, idx) =>
    buildDay({ dayKey: runDayKey(idx), date: `Jun ${14 + idx}`, dayName: RUN_DAY_NAMES[idx % 7] }),
  )
}

// buildUpperAirDay pins its own date/dayName, but the intro's day names are
// read off the upper-air day itself, so the run has to carry real ones.
function buildUpperAirRun(count: number, troughIndexes: number[], outlookOverrides: Record<string, unknown> = {}) {
  return Array.from({ length: count }, (_, idx) => ({
    ...buildUpperAirDay(runDayKey(idx), { troughSupport: troughIndexes.includes(idx), ...outlookOverrides }),
    date: `Jun ${14 + idx}`,
    dayName: RUN_DAY_NAMES[idx % 7],
  }))
}

describe('ForecastDrawer ten-day window resolution', () => {
  // ADR 0071 treats a boat with no upper-air plugin as a normal
  // configuration, not a degraded one. There is nothing to resolve, and the
  // Upper Air panel is absent too, so the header stays silent.
  it('says nothing when no upper-air provider is installed', () => {
    render(<ForecastDrawer forecast={buildDayRun(10)} loading={false} error={null} unit="metric" />)

    expect(screen.queryByTestId('forecast-extended-intro')).not.toBeInTheDocument()
  })

  it('names the flagged days that fall inside the visible ten', () => {
    render(
      <ForecastDrawer
        forecast={buildDayRun(10)}
        upperAirDays={buildUpperAirRun(10, [0, 4])}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getByTestId('forecast-extended-intro')).toHaveTextContent(
      'Upper air supports a surface low developing on Sun 14 and Thu 18.',
    )
  })

  // Naming a day the reader cannot select in this strip would be a dead
  // reference, so the ones past day ten are pointed at, not named.
  it('points at the trace instead of naming days beyond the strip', () => {
    render(
      <ForecastDrawer
        forecast={buildDayRun(10)}
        upperAirDays={buildUpperAirRun(12, [11])}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const intro = screen.getByTestId('forecast-extended-intro')
    expect(intro).toHaveTextContent(
      'Upper air supports a surface low developing later in the 12 day trace below.',
    )
    expect(intro).not.toHaveTextContent('Thu 25')
  })

  it('names the in-window days and still points at the rest', () => {
    render(
      <ForecastDrawer
        forecast={buildDayRun(10)}
        upperAirDays={buildUpperAirRun(12, [0, 11])}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getByTestId('forecast-extended-intro')).toHaveTextContent(
      'Upper air supports a surface low developing on Sun 14, and again later in the 12 day trace below.',
    )
  })

  // "Checked and clear" is a finding. It must not collapse into the same
  // silence as "no provider installed" - a future refactor that renders
  // nothing here would be telling the reader nothing was looked at.
  it('says so plainly when the upper air was checked and nothing is developing', () => {
    render(
      <ForecastDrawer
        forecast={buildDayRun(10)}
        upperAirDays={buildUpperAirRun(12, [])}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getByTestId('forecast-extended-intro')).toHaveTextContent(
      'No upper support for a surface low in the next 12 days.',
    )
  })

  // A run of days the provider had no data for is not a clear run. Claiming
  // "no upper support" off an absent outlook is the reassurance nobody
  // measured that the rest of this component is careful to avoid.
  it('stays silent when the provider returned no usable outlook for any day', () => {
    render(
      <ForecastDrawer
        forecast={buildDayRun(10)}
        upperAirDays={buildUpperAirRun(12, [], { present: false })}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.queryByTestId('forecast-extended-intro')).not.toBeInTheDocument()
  })

  // The badge used to label the units (500mb) while the meaning sat in a
  // title attribute nobody hovers on a boat. The mechanism is the trough.
  it('names the mechanism on the day card, not the pressure level', () => {
    render(
      <ForecastDrawer
        forecast={buildDayRun(2)}
        upperAirDays={buildUpperAirRun(2, [1])}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const marker = screen.getByTestId('forecast-upper-air-marker')
    expect(marker).toHaveTextContent('TROUGH')
    expect(marker).toHaveAccessibleName('Upper air supports a surface low developing')
  })
})

// --- The decoder keys are the signal, not the footnote ---
//
// Without these sentences the barbs, the bar opacity, the swell arrows and
// the shaded band cannot be read at all, so they render above the chart at
// text-sm rather than under it at 10px alongside the colour swatches.

function expectPrecedes(key: HTMLElement, chart: HTMLElement) {
  // eslint-disable-next-line no-bitwise
  expect(key.compareDocumentPosition(chart) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
}

describe('ForecastDrawer chart decoder keys', () => {
  it('puts the rain-opacity key above the cloud chart, not in the swatch row', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    const key = screen.getByTestId('forecast-cloud-key')
    expect(key).toHaveTextContent(/opacity/i)
    expect(key.className).toContain('text-sm')
    expectPrecedes(key, screen.getByTestId('forecast-cloud-chart'))
    expect(screen.getByTestId('forecast-cloud-legend')).not.toHaveTextContent(/opacity/i)
  })

  it('puts the wind-barb key above the wind chart, not in the swatch row', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    const key = screen.getByTestId('forecast-wind-key')
    expect(key).toHaveTextContent(/full feather/i)
    expect(key).toHaveTextContent(/coming from/i)
    expect(key.className).toContain('text-sm')
    expectPrecedes(key, screen.getByTestId('forecast-wind-chart'))
    expect(screen.getByTestId('forecast-wind-legend')).not.toHaveTextContent(/feather/i)
  })

  it('puts the swell-arrow key above the wave chart, not in the swatch row', () => {
    render(<ForecastDrawer forecast={[buildDay()]} waveDays={[buildWaveDay()]} loading={false} error={null} unit="metric" />)

    const key = screen.getByTestId('forecast-wave-key')
    expect(key).toHaveTextContent(/heading/i)
    expect(key).toHaveTextContent(/period/i)
    expect(key.className).toContain('text-sm')
    expectPrecedes(key, screen.getByTestId('forecast-wave-chart'))
    expect(screen.getByTestId('forecast-wave-legend')).not.toHaveTextContent(/arrows show/i)
  })

  it('puts the shaded-band key above the upper-air chart, not in the swatch row', () => {
    render(
      <ForecastDrawer
        forecast={buildDayRun(2)}
        upperAirDays={buildUpperAirRun(2, [1])}
        upperAirSeries={buildUpperAirSeries(['2026-06-14', '2026-06-15'], (idx) => 5900 - idx * 4)}
        upperAirWindow={UPPER_AIR_WINDOW}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const key = screen.getByTestId('forecast-upper-air-key')
    expect(key).toHaveTextContent('Grey band marks heights below 5851 m.')
    expect(key).toHaveTextContent('Red marks days with upper support for a surface low.')
    expect(key).toHaveTextContent('Trace smoothed over 18 hours.')
    // The gust scale and the window's extent read off the two axes now, so the
    // key no longer restates either.
    expect(key).not.toHaveTextContent(/full scale/i)
    expect(key).not.toHaveTextContent(/lowest fifth/i)
    expect(key.className).toContain('text-sm')
    expectPrecedes(key, screen.getByTestId('forecast-upper-air-chart'))

    // Both series are named by their own axis, so the swatch row is gone.
    expect(screen.queryByTestId('forecast-upper-air-legend')).not.toBeInTheDocument()
  })

  // The red sentence is a claim about specific days. With no flagged day it
  // would be explaining a colour that is nowhere on the chart.
  it('drops the red clause when no day is flagged', () => {
    render(
      <ForecastDrawer
        forecast={buildDayRun(2)}
        upperAirDays={buildUpperAirRun(2, [])}
        upperAirSeries={buildUpperAirSeries(['2026-06-14', '2026-06-15'], (idx) => 5900 - idx * 4)}
        upperAirWindow={UPPER_AIR_WINDOW}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const key = screen.getByTestId('forecast-upper-air-key')
    expect(key).not.toHaveTextContent(/red marks/i)
    expect(key).toHaveTextContent('Grey band marks heights below 5851 m.')
    expect(key).toHaveTextContent('Trace smoothed over 18 hours.')
  })
})

// --- Both axes carry their own scale, in their own series colour ---

describe('ForecastDrawer upper-air axes', () => {
  // The gust axis used to be hidden, which is why its full scale had to be
  // written into the legend for the amber trace to mean anything. An axis says
  // it better and says it beside the series.
  it('labels the gust axis on the right in the gust colour', () => {
    render(
      <ForecastDrawer
        forecast={[
          buildDay({ windGust: 33 }),
          buildDay({ dayKey: '2026-06-15', date: 'Jun 15', dayName: 'Monday', windGust: 38 }),
        ]}
        upperAirDays={buildUpperAirRun(2, [1])}
        upperAirSeries={buildUpperAirSeries(['2026-06-14', '2026-06-15'], (idx) => 5900 - idx * 4)}
        upperAirWindow={UPPER_AIR_WINDOW}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const gustTicks = screen.getAllByTestId('forecast-upper-air-gust-tick')
    // A 38kt window, so 10kt steps to 40. Unit on the top tick only, exactly
    // as the wind chart's ticks read.
    expect(gustTicks.map((tick) => tick.textContent)).toEqual(['0', '10', '20', '30', '40 kt'])
    expect(gustTicks.every((tick) => tick.getAttribute('text-anchor') === 'end')).toBe(true)
    expect(gustTicks.every((tick) => tick.getAttribute('fill') === 'hsl(var(--chart-gust-label))')).toBe(true)
  })

  // Two series on one frame with two different scales. Colouring each axis to
  // match its trace is what says which number belongs to which line, and is
  // what makes the swatch legend redundant.
  it('labels the height axis on the left in the height colour', () => {
    render(
      <ForecastDrawer
        forecast={buildDayRun(2)}
        upperAirDays={buildUpperAirRun(2, [1])}
        upperAirSeries={buildUpperAirSeries(['2026-06-14', '2026-06-15'], (idx) => 5900 - idx * 4)}
        upperAirWindow={UPPER_AIR_WINDOW}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const heightTicks = screen.getAllByTestId('forecast-upper-air-height-tick')
    expect(heightTicks).toHaveLength(2)
    expect(heightTicks.every((tick) => tick.getAttribute('fill') === 'hsl(var(--chart-wave-label))')).toBe(true)
    // Unit on the top tick only.
    expect(heightTicks[0].textContent).toMatch(/^\d+ m$/)
    expect(heightTicks[1].textContent).toMatch(/^\d+$/)

    expect(screen.queryByTestId('forecast-upper-air-legend')).not.toBeInTheDocument()
  })

  // ADR 0071 section 2: a day is judged against the REST OF THE WINDOW, and
  // flagged when it lands in the lowest quintile of it. The left axis used to
  // print the drawn frame's own extremes - the series min/max plus 15%
  // padding - which is an artefact of the drawing, and sits close enough to
  // the real window to be misread as it. Since the swatch legend went, nothing
  // on the page stated the window at all. The axis states it now.
  it('states the window the days are judged against, not the drawn frame', () => {
    render(
      <ForecastDrawer
        forecast={buildDayRun(2)}
        upperAirDays={buildUpperAirRun(2, [1])}
        // The series runs 5900 down to 5872, so the padded frame is
        // 5867..5905 - deliberately different numbers from the window's
        // 5835..5907, or this test could not tell the two apart.
        upperAirSeries={buildUpperAirSeries(['2026-06-14', '2026-06-15'], (idx) => 5900 - idx * 4)}
        upperAirWindow={UPPER_AIR_WINDOW}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const heightTicks = screen.getAllByTestId('forecast-upper-air-height-tick')
    expect(heightTicks.map((tick) => tick.textContent)).toEqual(['5907 m', '5835'])

    // And positioned at the heights they name, on the same scale the trace is
    // drawn with, rather than parked on the frame edge.
    const PLOT_TOP = 35
    const PLOT_BOTTOM = 125
    const frameMin = 5872 - 5
    const frameMax = 5900 + 5
    const heightYFor = (value: number) =>
      PLOT_TOP + (1 - (value - frameMin) / (frameMax - frameMin)) * (PLOT_BOTTOM - PLOT_TOP)
    expect(Number(heightTicks[0].getAttribute('y'))).toBeCloseTo(heightYFor(5907) + 3, 1)
    expect(Number(heightTicks[1].getAttribute('y'))).toBeCloseTo(heightYFor(5835) + 3, 1)
  })

  // A provider can return a series with no window - the contract's window is
  // optional and lands absent rather than zeroed. There is no window to state
  // then, so the axis falls back to the frame it actually draws.
  it('falls back to the drawn frame when the provider reports no window', () => {
    render(
      <ForecastDrawer
        forecast={buildDayRun(2)}
        upperAirDays={buildUpperAirRun(2, [])}
        upperAirSeries={buildUpperAirSeries(['2026-06-14', '2026-06-15'], (idx) => 5900 - idx * 4)}
        upperAirWindow={{ present: false, lowM: 0, highM: 0, lowQuintileM: 0 }}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    // 5872..5900, padded by 15% of the span with a 5m floor.
    const heightTicks = screen.getAllByTestId('forecast-upper-air-height-tick')
    expect(heightTicks.map((tick) => tick.textContent)).toEqual(['5905 m', '5867'])
  })

  // Moving the labels off the frame edge only works because the window is
  // always inside the frame: the backend cuts the window from the DAILY MEANS
  // (upperAirWindowFor over sortedPresentHeights) while the trace is the
  // sub-daily samples those means average, so the window's low and high can
  // never sit outside the series' own extremes, let alone outside them plus
  // 15% padding. This pins that - it is what keeps a label from being drawn
  // below the plot where nobody can read it.
  it('keeps both window labels inside the drawn plot', () => {
    render(
      <ForecastDrawer
        forecast={buildDayRun(2)}
        upperAirDays={buildUpperAirRun(2, [1])}
        upperAirSeries={buildUpperAirSeries(['2026-06-14', '2026-06-15'], (idx) => 5900 - idx * 4)}
        // A window that could actually come off this series: daily means of
        // sub-daily samples running 5900 down to 5872.
        upperAirWindow={{ present: true, lowM: 5876, highM: 5896, lowQuintileM: 5878 }}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const heightTicks = screen.getAllByTestId('forecast-upper-air-height-tick')
    expect(heightTicks.map((tick) => tick.textContent)).toEqual(['5896 m', '5876'])
    for (const tick of heightTicks) {
      const y = Number(tick.getAttribute('y'))
      expect(y).toBeGreaterThanOrEqual(35)
      expect(y).toBeLessThanOrEqual(125)
    }
  })

  // Same rule the wind and wave charts already use. A fixed 30kt floor renders
  // a 12kt window as a flat line hugging the bottom of a frame that has
  // nothing to do with it - which is exactly the bug a5f9348 fixed on the
  // other two charts and missed here.
  it('scales the gust axis to a calm window instead of a fixed 30kt floor', () => {
    render(
      <ForecastDrawer
        forecast={[
          buildDay({ windGust: 11 }),
          buildDay({ dayKey: '2026-06-15', date: 'Jun 15', dayName: 'Monday', windGust: 12 }),
        ]}
        upperAirDays={buildUpperAirRun(2, [])}
        upperAirSeries={buildUpperAirSeries(['2026-06-14', '2026-06-15'], (idx) => 5900 - idx * 4)}
        upperAirWindow={UPPER_AIR_WINDOW}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    // 12kt window, so 5kt steps up to 15 rather than 10kt steps up to 30.
    const gustTicks = screen.getAllByTestId('forecast-upper-air-gust-tick')
    expect(gustTicks.map((tick) => tick.textContent)).toEqual(['0', '5', '10', '15 kt'])
  })

  // The surface forecast is ten days and the upper-air one sixteen, so a null
  // gust tail is designed behaviour (ADR 0071 section 5) and a window with no
  // overlap at all is a real state. The axis floor is what keeps that from
  // dividing by zero.
  it('renders finite geometry when the window carries no surface gust at all', () => {
    render(
      <ForecastDrawer
        // No forecast day shares a dayKey with the series, so every sample's
        // surface gust is null - the same shape as the tail of a real window.
        forecast={[buildDay({ dayKey: '2026-05-01', date: 'May 1', dayName: 'Friday' })]}
        upperAirDays={buildUpperAirRun(2, [])}
        upperAirSeries={buildUpperAirSeries(['2026-06-14', '2026-06-15'], (idx) => 5900 - idx * 4)}
        upperAirWindow={UPPER_AIR_WINDOW}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const chart = screen.getByTestId('forecast-upper-air-chart')
    const svgs = [chart, ...Array.from(chart.parentElement!.querySelectorAll('svg'))]
    const suspect: string[] = []
    for (const root of svgs) {
      for (const node of Array.from(root.querySelectorAll('*'))) {
        for (const attr of Array.from(node.attributes)) {
          if (/NaN|Infinity/.test(attr.value)) suspect.push(`${node.nodeName}[${attr.name}]=${attr.value}`)
        }
      }
    }
    expect(suspect).toEqual([])

    // The nulls are skipped rather than counted as zeroes, so the axis still
    // carries a usable scale rather than collapsing.
    const gustTicks = screen.getAllByTestId('forecast-upper-air-gust-tick')
    expect(gustTicks.length).toBeGreaterThan(1)
    expect(gustTicks[gustTicks.length - 1].textContent).toMatch(/ kt$/)
  })
})

// --- One axis idiom across all five charts (see the rule block above
// --- AXIS_LABEL_COLOR in forecast-drawer.tsx) ---

describe('ForecastDrawer cloud chart axes', () => {
  const PLOT_TOP = 35
  const PLOT_BOTTOM = 125

  // Rule 1, ownership: the left axis describes one series - temperature - so
  // it takes that series' colour, the same way the upper-air chart's two axes
  // do. Rule 2, units: Cloud is dual-axis, so one <h4> cannot disambiguate the
  // two scales and each axis carries its own unit - once, on its top tick, not
  // on both.
  it('colours the temperature axis in the temperature colour and states its unit once', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    const ticks = screen.getAllByTestId('forecast-cloud-temp-tick')
    // Rule 4, density: temperature is read as a range, so extremes only.
    expect(ticks).toHaveLength(2)
    expect(ticks.every((tick) => tick.getAttribute('fill') === 'hsl(var(--chart-temp-label))')).toBe(true)
    expect(ticks[0].textContent).toMatch(/^-?\d+°C$/)
    expect(ticks[1].textContent).toMatch(/^-?\d+$/)
  })

  // Rule 1 again on the right-hand axis, and rule 3: these two labels were
  // still at a hardcoded y={40}/y={123} left over from an earlier scale, right
  // only by coincidence. They go through the same axisTickLabelY the other
  // axes use, over a precipYFor mapping [0, precipMax] onto the plot.
  it('colours the precipitation axis in the precipitation colour and positions it on its own scale', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    const ticks = screen.getAllByTestId('forecast-cloud-precip-tick')
    expect(ticks).toHaveLength(2)
    expect(ticks.every((tick) => tick.getAttribute('fill') === 'hsl(var(--chart-precip-label))')).toBe(true)
    // Unit on the top tick only, matching the temperature axis opposite it.
    expect(ticks[0].textContent).toMatch(/^\d+\.\d+mm$/)
    expect(ticks[1].textContent).toBe('0')

    // precipMax and 0 land on the frame edges, which is where axisTickLabelY's
    // shared nudge (+8 off the top, -2 off the bottom) applies. The old
    // hardcoded top label sat at 40, three pixels off what the scale says.
    expect(Number(ticks[0].getAttribute('y'))).toBeCloseTo(PLOT_TOP + 8, 5)
    expect(Number(ticks[1].getAttribute('y'))).toBeCloseTo(PLOT_BOTTOM - 2, 5)
  })
})

// --- The drawn trace is smoothed; the reported numbers are not ---

// Pulls the y coordinates out of an SVG path built from coordinate pairs
// (recharts emits M/C/L only for a line curve).
function pathYs(d: string) {
  const numbers = (d.match(/-?\d+(?:\.\d+)?/g) ?? []).map(Number)
  return numbers.filter((_, idx) => idx % 2 === 1)
}

describe('ForecastDrawer upper-air smoothing', () => {
  // 6-hourly samples carry a diurnal ripple that is not synoptic information.
  // A centred 3-sample mean is 18 hours, which flattens it without moving the
  // trough shape the panel exists to show.
  it('averages each sample with its neighbours', () => {
    expect(smoothUpperAirHeights([5900, 5860, 5900, 5860])).toEqual([
      (5900 + 5860) / 2,
      (5900 + 5860 + 5900) / 3,
      (5860 + 5900 + 5860) / 3,
      (5900 + 5860) / 2,
    ])
  })

  // Shrinking the window at the ends rather than dropping the points keeps the
  // trace spanning the full axis; dropping them would leave the frame's first
  // and last day blank.
  it('keeps every sample, shrinking the window at the ends', () => {
    expect(smoothUpperAirHeights([5900, 5880, 5860, 5870, 5890])).toHaveLength(5)
    expect(smoothUpperAirHeights([5900])).toEqual([5900])
    expect(smoothUpperAirHeights([])).toEqual([])
  })

  it('draws the smoothed series, not the raw ripple', () => {
    render(
      <ForecastDrawer
        forecast={buildDayRun(2)}
        upperAirDays={buildUpperAirRun(2, [])}
        upperAirSeries={buildUpperAirSeries(['2026-06-14', '2026-06-15'], (idx) =>
          idx % 2 === 0 ? 5900 : 5860,
        )}
        upperAirWindow={UPPER_AIR_WINDOW}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const curve = screen.getByTestId('forecast-upper-air-chart').querySelector('path.recharts-line-curve')
    const ys = pathYs(curve!.getAttribute('d')!)
    const drawnSpan = Math.max(...ys) - Math.min(...ys)

    // The raw 40m ripple would fill 40/52 of the 90px plot, about 69px. The
    // 3-sample mean leaves a 13m ripple, about 23px.
    expect(drawnSpan).toBeGreaterThan(5)
    expect(drawnSpan).toBeLessThan(40)
  })

  // ADR 0071 section 5: the drawn band and the marked days must not disagree.
  // The smoothing is display-only, so nothing the reader is given as a number
  // may come off it - the tooltip still reports the sample it is standing on.
  it('reports the raw height in the tooltip, not the smoothed one', () => {
    const testDir = dirname(fileURLToPath(import.meta.url))
    const source = readFileSync(resolve(testDir, '../components/forecast-drawer.tsx'), 'utf8')
    const panel = source.slice(source.indexOf('forecast-upper-air-key'))

    expect(panel).toContain('primary={`${Math.round(upperAirTooltipEntry.height500M)} m`}')
    expect(panel).not.toContain('upperAirTooltipEntry.height500Smoothed')
  })
})

// --- The wind warning is the highest-stakes element on the page ---

describe('ForecastDrawer wind warning prominence', () => {
  it('renders the wind warning as its own block under the summary', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        hourlyToday={[
          {
            label: 'Now',
            kind: 'forecast',
            temperatureF: 70,
            condition: 'Clear',
            windSpeedKts: 10,
            windGustKts: 14,
            windDirection: 'NE',
            windDirectionDeg: 45,
          },
        ]}
        activeForecastWarning={{
          provider: 'bom',
          region: 'Capricornia Coast',
          bulletins: [
            {
              id: 'IDQ20085',
              title: 'Marine Wind Warning Summary for Queensland',
              issuedAt: '2026-07-05T01:51:00Z',
              detailsUrl: 'http://www.bom.gov.au/qld/forecasts/map.shtml',
              category: 'wind',
              sections: [{ day: 'Sunday 5 July', warningType: 'Strong Wind Warning' }],
            },
          ],
        }}
        summary="Today's hourly forecast"
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const notice = screen.getByTestId('forecast-wind-warning')
    expect(notice).toHaveTextContent('Wind warning in effect.')
    // Its own block, not a run of text inside the summary paragraph.
    expect(notice.tagName).toBe('P')
    expect(notice.textContent).not.toContain("Today's hourly forecast")
  })
})

// --- Visual weight follows consequence, not convention ---
//
// A passage decision turns on wind. Temperature is context. The page used to
// allocate size the other way round: twelve hourly tiles each shouting a
// near-identical temperature in a display face while the wind sat in the
// smallest type in the tile, and a ten-chip stat row where every chip carried
// exactly the same weight, so nothing could be found without reading all ten.
//
// These pin the ranking. Deliberately NO colour assertions: ADR 0071 section 2
// rules out an invented wind threshold, and a window-relative one would light
// up on a calm week. Size and position carry the ranking, nothing else.
describe('ForecastDrawer visual weight', () => {
  const chipFor = (testId: string) => screen.getByTestId(testId).parentElement as HTMLElement

  it('ranks the decision stats above the reference stats in the same row', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        upperAirDays={[buildUpperAirDay('2026-06-14')]}
        upperAirSeries={buildUpperAirSeries(['2026-06-14'], (idx) => 5900 - idx * 4)}
        upperAirWindow={UPPER_AIR_WINDOW}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    // Every testid the rest of this suite reads through still resolves to the
    // same value - this is a re-ranking, not a rewrite of the data.
    expect(screen.getByTestId('forecast-selected-wind')).toHaveTextContent('10.0 kts')
    expect(screen.getByTestId('forecast-selected-gust')).toHaveTextContent('14.0 kts')
    expect(screen.getByTestId('forecast-selected-precip')).toHaveTextContent('5%')
    expect(screen.getByTestId('forecast-selected-humidity')).toBeInTheDocument()
    expect(screen.getByTestId('forecast-selected-visibility')).toBeInTheDocument()

    const wind = chipFor('forecast-selected-wind')
    const gust = chipFor('forecast-selected-gust')
    const upperAir = screen.getByTestId('forecast-upper-air-detail')

    // Tier 1 keeps the chip treatment and gains weight.
    for (const chip of [wind, gust, upperAir]) {
      expect(chip.className).toMatch(/\btext-sm\b/)
      expect(chip.className).toMatch(/\bbg-/)
    }
    // A stronger ground than the flat bg-muted/50 every chip used to share.
    expect(wind.className).not.toMatch(/bg-muted\/50\b/)
    expect(gust.className).not.toMatch(/bg-muted\/50\b/)

    // Tier 2 drops the chip entirely: plain text, smallest step, muted.
    const reference = [
      chipFor('forecast-selected-precip'),
      chipFor('forecast-selected-humidity'),
      chipFor('forecast-selected-visibility'),
    ]
    for (const stat of reference) {
      expect(stat.className).toMatch(/\btext-2xs\b/)
      expect(stat.className).toMatch(/\btext-muted-foreground\b/)
      expect(stat.className).not.toMatch(/\bbg-/)
      expect(stat.className).not.toMatch(/\brounded\b/)
    }

    // Both tiers still live in one wrapping row, decisions first.
    const row = wind.parentElement as HTMLElement
    for (const stat of [gust, upperAir, ...reference]) {
      expect(stat.parentElement).toBe(row)
    }
    const order = Array.from(row.children)
    for (const stat of reference) {
      expect(order.indexOf(stat)).toBeGreaterThan(order.indexOf(upperAir))
    }

    // Sunrise/sunset/moon keep their icons even without the chip ground.
    const moon = screen.getByText('Moon').closest('span') as HTMLElement
    expect(moon.className).toMatch(/\btext-2xs\b/)
    expect(moon.querySelector('[aria-hidden]')).not.toBeNull()
  })

  // The 500mb chip already reads differently when the air aloft supports a
  // surface low. Now that its whole tier sits on a stronger ground, that
  // distinction has to survive rather than be swallowed by the new baseline.
  it('keeps the 500mb chip distinct when the upper air supports a low', () => {
    const props = {
      forecast: [buildDay()],
      upperAirSeries: buildUpperAirSeries(['2026-06-14'], (idx) => 5900 - idx * 4),
      upperAirWindow: UPPER_AIR_WINDOW,
      loading: false,
      error: null,
      unit: 'metric' as const,
    }

    const { unmount } = render(
      <ForecastDrawer {...props} upperAirDays={[buildUpperAirDay('2026-06-14', { troughSupport: false })]} />,
    )
    const quiet = screen.getByTestId('forecast-upper-air-detail').className
    unmount()

    render(<ForecastDrawer {...props} upperAirDays={[buildUpperAirDay('2026-06-14', { troughSupport: true })]} />)
    const trough = screen.getByTestId('forecast-upper-air-detail').className

    expect(trough).not.toBe(quiet)
    expect(trough).toMatch(/gauge-secondary/)
    expect(quiet).not.toMatch(/gauge-secondary/)
  })

  it('gives the hourly tile display slot to wind, with temperature underneath', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        hourlyToday={[
          { label: 'Now', condition: 'Clear', temperatureF: 72, windSpeedKts: 11, windGustKts: 18, windDirection: 'NE', windDirectionDeg: 45, kind: 'forecast' },
        ]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const tile = screen.getByTestId('forecast-hour-tile')
    const headline = within(tile).getByTestId('forecast-hour-headline')
    const subline = within(tile).getByTestId('forecast-hour-subline')

    // 11 kts big, "kts NE" small beside it; 72F -> 22 degrees on the line the
    // wind used to occupy.
    expect(headline).toHaveTextContent('11')
    expect(headline.textContent).toContain('kts')
    expect(headline.textContent).toContain('NE')
    expect(headline.querySelector('.font-display.text-3xl')?.textContent).toBe('11')
    expect(subline).toHaveTextContent('22°')
    expect(subline.className).toMatch(/\btext-xs\b/)

    // The Now marker survives the swap.
    expect(tile.className).toMatch(/shadow-/)
    expect(tile.querySelector('.bg-gauge-primary')).not.toBeNull()
  })

  it('keeps Sunset in the hourly display slot', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        hourlyToday={[
          { label: 'Now', condition: 'Clear', temperatureF: 72, windSpeedKts: 11, windGustKts: 18, windDirection: 'NE', windDirectionDeg: 45, kind: 'forecast' },
          { label: '5:09PM', condition: 'Sunset', temperatureF: -1, windSpeedKts: -1, windGustKts: -1, windDirection: '—', windDirectionDeg: -1, kind: 'sunset' },
        ]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const sunsetTile = screen.getAllByTestId('forecast-hour-tile')[1]
    expect(within(sunsetTile).getByTestId('forecast-hour-headline')).toHaveTextContent('Sunset')
    expect(within(sunsetTile).queryByTestId('forecast-hour-subline')).not.toBeInTheDocument()
  })

  // Wind only renders when the provider actually gave one. A forecast hour
  // with no wind must not leave the biggest slot in the tile empty.
  it('falls back to temperature in the display slot when an hour has no wind', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay()]}
        hourlyToday={[
          { label: '2PM', condition: 'Clear', temperatureF: 70, windSpeedKts: -1, windGustKts: -1, windDirection: '—', windDirectionDeg: -1, kind: 'forecast' },
        ]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    const tile = screen.getByTestId('forecast-hour-tile')
    const headline = within(tile).getByTestId('forecast-hour-headline')
    expect(headline).toHaveTextContent('21°')
    expect(headline.textContent).not.toContain('kts')
    expect(within(tile).queryByTestId('forecast-hour-subline')).not.toBeInTheDocument()
  })

  it('gives the day card display slot to wind, with the high/low underneath', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    const card = screen.getByRole('button', { name: /Select forecast day Sunday Jun 14/i })
    const headline = within(card).getByTestId('forecast-day-headline')
    const subline = within(card).getByTestId('forecast-day-subline')

    expect(headline.querySelector('.font-display.text-lg')?.textContent).toBe('10')
    expect(headline.textContent).toContain('kts')
    expect(headline.textContent).toContain('NE')

    // 76F/62F -> 24 / 17, in the shape the card already used. The gaps
    // between the parts are CSS, as they were before, so textContent runs
    // them together.
    expect(subline).toHaveTextContent(/^24\s*°C\s*\/\s*17$/)
    expect(subline.className).toMatch(/\btext-2xs\b/)

    // The rest of the card is untouched.
    expect(within(card).getByText('Clear')).toBeInTheDocument()
    expect(within(card).getByText('5% precip')).toBeInTheDocument()
  })
})

/*
 * Scrubbing a chart overlay is the only way to get an exact hourly number off
 * these charts - the summary sentences give ranges. The overlays were
 * pointer-only (no role, no tabindex, no key handling), so with no pointer
 * there was no read path at all. Each overlay is now a focusable, named
 * graphic driven by useChartTooltip's keyboard handlers (covered on their own
 * in use-chart-tooltip.test.tsx).
 */
describe('ForecastDrawer chart keyboard access', () => {
  // jsdom lays nothing out, so the overlay's rect is 0 wide and the hook
  // (correctly) refuses to derive a pixel position from it. Give it a real
  // width the way a browser would.
  function layOut(svg: Element, width = 720) {
    ;(svg as SVGSVGElement).getBoundingClientRect = () =>
      ({ width, height: 175, left: 0, top: 0, right: width, bottom: 175, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect
  }

  function renderAllCharts() {
    render(
      <ForecastDrawer
        forecast={buildDayRun(2)}
        waveDays={[buildWaveDay()]}
        upperAirDays={buildUpperAirRun(2, [1])}
        upperAirSeries={buildUpperAirSeries(['2026-06-14', '2026-06-15'], (idx) => 5900 - idx * 4)}
        upperAirWindow={UPPER_AIR_WINDOW}
        loading={false}
        error={null}
        unit="metric"
      />,
    )
  }

  it('exposes every chart overlay as a focusable, named graphic', () => {
    renderAllCharts()

    // Named after what the chart shows and which day it covers, so the name
    // read aloud on focus is not just "graphic".
    const names = [
      /Cloud, temperature and rain for Sunday/i,
      /Wind and gusts for Sunday/i,
      /Wave height for Sunday/i,
      /500mb height and jet wind/i,
    ]

    for (const name of names) {
      const overlay = screen.getByRole('img', { name })
      expect(overlay.tagName.toLowerCase()).toBe('svg')
      expect(overlay).toHaveAttribute('tabindex', '0')
      // The name has to say how to drive it - an arrow-key affordance is
      // invisible otherwise.
      expect(overlay.getAttribute('aria-label')).toMatch(/arrow keys/i)
      // shadcn's focus-ring convention; --ring exists in all three themes.
      expect(overlay.getAttribute('class')).toContain('focus-visible:ring-2')
      expect(overlay.getAttribute('class')).toContain('focus-visible:ring-ring')
      expect(overlay.getAttribute('class')).toContain('focus-visible:outline-none')
    }
  })

  it('reads hourly wind values off the chart with the keyboard alone', () => {
    renderAllCharts()

    const overlay = screen.getByRole('img', { name: /Wind and gusts for Sunday/i })
    layOut(overlay)

    // Tabbing in shows the first hour rather than an empty focus ring.
    fireEvent.focus(overlay)
    expect(screen.getByText('Gusts: 15 kts')).toBeInTheDocument()

    fireEvent.keyDown(overlay, { key: 'ArrowRight' })
    expect(screen.getByText('Gusts: 16 kts')).toBeInTheDocument()

    fireEvent.keyDown(overlay, { key: 'End' })
    expect(screen.getByText('Gusts: 38 kts')).toBeInTheDocument()

    fireEvent.keyDown(overlay, { key: 'Home' })
    expect(screen.getByText('Gusts: 15 kts')).toBeInTheDocument()

    fireEvent.keyDown(overlay, { key: 'Escape' })
    expect(screen.queryByText('Gusts: 15 kts')).not.toBeInTheDocument()
  })

  it('reads the upper-air trace with the keyboard alone', () => {
    renderAllCharts()

    const overlay = screen.getByRole('img', { name: /500mb height and jet wind/i })
    layOut(overlay)

    fireEvent.keyDown(overlay, { key: 'End' })
    // Last sample of the two-day series: 5900 - 7*4 = 5872 m (the smoothing
    // window collapses to the raw value at the series end).
    expect(screen.getByText('5872 m')).toBeInTheDocument()
    expect(screen.getByText(/^Jet 30 kt/)).toBeInTheDocument()
  })
})
