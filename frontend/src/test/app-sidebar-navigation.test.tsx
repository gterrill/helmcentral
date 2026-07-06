/**
 * Sidebar navigation test: mounts <App> with all hooks mocked (same pattern
 * as App.smoke.test.tsx) and asserts that the shadcn Sidebar drives panel
 * navigation in place of the old BottomDrawer:
 *  - the dashboard grid is visible by default (Sidebar's "Dashboard" nav item present)
 *  - clicking the "Tides" sidebar nav button swaps the content region to the tide panel
 *  - clicking "Dashboard" again returns to the grid view
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { App } from '../App'

// ── stub fetch so components that call it don't throw ─────────────────────────
beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: false,
    json: async () => ({}),
  }))
})

// ── hook mocks (mirrors App.smoke.test.tsx) ────────────────────────────────────

vi.mock('@/hooks/use-vessel-state', () => ({
  useVesselState: () => ({
    depth: null, currentDriftKts: null, currentSetDeg: null, navigationState: null, latitude: null, longitude: null,
    headingTrue: null, speedOverGroundKts: null, windSpeedApparentKts: null,
    windAngleApparentDeg: null, windSide: null, windAngleRelativeDeg: null,
    maxGust10mKts: null, maxGust1hKts: null, generatorState: null,
    generatorManualStart: false, generatorManualStartTimer: 0,
    generatorRunningByCondition: null, generatorRuntime: null,
    engine0Rpm: null, engine1Rpm: null, gnssQualityIndicator: null, gnssHdop: null,
    gnssValidationState: null, gnssValidationReason: null, gnssCriticalAlert: false
  }),
}))

vi.mock('@/hooks/use-electrical-state', () => ({
  useElectricalState: () => ({
    batterySocPercent: null, batteryCapacityAh: null, chargingCurrentA: null,
    chargingPowerW: null, solarOutputW: null, acOutputW: null, dc12vPowerW: null,
    dc12vCurrentA: null, dc24vVoltageV: null, acLoadsW: null,
    generatorRealPowerW: null, batteryRatePercentPerHour: null, timeToGoHours: null,
  }),
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

// ── tests ─────────────────────────────────────────────────────────────────────

describe('App sidebar navigation', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  it('shows the dashboard by default, navigates to a panel via the sidebar, and back', () => {
    render(<App />)

    // Dashboard grid visible by default — "Depth & Tide" tile is part of the grid.
    expect(screen.getByText('Depth & Tide')).toBeInTheDocument()

    // Sidebar nav has a "Dashboard" item, active by default. It's the first
    // "Dashboard"-labelled button (a "Back to dashboard" button also appears
    // once a panel is open, so disambiguate by taking the nav item).
    const dashboardNavButton = screen.getAllByRole('button', { name: /dashboard/i })[0]
    expect(dashboardNavButton).toBeInTheDocument()

    // Click "Tides" in the sidebar nav.
    const tidesNavButton = screen.getByRole('button', { name: /tides/i })
    fireEvent.click(tidesNavButton)

    // The dashboard grid tile content should no longer be present; tide panel content should be.
    expect(screen.queryByText('Depth & Tide')).not.toBeInTheDocument()
    expect(screen.getAllByText(/tides/i).length).toBeGreaterThan(0)

    // Navigate back to the dashboard via the sidebar nav item.
    fireEvent.click(screen.getAllByRole('button', { name: /dashboard/i })[0])
    expect(screen.getByText('Depth & Tide')).toBeInTheDocument()
  })
})
