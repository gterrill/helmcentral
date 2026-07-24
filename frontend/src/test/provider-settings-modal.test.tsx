import { describe, it, expect, vi, afterEach } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { ProviderSettingsModal } from '@/components/settings/provider-settings-modal'
import { SecretsStatusProvider } from '@/components/settings/secrets-status-context'

const secretsGetResponse = {
  SIGNALK_USERNAME: false,
  SIGNALK_PASSWORD: false,
  INFLUXDB_TOKEN: false,
  STORMGLASS_API_KEY: false,
  GEONAMES_USERNAME: false,
  WEATHERKIT_KEY_ID: false,
  WEATHERKIT_TEAM_ID: false,
  WEATHERKIT_SERVICE_ID: false,
  WEATHERKIT_PRIVATE_KEY: false,
}

function renderModal(type: 'tide' | 'weather', providerId: string) {
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

  it('renders the Storm Glass API Key field for the stormglass tide provider (per the static table), and omits the allowlist block since it is non-sandboxed', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        if (url.includes('/api/plugins/tide/stormglass')) {
          return {
            ok: true,
            json: async () => ({
              type: 'tide',
              id: 'stormglass',
              name: 'Storm Glass',
              description: 'Paid worldwide tide data',
              sandboxed: false,
              allowed_hosts: [],
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

    renderModal('tide', 'stormglass')

    await waitFor(() => {
      expect(screen.getByLabelText('Storm Glass API Key')).toBeTruthy()
    })

    expect(screen.queryByLabelText('Allowed Hosts')).toBeNull()
    expect(screen.queryByText(/Allowlist changes require a backend restart/i)).toBeNull()
  })

  it('Save (the single modal button) also saves the touched secret field via POST /api/settings/secrets, for the non-sandboxed stormglass provider', async () => {
    const fetchMock = vi.fn(async (url: string, options?: { method?: string; body?: string }) => {
      if (url.includes('/api/plugins/tide/stormglass') && !url.includes('/overrides')) {
        return {
          ok: true,
          json: async () => ({
            type: 'tide',
            id: 'stormglass',
            name: 'Storm Glass',
            description: 'Paid worldwide tide data',
            sandboxed: false,
            allowed_hosts: [],
            allowed_hosts_overridden: false,
            allowed_secrets: [],
            allowed_secrets_overridden: false,
          }),
        }
      }
      if (url.includes('/api/settings/secrets') && options?.method === 'POST') {
        const body = JSON.parse(options.body!)
        expect(body).toEqual({ STORMGLASS_API_KEY: 'sg-test-key' })
        return { ok: true, json: async () => ({ ...secretsGetResponse, STORMGLASS_API_KEY: true }) }
      }
      if (url.includes('/api/settings/secrets')) {
        return { ok: true, json: async () => secretsGetResponse }
      }
      return { ok: false, json: async () => ({}) }
    })
    vi.stubGlobal('fetch', fetchMock)

    renderModal('tide', 'stormglass')

    await waitFor(() => {
      expect(screen.getByLabelText('Storm Glass API Key')).toBeTruthy()
    })

    // Only one Save button exists for a non-sandboxed provider with secret
    // fields (no allowlist editor for stormglass, so no second Save button
    // from that block either).
    const saveButtons = screen.getAllByRole('button', { name: /^save$/i })
    expect(saveButtons).toHaveLength(1)

    const apiKeyField = screen.getByLabelText('Storm Glass API Key') as HTMLInputElement
    fireEvent.change(apiKeyField, { target: { value: 'sg-test-key' } })

    fireEvent.click(saveButtons[0])

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining('/api/settings/secrets'),
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ STORMGLASS_API_KEY: 'sg-test-key' }),
        }),
      )
    })
  })

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

    expect(screen.queryByLabelText('Storm Glass API Key')).toBeNull()
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
})
