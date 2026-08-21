import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { urlBase64ToUint8Array, useWebPush } from '@/hooks/use-web-push'

const VAPID_PUBLIC_KEY =
  'BEl62iUYgUivxIkv69yViEuiBIa-Ib9-SkvMeAtA3LFgDzkrxZJjSgSnfckjBJuBkr3qBUYIHBQFLXYp5Nksh8U'

function stubSecureContext() {
  vi.stubGlobal('isSecureContext', true)
}

function stubPushCapableBrowser(overrides: {
  permission?: NotificationPermission
  existingSubscription?: unknown
} = {}) {
  stubSecureContext()

  const subscription = {
    endpoint: 'https://push.example/abc',
    toJSON: () => ({ endpoint: 'https://push.example/abc', keys: { p256dh: 'p', auth: 'a' } }),
    unsubscribe: vi.fn().mockResolvedValue(true),
  }

  const pushManager = {
    subscribe: vi.fn().mockResolvedValue(subscription),
    getSubscription: vi.fn().mockResolvedValue(overrides.existingSubscription ?? null),
  }

  const registration = { pushManager, scope: '/' }

  vi.stubGlobal('navigator', {
    userAgent: 'Mozilla/5.0 (X11; Linux x86_64) Chrome/120',
    maxTouchPoints: 0,
    serviceWorker: {
      register: vi.fn().mockResolvedValue(registration),
      ready: Promise.resolve(registration),
      getRegistration: vi.fn().mockResolvedValue(registration),
    },
  })

  const requestPermission = vi.fn().mockResolvedValue(overrides.permission ?? 'granted')
  vi.stubGlobal('Notification', {
    permission: overrides.permission === 'denied' ? 'denied' : 'default',
    requestPermission,
  })
  vi.stubGlobal('PushManager', function PushManagerStub() {})

  return { subscription, pushManager, registration, requestPermission }
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes('/webpush/key')) {
      return new Response(JSON.stringify({ public_key: VAPID_PUBLIC_KEY, subscribed_devices: 0 }), { status: 200 })
    }
    if (url.includes('/webpush/subscribe')) return new Response('{}', { status: 201 })
    if (url.includes('/webpush/unsubscribe')) return new Response(null, { status: 204 })
    return new Response('{}', { status: 200 })
  }))
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('urlBase64ToUint8Array', () => {
  /**
   * Chrome accepts a bare base64 string as applicationServerKey; Safari does
   * not. There is one code path, the one that works everywhere.
   */
  it('decodes a VAPID key to the 65-byte P-256 point the Push API expects', () => {
    const key = urlBase64ToUint8Array(VAPID_PUBLIC_KEY)

    expect(key).toBeInstanceOf(Uint8Array)
    expect(key.length).toBe(65)
    expect(key[0]).toBe(0x04)
  })
})

describe('useWebPush', () => {
  /**
   * jsdom's default URL is http://localhost/, so isSecureContext is already
   * false and serviceWorker is already absent — this needs no stubbing at all,
   * and is exactly the state a boat LAN produces.
   */
  it('reports an insecure context before anything else', async () => {
    const { result } = renderHook(() => useWebPush())

    await waitFor(() => expect(result.current.support.kind).toBe('insecure-context'))
  })

  it('reports ok on a secure, push-capable browser', async () => {
    stubPushCapableBrowser()
    const { result } = renderHook(() => useWebPush())

    await waitFor(() => expect(result.current.support.kind).toBe('ok'))
  })

  // Safari on iOS exposes serviceWorker in an ordinary tab but withholds
  // PushManager until the site is installed to the Home Screen. Reporting that
  // as "unsupported" would send the user chasing the wrong problem.
  it('distinguishes an uninstalled iOS browser from an unsupported one', async () => {
    stubSecureContext()
    vi.stubGlobal('navigator', {
      userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) Safari',
      maxTouchPoints: 5,
      serviceWorker: { register: vi.fn(), ready: Promise.resolve({}) },
    })
    vi.stubGlobal('Notification', { permission: 'default', requestPermission: vi.fn() })

    const { result } = renderHook(() => useWebPush())

    await waitFor(() => expect(result.current.support.kind).toBe('ios-not-installed'))
  })

  it('reports blocked when notifications are denied', async () => {
    stubPushCapableBrowser({ permission: 'denied' })
    const { result } = renderHook(() => useWebPush())

    await waitFor(() => expect(result.current.support.kind).toBe('blocked'))
  })

  it('subscribes and registers the device with the server', async () => {
    const { pushManager, requestPermission } = stubPushCapableBrowser()
    const { result } = renderHook(() => useWebPush())
    await waitFor(() => expect(result.current.support.kind).toBe('ok'))

    await act(async () => { await result.current.enableOnThisDevice('Helm tablet') })

    expect(requestPermission).toHaveBeenCalled()
    expect(pushManager.subscribe).toHaveBeenCalledWith(
      expect.objectContaining({
        userVisibleOnly: true,
        applicationServerKey: expect.any(Uint8Array),
      }),
    )

    const posted = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls
      .find(([url]) => String(url).includes('/webpush/subscribe'))
    expect(posted).toBeDefined()
    expect(JSON.parse(String(posted?.[1]?.body))).toMatchObject({
      endpoint: 'https://push.example/abc',
      label: 'Helm tablet',
    })
  })

  it('surfaces a denied permission as an error and registers nothing', async () => {
    const { pushManager } = stubPushCapableBrowser()
    ;(globalThis.Notification as unknown as { requestPermission: ReturnType<typeof vi.fn> })
      .requestPermission.mockResolvedValue('denied')

    const { result } = renderHook(() => useWebPush())
    await waitFor(() => expect(result.current.support.kind).toBe('ok'))

    await act(async () => { await result.current.enableOnThisDevice('Helm tablet') })

    expect(result.current.error).toBeTruthy()
    expect(pushManager.subscribe).not.toHaveBeenCalled()
  })

  /**
   * Server first: if the browser's own unsubscribe then fails, the row is
   * already gone and nothing pushes at a dead endpoint. The reverse order
   * leaves the server pushing at a revoked endpoint until a 410 prunes it.
   */
  it('tells the server before unsubscribing locally', async () => {
    // One stub, and its getSubscription resolves to its own subscription — the
    // same object whose unsubscribe is asserted on below.
    const capable = stubPushCapableBrowser()
    capable.pushManager.getSubscription.mockResolvedValue(capable.subscription)

    const order: string[] = []
    ;(globalThis.fetch as ReturnType<typeof vi.fn>).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/webpush/unsubscribe')) {
        order.push('server')
        return new Response(null, { status: 204 })
      }
      if (url.includes('/webpush/key')) {
        return new Response(JSON.stringify({ public_key: VAPID_PUBLIC_KEY, subscribed_devices: 1 }), { status: 200 })
      }
      return new Response('{}', { status: 200 })
    })
    capable.subscription.unsubscribe.mockImplementation(async () => { order.push('browser'); return true })

    const { result } = renderHook(() => useWebPush())
    await waitFor(() => expect(result.current.support.kind).toBe('ok'))

    await act(async () => { await result.current.disableOnThisDevice() })

    expect(order).toEqual(['server', 'browser'])
  })
})
