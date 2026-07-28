import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { App } from '../App'

const setAnchorHereMock = vi.fn()
let anchorStateMock: 'none' | 'set' = 'none'

beforeEach(() => {
  vi.clearAllMocks()
  anchorStateMock = 'none'
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
    latitude: -36.8485,
    longitude: 174.7633,
    gnssQualityIndicator: null,
    gnssHdop: null,
    gnssSatellites: null,
    gnssValidationState: null,
    gnssValidationReason: null,
    gnssCriticalAlert: false,
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

vi.mock('@/hooks/use-anchor-watch', () => ({
  useAnchorWatch: () => ({
    anchorState: anchorStateMock,
    anchorLat: anchorStateMock === 'set' ? -36.8485 : null,
    anchorLon: anchorStateMock === 'set' ? 174.7633 : null,
    radiusMeters: 20,
    rodeDeployedM: 0,
    seaState: 'calm',
    seabedType: 'sand',
    distanceMeters: null,
    bearingDeg: null,
    suggestSet: false,
    setAt: null,
    setAnchorHere: setAnchorHereMock,
    updatePosition: vi.fn(),
    updateRadius: vi.fn(),
    updateRodeAndConditions: vi.fn(),
    clearAnchor: vi.fn(),
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

vi.mock('@/config/app-config', () => ({
  appConfig: { boat: { name: 'Test Vessel', model: 'Test' } },
  FORECAST_REFRESH_SECONDS: 600,
  getUiConfig: () => ({ vesselStateRefreshSeconds: 10, distanceUnits: 'metric' }),
  getAnchorConfig: () => ({
    bowRollerHeightM: 0,
    chainSizeMm: 10,
    chainOnboardM: 50,
    hullType: 'power_cat',
    windageAreaM2: 10,
  }),
}))

describe('Anchor watch drawer drop button', () => {
  it('drops anchor from drawer when no watch is active', () => {
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: 'Anchor Watch' }))
    const dropButtons = screen.getAllByRole('button', { name: 'Drop' })
    fireEvent.click(dropButtons[dropButtons.length - 1])

    expect(setAnchorHereMock).toHaveBeenCalledWith(-36.8485, 174.7633)
  })

  it('does not show a drawer drop button when anchor watch is active', () => {
    anchorStateMock = 'set'
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: 'Anchor Watch' }))

    expect(screen.queryByRole('button', { name: 'Drop' })).toBeNull()
  })
})
