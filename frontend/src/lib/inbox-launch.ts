// How the upload page was opened (ADR 0280). The service worker sends the phone
// to /inbox/upload?shared=1 after keeping a share, or ?share_error=<code> when
// it could not. The flags come from the address as the page was opened
// (lib/opened-search.ts), because the router drops the query soon after start.
//
// They are cleared once the page is mounted, so going back to the page later does not say a share
// just arrived. Nothing starts by itself: a share lands on the page with its
// photos listed and the operator taps Upload.

import { OPENED_SEARCH } from '@/lib/opened-search'

/** Why a share did not reach the waiting list. The codes are the share_error values sw.js and the server send. */
export type ShareError =
  /** The worker ran but the phone would not keep the files (storage full or blocked). */
  | 'store-failed'
  /** The files reached the server, which has no use for them: no worker was running. */
  | 'no-worker'
  /** The worker ran but the share held no files it can take. */
  | 'no-files'

export interface Launch {
  /** Files were just shared and are on the waiting list. */
  shared: boolean
  shareError: ShareError | null
}

function readSearch(search: string): Launch {
  const params = new URLSearchParams(search)
  const code = params.get('share_error')
  return {
    shared: params.get('shared') === '1',
    shareError: code === 'store-failed' || code === 'no-worker' || code === 'no-files' ? code : null,
  }
}

let pending: Launch = readSearch(OPENED_SEARCH)

/**
 * The flags, the same on every call until clearLaunch. A render that React throws
 * away (a suspended or interrupted first draw, StrictMode) may ask more than once.
 */
export function takeLaunch(): Launch {
  return pending
}

/** Forgets the flags once the page has them mounted, so returning later does not say a share just arrived. */
export function clearLaunch(): void {
  pending = { shared: false, shareError: null }
}
