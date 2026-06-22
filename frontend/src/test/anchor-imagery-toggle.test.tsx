import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { App } from '../App'

vi.mock('@/components/anchor-watch-map', () => ({
  AnchorWatchMap: (props: {
    showImageryLayer?: boolean
    onImageryToggle?: (enabled: boolean) => void
  }) => (
    <div>
      <div data-testid="imagery-state">{props.showImageryLayer ? 'on' : 'off'}</div>
      <button
        aria-label="Toggle satellite imagery"
        onClick={() => props.onImageryToggle?.(!props.showImageryLayer)}
      >
        sat
      </button>
    </div>
  ),
}))

vi.mock('@/hooks/use-vessel-state', () => ({
  useVesselState: () => ({
    depth: 3.2,
    currentDriftKts: 0,
    currentSetDeg: 0,
    navigationState: 'at_anchor',
    latitude: -25.2939,
    longitude: 152.9103,
    gnssQualityIndicator: null,
    gnssHdop: null,
    gnssSatellites: null,
    gnssValidationState: null,
    gnssValidationReason: null,
    gnssCriticalAlert: false,
    headingTrue: 265,
    speedOverGroundKts: 0,
    windSpeedApparentKts: 5,
    windAngleApparentDeg: 220,
    windSide: 'port',
    windAngleRelativeDeg: 130,
    maxGust10mKts: 7,
    maxGust1hKts: 12,
    generatorState: 'stopped',
    generatorManualStart: false,
    generatorManualStartTimer: 0,
    generatorRunningByCondition: null,
    generatorRuntime: 0,
  }),
}))

vi.mock('@/hooks/use-electrical-state', () => ({
  useElectricalState: () => ({
    batterySocPercent: 90,
    batteryCapacityAh: 1000,
    chargingCurrentA: 2,
    chargingPowerW: 60,
    solarOutputW: 300,
    acOutputW: 0,
    dc12vPowerW: 0,
    dc12vCurrentA: 0,
    dc24vVoltageV: 27.8,
    acLoadsW: 0,
    generatorRealPowerW: 0,
    alternator0: null,
    alternator1: null,
    batteryRatePercentPerHour: 0.2,
    timeToGoHours: 1,
  }),
}))

vi.mock('@/hooks/use-nearby-vessels', () => ({
  useNearbyVessels: () => ({ vessels: [], loading: false }),
}))

vi.mock('@/hooks/use-anchor-watch', () => ({
  useAnchorWatch: () => ({
    anchorState: 'set',
    anchorLat: -25.2939,
    anchorLon: 152.9103,
    radiusMeters: 34,
    rodeDeployedM: 0,
    seaState: 'calm',
    seabedType: 'sand',
    distanceMeters: 26,
    bearingDeg: 81,
    suggestSet: false,
    setAnchorHere: vi.fn(),
    updatePosition: vi.fn(),
    updateRadius: vi.fn(),
    updateRodeAndConditions: vi.fn(),
    clearAnchor: vi.fn(),
  }),
}))

vi.mock('@/hooks/use-server-trails', () => ({
  useServerTrails: () => ({
    getSelfTrail: () => [],
    getAisTrails: () => new Map(),
  }),
}))

vi.mock('@/hooks/use-place-name', () => ({ usePlaceName: () => 'Urangan' }))

vi.mock('@/hooks/use-tanks-state', () => ({
  useTanksState: () => ({ tanks: [], loading: false }),
}))

vi.mock('@/hooks/use-tide-today', () => ({
  useTideToday: () => ({
    tide: {
      current_tide_height_ft: 0,
      tide_direction: 'Falling',
      high_tide_time: new Date().toISOString(),
      high_tide_height_ft: 2,
      low_tide_time: new Date().toISOString(),
      low_tide_height_ft: 0,
    },
  }),
}))

vi.mock('@/hooks/use-weather-today', () => ({
  useWeatherToday: () => ({
    weather: {
      temperature_f: 68,
      condition: 'Clear',
      high_temp_f: 72,
      low_temp_f: 60,
      wind_speed_kts: 8,
      wind_direction: 'ESE',
      wind_gust_kts: 10,
      precipitation_pct: 0,
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
    bowRollerHeightM: 0,
    chainSizeMm: 10,
    chainOnboardM: 50,
    hullType: 'power_cat',
    windageAreaM2: 10,
  },
}))

describe('Anchor imagery toggle wiring', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, json: async () => ({}) }))
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  it('toggles imagery state through App -> Tile -> Map path', () => {
    render(<App />)

    expect(screen.getByTestId('imagery-state')).toHaveTextContent('off')

    fireEvent.click(screen.getByLabelText('Toggle satellite imagery'))
    expect(screen.getByTestId('imagery-state')).toHaveTextContent('on')
  })
})
