import { act, renderHook } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { useTileSave } from '@/hooks/use-tile-save'

const deferred = () => {
  let resolve!: () => void
  let reject!: (err: unknown) => void
  const promise = new Promise<void>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

describe('useTileSave', () => {
  test('is busy until the task settles, then free with no error', async () => {
    const { result } = renderHook(() => useTileSave())
    const write = deferred()
    let run!: Promise<void>
    act(() => { run = result.current.run(() => write.promise) })
    expect(result.current.saving).toBe(true)
    await act(async () => { write.resolve(); await run })
    expect(result.current.saving).toBe(false)
    expect(result.current.error).toBeNull()
  })

  test('starts the task in the same tick', () => {
    const { result } = renderHook(() => useTileSave())
    let called = 0
    act(() => { void result.current.run(() => { called++ }) })
    expect(called).toBe(1)
  })

  test('shows a rejection and frees the button', async () => {
    const { result } = renderHook(() => useTileSave())
    await act(async () => { await result.current.run(() => Promise.reject(new Error('refused'))) })
    expect(result.current.saving).toBe(false)
    expect(result.current.error).toBe('refused')
  })

  test('shows a synchronous throw', async () => {
    const { result } = renderHook(() => useTileSave())
    await act(async () => { await result.current.run(() => { throw new Error('wired wrong') }) })
    expect(result.current.error).toBe('wired wrong')
    expect(result.current.saving).toBe(false)
  })

  test('names a non-Error rejection with the fallback', async () => {
    const { result } = renderHook(() => useTileSave())
    await act(async () => { await result.current.run(() => Promise.reject('nope')) })
    expect(result.current.error).toBe('Could not save the tile')
  })

  test('a second run while busy is ignored', async () => {
    const { result } = renderHook(() => useTileSave())
    const write = deferred()
    let calls = 0
    let first!: Promise<void>
    act(() => { first = result.current.run(() => { calls++; return write.promise }) })
    await act(async () => { await result.current.run(() => { calls++ }) })
    expect(calls).toBe(1)
    await act(async () => { write.resolve(); await first })
  })

  test('a retry clears the last failure, and reset clears it too', async () => {
    const { result } = renderHook(() => useTileSave())
    await act(async () => { await result.current.run(() => Promise.reject(new Error('refused'))) })
    const write = deferred()
    let retry!: Promise<void>
    act(() => { retry = result.current.run(() => write.promise) })
    expect(result.current.error).toBeNull()
    await act(async () => { write.reject(new Error('again')); await retry })
    expect(result.current.error).toBe('again')
    act(() => result.current.reset())
    expect(result.current.error).toBeNull()
  })
})
