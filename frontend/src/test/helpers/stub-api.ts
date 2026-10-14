import { vi } from 'vitest'

// One fetch stub for tests of anything that talks to /api. Routes are keyed
// 'METHOD /path'. The query string is ignored unless the key includes '?',
// in which case path and query must both match exactly. A request no route
// matches throws, so a test never passes against a default answer the real
// server would not give (AGENTS.md fallback policy).

export interface StubRequest {
  url: string
  method: string
  body: unknown
}

type RouteValue = unknown | ((req: StubRequest) => unknown | Response | Promise<unknown>)

export function jsonResponse(body: unknown, status = 200): Response {
  if (status === 204 || body === undefined) return new Response(null, { status })
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function parseBody(raw: BodyInit | null | undefined): unknown {
  if (typeof raw !== 'string') return undefined
  try {
    return JSON.parse(raw)
  } catch {
    return raw
  }
}

export function stubApi(routes: Record<string, RouteValue>) {
  const mock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const raw = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    const parsed = new URL(raw, 'http://stub.invalid')
    const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase()
    const pathOnly = `${method} ${parsed.pathname}`
    const withQuery = `${method} ${parsed.pathname}${parsed.search}`
    const key =
      Object.keys(routes).find((k) => k.includes('?') && k === withQuery) ??
      Object.keys(routes).find((k) => !k.includes('?') && k === pathOnly)
    if (key === undefined) {
      throw new Error(
        `stubApi: no route for ${withQuery}. Routes: ${Object.keys(routes).join(', ') || '(none)'}`,
      )
    }
    const value = routes[key]
    const result =
      typeof value === 'function'
        ? await (value as (req: StubRequest) => unknown)({
            url: raw,
            method,
            body: parseBody(init?.body),
          })
        : value
    return result instanceof Response ? result : jsonResponse(result)
  })
  vi.stubGlobal('fetch', mock)
  return mock
}
