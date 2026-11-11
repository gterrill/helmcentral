import { beforeEach, describe, expect, it, vi } from 'vitest'

// The router rewrites the address bar to its canonical form soon after the app
// starts, dropping ?shared=1, so the flags are read once, at import, and the
// upload page takes them from here.
async function loadAt(search: string) {
  window.history.replaceState(null, '', `/inbox/upload${search}`)
  vi.resetModules()
  return import('@/lib/inbox-launch')
}

describe('how the upload page was opened', () => {
  beforeEach(() => { window.history.replaceState(null, '', '/') })

  it('knows a share just arrived and starts the upload', async () => {
    const launch = await loadAt('?shared=1')
    expect(launch.takeLaunch()).toEqual({ autoStart: true, shareError: null })
  })

  it('gives the flags once, so going back to the page later does not start another upload', async () => {
    const launch = await loadAt('?shared=1')
    launch.takeLaunch()
    expect(launch.takeLaunch()).toEqual({ autoStart: false, shareError: null })
  })

  it('reports a share the phone could not keep', async () => {
    expect((await loadAt('?share_error=1')).takeLaunch()).toEqual({ autoStart: false, shareError: 'not-kept' })
  })

  it('reports a share that reached the server because no worker was running', async () => {
    expect((await loadAt('?share_error=2')).takeLaunch()).toEqual({ autoStart: false, shareError: 'no-worker' })
  })

  it('starts nothing for an ordinary visit', async () => {
    expect((await loadAt('')).takeLaunch()).toEqual({ autoStart: false, shareError: null })
  })

  it('lets the waiting banner ask for an upload on the page it opens', async () => {
    const launch = await loadAt('')
    launch.requestAutoUpload()
    expect(launch.takeLaunch().autoStart).toBe(true)
    expect(launch.takeLaunch().autoStart).toBe(false)
  })
})
