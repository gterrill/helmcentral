import { act, cleanup } from '@testing-library/react'
import { afterEach, beforeEach, vi } from 'vitest'

/**
 * The second adapter behind the telemetry stream's EventSource seam. The real
 * stream (hooks/use-telemetry-stream.ts) and the real stores run unchanged;
 * the browser's EventSource is the only thing replaced. A test says what the
 * server sent, and the stream's seed, replay, watchdog and reconnect behave as
 * they do on the boat.
 *
 *   const stream = installFakeTelemetryStream()
 *   it('...', () => {
 *     render(<Tile />)                       // a reader opens the connection
 *     stream.emit('vessel-state', { depth: 3.2 })
 *   })
 *
 * Call it once per file, at the top of a describe or the module. It installs
 * the fake before each test. After each test it unmounts what the test
 * rendered, resets every store (which is how the stream's last subscriber
 * leaves, ending its watchdog and window listeners) and removes the fake. The
 * reset goes to whichever copy of the store module is loaded then, so a file
 * that uses vi.resetModules and dynamic imports is cleaned up too.
 *
 * Options: `seeds` is what the server holds for each event at every test's
 * start, e.g. `{ 'vessel-state': {} }` to make a connection live with no
 * readings.
 *
 * emit() is wrapped in act(). It needs a connection, which exists once any
 * store or status reader has mounted; with none it throws rather than drop the
 * frame, so a test that forgot to render its reader says so.
 */
export interface FakeTelemetryStream {
  /** Stubs the global EventSource and clears the seeds. */
  install: (seeds?: Record<string, unknown>) => void
  /** Unmounts, resets the stores and restores the global. Runs after each test by installFakeTelemetryStream(). */
  teardown: () => Promise<void>
  /**
   * Delivers one event on the live connection. An object is sent as its JSON;
   * a string is sent as the raw frame, for malformed payloads.
   */
  emit: (event: string, payload?: unknown) => void
  /**
   * What the server holds for an event, sent to every connection as it is
   * dialled, as the real server seeds a new connection. Use it to give a test
   * its readings before anything has mounted: the stream remembers the seed, so
   * a reader that mounts at any point sees it. A connection is seeded once per
   * event, as the hub does, so a reader mounting later is handed the remembered
   * frame, not the seed again. Cleared by install() and teardown(). Call it
   * again to change what the next connection is seeded with.
   */
  seed: (event: string, payload: unknown) => void
  /** The server's keep-alive: counts as traffic and moves the status to connected. */
  heartbeat: () => void
  /** The transport opened. The stream waits for traffic, so this changes nothing visible. */
  open: () => void
  /** A transport error. `closed` is the browser giving up for good (non-200 or wrong content-type). */
  error: (options?: { closed?: boolean }) => void
  /** How many connections the stream has dialled so far, reconnects included. */
  readonly connections: number
  /** Whether the live connection exists and is not closed. */
  readonly isConnected: boolean
}

class FakeEventSource {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSED = 2
  static instances: FakeEventSource[] = []
  /** The server's current payload per event, delivered when a listener first attaches to a new connection. */
  static seeds = new Map<string, string>()

  readyState = FakeEventSource.CONNECTING
  onerror: ((ev: Event) => void) | null = null
  private listeners = new Map<string, Set<(e: Event) => void>>()
  private seeded = new Set<string>()

  constructor(public url: string) {
    FakeEventSource.instances.push(this)
  }

  addEventListener(type: string, listener: (e: Event) => void) {
    let set = this.listeners.get(type)
    if (!set) this.listeners.set(type, (set = new Set()))
    const added = !set.has(listener)
    set.add(listener)
    // The hub seeds a connection once per event, as the stream wires its
    // listeners and before the first reader is handed anything. The stream
    // adds the same dispatcher again for each later subscriber; that is not a
    // new connection and gets no seed.
    const seed = FakeEventSource.seeds.get(type)
    if (added && seed !== undefined && !this.seeded.has(type)) {
      this.seeded.add(type)
      listener({ type, data: seed, currentTarget: this } as unknown as MessageEvent)
    }
  }

  removeEventListener(type: string, listener: (e: Event) => void) {
    this.listeners.get(type)?.delete(listener)
  }

  dispatchEvent() { return false }

  close() { this.readyState = FakeEventSource.CLOSED }

  deliver(event: string, data: string) {
    for (const l of [...(this.listeners.get(event) ?? [])]) {
      l({ type: event, data, currentTarget: this } as unknown as MessageEvent)
    }
  }
}

function live(): FakeEventSource | undefined {
  const last = FakeEventSource.instances.at(-1)
  return last && last.readyState !== FakeEventSource.CLOSED ? last : undefined
}

function requireLive(what: string): FakeEventSource {
  const es = live()
  if (!es) throw new Error(`fake telemetry stream: ${what} with no open connection; render a reader of the stream first`)
  return es
}

let previousEventSource: unknown

export const fakeTelemetryStream: FakeTelemetryStream = {
  install(seeds = {}) {
    FakeEventSource.instances = []
    FakeEventSource.seeds = new Map()
    for (const [event, payload] of Object.entries(seeds)) fakeTelemetryStream.seed(event, payload)
    previousEventSource = (globalThis as { EventSource?: unknown }).EventSource
    vi.stubGlobal('EventSource', FakeEventSource)
  },
  async teardown() {
    // Readers first, so no component is mounted over a store being reset. Then
    // the stores of the module copy loaded now (a test that used vi.resetModules
    // has its own), which releases the stream's last subscriber.
    cleanup()
    const { resetTelemetryStoresForTest } = await import('@/lib/telemetry-store')
    act(() => { resetTelemetryStoresForTest() })
    FakeEventSource.instances = []
    FakeEventSource.seeds = new Map()
    vi.stubGlobal('EventSource', previousEventSource)
  },
  emit(event, payload = {}) {
    const es = requireLive(`emit('${event}')`)
    const data = typeof payload === 'string' ? payload : JSON.stringify(payload)
    act(() => { es.deliver(event, data) })
  },
  seed(event, payload) {
    FakeEventSource.seeds.set(event, typeof payload === 'string' ? payload : JSON.stringify(payload))
  },
  heartbeat() {
    const es = requireLive('heartbeat')
    act(() => { es.deliver('heartbeat', '') })
  },
  open() {
    const es = requireLive('open')
    es.readyState = FakeEventSource.OPEN
    act(() => { es.deliver('open', '') })
  },
  error(options = {}) {
    const es = requireLive('error')
    es.readyState = options.closed ? FakeEventSource.CLOSED : FakeEventSource.CONNECTING
    act(() => { es.onerror?.(new Event('error')) })
  },
  get connections() { return FakeEventSource.instances.length },
  get isConnected() { return live() !== undefined },
}

/** Installs the fake before each test and tears it down after. Returns the controls. */
export function installFakeTelemetryStream(options: { seeds?: Record<string, unknown> } = {}): FakeTelemetryStream {
  beforeEach(() => { fakeTelemetryStream.install(options.seeds) })
  afterEach(async () => { await fakeTelemetryStream.teardown() })
  return fakeTelemetryStream
}
