import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'

import { ForecastTideSection } from '@/components/forecast-tide-section'
import { useTideChart } from '@/hooks/use-tide-chart'
import { useTideSettings } from '@/hooks/use-tide-settings'

vi.mock('@/hooks/use-tide-settings')
vi.mock('@/hooks/use-tide-chart')

const mockedUseTideSettings = vi.mocked(useTideSettings)
const mockedUseTideChart = vi.mocked(useTideChart)

const STATION = {
  stationId: 'station-1',
  name: 'Test Harbor',
  state: 'CA',
  lat: 37.0,
  lon: -122.0,
  timezone: 'America/Los_Angeles',
}

function settingsState(overrides: Partial<ReturnType<typeof useTideSettings>> = {}) {
  return {
    tideProvider: 'bom',
    tideStationId: 'station-1',
    tideStationName: 'Test Harbor',
    tideAutoStation: false,
    loading: false,
    saving: false,
    error: null,
    refetch: vi.fn(),
    saveStation: vi.fn(),
    ...overrides,
  }
}

function chartState(overrides: Partial<ReturnType<typeof useTideChart>> = {}) {
  return {
    chart: null,
    loading: false,
    error: null,
    isCached: false,
    updatedAt: null,
    ttlSeconds: null,
    refetch: vi.fn(),
    ...overrides,
  }
}

function buildChart(overrides: Record<string, unknown> = {}) {
  const now = Date.now()
  return {
    station: STATION,
    extremes: [
      { time: new Date(now - 60 * 60 * 1000).toISOString(), heightM: 1.8, high: true },
      { time: new Date(now + 5 * 60 * 60 * 1000).toISOString(), heightM: 0.3, high: false },
    ],
    currentHeightM: 0.9,
    direction: 'Rising',
    ...overrides,
  }
}

describe('ForecastTideSection', () => {
  afterEach(() => {
    vi.clearAllMocks()
    vi.unstubAllGlobals()
  })

  it('shows the loading-settings state', () => {
    mockedUseTideSettings.mockReturnValue(settingsState({ loading: true }))
    mockedUseTideChart.mockReturnValue(chartState())

    render(<ForecastTideSection isImperial={false} dayOffset={0} />)

    expect(screen.getByText('Loading tide settings...')).toBeInTheDocument()
  })

  it('shows the BOM auto-detecting state while /api/tide-nearest is in flight', () => {
    vi.stubGlobal('fetch', vi.fn().mockReturnValue(new Promise(() => {}))) // never resolves
    mockedUseTideSettings.mockReturnValue(settingsState({ tideProvider: 'bom', tideStationId: '' }))
    mockedUseTideChart.mockReturnValue(chartState())

    render(<ForecastTideSection isImperial={false} dayOffset={0} />)

    expect(screen.getByText('Finding nearest tide station…')).toBeInTheDocument()
  })

  it('shows the choose-a-station picker when BOM auto-detect fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, json: async () => ({}) }))
    mockedUseTideSettings.mockReturnValue(settingsState({ tideProvider: 'bom', tideStationId: '' }))
    mockedUseTideChart.mockReturnValue(chartState())

    render(<ForecastTideSection isImperial={false} dayOffset={0} />)

    expect(await screen.findByText('Could not detect vessel position — choose a tide station')).toBeInTheDocument()
    expect(screen.getByLabelText('Search tide stations')).toBeInTheDocument()
  })

  it('renders the chart once settings and chart data resolve', () => {
    mockedUseTideSettings.mockReturnValue(settingsState())
    mockedUseTideChart.mockReturnValue(chartState({ chart: buildChart() }))

    render(<ForecastTideSection isImperial={false} dayOffset={0} />)

    expect(screen.getByText('Test Harbor')).toBeInTheDocument()
    expect(screen.queryByTestId('forecast-tide-unavailable')).not.toBeInTheDocument()
  })

  it('toggles the inline station picker via "Change Station"', () => {
    mockedUseTideSettings.mockReturnValue(settingsState())
    mockedUseTideChart.mockReturnValue(chartState({ chart: buildChart() }))

    render(<ForecastTideSection isImperial={false} dayOffset={0} />)

    expect(screen.queryByLabelText('Search tide stations')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Change Station' }))
    expect(screen.getByLabelText('Search tide stations')).toBeInTheDocument()
  })

  it('shows an inline chart-fetch error with a Retry button, not a whole-panel error', () => {
    mockedUseTideSettings.mockReturnValue(settingsState())
    mockedUseTideChart.mockReturnValue(chartState({ error: 'Failed to load tide chart' }))

    render(<ForecastTideSection isImperial={false} dayOffset={0} />)

    expect(screen.getByText('Failed to load tide chart')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument()
    // Station header (proving this isn't a whole-component early return) is still shown.
    expect(screen.getByText('Test Harbor')).toBeInTheDocument()
  })

  it('surfaces the day-scoped unavailable message when dayOffset falls outside the fetched extremes', () => {
    mockedUseTideSettings.mockReturnValue(settingsState())
    mockedUseTideChart.mockReturnValue(chartState({ chart: buildChart() }))

    // Extremes are all on "today" (dayOffset 0); dayOffset 5 is five days out,
    // well beyond them, so the tide chart's own window-scoped empty state should show.
    render(<ForecastTideSection isImperial={false} dayOffset={5} />)

    expect(screen.getByTestId('forecast-tide-unavailable')).toBeInTheDocument()
  })
})
