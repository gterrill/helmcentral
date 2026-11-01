import { describe, it, expect, vi, afterEach } from 'vitest'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { getQueryData, invalidateQueries, setQueryData } from '@/lib/api-query'
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

function renderModal(type: ProviderDomain, providerId: string, onOpenChange: (open: boolean) => void = () => {}) {
  return render(
    <SecretsStatusProvider>
      <ProviderSettingsModal type={type} providerId={providerId} open onOpenChange={onOpenChange} />
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
    expect(screen.getByLabelText('Allowed Secrets')).not.toHaveAttribute('placeholder')
    const overlay = document.querySelector('[role="presentation"][data-open]')
    expect(overlay).not.toBeNull()
    expect(overlay!.className).toEqual(expect.stringContaining('z-70'))
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

  it('keeps a typed draft when a background refetch of the provider lands', async () => {
    let stored = 'https://first.example/api/interpreter'
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        if (url.includes('/api/plugins/poi/osm-overpass')) {
          return { ok: true, status: 200, json: async () => osmOverpassInfo(stored) }
        }
        if (url.includes('/api/settings/secrets')) {
          return { ok: true, status: 200, json: async () => secretsGetResponse }
        }
        return { ok: false, json: async () => ({}) }
      }),
    )

    renderModal('poi', 'osm-overpass')
    const input = (await screen.findByLabelText('Overpass server')) as HTMLInputElement
    await waitFor(() => expect(input.value).toBe('https://first.example/api/interpreter'))
    fireEvent.change(input, { target: { value: 'https://typed.example/api/interpreter' } })

    stored = 'https://second.example/api/interpreter'
    await act(async () => {
      invalidateQueries(['/api/plugins/poi/osm-overpass'])
      await Promise.resolve()
    })
    await waitFor(() =>
      expect(getQueryData<{ config_fields: { value: string }[] }>(['/api/plugins/poi/osm-overpass'])?.config_fields[0].value).toBe(
        'https://second.example/api/interpreter',
      ),
    )
    expect(input.value).toBe('https://typed.example/api/interpreter')
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

  it('compares later saves with what the server stored, not what was typed', async () => {
    const fetchMock = vi.fn(async (url: string, options?: { method?: string }) => {
      if (url.endsWith('/osm-overpass/config') && options?.method === 'POST') {
        return { ok: true, json: async () => osmOverpassInfo('https://tidy.example/api') }
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

    const input = (await screen.findByLabelText('Overpass server')) as HTMLInputElement
    fireEvent.change(input, { target: { value: ' https://tidy.example/api ' } })
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => expect(input.value).toBe('https://tidy.example/api'))
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
    await waitFor(() => expect(screen.getByRole('button', { name: /^save$/i })).toBeTruthy())
    expect(fetchMock.mock.calls.filter(([u, o]) => String(u).endsWith('/config') && o?.method === 'POST')).toHaveLength(1)
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

const BLEND_SWITCH = "Use Open-Meteo's blend where GFS-Wave has no data"

describe('ProviderSettingsModal boolean config fields', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  const waveInfo = (value: string) => ({
    type: 'wave',
    id: 'open-meteo-marine',
    name: 'Open-Meteo Marine',
    description: 'Free marine forecasts',
    sandboxed: true,
    allowed_hosts: ['marine-api.open-meteo.com'],
    allowed_hosts_overridden: false,
    allowed_secrets: [],
    allowed_secrets_overridden: false,
    config_fields: [
      {
        key: 'use_blend_fallback',
        label: BLEND_SWITCH,
        type: 'boolean',
        help: 'Asks the blend for spots GFS-Wave cannot answer.',
        placeholder: '',
        value,
      },
    ],
  })

  it('shows a switch, and Save posts "true" once it is turned on', async () => {
    const fetchMock = vi.fn(async (url: string, options?: { method?: string; body?: string }) => {
      if (url.includes('/api/plugins/wave/open-meteo-marine/config') && options?.method === 'POST') {
        expect(JSON.parse(options.body!).values).toEqual({ use_blend_fallback: 'true' })
        return { ok: true, json: async () => waveInfo('true') }
      }
      if (url.includes('/api/plugins/wave/open-meteo-marine/overrides') && options?.method === 'POST') {
        return { ok: true, json: async () => waveInfo('true') }
      }
      if (url.includes('/api/plugins/wave/open-meteo-marine')) {
        return { ok: true, json: async () => waveInfo('') }
      }
      if (url.includes('/api/settings/secrets')) {
        return { ok: true, json: async () => secretsGetResponse }
      }
      return { ok: false, json: async () => ({}) }
    })
    vi.stubGlobal('fetch', fetchMock)

    renderModal('wave', 'open-meteo-marine')

    const toggle = await screen.findByRole('switch', { name: BLEND_SWITCH })
    expect(toggle.getAttribute('aria-checked')).toBe('false')
    expect(screen.getByText('Asks the blend for spots GFS-Wave cannot answer.')).toBeTruthy()

    fireEvent.click(toggle)
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/plugins/wave/open-meteo-marine/config',
        expect.objectContaining({ method: 'POST' }),
      )
    })
  })

  // Save closes the dialog once it has worked, and only posts what the
  // operator actually changed: an untouched allowlist must not be pinned
  // (allowed_hosts_overridden) to its current values.
  const saveMock = (opts: { configFails?: boolean; overridesFails?: () => boolean; overridesGate?: Promise<void>; getInfo?: () => object } = {}) => {
    // The server keeps what a /config save stored, so later answers carry it.
    let storedBlend = ''
    return vi.fn(async (url: string, options?: { method?: string; body?: string }) => {
      if (url.includes('/api/plugins/wave/open-meteo-marine/config') && options?.method === 'POST') {
        if (opts.configFails) {
          return { ok: false, status: 500, json: async () => ({ error: 'config store unavailable' }) }
        }
        storedBlend = 'true'
        return { ok: true, json: async () => waveInfo('true') }
      }
      if (url.includes('/api/plugins/wave/open-meteo-marine/overrides') && options?.method === 'POST') {
        await opts.overridesGate
        if (opts.overridesFails?.()) {
          return { ok: false, status: 500, json: async () => ({ error: 'overrides unavailable' }) }
        }
        // Echo the posted lists the way the server stores them: no repeats.
        const posted = JSON.parse(options.body!)
        return {
          ok: true,
          json: async () => ({ ...waveInfo(storedBlend), allowed_hosts: [...new Set<string>(posted.allowed_hosts)] }),
        }
      }
      if (url.endsWith('/open-meteo-marine/overrides') && options?.method === 'DELETE') {
        return { ok: true, json: async () => waveInfo('') }
      }
      if (url.includes('/api/plugins/wave/open-meteo-marine')) {
        return { ok: true, json: async () => opts.getInfo?.() ?? waveInfo('') }
      }
      if (url.includes('/api/settings/secrets')) {
        return { ok: true, json: async () => secretsGetResponse }
      }
      return { ok: false, json: async () => ({}) }
    })
  }

  const posts = (fetchMock: ReturnType<typeof saveMock>, suffix: string) =>
    fetchMock.mock.calls.filter(([url, options]) => String(url).endsWith(suffix) && options?.method === 'POST')

  // Stubs fetch, renders the wave plugin's dialog and waits for the drafts to
  // be seeded, returning the mock, the onOpenChange spy and the hosts input.
  const openDialog = async (opts?: Parameters<typeof saveMock>[0]) => {
    const fetchMock = saveMock(opts)
    vi.stubGlobal('fetch', fetchMock)
    const onOpenChange = vi.fn()
    renderModal('wave', 'open-meteo-marine', onOpenChange)
    const hosts = (await screen.findByLabelText('Allowed Hosts')) as HTMLInputElement
    await waitFor(() => expect(hosts.value).toBe('marine-api.open-meteo.com'))
    return { fetchMock, onOpenChange, hosts }
  }

  const clickSave = () => fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
  const toggleSwitch = async () => fireEvent.click(await screen.findByRole('switch', { name: BLEND_SWITCH }))

  it('saving a toggled switch posts /config only and closes the dialog', async () => {
    const { fetchMock, onOpenChange } = await openDialog()

    await toggleSwitch()
    clickSave()

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(posts(fetchMock, '/config')).toHaveLength(1)
    expect(posts(fetchMock, '/overrides')).toHaveLength(0)
  })

  it('saving with nothing changed posts nothing and closes the dialog', async () => {
    const { fetchMock, onOpenChange } = await openDialog()

    clickSave()

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(posts(fetchMock, '/config')).toHaveLength(0)
    expect(posts(fetchMock, '/overrides')).toHaveLength(0)
  })

  it('saving edited allowed hosts posts /overrides and stays open with the restart note', async () => {
    const { fetchMock, onOpenChange, hosts } = await openDialog()

    fireEvent.change(hosts, { target: { value: 'marine-api.open-meteo.com, example.com' } })
    clickSave()

    await waitFor(() => expect(screen.getByRole('status').textContent).toMatch(/^Saved\.$/))
    expect(onOpenChange).not.toHaveBeenCalled()
    expect(posts(fetchMock, '/overrides')).toHaveLength(1)
    expect(JSON.parse(posts(fetchMock, '/overrides')[0][1]!.body!)).toEqual({
      allowed_hosts: ['marine-api.open-meteo.com', 'example.com'],
      allowed_secrets: [],
    })
  })

  it('reordered or repeated hosts count as no change', async () => {
    const { fetchMock, onOpenChange, hosts } = await openDialog()

    fireEvent.change(hosts, { target: { value: 'marine-api.open-meteo.com, marine-api.open-meteo.com' } })
    clickSave()

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(posts(fetchMock, '/overrides')).toHaveLength(0)
  })

  it('compares against what the dialog was opened with, not a later refetch', async () => {
    const { fetchMock, onOpenChange } = await openDialog()

    setQueryData(['/api/plugins/wave/open-meteo-marine'], {
      ...waveInfo(''),
      allowed_hosts: ['marine-api.open-meteo.com', 'new-default.example'],
    })
    await toggleSwitch()
    clickSave()

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(posts(fetchMock, '/overrides')).toHaveLength(0)
  })

  it('keeps a host edit when /config saves but /overrides fails, and a retry posts /overrides only', async () => {
    let overridesFail = true
    const { fetchMock, onOpenChange, hosts } = await openDialog({ overridesFails: () => overridesFail })

    await toggleSwitch()
    fireEvent.change(hosts, { target: { value: 'marine-api.open-meteo.com, example.com' } })
    clickSave()

    await waitFor(() => expect(screen.getByText('overrides unavailable')).toBeTruthy())
    expect(screen.getByRole('status').textContent).toBe('Options saved.')
    expect(hosts.value).toBe('marine-api.open-meteo.com, example.com')
    expect(onOpenChange).not.toHaveBeenCalled()

    overridesFail = false
    clickSave()

    await waitFor(() => expect(screen.getByRole('status')).toBeTruthy())
    expect(posts(fetchMock, '/config')).toHaveLength(1)
    expect(posts(fetchMock, '/overrides')).toHaveLength(2)
  })

  it('after an allowlist save the inputs show the lists the server stored, and config drafts are left alone', async () => {
    const { hosts } = await openDialog()

    await toggleSwitch()
    fireEvent.change(hosts, { target: { value: 'marine-api.open-meteo.com,   example.com , example.com' } })
    clickSave()

    await waitFor(() => expect(screen.getByRole('status')).toBeTruthy())
    expect(hosts.value).toBe('marine-api.open-meteo.com, example.com')
    expect(screen.getByRole('switch', { name: BLEND_SWITCH }).getAttribute('aria-checked')).toBe('true')
  })

  it('Reset to defaults replaces an earlier saved note with its own', async () => {
    const { hosts } = await openDialog()

    fireEvent.change(hosts, { target: { value: 'marine-api.open-meteo.com, example.com' } })
    clickSave()
    await waitFor(() => expect(screen.getByRole('status').textContent).toMatch(/^Saved\./))

    fireEvent.click(screen.getByRole('button', { name: /reset to defaults/i }))

    await waitFor(() =>
      expect(screen.getByRole('status').textContent).toBe(
        'Reset to defaults.',
      ),
    )
  })

  it('disables Reset while a save is in flight and Save while a reset is', async () => {
    let release: () => void = () => {}
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    const { hosts } = await openDialog({ overridesGate: gate })

    fireEvent.change(hosts, { target: { value: 'marine-api.open-meteo.com, example.com' } })
    clickSave()

    await waitFor(() => expect(screen.getByRole('button', { name: /^saving$/i })).toBeTruthy())
    expect((screen.getByRole('button', { name: /reset to defaults/i }) as HTMLButtonElement).disabled).toBe(true)
    release()
    await waitFor(() => expect(screen.getByRole('status')).toBeTruthy())
  })

  it('a refetch reseeds the parts the operator has not touched and leaves edited parts alone', async () => {
    const { hosts } = await openDialog()
    const secrets = screen.getByLabelText('Allowed secrets') as HTMLInputElement

    fireEvent.change(secrets, { target: { value: 'MY_KEY' } })
    act(() => {
      setQueryData(['/api/plugins/wave/open-meteo-marine'], {
        ...waveInfo('true'),
        allowed_hosts: ['marine-api.open-meteo.com', 'new-default.example'],
        allowed_secrets: ['OTHER'],
      })
    })

    await waitFor(() => expect(hosts.value).toBe('marine-api.open-meteo.com, new-default.example'))
    expect(secrets.value).toBe('MY_KEY')
    expect(screen.getByRole('switch', { name: BLEND_SWITCH }).getAttribute('aria-checked')).toBe('true')
  })

  it('never posts a config key the plugin no longer declares', async () => {
    const oldField = { key: 'old_key', label: 'Old option', type: 'text', help: '', placeholder: '', value: 'x' }
    const withOld = { ...waveInfo(''), config_fields: [...waveInfo('').config_fields, oldField] }
    const { fetchMock, onOpenChange } = await openDialog({ getInfo: () => withOld })
    await screen.findByLabelText('Old option')

    act(() => {
      setQueryData(['/api/plugins/wave/open-meteo-marine'], waveInfo(''))
    })
    await toggleSwitch()
    clickSave()

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(JSON.parse(posts(fetchMock, '/config')[0][1]!.body!).values).toEqual({ use_blend_fallback: 'true' })
  })

  it('a change of host case is a real edit and posts /overrides', async () => {
    const { fetchMock, hosts } = await openDialog()

    fireEvent.change(hosts, { target: { value: 'Marine-API.Open-Meteo.com' } })
    clickSave()

    await waitFor(() => expect(posts(fetchMock, '/overrides')).toHaveLength(1))
  })

  it('posts only the config fields that changed', async () => {
    const other = { key: 'other', label: 'Other option', type: 'text', help: '', placeholder: '', value: 'x' }
    const info = { ...waveInfo(''), config_fields: [...waveInfo('').config_fields, other] }
    const { fetchMock, onOpenChange } = await openDialog({ getInfo: () => info })

    fireEvent.change(await screen.findByLabelText('Other option'), { target: { value: 'y' } })
    clickSave()

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(JSON.parse(posts(fetchMock, '/config')[0][1]!.body!).values).toEqual({ other: 'y' })
  })

  it('does not close mid-save when Escape is pressed', async () => {
    let release: () => void = () => {}
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    const { hosts, onOpenChange } = await openDialog({ overridesGate: gate })

    fireEvent.change(hosts, { target: { value: 'marine-api.open-meteo.com, example.com' } })
    clickSave()
    await waitFor(() => expect(hosts.disabled).toBe(true))
    fireEvent.keyDown(document.body, { key: 'Escape' })
    fireEvent.keyDown(hosts, { key: 'Escape' })
    expect(onOpenChange).not.toHaveBeenCalled()

    release()
    await waitFor(() => expect(screen.getByRole('status')).toBeTruthy())
  })

  it('clears the saved note when the operator edits a field afterwards', async () => {
    const { hosts } = await openDialog()

    fireEvent.change(hosts, { target: { value: 'marine-api.open-meteo.com, example.com' } })
    clickSave()
    await waitFor(() => expect(screen.getByRole('status')).toBeTruthy())

    fireEvent.change(hosts, { target: { value: 'marine-api.open-meteo.com' } })
    expect(screen.queryByRole('status')).toBeNull()
  })

  it('a boolean stored as unset, switched on then off, is no change', async () => {
    const { fetchMock, onOpenChange } = await openDialog()

    await toggleSwitch()
    await toggleSwitch()
    clickSave()

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(posts(fetchMock, '/config')).toHaveLength(0)
    expect(posts(fetchMock, '/overrides')).toHaveLength(0)
  })

  it('disables the inputs while a save is in flight', async () => {
    let release: () => void = () => {}
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    const { hosts } = await openDialog({ overridesGate: gate })

    fireEvent.change(hosts, { target: { value: 'marine-api.open-meteo.com, example.com' } })
    clickSave()

    await waitFor(() => expect(hosts.disabled).toBe(true))
    expect((screen.getByLabelText('Allowed secrets') as HTMLInputElement).disabled).toBe(true)
    expect((screen.getByRole('switch', { name: BLEND_SWITCH }) as HTMLElement).hasAttribute('data-disabled')).toBe(true)
    release()
    await waitFor(() => expect(hosts.disabled).toBe(false))
  })

  it('a failed save keeps the dialog open and shows the error', async () => {
    const { onOpenChange } = await openDialog({ configFails: true })

    await toggleSwitch()
    clickSave()

    await waitFor(() => expect(screen.getByText('config store unavailable')).toBeTruthy())
    expect(onOpenChange).not.toHaveBeenCalled()
  })
})

describe('ProviderSettingsModal allowlist saved but secrets failed', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows the error and says the allowlist saved, even when secrets fail first', async () => {
    let release: () => void = () => {}
    const overridesGate = new Promise<void>((resolve) => {
      release = resolve
    })
    const info = {
      type: 'weather',
      id: 'weatherkit',
      name: 'WeatherKit',
      description: "Apple's weather API",
      sandboxed: true,
      allowed_hosts: ['weatherkit.apple.com'],
      allowed_hosts_overridden: false,
      allowed_secrets: ['WEATHERKIT_KEY_ID'],
      allowed_secrets_overridden: false,
    }
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, options?: { method?: string }) => {
        if (url.includes('/api/plugins/weather/weatherkit/overrides') && options?.method === 'POST') {
          await overridesGate
          return { ok: true, json: async () => ({ ...info, allowed_hosts: ['weatherkit.apple.com', 'example.com'] }) }
        }
        if (url.includes('/api/plugins/weather/weatherkit')) {
          return { ok: true, json: async () => info }
        }
        if (url.includes('/api/settings/secrets') && options?.method === 'POST') {
          return { ok: false, status: 500, json: async () => ({ error: 'secret store unavailable' }) }
        }
        if (url.includes('/api/settings/secrets')) {
          return { ok: true, json: async () => secretsGetResponse }
        }
        return { ok: false, json: async () => ({}) }
      }),
    )
    const onOpenChange = vi.fn()
    renderModal('weather', 'weatherkit', onOpenChange)

    const hosts = (await screen.findByLabelText('Allowed Hosts')) as HTMLInputElement
    await waitFor(() => expect(hosts.value).toBe('weatherkit.apple.com'))
    fireEvent.change(hosts, { target: { value: 'weatherkit.apple.com, example.com' } })
    fireEvent.change(screen.getByLabelText('WeatherKit Key ID'), { target: { value: 'ABC123' } })
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

    await new Promise((resolve) => setTimeout(resolve, 30))
    release()
    await waitFor(() => expect(screen.getByText('secret store unavailable')).toBeTruthy())
    expect(screen.getByRole('status').textContent).toMatch(/Allowlist saved/)
    expect(onOpenChange).not.toHaveBeenCalled()
  })
})
