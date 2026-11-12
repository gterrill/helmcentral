// The one service worker file and the one way to register it. Push registers it
// on the operator's click (hooks/use-web-push.ts) and Share to Helmcentral when
// the app starts installed (lib/share-worker.ts); each keeps its own gate, and
// registering twice is harmless.

export const SERVICE_WORKER_PATH = '/sw.js'

/** Registers the app's worker for the whole origin. Rejects if the browser refuses. */
export function registerAppWorker(): Promise<ServiceWorkerRegistration> {
  return navigator.serviceWorker.register(SERVICE_WORKER_PATH, { scope: '/' })
}
