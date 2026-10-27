import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  ApiError,
  GC_TIME_MS,
  apiRequest,
  getQueryData,
  invalidateQueries,
  resetQueryStore,
  setQueryData,
  useApiQuery,
  useApiQuerySelector,
} from '@/lib/api-query'
import { jsonResponse, stubApi } from './helpers/stub-api'

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

async function failure(p: Promise<unknown>): Promise<ApiError> {
  return p.then(
    () => {
      throw new Error('expected rejection')
    },
    (e: ApiError) => e,
  )
}

function deferred<T>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}

describe('apiRequest', () => {
  it('fetches the path as given when no base URL is configured', async () => {
    const mock = stubApi({ 'GET /api/x': { a: 1 } })
    await expect(apiRequest('/api/x')).resolves.toEqual({ a: 1 })
    expect(mock.mock.calls[0][0]).toBe('/api/x')
  })

  it('sends json as a body with a JSON content type', async () => {
    const mock = stubApi({ 'POST /api/x': ({ body }: { body: unknown }) => ({ echoed: body }) })
    const out = await apiRequest('/api/x', { method: 'POST', json: { n: 2 } })
    expect(out).toEqual({ echoed: { n: 2 } })
    const init = mock.mock.calls[0][1] as RequestInit
    expect(init.body).toBe('{"n":2}')
    expect(new Headers(init.headers).get('Content-Type')).toBe('application/json')
  })

  it('returns undefined for 204', async () => {
    stubApi({ 'DELETE /api/x': () => new Response(null, { status: 204 }) })
    await expect(apiRequest('/api/x', { method: 'DELETE' })).resolves.toBeUndefined()
  })

  it('fails on a 200 whose body is not JSON', async () => {
    stubApi({ 'GET /api/empty': () => new Response('', { status: 200 }) })
    await expect(apiRequest('/api/empty')).rejects.toThrow()
  })

  it('reads the body with json() so plain { ok, status, json } mocks work', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, status: 200, json: async () => ({ a: 1 }) })))
    await expect(apiRequest('/api/x')).resolves.toEqual({ a: 1 })
  })

  it('uses the server error message', async () => {
    stubApi({ 'GET /api/x': () => jsonResponse({ error: 'zone is in use' }, 409) })
    const err = await failure(apiRequest('/api/x'))
    expect(err).toBeInstanceOf(ApiError)
    expect(err.message).toBe('zone is in use')
    expect(err.status).toBe(409)
    expect(err.fieldErrors).toBeUndefined()
  })

  it('falls back to HTTP status for a body that is not JSON or has no message', async () => {
    stubApi({
      'GET /api/a': () => new Response('<html>bad gateway</html>', { status: 502 }),
      'GET /api/b': () => jsonResponse({}, 500),
      'GET /api/c': () => jsonResponse({ error: '' }, 500),
    })
    expect((await failure(apiRequest('/api/a'))).message).toBe('HTTP 502')
    expect((await failure(apiRequest('/api/b'))).message).toBe('HTTP 500')
    expect((await failure(apiRequest('/api/c'))).message).toBe('HTTP 500')
  })

  it('reads a single {field, message} body into fieldErrors', async () => {
    stubApi({ 'POST /api/x': () => jsonResponse({ field: 'install_date', message: 'bad date' }, 400) })
    const err = await failure(apiRequest('/api/x', { method: 'POST', json: {} }))
    expect(err.message).toBe('bad date')
    expect(err.fieldErrors).toEqual({ install_date: 'bad date' })
    expect(err.status).toBe(400)
  })

  it('reads an {errors: [...]} body into fieldErrors and joins the messages', async () => {
    stubApi({
      'POST /api/x': () =>
        jsonResponse(
          { errors: [{ field: 'name', message: 'required' }, { field: 'qty', message: 'too big' }] },
          400,
        ),
    })
    const err = await failure(apiRequest('/api/x', { method: 'POST', json: {} }))
    expect(err.message).toBe('required; too big')
    expect(err.fieldErrors).toEqual({ name: 'required', qty: 'too big' })
  })

  it('lets a network failure through unchanged', async () => {
    const boom = new TypeError('Failed to fetch')
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(boom))
    await expect(apiRequest('/api/x')).rejects.toBe(boom)
  })
})

describe('query store', () => {
  beforeEach(() => {
    resetQueryStore()
  })

  const opts = (over: Record<string, unknown> = {}) => ({
    queryKey: ['items'] as const,
    path: '/api/items',
    ...over,
  })

  // Deferred responses for the review's ordering cases: each call to a route
  // parks until the test resolves it.
  function deferredRoute() {
    const pending: Array<(body: unknown) => void> = []
    const handler = () => new Promise<Response>((resolve) => pending.push((body) => resolve(jsonResponse(body))))
    return { handler, pending }
  }

  it('refetch after a write supersedes a read already in flight', async () => {
    const route = deferredRoute()
    stubApi({ 'GET /api/items': route.handler })
    const { result } = renderHook(() => useApiQuery<number[]>(opts()))
    await waitFor(() => expect(route.pending).toHaveLength(1))
    let refetched = false
    act(() => {
      void result.current.refetch().then(() => { refetched = true })
    })
    await waitFor(() => expect(route.pending).toHaveLength(2))
    await act(async () => { route.pending[0]([1, 2]) })
    expect(refetched).toBe(false)
    expect(result.current.data).toBeUndefined()
    await act(async () => { route.pending[1]([1]) })
    await waitFor(() => expect(refetched).toBe(true))
    expect(result.current.data).toEqual([1])
  })

  it('setQueryData wins over a read that started before it', async () => {
    const route = deferredRoute()
    stubApi({ 'GET /api/items': route.handler })
    const { result } = renderHook(() => useApiQuery<number[]>(opts()))
    await waitFor(() => expect(route.pending).toHaveLength(1))
    act(() => setQueryData(['items'], [9]))
    await act(async () => { route.pending[0]([1, 2]) })
    expect(result.current.data).toEqual([9])
    expect(result.current.isFetching).toBe(false)
  })

  it('setQueryData during the first load does not store a fragment, and the full list comes from a fresh read', async () => {
    const first = deferred<number[]>()
    let calls = 0
    stubApi({ 'GET /api/items': () => (++calls === 1 ? first.promise : [1, 2, 9]) })
    const { result } = renderHook(() => useApiQuery<number[]>(opts()))
    await waitFor(() => expect(calls).toBe(1))
    act(() => setQueryData<number[]>(['items'], (prev) => [...(prev ?? []), 9]))
    expect(result.current.data).toBeUndefined()
    await waitFor(() => expect(result.current.data).toEqual([1, 2, 9]))
    expect(calls).toBe(2)
    await act(async () => { first.resolve([1, 2]) })
    expect(result.current.data).toEqual([1, 2, 9])
  })

  it('a fragment write whose fresh read fails leaves the error, not the fragment as the list', async () => {
    const first = deferred<number[]>()
    let calls = 0
    stubApi({ 'GET /api/items': () => (++calls === 1 ? first.promise : jsonResponse({ error: 'down' }, 502)) })
    const { result } = renderHook(() => useApiQuery<number[]>(opts()))
    await waitFor(() => expect(calls).toBe(1))
    act(() => setQueryData<number[]>(['items'], (prev) => [...(prev ?? []), 9]))

    await waitFor(() => expect(result.current.error).toBeInstanceOf(Error))
    expect(result.current.data).toBeUndefined()
    expect(getQueryData(['items'])).toBeUndefined()
  })

  it('setQueryData with an updater over an entry whose load failed is followed by a fresh read', async () => {
    let calls = 0
    stubApi({ 'GET /api/items': () => (++calls === 1 ? jsonResponse({ error: 'down' }, 502) : [1, 2, 9]) })
    const { result } = renderHook(() => useApiQuery<number[]>(opts()))
    await waitFor(() => expect(result.current.error).toBeInstanceOf(Error))

    act(() => setQueryData<number[]>(['items'], (prev) => [...(prev ?? []), 9]))

    await waitFor(() => expect(result.current.data).toEqual([1, 2, 9]))
    expect(calls).toBe(2)
  })

  it('a select that throws lands in error instead of throwing during render', async () => {
    stubApi({ 'GET /api/items': null })
    const { result } = renderHook(() =>
      useApiQuery<number[]>(opts({ select: (raw: { items: number[] }) => raw.items.map((n) => n) })),
    )
    await waitFor(() => expect(result.current.error).toBeInstanceOf(Error))
    expect(result.current.data).toBeUndefined()
    expect(result.current.isLoading).toBe(false)
  })

  it('a 204 answer is loaded, not loading forever', async () => {
    stubApi({ 'GET /api/items': () => new Response(null, { status: 204 }) })
    const { result } = renderHook(() => useApiQuery<number[]>(opts()))
    await waitFor(() => expect(result.current.isFetching).toBe(false))
    expect(result.current.isLoading).toBe(false)
    expect(result.current.data).toBeUndefined()
  })

  it('loads data and reports isLoading then data', async () => {
    stubApi({ 'GET /api/items': [1, 2] })
    const { result } = renderHook(() => useApiQuery<number[]>(opts()))
    expect(result.current.isLoading).toBe(true)
    expect(result.current.data).toBeUndefined()
    await waitFor(() => expect(result.current.data).toEqual([1, 2]))
    expect(result.current.isLoading).toBe(false)
    expect(result.current.isFetching).toBe(false)
    expect(result.current.error).toBeNull()
  })

  it('applies select to the data', async () => {
    stubApi({ 'GET /api/items': { items: [1, 2] } })
    const { result } = renderHook(() =>
      useApiQuery<number[]>(opts({ select: (d: { items: number[] }) => d.items })),
    )
    await waitFor(() => expect(result.current.data).toEqual([1, 2]))
  })

  it('shares one request between two mounts in the same tick', async () => {
    const mock = stubApi({ 'GET /api/items': [1] })
    const a = renderHook(() => useApiQuery<number[]>(opts()))
    const b = renderHook(() => useApiQuery<number[]>(opts()))
    await waitFor(() => expect(a.result.current.data).toEqual([1]))
    await waitFor(() => expect(b.result.current.data).toEqual([1]))
    expect(mock).toHaveBeenCalledTimes(1)
  })

  it('makes one request under StrictMode', async () => {
    const mock = stubApi({ 'GET /api/items': [1] })
    const { result } = renderHook(() => useApiQuery<number[]>(opts()), { reactStrictMode: true })
    await waitFor(() => expect(result.current.data).toEqual([1]))
    expect(mock).toHaveBeenCalledTimes(1)
  })

  it('discards a response that was in flight when the key was invalidated', async () => {
    const first = deferred<unknown>()
    let calls = 0
    stubApi({
      'GET /api/items': () => {
        calls += 1
        return calls === 1 ? first.promise : ['fresh']
      },
    })
    const { result } = renderHook(() => useApiQuery<string[]>(opts()))
    await waitFor(() => expect(calls).toBe(1))
    act(() => {
      invalidateQueries(['items'])
    })
    await waitFor(() => expect(result.current.data).toEqual(['fresh']))
    await act(async () => {
      first.resolve(['stale'])
      await Promise.resolve()
    })
    expect(result.current.data).toEqual(['fresh'])
    expect(calls).toBe(2)
  })

  it('shows cached data at once on remount and refetches in the background', async () => {
    let n = 0
    stubApi({ 'GET /api/items': () => [++n] })
    const first = renderHook(() => useApiQuery<number[]>(opts()))
    await waitFor(() => expect(first.result.current.data).toEqual([1]))
    first.unmount()
    const second = renderHook(() => useApiQuery<number[]>(opts()))
    expect(second.result.current.data).toEqual([1])
    expect(second.result.current.isLoading).toBe(false)
    await waitFor(() => expect(second.result.current.data).toEqual([2]))
  })

  it('drops the entry after the GC time and loads fresh', async () => {
    vi.useFakeTimers()
    stubApi({ 'GET /api/items': [1] })
    const first = renderHook(() => useApiQuery<number[]>(opts()))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(first.result.current.data).toEqual([1])
    first.unmount()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(GC_TIME_MS - 1)
    })
    expect(getQueryData(['items'])).toEqual([1])
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2)
    })
    expect(getQueryData(['items'])).toBeUndefined()
    const second = renderHook(() => useApiQuery<number[]>(opts()))
    expect(second.result.current.isLoading).toBe(true)
    expect(second.result.current.data).toBeUndefined()
  })

  it('keeps the entry alive when a subscriber returns before GC', async () => {
    vi.useFakeTimers()
    stubApi({ 'GET /api/items': [1] })
    const first = renderHook(() => useApiQuery<number[]>(opts()))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    first.unmount()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(GC_TIME_MS - 1000)
    })
    renderHook(() => useApiQuery<number[]>(opts()))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(GC_TIME_MS)
    })
    expect(getQueryData(['items'])).toEqual([1])
  })

  it('retunes the poll when refetchInterval changes, without fetching again', async () => {
    vi.useFakeTimers()
    const mock = stubApi({ 'GET /api/items': [1] })
    const { rerender } = renderHook(
      ({ ms }: { ms: number }) => useApiQuery<number[]>(opts({ refetchInterval: ms })),
      { initialProps: { ms: 5_000 } },
    )
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(mock).toHaveBeenCalledTimes(1)
    rerender({ ms: 60_000 })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(mock).toHaveBeenCalledTimes(1)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(59_000)
    })
    expect(mock).toHaveBeenCalledTimes(1)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000)
    })
    expect(mock).toHaveBeenCalledTimes(2)
  })

  it('polls at the smallest interval among live subscribers and stops on unmount', async () => {
    vi.useFakeTimers()
    const mock = stubApi({ 'GET /api/items': [1] })
    const slow = renderHook(() => useApiQuery<number[]>(opts({ refetchInterval: 10_000 })))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(mock).toHaveBeenCalledTimes(1)
    const fast = renderHook(() => useApiQuery<number[]>(opts({ refetchInterval: 2_000 })))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    const afterMount = mock.mock.calls.length
    await act(async () => {
      await vi.advanceTimersByTimeAsync(6_000)
    })
    expect(mock.mock.calls.length - afterMount).toBe(3)
    fast.unmount()
    const afterFast = mock.mock.calls.length
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000)
    })
    expect(mock.mock.calls.length - afterFast).toBe(1)
    slow.unmount()
    const afterSlow = mock.mock.calls.length
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000)
    })
    expect(mock.mock.calls.length).toBe(afterSlow)
  })

  it('invalidates by key prefix, refetching mounted keys and leaving others', async () => {
    let a = 0
    let b = 0
    let c = 0
    stubApi({
      'GET /api/a': () => [++a],
      'GET /api/b': () => [++b],
      'GET /api/c': () => [++c],
    })
    const ra = renderHook(() => useApiQuery<number[]>({ queryKey: ['inv', 'a'], path: '/api/a' }))
    const rb = renderHook(() => useApiQuery<number[]>({ queryKey: ['inv', 'b', 1], path: '/api/b' }))
    const rc = renderHook(() => useApiQuery<number[]>({ queryKey: ['other'], path: '/api/c' }))
    await waitFor(() => expect(rc.result.current.data).toEqual([1]))
    await waitFor(() => expect(ra.result.current.data).toEqual([1]))
    await waitFor(() => expect(rb.result.current.data).toEqual([1]))
    act(() => {
      invalidateQueries(['inv'])
    })
    await waitFor(() => expect(ra.result.current.data).toEqual([2]))
    await waitFor(() => expect(rb.result.current.data).toEqual([2]))
    expect(rc.result.current.data).toEqual([1])
    expect(c).toBe(1)
  })

  it('refetches an unmounted key on its next mount', async () => {
    let n = 0
    const mock = stubApi({ 'GET /api/items': () => [++n] })
    const first = renderHook(() => useApiQuery<number[]>(opts()))
    await waitFor(() => expect(first.result.current.data).toEqual([1]))
    first.unmount()
    invalidateQueries(['items'])
    expect(mock).toHaveBeenCalledTimes(1)
    const second = renderHook(() => useApiQuery<number[]>(opts()))
    await waitFor(() => expect(second.result.current.data).toEqual([2]))
  })

  it('setQueryData writes a value or an updater result to subscribers', async () => {
    stubApi({ 'GET /api/items': [1] })
    const { result } = renderHook(() => useApiQuery<number[]>(opts()))
    await waitFor(() => expect(result.current.data).toEqual([1]))
    act(() => {
      setQueryData<number[]>(['items'], [5])
    })
    expect(result.current.data).toEqual([5])
    act(() => {
      setQueryData<number[]>(['items'], (prev) => [...(prev ?? []), 6])
    })
    expect(result.current.data).toEqual([5, 6])
    expect(getQueryData(['items'])).toEqual([5, 6])
  })

  it('does not fetch when enabled is false and is not loading', async () => {
    const mock = stubApi({ 'GET /api/items': [1] })
    const { result } = renderHook(() => useApiQuery<number[]>(opts({ enabled: false })))
    expect(result.current.isLoading).toBe(false)
    expect(result.current.data).toBeUndefined()
    await act(async () => {
      await Promise.resolve()
    })
    expect(mock).not.toHaveBeenCalled()
  })

  it('switches entries on a key change without keeping the previous data', async () => {
    const second = deferred<unknown>()
    stubApi({
      'GET /api/items?f=a': ['a'],
      'GET /api/items?f=b': () => second.promise,
    })
    const { result, rerender } = renderHook(
      ({ f }: { f: string }) =>
        useApiQuery<string[]>({ queryKey: ['items', f], path: `/api/items?f=${f}` }),
      { initialProps: { f: 'a' } },
    )
    await waitFor(() => expect(result.current.data).toEqual(['a']))
    rerender({ f: 'b' })
    expect(result.current.data).toBeUndefined()
    expect(result.current.isLoading).toBe(true)
    await act(async () => {
      second.resolve(['b'])
      await Promise.resolve()
    })
    await waitFor(() => expect(result.current.data).toEqual(['b']))
  })

  it('keeps the last good data beside an error until the next success', async () => {
    let fail = false
    stubApi({
      'GET /api/items': () => (fail ? jsonResponse({ error: 'down' }, 503) : [1]),
    })
    const { result } = renderHook(() => useApiQuery<number[]>(opts()))
    await waitFor(() => expect(result.current.data).toEqual([1]))
    fail = true
    await act(async () => {
      await result.current.refetch()
    })
    expect(result.current.error?.message).toBe('down')
    expect(result.current.data).toEqual([1])
    expect(result.current.isLoading).toBe(false)
    fail = false
    await act(async () => {
      await result.current.refetch()
    })
    expect(result.current.error).toBeNull()
  })

  it('surfaces a first-load failure as error with no data', async () => {
    stubApi({ 'GET /api/items': () => jsonResponse({ error: 'nope' }, 500) })
    const { result } = renderHook(() => useApiQuery<number[]>(opts()))
    await waitFor(() => expect(result.current.error).toBeInstanceOf(ApiError))
    expect(result.current.data).toBeUndefined()
    expect(result.current.isLoading).toBe(false)
  })

  it('refetch resolves after the fetch settles and never throws', async () => {
    let n = 0
    stubApi({
      'GET /api/items': () => (++n === 1 ? [1] : jsonResponse({ error: 'bad' }, 500)),
    })
    const { result } = renderHook(() => useApiQuery<number[]>(opts()))
    await waitFor(() => expect(result.current.data).toEqual([1]))
    let outcome: unknown = 'unset'
    await act(async () => {
      outcome = await result.current.refetch()
    })
    expect(outcome).toBeUndefined()
    expect(result.current.error?.message).toBe('bad')
  })

  it('resetQueryStore clears entries and stops polling', async () => {
    vi.useFakeTimers()
    const mock = stubApi({ 'GET /api/items': [1] })
    const { unmount } = renderHook(() => useApiQuery<number[]>(opts({ refetchInterval: 1_000 })))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(getQueryData(['items'])).toEqual([1])
    resetQueryStore()
    expect(getQueryData(['items'])).toBeUndefined()
    const calls = mock.mock.calls.length
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000)
    })
    expect(mock.mock.calls.length).toBe(calls)
    unmount()
  })
})

describe('useApiQuerySelector', () => {
  beforeEach(() => {
    resetQueryStore()
  })

  const selectCount = (data: { count: number; at: string } | undefined) => data?.count ?? null

  it('re-renders only when its slice changes, not on every request', async () => {
    stubApi({ 'GET /api/items': { count: 1, at: 't0' } })
    let renders = 0
    const { result } = renderHook(() => {
      renders += 1
      return useApiQuerySelector({ queryKey: ['items'], path: '/api/items' }, selectCount)
    })
    await waitFor(() => expect(result.current).toBe(1))
    const settled = renders

    // A poll answering the same count, and one changing only a field the
    // reader does not select: neither is a change to what it shows.
    await act(async () => { setQueryData(['items'], { count: 1, at: 't1' }) })
    await act(async () => { setQueryData(['items'], { count: 1, at: 't2' }) })
    expect(renders).toBe(settled)

    await act(async () => { setQueryData(['items'], { count: 2, at: 't3' }) })
    expect(result.current).toBe(2)
    expect(renders).toBe(settled + 1)
  })

  it('selects from undefined until the first answer lands', () => {
    stubApi({ 'GET /api/items': () => new Promise(() => {}) })
    const { result } = renderHook(() => useApiQuerySelector({ queryKey: ['items'], path: '/api/items' }, selectCount))
    expect(result.current).toBeNull()
  })
})

describe('useApiQuerySelector with a select that throws', () => {
  beforeEach(() => {
    resetQueryStore()
  })

  // Like useApiQuery's select: a body the selector cannot read becomes the
  // query's error, handed to the selector, rather than a throw during render
  // that unmounts the subtree.
  it('hands the selector the error instead of throwing', async () => {
    stubApi({ 'GET /api/items': { count: 1 } })
    const select = (data: { count: number } | null | undefined, error: Error | null) => {
      if (data === null) throw new Error('no body')
      return error ? `error: ${error.message}` : (data?.count ?? 'none')
    }
    const { result } = renderHook(() => useApiQuerySelector({ queryKey: ['items'], path: '/api/items' }, select))
    await waitFor(() => expect(result.current).toBe(1))

    await act(async () => { setQueryData(['items'], null) })

    expect(result.current).toBe('error: no body')
  })
})

// ADR 0215 phase 6: a poll that answers with equal data is nothing a reader
// shows, so it should not cost the reader a render.
describe('useApiQuery and isFetching', () => {
  beforeEach(() => {
    resetQueryStore()
  })

  it('does not re-render a reader that never reads isFetching on a request in flight', async () => {
    const answer = deferred<unknown>()
    let first = true
    stubApi({ 'GET /api/items': () => { if (first) { first = false; return { count: 1 } } return answer.promise } })
    let renders = 0
    const { result } = renderHook(() => {
      renders += 1
      const { data } = useApiQuery<{ count: number }>({ queryKey: ['items'], path: '/api/items' })
      return data
    })
    await waitFor(() => expect(result.current).toEqual({ count: 1 }))
    const settled = renders

    // The request leaves: only isFetching changed.
    await act(async () => { void invalidateQueries(['items']) })
    expect(renders).toBe(settled)

    await act(async () => { answer.resolve({ count: 2 }); await answer.promise })
    expect(result.current).toEqual({ count: 2 })
    expect(renders).toBe(settled + 1)
  })

  it('re-renders a reader that reads isFetching when a request leaves and lands', async () => {
    const answer = deferred<unknown>()
    let first = true
    stubApi({ 'GET /api/items': () => { if (first) { first = false; return { count: 1 } } return answer.promise } })
    const { result } = renderHook(() => useApiQuery<{ count: number }>({ queryKey: ['items'], path: '/api/items' }))
    await waitFor(() => expect(result.current.data).toEqual({ count: 1 }))
    expect(result.current.isFetching).toBe(false)

    await act(async () => { void invalidateQueries(['items']) })
    expect(result.current.isFetching).toBe(true)

    await act(async () => { answer.resolve({ count: 1 }); await answer.promise })
    expect(result.current.isFetching).toBe(false)
  })
})

describe('useApiQuery with structuralSharing', () => {
  beforeEach(() => {
    resetQueryStore()
  })

  it('keeps the data object, and does not re-render, when a poll answers with equal data', async () => {
    stubApi({ 'GET /api/items': () => ({ list: [{ id: 'a', n: 1 }] }) })
    let renders = 0
    const { result } = renderHook(() => {
      renders += 1
      return useApiQuery<{ list: Array<{ id: string; n: number }> }>({ queryKey: ['items'], path: '/api/items', structuralSharing: true }).data
    })
    await waitFor(() => expect(result.current).toEqual({ list: [{ id: 'a', n: 1 }] }))
    const held = result.current
    const settled = renders

    await act(async () => { await invalidateQueries(['items']) })
    await act(async () => { await invalidateQueries(['items']) })

    expect(result.current).toBe(held)
    expect(renders).toBe(settled)
  })

  it('hands over the new data when it changed', async () => {
    let n = 1
    stubApi({ 'GET /api/items': () => ({ n }) })
    const { result } = renderHook(() => useApiQuery<{ n: number }>({ queryKey: ['items'], path: '/api/items', structuralSharing: true }).data)
    await waitFor(() => expect(result.current).toEqual({ n: 1 }))

    n = 2
    await act(async () => { await invalidateQueries(['items']) })

    expect(result.current).toEqual({ n: 2 })
  })

  it('still reports an error after equal data, and drops nothing it showed', async () => {
    let fail = false
    stubApi({ 'GET /api/items': () => (fail ? jsonResponse({ error: 'down' }, 503) : { n: 1 }) })
    const { result } = renderHook(() => useApiQuery<{ n: number }>({ queryKey: ['items'], path: '/api/items', structuralSharing: true }))
    await waitFor(() => expect(result.current.data).toEqual({ n: 1 }))

    fail = true
    await act(async () => { await invalidateQueries(['items']) })

    expect(result.current.error?.message).toBe('down')
  })
})

describe('useApiQuerySelector and the previous selection', () => {
  beforeEach(() => {
    resetQueryStore()
  })

  it('hands select the previous selection, so a reader can keep the parts that did not change', async () => {
    stubApi({ 'GET /api/items': { list: [{ id: 'a', n: 1 }, { id: 'b', n: 1 }] } })
    type Item = { id: string; n: number }
    const select = (data: { list: Item[] } | undefined, _error: Error | null, previous: Item[] | undefined): Item[] => {
      const next = data?.list ?? []
      return next.map((item) => previous?.find((p) => p.id === item.id && p.n === item.n) ?? item)
    }
    const { result } = renderHook(() => useApiQuerySelector({ queryKey: ['items'], path: '/api/items' }, select))
    await waitFor(() => expect(result.current).toHaveLength(2))
    const [a] = result.current

    await act(async () => { setQueryData(['items'], { list: [{ id: 'a', n: 1 }, { id: 'b', n: 2 }] }) })

    expect(result.current[0]).toBe(a)
    expect(result.current[1]).toEqual({ id: 'b', n: 2 })
  })
})
