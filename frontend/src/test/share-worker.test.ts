import { afterEach, describe, expect, it, vi } from 'vitest'

import { registerShareWorker } from '@/lib/share-worker'

// Sharing to Helmcentral needs the service worker running before the first
// share. Only the installed app can be a share target, so that is when it is
// registered; a kiosk or a browser tab is left as it was (ADR 0045).
function env({ secure = true, standalone = true, hasWorker = true } = {}) {
  const register = vi.fn().mockResolvedValue(undefined)
  vi.stubGlobal('isSecureContext', secure)
  vi.stubGlobal('matchMedia', (q: string) => ({ matches: standalone && q === '(display-mode: standalone)' }))
  if (hasWorker) vi.stubGlobal('navigator', { serviceWorker: { register } })
  else vi.stubGlobal('navigator', {})
  return register
}

describe('registerShareWorker', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('registers the worker for the installed app', async () => {
    const register = env()
    await registerShareWorker()
    expect(register).toHaveBeenCalledWith('/sw.js', { scope: '/' })
  })

  it('leaves a browser tab alone', async () => {
    const register = env({ standalone: false })
    await registerShareWorker()
    expect(register).not.toHaveBeenCalled()
  })

  it('does nothing on a plain-http address, where there are no workers', async () => {
    const register = env({ secure: false })
    await registerShareWorker()
    expect(register).not.toHaveBeenCalled()
  })

  it('does nothing in a browser without service workers', async () => {
    env({ hasWorker: false })
    await expect(registerShareWorker()).resolves.toBeUndefined()
  })

  it('reports a registration that fails, so a share is not lost to a silent failure', async () => {
    const register = env()
    register.mockRejectedValue(new Error('SecurityError'))
    await expect(registerShareWorker()).rejects.toThrow('SecurityError')
  })
})
