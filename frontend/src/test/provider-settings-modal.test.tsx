import { describe, it, expect, vi, afterEach } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { ProviderSettingsModal, type ProviderDomain } from '@/components/settings/provider-settings-modal'
import { SecretsStatusProvider } from '@/components/settings/secrets-status-context'

const secretsGetResponse = {
  SIGNALK_USERNAME: false,
  SIGNALK_PASSWORD: false,
  INFLUXDB_TOKEN: false,
  WEATHERKIT_KEY_ID: false,
  WEATHERKIT_TEAM_ID: false,
  WEATHERKIT_SERVICE_ID: false,
  WEATHERKIT_PRIVATE_KEY: false,
}

function renderModal(type: ProviderDomain, providerId: string) {
  return render(
    <SecretsStatusProvider>
      <ProviderSettingsModal type={type} providerId={providerId} open onOpenChange={() => {}} />
    </SecretsStatusProvider>,
  )
}

describe('ProviderSettingsModal', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  // Every provider registered anywhere in the app (tide/weather/wave/
  // forecast-warnings) is a WASM plugin - see
  // backend/plugin_overrides_handlers.go - so once a provider's info has
  // loaded, `info.sandboxed` is always true and the modal always renders
  // the allowlist editor. The weatherkit test below covers the case where a
  // provider is both sandboxed AND has entries in PROVIDER_SECRET_FIELDS,
  // asserting the secret fields and the allowlist editor render together.

  it('renders no secret fields and the allowlist editor for the sandboxed bom tide provider', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        if (url.includes('/api/plugins/tide/bom')) {
          return {
            ok: true,
            json: async () => ({
              type: 'tide',
              id: 'bom',
              name: 'Bureau of Meteorology',
              description: 'Australian tide data',
              sandboxed: true,
              allowed_hosts: ['bom.gov.au'],
              allowed_hosts_overridden: false,
              allowed_secrets: [],
              allowed_secrets_overridden: false,
            }),
          }
        }
        if (url.includes('/api/settings/secrets')) {
          return { ok: true, json: async () => secretsGetResponse }
        }
        return { ok: false, json: async () => ({}) }
      }),
    )

    renderModal('tide', 'bom')

    await waitFor(() => {
      expect(screen.getByLabelText('Allowed Hosts')).toBeTruthy()
    })

    expect(screen.queryByLabelText('WeatherKit Key ID')).toBeNull()
    expect(screen.getByText(/Allowlist changes require a backend restart/i)).toBeTruthy()
  })

  it('Save posts the edited allowlists and Reset issues a DELETE, both updating from the response', async () => {
    const fetchMock = vi.fn(async (url: string, options?: { method?: string; body?: string }) => {
      if (url.includes('/api/plugins/tide/bom/overrides') && options?.method === 'POST') {
        const body = JSON.parse(options.body!)
        expect(body.allowed_hosts).toEqual(['bom.gov.au', 'example.com'])
        return {
          ok: true,
          json: async () => ({
            type: 'tide',
            id: 'bom',
            name: 'Bureau of Meteorology',
            description: 'Australian tide data',
            sandboxed: true,
            allowed_hosts: ['bom.gov.au', 'example.com'],
            allowed_hosts_overridden: true,
            allowed_secrets: [],
            allowed_secrets_overridden: true,
          }),
        }
      }
      if (url.includes('/api/plugins/tide/bom/overrides') && options?.method === 'DELETE') {
        return {
          ok: true,
          json: async () => ({
            type: 'tide',
            id: 'bom',
            name: 'Bureau of Meteorology',
            description: 'Australian tide data',
            sandboxed: true,
            allowed_hosts: ['bom.gov.au'],
            allowed_hosts_overridden: false,
            allowed_secrets: [],
            allowed_secrets_overridden: false,
          }),
        }
      }
      if (url.includes('/api/plugins/tide/bom')) {
        return {
          ok: true,
          json: async () => ({
            type: 'tide',
            id: 'bom',
            name: 'Bureau of Meteorology',
            description: 'Australian tide data',
            sandboxed: true,
            allowed_hosts: ['bom.gov.au'],
            allowed_hosts_overridden: false,
            allowed_secrets: [],
            allowed_secrets_overridden: false,
          }),
        }
      }
      if (url.includes('/api/settings/secrets')) {
        return { ok: true, json: async () => secretsGetResponse }
      }
      return { ok: false, json: async () => ({}) }
    })
    vi.stubGlobal('fetch', fetchMock)

    renderModal('tide', 'bom')

    await waitFor(() => {
      expect(screen.getByLabelText('Allowed Hosts')).toBeTruthy()
    })

    const hostsInput = screen.getByLabelText('Allowed Hosts') as HTMLInputElement
    fireEvent.change(hostsInput, { target: { value: 'bom.gov.au, example.com' } })

    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/plugins/tide/bom/overrides',
        expect.objectContaining({ method: 'POST' }),
      )
    })

    fireEvent.click(screen.getByRole('button', { name: /reset to defaults/i }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/plugins/tide/bom/overrides',
        expect.objectContaining({ method: 'DELETE' }),
      )
    })

    await waitFor(() => {
      expect((screen.getByLabelText('Allowed Hosts') as HTMLInputElement).value).toBe('bom.gov.au')
    })
  })

  it('renders both the secret fields and the allowlist editor for the sandboxed weatherkit provider', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        if (url.includes('/api/plugins/weather/weatherkit')) {
          return {
            ok: true,
            json: async () => ({
              type: 'weather',
              id: 'weatherkit',
              name: 'WeatherKit',
              description: "Apple's weather API",
              sandboxed: true,
              allowed_hosts: ['weatherkit.apple.com'],
              allowed_hosts_overridden: false,
              allowed_secrets: ['WEATHERKIT_KEY_ID'],
              allowed_secrets_overridden: false,
            }),
          }
        }
        if (url.includes('/api/settings/secrets')) {
          return { ok: true, json: async () => secretsGetResponse }
        }
        return { ok: false, json: async () => ({}) }
      }),
    )

    renderModal('weather', 'weatherkit')

    await waitFor(() => {
      expect(screen.getByLabelText('Allowed Hosts')).toBeTruthy()
    })

    expect(screen.getByLabelText('WeatherKit Key ID')).toBeTruthy()
    expect(screen.getByLabelText('WeatherKit Team ID')).toBeTruthy()
    expect(screen.getByLabelText('WeatherKit Service ID')).toBeTruthy()
    expect(screen.getByLabelText('WeatherKit Private Key')).toBeTruthy()
    expect(screen.getByText(/Allowlist changes require a backend restart/i)).toBeTruthy()
  })
})

// ── plugin-declared config fields (ADR 0100 rewrite: "plugins declare
// their own settings") ─────────────────────────────────────────────────────
//
// osm-overpass declares one field, "overpass_url" (label "Overpass
// server"), used here as the representative example throughout - the same
// shape any future plugin's config_fields.json produces.

const osmOverpassInfo = (storedMirrorURL: string) => ({
  type: 'poi',
  id: 'osm-overpass',
  name: 'OpenStreetMap via Overpass',
  description: 'Free, keyless POI data from OpenStreetMap',
  sandboxed: true,
  allowed_hosts: ['overpass-api.de', 'overpass.openstreetmap.fr'],
  allowed_hosts_overridden: false,
  allowed_secrets: [],
  allowed_secrets_overridden: false,
  config_fields: [
    {
      key: 'overpass_url',
      label: 'Overpass server',
      type: 'url',
      help: 'Blank uses the public overpass-api.de.',
      placeholder: 'https://overpass-api.de/api/interpreter',
      value: storedMirrorURL,
    },
  ],
})

describe('ProviderSettingsModal config fields', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders a declared config field with its stored value and help text', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        if (url.includes('/api/plugins/poi/osm-overpass')) {
          return { ok: true, json: async () => osmOverpassInfo('https://overpass.openstreetmap.fr/api/interpreter') }
        }
        if (url.includes('/api/settings/secrets')) {
          return { ok: true, json: async () => secretsGetResponse }
        }
        return { ok: false, json: async () => ({}) }
      }),
    )

    renderModal('poi', 'osm-overpass')

    await waitFor(() => {
      expect(screen.getByLabelText('Overpass server')).toBeTruthy()
    })

    expect((screen.getByLabelText('Overpass server') as HTMLInputElement).value).toBe(
      'https://overpass.openstreetmap.fr/api/interpreter',
    )
    expect(screen.getByText('Blank uses the public overpass-api.de.')).toBeTruthy()
    expect(screen.getByText(/Applies on the next call/i)).toBeTruthy()
  })

  it('Save posts the edited config field values to /config', async () => {
    const fetchMock = vi.fn(async (url: string, options?: { method?: string; body?: string }) => {
      if (url.includes('/api/plugins/poi/osm-overpass/config') && options?.method === 'POST') {
        const body = JSON.parse(options.body!)
        expect(body.values).toEqual({ overpass_url: 'https://overpass.kumi.systems/api/interpreter' })
        return { ok: true, json: async () => osmOverpassInfo('https://overpass.kumi.systems/api/interpreter') }
      }
      if (url.includes('/api/plugins/poi/osm-overpass/overrides') && options?.method === 'POST') {
        return { ok: true, json: async () => osmOverpassInfo('https://overpass.kumi.systems/api/interpreter') }
      }
      if (url.includes('/api/plugins/poi/osm-overpass')) {
        return { ok: true, json: async () => osmOverpassInfo('') }
      }
      if (url.includes('/api/settings/secrets')) {
        return { ok: true, json: async () => secretsGetResponse }
      }
      return { ok: false, json: async () => ({}) }
    })
    vi.stubGlobal('fetch', fetchMock)

    renderModal('poi', 'osm-overpass')

    await waitFor(() => {
      expect(screen.getByLabelText('Overpass server')).toBeTruthy()
    })

    fireEvent.change(screen.getByLabelText('Overpass server'), {
      target: { value: 'https://overpass.kumi.systems/api/interpreter' },
    })
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/plugins/poi/osm-overpass/config',
        expect.objectContaining({ method: 'POST' }),
      )
    })

    await waitFor(() => {
      expect((screen.getByLabelText('Overpass server') as HTMLInputElement).value).toBe(
        'https://overpass.kumi.systems/api/interpreter',
      )
    })
  })

  it('shows the 400 error message from a rejected config save inline', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, options?: { method?: string }) => {
        if (url.includes('/api/plugins/poi/osm-overpass/config') && options?.method === 'POST') {
          return { ok: false, status: 400, json: async () => ({ error: 'Overpass server must be an absolute http(s) URL' }) }
        }
        if (url.includes('/api/plugins/poi/osm-overpass')) {
          return { ok: true, json: async () => osmOverpassInfo('') }
        }
        if (url.includes('/api/settings/secrets')) {
          return { ok: true, json: async () => secretsGetResponse }
        }
        return { ok: false, json: async () => ({}) }
      }),
    )

    renderModal('poi', 'osm-overpass')

    await waitFor(() => {
      expect(screen.getByLabelText('Overpass server')).toBeTruthy()
    })

    fireEvent.change(screen.getByLabelText('Overpass server'), { target: { value: 'not-a-url' } })
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => {
      expect(screen.getByText('Overpass server must be an absolute http(s) URL')).toBeTruthy()
    })
  })
})
