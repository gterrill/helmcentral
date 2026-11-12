import { beforeEach, describe, expect, it, vi } from 'vitest'

// The router rewrites the address bar to its canonical form soon after the app
// starts, dropping ?shared=1, so the flags come from the address as it was
// opened (lib/opened-search) and the upload page takes them from here.
async function loadAt(search: string) {
  window.history.replaceState(null, '', `/inbox/upload${search}`)
  vi.resetModules()
  return import('@/lib/inbox-launch')
}

describe('how the upload page was opened', () => {
  beforeEach(() => { window.history.replaceState(null, '', '/') })

  it('knows a share just arrived', async () => {
    const launch = await loadAt('?shared=1')
    expect(launch.takeLaunch()).toEqual({ shared: true, shareError: null })
  })

  it('gives the same flags until they are cleared, so a draw React throws away loses nothing', async () => {
    const launch = await loadAt('?shared=1')
    launch.takeLaunch()
    expect(launch.takeLaunch()).toEqual({ shared: true, shareError: null })
  })

  it('forgets the flags once cleared, so going back to the page later does not say a share just arrived', async () => {
    const launch = await loadAt('?shared=1')
    launch.takeLaunch()
    launch.clearLaunch()
    expect(launch.takeLaunch()).toEqual({ shared: false, shareError: null })
  })

  it('reports a share the phone could not keep', async () => {
    expect((await loadAt('?share_error=store-failed')).takeLaunch()).toEqual({ shared: false, shareError: 'store-failed' })
  })

  it('reports a share that reached the server because no worker was running', async () => {
    expect((await loadAt('?share_error=no-worker')).takeLaunch()).toEqual({ shared: false, shareError: 'no-worker' })
  })

  it('ignores a share_error code it does not know', async () => {
    expect((await loadAt('?share_error=1')).takeLaunch()).toEqual({ shared: false, shareError: null })
  })

  it('reports nothing for an ordinary visit', async () => {
    expect((await loadAt('')).takeLaunch()).toEqual({ shared: false, shareError: null })
  })

  it('reports a share that held nothing the app can take', async () => {
    expect((await loadAt('?share_error=no-files')).takeLaunch()).toEqual({ shared: false, shareError: 'no-files' })
  })
})
