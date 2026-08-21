import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { beforeAll, describe, expect, it, vi } from 'vitest'

/**
 * The service worker is plain JS served verbatim from public/, so it is never
 * bundled and never type-checked. This loads the real file and evaluates it
 * against a fake `self`, which works precisely because the file is written to
 * touch nothing else — a constraint recorded in its header comment.
 */
const swSource = readFileSync(
  resolve(dirname(fileURLToPath(import.meta.url)), '../../public/sw.js'),
  'utf-8',
)

type Handler = (event: unknown) => void

interface FakeSelf {
  addEventListener: (type: string, handler: Handler) => void
  registration: { showNotification: ReturnType<typeof vi.fn> }
  clients: { matchAll: ReturnType<typeof vi.fn>; openWindow: ReturnType<typeof vi.fn>; claim: ReturnType<typeof vi.fn> }
  skipWaiting: ReturnType<typeof vi.fn>
  handlers: Record<string, Handler>
}

function loadServiceWorker(): FakeSelf {
  const handlers: Record<string, Handler> = {}
  const fakeSelf: FakeSelf = {
    addEventListener: (type, handler) => { handlers[type] = handler },
    registration: { showNotification: vi.fn().mockResolvedValue(undefined) },
    clients: {
      matchAll: vi.fn().mockResolvedValue([]),
      openWindow: vi.fn().mockResolvedValue(undefined),
      claim: vi.fn().mockResolvedValue(undefined),
    },
    skipWaiting: vi.fn(),
    handlers,
  }

  new Function('self', swSource)(fakeSelf)
  return fakeSelf
}

/**
 * A real `waitUntil` extends the event's lifetime; here it captures the promise
 * so a test can await the work the handler kicked off. Awaiting the handler
 * itself is not enough — the handlers are synchronous and hand their real work
 * to waitUntil.
 */
function captureWaitUntil() {
  const pending: Promise<unknown>[] = []
  return {
    waitUntil: (promise: Promise<unknown>) => { pending.push(promise) },
    settled: () => Promise.all(pending),
  }
}

function pushEvent(payload: unknown, malformed = false) {
  const lifetime = captureWaitUntil()
  return {
    data: {
      json: () => {
        if (malformed) throw new SyntaxError('not json')
        return payload
      },
      text: () => (malformed ? 'not json' : JSON.stringify(payload)),
    },
    waitUntil: lifetime.waitUntil,
    settled: lifetime.settled,
  }
}

const ALARM = {
  title: '[ALARM] Pikorua: House bank low',
  body: 'House bank low: below 11.8 (11)',
  tag: 'rule-1|raised',
  state: 'alarm',
  kind: 'raised',
  url: '/',
}

describe('service worker push handling', () => {
  beforeAll(() => {
    expect(swSource).toContain('push')
  })

  it('registers the push, notificationclick and pushsubscriptionchange handlers', () => {
    const sw = loadServiceWorker()

    expect(Object.keys(sw.handlers)).toEqual(
      expect.arrayContaining(['install', 'activate', 'push', 'notificationclick', 'pushsubscriptionchange']),
    )
  })

  // A boat alarm cannot wait for every tab to close before a new worker takes
  // over, so the worker activates immediately.
  it('takes over immediately on install and activate', () => {
    const sw = loadServiceWorker()

    sw.handlers.install(captureWaitUntil())
    expect(sw.skipWaiting).toHaveBeenCalled()

    sw.handlers.activate(captureWaitUntil())
    expect(sw.clients.claim).toHaveBeenCalled()
  })

  it('shows the alarm with the title, body and tag the backend sent', async () => {
    const sw = loadServiceWorker()

    const event = pushEvent(ALARM)
    sw.handlers.push(event)
    await event.settled()

    expect(sw.registration.showNotification).toHaveBeenCalledTimes(1)
    const [title, options] = sw.registration.showNotification.mock.calls[0]
    expect(title).toBe(ALARM.title)
    expect(options.body).toBe(ALARM.body)
    // The tag makes the OS replace rather than stack a duplicate, and
    // renotify:false stops the replacement buzzing a second time. Together with
    // the backend's Topic header this is what makes retry duplicates harmless.
    expect(options.tag).toBe(ALARM.tag)
    expect(options.renotify).toBe(false)
  })

  it('requires interaction for alarm and emergency but not for lesser states', async () => {
    const sw = loadServiceWorker()

    for (const state of ['alarm', 'emergency', 'warn']) {
      const event = pushEvent({ ...ALARM, state })
      sw.handlers.push(event)
      await event.settled()
    }

    const calls = sw.registration.showNotification.mock.calls
    expect(calls[0][1].requireInteraction).toBe(true)
    expect(calls[1][1].requireInteraction).toBe(true)
    expect(calls[2][1].requireInteraction).toBe(false)
  })

  /**
   * Silence is the one outcome an alarm system must never choose. A push event
   * that shows no notification is also a spec violation — Chrome substitutes
   * its own "site updated in the background" message — so an unreadable payload
   * still surfaces something the crew can act on.
   */
  it('still shows a notification when the payload cannot be parsed', async () => {
    const sw = loadServiceWorker()

    const event = pushEvent(null, true)
    sw.handlers.push(event)
    await event.settled()

    expect(sw.registration.showNotification).toHaveBeenCalledTimes(1)
    const [title] = sw.registration.showNotification.mock.calls[0]
    expect(String(title).toLowerCase()).toContain('helmcentral')
  })

  it('focuses an existing window on click rather than opening a second one', async () => {
    const sw = loadServiceWorker()
    const focus = vi.fn().mockResolvedValue(undefined)
    sw.clients.matchAll.mockResolvedValue([{ url: 'https://boat.ts.net/', focus, navigate: vi.fn() }])

    const lifetime = captureWaitUntil()
    sw.handlers.notificationclick({
      notification: { close: vi.fn(), data: { url: '/' } },
      waitUntil: lifetime.waitUntil,
    })
    await lifetime.settled()

    expect(focus).toHaveBeenCalled()
    expect(sw.clients.openWindow).not.toHaveBeenCalled()
  })

  it('opens a window on click when none is open', async () => {
    const sw = loadServiceWorker()
    sw.clients.matchAll.mockResolvedValue([])

    const lifetime = captureWaitUntil()
    sw.handlers.notificationclick({
      notification: { close: vi.fn(), data: { url: '/' } },
      waitUntil: lifetime.waitUntil,
    })
    await lifetime.settled()

    expect(sw.clients.openWindow).toHaveBeenCalledWith('/')
  })
})
