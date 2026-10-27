# ADR 0215: Tiles subscribe to the live data they show

## Status

Accepted (2026-10-27). Phases 0 to 3 are built; phases 4 to 7 are planned.
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
paths their resolved config binds, listed explicitly: the cluster's slots and
fuel capacities (`clusterPaths`), the group's gauges, the strip's lamps. The
pinned ribbon is a `LampStripHost`, so it does too. `AppShell` reads no gauge
value, age or engine state.

Engine state leaves the equipment tile context. Two hosts read it, for two
different questions:

- **Is the item silent?** An engine item that is off is known, not "Not
  reporting". `engineIdOf(item)` answers which engine an item is, for both the
  resolver and the host, so the rule lives in one place.
- **Is the card OFF?** The cluster's OFF pill follows the propulsion id its
  ring reads (`propulsion.port.revolutions` → `port`), whatever kind the item
  was given in Inventory, as it did before.

### Ages change only when what a tile shows changes

The server rounds ages to a tenth of a second. On the live stream 15 to 20 of
27 bound ages move on every one-second tick, while 7 to 10 values do. Compared
exactly, those ages would re-render every gauge, cluster, group and lamp tile
each tick, which is the cost this ADR exists to remove.

A tile uses an age for two things only: `isStale` at its default threshold of
120 seconds, and `formatDataAge` once stale, for the "Stale 2m" badge. So the
store compares each path's age on what a tile can show for it:

- `none` for an unknown age
- `fresh` for one under the stale threshold
- the `formatDataAge` label for a stale one

While that key holds, the store keeps handing out the age it last handed out,
and notifies nobody. A fresh age can therefore read lower than the true one.
Nothing shows a fresh age as a figure, so nothing shows it wrong.

The key uses the default threshold only, because every tile that reads this
store calls `isStale` without one. Passing a threshold through the hooks would
serve no tile today. A tile that needs its own threshold has to change the key
first, or its stale badge will lag by up to a whole band.

The presentational tiles, `GaugeTileView`, `EngineClusterTile`,
`GaugeGroupTile` and `LampStripTile`, still take values as props. Their tests
render them with fixed readings, and a host is the only thing that wires them
to the store.

### Alarms (phase 2)

`frontend/src/hooks/use-alarms.ts` holds one store for the `alarms` event,
the same shape as the gauge store. It exports:

- `useAlarmsSelector(select, isEqual)`: any slice, re-rendering only when
  `isEqual` calls the new slice different.
- `useAlarmList()`: the list, empty until the first event, for the banner,
  the lamps and the wall pill, where an empty list shows nothing.
- `useKnownAlarmList()` and `useWorstAlarmState()`: the list and the worst
  state, each null until the first event. The Alarms page and the CHK lamp
  read these, because they are the two surfaces that say "all clear".
- `useAisCollisionAlarms()`: vessel id to worst collision state, compared
  entry by entry, so a rule alarm coming or going leaves the AIS maps alone.
- `useAnchorDragAlarm()`: the server's drag alarm, for the anchor watch tile.
- `acknowledgeAlarm` and `silenceAlarm`: plain module functions, stable
  without `useCallback`.
- `useRetainAlarms()`: starts the store without listening.

`useAlarms()`, the whole list and both actions in one object, is gone: no
surface wants all of it, and a caller that took it would re-render on every
change.

The server sends the whole list every two seconds, changed or not. The store
compares each alarm to the one with the same rule id, field by field, and an
equal alarm keeps its object. When every alarm and the worst state match, the
snapshot keeps its reference and no listener runs. Every field counts. Unlike
gauge ages, nothing in an alarm moves on every tick without showing: the
cards show the reading, the radar figures and the live sensors, so a change
to any of them is a change on screen.

The surfaces read the store through wrappers in
`frontend/src/components/live-surfaces.tsx`: `LiveAlarmBanner`,
`LiveAlarmsDrawer`, `LiveDisplayShell`, and, for the collision colours,
`LiveNearbyVesselsTile`, `LiveAnchorWatchTile`, `LivePoiMapTile` and
`LiveAnchorWatchDrawer`. Each wrapper reads its slice and passes it as the
prop the component always took, so `AlarmBanner`, `AlarmsDrawer`,
`DisplayShell` and the tiles stay prop-driven and their tests stand. The
wrappers are memoized; the shell's remaining props are stable.
`LampStripHost`, and so the pinned ribbon, reads the list and the worst state
itself, as it reads its lamps.

`LiveDisplayShell` is not memoized: the wall's pages arrive as `children`,
an element the shell builds. An alarm tick re-renders the frame for its pill,
React receives the same `children` element, and the pages below it do not
re-render.

The shell reads no alarm. It calls `useRetainAlarms` and nothing else, for the
same reason it retains the gauge store: the Alarms page and the wall's pill
must show the list the moment they mount. The sidebar has no alarm badge, so
nothing else needed moving.

### Vessel state (phase 3)

`frontend/src/hooks/use-vessel-state.ts` holds one store for the
`vessel-state` event. It parses each tick once into `VesselStateValue`, with
the same rules the hook's thirty-odd `useState`s applied: `-1` and absent
fields read as null, and true wind never borrows from apparent. It exports:

- `useVesselStateSelector(select, isEqual)`: any slice.
- `useVesselFields(fields)`: the named fields as one object, kept while none
  of them changed. A tile passes a module constant, its own field list.
- `getVesselStateSnapshot()`: the state at this moment, for a click handler.
- `useRetainVesselState()`: starts the store without listening.
- `INITIAL_VESSEL_STATE`: every field unknown, the state before the first
  event. The tests' stand-in for this module builds on it.

`useVesselState()`, the whole state, is gone. The vessel particulars form and
the import wizard's vessel step read length and draft through
`useVesselFields`, like the tiles.

A field that shows the same as before keeps its previous value; a tick in
which every field shows the same keeps the snapshot and notifies nobody. The
three ages, depth, position and wind, follow the gauge rule: each keeps the
number last handed out while what a tile shows for it holds (`shownAge`,
moved into `lib/staleness.ts` so both stores share it). The backend gates
this event on everything but its ages and `datetime`, so a tick that arrives
almost always carries a moved age and often nothing else a tile shows. The
gust ladders compare window by window. `datetime` is not parsed at all.

The tiles read the store through wrappers in `live-surfaces.tsx`, as the
alarm surfaces do: `LiveWindTile`, `LiveDepthTideTile`, `LivePositionTile`,
`LiveClockTile`, `LiveCurrentConditionsTile`, `LiveRouteTile`,
`LiveGeneratorTile`, `LiveAutopilotTile`, `LiveAlternatorTile`,
`LiveAnchorWatchTile` and `LivePoiMapTile`, and the pages
`LiveAnchorWatchDrawer`, `LiveRadarDrawer`, `LiveForecastPage` and
`LiveSignalKDiscoveryPrompt`. The presentational components still take every
reading as a prop.

Three things moved out of the shell with the readings:

- **The place name.** The query is keyed on the endpoint alone and enabled
  by whether there is a fix, so `usePlaceName(hasFix)` now takes exactly
  that. The Position and Clock wrappers call it, sharing one query, so the
  lookup polls only while one of those tiles is on screen.
- **The clock's trip ETA.** `computeClockTripEta` needs the fix and speed,
  but only while there is a trip: an active route, or a go-to destination.
  `LiveClockTile` selects the fix and speed while there is one, and otherwise
  only whether there is a fix, so a clock at anchor does not redraw on every
  fix.
- **The fix against the anchor.** `useAnchorWatch(lat, lon)` worked out
  distance, bearing and the drag state from the fix it was given, so the
  shell needed the fix every tick. It now takes no arguments and returns the
  record, `AnchorWatchRecord`: whether a watch is set (`active`), the
  auto-raise, the planning depth. `anchorWatchAt(record, lat, lon,
  gnssCritical)` is the only place a fix is applied, returning
  `AnchorWatchResult` with distance, bearing, the drag state and the GNSS
  flag. The anchor watch tile and page call it with their own fix and keep
  the result's object while those four outputs are unchanged, so a fix that
  moves with no watch set hands the maps the same watch.

*Drop anchor here* reads the fix and depth with `getVesselStateSnapshot()`
when it is tapped. A render subscription would re-render the shell every
second to keep a handler current that runs a few times a season.

The shell selects one field, `navigationState`, for `useDisplayRotation`,
which picks the wall's pages by it. A navigation state change re-renders the
shell; nothing else in vessel state does.

Readings from the phase 4 and 5 streams stay on props from the shell for now:
the anchor watch record, its trails and placemarks, the AIS and radar inputs
to the anchor and Nearby maps, the Generator tile's power and battery, the
autopilot's state, the alternator readings, the Depth & Tide tile's tide and
trend, and the forecasts. Each wrapper moved only the vessel-state part.

### How long a store lives

Every store is built by `createTelemetryStore(subscribe, event, initial,
apply)` in `frontend/src/lib/telemetry-store.ts`. It owns the subscription,
the snapshot, the listeners, `useSelector`, `getSnapshot` and `useRetain`;
each store module supplies only its event's parser and equality rules. The
gauge store's per-path listeners are the helper's keyed listeners, so the
three stores share one lifecycle.

A store starts with its first reader and stays subscribed for the life of the
page. It never empties itself when its last reader leaves.

Phases 1 to 3 first built stores that unsubscribed and emptied when nothing
held them. A code review found the false all-clear in that: an Alarms page
mounted after a moment with no holders read an empty list and said "All
clear" until the next push. The next push was not prompt. The server's hub
seeds a new connection with every event's current payload
(`telemetryHub.Subscribe` and `seed` in `backend/vessel_state_stream.go`),
but the browser shares one connection between every store
(`use-telemetry-stream.ts`). A store coming back to a connection already open
adds a listener and is sent nothing until the event next goes out: two
seconds for alarms, and for the change-gated `vessel-state` and
`gauge-values`, not until a reading changes. Keeping the last snapshot across
a release would have been no better, because a store that had stopped
listening could not know that snapshot was still true.

So the store does not stop. While the connection is up, its snapshot is
whatever the server last said. When the connection drops, the shared stream
reconnects, the server seeds the new connection, and every store hears it.
The outage itself shows through `useTelemetryStatus`, not through the stores.
The cost is one parse per tick for an event nobody is reading, which on the
boat never happens: `AppShell` starts all three stores at boot through
`useRetainGaugeValues`, `useRetainAlarms` and `useRetainVesselState`, without
listening to any of them.

Before its first event, a store holds its `initial`, and each `initial`
claims nothing:

- **Gauge values and vessel state:** every reading null, which every tile
  shows as `--`.
- **Alarms:** `received: false`. `useKnownAlarmList` and `useWorstAlarmState`
  return null, the Alarms page says "Waiting for the alarm list" instead of
  "All clear", and the CHK lamp reads no data instead of clear. The banner
  and the wall pill show nothing until there is something to show, which is
  their behaviour with an empty list anyway.

A delayed release, keeping a store a few seconds after its last holder left,
was considered for phase 1 and rejected. The delay is a guess that has to
outlast the slowest page mount, it puts a timer into every test of the store,
and it would not have closed the gap above.

Tests reset every store, subscription included, after each test
(`resetTelemetryStoresForTest` in `src/test/setup.ts`), so one test's readings
never reach the next. The stream is passed in to `createTelemetryStore` and
read only when a store starts, so a test's mock of the stream still applies
and a module that only imports a store's types or helpers never touches it.

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
  strips whose own values changed, or whose age crossed into stale or moved
  to a new stale label. A tick that changes nothing they show re-renders
  nothing.
- An engine keying off re-renders the cluster and gauge-group hosts showing
  that engine, not every equipment host and not the shell.
- An age read from the store is exact only once stale. Code that wants the
  true age of a fresh reading cannot get it from these hooks.
- An alarms tick re-renders the surfaces whose slice changed, and a repeat of
  the same list re-renders nothing. The shell no longer renders on alarms.
- A vessel-state tick re-renders the tiles whose fields changed. A tick that
  moves only ages within the fresh band re-renders nothing, and the shell
  renders only when the navigation state changes.
- The place-name lookup runs only while a Position or Clock tile is on screen.
- The shell still re-renders on the autopilot, anchor watch and other
  streams until phases 4 and 5 land, and `renderWidget` still runs for every
  tile when it does. The gauge tiles are protected from those renders by their
  own memo: their props are now config and stable callbacks only.
- Each store stays subscribed for the life of the page once started. A
  surface mounted at any moment shows the last thing the server said, never
  an empty store's "all clear".
- Before the first alarms event, the Alarms page and the CHK lamp say the
  state is not known yet.
- The whole-map hooks `useGaugeValues`, `useGaugeAges` and `useEngineStates`
  are gone, so a component cannot take every path again by accident.
- `EquipmentTileContext` carries only items, profiles and the reporting set.
  Anything live is read by the host that needs it.
