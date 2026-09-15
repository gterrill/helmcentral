import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { ProviderGroup } from '@/components/settings/provider-group'
import { SettingsFormProvider } from '@/components/settings/settings-form-context'
import { SecretsStatusProvider } from '@/components/settings/secrets-status-context'

// Guards against a regression of the bug where a provider select/card
// silently pre-selected a hardcoded provider before settings ever loaded,
// and would then get written back to settings.yaml on the next unrelated
// save even though nothing was actually configured. Unlike weather/wave/
// forecast-warnings (which do have an "active if unset" default), tide must
// show NO provider active until either the server reports one configured,
// or the operator picks one themselves.
describe('ProviderGroup tide provider default', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows no tide provider active when none is configured', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: true, json: async () => ({ ui: {} }) })),
    )

    render(
      <SettingsFormProvider>
        <SecretsStatusProvider>
          <ProviderGroup
            type="tide"
            providers={[
              { id: 'bom', name: 'Bureau of Meteorology', description: 'Australian tide data' },
              { id: 'noaa', name: 'NOAA', description: 'US tide data' },
            ]}
          />
        </SecretsStatusProvider>
      </SettingsFormProvider>,
    )

    await waitFor(() => {
      expect(screen.getByText('Bureau of Meteorology')).toBeTruthy()
    })

    expect(screen.queryByText('Active')).toBeNull()
    expect(screen.getAllByText('Inactive')).toHaveLength(2)
  })

  it('defaults weather to open-meteo active when unconfigured', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: true, json: async () => ({ ui: {} }) })),
    )

    render(
      <SettingsFormProvider>
        <SecretsStatusProvider>
          <ProviderGroup
            type="weather"
            providers={[
              { id: 'open-meteo', name: 'Open-Meteo', description: 'Free worldwide weather data' },
              { id: 'weatherkit', name: 'WeatherKit', description: "Apple's weather API" },
            ]}
          />
        </SecretsStatusProvider>
      </SettingsFormProvider>,
    )

    await waitFor(() => {
      expect(screen.getByText('Active')).toBeTruthy()
    })

    const openMeteoSwitch = screen.getByRole('switch', { name: /activate open-meteo/i })
    expect(openMeteoSwitch).toHaveAttribute('aria-disabled', 'true')
  })

  it('defaults poi to osm-overpass active when unconfigured', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: true, json: async () => ({ ui: {} }) })),
    )

    render(
      <SettingsFormProvider>
        <SecretsStatusProvider>
          <ProviderGroup
            type="poi"
            providers={[
              { id: 'osm-overpass', name: 'OpenStreetMap (Overpass)', description: 'Free, keyless POI data from OpenStreetMap' },
              { id: 'google-places', name: 'Google Places', description: "Google's Places API (New)" },
            ]}
          />
        </SecretsStatusProvider>
      </SettingsFormProvider>,
    )

    await waitFor(() => {
      expect(screen.getByText('Active')).toBeTruthy()
    })

    const osmSwitch = screen.getByRole('switch', { name: /activate openstreetmap/i })
    expect(osmSwitch).toHaveAttribute('aria-disabled', 'true')
  })

  // ADR 0101: ui.place_name_provider is its own setting, independent of
  // ui.poi_provider, but defaults to the same plugin (osm-overpass) so a
  // fresh install needs no configuration either.
  it('defaults place-names to osm-overpass active when unconfigured', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: true, json: async () => ({ ui: {} }) })),
    )

    render(
      <SettingsFormProvider>
        <SecretsStatusProvider>
          <ProviderGroup
            type="place-names"
            providers={[
              { id: 'osm-overpass', name: 'OpenStreetMap (Overpass)', description: 'Free, keyless place names from OpenStreetMap' },
            ]}
          />
        </SecretsStatusProvider>
      </SettingsFormProvider>,
    )

    await waitFor(() => {
      expect(screen.getByText('Active')).toBeTruthy()
    })

    const osmSwitch = screen.getByRole('switch', { name: /activate openstreetmap/i })
    expect(osmSwitch).toHaveAttribute('aria-disabled', 'true')
  })

  // The place-names picker and the Nearby picker can name the same
  // installed POI plugin, but there is no "place-names" plugin kind on the
  // backend - the gear on a place-names card must open the same "poi"
  // settings modal a Nearby card would (same GET/POST /api/plugins/poi/...
  // endpoints), not a nonexistent /api/plugins/place-names/... one.
  it('opens the poi settings modal (not a place-names one) from a place-names card', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url === '/api/plugins/poi/osm-overpass') {
        return {
          ok: true,
          json: async () => ({
            type: 'poi',
            id: 'osm-overpass',
            name: 'OpenStreetMap (Overpass)',
            description: 'Free, keyless place names from OpenStreetMap',
            sandboxed: true,
            allowed_hosts: [],
            allowed_hosts_overridden: false,
            allowed_secrets: [],
            allowed_secrets_overridden: false,
            config_fields: [],
          }),
        }
      }
      return { ok: true, json: async () => ({ ui: {} }) }
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <SettingsFormProvider>
        <SecretsStatusProvider>
          <ProviderGroup
            type="place-names"
            providers={[
              { id: 'osm-overpass', name: 'OpenStreetMap (Overpass)', description: 'Free, keyless place names from OpenStreetMap' },
            ]}
          />
        </SecretsStatusProvider>
      </SettingsFormProvider>,
    )

    await waitFor(() => {
      expect(screen.getByText('OpenStreetMap (Overpass)')).toBeTruthy()
    })
    screen.getByRole('button', { name: 'Settings' }).click()

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith('/api/plugins/poi/osm-overpass')
    })
  })
})
