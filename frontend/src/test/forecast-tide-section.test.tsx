import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'

import { ForecastTideSection } from '@/components/forecast-tide-section'
import { useConfiguredTideChart } from '@/hooks/use-configured-tide-chart'
import { useTideSettings } from '@/hooks/use-tide-settings'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'

// ADR 0230: times are the boat's. The runner is on UTC.
installFakeTelemetryStream({ seeds: { 'vessel-state': { timezone: 'UTC', timezone_source: 'fix' } } })


vi.mock('@/hooks/use-anchor-watch', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-anchor-watch')>()),
  useAnchorWatchSelector: () => '',
}))
vi.mock('@/hooks/use-tide-settings')
vi.mock('@/hooks/use-configured-tide-chart')

const mockedUseTideSettings = vi.mocked(useTideSettings)
const mockedUseTideChart = vi.mocked(useConfiguredTideChart)

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

function chartState(overrides: Partial<ReturnType<typeof useConfiguredTideChart>> = {}) {
  return {
    chart: null,
    loading: false,
    error: null,
    noStation: false,
    notSetUp: false,
    isCached: false,
    updatedAt: null,
    ttlSeconds: null,
    providerName: '',
    providerId: '',
    positionBased: false as boolean | null,
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

  // A station provider with none chosen is the only provider that answers
  // `no_station`, so the nearest station it finds is a real one and is saved,
  // as it always was. The save changes the chart's key, which fetches it again.
  it.each(['bom', 'noaa'])('finds and saves the nearest station for a %s user with none chosen', async (tideProvider) => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ station_id: 'S9', name: 'Nearest Bar' }) })
    vi.stubGlobal('fetch', fetchMock)
    const settings = settingsState({ tideProvider, tideStationId: '' })
    mockedUseTideSettings.mockReturnValue(settings)
    mockedUseTideChart.mockReturnValue(chartState({ noStation: true, error: 'no tide station configured' }))

    render(<ForecastTideSection isImperial={false} dayOffset={0} />)

    expect(screen.getByText('Finding nearest tide station…')).toBeInTheDocument()
    await waitFor(() => expect(settings.saveStation).toHaveBeenCalledWith('S9', 'Nearest Bar'))
    expect(String(fetchMock.mock.calls.find((c) => String(c[0]).includes('tide-nearest'))?.[0])).toContain(`provider=${tideProvider}`)
  })

  it('shows the choose-a-station picker when the nearest station cannot be found', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 503, json: async () => ({ error: 'vessel position unavailable' }) }))
    const settings = settingsState({ tideProvider: 'bom', tideStationId: '' })
    mockedUseTideSettings.mockReturnValue(settings)
    mockedUseTideChart.mockReturnValue(chartState({ noStation: true, error: 'no tide station configured' }))

    render(<ForecastTideSection isImperial={false} dayOffset={0} />)

    expect(await screen.findByText('Could not detect vessel position — choose a tide station')).toBeInTheDocument()
    expect(screen.getByLabelText('Search tide stations')).toBeInTheDocument()
    expect(settings.saveStation).not.toHaveBeenCalled()
  })

  it('never looks a station up for a chart that is not waiting on one', () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    mockedUseTideSettings.mockReturnValue(settingsState({ tideProvider: '', tideStationId: '' }))
    mockedUseTideChart.mockReturnValue(chartState({ chart: buildChart(), positionBased: true }))

    render(<ForecastTideSection isImperial={false} dayOffset={0} />)

    expect(fetchMock.mock.calls.filter((c) => String(c[0]).includes('tide-nearest'))).toHaveLength(0)
  })

  it('asks the server for the configured chart, keyed by the saved provider and station', () => {
    mockedUseTideSettings.mockReturnValue(settingsState({ tideProvider: '', tideStationId: '' }))
    mockedUseTideChart.mockReturnValue(chartState({ loading: true }))

    render(<ForecastTideSection isImperial={false} dayOffset={0} />)

    expect(mockedUseTideChart).toHaveBeenCalledWith('||', true)
    expect(screen.getByText('Loading tide data...')).toBeInTheDocument()
  })

  it('renders the chart once settings and chart data resolve', () => {
    mockedUseTideSettings.mockReturnValue(settingsState())
    mockedUseTideChart.mockReturnValue(chartState({ chart: buildChart() }))

    render(<ForecastTideSection isImperial={false} dayOffset={0} />)

    expect(screen.getByText('Test Harbor')).toBeInTheDocument()
    expect(screen.queryByTestId('forecast-tide-unavailable')).not.toBeInTheDocument()
  })

  // Tide is a single-axis chart, so it follows the same rule Wind ("Wind
  // (kts)") and Wave ("Wave (m)") already do: the unit lives in the header and
  // the ticks are bare numbers. One axis, one header, nothing to disambiguate.
  it('carries the height unit in the header rather than on the axis ticks', () => {
    mockedUseTideSettings.mockReturnValue(settingsState())
    mockedUseTideChart.mockReturnValue(chartState({ chart: buildChart() }))

    const { rerender } = render(<ForecastTideSection isImperial={false} dayOffset={0} />)
    expect(screen.getByText('Tide (m)')).toBeInTheDocument()

    rerender(<ForecastTideSection isImperial dayOffset={0} />)
    expect(screen.getByText('Tide (ft)')).toBeInTheDocument()
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

  // Bug #14: "Change Station" used to be gated on tideProvider === 'bom',
  // so a NOAA user (also station-based) could never change their station.
  it('renders "Change Station" for a NOAA user too, not just BOM', () => {
    mockedUseTideSettings.mockReturnValue(settingsState({ tideProvider: 'noaa' }))
    mockedUseTideChart.mockReturnValue(chartState({ chart: buildChart() }))

    render(<ForecastTideSection isImperial={false} dayOffset={0} />)

    expect(screen.getByRole('button', { name: 'Change Station' })).toBeInTheDocument()
  })

  // Bug #15: the "Data:" source line used to hardcode a two-way binary
  // between two specific provider ids, so any other configured provider
  // (e.g. NOAA) displayed the wrong name. It must instead look up the real
  // provider name from useTideProviders().
  it('shows the real provider name the server sent, not a hardcoded binary', () => {
    mockedUseTideSettings.mockReturnValue(settingsState({ tideProvider: 'noaa' }))
    mockedUseTideChart.mockReturnValue(chartState({ chart: buildChart(), updatedAt: new Date().toISOString(), providerName: 'NOAA (US)' }))

    render(<ForecastTideSection isImperial={false} dayOffset={0} />)

    expect(screen.getByText(/Data: NOAA \(US\)/)).toBeInTheDocument()
  })

  it('falls back to the raw provider id in the "Data:" line when the server named none', () => {
    mockedUseTideSettings.mockReturnValue(settingsState({ tideProvider: 'noaa' }))
    mockedUseTideChart.mockReturnValue(chartState({ chart: buildChart(), updatedAt: new Date().toISOString() }))

    render(<ForecastTideSection isImperial={false} dayOffset={0} />)

    expect(screen.getByText(/Data: noaa/)).toBeInTheDocument()
  })

  // The refresh line used to sit in this component's own header, above the
  // chart. It now rides down into TideChart as footerMeta and sits beside
  // the tide legend at the bottom, matching the Cloud/Wind/Wave cards.
  it('renders the refresh meta at text-sm, sharing a row with the tide legend rather than the header', () => {
    mockedUseTideSettings.mockReturnValue(settingsState())
    mockedUseTideChart.mockReturnValue(chartState({ chart: buildChart(), updatedAt: new Date().toISOString() }))

    render(<ForecastTideSection isImperial={false} dayOffset={0} />)

    const meta = screen.getByTestId('forecast-tide-refresh-meta')
    expect(meta.className).toContain('text-sm')
    const legend = screen.getByTestId('forecast-tide-legend')
    expect(meta.parentElement).toBe(legend.parentElement)
  })

  // When there is no chart to host the footer row (no station yet, still
  // loading, or the fetch failed with nothing cached), the refresh meta must
  // still surface somewhere rather than disappearing along with the header
  // line it used to live in.
  it('shows the refresh meta at the bottom of the section when no chart is available to host it', () => {
    mockedUseTideSettings.mockReturnValue(settingsState())
    mockedUseTideChart.mockReturnValue(chartState({ chart: null, updatedAt: new Date().toISOString() }))

    render(<ForecastTideSection isImperial={false} dayOffset={0} />)

    expect(screen.getByText('No tide data available')).toBeInTheDocument()
    expect(screen.getByTestId('forecast-tide-refresh-meta')).toBeInTheDocument()
  })

  // Until the chart or its error says what kind of provider this is, the saved
  // station and the station controls are not shown for it.
  it('shows neither the saved station nor Change Station while the provider kind is unknown', () => {
    mockedUseTideSettings.mockReturnValue(settingsState({ tideProvider: '', tideStationName: 'Old Harbour' }))
    mockedUseTideChart.mockReturnValue(chartState({ loading: true, positionBased: null }))

    render(<ForecastTideSection isImperial={false} dayOffset={0} />)

    expect(screen.queryByText('Old Harbour')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Change Station' })).not.toBeInTheDocument()
  })

  it('shows neither for a failed answer that says the provider follows a position', () => {
    mockedUseTideSettings.mockReturnValue(settingsState({ tideProvider: '', tideStationName: 'Old Harbour' }))
    mockedUseTideChart.mockReturnValue(chartState({ error: 'vessel position unavailable', positionBased: true }))

    render(<ForecastTideSection isImperial={false} dayOffset={0} />)

    expect(screen.getByText('vessel position unavailable')).toBeInTheDocument()
    expect(screen.queryByText('Old Harbour')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Change Station' })).not.toBeInTheDocument()
  })

  it('looks for the nearest station again on a later no_station for the same provider', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ station_id: 'S9', name: 'Nearest Bar' }) })
    vi.stubGlobal('fetch', fetchMock)
    const settings = settingsState({ tideProvider: 'bom', tideStationId: '' })
    mockedUseTideSettings.mockReturnValue(settings)
    mockedUseTideChart.mockReturnValue(chartState({ noStation: true, error: 'no tide station configured' }))
    const { rerender } = render(<ForecastTideSection isImperial={false} dayOffset={0} />)
    await waitFor(() => expect(settings.saveStation).toHaveBeenCalledTimes(1))

    // The station is saved and the chart comes back; later it is lost again.
    // (dayOffset changes only to make the memoized section render again.)
    mockedUseTideChart.mockReturnValue(chartState({ chart: buildChart() }))
    rerender(<ForecastTideSection isImperial={false} dayOffset={1} />)
    mockedUseTideChart.mockReturnValue(chartState({ noStation: true, error: 'no tide station configured' }))
    rerender(<ForecastTideSection isImperial={false} dayOffset={2} />)

    await waitFor(() => expect(fetchMock.mock.calls.filter((c) => String(c[0]).includes('tide-nearest'))).toHaveLength(2))
    await waitFor(() => expect(settings.saveStation).toHaveBeenCalledTimes(2))
  })
})
