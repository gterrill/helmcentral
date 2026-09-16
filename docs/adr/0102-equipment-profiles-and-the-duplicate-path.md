# ADR 0102: Equipment Profiles, and Keeping Units Through a Duplicate

## Status

Accepted. Extends ADR 0049 (gauge groups and the duplicate affordance, whose
find-and-replace retarget row this ADR restores), ADR 0050 (gauge zones as the
alarm source, whose unit conversion is what made the failure dangerous rather
than merely untidy), ADR 0053 (engine profiles, generalised here to equipment
profiles) and ADR 0054, which corrected ADR 0053's bundled profile for the same
class of fault this ADR corrects again.

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

Three changes had to line up for that to happen, and all three were ours:

The gauge path picker overwrote a gauge's quantity and unit on every repick,
inferring them from SignalK's `meta.units`. Almost nothing on this vessel
declares `meta.units` (eight paths out of 832), so the inference fell through to
`raw` and silently discarded a deliberate choice. The picker could not tell "the
path declares nothing, leave what is there" from "the path declares a raw
number".

The find-and-replace retarget row was removed from the gauge group dialog in
favour of a Duplicate button. Retargeting a copied group then meant repicking
every path by hand, which ran the picker once per member and wiped the units
four times over.

The bundled alternator profile shipped live warn thresholds where the rule is
that a bundled profile ships empty slots. Published manufacturer data gives
advisory ranges, not factory setpoints, and the profile's own source note said
as much while shipping a threshold of 100 anyway.

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

### A bundled profile ships slots, never setpoints

Every zone in a bundled profile carries a `null` threshold. The band, its
direction, its severity and its cited source all ship; the number does not. The
operator fills it from their own service manuals.

This is not a style preference. Under ADR 0050 a threshold in a bundled file is
a live alarm on someone's boat, set from a datasheet by someone who has never
seen the installation. `TestBundledProfilesShipNoAlarmThresholds` enforces it,
and it caught this violation. The test was committed failing, which is the only
reason the rule needed restating here.

### Picking a path does not overwrite units

`inferredQuantityForSIUnit` returns null rather than falling back to `raw`, so
"nothing to infer" and "infer a raw number" are distinguishable. The picker
replaces a gauge's quantity and unit only when the path actually declares units
that map to a known quantity, and otherwise leaves the operator's choice alone.

A fresh gauge still lands on `raw` when it picks a unit-less path, because that
is what it already was.

### The retarget row comes back, and duplicate reaches every kind again

The find-and-replace row returns to the gauge group dialog. It is the only way
to move a copied group onto another instance without running the path picker,
and after this ADR it is no longer the only safe one, but it is still the right
tool: one edit instead of one per member.

The per-widget duplicate button returns to the grid chrome for every
multi-instance kind, as ADR 0049 had it. The gauge group dialog keeps its own
Duplicate button as well, since it is more discoverable while configuring.
Duplicating a group now persists an unsaved draft alongside its copy, saves
dialog edits back to the original rather than discarding them, and offsets the
copy so it does not land underneath its source.

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

**Relaxing the bundled-threshold test.** It was the guard that caught the fault.
A test that fails only when something is wrong and gets edited when it does is
not a test.

**Keeping the old `raw` fallback behind a flag.** The picker had exactly one
correct behaviour here and the old one had no defensible use: an operator who
wants a raw number can still choose it from the quantity picker.

**Keeping the prefix rule in both the schema and Go.** Symmetry is not worth two
copies of one rule.

**Leaving duplicate as a gauge-group-only action.** Nothing about the other five
kinds made the affordance wrong for them, and the removal was never written
down as a decision.

## Consequences

A gauge group copied and retargeted keeps its units, so a zone authored in
Celsius stays a Celsius zone and the alarm it derives compares like with like.

Bundled profiles are now inert until an operator fills them in. A freshly
applied profile raises nothing, which is a deliberate trade: a profile that
alarms out of the box would be alarming on numbers nobody on the vessel chose.

The `outputVoltage`, `outputCurrent` and `outputPower` suffix aliases added
alongside the alternator profile are removed. No shipped profile used them and
this vessel publishes neither spelling, so they were a shim for a case that did
not exist.
