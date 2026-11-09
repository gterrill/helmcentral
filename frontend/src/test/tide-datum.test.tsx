import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, renderHook, screen, waitFor } from '@testing-library/react'

import { DepthTideTile } from '@/components/depth-tide-tile'
import { AnchorWatchReadings } from '@/components/anchor-watch-readings'
import { ForecastTideSection } from '@/components/forecast-tide-section'
import { PluginsSection } from '@/components/settings/sections/plugins-section'
import { SettingsFormProvider } from '@/components/settings/settings-form-context'
import { SecretsStatusProvider } from '@/components/settings/secrets-status-context'
import { initialRegularSettingsDraft } from '@/components/settings/settings-draft'
import { useForecastWarningsProviders } from '@/hooks/use-forecast-warnings-providers'
import { usePlaceNameProviders } from '@/hooks/use-place-name-providers'
import { usePOIProviders } from '@/hooks/use-poi-providers'
import { useConfiguredTideChart } from '@/hooks/use-configured-tide-chart'
import { useTideProviders } from '@/hooks/use-tide-providers'
import { useTideSettings } from '@/hooks/use-tide-settings'
import { useTideToday, type TideToday } from '@/hooks/use-tide-today'
import { useWaveProviders } from '@/hooks/use-wave-providers'
import { useWeatherProviders } from '@/hooks/use-weather-providers'
import { lowWaterClearanceReasonLabel } from '@/lib/low-water-clearance'
import { maxExpectedDepthM, planningTideDatumDiffers, tideCapture, tideHeightFtOrNull } from '@/lib/rode-plan'
import { currentTideHeightKnown, effectiveTideProvider, tideHeightFtKnown } from '@/lib/tide-datum'
import { selectAnchorPointKey, setAnchorHere, updatePlanningDepth } from '@/hooks/use-anchor-watch'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'

installFakeTelemetryStream({ seeds: { 'vessel-state': { timezone: 'UTC', timezone_source: 'fix' } } })

vi.mock('@/hooks/use-anchor-watch', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-anchor-watch')>()),
  useAnchorWatchSelector: () => '',
}))
vi.mock('@/hooks/use-tide-settings')
vi.mock('@/hooks/use-configured-tide-chart')
vi.mock('@/hooks/use-tide-providers')
vi.mock('@/hooks/use-weather-providers')
vi.mock('@/hooks/use-wave-providers')
vi.mock('@/hooks/use-poi-providers')
vi.mock('@/hooks/use-forecast-warnings-providers')
vi.mock('@/hooks/use-place-name-providers')

const mockedSettings = vi.mocked(useTideSettings)
const mockedChart = vi.mocked(useConfiguredTideChart)
const mockedProviders = vi.mocked(useTideProviders)

const FT_PER_M = 3.28084

const mslTide: TideToday = {
  datetime: new Date().toISOString(),
  current_tide_height_ft: -0.5 * FT_PER_M, // -0.5 m from mean sea level
  tide_direction: 'Rising',
  high_tide_time: new Date(Date.now() + 3 * 3600_000).toISOString(),
  high_tide_height_ft: -0.1 * FT_PER_M,
  low_tide_time: new Date(Date.now() + 9 * 3600_000).toISOString(),
  low_tide_height_ft: -0.9 * FT_PER_M,
  station_name: "Boat's position",
  provider: 'open-meteo',
  height_datum: 'mean_sea_level',
}
const chartTide: TideToday = { ...mslTide, height_datum: 'chart_datum', station_name: 'Test Station', provider: 'bom' }

describe('tide datum helpers', () => {
  it('knows a current height from chart datum only at or above zero', () => {
    expect(currentTideHeightKnown({ current_tide_height_ft: 3 })).toBe(true)
    expect(currentTideHeightKnown({ current_tide_height_ft: 0 })).toBe(true)
    expect(currentTideHeightKnown({ current_tide_height_ft: -1, height_datum: 'chart_datum' })).toBe(false)
    expect(currentTideHeightKnown({ current_tide_height_ft: -2.2, height_datum: 'chart_datum' })).toBe(false)
  })

  it('knows a negative height from mean sea level is real, and -1 is still none', () => {
    expect(currentTideHeightKnown({ current_tide_height_ft: -2.2, height_datum: 'mean_sea_level' })).toBe(true)
    expect(currentTideHeightKnown({ current_tide_height_ft: -1, height_datum: 'mean_sea_level' })).toBe(false)
    expect(currentTideHeightKnown({ current_tide_height_ft: Number.NaN, height_datum: 'mean_sea_level' })).toBe(false)
    expect(tideHeightFtKnown(-1.0000000000000002, 'mean_sea_level')).toBe(true)
  })

  it('names the default provider when none is saved', () => {
    expect(effectiveTideProvider('')).toBe('open-meteo')
    expect(effectiveTideProvider('  ')).toBe('open-meteo')
    expect(effectiveTideProvider('bom')).toBe('bom')
  })
})

describe('rode planner with a tide from mean sea level', () => {
  it('reads a negative current height as a reading, and stamps it as mean sea level', () => {
    expect(tideHeightFtOrNull(mslTide)).toBeCloseTo(-0.5 * FT_PER_M, 6)
    expect(tideCapture(mslTide)).toEqual({ ft: -0.5 * FT_PER_M, datum: 'mean_sea_level' })
  })

  it('still reads a chart-datum height below zero as none, and stamps nothing for chart datum', () => {
    expect(tideHeightFtOrNull({ ...chartTide, current_tide_height_ft: -1.5 })).toBeNull()
    expect(tideCapture(chartTide)).toBeNull()
    expect(tideCapture({ ...chartTide, current_tide_height_ft: 3 })).toEqual({ ft: 3, datum: 'chart_datum' })
    expect(tideCapture({ ...mslTide, current_tide_height_ft: -1 })).toBeNull()
  })

  it('adds the rise to the next high from a negative mean-sea-level height', () => {
    const depth = { depthM: 4, tideHeightFt: -0.5 * FT_PER_M, tideDatum: 'mean_sea_level' as const }
    // -0.5 m now, -0.1 m at the high: 0.4 m of rise.
    expect(maxExpectedDepthM(depth, mslTide)).toBeCloseTo(4.4, 3)
  })

  it('gives no rise when the stored height and the tide are from different datums', () => {
    const chartStamped = { depthM: 4, tideHeightFt: 3, tideDatum: 'chart_datum' as const }
    expect(maxExpectedDepthM(chartStamped, mslTide)).toBeNull()
    const mslStamped = { depthM: 4, tideHeightFt: -1.6, tideDatum: 'mean_sea_level' as const }
    expect(maxExpectedDepthM(mslStamped, chartTide)).toBeNull()
  })

  it('leaves a chart-datum rise as it was', () => {
    const tide: TideToday = { ...chartTide, current_tide_height_ft: 3, high_tide_height_ft: 5 }
    expect(maxExpectedDepthM({ depthM: 4, tideHeightFt: 3 }, tide)).toBeCloseTo(4 + 2 / FT_PER_M, 6)
    expect(maxExpectedDepthM({ depthM: 4, tideHeightFt: null }, tide)).toBeNull()
  })
})

describe('the anchor watch records the datum of the tide it was dropped on', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  function stubFetch() {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ active: true }) })
    vi.stubGlobal('fetch', fetchMock)
    return fetchMock
  }

  it('sends planning_tide_datum with a mean-sea-level height on the drop', async () => {
    const fetchMock = stubFetch()
    await setAnchorHere(-21, 149, { planningDepthM: 5, planningTide: { ft: -1.6, datum: 'mean_sea_level' } })
    const body = JSON.parse(fetchMock.mock.calls[0][1].body as string)
    expect(body.planning_tide_height_ft).toBe(-1.6)
    expect(body.planning_tide_datum).toBe('mean_sea_level')
  })

  it('sends no datum for a chart-datum drop, so the request is as it always was', async () => {
    const fetchMock = stubFetch()
    await setAnchorHere(-21, 149, { planningDepthM: 5, planningTide: { ft: 3, datum: 'chart_datum' } })
    const body = JSON.parse(fetchMock.mock.calls[0][1].body as string)
    expect('planning_tide_datum' in body).toBe(false)
  })

  it('sends the datum when the planner edits the depth', async () => {
    const fetchMock = stubFetch()
    await updatePlanningDepth(5, { ft: -1.6, datum: 'mean_sea_level' })
    const body = JSON.parse(fetchMock.mock.calls[0][1].body as string)
    expect(body).toEqual({ planning_depth_m: 5, planning_tide_height_ft: -1.6, planning_tide_datum: 'mean_sea_level' })
  })
})

describe('the tide tile with heights from mean sea level', () => {
  it('shows a negative height and says what it is from', () => {
    render(
      <DepthTideTile depth={4} isImperialDistance={false} navigationState="anchored" depthTrend={{ points: [], since: 'window' }} tide={mslTide} lastUpdateAgeS={null} timeZone="UTC" />,
    )
    expect(screen.getByText('-0.50')).toBeInTheDocument()
    expect(screen.getByTestId('tide-datum-label')).toHaveTextContent('from mean sea level')
  })

  it('still draws a dash for a chart-datum height below zero, with no datum label', () => {
    render(
      <DepthTideTile depth={4} isImperialDistance={false} navigationState="anchored" depthTrend={{ points: [], since: 'window' }} tide={{ ...chartTide, current_tide_height_ft: -2 }} lastUpdateAgeS={null} timeZone="UTC" />,
    )
    expect(screen.queryByTestId('tide-datum-label')).toBeNull()
    expect(screen.getByText('—')).toBeInTheDocument()
  })
})

describe('useTideToday carries the datum', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('passes height_datum through and defaults an absent one to chart datum', async () => {
    const payload = {
      datetime: '2026-11-09T10:00:00Z', current_tide_height_ft: -1.2, tide_direction: 'Rising',
      high_tide_time: '2026-11-09T14:00:00Z', high_tide_height_ft: -0.3, low_tide_time: '2026-11-09T20:00:00Z', low_tide_height_ft: -3,
      station_name: "Boat's position", provider: 'open-meteo',
    }
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ ...payload, height_datum: 'mean_sea_level' }) }))
    const first = renderHook(() => useTideToday())
    await waitFor(() => expect(first.result.current.loading).toBe(false))
    expect(first.result.current.tide.height_datum).toBe('mean_sea_level')
  })
})

const noChart = {
  chart: null, loading: false, error: null, noStation: false, notSetUp: false, isCached: false, updatedAt: null,
  ttlSeconds: null, providerName: '', positionBased: false, refetch: vi.fn(),
}

describe('ForecastTideSection with a provider that follows the boat', () => {
  function settingsState(overrides = {}) {
    return {
      tideProvider: '', tideStationId: '', tideStationName: '', tideAutoStation: false,
      loading: false, saving: false, error: null, refetch: vi.fn(), saveStation: vi.fn(), ...overrides,
    }
  }

  afterEach(() => {
    vi.clearAllMocks()
    vi.unstubAllGlobals()
  })

  const mslChart = () => ({
    station: { stationId: '45.45,12.35', name: "Boat's position", lat: 45.45, lon: 12.35 },
    extremes: [
      { time: new Date(Date.now() - 3600_000).toISOString(), heightM: -0.1, high: true },
      { time: new Date(Date.now() + 5 * 3600_000).toISOString(), heightM: -0.9, high: false },
    ],
    currentHeightM: -0.4,
    direction: 'Falling',
    heightDatum: 'mean_sea_level' as const,
  })

  it('needs no station: no picker, nothing saved, the chart is asked for the boat', () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const settings = settingsState()
    mockedSettings.mockReturnValue(settings)
    mockedChart.mockReturnValue({ ...noChart, chart: mslChart(), positionBased: true, providerName: 'Open-Meteo' })

    render(<ForecastTideSection isImperial={false} dayOffset={0} />)

    // The screen sends no provider and no station: the server picks the tide.
    expect(mockedChart).toHaveBeenCalledWith('open-meteo||', true)
    expect(screen.queryByLabelText('Search tide stations')).toBeNull()
    expect(screen.queryByText('Change Station')).toBeNull()
    expect(screen.getByText("Boat's position")).toBeInTheDocument()
    expect(screen.getByTestId('forecast-tide-follows-boat')).toHaveTextContent('from mean sea level')
    // Labelled from the chart's own datum.
    expect(screen.getByText(/Tide \(m, from mean sea level\)/)).toBeInTheDocument()
    expect(settings.saveStation).not.toHaveBeenCalled()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('says so when the server has no boat position, instead of showing a tide for nowhere', () => {
    mockedSettings.mockReturnValue(settingsState())
    mockedChart.mockReturnValue({ ...noChart, error: 'vessel position unavailable' })

    render(<ForecastTideSection isImperial={false} dayOffset={0} />)

    expect(screen.getByText('vessel position unavailable')).toBeInTheDocument()
  })

})

describe('Settings tide section', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  function renderSection(providers: Array<Record<string, unknown>>, savedProvider: string) {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, json: async () => ({ ui: savedProvider ? { tide_provider: savedProvider } : {} }) })))
    mockedProviders.mockReturnValue({ providers: providers as never, loading: false, error: null })
    vi.mocked(useWeatherProviders).mockReturnValue({ providers: [], loading: false, error: null })
    vi.mocked(useWaveProviders).mockReturnValue({ providers: [], loading: false, error: null })
    vi.mocked(useForecastWarningsProviders).mockReturnValue({ providers: [], loading: false, error: null })
    vi.mocked(usePOIProviders).mockReturnValue({ providers: [], loading: false, error: null })
    vi.mocked(usePlaceNameProviders).mockReturnValue({ providers: [], loading: false, error: null })
    render(
      <SettingsFormProvider>
        <SecretsStatusProvider>
          <PluginsSection draft={initialRegularSettingsDraft} onChange={() => {}} />
        </SecretsStatusProvider>
      </SettingsFormProvider>,
    )
  }

  const both = [
    { id: 'bom', name: 'BOM', description: '' },
    { id: 'open-meteo', name: 'Open-Meteo (global sea level)', description: '', position_based: true, height_datum: 'mean_sea_level' },
  ]

  it('hides the station fields and says tides follow the boat, by default', async () => {
    renderSection(both, '')
    await waitFor(() => expect(screen.getByTestId('tide-follows-boat')).toBeInTheDocument())
    expect(screen.getByTestId('tide-follows-boat')).toHaveTextContent('no station to choose')
    expect(screen.getByTestId('tide-follows-boat')).toHaveTextContent('mean sea level')
    expect(screen.queryByLabelText('Tide station id')).toBeNull()
    expect(screen.queryByLabelText('Tide station name')).toBeNull()
  })

  it('keeps the station fields for a station-based provider', async () => {
    renderSection(both, 'bom')
    await waitFor(() => expect(screen.getByLabelText('Tide station id')).toBeInTheDocument())
    expect(screen.queryByTestId('tide-follows-boat')).toBeNull()
  })
})

describe('review fixes', () => {
  afterEach(() => {
    vi.clearAllMocks()
    vi.unstubAllGlobals()
  })

  it('pins an exact -1 ft from mean sea level as none, the way the server does', () => {
    expect(tideHeightFtKnown(-1, 'mean_sea_level')).toBe(false)
    expect(tideHeightFtKnown(-1, 'chart_datum')).toBe(false)
    expect(tideHeightFtKnown(-1.0000000000000002, 'mean_sea_level')).toBe(true)
  })

  it('labels the next high or low on the Anchor Watch readings as from mean sea level', () => {
    render(
      <AnchorWatchReadings
        variant="card" depthMeters={4} depthLastUpdateAgeS={null} tide={mslTide} lowWaterClearance={null}
        lowWaterWorkedOutAt={null} lowWaterFailed={false} isImperial={false} noHeadingAtDrop={false}
      />,
    )
    expect(screen.getByText(/from mean sea level/)).toBeInTheDocument()
  })

  it('leaves a chart-datum next high or low unlabelled', () => {
    render(
      <AnchorWatchReadings
        variant="card" depthMeters={4} depthLastUpdateAgeS={null} tide={chartTide} lowWaterClearance={null}
        lowWaterWorkedOutAt={null} lowWaterFailed={false} isImperial={false} noHeadingAtDrop={false}
      />,
    )
    expect(screen.queryByText(/from mean sea level/)).toBeNull()
  })

  it('words the plugin-not-installed reason on its own', () => {
    expect(lowWaterClearanceReasonLabel('no_plugin')).toBe('Tide plugin not installed')
  })

  it('words the no-position reason on its own', () => {
    expect(lowWaterClearanceReasonLabel('no_position')).toBe('No position for the tide')
  })

  it('says a depth saved on another tide source gets no rise, not that there is no station', () => {
    const datum = { depthM: 4, tideHeightFt: 3, tideDatum: 'chart_datum' as const }
    expect(planningTideDatumDiffers(datum, mslTide)).toBe(true)
    expect(planningTideDatumDiffers(datum, chartTide)).toBe(false)
    expect(planningTideDatumDiffers({ ...datum, tideHeightFt: null }, mslTide)).toBe(false)
    expect(planningTideDatumDiffers(null, mslTide)).toBe(false)
  })

})

describe('the chart follows the anchor', () => {
  it('keys the configured chart on the anchor point, so a drop, an Adjust or a raise fetches it again', () => {
    const base = { loaded: true, active: true, anchorLat: 45.5, anchorLon: 12.4 } as never
    expect(selectAnchorPointKey(base)).toBe('45.5,12.4')
    expect(selectAnchorPointKey({ ...(base as object), anchorLat: 45.6 } as never)).toBe('45.6,12.4')
    expect(selectAnchorPointKey({ ...(base as object), active: false } as never)).toBe('')
    expect(selectAnchorPointKey({ ...(base as object), anchorLat: null } as never)).toBe('')
  })
})
