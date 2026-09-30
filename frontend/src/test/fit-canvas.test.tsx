import { act, render } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { FitCanvas } from '@/components/fit-canvas'

let observed: ((entries: unknown[]) => void) | null = null

beforeEach(() => {
  vi.stubGlobal('ResizeObserver', class {
    constructor(cb: (entries: unknown[]) => void) { observed = cb }
    observe() {}
    disconnect() {}
  })
})
afterEach(() => { vi.unstubAllGlobals(); observed = null })

function resize(w: number, h: number) {
  act(() => { observed?.([{ contentRect: { width: w, height: h } }]) })
}

describe('FitCanvas', () => {
  test('fills a tall, wide box up to the cap and centres the canvas', () => {
    const { container } = render(<FitCanvas designWidth={400} designHeight={200} canvasProps={{ 'data-testid': 'c' } as never}><span /></FitCanvas>)
    resize(600, 300)
    const canvas = container.querySelector('[data-testid="c"]') as HTMLElement
    expect(canvas.style.transform).toBe('translate(0px, 0px) scale(1.5)')
    resize(1600, 1000)
    expect(canvas.style.transform).toBe('translate(400px, 300px) scale(2)')
  })

  test('is limited by height when the box is short', () => {
    const { container } = render(<FitCanvas designWidth={400} designHeight={200} canvasProps={{ 'data-testid': 'c' } as never}><span /></FitCanvas>)
    resize(800, 100)
    const canvas = container.querySelector('[data-testid="c"]') as HTMLElement
    expect(canvas.style.transform).toBe('translate(300px, 0px) scale(0.5)')
  })

  test('the box keeps a width-only minimum height so a height-less layout is unchanged', () => {
    const { container } = render(<FitCanvas designWidth={400} designHeight={200}><span /></FitCanvas>)
    resize(200, 100)
    expect((container.firstChild as HTMLElement).style.minHeight).toBe('100px')
    resize(900, 100)
    expect((container.firstChild as HTMLElement).style.minHeight).toBe('200px')
  })
})
