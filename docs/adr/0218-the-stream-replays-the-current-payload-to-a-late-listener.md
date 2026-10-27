# ADR 0218: The stream replays the current payload to a late listener

## Status

Accepted (2026-10-27). Amends [ADR 0215](0215-tiles-subscribe-to-live-data.md):
the `useRetain*` hooks and the rule that the shell starts every store at boot
are gone, and step 1 of "Adding a live tile" no longer asks for one. Folds the
hand-rolled vessel identity store, which predates that ADR, into the
vessel-state store. The transport of [ADR 0037](0037-signalk-delta-stream-ingestion.md)
and the hub of [ADR 0097](0097-shared-telemetry-stream-hub.md) are unchanged.

## Context

The server seeds a new connection with the current payload of every event. The
browser shares one connection between all its listeners, and
`subscribeTelemetry` kept no payload of its own, so a listener added after the
seed heard nothing until its event next went out. Alarms come every two seconds,
but `vessel-state` and `gauge-values` are change-gated and can stay quiet for
as long as the boat sits still.

ADR 0215 worked around this twice. Each store stays subscribed once started,
so the snapshot is always the last thing the server said. And `AppShell` called
nine `useRetain*` hooks (gauge values, vessel state, alarms, autopilot,
electrical, solar, tanks, nearby vessels, radar targets) so that every store
subscribed in the same effects flush that opened the EventSource. The rule
behind it, that a store must be listening before the seed arrives, lived in the
shell and nowhere in the stream module. A tenth store, or a shell that stopped
calling one hook, would show blanks or a false "All clear" with no test to say
why.

The vessel identity hook (`use-vessel-identity.ts`) was the other workaround.
It parsed the `vessel-state` event a second time, kept its own subscription
that it dropped at zero holders (so `AppShell` read the time zone just to keep
it alive), and republished every second from a clock timer. The marine header
reads a name, a model and a state, yet re-rendered every second with the clock.
Its initial snapshot also claimed the status "At Anchor" before any event had
arrived, which is a claim the boat had not made.

## Decision

### The stream remembers, and replays to the late listener only

`use-telemetry-stream.ts` keeps the last raw payload of each event for the
life of the current connection. `subscribeTelemetry(event, onData)` hands the
remembered payload to the new listener, and only to it, before it returns.

- **It listens for every seeded event on every connection.** The nine events
  the hub seeds are named in one list in the stream module, and each
  EventSource listens for all of them whether or not anything has subscribed.
  Without that, a payload sent before a store's first reader mounted would
  never be seen, and the replay would cover only events someone already
  listened to. An event outside the list is remembered from the moment its
  first listener subscribes, and a seeded event keeps being remembered after
  its last listener leaves.
- **A listener is told when the payload arrived.** The stream stores each
  payload with `performance.now()` at its arrival and passes it to listeners
  as a second argument: now for a live frame, the stored arrival for a replay.
  The vessel clock needs it. A store that starts five minutes after a frame was
  remembered would otherwise take the replayed `datetime` as current and read
  five minutes slow.
- **A remembered frame is still what would be shown.** The replay hands out the
  last frame the server broadcast, which carries the ages from the moment it
  was built. That is correct only because the backend's change gate
  (`telemetry_gate.go`) re-sends an event whenever anything a tile shows
  changes. Fresh ages (up to 120 s) band to one value, since no tile shows
  them. A stale age bands to the whole minute (the whole day past a day), which
  is how often its label changes, so crossing into stale and each later label
  change send a new frame. The nearby vessels' per-vessel ages stay exact. The
  radar targets' ages are banded because nothing renders them. Alarms and the
  autopilot are not gated. If the gate is ever loosened so that it skips a
  change a tile would show, a late listener will be handed an out-of-date frame
  until the next broadcast.
- **Delivery is synchronous.** A microtask would let a live frame arrive
  between the subscription and the replay, and the older remembered payload
  would then overwrite the newer one, so the listener would need a flag to
  refuse it. Synchronous delivery has no such gap: the replay is the first
  thing the listener hears, and any live frame follows it. A store applying it
  has the data before its reader's first check. `useSyncExternalStore` accepts
  a store that changes inside `subscribe`: the listener is added before the
  store starts, so the reader is notified and re-renders. The cost is that the
  listener runs before `subscribeTelemetry` returns, so it has no unsubscribe
  handle yet. Every listener here is a store's `onEvent`, which does not need
  one.
- **A late listener never gets a frame from before an outage.** The remembered
  payloads are dropped when the connection errors (whether the browser retries
  the same instance or we dial a new one), when the silence watchdog or an
  offline event replaces it, and when the last subscriber leaves. The reconnect
  re-seeds from the server, and a listener that subscribes during the outage
  hears nothing until then. This is the fail-fast rule: stale data is never
  handed out as current. The stores that were already listening keep their last
  snapshot through the outage as before, and `useTelemetryStatus` says the feed
  is down.

### The stores start on their first reader, and the retain hooks are gone

`createTelemetryStore` keeps its lifecycle: it starts with its first reader
and stays subscribed for the life of the page, so its snapshot survives
the moment between one page's tiles unmounting and the next page's mounting.
Nothing else changes there. What goes is the reason to start a store before any
reader needs it. A store started late now begins from the payload the stream
remembers, not from silence.

All nine `useRetain*` exports, the helper's `useRetain`, and their calls in
`AppShell` are deleted. The shell still reads the navigation state from the
vessel-state store, so that store starts at boot. `getVesselStateSnapshot`,
which a click handler calls without subscribing, relies on that.

The Alarms page and the wall's pill still show the list the moment they mount.
A test mounts them after the seed has been delivered and no reader has ever
existed, and they show the seeded list with no new event.

Step 1 of "Adding a live tile" in ADR 0215 becomes: a stream event gets a
store built on `createTelemetryStore`, with an `apply` that returns null for a
tick that changes nothing shown, and selectors. If the event is new on the
server, add its name to the seeded list in the stream module.

### Vessel identity is fields of the vessel-state store

The store gains two fields, null until the boat has said them:

- **`boatName`**: the name with its prefix ("M/V Pikorua"). An absent prefix
  reads as "M/V", as before, and a blank name reads as unset.
- **`timeZone`**: the vessel's IANA zone.

The status string and the SignalK connection already had a home.
`navigationState` is the event's `status`, which the header called
`vesselStatus`, so one field serves both. `source` carries "signalk" when
SignalK is connected; the status bar reads `source === 'signalk'` and treats a
null source as unknown, not as lost.

**The header says nothing about the state until it is known.** With a null
`navigationState` the Vessel tile shows the model alone, with no state and no
separator. An unknown state is left out, not drawn as a dash, because a dash
beside a model reads as a value that failed to load.

### The clock is a separate source, fed by the same parse

The vessel-state store still does not keep `datetime`, because it moves on
every tick and no field reader shows it. The store reads it while it parses
the event and hands it to `lib/server-clock.ts`, so each event is parsed once.

The clock source is a module-level source read with `useSyncExternalStore`. It
keeps the latest server datetime and the moment it arrived, and reads the
boat's time as that stamp plus the time since, to the whole second. The time
since is measured on `performance.now()`, not `Date.now()`, so a correction of
the kiosk's wall clock (NTP) does not move the boat's time. The arrival it
uses is the one the stream passes with the payload, so a replayed frame is
aged by how long the stream remembered it. It ticks only while a clock
reader is mounted: a timer aligned to the boat's second starts with the first
reader and stops with the last. Each new datetime re-bases it. An event with no
datetime, or one that does not parse, leaves the clock counting from the last
good stamp. A stamp that would move the shown time back by less than two
seconds is network jitter and is ignored, so the seconds never step backwards
for it. A larger step back is a real change of the server's time and is
applied as it is.

Before any datetime has arrived the clock is unknown (null). It is not the
browser's time, which the old snapshot used and which can be wrong, or in
another zone, on a wall display. The status bar and the Clock tile show dashes
until the first event.

`hooks/use-vessel-clock.ts` is the only reader of the clock: the module exports
a subscribe and a read function, not a hook. `useVesselClock` also reads the
zone from the vessel-state store, which starts that store, and the store is what
feeds the clock. A clock reader with no other vessel-state reader therefore
still gets a time. It combines the clock with the zone for the readers
that format it. The formatters (`formatClock`, `formatDate`, `formatWeekday`,
`isSameLocalDate`) move to `lib/vessel-time.ts` unchanged. `ClockTile` takes
the time, the formatted clock and the zone as props, and `LiveClockTile` reads
them, like every other live tile. The marine header reads `boatName` and
`navigationState` through `useVesselFields` and is memoized, so neither the
clock nor a vessel-state tick that changes neither field re-renders it.
`use-vessel-identity.ts` is deleted, and the callers that wanted only the zone
read it with `useVesselStateSelector(selectTimeZone)`.

### Tests drive the real stores through a fake EventSource

The EventSource is the seam. The browser's is one adapter and
`src/test/helpers/fake-telemetry-stream.ts` is the other. A test calls
`installFakeTelemetryStream()`, then `emit(event, payload)` for a frame from the
server, or `seed(event, payload)` for what the server holds when a connection
opens. A connection is seeded once per event, as the hub does, and the stream
replays the remembered frame to any reader that mounts later, so a newer frame
is never rewound. `error()`, `heartbeat()` and `open()` move the status, and a
closed connection redials after the stream's backoff (fake timers advance it)
and is seeded again. Teardown unmounts the test's components first, then resets
the stores of whichever copy of the modules is loaded, so a file using
`vi.resetModules` leaves no watchdog or window listener behind. There is no
`disconnect()`: the stream reads disconnected only with no subscribers, and a
mounted store is one. To count a store's subscriptions, wrap the real
`subscribeTelemetry` in `vi.fn` through `vi.mock` with `importOriginal`. The real stream and the real
stores run, so a test sees the equality rules and the render counts the boat
would, and it never touches the listener registry, the seeded list or the
replay.

This retired the three stand-in store modules (`vessel-state-mock`,
`electrical-state-mock`, `anchor-watch-mock`). The anchor watch is a poll, so
its tests land a `GET /api/anchor-watch` answer with `landAnchorWatch`
(`src/test/helpers/anchor-watch-poll.ts`) and check the writes on the fetch
stub. What still mocks the stream module is a test of its status alone, where
no store is mounted to open a connection: `connection-banner`,
`display-status-badge`, `embed-tile`, `display-shell`,
`signalk-discovery-prompt` and `use-auth`. `use-telemetry-stream.test.ts`
tests the stream module itself and keeps its own fake.

## Rejected

- **Keeping the retain hooks and only documenting the order.** The rule would
  stay a convention that a tenth store could break silently, and each new store
  would still need a line in the shell.
- **A microtask replay.** It needs a guard against an older replay landing
  after a newer live frame, and it lets a reader render one frame of the
  initial snapshot more than it needs to.
- **Replaying frames older than the connection.** A reconnect re-seeds, and a
  frame from before an outage could be hours old by then.
- **Parsing `datetime` in the vessel-state store as a field.** Every tick
  would change the snapshot and wake every reader of the store.
- **A second subscription to the event for the clock.** It would parse the
  whole payload a second time on every tick, the thing the old hook did.
- **Falling back to the browser's clock before the first event.** It would show
  a time the boat had not given, in a zone it had not given.

## Consequences

- A store can start at any moment and still start from what the server last
  said. There is no ordering rule to keep, and adding a store means adding its
  event name to the stream's seeded list, not a line in the shell.
- A listener may be called before `subscribeTelemetry` returns.
- The marine header re-renders only when the boat's name or state changes. The
  per-second re-render belongs to the status bar and the Clock tile.
- Until the first vessel-state event, the header shows no state, the status bar
  clock and the Clock tile show dashes, and the status badge treats a null
  source as unknown rather than "No Signal".
- The seeded-event list in the stream module (the client's one declaration)
  copies the hub's event list in `backend/vessel_state_stream.go`. A Go test,
  `telemetry_seeded_events_test.go`, reads the list from the TypeScript source
  and fails when the two differ, so a new server event cannot be left out
  silently. Each side has a comment pointing at the test.

## Not verified

The replay, the reconnect behaviour and the header are covered by tests over a
fake EventSource. It has not been run against the live server.
