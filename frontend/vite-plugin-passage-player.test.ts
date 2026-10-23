import path from 'node:path'

import { beforeEach, describe, expect, it, vi } from 'vitest'

const build = vi.hoisted(() => vi.fn())
vi.mock('vite', () => ({ build }))

import { passagePlayer, playerSources } from './vite-plugin-passage-player'

const root = __dirname

describe('the files the player is built from', () => {
  it('are found by reading the imports, so they are known without a build', () => {
    const rel = playerSources(root).map((f: string) => path.relative(root, f))
    for (const f of ['src/export-player/main.ts', 'src/export-player/plan.ts', 'src/export-player/png.ts', 'src/lib/replay-clock.ts', 'src/lib/replay-camera.ts', 'src/lib/boat-layer.ts', 'src/lib/terrain-sea.ts']) {
      expect(rel).toContain(f)
    }
    expect(rel.some((f: string) => /react|components\//.test(f))).toBe(false)
  })
})

describe('the virtual module', () => {
  const chunk = { output: [{ type: 'chunk', code: 'var HcPlayer=1', moduleIds: ['/x/src/export-player/main.ts'] }] }
  const run = (plugin: ReturnType<typeof passagePlayer>) => {
    const watched: string[] = []
    const ctx = { addWatchFile: (f: string) => watched.push(f) }
    const load = plugin.load as unknown as (this: typeof ctx, id: string) => Promise<string | null>
    const resolved = (plugin.resolveId as unknown as (id: string) => string)('virtual:passage-player')
    return { watched, result: load.call(ctx, resolved) }
  }
  beforeEach(() => build.mockReset())

  it('builds as production, whatever ran it', async () => {
    build.mockResolvedValue(chunk)
    await run(passagePlayer(root)).result
    const config = build.mock.calls[0][0] as { mode: string; define: Record<string, string> }
    expect(config.mode).toBe('production')
    expect(config.define).toMatchObject({
      'import.meta.env.DEV': 'false', 'import.meta.env.PROD': 'true',
      'import.meta.env.MODE': '"production"', 'process.env.NODE_ENV': '"production"',
    })
  })

  it('does not keep a failed build, and tries again on the next load', async () => {
    build.mockRejectedValueOnce(new Error('syntax error')).mockResolvedValueOnce(chunk)
    const plugin = passagePlayer(root)
    const first = run(plugin)
    await expect(first.result).rejects.toThrow('syntax error')
    // Still watching the player's files, so the fix is seen.
    expect(first.watched.some((f) => f.endsWith('src/export-player/main.ts'))).toBe(true)
    const second = await run(plugin).result
    expect(second).toContain('var HcPlayer=1')
    expect(build).toHaveBeenCalledTimes(2)
  })

  it('hands the virtual module back with a changed player file, so importers update', () => {
    const plugin = passagePlayer(root)
    const mod = { id: '\0virtual:passage-player' }
    const invalidate = vi.fn()
    const ctx = { environment: { moduleGraph: { getModuleById: (id: string) => (id === mod.id ? mod : undefined), invalidateModule: invalidate } } }
    const hot = plugin.hotUpdate as unknown as (this: typeof ctx, o: { file: string; modules: unknown[] }) => unknown[] | undefined
    const file = path.join(root, 'src/export-player/plan.ts')
    expect(hot.call(ctx, { file, modules: [] })).toEqual([mod])
    expect(invalidate).toHaveBeenCalledWith(mod)
    expect(hot.call(ctx, { file: path.join(root, 'src/other.ts'), modules: [] })).toBeUndefined()
  })
})
