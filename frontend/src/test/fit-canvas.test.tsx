import { act, render } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { FitCanvas } from '@/components/fit-canvas'

const DESIGN = { key: 'one', width: 400, height: 200 }
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
    const { container } = render(<FitCanvas designs={[DESIGN]} canvasProps={{ 'data-testid': 'c' } as never}><span /></FitCanvas>)
    resize(600, 300)
    const canvas = container.querySelector('[data-testid="c"]') as HTMLElement
    expect(canvas.style.transform).toBe('translate(0px, 0px) scale(1.5)')
    resize(1600, 1000)
    expect(canvas.style.transform).toBe('translate(400px, 300px) scale(2)')
  })

  test('is limited by height when the box is short', () => {
    const { container } = render(<FitCanvas designs={[DESIGN]} canvasProps={{ 'data-testid': 'c' } as never}><span /></FitCanvas>)
    resize(800, 100)
    const canvas = container.querySelector('[data-testid="c"]') as HTMLElement
    expect(canvas.style.transform).toBe('translate(300px, 0px) scale(0.5)')
  })

  test('the box asks for a width-only height but never forces one', () => {
    const { container } = render(<FitCanvas designs={[DESIGN]}><span /></FitCanvas>)
    const box = container.firstChild as HTMLElement
    resize(200, 100)
    expect(box.style.flexBasis).toBe('100px')
    resize(900, 100)
    expect(box.style.flexBasis).toBe('200px')
    // A min-height would override the height a sized tile gives the box, and
    // the canvas would overflow it.
    expect(box.style.minHeight).toBe('')
  })

  test('picks the design that fits better and tells the children which', () => {
    const designs = [
      { key: 'tall', width: 800, height: 540, stretch: true },
      { key: 'wide', width: 1000, height: 420, stretch: true },
    ]
    const { container } = render(
      <FitCanvas designs={designs} canvasProps={{ 'data-testid': 'c' } as never}>
        {(d) => <span data-testid="kid">{`${d.key} ${Math.round(d.width)}`}</span>}
      </FitCanvas>,
    )
    const canvas = container.querySelector('[data-testid="c"]') as HTMLElement
    resize(500, 400)
    expect(canvas.dataset.layout).toBe('tall')
    expect(canvas.style.width).toBe('800px')
    resize(1000, 210)
    expect(canvas.dataset.layout).toBe('wide')
    // Height sets the scale (0.5); the canvas is stretched to fill the width.
    expect(canvas.style.transform).toBe('translate(0px, 0px) scale(0.5)')
    expect(canvas.style.width).toBe('2000px')
    expect(container.querySelector('[data-testid="kid"]')?.textContent).toBe('wide 2000')
  })
})
