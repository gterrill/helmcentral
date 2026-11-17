# ADR 0291: Pins without an anchor

## Status

Accepted (2026-11-18). Supersedes two points of
[ADR 0048](0048-session-bound-anchor-placemarks.md): a pin belongs to one anchoring
session, and creating a pin with no watch running is a `409`. Supersedes the line in
[ADR 0059](0059-always-on-anchor-map-drop-raise-and-scope-method.md) §1 that the map
offers no pin without an anchor.

## Context

The Anchor Watch map shows range and bearing from the boat to every pin. Anchoring is
not the only time that is useful. Approaching a narrow channel, the skipper wants to mark
each side and read off how wide it is before committing the boat. ADR 0048 tied pins to
the anchoring session so that yesterday's bombie could not turn up in tonight's bay, and
so the server refused a pin with no watch to bind it to. That refusal left the map
silent in exactly the state where the operator was still choosing a spot.

## Decision

- **A pin can be created, listed and removed with no watch running.** The `409` for "no
  active anchor watch" is gone. The limit of 50, the fields and the 40 character label
  are unchanged.
- **Pins outlive a Drop.** Drop never touched them, and still doesn't, so pins placed
  before anchoring carry into the anchoring that follows. Raise still clears every pin
  and removes the state file.
- **Pins have no expiry.** With no anchoring to end them, a pin stays until the operator
  removes it or raises an anchor. The cost is a forgotten pin that lingers. We accept it
  because a timer would delete a mark the operator placed deliberately, and the Raise
  confirmation already says it clears every pin.
- **A restart keeps them.** `loadAnchorPlacemarks` restores the file whether or not a
  watch is active. It no longer discards the file when none is.
- **Every client shows them in every state.** The map accepts the tap that raises the
  Pin tooltip with or without an anchor, and the pins are polled whether or not a watch
  is set, so the tile, the page and the wall all show them. Tapping the boat's own icon
  still places no pin.
- **Nothing else changes while unanchored.** No swing circle, anchor marker, rode planner
  link or distance between pins appears. Mate, Nearby and passage plans still don't see
  pins.

## Consequences

- A pin dropped on a passage and never removed stays on the map until the next Raise. That
  is the operator's call, made visible on the map rather than hidden by a timer.
- The reason in ADR 0048 for expiry, a fictional hazard carried to a new bay, now rests
  on Raise alone. An operator who anchors in a new bay without raising the old watch keeps
  the old pins, as before.
