import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// hasWebGL2 memoises its result in module-level state, so each case needs a
// fresh module instance (vi.resetModules + a dynamic import) to see the
// canvas stub it installs rather than a previous test's cached answer.
describe('hasWebGL2', () => {
  const originalGetContext = HTMLCanvasElement.prototype.getContext

  beforeEach(() => {
    vi.resetModules()
  })

  afterEach(() => {
    HTMLCanvasElement.prototype.getContext = originalGetContext
  })

  it('returns true when the canvas hands back a webgl2 context', async () => {
    HTMLCanvasElement.prototype.getContext = vi.fn(() => ({})) as unknown as typeof HTMLCanvasElement.prototype.getContext

    const { hasWebGL2 } = await import('@/lib/webgl')

    expect(hasWebGL2()).toBe(true)
  })

  it('returns false when getContext returns null', async () => {
    HTMLCanvasElement.prototype.getContext = vi.fn(() => null) as unknown as typeof HTMLCanvasElement.prototype.getContext

    const { hasWebGL2 } = await import('@/lib/webgl')

    expect(hasWebGL2()).toBe(false)
  })

  it('returns false when getContext throws (some WebKit builds throw instead of returning null)', async () => {
    HTMLCanvasElement.prototype.getContext = vi.fn(() => {
      throw new Error('WebGL2 not supported')
    }) as unknown as typeof HTMLCanvasElement.prototype.getContext

    const { hasWebGL2 } = await import('@/lib/webgl')

    expect(hasWebGL2()).toBe(false)
  })

  it('memoises the result: a second call does not probe the canvas again', async () => {
    const getContextMock = vi.fn(() => ({}))
    HTMLCanvasElement.prototype.getContext = getContextMock as unknown as typeof HTMLCanvasElement.prototype.getContext

    const { hasWebGL2 } = await import('@/lib/webgl')

    expect(hasWebGL2()).toBe(true)
    expect(hasWebGL2()).toBe(true)
    expect(getContextMock).toHaveBeenCalledTimes(1)
  })

  // The probe context was never released: hasWebGL2() created a WebGL2
  // context on a throwaway canvas and just dropped the reference, leaving
  // the browser to hold onto that context (and whatever GPU resources back
  // it) for as long as the page lives. WEBGL_lose_context's loseContext()
  // is the standard way to force an unused context to free its resources
  // immediately rather than waiting on GC.
  it('releases the probed context via WEBGL_lose_context after probing', async () => {
    const loseContext = vi.fn()
    const getExtension = vi.fn(() => ({ loseContext }))
    HTMLCanvasElement.prototype.getContext = vi.fn(() => ({ getExtension })) as unknown as typeof HTMLCanvasElement.prototype.getContext

    const { hasWebGL2 } = await import('@/lib/webgl')

    expect(hasWebGL2()).toBe(true)
    expect(getExtension).toHaveBeenCalledWith('WEBGL_lose_context')
    expect(loseContext).toHaveBeenCalledTimes(1)
  })

  it('does not throw when the probed context has no getExtension (e.g. the other tests\' plain-object stub)', async () => {
    HTMLCanvasElement.prototype.getContext = vi.fn(() => ({})) as unknown as typeof HTMLCanvasElement.prototype.getContext

    const { hasWebGL2 } = await import('@/lib/webgl')

    expect(() => hasWebGL2()).not.toThrow()
    expect(hasWebGL2()).toBe(true)
  })
})
