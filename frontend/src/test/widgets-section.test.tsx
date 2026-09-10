import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'

import { WidgetsSection } from '@/components/settings/sections/widgets-section'
import { SettingsFormProvider } from '@/components/settings/settings-form-context'
import { SecretsStatusProvider } from '@/components/settings/secrets-status-context'
import { initialRegularSettingsDraft } from '@/components/settings/settings-draft'
import { useForecastWarningsProviders } from '@/hooks/use-forecast-warnings-providers'
import { usePOIProviders } from '@/hooks/use-poi-providers'
import { useTideProviders } from '@/hooks/use-tide-providers'
import { useWaveProviders } from '@/hooks/use-wave-providers'
import { useWeatherProviders } from '@/hooks/use-weather-providers'

vi.mock('@/hooks/use-tide-providers')
vi.mock('@/hooks/use-weather-providers')
vi.mock('@/hooks/use-wave-providers')
vi.mock('@/hooks/use-poi-providers')
vi.mock('@/hooks/use-forecast-warnings-providers')

const mockedUseTideProviders = vi.mocked(useTideProviders)
const mockedUseWeatherProviders = vi.mocked(useWeatherProviders)
const mockedUseWaveProviders = vi.mocked(useWaveProviders)
const mockedUsePOIProviders = vi.mocked(usePOIProviders)
const mockedUseForecastWarningsProviders = vi.mocked(useForecastWarningsProviders)

// The Nearby tab (ui.poi_provider) added alongside the poi plugin kind
// (docs/adr/0090) - this guards the tab existing and rendering the
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
