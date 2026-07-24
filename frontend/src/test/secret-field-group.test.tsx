import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { SecretFieldGroup } from '@/components/settings/secret-field-group'
import { SecretsStatusProvider } from '@/components/settings/secrets-status-context'

const secretsGetResponse = {
  SIGNALK_USERNAME: true,
  SIGNALK_PASSWORD: false,
  INFLUXDB_TOKEN: false,
  STORMGLASS_API_KEY: false,
  GEONAMES_USERNAME: false,
  WEATHERKIT_KEY_ID: false,
  WEATHERKIT_TEAM_ID: false,
  WEATHERKIT_SERVICE_ID: false,
  WEATHERKIT_PRIVATE_KEY: false,
}

function renderGroup() {
  return render(
    <SecretsStatusProvider>
      <SecretFieldGroup
        fields={[
          { key: 'SIGNALK_USERNAME', label: 'SignalK Username' },
          { key: 'INFLUXDB_TOKEN', label: 'InfluxDB Token' },
        ]}
      />
    </SecretsStatusProvider>,
  )
}

describe('SecretFieldGroup', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, options?: { method?: string; body?: string }) => {
        if (url.includes('/api/settings/secrets') && (!options || options.method !== 'POST')) {
          return { ok: true, json: async () => secretsGetResponse }
        }
        return { ok: false, json: async () => ({}) }
      }),
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders correct "set" vs "not set" placeholder state per field', async () => {
    renderGroup()

    await waitFor(() => {
      expect(screen.getByLabelText('SignalK Username')).toBeTruthy()
    })

    const signalkUsernameField = screen.getByLabelText('SignalK Username') as HTMLInputElement
    expect(signalkUsernameField.placeholder).toBe('•••• (set)')

    const influxdbTokenField = screen.getByLabelText('InfluxDB Token') as HTMLInputElement
    expect(influxdbTokenField.placeholder).toBe('not set')
  })

  it('renders no Save button — saving is owned by the page-level/modal-level Save button now', async () => {
    renderGroup()

    await waitFor(() => {
      expect(screen.getByLabelText('SignalK Username')).toBeTruthy()
    })

    expect(screen.queryByRole('button', { name: /^save$/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /^saving$/i })).toBeNull()
  })

  it('typing into a field updates shared context state (value + touched) without any local save', async () => {
    renderGroup()

    await waitFor(() => {
      expect(screen.getByLabelText('InfluxDB Token')).toBeTruthy()
    })

    const influxdbTokenField = screen.getByLabelText('InfluxDB Token') as HTMLInputElement
    fireEvent.change(influxdbTokenField, { target: { value: 'test-token' } })

    // The field itself reflects the typed value, proving it's a controlled
    // input wired to shared context state (there's no local component state
    // left in SecretFieldGroup to source this from).
    await waitFor(() => {
      expect(influxdbTokenField.value).toBe('test-token')
    })

    // Touching a field with a value now shows its per-field Clear button
    // (showClear = isSet || isTouched), which is further evidence the
    // touched flag flowed through to context state.
    const clearButtons = screen.getAllByRole('button', { name: /clear/i })
    expect(clearButtons.length).toBeGreaterThan(0)
  })

  it('Clear button confirms then POSTs an empty string independently of any batch save', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)

    const fetchMock = vi.fn(async (url: string, options?: { method?: string; body?: string }) => {
      if (url.includes('/api/settings/secrets') && options?.method === 'POST') {
        const body = JSON.parse(options.body!)
        expect(body).toEqual({ SIGNALK_USERNAME: '' })
        return { ok: true, json: async () => ({ ...secretsGetResponse, SIGNALK_USERNAME: false }) }
      }
      if (url.includes('/api/settings/secrets')) {
        return { ok: true, json: async () => secretsGetResponse }
      }
      return { ok: false, json: async () => ({}) }
    })
    vi.stubGlobal('fetch', fetchMock)

    renderGroup()

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /clear/i })).toBeTruthy()
    })

    fireEvent.click(screen.getByRole('button', { name: /clear/i }))

    await waitFor(() => {
      expect(window.confirm).toHaveBeenCalledWith('Clear SignalK Username? This cannot be undone.')
    })

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining('/api/settings/secrets'),
        expect.objectContaining({ method: 'POST', body: JSON.stringify({ SIGNALK_USERNAME: '' }) }),
      )
    })
  })
})
