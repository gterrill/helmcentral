import { describe, it, expect, vi, afterEach } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'

import { useMateTelemetryWatch } from '@/hooks/use-mate-telemetry-watch'

// ADR 0160: the chat's view of a watch Mate started. GET .../watch answers
// 200 with the watch or 204 with none; DELETE is the chip's Stop.

const watchBody = {
  watch: {
    id: 'w1',
    conversation_id: 'c1',
    subject: 'Port engine load and Starboard engine load',
    labels: ['Port engine load', 'Starboard engine load'],
    minutes: 5,
    started_at: '2026-10-04T04:00:00Z',
    ends_at: '2026-10-04T04:05:00Z',
    status: 'watching',
  },
}

function jsonResponse(status: number, body?: unknown) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  }
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('useMateTelemetryWatch', () => {
  it('reads the running watch for the conversation', async () => {
    const fetchMock = vi.fn(async () => jsonResponse(200, watchBody))
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useMateTelemetryWatch('c1'))

    await waitFor(() => expect(result.current.watch).not.toBeNull())
    expect(result.current.watch).toMatchObject({
      id: 'w1',
      subject: 'Port engine load and Starboard engine load',
      endsAt: '2026-10-04T04:05:00Z',
      status: 'watching',
    })
    expect(fetchMock).toHaveBeenCalledWith(expect.stringMatching(/\/api\/assistant\/conversations\/c1\/watch$/), expect.anything())
  })

  it('shows nothing when no watch is running, and does not fetch without a conversation', async () => {
    const fetchMock = vi.fn(async () => jsonResponse(204))
    vi.stubGlobal('fetch', fetchMock)

    const { result, rerender } = renderHook(({ id }: { id: string | null }) => useMateTelemetryWatch(id), {
      initialProps: { id: null as string | null },
    })
    expect(fetchMock).not.toHaveBeenCalled()

    rerender({ id: 'c1' })
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    expect(result.current.watch).toBeNull()
    expect(result.current.ended).toBe(0)
  })

  it('counts a watch that disappears on its own as ended, so the thread can collect the report', async () => {
    let running = true
    vi.stubGlobal('fetch', vi.fn(async () => (running ? jsonResponse(200, watchBody) : jsonResponse(204))))

    const { result } = renderHook(() => useMateTelemetryWatch('c1', { pollMs: 20 }))
    await waitFor(() => expect(result.current.watch).not.toBeNull())

    running = false
    await waitFor(() => expect(result.current.watch).toBeNull())
    expect(result.current.ended).toBe(1)
  })

  it('Stop deletes the watch and does not count it as ended', async () => {
    const fetchMock = vi.fn(async (_url: string, init?: RequestInit) =>
      init?.method === 'DELETE' ? jsonResponse(204) : jsonResponse(200, watchBody),
    )
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useMateTelemetryWatch('c1'))
    await waitFor(() => expect(result.current.watch).not.toBeNull())

    await act(async () => {
      await result.current.stop()
    })

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringMatching(/\/api\/assistant\/conversations\/c1\/watch$/),
      expect.objectContaining({ method: 'DELETE' }),
    )
    expect(result.current.watch).toBeNull()
    expect(result.current.ended).toBe(0)
  })

  it('surfaces a failed Stop in the server\'s words and keeps the chip', async () => {
    vi.stubGlobal('fetch', vi.fn(async (_url: string, init?: RequestInit) =>
      init?.method === 'DELETE' ? jsonResponse(403, { error: 'this action requires readwrite access' }) : jsonResponse(200, watchBody),
    ))

    const { result } = renderHook(() => useMateTelemetryWatch('c1'))
    await waitFor(() => expect(result.current.watch).not.toBeNull())

    await act(async () => {
      await result.current.stop()
    })
    expect(result.current.error).toBe('this action requires readwrite access')
    expect(result.current.watch).not.toBeNull()
  })

  it('a Stop that lands after the watch ended keeps following it to Mate\'s answer', async () => {
    let phase: 'watching' | 'reporting' | 'gone' = 'watching'
    vi.stubGlobal('fetch', vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'DELETE') {
        phase = 'reporting'
        return jsonResponse(409, { error: 'the watch has already finished; Mate is reading its report' })
      }
      if (phase === 'gone') return jsonResponse(204)
      return jsonResponse(200, { watch: { ...watchBody.watch, status: phase } })
    }))

    const { result } = renderHook(() => useMateTelemetryWatch('c1', { pollMs: 20 }))
    await waitFor(() => expect(result.current.watch).not.toBeNull())

    await act(async () => {
      await result.current.stop()
    })
    // Not an error to the operator: the watch simply finished first.
    expect(result.current.error).toBeNull()
    await waitFor(() => expect(result.current.watch?.status).toBe('reporting'))

    phase = 'gone'
    await waitFor(() => expect(result.current.ended).toBe(1))
  })

  it('switching conversation drops the old watch without counting it as ended', async () => {
    vi.stubGlobal('fetch', vi.fn(async (url: string) =>
      url.includes('/c1/') ? jsonResponse(200, watchBody) : jsonResponse(204),
    ))

    const { result, rerender } = renderHook(({ id }: { id: string }) => useMateTelemetryWatch(id), {
      initialProps: { id: 'c1' },
    })
    await waitFor(() => expect(result.current.watch).not.toBeNull())

    rerender({ id: 'c2' })
    await waitFor(() => expect(result.current.watch).toBeNull())
    expect(result.current.ended).toBe(0)
  })
})
