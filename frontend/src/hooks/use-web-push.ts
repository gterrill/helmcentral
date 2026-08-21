import { useCallback, useEffect, useState } from 'react'

/**
 * Web push registration for this browser (ADR 0038, ADR 0045).
 *
 * The Push API needs a secure context, which Helmcentral does not ship: on a
 * plain-HTTP boat LAN the whole API is absent. So the first job here is to work
 * out *why* push is unavailable precisely enough that the UI can tell the
 * operator what to do, rather than showing a toggle that cannot work.
 */
export type WebPushSupport =
  | { kind: 'ok' }
  | { kind: 'insecure-context' }
  | { kind: 'no-service-worker' }
  | { kind: 'ios-not-installed' }
  | { kind: 'unsupported' }
  | { kind: 'blocked' }

const SERVICE_WORKER_PATH = '/sw.js'
const KEY_URL = '/api/alarm-transports/webpush/key'
const SUBSCRIBE_URL = '/api/alarm-transports/webpush/subscribe'
const UNSUBSCRIBE_URL = '/api/alarm-transports/webpush/unsubscribe'

/**
 * The Push API wants applicationServerKey as raw bytes. Chrome tolerates a
 * base64 string; Safari does not, so there is one code path — the one that
 * works everywhere.
 */
export function urlBase64ToUint8Array(base64: string): Uint8Array<ArrayBuffer> {
  const padded = base64.padEnd(base64.length + ((4 - (base64.length % 4)) % 4), '=')
  const binary = atob(padded.replace(/-/g, '+').replace(/_/g, '/'))

  // Backed by an explicit ArrayBuffer, not the default ArrayBufferLike: since
  // TypeScript 5.7 a Uint8Array is generic over its buffer, and only the
  // ArrayBuffer form satisfies BufferSource — which is what
  // PushManager.subscribe's applicationServerKey requires.
  const bytes = new Uint8Array(new ArrayBuffer(binary.length))
  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i)
  return bytes
}

function isAppleMobile(): boolean {
  const ua = navigator.userAgent
  // iPadOS reports itself as a Mac, and only maxTouchPoints gives it away.
  return /iP(hone|ad|od)/.test(ua) || (navigator.maxTouchPoints > 1 && /Macintosh/.test(ua))
}

function isInstalledToHomeScreen(): boolean {
  if (typeof window.matchMedia !== 'function') return false
  return window.matchMedia('(display-mode: standalone)').matches
}

function notificationPermission(): NotificationPermission | null {
  // A bare Notification.permission throws on some iOS Safari builds.
  if (!('Notification' in window)) return null
  return Notification.permission
}

/**
 * Resolved in this order deliberately. On http:// every other capability is
 * missing *as a consequence* of the insecure context, so checking support first
 * would tell a Chrome user their browser cannot do push and send them chasing
 * the wrong problem. localhost is a secure context, so dev needs no special case.
 */
function detectSupport(): WebPushSupport {
  if (!window.isSecureContext) return { kind: 'insecure-context' }
  if (!('serviceWorker' in navigator)) return { kind: 'no-service-worker' }

  if (!('PushManager' in window)) {
    // Safari on iOS exposes serviceWorker in an ordinary tab but withholds
    // PushManager until the site is installed to the Home Screen.
    if (isAppleMobile() && !isInstalledToHomeScreen()) return { kind: 'ios-not-installed' }
    return { kind: 'unsupported' }
  }

  if (notificationPermission() === 'denied') return { kind: 'blocked' }
  return { kind: 'ok' }
}

export function useWebPush() {
  const [support, setSupport] = useState<WebPushSupport>({ kind: 'insecure-context' })
  const [subscribed, setSubscribed] = useState(false)
  const [deviceCount, setDeviceCount] = useState(0)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    const detected = detectSupport()
    setSupport(detected)
    if (detected.kind !== 'ok') return

    try {
      const response = await fetch(KEY_URL)
      if (response.ok) {
        const payload = (await response.json()) as { subscribed_devices?: number }
        setDeviceCount(payload.subscribed_devices ?? 0)
      }

      // Only ask about an existing subscription if a worker is already
      // registered: registering one here would install a service worker on
      // every dashboard that merely opened the settings page.
      const registration = await navigator.serviceWorker.getRegistration?.(SERVICE_WORKER_PATH)
      if (registration) {
        setSubscribed((await registration.pushManager.getSubscription()) !== null)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [])

  useEffect(() => { void refresh() }, [refresh])

  const enableOnThisDevice = useCallback(async (label: string) => {
    setBusy(true)
    setError(null)
    try {
      // First, and with nothing awaited before it: Safari throws
      // NotAllowedError unless requestPermission is synchronously reachable
      // from the user gesture that started this.
      const permission = await Notification.requestPermission()
      if (permission !== 'granted') {
        setSupport(permission === 'denied' ? { kind: 'blocked' } : detectSupport())
        throw new Error('Notifications were not allowed for this site.')
      }

      const keyResponse = await fetch(KEY_URL)
      if (!keyResponse.ok) {
        const body = (await keyResponse.json().catch(() => ({}))) as { error?: string }
        throw new Error(body.error ?? 'Could not read the push key from Helmcentral.')
      }
      const { public_key: publicKey } = (await keyResponse.json()) as { public_key: string }

      // Registered lazily, on this click. A helm kiosk that will never want
      // push should not have a service worker installed on first paint.
      await navigator.serviceWorker.register(SERVICE_WORKER_PATH, { scope: '/' })
      const registration = await navigator.serviceWorker.ready

      const subscription = await registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: urlBase64ToUint8Array(publicKey),
      })

      const response = await fetch(SUBSCRIBE_URL, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          ...subscription.toJSON(),
          label: label.trim(),
          user_agent: navigator.userAgent,
        }),
      })
      if (!response.ok) {
        const body = (await response.json().catch(() => ({}))) as { error?: string }
        throw new Error(body.error ?? 'Helmcentral rejected this device.')
      }

      setSubscribed(true)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }, [refresh])

  const disableOnThisDevice = useCallback(async () => {
    setBusy(true)
    setError(null)
    try {
      const registration = await navigator.serviceWorker.getRegistration?.(SERVICE_WORKER_PATH)
      const subscription = registration ? await registration.pushManager.getSubscription() : null
      if (!subscription) {
        setSubscribed(false)
        return
      }

      // Server first. If the browser's own unsubscribe then fails, the row is
      // already gone and nothing pushes at a dead endpoint; the reverse order
      // leaves Helmcentral pushing at a revoked endpoint until a 410 prunes it.
      await fetch(UNSUBSCRIBE_URL, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ endpoint: subscription.endpoint }),
      })
      await subscription.unsubscribe()

      setSubscribed(false)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }, [refresh])

  return {
    support,
    permission: notificationPermission(),
    subscribed,
    deviceCount,
    busy,
    error,
    enableOnThisDevice,
    disableOnThisDevice,
  }
}
