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
})
