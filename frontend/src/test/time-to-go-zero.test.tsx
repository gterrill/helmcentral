import { describe, it, vi, beforeEach, expect } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import { App } from '../App'

const electricalStateMock = vi.hoisted(() => ({
  batterySocPercent: 100,
  batteryCapacityAh: 1440,
  chargingCurrentA: -73.8,
  chargingPowerW: -1988,
  solarOutputW: 0,
  acOutputW: 1494,
  dc12vPowerW: 422.2,
  dc12vCurrentA: 15.7,
  dc24vVoltageV: 26.9,
  acLoadsW: 1494,
  generatorRealPowerW: 0,
  alternator0: { currentA: null, voltageV: null, powerW: null, temperatureC: null },
  alternator1: { currentA: null, voltageV: null, powerW: null, temperatureC: null },
  batteryRatePercentPerHour: -0.2,
  timeToGoHours: 0,
}))

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: false,
    json: async () => ({}),
  }))
  vi.spyOn(console, 'error').mockImplementation(() => {})
  electricalStateMock.timeToGoHours = 0
})

vi.mock('@/hooks/use-vessel-state', () => ({
  useVesselState: () => ({
    depth: null, currentDriftKts: null, currentSetDeg: null, navigationState: null, latitude: null, longitude: null,
    headingTrue: null, speedOverGroundKts: null, windSpeedApparentKts: null,
    windAngleApparentDeg: null, windSide: null, windAngleRelativeDeg: null,
    maxGust10mKts: null, maxGust1hKts: null, generatorState: null,
    generatorManualStart: false, generatorManualStartTimer: 0,
    generatorRunningByCondition: null, generatorRuntime: null,
    engine0Rpm: null, engine1Rpm: null, gnssQualityIndicator: null, gnssHdop: null,
    gnssValidationState: null, gnssValidationReason: null, gnssCriticalAlert: false,
  }),
}))

vi.mock('@/hooks/use-electrical-state', () => ({
  useElectricalState: () => electricalStateMock,
}))

vi.mock('@/hooks/use-nearby-vessels', () => ({
  useNearbyVessels: () => ({ vessels: [], loading: false }),
}))

vi.mock('@/hooks/use-anchor-watch', () => ({
  useAnchorWatch: () => ({
    anchorState: 'none', anchorLat: null, anchorLon: null, radiusMeters: 0,
    rodeDeployedM: 0, seaState: 'calm', seabedType: 'sand',
    distanceMeters: null, bearingDeg: null, suggestSet: false,
    setAnchorHere: vi.fn(), updateRadius: vi.fn(),
    updateRodeAndConditions: vi.fn(), clearAnchor: vi.fn(),
  }),
}))

vi.mock('@/hooks/use-place-name', () => ({ usePlaceName: () => null }))

vi.mock('@/hooks/use-tanks-state', () => ({
  useTanksState: () => ({ tanks: [], loading: false }),
}))

vi.mock('@/hooks/use-tide-today', () => ({
  useTideToday: () => ({
    tide: {
      current_tide_height_ft: -1, tide_direction: '—',
      high_tide_time: new Date(0).toISOString(), high_tide_height_ft: -1,
      low_tide_time: new Date(0).toISOString(), low_tide_height_ft: -1,
    },
  }),
}))

vi.mock('@/hooks/use-weather-today', () => ({
  useWeatherToday: () => ({
    weather: {
      temperature_f: -1, condition: '—', high_temp_f: -1, low_temp_f: -1,
      wind_speed_kts: -1, wind_direction: '—', wind_gust_kts: -1, precipitation_pct: -1,
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
  uiConfig: { vesselStateRefreshSeconds: 10, distanceUnits: 'metric' },
  anchorConfig: {
    bowRollerHeightM: 0, chainSizeMm: 10, chainOnboardM: 50,
    hullType: 'power_cat', windageAreaM2: 10,
  },
}))

describe('Battery tile time to go', () => {
  it('shows an em dash when time to go computes to zero', () => {
    render(<App />)

    const label = screen.getByText('Time Remaining')
    const card = label.closest('div')
    expect(card).not.toBeNull()
    expect(within(card as HTMLDivElement).getByText('—')).toBeInTheDocument()
  })

  it('shows rounded weeks for very large remaining time', () => {
    electricalStateMock.timeToGoHours = 2057.15

    render(<App />)

    const label = screen.getByText('Time Remaining')
    const card = label.closest('div')
    expect(card).not.toBeNull()
    expect(within(card as HTMLDivElement).getByText('12w')).toBeInTheDocument()
  })
})
