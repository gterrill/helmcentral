import { describe, expect, it, vi } from 'vitest'

describe('default fetch', () => {
  it('rejects naming the URL when a test has not stubbed it', async () => {
    await expect(fetch('/api/settings')).rejects.toThrow('unstubbed fetch: /api/settings')
  })

  it('gives way to vi.stubGlobal and comes back after unstubAllGlobals', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))
    await expect(fetch('/x')).resolves.toEqual({ ok: true })
    vi.unstubAllGlobals()
    await expect(fetch('/x')).rejects.toThrow('unstubbed fetch: /x')
  })
})
