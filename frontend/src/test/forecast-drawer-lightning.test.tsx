import { afterEach, describe, it, expect, vi } from 'vitest'
import { fireEvent, render, screen, within } from '@testing-library/react'

const boat = vi.hoisted(() => ({ zone: 'Australia/Brisbane' }))

vi.mock('@/components/forecast-tide-section', () => ({
  ForecastTideSection: () => <div data-testid="mock-forecast-tide-section" />,
  ForecastPlaceTideSection: () => <div data-testid="mock-place-tide" />,
}))

vi.mock('@/hooks/use-vessel-state', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-vessel-state')>()),
  useVesselStateSelector: (select: (state: { timeZone: string | null }) => unknown) => select({ timeZone: boat.zone }),
}))

import { ForecastDrawer } from '@/components/forecast-drawer'
import type { LightningFeed, LightningWindow } from '@/lib/lightning-bands'

const H = 3_600_000
// Brisbane is UTC+10 all year, so 2026-06-14 runs 14:00Z on the 13th to 14:00Z on the 14th.
const DAY_START = Date.UTC(2026, 5, 13, 14, 0, 0)
const NOW_ISO = new Date().toISOString()

const HOUR_LABELS = Array.from({ length: 24 }, (_, h) => `${h % 12 === 0 ? 12 : h % 12}${h < 12 ? 'AM' : 'PM'}`)

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
    windSummary: null,
    precipitationSummary: null,
    precipitation: 5,
    humidityPct: 58,
    visibilityNm: 9.5,
    sunriseTime: '2026-06-13T20:32:00Z',
    sunsetTime: '2026-06-14T07:47:00Z',
    moonPhase: null,
    hourlyWind: [],
    hourlyPrecip: [],
    hourlyUV: [],
    hourlyCloud: Array.from({ length: 24 }, (_, idx) => ({
      label: HOUR_LABELS[idx],
      hourOfDay: idx,
      condition: 'Cloudy',
      temperatureF: 60 + Math.round(10 * Math.sin((idx / 24) * Math.PI)),
      isDaylight: idx >= 6 && idx <= 18,
      humidityPct: 50,
      visibilityNm: 10,
    })),
    ...overrides,
  }
}

function upperAirSeries(dayKeys: string[]) {
  return dayKeys.flatMap((dayKey, dayIdx) =>
    [0, 6, 12, 18].map((hour, block) => ({
      time: `${dayKey}T${String(hour).padStart(2, '0')}:00:00Z`,
      dayKey,
      localHour: hour,
      height500M: 5900 - (dayIdx * 4 + block) * 2,
      thicknessM: 5645,
      wind500Kts: 30,
      temperature500C: -8,
    })),
  )
}

function windowAt(startHoursFromDayStart: number, probability: number): LightningWindow {
  const startMs = DAY_START + startHoursFromDayStart * H
  return { startMs, endMs: startMs + 3 * H, probability }
}

function feed(overrides: Partial<LightningFeed> = {}): LightningFeed {
  return {
    // Local 1-4 AM 5%, 4-7 AM 20%, 7-10 AM 45%, 10 AM-1 PM 80%.
    windows: [windowAt(1, 0.05), windowAt(4, 0.2), windowAt(7, 0.45), windowAt(10, 0.8)],
    provider: 'ukmo-lightning',
    providerSeen: true,
    refreshing: false,
    refreshError: null,
    error: null,
    updatedAt: NOW_ISO,
    ttlSeconds: 21600,
    ...overrides,
  }
}

function renderDrawer(lightning: LightningFeed | undefined, extra: Record<string, unknown> = {}) {
  return render(
    <ForecastDrawer
      forecast={[buildDay()]}
      lightning={lightning}
      loading={false}
      error={null}
      unit="metric"
      {...extra}
    />,
  )
}

const STAY = {
  placeName: 'Coral Coast Marina',
  lat: -27.2,
  lon: 153.1,
  arrive: '2026-06-14',
  depart: '2026-06-16',
  timezone: 'Australia/Brisbane',
}

// jsdom measures nothing, so the chart is 1000 wide: plot from x=30 to x=980,
// 24 hourly points over hours 0 to 23.
const hourX = (hour: number) => 30 + (hour / 23) * 950

afterEach(() => {
  boat.zone = 'Australia/Brisbane'
})

describe('ForecastDrawer lightning band on a day whose clock is not 24 elapsed hours from 0 to 23', () => {
  const T = (iso: string) => Date.parse(iso)

  function hoverHour(steps: number) {
    const overlay = screen.getByRole('img', { name: /Cloud, temperature and rain for/i })
    ;(overlay as unknown as SVGSVGElement).getBoundingClientRect = () =>
      ({ width: 1000, height: 250, left: 0, top: 0, right: 1000, bottom: 250, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect
    fireEvent.focus(overlay)
    for (let i = 0; i < steps; i += 1) fireEvent.keyDown(overlay, { key: 'ArrowRight' })
  }

  it('draws the last half slot for a window that holds only the 11 PM point, and the label and tooltip agree', () => {
    boat.zone = 'Australia/Sydney' // UTC+11 in December
    // Local 23:00 to 02:00 is 12:00Z to 15:00Z on the 10th.
    const windows = [{ startMs: T('2026-12-10T12:00:00Z'), endMs: T('2026-12-10T15:00:00Z'), probability: 0.8 }]
    renderDrawer(feed({ windows }), { forecast: [buildDay({ dayKey: '2026-12-10', date: 'Dec 10', dayName: 'Thursday' })] })

    const blocks = screen.getAllByTestId('forecast-cloud-lightning-block')
    expect(blocks).toHaveLength(1)
    expect(Number(blocks[0].getAttribute('x'))).toBeCloseTo(hourX(22.5), 1)
    expect(Number(blocks[0].getAttribute('x')) + Number(blocks[0].getAttribute('width'))).toBeCloseTo(hourX(23), 1)
    expect(screen.getByRole('img', { name: 'Lightning probability, highest 80% at 11.00 PM, no data for part of it' })).toBeInTheDocument()

    hoverHour(23)
    expect(screen.getByText(/Lightning 80%/)).toBeInTheDocument()
  })

  it('draws nothing for a window that ends exactly at the first point, and hatches the whole day', () => {
    boat.zone = 'Australia/Sydney'
    // Local 21:00 on the 9th to 00:00 on the 10th.
    const windows = [{ startMs: T('2026-12-09T10:00:00Z'), endMs: T('2026-12-09T13:00:00Z'), probability: 0.8 }]
    renderDrawer(feed({ windows }), { forecast: [buildDay({ dayKey: '2026-12-10', date: 'Dec 10', dayName: 'Thursday' })] })

    expect(screen.queryAllByTestId('forecast-cloud-lightning-block')).toHaveLength(0)
    expect(screen.getByTestId('forecast-cloud-lightning-nodata')).toBeInTheDocument()
    expect(screen.getByTestId('forecast-cloud-lightning-key')).not.toHaveTextContent(/reach/)
    expect(screen.getByRole('img', { name: 'No lightning data' })).toBeInTheDocument()
  })

  it('places a 3 PM to 6 PM window at the 3 PM point on the spring-forward day', () => {
    boat.zone = 'Australia/Sydney' // 2026-10-04: 2 AM jumps to 3 AM, so there is no hour 2
    const hours = [0, 1, ...Array.from({ length: 21 }, (_, i) => i + 3)]
    const hourlyCloud = hours.map((hourOfDay) => ({
      label: HOUR_LABELS[hourOfDay],
      hourOfDay,
      condition: 'Cloudy',
      temperatureF: 65,
      isDaylight: hourOfDay >= 6 && hourOfDay <= 18,
      humidityPct: 50,
      visibilityNm: 10,
    }))
    // 15:00 to 18:00 AEDT (UTC+11) is 04:00Z to 07:00Z.
    const windows = [{ startMs: T('2026-10-04T04:00:00Z'), endMs: T('2026-10-04T07:00:00Z'), probability: 0.4 }]
    renderDrawer(feed({ windows }), {
      forecast: [buildDay({ dayKey: '2026-10-04', date: 'Oct 4', dayName: 'Sunday', hourlyCloud })],
    })

    const [block] = screen.getAllByTestId('forecast-cloud-lightning-block')
    // Holds the 3, 4 and 5 PM points: from halfway to 2 PM to halfway to 6 PM.
    expect(Number(block.getAttribute('x'))).toBeCloseTo(hourX(14.5), 1)
    expect(Number(block.getAttribute('width'))).toBeCloseTo(hourX(17.5) - hourX(14.5), 1)
    expect(screen.getByRole('img', { name: 'Lightning probability, highest 40% at 3.00 PM, no data for part of it' })).toBeInTheDocument()
  })
})

describe('ForecastDrawer lightning band on the Cloud, Temperature & Rain chart', () => {
  it('draws one block per window of the day that reaches 10 percent, at its hours and step', () => {
    renderDrawer(feed())

    const blocks = screen.getAllByTestId('forecast-cloud-lightning-block')
    // The 5% window draws nothing.
    expect(blocks).toHaveLength(3)
    expect(blocks.map((b) => b.getAttribute('data-step'))).toEqual(['1', '2', '3'])
    // A window draws the slots of the points it holds: 4 to 6 AM runs from halfway to 3 to halfway to 7.
    expect(Number(blocks[0].getAttribute('x'))).toBeCloseTo(hourX(3.5), 1)
    expect(Number(blocks[0].getAttribute('width'))).toBeCloseTo(hourX(6.5) - hourX(3.5), 1)
    expect(Number(blocks[2].getAttribute('x'))).toBeCloseTo(hourX(9.5), 1)
    expect(blocks.map((b) => b.getAttribute('fill-opacity'))).toEqual(['0.4', '0.7', '1'])
    expect(blocks[0].getAttribute('fill')).toBe('hsl(var(--chart-lightning))')
  })

  it('merges back-to-back windows of one step into a single block', () => {
    renderDrawer(feed({ windows: [windowAt(4, 0.15), windowAt(7, 0.25), windowAt(10, 0.45)] }))

    const blocks = screen.getAllByTestId('forecast-cloud-lightning-block')
    expect(blocks.map((b) => b.getAttribute('data-step'))).toEqual(['1', '2'])
    expect(Number(blocks[0].getAttribute('x'))).toBeCloseTo(hourX(3.5), 1)
    expect(Number(blocks[0].getAttribute('width'))).toBeCloseTo(hourX(9.5) - hourX(3.5), 1)
  })

  it('keeps a quiet window between two windows of one step as a gap', () => {
    renderDrawer(feed({ windows: [windowAt(1, 0.2), windowAt(4, 0.05), windowAt(7, 0.2)] }))

    expect(screen.getAllByTestId('forecast-cloud-lightning-block')).toHaveLength(2)
  })

  it('draws no block for a window under 10 percent, and leaves its hours unhatched', () => {
    renderDrawer(feed({ windows: [windowAt(1, 0.05), windowAt(4, 0.0999)] }))

    expect(screen.queryAllByTestId('forecast-cloud-lightning-block')).toHaveLength(0)
    // Held for hours 1 to 6 (local), so the hatch is hour 0 and hour 7 on, not those.
    const gaps = screen.getAllByTestId('forecast-cloud-lightning-nodata')
    expect(gaps).toHaveLength(2)
    expect(Number(gaps[0].getAttribute('x'))).toBeCloseTo(hourX(0), 1)
    expect(Number(gaps[0].getAttribute('width'))).toBeCloseTo(hourX(0.5) - hourX(0), 1)
    expect(Number(gaps[1].getAttribute('x'))).toBeCloseTo(hourX(6.5), 1)
    expect(Number(gaps[1].getAttribute('x')) + Number(gaps[1].getAttribute('width'))).toBeCloseTo(hourX(23), 1)
  })

  it('hatches the hours no window covers with a token-coloured pattern, never leaving a gap', () => {
    renderDrawer(feed())

    const gaps = screen.getAllByTestId('forecast-cloud-lightning-nodata')
    expect(gaps).toHaveLength(2)
    expect(gaps[0].getAttribute('fill')).toMatch(/^url\(#.+\)$/)
    const pattern = screen.getByTestId('forecast-cloud-lightning-band').querySelector('pattern')
    expect(pattern).not.toBeNull()
    expect(pattern?.querySelector('line')?.getAttribute('stroke')).toBe('hsl(var(--chart-lightning))')
  })

  it('draws no hatch where windows cover the whole day', () => {
    renderDrawer(feed({ windows: [{ startMs: DAY_START - H, endMs: DAY_START + 26 * H, probability: 0.4 }] }))

    expect(screen.queryAllByTestId('forecast-cloud-lightning-nodata')).toHaveLength(0)
    expect(screen.getAllByTestId('forecast-cloud-lightning-block')).toHaveLength(1)
  })

  it('clips a window that began the evening before to the start of the day', () => {
    renderDrawer(feed({ windows: [windowAt(-2, 0.4)] }))

    const [block] = screen.getAllByTestId('forecast-cloud-lightning-block')
    expect(Number(block.getAttribute('x'))).toBeCloseTo(hourX(0), 1)
    // Local 10 PM to 1 AM holds the midnight point only, which stops at itself.
    expect(Number(block.getAttribute('width'))).toBeCloseTo(hourX(0.5) - hourX(0), 1)
  })

  it('names a time on the chart when the peak window began before the day', () => {
    renderDrawer(feed({ windows: [windowAt(-2, 0.9)] }))

    expect(screen.getByRole('img', { name: 'Lightning probability, highest 90% at 12.00 AM, no data for part of it' })).toBeInTheDocument()
  })

  it('names the band for assistive technology with the highest chance and when', () => {
    renderDrawer(feed())

    expect(screen.getByRole('img', { name: 'Lightning probability, highest 80% at 10.00 AM, no data for part of it' })).toBeInTheDocument()
  })

  it('adds the chance to the tooltip when the hovered hour falls in a window', () => {
    renderDrawer(feed())
    const overlay = screen.getByRole('img', { name: /Cloud, temperature and rain for Sunday/i })
    ;(overlay as unknown as SVGSVGElement).getBoundingClientRect = () =>
      ({ width: 1000, height: 250, left: 0, top: 0, right: 1000, bottom: 250, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect

    fireEvent.focus(overlay)
    for (let i = 0; i < 5; i += 1) fireEvent.keyDown(overlay, { key: 'ArrowRight' })
    // Hour 4 is in the 20% window.
    expect(screen.getByText(/Lightning 20%/)).toBeInTheDocument()
  })

  it('keeps the key to the three steps, with the window and the credit behind its info button', () => {
    renderDrawer(feed())

    const key = screen.getByTestId('forecast-cloud-lightning-key')
    expect(key).toHaveTextContent(/^Lightning chance: 10 to 30% 30 to 60% 60%\+$/)
    expect(screen.queryByText(/Crown copyright/)).toBeNull()

    fireEvent.click(within(key).getByRole('button', { name: 'Lightning details' }))
    expect(screen.getByText('The chance of at least one lightning flash within about 20 km in each 3-hour window.')).toBeInTheDocument()
    expect(screen.getByText('Lightning data © Crown copyright, Met Office, CC BY-SA 4.0.')).toBeInTheDocument()
  })

  it('gives no Met Office credit for another provider', () => {
    renderDrawer(feed({ provider: 'someone-else' }))

    expect(screen.getAllByTestId('forecast-cloud-lightning-block').length).toBeGreaterThan(0)
    fireEvent.click(within(screen.getByTestId('forecast-cloud-lightning-key')).getByRole('button', { name: 'Lightning details' }))
    expect(screen.getByText(/within about 20 km/)).toBeInTheDocument()
    expect(screen.queryByText(/Met Office/)).toBeNull()
  })

  it('draws no band and says why when the forecast is for a stay', () => {
    renderDrawer(feed(), { stay: STAY })

    expect(screen.queryAllByTestId('forecast-cloud-lightning-block')).toHaveLength(0)
    expect(screen.getByTestId('forecast-cloud-lightning-key')).toHaveTextContent(
      "Lightning is for the boat's position, not the stay.",
    )
    expect(screen.getByTestId('forecast-cloud-lightning-key')).not.toHaveTextContent(/Met Office/)
  })

  it('says nothing about the stay when no lightning provider is installed', () => {
    renderDrawer(feed({ provider: null, providerSeen: false, windows: [] }), { stay: STAY })

    expect(screen.queryByTestId('forecast-cloud-lightning-key')).not.toBeInTheDocument()
  })

  it('shows a request error as one line, with every hour hatched, when nothing is held', () => {
    renderDrawer(feed({ windows: [], error: 'lightning provider "ukmo-lightning" unavailable: no run found\nsecond line' }))

    expect(screen.queryAllByTestId('forecast-cloud-lightning-block')).toHaveLength(0)
    expect(screen.getAllByTestId('forecast-cloud-lightning-nodata')).toHaveLength(1)
    const key = screen.getByTestId('forecast-cloud-lightning-key')
    expect(key).toHaveTextContent('Lightning unavailable: lightning provider "ukmo-lightning" unavailable: no run found')
    expect(key).not.toHaveTextContent('second line')
  })

  it('shows a request error under a band that is still drawn', () => {
    renderDrawer(feed({ error: 'network down' }))

    expect(screen.getAllByTestId('forecast-cloud-lightning-block').length).toBeGreaterThan(0)
    expect(screen.getByTestId('forecast-cloud-lightning-key')).toHaveTextContent('Lightning not updated: network down')
  })

  it('shows a refresh error under a band that is still drawn, and when nothing is held', () => {
    const { unmount } = renderDrawer(feed({ refreshError: 'bucket unreachable\nsecond line' }))
    expect(screen.getAllByTestId('forecast-cloud-lightning-block').length).toBeGreaterThan(0)
    expect(screen.getByTestId('forecast-cloud-lightning-key')).toHaveTextContent('Lightning not updated: bucket unreachable')
    expect(screen.getByTestId('forecast-cloud-lightning-key')).not.toHaveTextContent('second line')
    unmount()

    renderDrawer(feed({ windows: [], refreshError: 'bucket unreachable' }))
    expect(screen.getByTestId('forecast-cloud-lightning-key')).toHaveTextContent('Lightning unavailable: bucket unreachable')
  })

  it('draws no loading line while the host refreshes once a provider has been named', () => {
    renderDrawer(feed({ windows: [], refreshing: true }))

    const key = screen.getByTestId('forecast-cloud-lightning-key')
    expect(key).not.toHaveTextContent(/loading|unavailable/i)
    expect(screen.getAllByTestId('forecast-cloud-lightning-nodata')).toHaveLength(1)
  })

  it('says nothing, draws nothing and hatches nothing until a provider has been named: first load, error, refresh', () => {
    const silent = { provider: null, providerSeen: false, windows: [] }
    for (const overrides of [{}, { error: 'boom' }, { refreshError: 'boom' }, { refreshing: true }]) {
      const { unmount } = renderDrawer(feed({ ...silent, ...overrides }))
      expect(screen.queryByTestId('forecast-cloud-lightning-key')).not.toBeInTheDocument()
      expect(screen.queryByTestId('forecast-cloud-lightning-band')).not.toBeInTheDocument()
      expect(screen.queryAllByTestId('forecast-cloud-lightning-nodata')).toHaveLength(0)
      expect(screen.queryByText(/Lightning/i)).not.toBeInTheDocument()
      unmount()
    }
  })

  it('draws nothing and says nothing with no provider', () => {
    renderDrawer(feed({ provider: null, providerSeen: false, windows: [] }))

    expect(screen.queryAllByTestId('forecast-cloud-lightning-block')).toHaveLength(0)
    expect(screen.queryByTestId('forecast-cloud-lightning-key')).not.toBeInTheDocument()
    expect(screen.queryByText(/Lightning/)).not.toBeInTheDocument()
  })

  it('draws nothing and says nothing without a lightning feed at all', () => {
    renderDrawer(undefined)

    expect(screen.queryByTestId('forecast-cloud-lightning-key')).not.toBeInTheDocument()
  })

  it('marks a feed that has stopped updating stale and greys the band', () => {
    const old = new Date(Date.now() - 4 * 24 * 3_600_000).toISOString()
    renderDrawer(feed({ updatedAt: old }))

    expect(screen.getByTestId('forecast-cloud-lightning-stale-badge')).toBeInTheDocument()
    expect(screen.getByTestId('forecast-cloud-lightning-band')).toHaveClass('grayscale')
  })

  it('does not mark a fresh feed stale', () => {
    renderDrawer(feed())

    expect(screen.queryByTestId('forecast-cloud-lightning-stale-badge')).not.toBeInTheDocument()
  })
})

describe('ForecastDrawer lightning band over thin data and changing answers', () => {
  const onePoint = (hourOfDay: number) =>
    buildDay({ hourlyCloud: [{ label: HOUR_LABELS[hourOfDay], hourOfDay, condition: 'Cloudy', temperatureF: 60, isDaylight: true, humidityPct: 50, visibilityNm: 10 }] })

  it('gives a day with one hourly point the whole plot width, or a hatch, never a blank band', () => {
    // Local 10 AM to 1 PM at 80% holds the one 12 PM point.
    const { unmount } = renderDrawer(feed(), { forecast: [onePoint(12)] })
    const [block] = screen.getAllByTestId('forecast-cloud-lightning-block')
    expect(Number(block.getAttribute('x'))).toBeCloseTo(30, 1)
    expect(Number(block.getAttribute('width'))).toBeCloseTo(950, 1)
    unmount()

    renderDrawer(feed({ windows: [] }), { forecast: [onePoint(12)] })
    const [gap] = screen.getAllByTestId('forecast-cloud-lightning-nodata')
    expect(Number(gap.getAttribute('width'))).toBeCloseTo(950, 1)
    expect(screen.getByRole('img', { name: 'No lightning data' })).toBeInTheDocument()
  })

  it('draws no band at all for a day with no hourly points, as there is no chart', () => {
    renderDrawer(feed(), { forecast: [buildDay({ hourlyCloud: [] })] })
    expect(screen.queryByTestId('forecast-cloud-lightning-band')).not.toBeInTheDocument()
  })

  it('has no Upper Air panel, and so no band, for a one-sample trace', () => {
    renderDrawer(feed(), { upperAirSeries: upperAirSeries(['2026-06-14']).slice(0, 1) })
    expect(screen.queryByTestId('forecast-upper-air-lightning-band')).not.toBeInTheDocument()
  })

  it('draws nothing once the latest answer names no provider, though one answered before', () => {
    const { rerender } = renderDrawer(feed())
    expect(screen.getByTestId('forecast-cloud-lightning-band')).toBeInTheDocument()
    rerender(
      <ForecastDrawer forecast={[buildDay()]} lightning={feed({ provider: null, windows: [] })} loading={false} error={null} unit="metric" />,
    )
    expect(screen.queryByTestId('forecast-cloud-lightning-band')).not.toBeInTheDocument()
    expect(screen.queryByTestId('forecast-cloud-lightning-key')).not.toBeInTheDocument()
  })

  it('words a failure over held windows as not updated, and over nothing held as unavailable', () => {
    const { unmount } = renderDrawer(feed({ refreshError: 'bucket gone\nmore' }))
    expect(screen.getByTestId('forecast-cloud-lightning-key')).toHaveTextContent('Lightning not updated: bucket gone')
    expect(screen.getByTestId('forecast-cloud-lightning-key')).not.toHaveTextContent('unavailable')
    unmount()

    renderDrawer(feed({ windows: [], refreshError: 'bucket gone' }))
    expect(screen.getByTestId('forecast-cloud-lightning-key')).toHaveTextContent('Lightning unavailable: bucket gone')
  })
})

describe('ForecastDrawer lightning strip on the day cards', () => {
  it('lays the windows along the day card at the chart steps, and names the peak', () => {
    renderDrawer(feed({ windows: [windowAt(0, 0.05), windowAt(3, 0.2), windowAt(6, 0.45), windowAt(9, 0.8), { startMs: DAY_START + 12 * H, endMs: DAY_START + 24 * H, probability: 0.02 }] }))

    const card = screen.getByRole('button', { name: /^Select forecast day Sunday/ })
    const cells = within(card).getByTestId('forecast-day-lightning').querySelectorAll('[data-step]')
    expect(Array.from(cells, (c) => c.getAttribute('data-step'))).toEqual(['1', '2', '3'])
    // The chart's own slots for hours 9 to 11, on a 24-point axis.
    expect(parseFloat((cells[2] as HTMLElement).style.left)).toBeCloseTo((8.5 / 23) * 100)
    expect(parseFloat((cells[2] as HTMLElement).style.width)).toBeCloseTo((3 / 23) * 100)
    expect(card).toHaveAccessibleName(/, lightning chance up to 80%$/)
  })

  it('adds the lightning sentence to the day summary, between rain and sun', () => {
    renderDrawer(feed({ windows: [windowAt(0, 0.05), windowAt(3, 0.2), windowAt(6, 0.45), windowAt(9, 0.8), { startMs: DAY_START + 12 * H, endMs: DAY_START + 24 * H, probability: 0.02 }] }), {
      forecast: [buildDay({ precipitationSummary: 'Heavy rain expected throughout the day.' })],
    })

    expect(screen.getByTestId('forecast-cloud-summary').textContent).toBe(
      'Heavy rain expected throughout the day. Lightning likely from 9 AM to 12 PM. No sun protection needed.',
    )
  })

  it('leaves lightning out of the summary on a quiet day and for a stay', () => {
    const quiet = feed({ windows: [{ startMs: DAY_START, endMs: DAY_START + 24 * H, probability: 0.05 }] })
    const { unmount } = renderDrawer(quiet)
    expect(screen.getByTestId('forecast-cloud-summary')).not.toHaveTextContent(/lightning/i)
    unmount()

    renderDrawer(feed(), { stay: STAY })
    expect(screen.getByTestId('forecast-cloud-summary')).not.toHaveTextContent(/lightning/i)
  })

  it('draws no strip on a quiet day and says nothing about lightning', () => {
    renderDrawer(feed({ windows: [{ startMs: DAY_START, endMs: DAY_START + 24 * H, probability: 0.05 }] }))

    const card = screen.getByRole('button', { name: /^Select forecast day Sunday/ })
    expect(within(card).queryByTestId('forecast-day-lightning')).toBeNull()
    expect(card).not.toHaveAccessibleName(/lightning/)
  })

  it('marks the hours past the run as no data, not as quiet', () => {
    renderDrawer(feed({ windows: [{ startMs: DAY_START, endMs: DAY_START + 12 * H, probability: 0.05 }] }))

    const cells = screen.getByTestId('forecast-day-lightning').querySelectorAll('[data-step]')
    expect(Array.from(cells, (c) => c.getAttribute('data-step'))).toEqual(['none'])
  })

  it('greys the strip when the feed is stale, as the chart band is', () => {
    renderDrawer(feed({ updatedAt: new Date(Date.now() - 3 * 86_400_000).toISOString() }))

    expect(screen.getByTestId('forecast-day-lightning')).toHaveClass('grayscale')
  })

  it('draws no strip when no window is held', () => {
    renderDrawer(feed({ windows: [], refreshError: 'GET bucket: 503' }))

    expect(screen.queryByTestId('forecast-day-lightning')).toBeNull()
  })

  it('draws no strip for a planned stay or a boat without a lightning plugin', () => {
    const { unmount } = renderDrawer(feed(), { stay: STAY })
    expect(screen.queryByTestId('forecast-day-lightning')).toBeNull()
    unmount()

    renderDrawer(feed({ provider: null, providerSeen: false }))
    expect(screen.queryByTestId('forecast-day-lightning')).toBeNull()
  })
})

describe('ForecastDrawer lightning band on the Upper Air chart', () => {
  const DAYS = ['2026-06-14', '2026-06-15']
  // Samples at 00, 06, 12 and 18Z on each day: eight points, index 0 to 7.
  const upperAirX = (idx: number) => 30 + (idx / 7) * 950
  const T = (iso: string) => Date.parse(iso)

  function renderUpperAir(lightning: LightningFeed | undefined, extra: Record<string, unknown> = {}) {
    return render(
      <ForecastDrawer
        forecast={[buildDay()]}
        upperAirSeries={upperAirSeries(DAYS)}
        lightning={lightning}
        loading={false}
        error={null}
        unit="metric"
        {...extra}
      />,
    )
  }

  function upperFeed(overrides: Partial<LightningFeed> = {}) {
    return feed({
      windows: [
        { startMs: T('2026-06-14T06:00:00Z'), endMs: T('2026-06-14T12:00:00Z'), probability: 0.35 },
        { startMs: T('2026-06-14T12:00:00Z'), endMs: T('2026-06-14T15:00:00Z'), probability: 0.03 },
        { startMs: T('2026-06-15T06:00:00Z'), endMs: T('2026-06-15T09:00:00Z'), probability: 0.7 },
      ],
      ...overrides,
    })
  }

  it('draws blocks on the trace time axis and nothing past the last window', () => {
    renderUpperAir(upperFeed())

    const blocks = screen.getAllByTestId('forecast-upper-air-lightning-block')
    expect(blocks.map((b) => b.getAttribute('data-step'))).toEqual(['2', '3'])
    // 06Z to 12Z holds the 06Z sample only: half a step either side of index 1.
    expect(Number(blocks[0].getAttribute('x'))).toBeCloseTo(upperAirX(0.5), 1)
    expect(Number(blocks[0].getAttribute('width'))).toBeCloseTo(upperAirX(1.5) - upperAirX(0.5), 1)
    // 06Z to 09Z on the second day holds the 06Z sample, index 5.
    expect(Number(blocks[1].getAttribute('x'))).toBeCloseTo(upperAirX(4.5), 1)
    expect(Number(blocks[1].getAttribute('width'))).toBeCloseTo(upperAirX(5.5) - upperAirX(4.5), 1)
    expect(Number(blocks[1].getAttribute('x')) + Number(blocks[1].getAttribute('width'))).toBeLessThan(upperAirX(7))
  })

  it('hatches the trace where no window covers it, including past the end of the run, and not a quiet window', () => {
    renderUpperAir(upperFeed())

    const gaps = screen.getAllByTestId('forecast-upper-air-lightning-nodata')
    expect(gaps).toHaveLength(3)
    // The first sample, up to halfway to the second.
    expect(Number(gaps[0].getAttribute('x'))).toBeCloseTo(upperAirX(0), 1)
    expect(Number(gaps[0].getAttribute('width'))).toBeCloseTo(upperAirX(0.5) - upperAirX(0), 1)
    // The 12Z sample sits in a 3% window: quiet, not hatched. Hatch resumes at 18Z.
    expect(Number(gaps[1].getAttribute('x'))).toBeCloseTo(upperAirX(2.5), 1)
    expect(Number(gaps[1].getAttribute('x')) + Number(gaps[1].getAttribute('width'))).toBeCloseTo(upperAirX(4.5), 1)
    // Past the run's last window to the end of the trace.
    expect(Number(gaps[2].getAttribute('x'))).toBeCloseTo(upperAirX(5.5), 1)
    expect(Number(gaps[2].getAttribute('x')) + Number(gaps[2].getAttribute('width'))).toBeCloseTo(upperAirX(7), 1)
  })

  it('draws no hatch where windows cover the whole trace', () => {
    renderUpperAir(upperFeed({ windows: [{ startMs: T('2026-06-13T00:00:00Z'), endMs: T('2026-06-17T00:00:00Z'), probability: 0.4 }] }))

    expect(screen.queryAllByTestId('forecast-upper-air-lightning-nodata')).toHaveLength(0)
  })

  it('names days as the Upper Air axis does when it has labels for them', () => {
    const upperAirDay = (dayKey: string, date: string, dayName: string) => ({
      dayKey,
      date,
      dayName,
      outlook: { present: false, height500M: 0, thicknessM: 0, peakWind500Kts: 0, heightPercentile: 0, tendency24hM: 0, troughSupport: false },
    })
    renderUpperAir(upperFeed(), {
      upperAirDays: [upperAirDay('2026-06-14', 'Jun 14', 'Sunday'), upperAirDay('2026-06-15', 'Jun 15', 'Monday')],
    })

    expect(screen.getByRole('img', { name: 'Lightning probability, highest 70% at Mon 15 4.00 PM, no data for part of it' })).toBeInTheDocument()
  })

  it('names the band with the highest chance, its day and time', () => {
    renderUpperAir(upperFeed())

    expect(screen.getByRole('img', { name: 'Lightning probability, highest 70% at Mon 15 4.00 PM, no data for part of it' })).toBeInTheDocument()
  })

  it('names a time on the trace when the peak window began before it', () => {
    renderUpperAir(
      upperFeed({ windows: [{ startMs: T('2026-06-13T21:00:00Z'), endMs: T('2026-06-14T03:00:00Z'), probability: 0.8 }] }),
    )

    // The trace starts 00Z on the 14th, 10 AM in Brisbane.
    expect(screen.getByRole('img', { name: 'Lightning probability, highest 80% at Sun 14 10.00 AM, no data for part of it' })).toBeInTheDocument()
  })

  it('hatches the whole trace and does not say the forecast falls short when no window overlaps it', () => {
    renderUpperAir(
      upperFeed({
        windows: [{ startMs: T('2026-06-01T00:00:00Z'), endMs: T('2026-06-01T03:00:00Z'), probability: 0.4 }],
      }),
    )

    expect(screen.getAllByTestId('forecast-upper-air-lightning-nodata')).toHaveLength(1)
    expect(screen.getByTestId('forecast-upper-air-lightning-key')).not.toHaveTextContent(/reach/)
  })

  it('reports unreadable trace times as a data problem, not as a forecast that falls short', () => {
    const series = upperAirSeries(DAYS)
    series[2] = { ...series[2], time: 'not a time' }
    renderUpperAir(upperFeed(), { upperAirSeries: series })

    const key = screen.getByTestId('forecast-upper-air-lightning-key')
    expect(key).toHaveTextContent('Lightning unavailable: the upper-air times could not be read.')
    expect(screen.queryAllByTestId('forecast-upper-air-lightning-block')).toHaveLength(0)
  })

  it('adds the chance to the trace tooltip when the hovered sample falls in a window', () => {
    renderUpperAir(upperFeed())
    const overlay = screen.getByRole('img', { name: /500mb height and jet wind/i })
    ;(overlay as unknown as SVGSVGElement).getBoundingClientRect = () =>
      ({ width: 1000, height: 250, left: 0, top: 0, right: 1000, bottom: 250, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect

    fireEvent.focus(overlay)
    // Sample 1 is 06Z on the 14th, inside the 35% window.
    fireEvent.keyDown(overlay, { key: 'ArrowRight' })
    expect(screen.getByText(/Lightning 35%/)).toBeInTheDocument()
  })

  it('credits the Met Office behind the info button, only for its provider', () => {
    const { unmount } = renderUpperAir(upperFeed())
    const key = screen.getByTestId('forecast-upper-air-lightning-key')
    expect(key).not.toHaveTextContent(/Met Office/)
    fireEvent.click(within(key).getByRole('button', { name: 'Lightning details' }))
    expect(screen.queryByText(/Hatched/)).toBeNull()
    expect(screen.getByText('Lightning data © Crown copyright, Met Office, CC BY-SA 4.0.')).toBeInTheDocument()
    unmount()

    renderUpperAir(upperFeed({ provider: 'someone-else' }))
    fireEvent.click(within(screen.getByTestId('forecast-upper-air-lightning-key')).getByRole('button', { name: 'Lightning details' }))
    expect(screen.queryByText(/Met Office/)).toBeNull()
  })

  it('draws no band and says why when the forecast is for a stay', () => {
    renderUpperAir(upperFeed(), { stay: STAY })

    expect(screen.queryAllByTestId('forecast-upper-air-lightning-block')).toHaveLength(0)
    expect(screen.getByTestId('forecast-upper-air-lightning-key')).toHaveTextContent(
      "Lightning is for the boat's position, not the stay.",
    )
  })

  it('reports errors under a drawn band and stays silent with no provider, as the other chart does', () => {
    const { unmount: u1 } = renderUpperAir(upperFeed({ error: 'no run found' }))
    expect(screen.getByTestId('forecast-upper-air-lightning-key')).toHaveTextContent('Lightning not updated: no run found')
    expect(screen.getAllByTestId('forecast-upper-air-lightning-block').length).toBeGreaterThan(0)
    u1()

    const { unmount: u2 } = renderUpperAir(upperFeed({ refreshError: 'bucket gone' }))
    expect(screen.getByTestId('forecast-upper-air-lightning-key')).toHaveTextContent('Lightning not updated: bucket gone')
    u2()

    renderUpperAir(upperFeed({ windows: [], provider: null, providerSeen: false, error: 'boom' }))
    expect(screen.queryByTestId('forecast-upper-air-lightning-key')).not.toBeInTheDocument()
    expect(screen.queryByTestId('forecast-upper-air-lightning-band')).not.toBeInTheDocument()
    expect(within(screen.getByTestId('forecast-panel-upper-air')).queryByText(/Lightning/)).not.toBeInTheDocument()
  })

  it('marks a feed that has stopped updating stale', () => {
    const old = new Date(Date.now() - 4 * 24 * 3_600_000).toISOString()
    renderUpperAir(upperFeed({ updatedAt: old }))

    expect(screen.getByTestId('forecast-upper-air-lightning-stale-badge')).toBeInTheDocument()
  })
})
