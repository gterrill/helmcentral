import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'

// use-auth.ts (ADR 0040) owns the frontend half of SignalK delegated
// authentication: /api/auth/mode + /api/auth/me on mount, login/logout, a
// shared 401 handler (any /api response of 401 drops back to the login
// screen), and the SSE-blip regression guard — a flaky boat LAN reconnecting
// the shared EventSource must never be treated as a logout on its own; only
// a confirmed authenticated:false from /api/auth/me may do that.
//
// use-telemetry-stream.ts's own reconnect/backoff logic is out of scope here
// (it has its own test file) and is not re-tested — it is mocked so this
// file can drive its status broadcast directly.

const { statusListeners } = vi.hoisted(() => ({
  statusListeners: new Set<(status: string) => void>(),
}))

vi.mock('@/hooks/use-telemetry-stream', () => ({
  subscribeTelemetryStatus: (cb: (status: string) => void) => {
    statusListeners.add(cb)
    return () => { statusListeners.delete(cb) }
  },
}))

function emitTelemetryStatus(status: 'connected' | 'reconnecting' | 'disconnected') {
  for (const listener of statusListeners) listener(status)
}

const okJson = (body: unknown) => ({ ok: true, status: 200, json: async () => body })
const errJson = (status: number, body: unknown) => ({ ok: false, status, json: async () => body })

async function loadModule() {
  vi.resetModules()
  statusListeners.clear()
  return import('@/hooks/use-auth')
}

describe('useAuth', () => {
  beforeEach(() => {
    vi.resetModules()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('resolves mode "none" and stays unauthenticated when there is no session', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(okJson({ mode: 'none' }))
      .mockResolvedValueOnce(okJson({ authenticated: false }))
    vi.stubGlobal('fetch', fetchMock)

    const { useAuth } = await loadModule()
    const { result } = renderHook(() => useAuth())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.mode).toBe('none')
    expect(result.current.user).toBeNull()
    expect(result.current.role).toBeNull()
  })

  it('resolves mode "signalk" and reflects an existing authenticated session', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(okJson({ mode: 'signalk' }))
      .mockResolvedValueOnce(okJson({ authenticated: true, username: 'skipper', role: 'admin' }))
    vi.stubGlobal('fetch', fetchMock)

    const { useAuth } = await loadModule()
    const { result } = renderHook(() => useAuth())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.mode).toBe('signalk')
    expect(result.current.user).toEqual({ username: 'skipper', role: 'admin' })
    expect(result.current.role).toBe('admin')
  })

  it('goes from unauthenticated to authenticated via login()', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(okJson({ mode: 'signalk' }))
      .mockResolvedValueOnce(okJson({ authenticated: false }))
      .mockResolvedValueOnce(okJson({ authenticated: true, username: 'mate', role: 'readwrite' }))
    vi.stubGlobal('fetch', fetchMock)

    const { useAuth } = await loadModule()
    const { result } = renderHook(() => useAuth())
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.user).toBeNull()

    let loginResult: boolean | undefined
    await act(async () => {
      loginResult = await result.current.login('mate', 'hunter2')
    })

    expect(loginResult).toBe(true)
    expect(result.current.user).toEqual({ username: 'mate', role: 'readwrite' })
    expect(result.current.role).toBe('readwrite')
    expect(result.current.error).toBeNull()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/auth/login',
      expect.objectContaining({
        method: 'POST',
        credentials: 'include',
        body: JSON.stringify({ username: 'mate', password: 'hunter2' }),
      }),
    )
  })

  it('logout() clears the authenticated user', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(okJson({ mode: 'signalk' }))
      .mockResolvedValueOnce(okJson({ authenticated: true, username: 'skipper', role: 'admin' }))
      .mockResolvedValueOnce(okJson({ ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    const { useAuth } = await loadModule()
    const { result } = renderHook(() => useAuth())
    await waitFor(() => expect(result.current.user).not.toBeNull())

    await act(async () => {
      await result.current.logout()
    })

    expect(result.current.user).toBeNull()
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/auth/logout',
      expect.objectContaining({ method: 'POST', credentials: 'include' }),
    )
  })

  it('a failed login surfaces the backend error message verbatim, not a generic string', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(okJson({ mode: 'signalk' }))
      .mockResolvedValueOnce(okJson({ authenticated: false }))
      .mockResolvedValueOnce(errJson(401, { error: 'signalk rejected login: invalid username or password' }))
    vi.stubGlobal('fetch', fetchMock)

    const { useAuth } = await loadModule()
    const { result } = renderHook(() => useAuth())
    await waitFor(() => expect(result.current.loading).toBe(false))

    let loginResult: boolean | undefined
    await act(async () => {
      loginResult = await result.current.login('bad', 'creds')
    })

    expect(loginResult).toBe(false)
    expect(result.current.user).toBeNull()
    expect(result.current.error).toBe('signalk rejected login: invalid username or password')
  })

  it('distinguishes the 403 unrecognised-userLevel case from a plain 401 by its own exact message', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(okJson({ mode: 'signalk' }))
      .mockResolvedValueOnce(okJson({ authenticated: false }))
      .mockResolvedValueOnce(errJson(403, {
        error: 'signalk reported unrecognised userLevel "superuser" — refusing to log in rather than guessing a permission tier',
      }))
    vi.stubGlobal('fetch', fetchMock)

    const { useAuth } = await loadModule()
    const { result } = renderHook(() => useAuth())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.login('odd', 'creds')
    })

    expect(result.current.error).toBe(
      'signalk reported unrecognised userLevel "superuser" — refusing to log in rather than guessing a permission tier',
    )
    // Not the 401 bad-credentials message from the previous test — the two
    // failure modes must stay distinguishable so the operator isn't told
    // "wrong password" when the real problem is an unmapped SignalK role.
    expect(result.current.error).not.toMatch(/invalid username or password/)
  })

  it('mode "none" never shows a login-required state even without a session', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(okJson({ mode: 'none' }))
      .mockResolvedValueOnce(okJson({ authenticated: false }))
    vi.stubGlobal('fetch', fetchMock)

    const { useAuth } = await loadModule()
    const { result } = renderHook(() => useAuth())
    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(result.current.mode).toBe('none')
    // No behaviour change in mode:none — the hook must not need a login call
    // to make the dashboard usable, and must not carry an error just because
    // there is no session.
    expect(result.current.error).toBeNull()
  })

  // ── the shared 401 handler ────────────────────────────────────────────────

  it('any /api response of 401 — even from a totally unrelated hook\'s fetch call — clears the authenticated user', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(okJson({ mode: 'signalk' }))
      .mockResolvedValueOnce(okJson({ authenticated: true, username: 'skipper', role: 'admin' }))
      .mockResolvedValueOnce(errJson(401, { error: 'session expired' }))
    vi.stubGlobal('fetch', fetchMock)

    const { useAuth } = await loadModule()
    const { result } = renderHook(() => useAuth())
    await waitFor(() => expect(result.current.user).not.toBeNull())

    // Simulate some unrelated hook (e.g. use-czone-switches.ts) making its own
    // bare fetch() call and getting a 401 back — global fetch is patched by
    // use-auth.ts's module load, so this must be observed centrally.
    await act(async () => {
      await fetch('/api/czone/switches')
    })

    expect(result.current.user).toBeNull()
  })

  it('does not react to a 403 from an unrelated call — that is a permission problem, not a dead session', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(okJson({ mode: 'signalk' }))
      .mockResolvedValueOnce(okJson({ authenticated: true, username: 'skipper', role: 'readonly' }))
      .mockResolvedValueOnce(errJson(403, { error: 'requires readwrite' }))
    vi.stubGlobal('fetch', fetchMock)

    const { useAuth } = await loadModule()
    const { result } = renderHook(() => useAuth())
    await waitFor(() => expect(result.current.user).not.toBeNull())

    await act(async () => {
      await fetch('/api/czone/switches/1/state')
    })

    expect(result.current.user).not.toBeNull()
  })

  // ── the SSE-blip regression: the critical case ───────────────────────────

  it('an EventSource reconnect blip does NOT log the helm out when /api/auth/me still reports authenticated', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(okJson({ mode: 'signalk' })) // GET /api/auth/mode
      .mockResolvedValueOnce(okJson({ authenticated: true, username: 'skipper', role: 'admin' })) // GET /api/auth/me (mount)
      .mockResolvedValueOnce(okJson({ authenticated: true, username: 'skipper', role: 'admin' })) // GET /api/auth/me (re-check on blip)
    vi.stubGlobal('fetch', fetchMock)

    const { useAuth } = await loadModule()
    const { result } = renderHook(() => useAuth())
    await waitFor(() => expect(result.current.user).not.toBeNull())

    await act(async () => {
      emitTelemetryStatus('reconnecting')
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(result.current.user).not.toBeNull()
    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(fetchMock).toHaveBeenLastCalledWith('/api/auth/me', expect.objectContaining({ credentials: 'include' }))
  })

  it('falls back to the login screen only once /api/auth/me confirms the session is actually gone', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(okJson({ mode: 'signalk' }))
      .mockResolvedValueOnce(okJson({ authenticated: true, username: 'skipper', role: 'admin' }))
      .mockResolvedValueOnce(okJson({ authenticated: false })) // re-check on blip: session really is gone
    vi.stubGlobal('fetch', fetchMock)

    const { useAuth } = await loadModule()
    const { result } = renderHook(() => useAuth())
    await waitFor(() => expect(result.current.user).not.toBeNull())

    await act(async () => {
      emitTelemetryStatus('reconnecting')
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(result.current.user).toBeNull()
  })

  it('does not re-check auth on an SSE blip in mode:none', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(okJson({ mode: 'none' }))
      .mockResolvedValueOnce(okJson({ authenticated: false }))
    vi.stubGlobal('fetch', fetchMock)

    const { useAuth } = await loadModule()
    const { result } = renderHook(() => useAuth())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      emitTelemetryStatus('reconnecting')
      await Promise.resolve()
    })

    // Only the two mount-time calls (mode, me) — no extra /api/auth/me probe,
    // since mode:none never shows a login screen regardless of what /me says.
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
