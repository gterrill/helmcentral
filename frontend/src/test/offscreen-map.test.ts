import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { openOffscreenMap, type OffscreenMapOptions } from '@/lib/offscreen-map'

type Handler = (e?: unknown) => void
const fake = vi.hoisted(() => ({ made: [] as { options: Record<string, unknown>; handlers: Map<string, Set<Handler>>; removed: boolean; styles: unknown[]; repaints: number }[] }))

vi.mock('maplibre-gl', () => {
  class Map {
    handlers = new globalThis.Map<string, Set<Handler>>()
    removed = false
    styles: unknown[] = []
    repaints = 0
    constructor(public options: Record<string, unknown>) { fake.made.push(this) }
    on(t: string, f: Handler) { (this.handlers.get(t) ?? this.handlers.set(t, new Set()).get(t)!).add(f) }
    off(t: string, f: Handler) { this.handlers.get(t)?.delete(f) }
    setStyle(style: unknown, options: unknown) { this.styles.push([style, options]) }
    triggerRepaint() { this.repaints++ }
    remove() { this.removed = true }
  }
  return { default: { Map } }
})

const emit = (type: string, e?: unknown) => fake.made[0].handlers.get(type)?.forEach((f) => f(e))

const options = (extra: Partial<OffscreenMapOptions> = {}): OffscreenMapOptions => ({
  width: 200, height: 100, pixelRatio: 2, hostStyle: { position: 'fixed' }, map: { style: '/style' },
  loadTimeoutMs: 1000, loadTimeoutError: () => new Error('load timed out'), disposedError: () => new Error('taken down'),
  onError: (e) => (e.error?.status === 404 ? null : new Error(`broke: ${e.error?.message}`)),
  ...extra,
})

beforeEach(() => { fake.made.length = 0; vi.useFakeTimers() })
afterEach(() => { vi.useRealTimers(); document.body.innerHTML = '' })

describe('openOffscreenMap', () => {
  it('makes the map in a laid-out element of its size, kept drawable, and hands it back once the style loads', async () => {
    const opening = openOffscreenMap(options())
    emit('load')
    const o = await opening
    const made = fake.made[0]
    expect(made.options).toMatchObject({ pixelRatio: 2, interactive: false, fadeDuration: 0, style: '/style', canvasContextAttributes: { preserveDrawingBuffer: true } })
    const host = made.options.container as HTMLElement
    expect(host.style.width).toBe('200px')
    expect(host.style.height).toBe('100px')
    expect(document.body.contains(host)).toBe(true)
    o.dispose()
    expect(made.removed).toBe(true)
    expect(document.body.contains(host)).toBe(false)
  })

  it('sets a style that is to be changed on its way in with setStyle, not in the constructor', async () => {
    const transformStyle = vi.fn()
    const opening = openOffscreenMap(options({ transformStyle }))
    expect(fake.made[0].options.style).toBeUndefined()
    expect(fake.made[0].styles).toEqual([['/style', { transformStyle }]])
    emit('load')
    await opening
  })

  it('fails, and takes the map down, when the style does not load in time', async () => {
    const opening = openOffscreenMap(options()).catch((e: unknown) => e)
    await vi.advanceTimersByTimeAsync(1001)
    expect(((await opening) as Error).message).toBe('load timed out')
    expect(fake.made[0].removed).toBe(true)
  })

  it('fails on a map error that the caller counts, and passes over one it does not', async () => {
    const opening = openOffscreenMap(options())
    emit('error', { error: { status: 404 } })
    emit('load')
    await opening
    const second = openOffscreenMap(options()).catch((e: unknown) => e)
    fake.made[1].handlers.get('error')!.forEach((f) => f({ error: { message: 'tile 500' } }))
    expect(((await second) as Error).message).toBe('broke: tile 500')
    expect(fake.made[1].removed).toBe(true)
  })

  it('settles on idle after asking for a repaint, and gives up after the time it is given', async () => {
    const opening = openOffscreenMap(options())
    emit('load')
    const o = await opening
    const done = o.settle(500, () => new Error('slow frame'))
    expect(fake.made[0].repaints).toBe(1)
    emit('idle')
    await done
    const slow = o.settle(500, () => new Error('slow frame')).catch((e: unknown) => e)
    await vi.advanceTimersByTimeAsync(501)
    expect(((await slow) as Error).message).toBe('slow frame')
  })

  it('ends a wait that is still going when the map is taken down: no timer, no listener, rejected as cancelled', async () => {
    const opening = openOffscreenMap(options())
    emit('load')
    const o = await opening
    const waiting = o.settle(500, () => new Error('slow frame')).catch((e: unknown) => e)
    expect(fake.made[0].handlers.get('idle')!.size).toBe(1)
    o.dispose()
    expect(((await waiting) as Error).message).toBe('taken down')
    expect(fake.made[0].handlers.get('idle')!.size).toBe(0)
    // The timer was cleared: nothing fires later.
    expect(vi.getTimerCount()).toBe(0)
    // And a wait asked for after that is cancelled too.
    expect(((await o.settle(500, () => new Error('x')).catch((e: unknown) => e)) as Error).message).toBe('taken down')
  })

  it('stops a frame that is being waited on when the map reports a fault', async () => {
    const opening = openOffscreenMap(options())
    emit('load')
    const o = await opening
    const waiting = o.settle(500, () => new Error('slow frame')).catch((e: unknown) => e)
    emit('error', { error: { message: 'terrain' } })
    expect(((await waiting) as Error).message).toBe('broke: terrain')
    // And it stays failed.
    expect(((await o.settle(500, () => new Error('x')).catch((e: unknown) => e)) as Error).message).toBe('broke: terrain')
  })
})
