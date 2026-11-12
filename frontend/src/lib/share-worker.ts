// Starts the service worker for the installed app so a share from the phone's
// share sheet has something to catch it (ADR 0280). Only an installed app can
// be a share target, so a browser tab and a kiosk that has not been installed
// are left alone, which keeps ADR 0045's stance that nothing installs a worker
// on first paint for no reason. Push registers the same worker on its own
// click; registering twice is harmless.

import { runningFromHomeScreen } from '@/hooks/use-screen-wake-lock'
import { registerAppWorker } from '@/lib/service-worker'

/** Resolves once registered or when there is nothing to do; rejects if the browser refuses. */
export async function registerShareWorker(): Promise<void> {
  if (!globalThis.isSecureContext) return
  if (!('serviceWorker' in globalThis.navigator)) return
  if (!runningFromHomeScreen()) return
  await registerAppWorker()
}
