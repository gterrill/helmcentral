import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { App } from '../App'

// The drawer now mounts a map at every anchor state (Phase F), including
// 'none' — this test's vessel position is a real fix, so without this stub
// it would try to mount the real maplibre-gl map in jsdom, which the map's
// own dedicated tests handle separately (see anchor-imagery-toggle.test.tsx
// ~lines 22-37 for the same pattern).
vi.mock('@/components/anchor-watch-map', () => ({
  AnchorWatchMap: () => <div data-testid="anchor-watch-map" />,
}))

// This test renders the dashboard, not the auth gate. State the precondition
// explicitly — an install with auth.mode:none — rather than depending on what
// the real useAuth happens to return when its probe fails (ADR 0040).
vi.mock('@/hooks/use-auth', () => ({
  refreshAuthState: vi.fn().mockResolvedValue({ mode: 'none', user: null }),
  useAuth: () => ({
    mode: 'none' as const,
    user: null,
    role: null,
    loading: false,
    error: null,
    login: vi.fn(),
    logout: vi.fn(),
  }),
}))


const setAnchorHereMock = vi.fn()
const clearAnchorMock = vi.fn()
let anchorStateMock: 'none' | 'set' = 'none'
let positionMock: { latitude: number | null; longitude: number | null; gnssCriticalAlert: boolean } = {
  latitude: -36.8485, longitude: 174.7633, gnssCriticalAlert: false,
}
// ADR 0099: the server-side auto-raise watcher's evidence, surfaced through
// GET /api/anchor-watch and this hook. null until a test sets it.
let lastAutoRaiseMock: { at: string; reason: string } | null = null
beforeEach(() => {
  vi.clearAllMocks()
  anchorStateMock = 'none'
  positionMock = { latitude: -36.8485, longitude: 174.7633, gnssCriticalAlert: false }
  lastAutoRaiseMock = null
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: false,
    json: async () => ({}),
  }))
})

vi.mock('@/hooks/use-vessel-state', () => ({
  useVesselState: () => ({
    depth: null,
    currentDriftKts: null,
    currentSetDeg: null,
    navigationState: null,
    gnssQualityIndicator: null,
    gnssHdop: null,
    gnssSatellites: null,
    gnssValidationState: null,
    gnssValidationReason: null,
    headingTrue: null,
    speedOverGroundKts: null,
    windSpeedApparentKts: null,
    windAngleApparentDeg: null,
    windSide: null,
    windAngleRelativeDeg: null,
    maxGustKts: { '10m': null, '30m': null, '1h': null, '24h': null },
    generatorState: null,
    generatorManualStart: false,
    generatorManualStartTimer: 0,
    generatorRunningByCondition: null,
    generatorRuntime: null,
    engine0Rpm: -1,
    engine1Rpm: 850,
    ...positionMock,
  }),
}))

vi.mock('@/hooks/use-electrical-state', () => ({
  useElectricalState: () => ({
    batterySocPercent: null,
    batteryCapacityAh: null,
    chargingCurrentA: null,
    chargingPowerW: null,
    solarOutputW: null,
    acOutputW: null,
    dc12vPowerW: null,
    dc12vCurrentA: null,
    dc24vVoltageV: null,
    acLoadsW: null,
    generatorRealPowerW: null,
    charger0CurrentA: null,
    charger0AcIn1CurrentA: null,
    charger0ChargingMode: null,
    charger0Error: null,
    batteryRatePercentPerHour: null,
    timeToGoHours: null,
  }),
}))

vi.mock('@/hooks/use-solar-state', () => ({
  useSolarState: () => ({
    currentW: null,
    todayKWh: null,
    yesterdayKWh: null,
    peakTodayW: null,
    controllers: [],
  }),
}))

vi.mock('@/hooks/use-nearby-vessels', () => ({
  useNearbyVessels: () => ({ vessels: [], loading: false }),
}))

vi.mock('@/hooks/use-radar-targets', () => ({
  useRadarTargets: () => ({ targets: [], radars: [], source: 'disabled', loading: false }),
}))

vi.mock('@/hooks/use-anchor-watch', () => ({
  useAnchorWatch: () => ({
    anchorState: anchorStateMock,
    anchorLat: anchorStateMock === 'set' ? -36.8485 : null,
    anchorLon: anchorStateMock === 'set' ? 174.7633 : null,
    radiusMeters: 20,
    rodeDeployedM: 0,
    seaState: 'calm',
    seabedType: 'sand',
    distanceMeters: 30,
    bearingDeg: null,
    setAt: null,
    planningDepthM: null,
    planningTideHeightFt: null,
    lastAutoRaise: lastAutoRaiseMock,
    setAnchorHere: setAnchorHereMock,
    updateRadius: vi.fn(),
    updateRodeAndConditions: vi.fn(),
    updatePlanningDepth: vi.fn(),
    clearAnchor: clearAnchorMock,
  }),
}))

vi.mock('@/hooks/use-place-name', () => ({ usePlaceName: () => null }))

vi.mock('@/hooks/use-tanks-state', () => ({
  useTanksState: () => ({ tanks: [], loading: false }),
}))

vi.mock('@/hooks/use-tide-today', () => ({
  useTideToday: () => ({
    tide: {
      current_tide_height_ft: -1,
      tide_direction: '—',
      high_tide_time: new Date(0).toISOString(),
      high_tide_height_ft: -1,
      low_tide_time: new Date(0).toISOString(),
      low_tide_height_ft: -1,
    },
  }),
}))

vi.mock('@/hooks/use-weather-today', () => ({
  useWeatherToday: () => ({
    weather: {
      temperature_f: -1,
      condition: '—',
      high_temp_f: -1,
      low_temp_f: -1,
      wind_speed_kts: -1,
      wind_direction: '—',
      wind_gust_kts: -1,
      precipitation_pct: -1,
    },
  }),
}))

vi.mock('@/hooks/use-weather-forecast', () => ({
  useWeatherForecast: () => ({ forecast: [], loading: false, error: null, refetch: vi.fn() }),
}))

vi.mock('@/hooks/use-czone-switches', () => ({
  useCZoneSwitches: () => ({ switches: [], loading: false, pending: {}, toggleSwitch: vi.fn() }),
}))

vi.mock('@/hooks/use-depth-trend', () => ({ useDepthTrend: () => ({ points: [], since: 'window' }) }))

vi.mock('@/hooks/use-app-config', () => ({
  useAppConfig: () => ({
    ui: { distanceUnits: 'metric', autoCloseAnchorWatchOnEngine: true },
    anchor: {
      bowRollerHeightM: 0, chainSizeMm: 10, chainOnboardM: 50,
      hullType: 'power_cat', scopeMethod: 'ratio', windageAreaM2: 10,
      gpsFromBowM: 0, loaM: 0,
    },
    loaded: true,
  }),
  publishAppConfigSettings: vi.fn(),
}))

describe('Anchor watch drawer drop button', () => {
  // useVesselState mock above has depth: null, so this is the "no sounder at
  // drop" case (ADR 0063) — setAnchorHere still gets called, now with a
  // third capture argument carrying explicit nulls rather than silently
  // omitting them.
  it('drops anchor from drawer when no watch is active, capturing null depth/tide when there is no sounder reading', async () => {
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: 'Anchor Watch' }))
    const dropButtons = await screen.findAllByRole('button', { name: 'Drop' })
    fireEvent.click(dropButtons[dropButtons.length - 1])

    expect(setAnchorHereMock).toHaveBeenCalledWith(-36.8485, 174.7633, {
      planningDepthM: null,
      planningTideHeightFt: null,
    })
  })

  it('does not show a drawer drop button when anchor watch is active', () => {
    anchorStateMock = 'set'
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: 'Anchor Watch' }))

    expect(screen.queryByRole('button', { name: 'Drop' })).toBeNull()
  })

  it('raises the anchor from the drawer through the confirm dialog when a watch is active', async () => {
    anchorStateMock = 'set'
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: 'Anchor Watch' }))
    const raiseButtons = await screen.findAllByRole('button', { name: 'Raise' })
    fireEvent.click(raiseButtons[raiseButtons.length - 1])

    const dialog = screen.getByRole('alertdialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Raise' }))

    expect(clearAnchorMock).toHaveBeenCalledTimes(1)
  })
})

// ADR 0099: the auto-raise decision moved server-side. Clients learn about
// it through last_auto_raise on the same GET /api/anchor-watch poll every
// client already makes (useAnchorWatch), and each shows the toast once per
// event per client by comparing against what it last saw.
describe('Anchor watch auto-raise toast', () => {
  it('shows the toast once a new last_auto_raise arrives after mount', async () => {
    const { rerender } = render(<App />)

    expect(screen.queryByText(/raised automatically/i)).not.toBeInTheDocument()

    lastAutoRaiseMock = { at: '2026-09-15T00:00:00Z', reason: 'engines_running' }
    rerender(<App />)

    await waitFor(() => expect(screen.getByText(/raised automatically/i)).toBeInTheDocument())
  })

  it('does not toast for a last_auto_raise already present at mount', async () => {
    lastAutoRaiseMock = { at: '2026-09-15T00:00:00Z', reason: 'engines_running' }
    render(<App />)

    // Give the establishing effect a tick, then confirm no toast fired for
    // an event that predates this page load.
    await waitFor(() => expect(screen.getByRole('status')).toBeInTheDocument())
    expect(screen.queryByText(/raised automatically/i)).not.toBeInTheDocument()
  })
})
