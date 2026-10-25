# ADR 0210: Legs Within a Passage Are Joins, Not Nested Cruises

## Status

Accepted (2026-10-26). Builds on [ADR 0204](0204-passage-replay-signs.md)
(signs) and [ADR 0176](0176-helmcentral-records-its-own-track.md) (the track
log).

## Context

The operator wanted one passage, GBR Southbound, that held two legs sailed on
24 October. Merge was not on screen where they looked, and when they found it
they saw it dropped the middle place name: a merged passage keeps the first
leg's origin and the last leg's destination, and the stop between them
vanishes. So they grouped the two legs into a cruise called "Oct 24" and asked
for cruises inside cruises.

What they valued in the cruise was small and specific. Each leg was drawn in
its own colour, and the place where the legs met had a name. A merged passage
gave them neither, because a passage is one colour and one pair of ends.

## Decision

1. **Cruises stay flat.** A passage is in at most one cruise, through
   `passages.cruise_id`, as before. Nothing about grouping changes.
2. **A join is a replay sign with a `starts_leg` flag.** `replay_signs` gains
   `starts_leg INTEGER NOT NULL DEFAULT 0`, exposed as `starts_leg` in the API
   on create, patch and list. From a join's time onward the track is a new leg.
   A sign is keyed by time and carries no passage id (ADR 0204), so a join
   survives later splits, merges and trims the way any sign does. Deleting the
   sign, or clearing the flag, makes the track one colour again.
3. **Merge writes a join at every boundary.** `MergePassages(ids...)` takes two
   or more passages, sorts them by start, applies the refusals it always had
   (a hidden passage, different cruises, a passage in between), folds them into
   the earliest, and inserts one sign per boundary, all in one transaction. The
   sign sits at the earlier leg's end, with visibility `after` and the default
   `show_s`. Its title is the earlier leg's destination, else the later leg's
   origin. `POST /api/passages/merge` takes `ids` with two or more entries.
4. **A join may be untitled; nothing else may.** Validation requires a title of
   1 to 80 characters unless `starts_leg` is set. Merge creates an untitled
   join when neither leg has a name for the place. The naming worker then
   names it from the last track point at or before the join's time, with the
   same place lookup passages use. `replay_signs.name_attempted_ms` stamps each
   try, so a failed lookup retries on the passages' schedule and not on every
   scan. A title the operator types during the lookup wins: the update only
   applies while the title is still empty, the flag still set and the time
   unmoved.
5. **Colours run leg by leg in every view.** A pure helper turns passages and
   signs into legs, each with a passage id, a time range and a colour index
   that runs across every leg in the view. The passage map, the cruise map,
   the in-app replay, the legend, the exported still and the embedded player
   all draw from it. A join that falls on a passage boundary adds no leg, and
   a passage with no joins is one leg, so existing views look as they did.
   After six legs the palette wraps.
6. **Split at a join returns the names.** `SplitPassage` looks for a titled
   `starts_leg` sign within 60 seconds of the cut, and strictly inside the
   passage. If there is one (the nearest wins), the cut moves to the sign's
   exact time and both halves take its
   title as the destination of the first and the origin of the second, and
   neither is queued for naming. Without one, the old behaviour stands: the
   first half's destination clears and both ends are looked up.
7. **The Signs panel and the index carry the controls.** A **Starts a new leg**
   switch sits in the sign editor. An untitled join reads "Awaiting a name" and
   draws no label on the map. Turning the switch off on an untitled join asks
   for a name first, because the sign could not be saved without one. **Merge**
   appears beside **Save as cruise** in the passages index bulk bar and in the
   combined view, enabled for two or more neighbouring passages in the same
   cruise or in none, and disabled with the reason when they are not. The
   export waits for the signs to load before it draws, so a still never shows
   a leg as one colour.

## Rejected

- **Nested cruises, two levels deep.** It answers the question as asked and
  costs the most. Every cruise query, the list, the combined view and the
  figures that sum a cruise would need to know about depth. The operator wanted
  colours and a name, which a flag on a sign supplies.
- **A separate joins table.** It would hold a time and a name, which is a sign
  without the photo. A second table would need its own validation, its own
  editor and its own survival rules through split and merge, and the Signs
  panel already has all three.
- **Joins inferred from stops in the track.** The detector folds in stops
  shorter than an hour (a lock, a fuel dock), so the places a skipper thinks of
  as legs are often inside one passage already. A stop the boat never makes can
  still end a leg: a fish fight at 1.5 knots does not register as a stop at
  all, and the operator would have no way to say that the leg began there.
  Inference also redraws a leg whenever the detector's thresholds change. An
  operator's join stays where they put it.

## Consequences

- A merged passage keeps its legs, its colours and the name of the place where
  they met. Merge stays a plain operation on passages, and the legs live
  entirely in signs.
- A join is a sign, so it shows on the replay as a card at the boundary and
  can carry a note or a photo. A skipper who does not want the card sets its
  visibility, or deletes the sign and loses the colour break with it. The two
  are the same object by design.
- A trim that cuts a join's time out of every passage hides the sign, and with
  it the colour break, until the trim is undone (ADR 0204). Nothing deletes it.
- Statistics are unchanged and there are no per-leg figures. Time underway and
  mean speed already skip stopped stretches, so a merged passage's figures are
  what the two legs would add up to.
- An untitled join waits for the place-name source. With none selected, or no
  name near the position, it stays untitled and the editor shows "Awaiting a
  name". Nothing invents a placeholder, and the operator can type one at any
  time.
- The 24 October cruise is cleaned up by hand: merge its two passages, then
  delete the cruise.
