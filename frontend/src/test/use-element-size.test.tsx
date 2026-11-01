import { act, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useElementSize } from '@/hooks/use-element-size'

type Callback = (entries: { contentRect: { width: number; height: number } }[]) => void
const observers: { callback: Callback; observe: ReturnType<typeof vi.fn>; disconnect: ReturnType<typeof vi.fn> }[] = []
const original = globalThis.ResizeObserver

beforeEach(() => {
  observers.length = 0
  globalThis.ResizeObserver = class {
    observe = vi.fn()
    disconnect = vi.fn()
    unobserve = vi.fn()
    constructor(public callback: Callback) { observers.push(this) }
  } as unknown as typeof ResizeObserver
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({ width: 640, height: 400 } as DOMRect)
})
afterEach(() => {
  globalThis.ResizeObserver = original
  vi.restoreAllMocks()
})

function Probe({ shown = true }: { shown?: boolean }) {
  const [ref, size] = useElementSize<HTMLDivElement>()
  return (
    <>
      {shown && <div ref={ref} data-testid="box" />}
      <span data-testid="size">{size ? `${size.w}x${size.h}` : 'none'}</span>
    </>
  )
}

describe('useElementSize', () => {
  it('measures the element once it is there, though the ref was not attached on the first render', () => {
    render(<Probe />)
    expect(screen.getByTestId('size')).toHaveTextContent('640x400')
  })

  it('follows a resize', () => {
    render(<Probe />)
    act(() => observers[0].callback([{ contentRect: { width: 300, height: 500 } }]))
    expect(screen.getByTestId('size')).toHaveTextContent('300x500')
    act(() => observers[0].callback([{ contentRect: { width: 1000, height: 500 } }]))
    expect(screen.getByTestId('size')).toHaveTextContent('1000x500')
  })

  it('has no size while there is no element, and starts again when one comes', () => {
    function Toggle() {
      const [shown, setShown] = useState(false)
      return (
        <>
          <button onClick={() => setShown(true)}>show</button>
          <Probe shown={shown} />
        </>
      )
    }
    render(<Toggle />)
    expect(screen.getByTestId('size')).toHaveTextContent('none')
    act(() => screen.getByText('show').click())
    expect(screen.getByTestId('size')).toHaveTextContent('640x400')
  })

  it('stops watching when the element goes', () => {
    const { unmount } = render(<Probe />)
    unmount()
    expect(observers[0].disconnect).toHaveBeenCalled()
  })
})
