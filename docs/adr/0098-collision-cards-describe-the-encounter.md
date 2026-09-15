# ADR 0098: Collision Cards Describe the Encounter

## Status
Accepted

Amends ADR 0090 §2. Extends ADR 0057 (AIS collision alarms from target contexts), ADR 0058 (the collision profile follows vessel state) and ADR 0062 (marine radar targets from mayara-server).

## Context

A collision card today says `EPICURUS I - CPA WARNING` and nothing about the encounter itself: not what kind of crossing this is, which side she's on, or what the rules ask of us. Working that out means going to the chart and doing it by eye, at exactly the moment the operator least wants to be looking down.

The operator wants the card to say what kind of encounter it is and what COLREGS asks of us, in a "situation + role" form:

> Crossing, she is on our starboard bow (040° rel). We give way (Rule 15): alter to starboard, pass astern.

That is a description of a rule already in force, not a recommendation. ADR 0090 §2 rejected a standing recommendation on the card ("at a dock, raise the warning speed floor") because the right advice depends on a threshold set the operator owns and changes over time. Nothing here recomputes a threshold or suggests one. It names the rule that already applies to the geometry on screen and says which side of it we're on. See the amendment below.

Scope is the card only: no push, no banner, no kiosk, no Mate. Both target sources get a classifier. Only AIS gets a card today. ADR 0062 §7b found mayara's own danger flag untrustworthy at anchor (wave and shoreline return tracked as vessels making 20 knots) and §8 holds the radar CPA alarm until a capture taken underway proves the flag means something once the boat is moving. This plan builds and table-tests the radar branch of the classifier so the geometry and the Rule 19(d) wording exist and are pinned, but nothing radar-side renders anywhere until that alarm ships.

## Decision

### 1. A pure classifier: `classifyEncounter`

`backend/collision_colregs.go` holds `classifyEncounter(in encounterInputs) (encounter, bool)`. It touches no snapshot, no HTTP, no clock beyond what the caller hands it as plain numbers and strings, which is what makes every branch table-testable (`collision_colregs_test.go`) without a fixture or a mock.

**Geometry.** Bearings come from the caller, computed from both vessels' own positions with `bearingDeg` (`poi_providers.go`), never from the plugin's own `bearing` figure. That figure's direction has never been verified end to end (ADR 0057 only pinned its *unit*, not which way it points), and this line names a give-way side, so a wrong direction there would put "alter to starboard" on a card telling the wrong vessel to do it. The reciprocal bearing (from her back to us) is its own `bearingDeg` call, not the forward bearing plus 180: a great-circle reciprocal isn't that except by coincidence, and the two bearings are what let the classifier ask each vessel's relative-bearing question in its own reference frame (ours for `RB`, hers for `RBt`).

`relativeAngleDeg` (`assistant_geometry.go`) already folds a bearing difference to 0-180; it was built for a wind/wave direction reported as "from", and it stays exactly that. What this classifier adds beside it is `relativeBearingDeg`, the same modular-diff pattern kept unfolded at 0-360, because port and starboard are the same folded magnitude and only the signed form tells them apart.

- Overtaking (Rule 13): our relative bearing of her (`RB`), or her relative bearing of us (`RBt`), inside (112.5°, 247.5°) means the trailing vessel is coming up from more than 22.5° abaft the leading vessel's beam. Checked first, ahead of every type check: Rule 13 assigns give-way to whoever is overtaking, whatever either vessel is.
- Head-on (Rule 14): `RB` and `RBt` both within 6° of dead ahead. Applies only once both vessels have resolved to power-driven, see below; the same geometry with a sail or unknown-type pairing falls to Rule 18 or "type unknown" instead.
- Otherwise crossing: she's on our starboard side when `RB < 180°`.

The card's own label for where she is (bow, beam, or quarter) is a three-band scale local to this feature (0-45° bow, 45-135° beam, beyond that quarter), not `relativeAngleLabel`'s five-band wind/wave scale. That was tried first and rejected: `relativeAngleLabel`'s "head" band runs to 45°, which would call the plan's own worked example, a 40° relative bearing, a head bearing rather than a bow one. The folding arithmetic is shared; the words a lookout actually uses for it are not.

**Vessel typing.** Our own type comes from `navigation.state`: `sailing` gives a sailing vessel, `motoring` gives power-driven. `anchored`, `moored`, or anything else gives no role at all, only the situation, because a vessel not under way in any COLREGS sense has no give-way or stand-on duty to report. Her type comes from her AIS `navigation.state`, read verbatim against the SignalK schema's own enum (`schemas/groups/navigation.json`), never guessed from `design.aisShipType`: a sailboat's ship type (36 or 37) says nothing about whether her engine happens to be running, and that guess would be exactly the assumed default the fallback policy forbids. `TargetShipTypeID` is still carried on `encounterInputs` for completeness and is never read for role.

| AIS `navigation.state` | Her type | Role assignment |
| --- | --- | --- |
| `not under command`, `restricted manouverability`, `fishing` | priority | Rule 18: we give way, whatever we are |
| `constrained by draft` | constrained by her draught | Rule 18(d): don't impede her |
| `sailing` | sail | Rule 12 if we're also sail, Rule 18 (she stands on) if we're power |
| `motoring` | power | Rule 15/14 if we're also power, Rule 18 (we give way) if we're sail |
| anything else, or absent | unknown | situation only, plus "her type isn't transmitted, so the role can't be set" |

Two of those spellings are worth flagging because they read like COLREGS quotes and are not. The schema spells restricted manoeuvrability `restricted manouverability`, missing the second e. And it spells the constrained-by-draught status `constrained by draft`, dropping "her" entirely, which is COLREGS Rule 3(h)'s own wording, not the wire value. Both are matched against the schema's exact spelling. This boat's own traffic hasn't produced either status yet (`testdata/signalk_notifications/README.md` records the full capture, three values only: `motoring`, `anchored`, `moored`), so both are unverified against a live capture and cited to the schema instead; guessing at the "correct" spelling would just silently stop matching real traffic the day it shows up, which is the masking fallback the project forbids.

**Rule 17 and 17(c).** Stand-on wording is fixed: "hold course and speed; act if she doesn't." A power-driven stand-on vessel with the other on her own port side (the crossing case, Rule 15, that puts us stand-on) gets Rule 17(c) appended: don't alter to port for a vessel on your port side.

**Rule 12, both under sail.** Our own tack comes from the sign of our apparent wind angle (`environment.wind.angleApparent`, negative to port per its own SignalK meta, confirmed at capture). Her tack cannot be read at all: no AIS field carries a target's wind, only her course, and inferring a tack from a course plus our own wind would be a plausible-looking guess with no way to verify it. On port tack we give way; rule 12(a)(i) gives way whenever tacks differ, and even on the same tack 12(a)(ii) would only ask us to give way if we were windward, so port tack gives way safely either way.

On starboard tack the card has to rule out the one case where 12(a)(ii) could put the duty on us: same tack, and us the windward vessel. That only happens if she is to our **leeward**: if she's to windward, she'd carry the give-way duty on a shared tack regardless, so standing on is safe. Which of the two vessels is windward of the other is answered by our own wind angle's sign plus her relative bearing; it needs no data about her hull at all, unlike her tack. So on starboard tack with her to windward, we stand on; with her to leeward, the card says only the situation, because her unknown tack is exactly what would decide it.

This inverts a first draft of the same rule that read "stand on unless she's *also on starboard tack and to windward*," on the reasoning that windward is where the uncertainty bites. Working two live headings through Rule 12(a)(ii) by hand the other way: windward-of-us, same tack, and rule (ii) puts the give-way duty on whichever of the two is windward. If she's the one to windward, she carries it and we're clear to stand on regardless of what her actual tack turns out to be. It's leeward-of-us where a shared tack would make *us* windward and hand *us* the duty, which is exactly the case an AIS feed with no wind data for the target can't rule out. Flagged here in case a working sailor's read of Rule 12(a)(ii) differs from this one; the table tests (`TestClassifyEncounterBothSailingStarboardTackWindwardStandsOn`, `TestClassifyEncounterBothSailingStarboardTackLeewardGivesSituationOnly`) pin the behaviour actually shipped.

**No line at all**, rather than a wrong one, when: her SOG or our own is below 1 knot (a stationary vector is noise, not a course, the same lesson ADR 0057 §6 and ADR 0062 §7b already drew from a different input each); our heading, the bearing (which needs both positions), or her COG is missing; or TCPA is negative, meaning she's opening. TCPA itself is never recomputed, only its sign read: it's the plugin's (AIS) or mayara's (radar) own figure, already trusted whole by ADR 0057 decision 2 and ADR 0062 decision 2.

**Radar, no type, visibility unknown.** A radar contact carries a bearing, a course, a speed, and nothing else: no nav status, no ship type, no way to tell sail from power. Rules 12 through 18 all turn on a type one side or the other doesn't have here, so none of them apply; Rule 19(d), for a vessel detected only by radar, does, and it assigns no give-way or stand-on role at all, only two cautions. Forward of the beam: avoid altering to port. Abeam or abaft: avoid altering towards her. Both prefixed "If not in sight", since a radar-only contact may or may not actually be out of sight, and the card can't tell. Rule 19(d)(i) exempts a vessel we are overtaking from the port-turn caution; when our own geometry already says we're overtaking her, that caution is dropped from the line rather than printed and then contradicted.

### 2. Wired into the AIS alarm

`signalKCollisionNotifications` (`backend/collision_ais.go`) reads our own heading, SOG, state, apparent wind and position once per call, and, for each vessel whose notification is currently live (normally one to three), her position, COG, SOG, nav status and ship type through the snapshot's leaf readers, `nodeAt` for our own tree and the new `nodeAtContext` for hers, never `vesselsTree()` or a whole-context `treeFor`. A test (`TestSignalKCollisionNotificationsEncounterLineCostsNoWholeTreeCopy`) pins `snapshotWholeTreeCopies` at zero new copies for exactly this reason: the perf audit's Tier 1 #2 finding was that a whole-tree copy per alarming vessel, on a path evaluated every `activeAlarms()` call, is what actually cost time, not the number of fields read.

`alarmStatus` gains `Encounter string` (json `encounter,omitempty`), collision-only. It's computed live on every call, not frozen at the raise the way ADR 0090 §4's at-raise figures are: this line exists to guide what happens next, so it should keep following the target if she alters or we do. That's safe because `busNotificationWatcher.check` keys raise, escalate and clear on `RuleID` and severity alone (`alarm_bus_watch.go`); nothing in it reads `Encounter`, so a changing line never produces a second raise. `TestBusNotificationWatcherDoesNotReRaiseWhenOnlyTheEncounterChanges` pins this against the live fixture: a raise, then a hard course change that visibly changes the `Encounter` text, then a second `check()` that emits nothing.

### 3. The card

`ActiveAlarm.encounter?: string` (`frontend/src/hooks/use-alarms.ts`). The drawer renders it as its own line directly under `alarmConditionSentence`, above the AIS Target Prioritizer tuning link, and only when the field is present (`alarms-drawer.tsx`).

### 4. Radar branch built, not wired

`classifyEncounter` already accepts `Kind: encounterKindRadar` and is table-tested for it (forward of the beam, abeam or abaft, and the overtaking exemption). Nothing calls it with a radar target today. When `radarCollisionNotifications` ships under ADR 0062 §8, wiring it is the same one-line call `signalKCollisionNotifications` already makes, reading `radarTarget`'s own `BearingRad`, `CourseRad` and `SogKnots` (`radar_targets.go`) instead of a snapshot leaf.

## Rejected

**Trusting the plugin's own `bearing` figure.** ADR 0057 verified only its unit was degrees, not which way it points end to end. A give-way line built on an unverified direction is worse than none.

**Guessing target type from AIS ship type when nav status doesn't say.** A ship type of 36 or 37 means the hull is a sailboat, not that her engine is off right now. Rejected as the exact assumed-default the fallback policy forbids.

**Inferring her tack from her COG plus our own wind reading.** Technically possible under a uniform-wind assumption, and rejected anyway: it would produce a confident-looking role built on an assumption this classifier can't verify, on a rule (12) where getting the side wrong sends the card's advice in the wrong direction rather than just omitting it.

**A default power-driven role when type is unknown.** Considered and rejected for the same reason. The card says the type isn't transmitted and stops there.

## Amendment to ADR 0090 §2

§2 rejected a standing recommendation because the right threshold advice depends on the profile in force and rots the moment it's acted on. That reasoning doesn't reach this line: nothing here recomputes a CPA or TCPA threshold, or tells the operator to change one. It names the COLREGS rule that already governs the geometry on screen, the same way the card already names the plugin, the tier, and the figures at raise. §2's conclusion that the card carries no threshold advice stands unchanged; the encounter description is a different kind of fact and is now part of the card.

## Consequences

The card says, in the words of the rule that applies, what to do next, without the operator having to work it out from the plotter at the moment it matters most.

This line is an aid to the lookout, not a substitute for one. It restates a rule already in force; it does not stand in for Rule 5 (keep a proper lookout by all available means) or Rule 7 (use all available means to assess risk of collision). A card that says "we stand on" is not permission to stop watching what the other vessel is actually doing.

The radar branch exists and is pinned by tests before any alarm reads it, so wiring ADR 0062 §8's radar CPA alarm to a card is a one-line change with no new geometry to design at that point, only the alarm itself.
