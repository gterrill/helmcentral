import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { useSecretsStatus } from '@/hooks/use-secrets-status'

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

describe('useSecretsStatus', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('saveTouchedKeys only sends the touched subset of the given keys', async () => {
    const fetchMock = vi.fn(async (url: string, options?: { method?: string; body?: string }) => {
      if (url.includes('/api/settings/secrets') && options?.method === 'POST') {
        const body = JSON.parse(options.body!)
        expect(Object.keys(body)).toEqual(['SIGNALK_USERNAME'])
        expect(body.SIGNALK_USERNAME).toBe('alice')
        return { ok: true, json: async () => ({ ...secretsGetResponse, SIGNALK_USERNAME: true }) }
      }
      if (url.includes('/api/settings/secrets')) {
        return { ok: true, json: async () => secretsGetResponse }
      }
      return { ok: false, json: async () => ({}) }
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useSecretsStatus())
    await waitFor(() => expect(result.current.loading).toBe(false))

    act(() => {
      result.current.setFieldValue('SIGNALK_USERNAME', 'alice')
    })

    await act(async () => {
      await result.current.saveTouchedKeys(['SIGNALK_USERNAME', 'SIGNALK_PASSWORD'])
    })

    const postCall = fetchMock.mock.calls.find(([, options]) => options?.method === 'POST')
    expect(postCall).toBeTruthy()
    expect(result.current.status.SIGNALK_USERNAME).toBe(true)
  })

  it('does not make a network call when none of the given keys are touched', async () => {
    const fetchMock = vi.fn(async (url: string, options?: { method?: string }) => {
      if (url.includes('/api/settings/secrets') && options?.method === 'POST') {
        throw new Error('should not POST when nothing touched')
      }
      return { ok: true, json: async () => secretsGetResponse }
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useSecretsStatus())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.saveTouchedKeys(['SIGNALK_USERNAME', 'SIGNALK_PASSWORD'])
    })

    expect(fetchMock.mock.calls.some(([, options]) => options?.method === 'POST')).toBe(false)
  })

  it('leaves a touched key alone when it is not part of the keys passed to saveTouchedKeys', async () => {
    const fetchMock = vi.fn(async (url: string, options?: { method?: string; body?: string }) => {
      if (url.includes('/api/settings/secrets') && options?.method === 'POST') {
        const body = JSON.parse(options.body!)
        expect(Object.keys(body)).toEqual(['SIGNALK_USERNAME'])
        return { ok: true, json: async () => ({ ...secretsGetResponse, SIGNALK_USERNAME: true }) }
      }
      if (url.includes('/api/settings/secrets')) {
        return { ok: true, json: async () => secretsGetResponse }
      }
      return { ok: false, json: async () => ({}) }
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useSecretsStatus())
    await waitFor(() => expect(result.current.loading).toBe(false))

    act(() => {
      result.current.setFieldValue('SIGNALK_USERNAME', 'alice')
      result.current.setFieldValue('WEATHERKIT_KEY_ID', 'mid-edit-elsewhere')
    })

    await act(async () => {
      await result.current.saveTouchedKeys(['SIGNALK_USERNAME'])
    })

    // The saved key is reset back to untouched/empty...
    expect(result.current.values.SIGNALK_USERNAME).toBe('')
    expect(result.current.touched.SIGNALK_USERNAME).toBe(false)
    // ...but the unrelated, still-mid-edit key elsewhere is undisturbed.
    expect(result.current.values.WEATHERKIT_KEY_ID).toBe('mid-edit-elsewhere')
    expect(result.current.touched.WEATHERKIT_KEY_ID).toBe(true)
  })

  it('clearKey resets that key\'s value/touched to empty/false on success', async () => {
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

    const { result } = renderHook(() => useSecretsStatus())
    await waitFor(() => expect(result.current.loading).toBe(false))

    act(() => {
      result.current.setFieldValue('SIGNALK_USERNAME', 'alice')
    })
    expect(result.current.touched.SIGNALK_USERNAME).toBe(true)

    await act(async () => {
      await result.current.clearKey('SIGNALK_USERNAME', 'SignalK Username')
    })

    expect(window.confirm).toHaveBeenCalledWith('Clear SignalK Username? This cannot be undone.')
    expect(result.current.values.SIGNALK_USERNAME).toBe('')
    expect(result.current.touched.SIGNALK_USERNAME).toBe(false)
    expect(result.current.status.SIGNALK_USERNAME).toBe(false)
  })
})
