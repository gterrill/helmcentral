# ADR 0102: Equipment Profiles, and Keeping Units Through a Duplicate

## Status

Accepted. Extends ADR 0049 (gauge groups and the duplicate affordance, restored
here for every widget kind), ADR 0050 (gauge zones as the alarm source, whose
unit conversion is what made the failure dangerous rather than merely untidy)
and ADR 0053 (engine profiles, generalised here to equipment profiles). Reverses
ADR 0053's rule that a bundled profile carries no alarm thresholds, replacing it
with a requirement that a bundled threshold cite its source. References ADR 0054,
which corrected ADR 0053's bundled profile for a neighbouring fault.

## Context

ADR 0053 shipped engine profiles: drop-in JSON files that configure a gauge
group's paths, scales and operating bands from manufacturer data. ADR 0050 made
a gauge's zones the alarm source, so a warn band authored in the profile becomes
a live alarm rule with no second system to keep in sync.

The two together have a sharp edge. A zone is authored in the gauge's display
unit and the alarm engine reads SI, so the derived rule converts the threshold
through the gauge's declared quantity. If the declared quantity is wrong, the
conversion is wrong, and nothing downstream can tell.

That is what happened. Building a Starboard Alternator tile by copying the Port
one produced a gauge group carrying `quantity: raw, unit: raw` on every member
while keeping warn bands authored in Celsius. The derived rule converted a
threshold of 100 through the identity unit and compared it against a path
reporting 329.15 Kelvin. The alarm raised at 56 C and could not clear, because
the reading would have had to fall to 97 K.

One change caused that, and it was ours. The threshold itself was never the
problem: the Port Alternator tile carries the same 100 C warn band, sat at 58 C
while Starboard was alarming at 57 C, and raised nothing. The difference was
entirely the declared quantity.

The gauge path picker overwrote a gauge's quantity and unit on every repick,
inferring them from SignalK's `meta.units`. Almost nothing on this vessel
declares `meta.units` (eight paths out of 832), so the inference fell through to
`raw` and silently discarded a deliberate choice. The picker could not tell "the
path declares nothing, leave what is there" from "the path declares a raw
number".

What made it easy to hit was that the find-and-replace retarget row had been
removed from the gauge group dialog in favour of a Duplicate button. Retargeting
a copied group then meant repicking every path by hand, which ran the picker
once per member and wiped the units four times over.

Separately, and wrongly, the bundled alternator profile's thresholds were
blamed for the alarm and nulled. They are the Prestolite specification's own
figures and they were correct all along.

The same commit range also removed the per-widget duplicate button from the grid
chrome for every widget kind, which cost clusters, standalone gauges, embeds,
POI maps and lamp strips an affordance they had under ADR 0049, with the
replacement reaching only gauge groups.

## Decision

### Profiles cover equipment, not only engines

`kind` is a first-class field taking `engine`, `alternator` or `generator`, and
`schema_version` pins the document at 1. A JSON Schema validates every profile
on load and on write, and the three kinds differ in the path suffixes they may
carry: generator profiles hang off `phase.` or `total.`, engine and alternator
profiles may not.

Profiles are readable, writable, uploadable and deletable over
`/api/equipment-profiles`, rather than being drop-in files an operator edits on
the box. `/api/engine-profiles` stays, filtered to `kind: engine`, so nothing
that already speaks it breaks.

The prefix rule lives in Go and not in the schema. `validateEngineProfile` is
reached on every path, including directly from tests; the schema is only reached
through the three entry points that all go on to call it. One rule in two places
drifts, and the copy that is always reached is the one worth keeping.

### A bundled threshold has to say where it came from

ADR 0053 ruled that bundled profiles ship `null` alarm thresholds, enforced by a
test. Its reasoning was specific and sound: Cummins does not publish QSB 6.7
setpoints, so any number in that file would have been a guess off a forum, and
under ADR 0050 a guess in a bundled file becomes a live alarm on someone's boat.

That reasoning was then generalised into a ban on all bundled thresholds, which
is broader than the argument supports. The Prestolite Electric / Leece-Neville
specification does publish figures for the Cummins 5285862, and refusing to ship
them helps nobody. A cited setpoint and a guessed one are different things, and
the old rule could not tell them apart because it only looked at whether a
number was present.

The rule is now about provenance. A warn or alarm zone may carry a threshold
only if the zone cites its `source`. An uncited threshold still fails the test,
and a `null` threshold is still a legitimate slot for the case ADR 0053
described, which is why the QSB and Onan profiles are unchanged. This supersedes
ADR 0053's blanket rule; the rest of that ADR stands.

### Picking a path does not overwrite units

`inferredQuantityForSIUnit` returns null rather than falling back to `raw`, so
"nothing to infer" and "infer a raw number" are distinguishable. The picker
replaces a gauge's quantity and unit only when the path actually declares units
that map to a known quantity, and otherwise leaves the operator's choice alone.

A fresh gauge still lands on `raw` when it picks a unit-less path, because that
is what it already was.

### Duplicate reaches every kind again, and stays Save As

The per-widget duplicate button returns to the grid chrome for every
multi-instance kind, as ADR 0049 had it. Clusters, standalone gauges, embeds,
Nearby maps and lamp strips had lost it with no decision recorded, and the
replacement button reached only gauge groups.

Duplicate applies the dialog's edits to the copy and leaves the original alone.
That is Save As, it is deliberate, and it is how the operator retargets a tile:
open the group, change what differs, press Duplicate instead of Save. The copy
is now placed below everything else rather than inheriting the source's
coordinates, which was the only part of that flow that needed fixing.

The find-and-replace retarget row stays removed. It belongs beside the Duplicate
button and revealed only once Duplicate is pressed, and no layout was found that
did not make the dialog resize as the preview grew a line per matching path.
`rewriteGaugePaths` is kept with its tests and no caller, against that layout
being solved later.

### Emphasis is declared, not spelled

A gauge group sized its members by testing the path and label against
`/temperature/` and rendering a match a step larger. Size then depended on
spelling: renaming a label from "Temp" to "Coolant" shrank the gauge, and
`exhaustTemperature` stayed small beside a sibling that happened to match.
`hero` already says which member should stand out, so it is the only thing that
changes a member's size now.

### Nothing selected is a state, not an empty string

The equipment settings screen used `''` to mean "no profile is selected" and
then fell back to the first profile whenever a lookup missed. Those are two
different things and the fallback swallowed both. Starting a new profile
cleared the selection, the lookup missed, the fallback substituted the first
profile, and a sync effect overwrote the blank draft and left edit mode. The
new-profile form never appeared unless the first profile happened to already be
selected. An upload hit the same path, because a just-created id does not
appear in the list until the reload lands.

The selection is now `string | null`, the lookup returns nothing rather than
substituting, and picking a default happens in one place: on first load, when
nothing has been chosen and no draft is open.

### Destructive actions confirm, and failures say so

Deleting a profile removes a file from disk and used to happen on one click.
It now asks, naming the profile.

The profile hooks turned a failed fetch into an empty list, so a broken backend
and an empty profiles directory produced the same "nothing installed" message.
Both hooks now carry the error, and both dialogs that consume them show it.

## What was rejected

**Deleting the bundled-threshold test.** Loosening it to check provenance rather
than absence is not the same as removing it. An uncited number in a bundled file
is still exactly the failure ADR 0053 was written to prevent, and the test still
catches it.

**Blaming the threshold for the alarm.** This was the first diagnosis and it was
wrong, which is worth recording because it nearly cost a correct profile its
figures. The Port tile carried the identical band and stayed quiet at a higher
temperature. Whenever a threshold looks absurd against a reading, check the
declared quantity before touching the number.

**Keeping the old `raw` fallback behind a flag.** The picker had exactly one
correct behaviour here and the old one had no defensible use: an operator who
wants a raw number can still choose it from the quantity picker.

**Keeping the prefix rule in both the schema and Go.** Symmetry is not worth two
copies of one rule.

**Leaving duplicate as a gauge-group-only action.** Nothing about the other five
kinds made the affordance wrong for them, and the removal was never written
down as a decision.

**Making Duplicate save the original too.** Briefly done and reverted. Duplicate
is Save As: the whole point is to edit the fields and take the result somewhere
new without disturbing what you opened.

**Restoring the retarget row where it used to sit.** It works, and it was
rejected on layout grounds rather than on function. Putting it back above the
gauge list reintroduces the resizing dialog that got it removed.

## Consequences

A gauge group copied and retargeted keeps its units, so a zone authored in
Celsius stays a Celsius zone and the alarm it derives compares like with like.

A bundled profile can now arrive with working alarms where the manufacturer
publishes the figures, as the alternator does, and still ships empty slots where
they do not, as both Cummins engines do. The operator reads the `source` beside
each number to know which they have.

Retargeting a duplicated tile means repicking each path until the layout problem
with the find-and-replace row is solved. That is safe now, since the picker
leaves an existing quantity alone, but it is still one edit per member.

The `outputVoltage`, `outputCurrent` and `outputPower` suffix aliases added
alongside the alternator profile are removed. No shipped profile used them and
this vessel publishes neither spelling, so they were a shim for a case that did
not exist.
