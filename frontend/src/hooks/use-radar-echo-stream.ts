import { apiBaseUrl } from '@/config/api'

/**
 * Shared WebSocket connection(s) for the radar picture overlay's spoke
 * stream, relayed by Helmcentral's own backend (`GET /api/radar/spokes?radar=<id>`,
 * backend/radar_spoke_relay.go) rather than dialled to mayara-server
 * directly -- see the plan's "Why the backend relays rather than the
 * browser connecting direct" for the mixed-content and CORS reasons.
 *
 * One connection per radar id, ref-counted, mirroring
 * use-telemetry-stream.ts's singleton-EventSource discipline: browsers cap
 * concurrent connections per origin, and the relay holds a goroutine and an
 * upstream mayara connection per browser client, so tile and drawer both
 * showing the same radar (the plan's "Two mounted maps") must share one
 * socket rather than each opening their own.
 *
 * use-telemetry-stream.ts's listener registry comment records a production
 * bug: its unsubscribe closure used to capture the EventSource instance
 * directly, so recreating the connection on error left every existing
 * subscriber attached to the dead instance. A reconnecting WebSocket has
 * the identical trap. The fix here is the same shape: `frameListeners`
 * lives on the per-radar `RadarEchoConnection` object, independent of
 * `connection.socket`, and survives a reconnect untouched -- only the
 * socket instance underneath it is swapped out. Every socket event handler
 * below guards with `connection.socket !== ws` so a stale/superseded
 * instance's events are ignored rather than acted on.
 *
 * Unlike EventSource, a WebSocket never retries itself: every close (clean
 * or not) needs a reconnect dialled by hand, so there is no CONNECTING-vs-
 * CLOSED distinction to make here the way use-telemetry-stream.ts has to
 * for EventSource's spec-mandated auto-retry.
 */

export type RadarEchoStatus = 'idle' | 'connected' | 'reconnecting' | 'disconnected'

type FrameListener = (frame: ArrayBuffer) => void

// Mirrors backend/radar_spoke_relay.go's minBackoff/maxBackoff (in turn
// backend/signalk_stream.go's defaultStreamMinBackoff/defaultStreamMaxBackoff)
// and streamStableDuration, so the browser leg and the backend->mayara leg
// behave consistently and neither drifts out of sync with the other's
// tuning.
const MIN_BACKOFF_MS = 1_000
const MAX_BACKOFF_MS = 30_000
const STABLE_DURATION_MS = 10_000

interface RadarEchoConnection {
  radarId: string
  socket: WebSocket | null
  subscriberCount: number
  backoff: number
  connectedAt: number | null
  reconnectTimer: ReturnType<typeof setTimeout> | null
  // Listener registry, independent of `socket` -- see the module doc above.
  frameListeners: Set<FrameListener>
}

const connections = new Map<string, RadarEchoConnection>()

// Status is a single shared value across every radar id's connection,
// rather than tracked per id: in practice at most one radar is ever
// overlaid at a time (the toggle shows one radar's picture), so one
// consumer-facing signal is enough, and it keeps subscribeRadarEchoStatus's
// signature free of a radarId parameter its only real caller (a status
// badge) has no natural use for.
let status: RadarEchoStatus = 'idle'
const statusListeners = new Set<(status: RadarEchoStatus) => void>()

function setStatus(next: RadarEchoStatus): void {
  if (status === next) return
  status = next
  for (const listener of statusListeners) listener(next)
}

/**
 * Same-origin ws(s) URL for radarId's spoke stream. Scheme follows the
 * page's own protocol -- wss: on https:, ws: otherwise -- which is the
 * whole point of relaying through this backend rather than dialling mayara
 * directly: `tailscale serve`'s https origin (ADR 0045) refuses a ws:
 * connection as mixed content, and this makes the overlay work on both the
 * LAN http origin and the tailnet https one.
 */
function radarSpokeUrl(radarId: string): string {
  const wsScheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  // apiBaseUrl (config/api.ts) is '' for the normal same-origin case, and
  // otherwise an absolute http(s) URL from the VITE_API_BASE_URL escape
  // hatch. Either way WebSocket needs an explicit ws(s) URL: unlike fetch,
  // a bare relative path resolves against the page's http(s) scheme and
  // the constructor throws rather than accepting it.
  const authority = apiBaseUrl === '' ? window.location.host : apiBaseUrl.replace(/^https?:\/\//, '')
  return `${wsScheme}//${authority}/api/radar/spokes?radar=${encodeURIComponent(radarId)}`
}

function getConnection(radarId: string): RadarEchoConnection {
  let connection = connections.get(radarId)
  if (!connection) {
    connection = {
      radarId,
      socket: null,
      subscriberCount: 0,
      backoff: MIN_BACKOFF_MS,
      connectedAt: null,
      reconnectTimer: null,
      frameListeners: new Set(),
    }
    connections.set(radarId, connection)
  }
  return connection
}

function attachSocket(connection: RadarEchoConnection): void {
  const ws = new WebSocket(radarSpokeUrl(connection.radarId))
  // Must happen synchronously, before any 'message' event can fire -- the
  // default binaryType is 'blob', and a frame delivered as a Blob rather
  // than an ArrayBuffer would reach subscribers as the wrong type entirely.
  ws.binaryType = 'arraybuffer'
  connection.socket = ws
  connection.connectedAt = null

  ws.addEventListener('open', () => {
    if (connection.socket !== ws) return // superseded by a later reconnect; ignore
    connection.connectedAt = Date.now()
    setStatus('connected')
  })

  ws.addEventListener('message', (event) => {
    if (connection.socket !== ws) return
    // binaryType is 'arraybuffer', set above before this socket could ever
    // receive a frame, so event.data is always an ArrayBuffer here. Handed
    // straight to subscribers, undecoded -- decoding is the caller's
    // business (spoke-message.ts), kept out of this module so it stays
    // testable without the decoder.
    const frame = event.data as ArrayBuffer
    for (const listener of connection.frameListeners) listener(frame)
  })

  // No 'error' handler: the WebSocket spec always follows a connection
  // failure with a 'close' event, which is where reconnection actually
  // happens below. 'error' itself carries no actionable detail (the spec
  // deliberately withholds it, to avoid leaking network information to the
  // page), so there is nothing an explicit handler here would do.

  ws.addEventListener('close', () => {
    if (connection.socket !== ws) return // already superseded; this close is stale
    scheduleReconnect(connection, ws)
  })
}

function scheduleReconnect(connection: RadarEchoConnection, deadSocket: WebSocket): void {
  deadSocket.close()
  connection.socket = null

  // A connection that stayed up for a while is evidence the relay/mayara is
  // healthy again, so the next drop retries promptly instead of inheriting
  // a backoff grown by an earlier, unrelated outage.
  if (connection.connectedAt !== null && Date.now() - connection.connectedAt >= STABLE_DURATION_MS) {
    connection.backoff = MIN_BACKOFF_MS
  }
  connection.connectedAt = null

  setStatus('reconnecting')

  connection.reconnectTimer = setTimeout(() => {
    connection.reconnectTimer = null
    if (connection.subscriberCount <= 0) return // last subscriber unsubscribed mid-backoff
    attachSocket(connection)
  }, connection.backoff)

  connection.backoff = Math.min(connection.backoff * 2, MAX_BACKOFF_MS)
}

function acquire(radarId: string): RadarEchoConnection {
  const connection = getConnection(radarId)
  connection.subscriberCount += 1
  if (!connection.socket && connection.reconnectTimer === null) {
    connection.backoff = MIN_BACKOFF_MS
    attachSocket(connection)
  }
  return connection
}

function release(connection: RadarEchoConnection): void {
  connection.subscriberCount = Math.max(0, connection.subscriberCount - 1)
  if (connection.subscriberCount > 0) return

  if (connection.reconnectTimer !== null) {
    clearTimeout(connection.reconnectTimer)
    connection.reconnectTimer = null
  }
  // Null out before close(): a real close event can be delivered
  // synchronously or on a later task depending on the implementation, and
  // either way the 'close' handler above must see this as already
  // superseded rather than schedule a pointless reconnect for a connection
  // nobody is subscribed to anymore.
  const socket = connection.socket
  connection.socket = null
  socket?.close()

  connection.backoff = MIN_BACKOFF_MS
  connection.connectedAt = null
  if (connections.get(connection.radarId) === connection) {
    connections.delete(connection.radarId)
  }
  setStatus('disconnected')
}

/**
 * Subscribes to one radar's spoke stream. Returns an unsubscribe function.
 * The upstream relay connection opens on the first subscriber for a given
 * radarId and closes once the last one unsubscribes.
 */
export function subscribeRadarSpokes(radarId: string, onFrame: FrameListener): () => void {
  const connection = acquire(radarId)
  connection.frameListeners.add(onFrame)

  return () => {
    connection.frameListeners.delete(onFrame)
    release(connection)
  }
}

/**
 * Subscribes to the radar echo connection status. Calls `cb` immediately
 * with the current value, then again on every change.
 */
export function subscribeRadarEchoStatus(cb: (s: RadarEchoStatus) => void): () => void {
  statusListeners.add(cb)
  cb(status)
  return () => {
    statusListeners.delete(cb)
  }
}
