import { useEffect, useState } from 'react'

import { apiBaseUrl } from '@/config/api'
import { subscribeTelemetryStatus } from '@/hooks/use-telemetry-stream'

/**
 * Frontend half of SignalK delegated authentication (ADR 0040,
 * backend/auth_handlers.go). The session itself lives entirely in an
 * HttpOnly cookie the browser manages — there is no token for this module to
 * hold or refresh, only the resolved `{mode, user}` the backend already
 * computed from it.
 */

export type AuthMode = 'none' | 'signalk'

export interface AuthUser {
  username: string
  role: string
}

export interface UseAuthResult {
  /** null until GET /api/auth/mode has answered. */
  mode: AuthMode | null
  user: AuthUser | null
  /** Convenience mirror of user?.role, since most call sites only need this. */
  role: string | null
  loading: boolean
  /** The server's own message from the most recent failed login, verbatim. */
  error: string | null
  login: (username: string, password: string) => Promise<boolean>
  logout: () => Promise<void>
}

interface AuthState {
  mode: AuthMode | null
  user: AuthUser | null
  loading: boolean
  error: string | null
}

const initialState: AuthState = { mode: null, user: null, loading: true, error: null }

// Module-level singleton, mirroring use-app-config.ts's pattern: this state
// is mutated from two places outside any single hook instance's own render
// cycle — the shared 401 handler below, and the SSE-blip re-check — so it
// has to live where both can reach it, with React components subscribing
// rather than owning it.
let current: AuthState = initialState
let inflight: Promise<void> | null = null
const listeners = new Set<(state: AuthState) => void>()

function publish(next: AuthState): void {
  current = next
  for (const listener of listeners) listener(next)
}

interface MeResponse {
  authenticated: boolean
  username?: string
  role?: string
}

function userFromMeResponse(body: MeResponse): AuthUser | null {
  if (!body.authenticated || !body.username || !body.role) return null
  return { username: body.username, role: body.role }
}

interface LoginResponse {
  authenticated?: boolean
  username?: string
  role?: string
  error?: string
}

async function loadModeAndMe(): Promise<void> {
  try {
    const modeRes = await fetch(`${apiBaseUrl}/api/auth/mode`)
    if (!modeRes.ok) {
      throw new Error(`Failed to fetch auth mode (${modeRes.status})`)
    }
    const modeBody = (await modeRes.json()) as { mode?: string }
    // The backend contract is exactly "none" | "signalk" (docs/adr/0040) —
    // anything else is surfaced rather than quietly coerced to a default,
    // per this project's no-masking-fallback policy.
    if (modeBody.mode !== 'none' && modeBody.mode !== 'signalk') {
      throw new Error(`Unexpected value from /api/auth/mode: ${JSON.stringify(modeBody.mode)}`)
    }
    const mode = modeBody.mode

    const meRes = await fetch(`${apiBaseUrl}/api/auth/me`, { credentials: 'include' })
    if (!meRes.ok) {
      throw new Error(`Failed to fetch auth status (${meRes.status})`)
    }
    const meBody = (await meRes.json()) as MeResponse

    publish({ mode, user: userFromMeResponse(meBody), loading: false, error: null })
  } catch (err) {
    publish({
      ...current,
      loading: false,
      error: err instanceof Error ? err.message : 'Failed to determine auth state',
    })
  }
}

/**
 * Re-reads mode and session, and returns the resulting state.
 *
 * Called after a settings save so a change to auth.mode takes effect in the
 * tab that made it. Without this the operator turns on "require login", sees
 * the dashboard carry on unchanged, and gets no signal that anything happened
 * — while every request from that tab is in fact now unauthenticated. Since
 * App.tsx gates on `mode === 'signalk' && user === null`, re-reading both is
 * enough to drop straight to the login screen.
 *
 * Unlike verifySession below this deliberately re-reads `mode` too, because
 * the mode is precisely what changed.
 */
export async function refreshAuthState(): Promise<{ mode: AuthMode | null; user: AuthUser | null }> {
  await loadModeAndMe()
  return { mode: current.mode, user: current.user }
}

/**
 * Re-checks GET /api/auth/me without touching `mode` or `error` — used only
 * by the SSE-blip guard below. A transport failure here (backend mid-restart,
 * LAN drop) is left alone rather than treated as a logout: the whole point of
 * this check is telling "the session actually died" apart from "the network
 * blipped", and a fetch that can't even complete is evidence of the latter,
 * not the former.
 */
async function verifySession(): Promise<void> {
  try {
    const res = await fetch(`${apiBaseUrl}/api/auth/me`, { credentials: 'include' })
    if (!res.ok) return
    const body = (await res.json()) as MeResponse
    publish({ ...current, user: userFromMeResponse(body) })
  } catch {
    // Unreachable during the blip — not proof of a dead session.
  }
}

async function login(username: string, password: string): Promise<boolean> {
  publish({ ...current, error: null })
  try {
    const res = await fetch(`${apiBaseUrl}/api/auth/login`, {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    })
    const body = (await res.json().catch(() => null)) as LoginResponse | null

    if (!res.ok) {
      // Surfaced verbatim: this is the one clue distinguishing bad
      // credentials from an unreachable SignalK server from an unrecognised
      // userLevel (docs/adr/0040) — flattening it to "login failed" would
      // destroy that.
      publish({ ...current, error: body?.error || `Login failed (${res.status})` })
      return false
    }

    const user = body ? userFromMeResponse({ authenticated: true, username: body.username, role: body.role }) : null
    if (!user) {
      publish({ ...current, error: 'Login response was missing the expected fields' })
      return false
    }

    publish({ ...current, user, error: null })
    return true
  } catch (err) {
    publish({ ...current, error: err instanceof Error ? err.message : 'Could not reach the server' })
    return false
  }
}

async function logout(): Promise<void> {
  try {
    await fetch(`${apiBaseUrl}/api/auth/logout`, { method: 'POST', credentials: 'include' })
  } finally {
    // Clears client-side regardless of whether the request itself landed —
    // the cookie carries its own 7-day expiry either way, and "Log out"
    // should never leave the operator staring at a dashboard that didn't
    // visibly respond.
    publish({ ...current, user: null, error: null })
  }
}

// ── the shared 401 handler ───────────────────────────────────────────────
//
// Hooks across this codebase call fetch('/api/...') independently and
// inconsistently (some via apiBaseUrl, most bare) — there is no single choke
// point to add a per-call check to without touching every one of them.
// Patching the one global `fetch` all of them ultimately call is the only
// place a 401 from *any* of them can be caught centrally. Installed once, at
// module load, so it is active before any other hook's mount effect can
// possibly issue its first request.

let fetchPatched = false

function isApiPath(input: RequestInfo | URL): boolean {
  const raw = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url
  try {
    return new URL(raw, window.location.origin).pathname.startsWith('/api/')
  } catch {
    return false
  }
}

function installUnauthorizedInterceptor(): void {
  if (fetchPatched) return
  fetchPatched = true
  const nativeFetch = globalThis.fetch.bind(globalThis)
  globalThis.fetch = (async (...args: Parameters<typeof fetch>) => {
    const response = await nativeFetch(...args)
    // Only 401 — a 403 is a permission problem for an authenticated user
    // (the role-gating in App.tsx is cosmetic; the server enforcement is
    // what actually returns 403), not a dead session. Reacting to 403 here
    // would log a readonly user out for tapping a disabled-looking control
    // that somehow still reached the server.
    if (response.status === 401 && isApiPath(args[0])) {
      publish({ ...current, user: null })
    }
    return response
  }) as typeof fetch
}

installUnauthorizedInterceptor()

export function useAuth(): UseAuthResult {
  const [state, setState] = useState<AuthState>(current)

  useEffect(() => {
    listeners.add(setState)
    // Re-sync in case `current` moved between render and this effect.
    if (current !== state) setState(current)

    if (current.loading && !inflight) {
      inflight = loadModeAndMe().finally(() => { inflight = null })
    }

    return () => {
      listeners.delete(setState)
    }
    // Subscribe once per consumer; `state` is intentionally not a dependency.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // The SSE-blip guard (docs/adr/0040 §frontend, use-telemetry-stream.ts):
  // a flaky boat LAN drives the shared EventSource into 'reconnecting'
  // constantly, and none of that is evidence the session died. Only once
  // reconnecting is observed AND the mode is signalk AND a user is currently
  // believed authenticated does this re-check /api/auth/me — and only THAT
  // response, not the SSE error itself, can clear `user`. This never touches
  // use-telemetry-stream.ts's own reconnect/backoff loop.
  useEffect(() => {
    return subscribeTelemetryStatus((status) => {
      if (status !== 'reconnecting') return
      if (current.mode !== 'signalk' || current.user === null) return
      void verifySession()
    })
  }, [])

  return {
    mode: state.mode,
    user: state.user,
    role: state.user?.role ?? null,
    loading: state.loading,
    error: state.error,
    login,
    logout,
  }
}
