import { cleanup, render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { BarometerRow, TodayNowTile, changeSpanLabel } from '@/components/today-now-tile'
import { BarometerSparkline } from '@/components/barometer-sparkline'
import type { BarometerTrendData } from '@/hooks/use-barometer-trend'
import type { WeatherToday } from '@/hooks/use-weather-today'

const WEATHER: WeatherToday = {
  datetime: '2026-11-12T14:00:00Z', temperature_f: 82, condition: 'Clear', wind_speed_kts: 14,
  wind_gust_kts: 19, wind_direction: 'ENE', precipitation_pct: 20, provider: 'weatherkit',
  cached: false, updated_at: '2026-11-12T14:00:00Z', ttl_seconds: 900,
}

const MIN = 60_000

/** Readings five minutes apart, the last one now. */
function trend(pa: number[], changePa: number | null, changeSpanS: number | null = changePa === null ? null : 10800): BarometerTrendData {
  const now = Date.now()
  return {
    points: pa.map((pressurePa, i) => ({ time: new Date(now - (pa.length - 1 - i) * 5 * MIN).toISOString(), pressurePa })),
    currentPa: pa.length ? pa[pa.length - 1] : null,
    changePa,
    changeSpanS,
    failed: false,
  }
}

const NONE: BarometerTrendData = { points: [], currentPa: null, changePa: null, changeSpanS: null, failed: false }

function renderTile(barometer: BarometerTrendData) {
  return render(
    <TodayNowTile weather={WEATHER} highTempF={88} lowTempF={74} seaTemperatureF={80} distanceUnits="metric" barometer={<BarometerRow barometer={barometer} />} />,
  )
}

describe('Today & Now barometer', () => {
  it('shows the reading in millibars with the 3 hour change and a sparkline', () => {
    renderTile(trend([101500, 101400, 101290], -210))
    const row = screen.getByTestId('today-barometer')
    expect(within(row).getByText('1012.9')).toBeTruthy()
    expect(within(row).getByText('mb')).toBeTruthy()
    expect(within(row).getByText(/▼/)).toBeTruthy()
    expect(within(row).getByText(/2\.1/)).toBeTruthy()
    expect(row.querySelector('svg')).not.toBeNull()
  })

  it('marks a rise', () => {
    renderTile(trend([101000, 101300], 300))
    expect(within(screen.getByTestId('today-barometer')).getByText(/▲/)).toBeTruthy()
  })

  it('calls a change under 1 mb steady', () => {
    renderTile(trend([101300, 101340], 40))
    const row = screen.getByTestId('today-barometer')
    expect(within(row).getByText(/steady/i)).toBeTruthy()
    expect(within(row).queryByText(/[▲▼]/)).toBeNull()
  })

  it('shows — with no sparkline and no change when the boat has no barometer', () => {
    renderTile(NONE)
    const row = screen.getByTestId('today-barometer')
    expect(within(row).getAllByText('—').length).toBeGreaterThan(0)
    expect(row.querySelector('svg')).toBeNull()
    expect(within(row).queryByText(/[▲▼]/)).toBeNull()
    expect(within(row).queryByText(/steady/i)).toBeNull()
    expect(within(row).queryByText('3h')).toBeNull()
  })

  it('says the barometer is unavailable when the request failed, which a boat with no barometer never does', () => {
    renderTile({ ...NONE, failed: true })
    const row = screen.getByTestId('today-barometer')
    expect(within(row).getByText('Barometer unavailable')).toBeTruthy()
    expect(row.querySelector('svg')).toBeNull()
    cleanup()
    renderTile(NONE)
    expect(screen.queryByText('Barometer unavailable')).toBeNull()
  })

  it('shows the reading but no change until three hours of history can give one, keeping the change column so the chart does not change width', () => {
    renderTile(trend([101300], null))
    const row = screen.getByTestId('today-barometer')
    expect(within(row).getByText('1013.0')).toBeTruthy()
    expect(within(row).queryByText(/steady/i)).toBeNull()
    expect(within(row).getByTestId('barometer-change').textContent).toContain('—')
  })

  it('labels the change with the history it covers, not 3h', () => {
    renderTile(trend([101000, 101300], 300, 2400))
    const row = screen.getByTestId('today-barometer')
    expect(within(row).getByText('40m')).toBeTruthy()
    expect(within(row).queryByText('3h')).toBeNull()
  })

  it('prints the span in lower case, as the docs show it', () => {
    renderTile(trend([101000, 101300], 300, 2400))
    const label = within(screen.getByTestId('barometer-change')).getByText('40m')
    expect(label.className).not.toMatch(/uppercase/)
  })

  it('draws a fall in the same colour as a rise, the arrow carries direction', () => {
    renderTile(trend([101500, 101290], -210))
    const fall = within(screen.getByTestId('today-barometer')).getByText(/▼/)
    expect(fall.className).not.toMatch(/amber|red/)
  })
})

describe('changeSpanLabel', () => {
  it.each([
    [1800, '30m'],
    [2400, '40m'],
    [2520, '40m'],
    [3600, '1h'],
    [5400, '1h 30m'],
    [7200, '2h'],
    [2700, '45m'],
    [2999, '45m'],
    [9760, '2h 40m'],
    [9840, '2h 40m'],
    [9899, '2h 40m'],
    [9900, '3h'],
    [10800, '3h'],
  ])('%i s reads %s', (s, label) => {
    expect(changeSpanLabel(s)).toBe(label)
  })
})

describe('BarometerSparkline', () => {
  const NOW = Date.parse('2026-11-12T12:00:00Z')
  const at = (minutesAgo: number, pressurePa: number) => ({ time: new Date(NOW - minutesAgo * MIN).toISOString(), pressurePa })
  /** One [x, y] list per drawn line. */
  const coords = (container: HTMLElement) =>
    Array.from(container.querySelectorAll('polyline')).map((l) =>
      (l.getAttribute('points') ?? '').split(' ').map((p) => p.split(',').map(Number)),
    )
  const ys = (container: HTMLElement) => coords(container)[0].map(([, y]) => y)

  it('draws nothing for fewer than two points', () => {
    const { container } = render(<BarometerSparkline points={[{ time: 't', pressurePa: 101300 }]} />)
    expect(container.querySelector('svg')).toBeNull()
  })

  it('is not inverted: a higher pressure plots higher on the chart', () => {
    const { container } = render(<BarometerSparkline points={trend([100000, 102000], null).points} />)
    const [first, last] = ys(container)
    expect(last).toBeLessThan(first)
  })

  it('keeps a flat barometer from looking like it swings, with a 2 mb minimum span', () => {
    const flat = render(<BarometerSparkline points={trend([101300, 101300, 101300], null).points} />)
    const flatYs = ys(flat.container)
    expect(Math.max(...flatYs) - Math.min(...flatYs)).toBe(0)

    // 0.5 mb of movement against a 2 mb floor uses a quarter of the height.
    const small = render(<BarometerSparkline points={trend([101300, 101350], null).points} />)
    const smallYs = ys(small.container)
    const full = render(<BarometerSparkline points={trend([101300, 101500], null).points} />)
    const fullYs = ys(full.container)
    const smallSpread = Math.max(...smallYs) - Math.min(...smallYs)
    const fullSpread = Math.max(...fullYs) - Math.min(...fullYs)
    expect(smallSpread / fullSpread).toBeCloseTo(0.25, 1)
  })

  it('places points by time on a three hour axis that ends at the newest point, whatever the device clock says', () => {
    const { container } = render(<BarometerSparkline points={[at(90, 101000), at(85, 101010), at(5, 101190), at(0, 101200)]} />)
    const [line] = coords(container)
    expect(line[0][0]).toBeCloseTo(80, 0) // half way along the 160 wide axis
    expect(line[1][0]).toBeCloseTo(84.4, 0) // five minutes later
    const lines = coords(container)
    const newest = lines[lines.length - 1]
    expect(newest[newest.length - 1][0]).toBeCloseTo(160, 0) // newest point at the right edge
  })

  it('breaks the line across a gap rather than drawing a slope through it', () => {
    const { container } = render(
      <BarometerSparkline points={[at(120,101000), at(115, 101010), at(30, 101200), at(25, 101210)]} />,
    )
    expect(coords(container)).toHaveLength(2)
  })

  it('keeps points five minutes apart on one line', () => {
    const { container } = render(
      <BarometerSparkline points={[at(20,101000), at(15, 101010), at(10, 101020), at(5, 101030)]} />,
    )
    expect(coords(container)).toHaveLength(1)
  })
})
