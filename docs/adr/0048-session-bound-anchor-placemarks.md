# ADR 0048: Session-bound placemarks on the anchor watch

## Status

Accepted.

## Context

At anchor, the numbers that matter are not all relative to the anchor. A bombie two
boat-lengths off the starboard quarter, or the rock shelf the stern swings towards at
low water, is a hazard you want to *watch closing* — but nothing on the map measured
range to anything except the anchor and AIS targets.

Clicking the map already dropped a `MapPin`, but it was a three-second readout: a
`showTransient` call that erased itself on a timer. It answered "how far is that point"
once, then forgot. There was no way to keep a mark, and no Pin control anywhere in the
app.

Two properties turn a mark into a useful one:

- **It must be shared.** The crew member on the bow and the one at the nav station are
  looking at different devices at the same anchorage. A pin dropped on one that is
  invisible on the other is worse than no pin, because the person who dropped it assumes
  it is being watched.
- **It must expire with the anchoring.** A hazard mark is meaningful relative to *this*
  anchorage. Carrying yesterday's bombie into tonight's bay puts a fictional hazard on
  the chart, which is exactly the kind of thing that trains an operator to distrust the
  display.

## Decision

- **Placemarks are server-side state, not client state**, exposed as
  `GET/POST /api/anchor-watch/placemarks` and
  `DELETE /api/anchor-watch/placemarks/:id`. Every client polls the same set, so a pin
  dropped on the phone appears on the chart-table browser. Reads are `tierRead`, writes
  are `tierWrite`, matching the rest of the anchor-watch surface (ADR 0040).
- **The record stores position only** — `id`, `lat`, `lon`, `label`, `created_at`. Range
  and bearing are deliberately *not* persisted: each client recomputes them from its own
  live vessel fix on every render. A stored range would be frozen at the moment the pin
  was dropped, which inverts the purpose of a hazard mark.
- **Lifetime is the anchoring session.** `deleteAnchorWatch` calls `clearPlacemarks()`,
  dropping the set and its state file. Repositioning the anchor mid-session (a `POST`
  while a watch is active) deliberately *keeps* them: dragging the marker corrects where
  you believe the hook lies, it does not begin a new anchorage.
- **Restart-safe within a session, discarded across sessions.** Pins persist to
  `cache/anchor_placemarks.json` (env `ANCHOR_PLACEMARKS_FILE`) so a backend restart
  mid-session keeps them, the same way the watch itself survives. `loadAnchorPlacemarks`
  runs after `loadAnchorWatch` and *discards* the file when no watch is active — a
  session that ended while the backend was down cannot leak its hazards into the next
  one.
- **Creating a pin with no watch running is a `409`, not a silent success.** There is no
  session to bind to, so the write fails loudly rather than storing an orphan (fallback
  policy: no masking of a state problem).
- **Bounded**: 50 pins per session, 40 characters of label, so a stuck client tapping
  the map cannot grow the state file without limit.
- **Interaction**: clicking unoccupied water raises a tooltip with live range/bearing and
  a **Pin** button; clicking a placed pin reveals **Remove**. Markers all suppress the
  follow-on map click, because MapLibre listens on an element that contains every marker
  — without suppression, pressing Pin also registers as a map click and reopens the
  tooltip under the button just pressed.

### AIS ranges follow the same rule

The live-range treatment proved more useful than the click-to-reveal one it replaced, so
AIS vessels adopted it: range and bearing now sit under every vessel permanently,
recomputed from the live fix. This retired `TransientInfo` — a struct whose only purpose
was holding a *frozen* distance snapshot — in favour of a plain `selectedVesselId`. The
marker deliberately ignores the server's `range_m`, which is only as fresh as the last
AIS poll and ignores own-vessel movement between polls.

### Label styling

Persistent map labels are shadowed text on no background; only interactive controls (the
Pin tooltip, the Remove button) get a solid plate. Placemark labels follow the AIS
convention exactly, differing only in colour (fuchsia vs amber) so a hazard is
distinguishable from a vessel without reading either.

## Consequences

- One poller for the whole app: `useAnchorPlacemarks` is called once in `App.tsx` and
  passed to both the tile and the fullscreen drawer, rather than each mounting its own.
- Polling failures keep the last known pins rather than blinking them off the chart;
  *write* failures surface as toasts, because a pin the user believes they dropped and
  didn't is a hazard they would stop watching.
- Two existing AIS tests changed contract: range is now asserted visible before any
  click, and only the 3-second expansion is transient. The id-based selection guarantee
  from the shared-name regression is untouched.
- A crowded anchorage now shows a range label under every AIS vessel. If that reads as
  clutter at low zoom, `markerScale` (which already shrinks markers below zoom 14) is
  where to hide them.
