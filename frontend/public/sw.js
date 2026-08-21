/*
 * Helmcentral service worker — alarm push only.
 *
 * SCOPE FENCE, deliberately narrow (see docs/adr/0045): there is no `fetch`
 * handler and no caching here. Caching the app shell would serve stale JS after
 * an upgrade, and this project has no cache-busting story for a service worker.
 * This file exists so an alarm reaches the lock screen with the dashboard
 * closed, and for nothing else.
 *
 * It also touches only `self` — no imports, no other globals — which is what
 * lets src/test/service-worker-push.test.ts load and exercise the real file
 * rather than a copy that can drift.
 */

// A boat alarm cannot wait for every open tab to close before a fixed worker
// takes over, so a new version activates immediately.
self.addEventListener('install', (event) => {
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(self.clients.claim())
})

self.addEventListener('push', (event) => {
  event.waitUntil(showAlarm(event))
})

function showAlarm(event) {
  let payload = null
  try {
    payload = event.data ? event.data.json() : null
  } catch (err) {
    payload = null
  }

  // An unreadable payload still surfaces something. Showing nothing would be
  // both a spec violation (Chrome substitutes its own "site updated in the
  // background" notice) and, far worse, an alarm system choosing silence.
  if (!payload || !payload.title) {
    return self.registration.showNotification('Helmcentral alarm', {
      body: 'An alarm was raised but its details could not be read. Open Helmcentral to see it.',
      tag: 'helmcentral-unreadable',
      renotify: false,
      requireInteraction: true,
      icon: '/icons/icon-192.png',
      badge: '/icons/badge-72.png',
      data: { url: '/' },
    })
  }

  const urgent = payload.state === 'alarm' || payload.state === 'emergency'

  return self.registration.showNotification(payload.title, {
    body: payload.body || '',
    // Same tag => the OS replaces the existing notification instead of stacking
    // a second one, and renotify:false means the replacement does not buzz
    // again. This is the device-side half of the duplicate collapsing that the
    // backend's Topic header starts (ADR 0038).
    tag: payload.tag || 'helmcentral-alarm',
    renotify: false,
    // A live alarm should stay on screen until someone deals with it; a
    // cleared or lesser notification should not demand a dismissal.
    requireInteraction: urgent,
    icon: '/icons/icon-192.png',
    badge: '/icons/badge-72.png',
    data: { url: payload.url || '/' },
  })
}

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  event.waitUntil(openDashboard(event.notification.data && event.notification.data.url))
})

async function openDashboard(url) {
  const target = url || '/'
  const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })

  // Focusing what is already open beats opening a second dashboard on a helm
  // tablet that may only have room for one.
  for (const client of windows) {
    if ('focus' in client) {
      if ('navigate' in client && client.navigate) {
        try {
          await client.navigate(target)
        } catch (err) {
          // Navigation can be refused cross-origin; focusing still helps.
        }
      }
      return client.focus()
    }
  }

  return self.clients.openWindow(target)
}

/*
 * A browser may rotate a subscription on its own (storage pressure, a long
 * absence). Re-registering here is what stops a device going quietly dark:
 * the server would otherwise keep pushing at a dead endpoint until the next
 * 410 prunes it, and the crew would never be told.
 */
self.addEventListener('pushsubscriptionchange', (event) => {
  event.waitUntil(resubscribe(event))
})

async function resubscribe(event) {
  const old = event.oldSubscription
  const applicationServerKey = old && old.options ? old.options.applicationServerKey : null
  if (!applicationServerKey) return

  const subscription = await self.registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey,
  })

  await fetch('/api/alarm-transports/webpush/subscribe', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ...subscription.toJSON(), label: '', user_agent: '' }),
  })
}
