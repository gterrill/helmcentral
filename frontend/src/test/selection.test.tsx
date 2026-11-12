import { act, renderHook } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'

import { createElement, type ReactNode } from 'react'

import { WallPageActiveContext } from '@/lib/wall-page-active'
import { useSelection } from '@/lib/selection'

function makeSource<S>(initial: S) {
  let snapshot = initial
  const listeners = new Set<() => void>()
  return {
    subscribe: (l: () => void) => { listeners.add(l); return () => { listeners.delete(l) } },
    getSnapshot: () => snapshot,
    set(next: S) { snapshot = next; for (const l of [...listeners]) l() },
    listenerCount: () => listeners.size,
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

  describe('when the snapshot source changes', () => {
    type Snap = { kind: string; from: string }
    const sameKind = (x: Snap, y: Snap) => x.kind === y.kind
    const sourceA = () => makeSource<Snap>({ kind: 'empty', from: 'a' })
    const sourceB = () => makeSource<Snap>({ kind: 'empty', from: 'b' })

    it('returns the new source\'s object although its snapshot compares equal', () => {
      const a = sourceA()
      const b = sourceB()
      const { result, rerender } = renderHook(
        ({ source }: { source: typeof a }) => useSelection({ ...source, sameSnapshot: sameKind }),
        { initialProps: { source: a } },
      )
      expect(result.current.from).toBe('a')
      rerender({ source: b })
      expect(result.current.from).toBe('b')
    })

    it('holds on a resting page after the change, not only before it', () => {
      const a = sourceA()
      const b = sourceB()
      let renders = 0
      const { result, rerender } = renderHook(
        ({ source }: { source: typeof a }) => {
          renders += 1
          return useSelection({ ...source, sameSnapshot: sameKind })
        },
        {
          initialProps: { source: a },
          wrapper: ({ children }: { children: ReactNode }) => createElement(WallPageActiveContext.Provider, { value: false }, children),
        },
      )
      rerender({ source: b })
      expect(result.current.from).toBe('b')
      const settled = renders
      act(() => { b.set({ kind: 'moved', from: 'b' }) })
      expect(renders).toBe(settled)
      expect(result.current.from).toBe('b')
    })
  })

  it('types the result as the snapshot without select, and as the selection with it', () => {
    const source = makeSource({ n: 1 })
    const { result } = renderHook(() => ({
      whole: useSelection({ ...source }),
      slice: useSelection({ ...source, select: (s: { n: number }) => s.n }),
    }))
    expect(result.current.whole.n).toBe(1)
    expect(result.current.slice).toBe(1)
    renderHook(() => {
      // @ts-expect-error - without select the result is the snapshot, not a number
      const wrong: number = useSelection<{ n: number }, number>({ ...source })
      return wrong
    })
  })

  describe('on a resting wall page', () => {
    function renderOnPage(source: ReturnType<typeof makeSource<{ n: number }>>, whileResting?: 'live') {
      let renders = 0
      const view = renderHook(
        () => {
          renders += 1
          return useSelection({ ...source, select: (s: { n: number }) => s.n, whileResting })
        },
        // The page flag is the module variable the test sets before a rerender.
        { wrapper: ({ children }: { children: ReactNode }) => createElement(Flag, null, children) },
      )
      return { ...view, renders: () => renders }
    }

    let flag = true
    beforeEach(() => { flag = true })
    function Flag({ children }: { children: ReactNode }) {
      return createElement(WallPageActiveContext.Provider, { value: flag }, children)
    }

    it('does not re-render on store changes', () => {
      const source = makeSource({ n: 1 })
      flag = false
      const { renders, result } = renderOnPage(source)
      const settled = renders()
      act(() => { source.set({ n: 2 }) })
      act(() => { source.set({ n: 3 }) })
      expect(renders()).toBe(settled)
      expect(result.current).toBe(1)
    })

    it('re-renders on store changes when the reader asks to stay live', () => {
      const source = makeSource({ n: 1 })
      flag = false
      const { result } = renderOnPage(source, 'live')
      act(() => { source.set({ n: 4 }) })
      expect(result.current).toBe(4)
    })

    it('has a value when it first mounts on a resting page', () => {
      const source = makeSource({ n: 7 })
      flag = false
      const { result } = renderOnPage(source)
      expect(result.current).toBe(7)
    })

    it('shows the latest value once its page becomes active, and follows changes again', () => {
      const source = makeSource({ n: 1 })
      flag = false
      const { result, rerender } = renderOnPage(source)
      act(() => { source.set({ n: 5 }) })
      expect(result.current).toBe(1)
      flag = true
      rerender()
      expect(result.current).toBe(5)
      act(() => { source.set({ n: 6 }) })
      expect(result.current).toBe(6)
    })

    it('holds its value again when the page rests', () => {
      const source = makeSource({ n: 1 })
      flag = true
      const { result, rerender } = renderOnPage(source)
      flag = false
      rerender()
      act(() => { source.set({ n: 9 }) })
      expect(result.current).toBe(1)
      // The subscription stays: a poll or a stream lives as long as it has a reader.
      expect(source.listenerCount()).toBe(1)
    })

    it('reads the new key once when its select or snapshot changes on a resting page', () => {
      const a = makeSource({ n: 1 })
      const b = makeSource({ n: 100 })
      flag = false
      let which = a
      const selectN = (s: { n: number }) => s.n
      const selectDouble = (s: { n: number }) => s.n * 2
      const { result, rerender } = renderHook(
        ({ source, select }: { source: typeof a; select: (s: { n: number }) => number }) =>
          useSelection({ ...source, select }),
        {
          initialProps: { source: which, select: selectN },
          wrapper: ({ children }: { children: ReactNode }) => createElement(Flag, null, children),
        },
      )
      expect(result.current).toBe(1)
      rerender({ source: which, select: selectDouble })
      expect(result.current).toBe(2)
      which = b
      rerender({ source: which, select: selectDouble })
      expect(result.current).toBe(200)
    })
  })
})
