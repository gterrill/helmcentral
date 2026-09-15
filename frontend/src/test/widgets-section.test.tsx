import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'

import { WidgetsSection } from '@/components/settings/sections/widgets-section'
import { SettingsFormProvider } from '@/components/settings/settings-form-context'
import { SecretsStatusProvider } from '@/components/settings/secrets-status-context'
import { initialRegularSettingsDraft } from '@/components/settings/settings-draft'
import { useForecastWarningsProviders } from '@/hooks/use-forecast-warnings-providers'
import { usePlaceNameProviders } from '@/hooks/use-place-name-providers'
import { usePOIProviders } from '@/hooks/use-poi-providers'
import { useTideProviders } from '@/hooks/use-tide-providers'
import { useWaveProviders } from '@/hooks/use-wave-providers'
import { useWeatherProviders } from '@/hooks/use-weather-providers'

vi.mock('@/hooks/use-tide-providers')
vi.mock('@/hooks/use-weather-providers')
vi.mock('@/hooks/use-wave-providers')
vi.mock('@/hooks/use-poi-providers')
vi.mock('@/hooks/use-forecast-warnings-providers')
vi.mock('@/hooks/use-place-name-providers')

const mockedUseTideProviders = vi.mocked(useTideProviders)
const mockedUseWeatherProviders = vi.mocked(useWeatherProviders)
const mockedUseWaveProviders = vi.mocked(useWaveProviders)
const mockedUsePOIProviders = vi.mocked(usePOIProviders)
const mockedUseForecastWarningsProviders = vi.mocked(useForecastWarningsProviders)
const mockedUsePlaceNameProviders = vi.mocked(usePlaceNameProviders)

// The Nearby tab (ui.poi_provider) added alongside the poi plugin kind
// (docs/adr/0091) - this guards the tab existing and rendering the
// registered POI providers, mirroring the existing tide/weather/wave/
// forecast-warnings tabs' wiring in this same section.
describe('WidgetsSection Nearby tab', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('lists the registered POI providers under the Nearby tab', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: true, json: async () => ({ ui: {} }) })),
    )
    mockedUseTideProviders.mockReturnValue({ providers: [], loading: false })
    mockedUseWeatherProviders.mockReturnValue({ providers: [], loading: false })
    mockedUseWaveProviders.mockReturnValue({ providers: [], loading: false })
    mockedUseForecastWarningsProviders.mockReturnValue({ providers: [], loading: false })
    mockedUsePOIProviders.mockReturnValue({
      providers: [
        { id: 'osm-overpass', name: 'OpenStreetMap (Overpass)', description: 'Free, keyless POI data' },
        { id: 'google-places', name: 'Google Places', description: "Google's Places API (New)" },
      ],
      loading: false,
    })
    mockedUsePlaceNameProviders.mockReturnValue({ providers: [], loading: false })

    render(
      <SettingsFormProvider>
        <SecretsStatusProvider>
          <WidgetsSection draft={initialRegularSettingsDraft} onChange={() => {}} />
        </SecretsStatusProvider>
      </SettingsFormProvider>,
    )

    fireEvent.click(screen.getByRole('tab', { name: 'Nearby' }))

    await waitFor(() => {
      expect(screen.getByText('OpenStreetMap (Overpass)')).toBeTruthy()
    })
    expect(screen.getByText('Google Places')).toBeTruthy()
  })
})

// ui.place_name_provider (ADR 0101) - a separate tab from Nearby, since the
// operator can point the two at different plugins even though both default
// to osm-overpass.
describe('WidgetsSection Place names tab', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('lists the registered place-name providers under the Place names tab and saves a selection', async () => {
    const saved: unknown[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString()
        if (url.endsWith('/api/settings') && init?.method === 'POST') {
          saved.push(JSON.parse(String(init.body)))
        }
        return { ok: true, json: async () => ({ ui: {} }) }
      }),
    )
    mockedUseTideProviders.mockReturnValue({ providers: [], loading: false })
    mockedUseWeatherProviders.mockReturnValue({ providers: [], loading: false })
    mockedUseWaveProviders.mockReturnValue({ providers: [], loading: false })
    mockedUseForecastWarningsProviders.mockReturnValue({ providers: [], loading: false })
    mockedUsePOIProviders.mockReturnValue({ providers: [], loading: false })
    mockedUsePlaceNameProviders.mockReturnValue({
      providers: [
        { id: 'osm-overpass', name: 'OpenStreetMap (Overpass)', description: 'Free, keyless place names' },
        { id: 'another-osm', name: 'Another OSM Mirror', description: 'A second place-names plugin' },
      ],
      loading: false,
    })

    render(
      <SettingsFormProvider>
        <SecretsStatusProvider>
          <WidgetsSection draft={initialRegularSettingsDraft} onChange={() => {}} />
        </SecretsStatusProvider>
      </SettingsFormProvider>,
    )

    fireEvent.click(screen.getByRole('tab', { name: 'Place names' }))

    await waitFor(() => {
      expect(screen.getByText('OpenStreetMap (Overpass)')).toBeTruthy()
    })
    expect(screen.getByText('Another OSM Mirror')).toBeTruthy()

    // osm-overpass is already the default-active card (unconfigured), so
    // activate the other one to prove a selection actually saves
    // ui.place_name_provider.
    fireEvent.click(screen.getByRole('switch', { name: /activate another osm mirror/i }))

    await waitFor(() => {
      expect(
        saved.some((body) => (body as { ui?: { place_name_provider?: string } }).ui?.place_name_provider === 'another-osm'),
      ).toBe(true)
    })
  })
})
