# ADR 0246: The anchor watch saves a sounded depth

## Status

Accepted (2026-11-01). Part three of three that follow
[ADR 0244](0244-mate-assesses-an-anchorage.md), and the third part that
[ADR 0245](0245-anchorage-depth-and-swing.md) left open. ADR 0245 expected the
depth to be saved when the watch starts; this decision replaces that with a
button the operator taps, for the reasons below. The planning depth and tide
pair is that of ADR 0063 and ADR 0135.

## Context

ADR 0245 gave a saved place a depth with a source, and reserved `sounded` for the
boat's own measurement. The anchor watch already holds one: the planning depth,
read from the sounder as the anchor goes down, paired with the tide height at
that moment. It is not yet a figure a saved place can hold, for two reasons.

The sounder reads from the transducer, not the surface, so the figure is the
water below the transducer. A saved place holds metres at chart datum, so it also
needs the distance from the waterline to the transducer, and the tide at the
drop taken off. The boat cannot know the first, and the second may be unknown.

Saving at the drop or the raise would also write a figure the operator has not
seen, over a place they may have charted with more care than a sounder reading
from a boat swinging over a ledge. And the operator can edit the planning depth
while the watch is up, so the figure that is right to save is the one on the
watch when they choose, not the one at the drop.

## Decision

**A setting, `anchor.transducer_depth_m`.** How far below the waterline the depth
transducer sits, in metres, in Settings, Anchor Watch. It has no default: it is
a fact about one hull, and a guess would put every saved depth out by the same
amount. It is read as unset when 0 or absent, the way `loa_m` is. A save with a
value below 0 or above 5 m, or not a number, is refused with the field named,
rather than corrected. Nothing but the save-depth figure uses it.

**One function works out the depth at datum.** `computeDatumDepth` in Go takes the
planning depth, the tide height in feet at the drop, and the transducer depth,
and returns the planning depth plus the transducer depth less the tide height in
metres. The watch's own sentinels mean unknown: a planning depth of 0 or less, a
tide height of -1 (the "not captured" value; a reading below 0 is real, and the
low-water clearance check treats it the same way), a transducer depth of 0. When
an input is unknown it returns no value and a sentence for the operator. It never
substitutes a value. The watch
responses (GET, POST and PATCH of `/api/anchor-watch`) carry `datum_depth_m`
(null when unknown) and `datum_depth_reason` (empty when known). The server works
it out, not the browser, so the arithmetic and the sentinels are tested in one
place. If the settings file cannot be read, that is the reason ("Settings could not be
read, so the transducer depth is unknown."), with the raw error logged; it is not
read as a transducer depth of zero. Only the one value is read, from the shared
settings cache, since the watch is polled constantly.

**The watch records who the planning depth came from.** `planning_depth_source`
is `sounder` when the depth arrived with the drop (the only POST that carries
one), `operator` when a PATCH wrote it (the rode planner), and empty when there
is no depth or the watch was stored before the field existed. A reposition keeps
the source. Only a sounder figure is read below the transducer, so only it gets
the transducer depth added; an operator figure is taken as depth below the
surface and needs no transducer setting. An empty source is not guessed: no
figure, and a sentence asking the operator to enter the depth again in the rode
planner. The panel says which it is: "from the sounder" or "the depth you
entered".

**The operator taps a button; nothing is saved on its own.** Anchor watch shows
the figure ("Save 4.2 m at datum") for as long as a watch is set. The target is
the place the anchorage reader (`anchorage_depth.go`) would read: the nearest
saved place within 0.1 nm (185.2 m) of the anchor that has a depth. With none
that has a depth, it is the nearest place in range. Saving to the place the
reader uses means the next assessment sees the new figure, rather than an older
depth on a farther place. The button reads "Save depth to" and the place's name. With no
place in range it reads "Save as a saved place" and asks for a name, prefilled
from the watch's pinned place name. Either way a confirm step follows. Where the
place holds a depth, it shows the old figure beside the new one with their
sources and dates. When the figure cannot be worked out, the reason stands where
the button would be. When the saved places or the boat's time zone are not
available, a sentence says so and there is no button.

**The date is the drop date at the boat.** The day the anchor went down
(`set_at`), in the boat's time zone as ADR 0230 sets (the vessel state's zone),
not the day of the tap, since the depth is that of the drop. With no zone known
there is no date, so no save.

**The writes use the existing saved places routes.** A new place is a POST and an
existing one a PATCH with the three depth fields. The validator already accepted
`sounded`, and a test now covers it through both routes. The nearest place is
found in the browser from the list the Saved places screen already reads, with
the haversine helper the anchor code uses. A server endpoint would add a route
and a second copy of the 0.1 nm rule for a list of a few dozen places.

## Consequences

- A boat with no transducer depth set sees why the button is missing, and where
  to set it. The figure is not guessed.
- The figure is as old as the planning pair. An operator who edits the planning
  depth saves the edited figure, stamped with the drop date, and read as depth
  below the surface. The operator chose that figure, and the confirm step shows it.
- The transducer depth applies to Save depth only. The rode planner and the
  low-water clearance check still treat a sounder reading as the depth below the
  surface. The setting's hint says so. Follow-up: have those two use the
  transducer depth, which changes the rode and clearance numbers and needs its own
  decision.
- The figure sent is the one shown, rounded to a centimetre, so imperial display
  and the saved value agree.
- A sounded figure replaces a charted one, as any new depth does. The confirm
  step is where the operator can decide not to.
- A place on the other side of a headland but within 0.1 nm is offered as the
  target. The name is on the button, and the confirm step names what it replaces.

## Rejected

- **Saving at the drop or the raise.** It writes a figure the operator has not
  seen, and misses a planning depth edited afterwards.
- **A default transducer depth.** It would be a boat-specific value shipped as a
  guess.
- **Taking the live reading at the tap.** The boat has swung and the tide has
  moved; the pair stored with the watch is the contemporaneous one.
- **Working out the datum depth in the browser.** The sentinels and the unit
  conversion would be tested in a second place for no gain.
- **A server route for the nearest saved place.** See above.
