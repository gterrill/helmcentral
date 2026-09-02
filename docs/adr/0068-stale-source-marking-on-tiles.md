# ADR 0068: Tiles Mark a Stale Source Instead of Rendering Its Last Value

## Status
Accepted

## Context

On the morning of 2 September 2026 the Solar tile read 0 W for an hour and a
half while the array was in fact making around 950 W. Nothing on the dashboard
suggested the number was wrong.

The cause was upstream of Helmcentral. The Cerbo GX lost its Ethernet lease and
reassociated to a WiFi network on a different subnet, so SignalK's Venus plugin
could no longer reach it. SignalK does not drop a path when its source stops
publishing; it keeps serving the last delta it received, timestamp and all.
Every `venus.com.victronenergy.*` path froze at 06:30:29 local, fifteen minutes
after sunrise, at which point the panels genuinely were producing nothing. The
tile faithfully rendered the last thing it was told.

That is the worst shape this failure can take. A tile showing `—` reads as
"no data" and sends the operator looking. A tile showing `0 W` reads as a
measurement, and 0 W is a plausible reading for a solar array. The same frozen
snapshot also drove the Battery & Power tile, which reported a state of charge
that was six points stale and falling.

The freshness signal was already available and already being discarded. The
SignalK payload carries a `timestamp` on every leaf, and
`readSolarController` was computing `LastUpdateAge` per controller and
plumbing it as far as the `useSolarState` hook, where nothing read it.

Anything derived from the browser clock was rejected. The vessel clock and a
tablet's clock disagree often enough that a client-side age would produce
spurious staleness on a boat where the tablet has been asleep, which is the
same false-alarm problem in a new place.

## Decision

Sources report their own age, and tiles refuse to present a value they cannot
vouch for.

- The backend computes ages against the vessel clock at sample time, so the
  browser clock is never involved. `solarStateData` and `electricalStateData`
  each carry `LastUpdateAge`, emitted as `last_update_age_s`.
- Solar takes the age of its **freshest** controller. A source is only as stale
  as its most recent update, so one dead MPPT among four does not condemn the
  array total. Individual controllers keep their own age and are marked
  separately.
- The electrical state walks the whole `electrical` subtree for the most recent
  timestamp on it, because that tile's values come from paths scattered across
  the subtree with several layers of fallback, and a feed that stops freezes all
  of them at once.
- `-1` means the age is unknown, and stays distinct from `0`. An unknown age is
  never rendered as freshly measured.
- The threshold is 120 seconds, in `frontend/src/lib/staleness.ts`. Victron and
  NMEA2000 sources publish every few seconds, so two minutes of silence is a
  stopped feed rather than a quiet one, and it sits well clear of the dashboard
  refresh interval so a slow poll cannot trip it.
- A stale tile dims its contents, marks the header with how long the source has
  been silent, and renders every reading as `—`. Blanking is the point of the
  change: the frozen `0 W` is exactly the value most likely to be read as fact.

An unknown age does not mark a tile stale. A source that has never carried a
timestamp gives no freshness signal, and flagging it forever would teach the
operator to ignore the marker, which costs more than it buys.

## Consequences

The dashboard now distinguishes "the panels are making nothing" from "this feed
died before dawn". The failure that prompted this becomes visible in the place
the operator was already looking, rather than requiring them to go and query the
GX over MQTT to find out the array was working the whole time.

Blanking a stale tile does discard the last known reading, which some operators
would rather still see. The header carries the age, so the reading is
recoverable from history if it is wanted; presenting it in the tile's own value
slots is what caused the incident.

Coverage stops at the Solar and Battery & Power tiles, the two fed by the link
that failed. `Tile` now takes `stale` and `staleLabel`, and `isStale` and
`formatDataAge` are shared, so extending this to other tiles is a matter of
plumbing an age from their source rather than new mechanism.

This does not detect the telemetry stream itself going away. Ages arrive with
each push, so a dashboard that has lost its connection to the backend holds the
last age it was given. That is a separate failure with a separate signal, and it
is not addressed here.
