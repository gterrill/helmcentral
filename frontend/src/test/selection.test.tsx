import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { useSelection } from '@/lib/selection'

function makeSource<S>(initial: S) {
  let snapshot = initial
  const listeners = new Set<() => void>()
  return {
    subscribe: (l: () => void) => { listeners.add(l); return () => { listeners.delete(l) } },
    getSnapshot: () => snapshot,
    set(next: S) { snapshot = next; for (const l of [...listeners]) l() },
  }
}

describe('useSelection', () => {
  it('keeps the previous reference while isEqual calls the new selection equal', () => {
    const source = makeSource({ a: [1], b: 0 })
    let renders = 0
    const same = (x: number[], y: number[]) => x.length === y.length && x.every((v, i) => v === y[i])
    const { result } = renderHook(() => {
      renders += 1
      return useSelection({ ...source, select: (s: { a: number[] }) => [...s.a], isEqual: same })
    })
    const held = result.current
    const settled = renders
    act(() => { source.set({ a: [1], b: 1 }) })
    expect(result.current).toBe(held)
    expect(renders).toBe(settled)
    act(() => { source.set({ a: [2], b: 1 }) })
    expect(result.current).toEqual([2])
  })

  it('a fresh select closure on every render causes no extra render', () => {
    const source = makeSource({ n: 1 })
    let renders = 0
    const { rerender } = renderHook(() => {
      renders += 1
      return useSelection({ ...source, select: (s: { n: number }) => ({ doubled: s.n * 2 }), isEqual: (a, b) => a.doubled === b.doubled })
    })
    const first = renders
    rerender()
    expect(renders).toBe(first + 1)
    act(() => { source.set({ n: 1 }) })
    expect(renders).toBe(first + 1)
  })

  it('recomputes when the select function changes, and hands select the previous value', () => {
    const source = makeSource({ n: 3 })
    const seen: Array<number | undefined> = []
    const { result, rerender } = renderHook(({ k }) =>
      useSelection({ ...source, select: (s: { n: number }, previous?: number) => { seen.push(previous); return s.n * k } }),
    { initialProps: { k: 1 } })
    expect(result.current).toBe(3)
    rerender({ k: 10 })
    expect(result.current).toBe(30)
    expect(seen).toContain(3)
  })

  it('treats snapshots as unchanged when sameSnapshot says so', () => {
    const source = makeSource({ data: 1, noise: 0 })
    let calls = 0
    renderHook(() => useSelection({
      ...source,
      select: (s: { data: number }) => { calls += 1; return s.data },
      sameSnapshot: (a, b) => a.data === b.data,
    }))
    const before = calls
    act(() => { source.set({ data: 1, noise: 1 }) })
    expect(calls).toBe(before)
  })
})
