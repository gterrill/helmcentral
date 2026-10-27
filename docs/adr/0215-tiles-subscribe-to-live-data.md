# ADR 0215: Tiles subscribe to the live data they show

## Status

Accepted (2026-10-27). Phases 0 and 1 are built; phases 2 to 7 are planned.
Keeps the transport of [ADR 0039](0039-bindable-gauge-widgets.md) and
[ADR 0083](0083-ages-ride-the-gauge-values-stream.md), and the shared stream of
[ADR 0097](0097-shared-telemetry-stream-hub.md).

## Context

Chrome put its "this tab is slowing down your browser" bar on the Passages tab.
The first cause was the passage map: every SignalK delta re-rendered the replay
panel, the panel handed the 3D boat layer an equal pose as a new object, and
each new object asked MapLibre for a frame. A paused map redrew about thirteen
times a second. The boat layer now ignores a pose equal by value, and that one
repaint stopped.

The repaint was a symptom. The cause sits in `AppShell` (`frontend/src/App.tsx`),
which calls the live-data hooks at its top level and so re-renders on every
stream tick. Measured in the browser, the shell renders on:

- `gauge-values`, every second
- `vessel-state`, every second
- autopilot, every second
- alarms, every two seconds
- the electrical, solar, tank, nearby-vessel and radar streams, at their own rates

Each shell render runs `renderWidget` for every tile on the page, because
`DashboardBentoGrid` is not memoized and calls it for each widget. The values
travel down as props. Memo catches some of it and misses the rest:

- Every `GaugeTile` received the whole `ages` map. One path ageing a second
  changed the map, so every gauge on the page re-rendered every second.
- `engines` lived inside the equipment tile context. An engine keying off
  changed the context, and every equipment host re-resolved its item.
- The routed pages (Documents, Passages, Inventory, Settings) re-rendered with
  the shell until each panel was wrapped in `memo`. That fix holds only while
  every prop handed to those panels stays stable.

Memo on each consumer treats the symptom one tile at a time. The shell still
renders on every delta, and every new tile or prop has to be stable by hand or
the cost returns.

## Decision

Each tile subscribes to the live data it shows. A tick re-renders only the
tiles whose own data changed; the shell renders only when something it draws
itself changes.

- **One store per stream, at module level.** Each store is built on
  `useSyncExternalStore`, the same singleton shape as `use-app-config.ts`. It
  holds one subscription to its stream event and parses each tick once,
  however many tiles read it.
- **Per-path or selector hooks.** A tile asks for what it shows: one path's
  value, one path's age, a subset of paths, or one engine's state. Listeners
  are kept per key, so a tick calls only the listeners whose key changed.
- **The shell keeps only what it needs itself.** It does not read a stream to
  pass it on.
- **`renderWidget` passes config, not live values.** A tile's props are its
  config, its edit state and stable callbacks.
- **The backend and its events stay as they are.** ADR 0039 rules out a
  subscription protocol or client registration: the backend already knows the
  bound paths from the page config, and sends them all. Selection happens in
  the browser. ADR 0083 keeps ages on the same `gauge-values` event, parsed
  once with the values.

### Gauge values (phase 1)

`frontend/src/hooks/use-gauge-values.ts` exports:

- `useGaugeValue(path)` and `useGaugeAge(path)`: one path's raw value or age,
  null when the event does not carry it.
- `useGaugeSubset(paths)`: values and ages for the given paths, keeping the
  same object while none of them changed, so it can feed a memoized tile.
- `useEngineStatus(id)`: one engine's running, off or lost state. An engine
  whose status is unchanged keeps its object across ticks.
- `useRetainGaugeValues()`: holds the store open without listening to it.

`GaugeTile` reads its own path. The three equipment hosts,
`EngineClusterHost`, `GaugeGroupHost` and `LampStripHost`, read the subset of
paths their resolved config binds; the pinned ribbon is a `LampStripHost`, so
it does too. The cluster and gauge-group hosts read the state of the engine
their item is, and pass it to the resolver, which no longer finds it in the
equipment tile context. `AppShell` reads no gauge value, age or engine state.

The presentational tiles, `GaugeTileView`, `EngineClusterTile`,
`GaugeGroupTile` and `LampStripTile`, still take values as props. Their tests
render them with fixed readings, and a host is the only thing that wires them
to the store.

### How long a store lives

A store with no holders unsubscribes and empties, so a tile mounted after that
starts blank and waits for the next tick. On the dashboard that gap would show
on every page switch and every wall rotation: the old page's tiles unmount
before the new page's mount, and for that moment nothing holds the store.

`AppShell` holds the gauge-values store for the life of the app with
`useRetainGaugeValues`, which acquires it on mount without adding a listener.
The shell never re-renders from it, and the last reading is there when the next
page's tiles mount.

A delayed release, keeping the store a few seconds after its last holder left,
was the other choice. It was rejected: the delay is a guess that has to outlast
the slowest page mount, it puts a timer into every test of the store, and the
app runs continuously on the boat, so a store that would only be released when
the app closes gains nothing from a timer. The subscription was already open
for the whole session while the shell read the values; it stays open.

### Phases

0. **Tests that fail first.** A tick that changes one gauge re-renders that
   gauge and not its neighbour, and does not re-render the shell; one path's
   age does not re-render another path's gauge; the store keeps its reading
   across a page switch.
1. **Gauge values.** As above.
2. **Alarms.** A store the lamp strips and the alarm surfaces read; the shell
   keeps only the part it draws itself.
3. **Vessel state.** A store the tiles select from; the shell selects only
   `navigationState`.
4. **Anchor watch, the Nearby map and the drawers.** Each reads its stream
   itself.
5. **Electrical, solar, tanks, nearby vessels, radar and autopilot.** One store
   each, with an equality check per field so an unchanged reading keeps its
   object.
6. **Polled forecasts.** Weather, wave, tide and forecast warnings are fetched
   by the tiles that show them, through the shared query cache, not by the
   shell.
7. **`WidgetHost` and the grid.** Once no tile takes a live prop, a memoized
   host per widget and a memoized grid stop a shell render from reaching any
   tile.

## Rejected

- **Memo everywhere.** It is what was done for the routed pages and the gauge
  tiles, and it is fragile: every prop on every path from the shell to a tile
  has to be stable, by hand, forever. One inline object or new callback undoes
  it silently, and the shell still renders on every delta. `renderWidget`
  closes over about sixty live values, so it cannot be made stable itself.
- **A server-side subscription protocol.** Tiles would register their paths
  and the backend would send each client only those. ADR 0039 rules it out:
  the backend already derives the bound paths from the page config, and a
  registration channel adds state that can drift from what is on screen.
  Choosing paths in the browser costs a key lookup per listener.
- **React context per stream.** A context provider re-renders every consumer
  when its value changes, and a stream value changes every tick. Context has
  no selector, so every gauge would still re-render when any path changed,
  which is the problem this ADR exists to fix.

## Consequences

- A gauge-values tick re-renders only the gauges, clusters, groups and lamp
  strips whose own paths changed. A tick that changes nothing they show
  re-renders nothing.
- An engine keying off re-renders the cluster and gauge-group hosts showing
  that engine, not every equipment host and not the shell.
- The shell still re-renders on the vessel-state, alarm, autopilot and other
  streams until phases 2 to 5 land, and `renderWidget` still runs for every
  tile when it does. The gauge tiles are protected from those renders by their
  own memo: their props are now config and stable callbacks only.
- The gauge-values subscription stays open for the life of the app, as it did
  when the shell read the values. A test that renders a tile without the shell
  gets a store that empties when the tile unmounts.
- The whole-map hooks `useGaugeValues`, `useGaugeAges` and `useEngineStates`
  are gone, so a component cannot take every path again by accident.
- `EquipmentTileContext` carries only items, profiles and the reporting set.
  Anything live is read by the host that needs it.
