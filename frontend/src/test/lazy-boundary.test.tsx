import { Suspense, lazy, type ComponentType } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'

import { LazyBoundary, isChunkLoadError } from '@/components/lazy-boundary'

const rejecting = (message: string) => lazy<ComponentType>(() => Promise.reject(new TypeError(message)))

describe('LazyBoundary', () => {
  beforeEach(() => { vi.spyOn(console, 'error').mockImplementation(() => {}) })
  afterEach(() => vi.restoreAllMocks())

  it('recognises the ways a browser words a chunk that would not load', () => {
    for (const m of [
      'Failed to fetch dynamically imported module: https://x/assets/a.js',
      'error loading dynamically imported module',
      'Importing a module script failed.',
      'Unable to preload CSS for /assets/a.css',
    ]) expect(isChunkLoadError(new TypeError(m))).toBe(true)
    expect(isChunkLoadError(new Error('x is undefined'))).toBe(false)
  })

  it('shows a small message in place of the screen, keeps its siblings, and reports the failure', async () => {
    const Broken = rejecting('Failed to fetch dynamically imported module: /assets/a.js')
    const onChunkError = vi.fn()
    render(
      <div>
        <p>shell</p>
        <LazyBoundary onChunkError={onChunkError}>
          <Suspense fallback={null}><Broken /></Suspense>
        </LazyBoundary>
      </div>,
    )
    expect(await screen.findByRole('alert')).toHaveTextContent("This screen couldn't be loaded.")
    expect(screen.getByText('shell')).toBeInTheDocument()
    expect(onChunkError).toHaveBeenCalledTimes(1)
  })

  it('tries the children again when the reset key changes', async () => {
    const Broken = rejecting('Failed to fetch dynamically imported module')
    const { rerender } = render(
      <LazyBoundary resetKey="a" onChunkError={vi.fn()}>
        <Suspense fallback={null}><Broken /></Suspense>
      </LazyBoundary>,
    )
    await screen.findByRole('alert')
    rerender(<LazyBoundary resetKey="b" onChunkError={vi.fn()}><p>other screen</p></LazyBoundary>)
    expect(screen.getByText('other screen')).toBeInTheDocument()
  })
})
