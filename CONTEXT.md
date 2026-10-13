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
