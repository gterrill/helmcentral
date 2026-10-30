# ADR 0239: Operator-Given Positions Are Kept Through A Proposal

## Status

Accepted (2026-10-31). Builds on ADR 0158's record types and proposal cards and
on ADR 0101's `find_places`. Leaves the `find_places` provider cache unchanged.

## Context

In one conversation the operator gave Mate two positions for spots no map names:
"Thomas Island South: 020º32.676' S / 149º07.136' E" and "Brampton Island - West
Bay (020º48.978' S / 149º15.691' E)". The next day, in a new conversation, Mate
could not find "Thomas Island South". `find_places` returned `search=none`, and
Mate spent several rounds on `find_places` and `search_web` trying to derive
coordinates the operator had already typed.

The `find_places` result cache holds provider answers only. These names were
never looked up, so it had nothing to return. Conversations are not a source
Mate searches for positions, and should not be.

## Decision

### Saved places are a record type

A `saved_places` table in the business database holds a name, latitude,
longitude, an optional note and created and updated times. It registers as the
`saved_place` record type, so the operator's Apply tap and the REST routes run
the same validation: a name of up to 80 characters, a note of up to 300, a
latitude within ±90 and a longitude within ±180.

Names are unique by a match key, not by their spelling. The key lowercases the
name, drops punctuation and spacing, and reads "is" and "isl" as "island". So
"Thomas Is. South" and "Thomas Island South" are one place. Nothing looser is
matched: a near miss that returns the wrong anchorage is worse than none.

The record does not store the conversation a place came from. ADR 0158 keeps
Mate's conversations out of registered records, and a place needs no provenance
to be useful.

### Mate offers; the operator applies

`save_place` takes a name, a latitude and longitude in signed decimal degrees,
and an optional note. It writes nothing. It builds a one-operation changeset,
a create, or an update when the match key is already saved, and returns it
through the same proposal path as `propose_changes`. An update's card shows the
old and the new position, both in degrees and decimal minutes, the format the
operator typed.

The prompt tells Mate to offer `save_place` when the operator gives a position
for a named spot `find_places` did not know, and to use a result with
`search: "saved"` as given.

### `find_places` answers from saved places first

Before the provider, `find_places` looks up the query's match key. A hit returns
that one place, marked `search: "saved"`, `source: "saved"`, with the straight
line distance and bearing from the search centre like any result. The provider
is not called. The run log shows `(search=saved, results=1)`.

Without a vessel position or `near_lat`/`near_lon`, a saved place is still
found, since its own position is the answer. The result then leaves out the
distance, bearing and centre rather than report 0 nm.

### The operator can edit them

**Settings → Saved places** lists, adds, edits (name, position, note) and
deletes places. Positions are typed as degrees and decimal minutes, or decimal
degrees. The Add and Edit dialogs call `POST` and `PATCH /api/saved-places`,
which run the proposal path's validator and name rule. A name that matches
another saved place is a 409 and the dialog shows the reason.

Outside assistants now get `find_places` (ADR 0236), so they get saved places
too, from the same function. They get no `save_place`: it proposes a write, and
the outside-assistant server is read-only.

## Alternatives rejected

- **Remember every coordinate from chat silently.** Mate would keep rough or
  wrong positions, such as an example, a position it was asked about and not
  told, or a hazard. The operator would have no list to correct and no moment to
  say yes.
- **Extend the provider cache.** The cache is keyed by provider, query and a
  0.02 degree cell, expires after hours and is cleared on a settings save.
  Operator-given positions need none of that, and must outlive all of it.
- **Search past conversations for positions.** It costs rounds and tokens, and
  it finds whatever was said last, not what the operator confirmed.
- **Merge saved places with provider results.** A saved hit is the operator's
  own position for their own name. Adding the map's candidates would put the
  ambiguity back, and the provider call is slow on the boat's link.

## Consequences

- A place exists only after a tap on Apply. A dismissed card saves nothing, and
  Mate will offer again next time the operator gives the position.
- Matching is whole-name. "Thomas Island" does not find "Thomas Island South".
- A saved place takes no part in the name shown on the position tile or the
  Nearby map. Those still come from the place-names provider.
