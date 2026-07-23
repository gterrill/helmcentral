import { describe, it, expect, vi, beforeEach } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { SecretsSettingsPanel } from '@/components/secrets-settings-panel'

describe('SecretsSettingsPanel', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, options?: { method?: string; body?: string }) => {
        if (url.includes('/api/settings/secrets') && (!options || options.method !== 'POST')) {
          return {
            ok: true,
            json: async () => ({
              SIGNALK_USERNAME: true,
              SIGNALK_PASSWORD: false,
              INFLUXDB_TOKEN: false,
              STORMGLASS_API_KEY: false,
              GEONAMES_USERNAME: false,
              WEATHERKIT_KEY_ID: false,
              WEATHERKIT_TEAM_ID: false,
              WEATHERKIT_SERVICE_ID: false,
              WEATHERKIT_PRIVATE_KEY: false,
            }),
          }
        }
        if (url.includes('/api/settings/secrets/import-env')) {
          return {
            ok: true,
            json: async () => ({
              imported: ['INFLUXDB_TOKEN', 'STORMGLASS_API_KEY'],
            }),
          }
        }
        return { ok: false, json: async () => ({}) }
      }),
    )
  })

  it('renders correct "set" vs "not set" placeholder state per field', async () => {
    render(<SecretsSettingsPanel />)

    await waitFor(() => {
      expect(screen.getByLabelText('SignalK Username')).toBeTruthy()
    })

    const signalkUsernameField = screen.getByLabelText('SignalK Username') as HTMLInputElement
    expect(signalkUsernameField.placeholder).toBe('•••• (set)')

    const influxdbTokenField = screen.getByLabelText('InfluxDB Token') as HTMLInputElement
    expect(influxdbTokenField.placeholder).toBe('not set')
  })

  it('sends POST with only touched fields on save', async () => {
    const fetchMock = vi.fn(async (url: string, options?: { method?: string; body?: string }) => {
      if (url.includes('/api/settings/secrets') && options?.method === 'POST') {
        const body = JSON.parse(options.body!)
        // Verify only INFLUXDB_TOKEN is in the payload
        expect(Object.keys(body)).toEqual(['INFLUXDB_TOKEN'])
        expect(body.INFLUXDB_TOKEN).toBe('test-token')
        return {
          ok: true,
          json: async () => ({
            SIGNALK_USERNAME: true,
            SIGNALK_PASSWORD: false,
            INFLUXDB_TOKEN: true,
            STORMGLASS_API_KEY: false,
            GEONAMES_USERNAME: false,
            WEATHERKIT_KEY_ID: false,
            WEATHERKIT_TEAM_ID: false,
            WEATHERKIT_SERVICE_ID: false,
            WEATHERKIT_PRIVATE_KEY: false,
          }),
        }
      }
      if (url.includes('/api/settings/secrets') && (!options || options.method !== 'POST')) {
        return {
          ok: true,
          json: async () => ({
            SIGNALK_USERNAME: true,
            SIGNALK_PASSWORD: false,
            INFLUXDB_TOKEN: false,
            STORMGLASS_API_KEY: false,
            GEONAMES_USERNAME: false,
            WEATHERKIT_KEY_ID: false,
            WEATHERKIT_TEAM_ID: false,
            WEATHERKIT_SERVICE_ID: false,
            WEATHERKIT_PRIVATE_KEY: false,
          }),
        }
      }
      return { ok: false, json: async () => ({}) }
    })

    vi.stubGlobal('fetch', fetchMock)

    render(<SecretsSettingsPanel />)

    await waitFor(() => {
      expect(screen.getByLabelText('InfluxDB Token')).toBeTruthy()
    })

    const influxdbTokenField = screen.getByLabelText('InfluxDB Token') as HTMLInputElement
    fireEvent.change(influxdbTokenField, { target: { value: 'test-token' } })

    const saveButton = screen.getByRole('button', { name: /Save Secrets/i })
    fireEvent.click(saveButton)

    await waitFor(() => {
      expect(screen.getByText(/Secrets saved/i)).toBeTruthy()
    })
  })

  it('does not include untouched fields in POST body', async () => {
    const fetchMock = vi.fn(async (url: string, options?: { method?: string; body?: string }) => {
      if (url.includes('/api/settings/secrets') && options?.method === 'POST') {
        const body = JSON.parse(options.body!)
        // Verify INFLUXDB_TOKEN is the only key
        expect(Object.keys(body).length).toBe(1)
        expect('STORMGLASS_API_KEY' in body).toBe(false)
        return {
          ok: true,
          json: async () => ({
            SIGNALK_USERNAME: true,
            SIGNALK_PASSWORD: false,
            INFLUXDB_TOKEN: true,
            STORMGLASS_API_KEY: false,
            GEONAMES_USERNAME: false,
            WEATHERKIT_KEY_ID: false,
            WEATHERKIT_TEAM_ID: false,
            WEATHERKIT_SERVICE_ID: false,
            WEATHERKIT_PRIVATE_KEY: false,
          }),
        }
      }
      if (url.includes('/api/settings/secrets') && (!options || options.method !== 'POST')) {
        return {
          ok: true,
          json: async () => ({
            SIGNALK_USERNAME: true,
            SIGNALK_PASSWORD: false,
            INFLUXDB_TOKEN: false,
            STORMGLASS_API_KEY: false,
            GEONAMES_USERNAME: false,
            WEATHERKIT_KEY_ID: false,
            WEATHERKIT_TEAM_ID: false,
            WEATHERKIT_SERVICE_ID: false,
            WEATHERKIT_PRIVATE_KEY: false,
          }),
        }
      }
      return { ok: false, json: async () => ({}) }
    })

    vi.stubGlobal('fetch', fetchMock)

    render(<SecretsSettingsPanel />)

    await waitFor(() => {
      expect(screen.getByLabelText('InfluxDB Token')).toBeTruthy()
    })

    const influxdbTokenField = screen.getByLabelText('InfluxDB Token') as HTMLInputElement
    fireEvent.change(influxdbTokenField, { target: { value: 'test-token' } })

    const saveButton = screen.getByRole('button', { name: /Save Secrets/i })
    fireEvent.click(saveButton)

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining('/api/settings/secrets'),
        expect.objectContaining({
          method: 'POST',
          body: expect.not.stringContaining('STORMGLASS_API_KEY'),
        }),
      )
    })
  })

  it('imports from environment and displays result', async () => {
    const fetchMock = vi.fn(async (url: string, options?: { method?: string }) => {
      if (url.includes('/api/settings/secrets/import-env')) {
        return {
          ok: true,
          json: async () => ({
            imported: ['INFLUXDB_TOKEN', 'STORMGLASS_API_KEY'],
          }),
        }
      }
      if (url.includes('/api/settings/secrets') && (!options || options.method !== 'POST')) {
        return {
          ok: true,
          json: async () => ({
            SIGNALK_USERNAME: true,
            SIGNALK_PASSWORD: false,
            INFLUXDB_TOKEN: true,
            STORMGLASS_API_KEY: true,
            GEONAMES_USERNAME: false,
            WEATHERKIT_KEY_ID: false,
            WEATHERKIT_TEAM_ID: false,
            WEATHERKIT_SERVICE_ID: false,
            WEATHERKIT_PRIVATE_KEY: false,
          }),
        }
      }
      return { ok: false, json: async () => ({}) }
    })

    vi.stubGlobal('fetch', fetchMock)

    render(<SecretsSettingsPanel />)

    await waitFor(() => {
      expect(screen.getByText(/Import from environment/i)).toBeTruthy()
    })

    const importButton = screen.getByRole('button', { name: /Import from environment/i })
    fireEvent.click(importButton)

    await waitFor(() => {
      expect(screen.getByText(/Imported: INFLUXDB_TOKEN, STORMGLASS_API_KEY/i)).toBeTruthy()
    })
  })

  it('handles empty import result', async () => {
    const fetchMock = vi.fn(async (url: string, options?: { method?: string }) => {
      if (url.includes('/api/settings/secrets/import-env')) {
        return {
          ok: true,
          json: async () => ({
            imported: [],
          }),
        }
      }
      if (url.includes('/api/settings/secrets') && (!options || options.method !== 'POST')) {
        return {
          ok: true,
          json: async () => ({
            SIGNALK_USERNAME: true,
            SIGNALK_PASSWORD: false,
            INFLUXDB_TOKEN: false,
            STORMGLASS_API_KEY: false,
            GEONAMES_USERNAME: false,
            WEATHERKIT_KEY_ID: false,
            WEATHERKIT_TEAM_ID: false,
            WEATHERKIT_SERVICE_ID: false,
            WEATHERKIT_PRIVATE_KEY: false,
          }),
        }
      }
      return { ok: false, json: async () => ({}) }
    })

    vi.stubGlobal('fetch', fetchMock)

    render(<SecretsSettingsPanel />)

    await waitFor(() => {
      expect(screen.getByText(/Import from environment/i)).toBeTruthy()
    })

    const importButton = screen.getByRole('button', { name: /Import from environment/i })
    fireEvent.click(importButton)

    await waitFor(() => {
      expect(screen.getByText(/Nothing to import/i)).toBeTruthy()
    })
  })

  it('displays inline error message on failed save', async () => {
    const fetchMock = vi.fn(async (url: string, options?: { method?: string; body?: string }) => {
      if (url.includes('/api/settings/secrets') && options?.method === 'POST') {
        return {
          ok: false,
          json: async () => ({ error: 'Database connection failed' }),
        }
      }
      if (url.includes('/api/settings/secrets') && (!options || options.method !== 'POST')) {
        return {
          ok: true,
          json: async () => ({
            SIGNALK_USERNAME: true,
            SIGNALK_PASSWORD: false,
            INFLUXDB_TOKEN: false,
            STORMGLASS_API_KEY: false,
            GEONAMES_USERNAME: false,
            WEATHERKIT_KEY_ID: false,
            WEATHERKIT_TEAM_ID: false,
            WEATHERKIT_SERVICE_ID: false,
            WEATHERKIT_PRIVATE_KEY: false,
          }),
        }
      }
      return { ok: false, json: async () => ({}) }
    })

    vi.stubGlobal('fetch', fetchMock)

    render(<SecretsSettingsPanel />)

    await waitFor(() => {
      expect(screen.getByLabelText('InfluxDB Token')).toBeTruthy()
    })

    const influxdbTokenField = screen.getByLabelText('InfluxDB Token') as HTMLInputElement
    fireEvent.change(influxdbTokenField, { target: { value: 'test-token' } })

    const saveButton = screen.getByRole('button', { name: /Save Secrets/i })
    fireEvent.click(saveButton)

    await waitFor(() => {
      expect(screen.getByText(/Database connection failed/i)).toBeTruthy()
    })
  })
})
