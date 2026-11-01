# ADR 0245: Anchorage depth, swing and clearance

## Status

Accepted (2026-11-01). Part two of three that follow
[ADR 0244](0244-mate-assesses-an-anchorage.md), which left depth out of
`anchorage_details`. The third part, the anchor watch saving a sounded depth, is
under Consequences. The draft check pattern is that of
[ADR 0234](0234-an-answer-about-wind-and-course-needs-the-passage-check.md) and
[ADR 0243](0243-an-anchorage-shelter-claim-needs-the-shoreline-check.md). The
scope maths is the rode planner of ADR 0047, whose clearance warning is ADR 0135.

## Context

ADR 0244 gave the tool no depth because a number with no check behind it invites
a confident wrong answer. Asked about an anchorage, Mate still volunteered one
("about 6 m on sand"), from memory, and the shelter, forecast and tide it had
just read said nothing about it. Two things make a depth worth having: whether
the keel clears the bottom at the lowest water of the stay, and whether the
chain aboard reaches at the highest. Both turn on the tide the tool already
reads, so the missing piece was a depth Mate can stand behind.

The boat has no sounding of an anchorage it has not reached. A figure can only
come from the operator, who read it off a chart or was told it, or from the
boat's own sounder once the hook is down. The first is available now. The second
is the anchor watch's to write, so the stored value has to carry where it came
from.

The rode planner's maths lives in TypeScript (`rode-plan.ts`, `catenary.ts`),
which the browser runs against the live tide. Mate's tool runs on the server
against a forecast tide, so it needs the same arithmetic in Go, for the part of
it that a stay not yet begun can feed.

## Decision

**A saved place stores one depth.** `saved_places` gains `depth_m` (nullable,
metres at chart datum, a drying height negative, bounded -10 to 1000),
`depth_source` (`charted` or `sounded`, required with a depth) and `depth_date`
(`YYYY-MM-DD`, the day the figure was taken or entered). The three are set
together or not at all. A new depth replaces the old one with its own source and
date, so a figure never inherits another's provenance, and a null `depth_m`
clears all three. The columns are added by `ALTER TABLE ... ADD COLUMN` guarded
by `addColumnIfMissing`, the way watches did it: the operator is one person with
no installed base, so there is nothing to migrate beyond the in-place column.
The record type, the REST handlers and the validator all go through one
`applySavedPlaceDepthFields` and `validateSavedPlaceDepth`. `sounded` is valid
now and written only by the anchor watch, later.

**Saved places get a depth the operator can type.** The Saved places dialog has a
depth box in the operator's distance units. A figure typed there is saved as
`charted`, dated today at the boat (the zone the vessel state reports, ADR 0230).
Before the boat has reported a zone the date is unknown, and Save is off with
that reason, since a depth is never dated by a guess. Where the place already has
a depth, and a different one is typed, the dialog shows the old figure and says it
will be replaced. An emptied box says what it clears.

**`save_place` can carry a depth.** A `depth_m` argument is for a figure the
operator stated in the conversation, never one from Mate's knowledge. It becomes
`charted`, dated with the operator's own calendar date from the message request
(the `today` field), and the proposal card shows the old and new depth on one
line each ("4.0 m at datum (charted 2026-11-01)"). Without a known date the tool
fails rather than date the figure from the server clock. The same figure is not a
change.

**`anchorage_details` takes an optional `depth_m`.** The tool's description
limits it to a figure the operator stated in this conversation. Without it, the
tool finds the nearest saved place within 0.1 nm (185.2 m) that has a depth and
uses it, naming it. A nearer place with no depth does not shadow a farther one
that has: a depth worth using is better than the nearest place's silence. When
places are in range and none has a depth, the note names the nearest and says it
has none saved. The argument wins over a saved
place. The result gains a `depth` section that says which figure was used and
where it came from ("4.0 m at datum, from you", "from saved place Butterfly Bay
(charted 2026-11-01)", or "(sounded ...)"), or that the depth is unknown. The
section is the usual kind: if the saved places cannot be read it carries an
`error`, and the checks that need a depth are skipped naming it.

**Three checks run when a depth is known,** each `ok`, a named failure, or
`skipped` with a reason, and none is ever reported as passed when skipped.

- *Clearance at the lowest water.* Depth plus the lowest water height in the
  stay, less the draft, against `anchor.min_clearance_at_low_m`. The clearance is judged rounded to
  0.1 m, as it is shown, so 0.46 m reads as 0.5 m and meets a 0.5 m wanted. The draft is
  SignalK's `design.draft.maximum` and is never assumed: without it the row says
  the boat's draft is not reported.
- *Rode at the highest water.* Depth plus the highest water height gives the
  planning depth. The rode is the ratio method's: the depth from the bow roller
  times 5, or times 7 from 20 knots of wind, compared with `chain_onboard_m` as
  "ok" or "short by X m". The wind is the strongest gust (or wind, where there
  is no gust) in any hourly forecast row from arrival to leaving, not only the
  three-hourly rows the card keeps, rounded up to the planner's wind band as the
  planner does. When the forecast ends before the stay does, the row is still
  worked out, and says (`wind_checked_until`) that the wind was only checked up
  to then. A chain
  aboard of zero or unset skips the row and names the setting; the bow roller
  height is checked once, by the ratio method, which says it is not configured. When the operator's
  `scope_method` is catenary the row is skipped, and says why (below).
- *Swing.* The rode plus `gps_from_bow_m` plus the boat's length, which is what
  the planner's swing circle is (`computeSwingRadiusM`). When the rode check is
  short, the rode is the chain aboard, not the rode needed: the swing is for all
  the chain out (`all_chain_out`), which is the circle the boat can actually
  make, and the line says it is short of what is needed. The length is
  `anchor.loa_m` when set, else the boat's length overall as the marina check
  reads it (SignalK, then the stored vessel particulars), else the row is
  skipped. With no rode there is no swing, so a skipped rode skips the swing and
  gives the same reason.

The lowest and highest water are those of the stay: the tide section's
extremes inside it, plus the interpolated heights at arrival and at leaving
when the prediction covers them. A stay wholly on a rising or falling tide has no
extreme in it, and its lowest water is then the height at one end. If the
prediction ends before the stay does, the text says that a lower or higher water
may follow. If the tide section errored, the checks that need it are skipped
with that error.

**A scope method of catenary skips the rode check, and the swing with it.** The
catenary method needs two inputs that nothing supplies for a stay not yet begun:
the sea state and the seabed. The anchor watch holds them for a set anchor.
Helmcentral has neither for an anchorage the boat has not reached, so the check
says that, in words for the operator, and points to the ratio method. Nothing is
assumed in its place. The operator chose this on seeing the alternatives, below.

**The scope maths is ported to Go and pinned by shared vectors.**
`anchorage_rode.go` ports the ratio method, `resolveLoaM`, `computeSwingRadiusM`
and the wind bands, and
`backend/testdata/rode_plan_vectors.json` holds inputs and expected outputs.
Both suites read the file: `anchorage_rode_test.go` and
`rode-plan.vectors.test.ts`. The expected values were generated from the
TypeScript, which is the reference, and the file says so. The Go works from a
planning depth already corrected for the tide, so it ports none of the
tide-correction arithmetic in feet, and the vectors reach the TypeScript at the
same seam by giving a depth with no tide stamp. The file lives under
`backend/testdata` because the frontend already reads the card golden from there.
Cases cover the ratio switch at 20 knots, shallow and deep water, a light wind,
no wind data, no bow roller height, an unknown boat length, and each way the boat
length is resolved.

**The card gains a `depth` section,** placed after the local features and before
the map, with every key present. The Go golden file carries a depth, and the
strict frontend decoder checks it: a known depth is a number, a skipped check
has a reason, the statuses are from a closed set. The card words each line from
the figures in the operator's distance units. The model's text is metres, and it
stays in the model's copy: the card carries no per-check `text` and no `label`,
so one place owns the wording of each line. The map
draws the swing circle, a 64-point ring round the anchorage, when it was
computed.

**The draft check flags an unsourced depth.** ADR 0243's pattern: a draft that,
in one sentence, gives a figure with a length unit, a depth word (depth, deep,
shoal, shallow, sounding, fathoms, of water, under the keel) and anchorage
wording (anchor, bay, cove, mooring, holding, swing) is sent back once if the run
has no source. A source is an `anchorage_details` result whose depth is known, or
any tool call that ran carrying a `depth_m` argument (the operator's figure being
passed on or offered for saving). The checker cannot see the conversation, so it
trusts the argument, and the tool's description is what restricts it. The
correction says to call the tool with the operator's figure or to say the depth is
unknown. The check is armed under the same conditions as the shelter check, since
the correction points at the same tool. A bare "charted" is not a depth word, so
a mooring "charted at 12 m" is a vessel size and passes. The common phrasing
"anchored in 5 m" has no depth word, so a sentence with "anchor", "anchored" or
"anchoring" followed directly by "in" and a figure with a length unit also counts.
Nothing is matched across sentences.

A card stored before the depth checks existed (anchorage cards from ADR 0244
have no `depth` key) decodes with no depth and draws no depth section. A depth
that is present is still decoded strictly.

## Consequences

- A boat gets depth checks only after the operator has given a depth, once, for
  an anchorage. Everywhere else the card says the depth is unknown. That is the
  intent, and the gap fills as saved places gain depths.
- The tide height is taken as above the datum the depth is given at. The tide
  providers do not all state their datum, so a chart depth on a different datum
  than the tide station's is a stated assumption on the card. It is not checked.
- An operator who set the catenary method gets no rode or swing check from Mate
  until they set the ratio method. The ratio method's rode can differ from what
  the anchor watch's own planner gives once the hook is down, since that planner
  is given the sea state and the seabed.
- The rode is a slant length, used as a radius, as the planner uses it. The
  swing is therefore a little generous, which is the safe side.
- One sourced depth anywhere in the run disarms the depth check for the whole
  reply. The checker cannot tell which anchorage a figure in the text is about,
  so a reply about two anchorages, one sourced, can state an unsourced depth for
  the other without being sent back. This is a known limit.
- A second Go implementation of the arithmetic exists. The shared vectors catch a
  drift in either, but a new behaviour in the TypeScript has to be added to the
  vectors, regenerated from the TypeScript, before the Go can follow.
- The guard can send back a reply that restates a depth the operator gave
  without a tool call. The correction tells Mate to pass the figure on, so the
  second answer is sourced. A depth stated in the same sentence as an anchorage
  word but about something else (a channel at a marina entrance has no anchorage
  word and passes) is the cost of a narrow pattern.
- The third part is
  [ADR 0246](0246-anchor-watch-saves-a-sounded-depth.md): the anchor watch
  saves its depth to a saved place as `sounded`, on the operator's tap, using a
  transducer depth setting so that a depth below the transducer becomes a depth
  at datum. It replaces a charted figure, as any new depth does.

## Rejected

- **A depth the model may give from its own knowledge, flagged as general
  knowledge.** The point of the change is that a number in a reply has a source.
- **Several depths per place, kept as a history.** The operator wanted one figure
  that is current, with its date. A history is a later feature if it is ever
  wanted.
- **Assuming a draft.** A check that passes on a draft nobody reported is worse
  than one that says it was skipped.
- **Assuming a calm sea on sand for the catenary method.** It would give a
  figure that reads as the operator's own method but is the gentlest case, so a
  rode that looks enough could be short on a soft seabed in a seaway. A skipped
  check is more honest than a flattering one.
- **Deriving the sea state from the forecast waves and the seabed from the
  OpenStreetMap seabed areas.** Considered and not taken. The wave height at the
  grid point is not the sea in the anchorage, the seabed areas are sparse and
  often absent, and each would need a rule for what to do when it is missing,
  which is the assumption again with more machinery.
- **Reading the tide height at the datum from the provider.** The providers do not
  all say, and a guess would be silent.
- **Calling the web or the TypeScript from the server for the maths.** The server
  has no browser, and a second runtime for a few lines of arithmetic is more to
  keep than a tested port.
- **A fixed shared constants file for the vectors' expected values written by
  hand.** The reference generates them, so a hand-typed figure cannot encode a
  mistake the same way twice.
