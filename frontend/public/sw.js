/*
 * Helmcentral service worker — alarm push only.
 *
 * SCOPE FENCE, deliberately narrow (see docs/adr/0045): there is no caching
 * here, and the only `fetch` handling is the share target's POST (ADR 0280),
 * which every other request passes by. Caching the app shell would serve stale
 * JS after an upgrade, and this project has no cache-busting story for a
 * service worker. This file exists so an alarm reaches the lock screen with the
 * dashboard closed, and so a photo shared from the phone is kept; nothing else.
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
      data: { url: '/alarms' },
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
    // Push exists only for alarms, so a payload with no url still lands on
    // the alarms panel rather than the dashboard root.
    data: { url: payload.url || '/alarms' },
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
 * Share to Helmcentral (ADR 0280). The manifest's share_target posts the shared
 * files to SHARE_ACTION. This is the one request the worker answers; every
 * other request returns from the handler without respondWith, so the browser
 * deals with it exactly as if there were no fetch handler (the scope fence at
 * the top still holds: no caching, no rewriting).
 *
 * The files go to the waiting list in IndexedDB, in one transaction, and the
 * phone is sent to the upload page, which uploads them. If they cannot be kept
 * the phone is sent there with share_error=1 and the page says so. A share is
 * never dropped quietly.
 *
 * The database below is the same one src/lib/inbox-queue.ts opens. That file
 * defines the schema; src/test/service-worker-share.test.ts reads what this
 * writes through it, so the two cannot drift apart unnoticed.
 */
const SHARE_ACTION = '/share-target'
const SHARE_FIELD = 'files'
const QUEUE_DB = 'helmcentral-inbox-queue'
const QUEUE_DB_VERSION = 1
const QUEUE_STORE = 'pending'

self.addEventListener('fetch', (event) => {
  const request = event.request
  if (request.method !== 'POST') return
  if (new URL(request.url).pathname !== SHARE_ACTION) return
  event.respondWith(receiveShare(request))
})

async function receiveShare(request) {
  const origin = new URL(request.url).origin
  try {
    const form = await request.formData()
    const files = form.getAll(SHARE_FIELD).filter((v) => typeof v !== 'string')
    if (files.length === 0) throw new Error('the share held no files')
    await queueShared(files)
    return Response.redirect(origin + '/inbox/upload?shared=1', 303)
  } catch (err) {
    return Response.redirect(origin + '/inbox/upload?share_error=1', 303)
  }
}

function queueShared(files) {
  return new Promise((resolve, reject) => {
    const open = self.indexedDB.open(QUEUE_DB, QUEUE_DB_VERSION)
    open.onupgradeneeded = () => {
      if (!open.result.objectStoreNames.contains(QUEUE_STORE)) {
        open.result.createObjectStore(QUEUE_STORE, { keyPath: 'id' })
      }
    }
    open.onerror = () => reject(open.error || new Error('could not open the waiting list'))
    open.onsuccess = () => {
      const db = open.result
      const tx = db.transaction(QUEUE_STORE, 'readwrite')
      tx.oncomplete = () => { db.close(); resolve() }
      tx.onerror = () => { db.close(); reject(tx.error || new Error('could not write the waiting list')) }
      tx.onabort = () => { db.close(); reject(tx.error || new Error('the waiting list write was aborted')) }
      const now = Date.now()
      files.forEach((file, i) => {
        tx.objectStore(QUEUE_STORE).add({
          id: now.toString(36).padStart(9, '0') + '-' + String(i).padStart(5, '0') + '-' + Math.random().toString(36).slice(2, 8),
          blob: file,
          name: file.name,
          type: file.type,
          lastModified: file.lastModified,
          addedAt: now,
        })
      })
    }
  })
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
