# ADR 0215: Tiles subscribe to the live data they show

## Status

Accepted (2026-10-27). Phases 0 to 7 are built, and the migration is complete.
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
snapshot keeps its reference and no listener runs. In the store every field
counts, and the Alarms page reads it that way: its cards show the reading,
the radar figures and the live sensors.

The other surfaces compare on what they show, through
`useAlarmListComparedOn(same)`. On the live stream a rule alarm's `value`
moved on every event while its title, sentence and state held: the falling
barometer's rate, in the fifth decimal of a Pa/s figure the card shows as
"Falling 1.2 mb/hr". Compared raw, that redrew the banner every two seconds.

- **The banner** (`sameShownAlarm`): every field but the reading, and the
  reading as `alarmConditionSentence` puts it.
- **The lamp strips** (`sameLampAlarm`): rule, path, state and phase. A lamp
  shows no reading.
- **The wall's pill** (`samePillAlarm`): rule, state and phase, which is all
  it counts.
- **The anchor drag alarm**: every field but the reading. The reading is the
  distance, which the tile works out from its own fix.

`useAlarmList()` is gone; every surface that took it now says what it
compares on.

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

Phase 3 moved only the vessel-state part of each wrapper. The anchor watch
record, its trails and pins moved in phase 4, and the phase 5 streams after
it. The forecasts stay on props until phase 6.

### Anchor watch, the Nearby map and the drawers (phase 4)

The anchor watch tile and page now read the watch record, the pins and the
trails themselves. The shell hands them config, toggles and the planning
depth, nothing it polled.

**The record.** `useAnchorWatch()` is still a poll of `GET /api/anchor-watch`
through the shared query cache, but it re-renders a reader only when the
record changes. `useApiQuery` re-renders on every request, because its
snapshot carries `isFetching`: two renders per poll, every five seconds while
a watch is set and every minute while none is. The new
`useApiQuerySelector(options, select, isEqual)` in `lib/api-query.ts` shares
the entry, the fetch and the poll, and re-renders only when the selection
changes. `select` gets the data and the query's error. A select that throws,
on a body it cannot read, is called again with no data and that error, the
way `useApiQuery` turns a failed select into its error rather than a throw
during render. `use-anchor-watch.ts` builds on it:

- `parseAnchorWatch(data)`: one answer as every reader sees it, with the
  same defaults the hook applied.
- `useAnchorWatchSelector(select, isEqual)`: any slice of the record. The
  poll cadence still follows `active`.
- `useAnchorWatch()`: the whole record, kept while a poll answers with an
  equal one. The auto-raise compares by its time and reason, because every
  poll parses a new object for it.
- `setAnchorHere`, `updateAnchorRadius`, `adjustAnchor`,
  `updateRodeAndConditions`, `updatePlanningDepth` and `clearAnchor`: plain
  module functions, each landing the server's answer in the shared entry.
  The record still carries them, so the tile and page take them as before.

The shell selects five fields: `loaded`, `active`, the auto-raise's time,
and the planning depth pair. Those drive the auto-raise toast, the reset of
the what-if planning depth when a watch starts or ends, and the depth the
drawer plans against. The toast takes its baseline from the first answer,
once `loaded` is true. The record before it carries no auto-raise because
nothing has been heard yet, and a baseline taken from it made a raise from
before the page loaded toast on every reload. A poll that moves the radius, the rode or the bow offset
re-renders the tile and page, not the shell.

**The trails.** `useServerTrails` and `useAnchorTrail` are called by
`LiveAnchorWatchTile`, `LiveAnchorWatchDrawer` and `LivePoiMapTile`, so a
trail polls while a map that draws it is on screen. The shell's gating on
the page's widgets, and its `activePanel === 'anchor-watch'` clause, are
gone; nothing else read them.

Each trail is one poller at module level, `createSharedPoller` in
`lib/shared-poller.ts`. The poll runs while at least one reader is mounted
and stops when the last leaves. The buffer and its `since` cursor sit beside
it, so:

- **Two maps on one page make one request.** An anchor watch tile and a
  Nearby map share the `/api/tracks` poll, as they did when the shell held it.
- **A remount asks only for what is new.** Moving between the anchor tile
  and the anchor page, or a wall rotation back to a map, keeps the trail and
  polls from the cursor, not from the start.

A first version gave each wrapper its own hook, and with it two polls on a
shared page and a full refetch on every remount. The review sent it back.

The poller is also a store, read the way the telemetry stores are. It holds
a snapshot (`{ self, ais }` for `/api/tracks`, the point array for the
anchor trail) and the hooks return it through `useSyncExternalStore`. The
`/api/tracks` hooks return a slice each, `useSelfTrail` and `useAisTrails`,
so the Nearby map is not woken by AIS changes it does not draw, nor the
anchor map by the boat's own fixes. A poll
publishes a new snapshot only when a trail changed: points appended, the
buffer trimmed, a restarted trail emptied, or an AIS trail that differs from
the one held (point by point). A poll that brings nothing leaves the reference alone and wakes
no reader. The hooks no longer hand out getters, so the maps take the trails
as plain props and memoize their GeoJSON on them. The anchor map keys its
AIS trail line on the sorted names of the vessels it draws, not on the vessel
array, which is new on every AIS delta.

This replaced two guesses. `AnchorWatchMap` ran its own 5 s interval and
compared a point-count-and-timestamp signature to decide whether a trail had
changed. The Nearby map recomputed its own-ship trail only when `poi.features`
changed, which happens when a place fetch succeeds, about once a minute. On
the Nearby map the boat's trail therefore redrew at the place cadence, and
stopped altogether while the place fetch was backing off after Overpass
errors. Both maps now redraw when the trail poll lands points, and only then.

**The pins.** `useAnchorPlacemarks(active)` moved into the tile and page with
the record. They share one query entry, so they share one poll.

*Drop anchor here* stays in the shell, with `useCallback` over the tide, and
calls `setAnchorHere` with the fix read at the tap. The radar page needed
nothing moved: its only live input is the fix, which phase 3 already gave it.

### The remaining streams (phase 5)

Six more stores, each built on `createTelemetryStore`, each with its own
equality rule. What a rule ignores was decided from consecutive payloads
captured off the boat's server (`GET /api/stream`, 2026-10-27, at anchor),
kept as `src/test/fixtures/live-stream-payloads.ts`:

| Event | Sent | What moved tick to tick | Store compares |
| --- | --- | --- | --- |
| `autopilot` | 1 s, on change | nothing; no pilot on that server | every field, the action list item by item |
| `electrical-state` | 5 s | bank current and power, DC, solar output, datetime | every field; the feed age by `shownAge`; each alternator by reading |
| `solar-state` | 10 s | each controller's watts and share, totals, `trend_24h_total`, datetime | every shown field; ages by `shownAge`; the trend is not parsed |
| `tanks-state` | 10 s | every age, datetime, now and then a level | each tank's id, label, kind, level and alarm levels; the feed age by `shownAge` |
| `nearby-vessels` | 5 s | every vessel's age and bearing, most ranges and CPAs | every vessel field, age included; the feed age by `shownAge` |
| `radar-targets` | 2 s, on change | nothing; no radar on that server | every target field; its age by `shownAge` |

The hub already sends an event only when its payload, or its gate key,
changed (`buildAndBroadcast`, `telemetry_gate.go`). A repeat reaching the
browser is therefore a reconnect's seed, or a tick that moved only what the
gate drops: datetime, fresh ages, the solar trend. The client rules exist for
those ticks, and for the objects inside a payload that did change. An
electrical tick that moves the bank current hands the Alternator and
Generator tiles the alternator objects and role flags they already hold.

Two rules differ from the earlier stores:

- **A nearby vessel's age counts.** The list shows it on every row ("2m
  ago"), so it must stay exact; the backend's gate keeps it exact for the
  same reason. Every contact's age moves every five seconds, so the Nearby
  Vessels list and both AIS maps redraw on each tick while a contact is in
  range. That is the stream showing what changed.
- **A radar target's age does not.** Nothing renders it, and the gate bands
  it already. A target that changed only its age keeps its object, which can
  then read lower than the true age: the same trade the gauge ages make.

Each store claims nothing before its first event:

- **Autopilot:** the state is null, and the tile shows `--`. It used to start
  as an absent pilot and say "No autopilot detected" until the first event.
- **Solar:** the controller list is null, so the tile lists none and does not
  say "No controller data".
- **Electrical:** every reading and both role flags are null, which the
  Battery & Power and Generator tiles already show as dashes.
- **Tanks, nearby vessels and radar:** `received: false`, read as `loading`,
  which each tile already showed while it waited.

The alternator tile made the same false claim from vessel state:
`enginesRunning` null read as stopped, "Engines not running". It now shows
`--`. Null also means no engine is set up or none is reporting, and there the
tile can't tell either way.

The wrappers in `live-surfaces.tsx` read the stores: `LiveAutopilotTile`
(state and controls from `useAutopilot`, which no longer listens to the
stream itself), `LiveBatteryPowerTile`, `LiveAlternatorTile`,
`LiveGeneratorTile`, `LiveSolarTile`, `LiveTanksTile`,
`LiveNearbyVesselsTile` and `LiveRadarTargetsTile`, plus the AIS and radar
inputs of `LiveAnchorWatchTile`, `LiveAnchorWatchDrawer` and
`LivePoiMapTile`.

Three things moved with their data:

- **The SoC bands.** `useSocBands` runs in `LiveBatteryPowerTile`, over the
  house bank's path from the electrical store and the rules from
  `useAlarmRuleList()`. That reads the query the Alarms page edits through
  `useApiQuerySelector`, so a saved rule still recolours the bar without a
  reload, and a refetch that answers with equal rules re-renders nothing.
  The shell keeps its own `useAlarmRules` call for the Alarms page.
- **The cluster's fuel bands.** `EngineClusterHost` took the whole tank list
  from the shell to colour its fuel bars by each tank's alarm levels. It now
  reads `useTankAlarmLevels()`, which keeps its list while only levels move,
  so a tank filling does not re-resolve every cluster.
- **The roles links.** The battery and generator tiles' "choose it in the
  roles" callbacks are `useCallback`s in the shell, not arrows rebuilt on
  every render.

Two consumers needed a narrower selection than their fields:

- **The generator tile** uses the bank's state of charge and rate for one
  figure, "Battery at Finish" during a timed manual run, rounded to a whole
  percent (`projectSocAtFinish`). Both move on nearly every electrical tick.
  `LiveGeneratorTile` reads them only while a timed run is set, and keeps the
  pair it has while the rounded figure holds. Outside a timed run an
  electrical tick re-renders Battery & Power and not the generator.
- **The autopilot's controls.** The pending commands, the last command error
  and the capability probe live at module level beside the state store, not
  in each tile. A tile unmounted while Engage is in flight, by a page switch
  or a wall rotation, comes back with Engage still pending and disabled. A
  dashboard tile and the wall's tile show the same pending set, and the probe
  runs once per pilot for the session, again only if a pilot appears or goes.
  The first version kept them per mount, so a remount re-enabled Engage
  mid-command: the review caught it.

Solar controllers are matched to their previous reading by id and place in
the list, because a controller the server sends with no id parses as
`unknown`, and several of those must each meet their own row.

`useElectricalState()`, the whole snapshot, is gone: its one caller was the
shell. Tiles read `useElectricalFields(fields)`. The store does not parse
the DC current or the AC loads, which no tile shows. `useTelemetryEvent`,
the per-component subscription the autopilot hook was the last to use, is
gone with it. `useSolarState`,
`useTanksState`, `useNearbyVessels` and `useRadarTargets` keep their names
and shapes, now over the stores, because each still feeds one wrapper that
shows all of it. The shared helpers live in `lib/telemetry-store.ts`:
`keepUnchanged`, `keepUnchangedItems`, `sameShallow`, `sameJSON` and
`useStoreFields`.

The shell starts the six stores with their `useRetain*` hooks and reads none
of them. What it still subscribes to is what it draws or passes on itself:

- **`navigationState`**, which picks the wall's pages.
- **Five fields of the anchor watch**, for the auto-raise toast and the
  planning depth.
- **The polled feeds**: weather, wave, tide, forecast warnings, upper air, the
  overnight projection, the depth trend, CZone switches and route activation.
  Phase 6 moved every one of them out.

### The polled feeds (phase 6)

With the streams gone, the shell still rendered about every four seconds on
the live server. A fiber hook diff put all of it on `useApiQuery` snapshots
and the arrays built from them. Two causes, one in the cache and one in the
shell:

- **`useApiQuery` rendered twice per request.** Its snapshot carries
  `isFetching`, so a request leaving and a request landing were two renders,
  even when the answer matched the last one.
- **An equal answer was still new data.** Every poll parses a fresh object,
  so a reader of `data` saw a change on every answer.
- **The shell read every feed.** It called the forecast, tide, route,
  depth-trend, CZone, overnight and alarm-rule hooks to hand their results
  down, so each of those polls rendered the shell, and through
  `renderWidget` every tile on the page.

#### Two changes to `api-query.ts`

- **`isFetching` re-renders only the readers that ask.** A `useApiQuery`
  reader re-renders when the data, the error or the status changes, and not
  when a request leaves or lands. `isFetching` is a plain field, read from
  the entry as the reader renders, so it is current whenever the reader
  renders for any reason. A reader that has to re-render on the flips, to
  show a request in flight, passes `trackFetching: true`.
- **`structuralSharing: true` keeps equal data, at the entry.** When any
  reader of a key opts in, an answer equal by value (`sameJSON`) to the
  entry's data leaves the entry holding the object it had. Every reader,
  every `useApiQuerySelector` on the key, and `getQueryData` then see no
  change, and the deep compare runs once per answer, not once per reader.
  It is whole or nothing: unlike TanStack Query's option of the same name, a
  changed answer is handed over entire. It is off by default, because a
  reader with an effect on `data` would no longer see a refetch that changed
  nothing, and sixty-odd hooks were not audited for that. The hooks that opt
  in are the ones this phase moved, plus the page, display, ribbon and
  equipment lists the shell keeps. Their consumers were checked for effects
  keyed on the data. The two found, the roles section's grouping and the
  profiles section's first pick, are a memo and a run-once default, both
  indifferent to an equal answer.

The first version made `isFetching` a getter that marked the reader as one
that reads it. The review sent it back. The mark was sticky, so a read in a
click handler or an effect made the reader re-render on every request from
then on. It was compared against the last snapshot published, so a reader
that started reading after a flip it was spared could hold a stale value.
The test for it read the getter from outside render, where it could not
fail. An explicit option has none of those faults, and only two readers want
it:

- **`useEquipmentItem`** asks whether a read is in flight when a write lands,
  and keeps the answer in a ref the hook refreshes as it renders.
- **`useMaintenanceRules`** reads today's date from the clock as it renders
  and keys on it. A view open over midnight moves to the new day's key on its
  next render. Rendering when its own read leaves moves it there while that
  day's read is still in flight, so it shares the read; rendering only when
  the read lands would move it after, and fetch the new day twice.

The first version also compared each reader's held data with the entry's on
every notify. It is gone: with the entry keeping the object, the readers'
identity checks do the work.

`useApiQuerySelector`'s `select` now also receives the selection it returned
last time, so a reader can hand back the unchanged parts of a changed answer.
Phase 7 uses it for the page list.

A failed poll still notifies: each failure is a new `Error`, and readers keep
showing their error state, as before. The entry keeps the last good data
beside the error, and readers that showed it still do. Nothing shows default
data in place of an error.

#### What moved, and where

| Query | Interval | The shell used it for | Now read by |
| --- | --- | --- | --- |
| Route activation | 15 s | the Clock's ETA, the Route tile, the Nearby map's route layer | `LiveClockTile`, `LiveRouteTile`, `LivePoiMapTile` |
| CZone switches | 5 s, while a CZone tile is placed | the CZone tile | `LiveCZoneSwitchesTile`, polling while it is mounted |
| Depth trend | 60 s | the Depth & Tide tile | `LiveDepthTideTile` |
| Overnight projection | 900 s | the Battery & Power footer | `LiveBatteryPowerTile` |
| Alarm rules | on mount and write | the Alarms page | `LiveAlarmsDrawer` |
| Settings | on mount | the Alarms page's AIS tuning link | `LiveAlarmsDrawer`, through a selector on the shared settings entry |
| Weather forecast | 600 s | the Auto theme, Today & Now, Clock, Current Conditions, Forecast Conditions, the Forecast page | the four tiles, `LiveForecastPage`; the shell keeps sunrise and sunset |
| Weather today | 600 s | Today & Now, Current Conditions | `LiveTodayNowTile`, `LiveCurrentConditionsTile` |
| Tide today | 600 s | Depth & Tide, the anchor watch tile and page, *Drop anchor here* | `LiveDepthTideTile`, `LiveAnchorWatchTile`, `LiveAnchorWatchDrawer` |
| Forecast warnings | 600 s | the sidebar dot, the banner, the Alarms page, the Forecast page | `LiveAlarmBanner`, `LiveAlarmsDrawer`, `LiveForecastPage`; the shell keeps the dot |
| Wave forecast | 600 s | Today & Now's sea temperature, Forecast Conditions, the Forecast page | `LiveTodayNowTile`, `LiveForecastConditionsTile`, `LiveForecastPage` |
| Upper air | 21 600 s | the Forecast page | `LiveForecastPage` |

The place name had already moved in phase 3. Three wrappers are new:
`LiveTodayNowTile`, `LiveForecastConditionsTile` and `LiveCZoneSwitchesTile`.
`LiveForecastPage` builds the boat's forecast props itself and is now
memoized. *Drop anchor here* moved into `LiveAnchorWatchDrawer`, which reads
the fix and depth at the tap, as before, and the tide from the reading the
page shows.

The CZone poll used to be gated on the shell finding a CZone tile on the page.
The tile now polls while it is mounted, which is the same condition, stated
where it holds.

Each consumer passes the same interval the shell passed, so a feed shared by
two tiles polls once, at that interval. A feed whose tiles all leave the page
stops polling, and the cache keeps its answer for five minutes, so a page
switch back shows the last answer and revalidates.

#### What the shell still reads, and why

- **Sunrise and sunset**, through `useForecastSunTimes`, a selector on the
  forecast entry compared field by field. Auto mode switches the theme on
  them, and the theme lives in the shell. The Clock reads `useForecastSun`,
  the same selector with the moon phase. A failed poll keeps the last good
  forecast beside its error, and the times with it, as the shell did when it
  read `useWeatherForecast`.
- **Whether a wind bulletin is in force**, through
  `useHasActiveWindBulletin`, a boolean selector on the warnings entry. It
  draws the sidebar's Forecast dot. After a failed poll the dot stays while
  the last good warnings stand, so it agrees with the banner, which reads the
  same data. Warnings the client cannot read are no dot.
- **The pages, the displays and the ribbon**, with `structuralSharing`. The
  shell lays the page out, builds the sidebar and the page switcher from the
  list, resolves the wall's display by slug and draws the ribbon. None of them
  polls on the helm; the wall's rotation refetches the pages on each turn,
  and an equal answer now leaves the shell alone.
- **The equipment items, profiles and reporting sources**, with
  `structuralSharing`, for `EquipmentTileContext`. Nothing polls them; they
  refetch after an equipment or settings write.
- **The five anchor-watch fields and `navigationState`**, as in phases 3 and 4.

#### Tested against captured payloads

`src/test/app-polled-queries.test.tsx` answers every endpoint with a payload
captured off the boat's server on 2026-10-27, at anchor
(`fixtures/live-poll-payloads.ts`, trimmed to a few entries per list), and
drives each poll through the real cache with `invalidateQueries`. It checks
that an equal poll of each of the fourteen queries renders neither the shell
nor any tile, that a changed poll renders only the tiles that show it, and
that with every stream ticking and every poll answering equal data the shell
renders 0 times.

Two of those payloads are never equal on the boat. The tide's `datetime` and
`current_tide_height_ft` move on every poll, and an anchored boat's tide
always moves. A tide poll therefore re-renders the Depth & Tide tile and the
anchor watch, and only them: that is the tide changing.

### WidgetHost and the grid (phase 7)

Phases 1 to 6 left the shell rendering only on what it draws. When it does
render (a page switch, layout editing, the Mate sheet opening, a toast, a
config change), `renderWidget` still ran for every widget and handed each
tile fresh elements. Each tile's own memo caught most of them; the cost was
the grid and its cells rendering every time.

- **`WidgetHost`** (`components/widget-host.tsx`) replaces `renderWidget`'s
  body. It is memoized and takes the widget record, the flags (`editing`,
  `interactive`, `isDarkTheme`, `forceDark`, `distanceUnits`, `readOnly`, the
  two anchor-map toggles), the equipment context, and one `actions` object.
- **`actions` is built once.** The shell builds it with `useMemo` over
  callbacks that are themselves stable: the config opener, the panel
  navigations, the two roles links, the two toggle setters. The roles links
  are undefined on the wall, as before.
- **One config handler, bound per widget.** `configureHandlerFor`, a cache
  of one closure per widget id, is gone. The shell passes
  `openWidgetConfig(id)`, which looks the widget up at the click and opens the
  matching draft, and `WidgetHost` binds its own id with `useCallback`. The
  tiles' `onConfigure` stays a stable `() => void`.
- **`renderWidget` is a `useCallback`** over exactly those props, so it
  changes only when a flag does.
- **`DashboardBentoGrid` is memoized.** Its handlers, `handleLayoutSettle`,
  `handleRemoveWidget` and `handleDuplicateWidget`, read the page and widget
  list from refs at the call, so they never change. `effectiveWidgets` is
  keyed on the page's widget list, not the page.
- **The page list keeps unchanged records.** `useDashboardPages` reads
  through `useApiQuerySelector`, and `sharePages` puts back every widget,
  page and list equal by value to the one before. A write lands the server's
  whole page with every widget a new object; after sharing, only the widget
  that changed is new. A page whose `widgets` is null or missing (a Go nil
  slice) reads as an empty page, beside the others.
- **The handlers' refs are assigned as the shell renders**, not in an
  effect, so a stable handler never writes through the page before. No test
  reaches the gap an effect would leave: React flushes passive effects before
  it dispatches a discrete event such as a drop or a click, and `act()`
  flushes them in tests, so a handler cannot run between a commit and its
  effects in either.
- **A drop always resyncs the grid.** react-grid-layout keeps the geometry a
  drag left and goes back to its `layout` prop only when the prop changes by
  value. With the page list keeping identity, a server answer that stores
  different geometry from the drop (clamped, normalised, refused) could equal
  the layout from before the drag, and the tile would stay where it was
  dropped. The grid now shows the dropped geometry from the drop until the
  write's answer lands (`onLayoutSettle` returns the write), then the stored
  layout, so the prop changes whenever the two differ.
  `dashboard-bento-grid-settle-resync.test.tsx` checks it against a stand-in
  that resyncs as RGL does.

What a shell render now reaches:

| Shell render | Grid | Tiles |
| --- | --- | --- |
| Mate sheet, help sheet, toast, header state | skipped | none |
| Page renamed | skipped | none |
| Hero changed | renders | the hero, which moves to its own row; no other tile |
| Skin changed | renders | every `WidgetHost`, because maps follow the instrument skin |
| One widget's config saved | renders | that tile only |
| Layout editing toggled | renders | every tile, which shows its gear |
| Theme, units, read-only, an anchor-map toggle | renders | every `WidgetHost`; each tile's own memo still stops the tiles whose props did not change |
| Page switch, wall rotation | renders | the new page's tiles |

The wall display is unchanged: `LiveDisplayShell` receives the grid as
`children`, the rotation drives `activePageId`, and a rotation turn renders
the grid for the new page. The hero row, drag, resize, remove and duplicate
go through the same handlers, now stable. The config dialogs and their drafts
stay in the shell. The ribbon is a `LampStripHost` the shell renders outside
the grid, as before.

`src/test/app-widget-host.test.tsx` counts the grid's own renders by its
calls to `renderWidget`, keeping the grid's memo, and each tile's renders by
its `Tile` title. Opening the Mate sheet and renaming the page ask the grid
for no tile; saving one gauge's display re-renders that gauge alone; toggling
layout editing re-renders the gauges.

### Adding a live tile

The shell passes a tile config, flags and stable callbacks, never a reading.
A new tile follows the same four steps:

1. **The data has a store or a query.** A stream event gets a store built on
   `createTelemetryStore` in its own `hooks/use-*.ts`, with an `apply` that
   returns null for a tick that changes nothing shown, a `useRetain*` hook,
   and selectors (`useStoreFields`, or `useSelector` with an equality). A
   poll gets a hook over `useApiQuery` with `structuralSharing: true`, or
   over `useApiQuerySelector` when the tile shows a slice.
2. **A `Live*` wrapper in `components/live-surfaces.tsx`** reads the store or
   query and passes the readings as the props the presentational tile always
   took. A hook it reads returns one object while its data holds (a
   `useMemo` over the data, error and loading state), so a wrapper that
   memoizes on it holds too. It is `memo()`'d and takes from the shell only config, flags and
   stable callbacks. The presentational tile stays prop-driven and keeps its
   own tests.
3. **`WidgetHost` renders the wrapper** for the widget's id. A flag it needs
   becomes a `WidgetHost` prop; a callback becomes a member of
   `WidgetHostActions`, stable in the shell.
4. **The tests know about it.** A new store registers for the reset after
   each test: `createTelemetryStore` does that itself, and other module
   state calls `onTelemetryTestReset`, both run by
   `resetTelemetryStoresForTest` in `src/test/setup.ts`. App tests that mock
   the hook module (`vi.mock('@/hooks/use-...')`) need every export the
   shell or a wrapper calls, including new selector hooks. A render test in
   `app-tile-memoization.test.tsx` or `app-polled-queries.test.tsx` shows a
   tick or poll reaching the tile and nothing else, with payloads captured
   off the boat's server.

If the shell itself must show something from the new data, it reads a
selector with value equality on exactly that, and this ADR says why.

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
boat never happens: `AppShell` starts every store at boot through its
`useRetain*` hook (gauge values, alarms, vessel state, and the six phase 5
streams), without listening to any of them.

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
6. **Polled feeds.** Every poll the shell made for a tile or page is made by
   the tile or page, through the shared query cache. The shell keeps only
   what it draws, compared on value.
7. **`WidgetHost` and the grid.** A memoized host per widget and a memoized
   grid stop a shell render from reaching any tile whose record and flags are
   unchanged.

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
- An anchor watch poll re-renders the tile and page only when the record
  changed, and the shell only when a watch starts or ends, an auto-raise
  arrives, or the planning depth moves.
- A trail polls only while a map that draws it is on screen, once however
  many maps draw it, and a map that comes back keeps what it had.
- An alarm whose reading moves without changing what a surface says
  re-renders only the Alarms page.
- An autopilot, electrical, solar, tanks, nearby-vessels or radar tick
  re-renders only the tiles whose readings changed, and never the shell.
- A poll re-renders only the tiles and pages that show what changed, and an
  equal answer re-renders nothing. The shell renders on a poll only when the
  sunrise or sunset moves, a wind bulletin starts or ends, or the pages,
  displays, ribbon or equipment change.
- A `useApiQuery` reader no longer re-renders when a request leaves or lands,
  unless it passes `trackFetching`.
- A shell render that changes nothing about the tiles does not render the
  grid. A saved config re-renders that tile alone; layout editing, the theme
  and the units re-render every host.
- A tile's record keeps its object while the server answers it unchanged, so
  code that compares widgets by reference sees only real changes.
- Each store stays subscribed for the life of the page once started. A
  surface mounted at any moment shows the last thing the server said, never
  an empty store's "all clear".
- Before the first alarms event, the Alarms page and the CHK lamp say the
  state is not known yet.
- The whole-map hooks `useGaugeValues`, `useGaugeAges` and `useEngineStates`
  are gone, so a component cannot take every path again by accident.
- `EquipmentTileContext` carries only items, profiles and the reporting set.
  Anything live is read by the host that needs it.
