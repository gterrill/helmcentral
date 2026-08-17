import { describe, expect, it, vi, beforeEach } from 'vitest'

import { refreshAuthState } from '@/hooks/use-auth'

/**
 * Turning authentication on has to take effect in the tab that turned it on.
 * Without this the operator saves "require login", sees the dashboard carry on
 * exactly as before, and has no signal that anything happened — while every
 * request is in fact now unauthenticated.
 */
describe('auth mode change forces re-authentication', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('re-reads mode and session, so enabling auth drops to the login gate', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/api/auth/mode')) {
        return new Response(JSON.stringify({ mode: 'signalk' }), { status: 200 })
      }
      if (url.includes('/api/auth/me')) {
        return new Response(JSON.stringify({ authenticated: false }), { status: 200 })
      }
      return new Response('{}', { status: 200 })
    })
    vi.stubGlobal('fetch', fetchMock)

    const state = await refreshAuthState()

    // App.tsx gates on exactly this pair: mode signalk with no user renders
    // the login screen.
    expect(state.mode).toBe('signalk')
    expect(state.user).toBeNull()

    const called = fetchMock.mock.calls.map((c) => String(c[0]))
    expect(called.some((u) => u.includes('/api/auth/mode'))).toBe(true)
    expect(called.some((u) => u.includes('/api/auth/me'))).toBe(true)
  })

  it('keeps an authenticated user signed in when the mode is unchanged', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/api/auth/mode')) {
        return new Response(JSON.stringify({ mode: 'signalk' }), { status: 200 })
      }
      if (url.includes('/api/auth/me')) {
        return new Response(JSON.stringify({ authenticated: true, username: 'skipper', role: 'admin' }), { status: 200 })
      }
      return new Response('{}', { status: 200 })
    })
    vi.stubGlobal('fetch', fetchMock)

    const state = await refreshAuthState()

    expect(state.mode).toBe('signalk')
    expect(state.user).toEqual({ username: 'skipper', role: 'admin' })
  })
})
