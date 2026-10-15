# Context

Names for the concepts the code is built around. Operator-facing words
(tile, alarm, operating mode) are set in `AGENTS.md`; this file holds the
engineering terms that name seams in the code.

## Frontend navigation

**App location**: the one value describing what the app is showing, as the
URL carries it: which panel, and that panel's view (a dashboard page, a
document, a bin, an equipment record). Parsed from and formatted to the URL by
`lib/app-location.ts`. Owned by the router; nothing else keeps a copy.
_Avoid_: route state, URL state.

**Navigation guard**: a panel's say in whether the app location may change
right now, because it holds unsaved changes or unfinished work (a stocktake, a
quick-add). A guard is registered by the panel that owns the dirty state,
decides from the move itself (where from, where to), and carries its own save,
so "Save and Continue" needs nothing from App.

**Panel route**: one panel's slice of the app location, a single union of that
panel's views. Going to a view replaces the whole slice, so sibling views
never need clearing.

## Mate

**Mate tool**: one thing the model can call during a run, defined by a single
record in `mateTools` (`backend/assistant_tools.go`): its name, its definition,
its handler, its status line, and optionally how its result becomes a card on
the reply and whether it is on offer for this run. Calls in a round run
concurrently, so a tool that writes is safe under that on its own.
_Avoid_: function, plugin, skill.

**Mate session**: one Mate surface's hold on a conversation in the browser:
which conversation is open, its messages, the question in flight and its
streaming answer (`useMateSession`). The Mate panel and the Mate sheet each
have their own, so each can show a different conversation. A session asks one
question at a time and sends the screen the operator is on with every one.
_Avoid_: chat, thread (the thread is the rendered list of messages).

## Passages

**Track point**: one recorded position with the readings taken at that moment
(speed, course, wind, gust, house state of charge, fuel rate, generator state,
fresh water). Kept by Helmcentral itself, whether or not the boat is moving. A
reading that was too old when the point was taken is absent, not copied from
an earlier point.
_Avoid_: trail point, sample, fix.

**Passage**: one leg the boat actually ran, from departing a place to coming
to rest at the next, found in the track points and then kept as a record the
operator can rename, split, merge, trim or hide. A stop of up to an hour stays
inside one passage. A leg that is only being planned is a **planned passage**,
never a Passage.
_Avoid_: trip (the trip ETA is TimeZero's next waypoint), voyage, leg, track.

**Cruise**: a named group of Passages, such as "Whitsundays with Steve and
Kim". A Passage belongs to at most one Cruise. Its span runs from the first
departure to the last arrival, and it owns the nights at anchor in between,
which is where generator hours, water made and battery figures come from.
Cruises do not nest.
_Avoid_: trip, voyage, season.
