// How the upload page was opened (ADR 0280). The service worker sends the phone
// to /inbox/upload?shared=1 after keeping a share, or ?share_error=1 when it
// could not. The router rewrites the address bar to its canonical form soon
// after the app starts, which drops the query, so this module reads it once when
// it is first imported (main.tsx imports it first) and the page takes the flags
// from here, once.

export type ShareError =
  /** The worker ran but the phone would not keep the files (storage full or blocked). */
  | 'not-kept'
  /** The files reached the server, which has no use for them: no worker was running. */
  | 'no-worker'

export interface Launch {
  /** Start the upload now: files were just shared, or the waiting banner asked. */
  autoStart: boolean
  shareError: ShareError | null
}

function readSearch(): Launch {
  const params = new URLSearchParams(globalThis.location?.search ?? '')
  const code = params.get('share_error')
  return {
    autoStart: params.get('shared') === '1',
    shareError: code === '1' ? 'not-kept' : code === '2' ? 'no-worker' : null,
  }
}

let pending: Launch = readSearch()

/** The flags, once. The next call gets none. */
export function takeLaunch(): Launch {
  const taken = pending
  pending = { autoStart: false, shareError: null }
  return taken
}

/** Asks the upload page, when it opens, to start sending what is waiting. */
export function requestAutoUpload(): void {
  pending = { ...pending, autoStart: true }
}
