import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { toast } from 'sonner'
import { useAnchorWatchAutoClose } from '@/hooks/use-anchor-watch-auto-close'

vi.mock('sonner', () => ({ toast: { error: vi.fn() } }))

const initialProps = {
  engine0: 800 as number | null | undefined,
  engine1: null as number | null | undefined,
  distance: 30 as number | null,
  radius: 20,
  active: true,
  enabled: true,
}
function setup(overrides: Partial<typeof initialProps> = {}) {
  return renderHook((p) => useAnchorWatchAutoClose(
    p.engine0, p.engine1, p.distance, p.radius, p.active, p.enabled,
  ), { initialProps: { ...initialProps, ...overrides } })
}
const advance = async (ms: number) => {
  await act(async () => { await vi.advanceTimersByTimeAsync(ms) })
}

describe('useAnchorWatchAutoClose', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it.each([[800, null], [null, 800], [-1, 0.1], [800, NaN]])(
    'arms with one finite positive main engine RPM (%s, %s)', (engine0, engine1) => {
      expect(setup({ engine0, engine1 }).result.current.isAutoCloseArmed).toBe(true)
    },
  )
  it.each([0, -1, null, undefined, NaN, Infinity, -Infinity])(
    'does not interpret unavailable/stopped RPM %s as running', async (rpm) => {
      const { result } = setup({ engine0: rpm, engine1: rpm })
      await advance(6000)
      expect(result.current.isAutoCloseArmed).toBe(false)
      expect(fetch).not.toHaveBeenCalled()
    },
  )
  it.each([
    { enabled: false }, { active: false }, { distance: null }, { distance: NaN },
    { distance: Infinity }, { distance: 20 }, { distance: 24.572 },
    { radius: NaN }, { radius: Infinity }, { radius: -1 },
  ])('does not arm without valid enabled/outside-watch evidence: %j', async (props) => {
    const { result } = setup(props)
    await advance(6000)
    expect(result.current.isAutoCloseArmed).toBe(false)
    expect(fetch).not.toHaveBeenCalled()
  })
  it('requires five continuous seconds and sends exactly one success event/request despite telemetry updates', async () => {
    const eventSpy = vi.spyOn(window, 'dispatchEvent')
    const { result, rerender } = setup()
    await advance(3000)
    expect(result.current.motoringSecondsElapsed).toBe(3)
    rerender({ ...initialProps, distance: 31, engine0: 900 })
    await advance(1900)
    expect(fetch).not.toHaveBeenCalled()
    await advance(100)
    expect(fetch).toHaveBeenCalledExactlyOnceWith('/api/anchor-watch', { method: 'DELETE' })
    expect(eventSpy.mock.calls.filter(([event]) => event.type === 'anchor-watch-auto-closed')).toHaveLength(1)
    expect(result.current.isAutoCloseArmed).toBe(false)
    rerender({ ...initialProps, distance: 32 })
    await advance(10000)
    expect(fetch).toHaveBeenCalledTimes(1)
  })
  it.each([{ engine0: 0 }, { distance: 20 }, { distance: null }, { active: false }, { enabled: false }])(
    'resets the countdown when evidence is lost: %j', async (props) => {
      const { result, rerender } = setup()
      await advance(3000)
      rerender({ ...initialProps, ...props })
      expect(result.current.isAutoCloseArmed).toBe(false)
      expect(result.current.motoringSecondsElapsed).toBe(0)
      await advance(6000)
      expect(fetch).not.toHaveBeenCalled()
      rerender(initialProps)
      await advance(4900)
      expect(fetch).not.toHaveBeenCalled()
      await advance(100)
      expect(fetch).toHaveBeenCalledTimes(1)
    },
  )
  it.each(['http', 'network'])('surfaces %s failure without success or repeated requests; rearms only after conditions reset', async (failure) => {
    if (failure === 'http') {
      vi.mocked(fetch).mockResolvedValue({ ok: false, status: 502, json: async () => ({ error: 'SignalK publish failed' }) } as Response)
    } else {
      vi.mocked(fetch).mockRejectedValue(new Error('SignalK publish failed'))
    }
    const eventSpy = vi.spyOn(window, 'dispatchEvent')
    const { result, rerender } = setup()
    await advance(15000)
    rerender({ ...initialProps, distance: 35 })
    await advance(6000)
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(toast.error).toHaveBeenCalledExactlyOnceWith('Could not automatically raise anchor', { description: 'SignalK publish failed' })
    expect(eventSpy.mock.calls.filter(([event]) => event.type === 'anchor-watch-auto-closed')).toHaveLength(0)
    expect(result.current.isAutoCloseArmed).toBe(false)
    rerender({ ...initialProps, engine0: 0 })
    rerender(initialProps)
    await advance(5000)
    expect(fetch).toHaveBeenCalledTimes(2)
  })
  it('does not overlap pending requests when conditions toggle', async () => {
    let resolve!: (response: Response) => void
    vi.mocked(fetch).mockReturnValue(new Promise((done) => { resolve = done }))
    const { rerender } = setup()
    await advance(5000)
    rerender({ ...initialProps, enabled: false })
    rerender(initialProps)
    await advance(6000)
    expect(fetch).toHaveBeenCalledTimes(1)
    await act(async () => { resolve({ ok: true } as Response) })
    await advance(6000)
    expect(fetch).toHaveBeenCalledTimes(1)
  })
  it('cleans up its countdown on unmount', async () => {
    const { unmount } = setup()
    await advance(3000)
    unmount()
    await advance(10000)
    expect(fetch).not.toHaveBeenCalled()
  })
})
