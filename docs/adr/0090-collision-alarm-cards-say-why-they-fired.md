# ADR 0090: Collision Alarm Cards Say Why They Fired

## Status
Accepted

Extends ADR 0057 (AIS collision alarms), ADR 0058 (the collision profile follows vessel state) and ADR 0088 (collision alarms colour the AIS marker).

## Context

Alongside a T-head at Hamilton Island Marina on 2026-09-11 the alarm drawer filled with `CPA WARNING` cards for boats tied up around us. The alarm log holds 100 collision raises between 17:53Z and 22:23Z on 2026-09-10, one every three minutes or so, all but four of them `warn` on a stationary neighbour: ULTRAVIOLET 25, SILK ROAD 18, ALASKA ONE 17, EPICURUS I 14. The one genuine hit in that window was FREEDOM MONARCH coming down the fairway at 4 to 5 knots with a 25 m CPA.

Nothing was misbehaving. The active profile was `harbor`, which the ADR 0058 syncer had selected correctly from `navigation.state = moored`, and its warning tier is still the plugin's shipped default: CPA under 0.5 NM, TCPA under 10 minutes, target SOG over 0.5 knots. In a marina every boat is inside 0.5 NM. For two stationary boats the CPA calculation collapses to present range (49 m for EPICURUS I) and TCPA becomes range divided by GPS jitter, so those two conditions are met more or less permanently. The only gate left is the speed floor, and a docked boat's reported SOG blips over 0.5 knots often enough to trip it every few minutes. Three seconds later the plugin's next evaluation puts the target back to `normal`, "Watching".

The fix is an edit to the harbor profile in the plugin's own webapp. The problem is that nothing on the alarm card says so. The card today carries the plugin's message (`EPICURUS I - CPA WARNING`), the path, and the sentence that this alarm was raised elsewhere and clears when its source clears it. It does not say which profile was in force, which tier tripped, what that tier's thresholds were, or what the target was doing at the moment it tripped. Working that out took a read of the plugin's source, an authenticated fetch of its profile document, and a poll of the server's vessel tree. An operator on the pontoon has none of those, and by the time they look at the nearby-vessels list the SOG blip that caused the raise has gone and the list says 0.0 knots.

ADR 0057 left "surfacing profile selection in the dashboard" as a later decision. This is that decision, widened to the question the card actually has to answer: why did this fire.

Two things this ADR is not about. The deployed build at the time (v0.18.0) predates ADR 0086, so each three-second blip also stuck in the drawer until acknowledged, which is what made the marina feel like a flood rather than a drip. Deploying fixes that, nothing here does. And the thresholds themselves stay the operator's; ADR 0058 settled that Helmcentral selects a profile and never edits a number.

## Decision

### 1. Every collision alarm card links to the plugin's page

The card for an alarm whose rule id carries the collision prefix (`notifications:navigation.closestApproach@`, ADR 0088 §2) gets one link, labelled with the plugin's own display name, "Adjust thresholds in AIS Target Prioritizer", opening `<signalk base url>/signalk-ais-target-prioritizer/` in a new tab. That page is where the profile thresholds live and the only place they can be changed, so the card points at it and nowhere else.

The URL is built in the browser from the SignalK address and port already held in settings, following the same rules as the backend's `buildSignalKURL` with one deliberate difference: an unconfigured address yields no link rather than a link to localhost. The backend defaults to localhost because it runs beside the server. The browser does not, and on a phone at the helm localhost is the wrong machine. Omitting the link is the honest outcome; a guessed host would be a broken one.

No other bus alarm gets a link. A Victron or N2K notification has no page to send anyone to, and a link that leads nowhere useful on most cards would teach the eye to skip it on the one card where it matters.

### 2. No advice on the card

A line such as "at a dock, raise the warning speed floor" was considered and rejected, for three reasons.

ADR 0058 decided the thresholds are the operator's, informed by their own waters; a standing recommendation on the card is the same decision in softer form. The right numbers depend on the profile in force, which changes with `navigation.state`; advice that is correct alongside a pontoon is wrong on the coastal profile with a real target closing. And advice rots the moment it is acted on: once the harbor profile has been edited the line is permanent clutter on a card that has to stay lean, for a single operator who read it once.

The harbor reasoning from the context above goes where ADR 0058 already keeps its anchor-profile starting point, as an amendment to that ADR's consequences, not into the UI.

### 3. The card names the profile in force and the tier that tripped

A collision card gains one line of fact, of the form:

> harbor profile · warning tier: CPA < 0.5 NM, TCPA < 10 min, SOG > 0.5 kt

Profile name and thresholds come from the plugin's profile document, the same `loadCollisionProfiles` call the ADR 0058 syncer already makes through the service account. The tier comes from the plugin's own verdict on the target, `collisionAlarmType` and `collisionAlarmState` on `navigation.closestApproach`, which `collisionFiguresFor` already reads. `collisionAlarmState` is `warning` or `danger`. `collisionAlarmType` is a comma-joined list of everything that tripped (`guard`, `cpa`, or `guard,cpa` when both did), so a `danger` that names `guard` renders the guard tier's range and speed floor, and a `danger` that does not renders the danger tier's CPA, TCPA and speed floor.

The numbers are shown in the units the operator typed into the plugin, nautical miles, minutes and knots, rather than converted to SI on the way in and back out on the card. The point of the line is to match what they will see on the edit form when they follow the link from decision 1.

The line is captured when the bus watcher raises the alarm and held on the alarm status for the life of that occurrence. A later edit to the profile does not rewrite the line on a card that is already up, because the card describes the raise that happened, not the rule that now applies. The profile document is read on the syncer's transitions and on a slow timer in between, since the operator can edit it in the plugin's webapp at any time; a raise that lands inside that window against a just-edited profile carries thresholds up to one interval old, which is accepted.

When the profile document cannot be read, because the service account lacks access or the plugin is absent, the line is omitted. The plugin's shipped defaults are not substituted: the shipped `harbor` is exactly the profile this marina says not to trust, and displaying it as if it were the one in force would be the masking fallback the project forbids. A failed read at a transition already raises the syncer's own refusal alarm (ADR 0058 §3), so the failure is not silent either way.

### 4. The card carries the target's figures at the moment of raise

The same line continues with what the target was doing when the plugin tripped, of the form:

> at raise: range 49 m, CPA 49 m, TCPA 4 min, SOG 0.7 kt

This is the half that explains a marina warning. The blip that satisfied the speed floor is gone by the plugin's next evaluation, three seconds later, so the live figures on the nearby-vessels list say 0.0 knots against a card that says `CPA WARNING`, and the two look like a contradiction. Figures frozen at the raise show the SOG that actually crossed the floor.

They are read from the snapshot's copy of the target tree at the point `busNotificationWatcher` emits the raise event: `collisionFiguresFor` for CPA, TCPA and range, and the target's `navigation.speedOverGround` for SOG. They are stored on the alarm status alongside the profile line and not refreshed afterwards, for the same reason the profile line is not: they are evidence of the raise. The alarm log's `value_at_raise` is the existing precedent for keeping the figure an alarm was judged on; four numbers do not fit one float, so the log row is left as it is for now and the figures live on the active status only.

### Rejected

**Muting the vessel in the plugin.** The plugin's webapp can mute a target, and the mute suppresses the notification entirely. It lives in memory and is lost on the next plugin restart, and it silences a named boat rather than a class of situation. It is a workaround, not a setting.

**A squelch in Helmcentral.** A second speed floor or a minimum-duration gate on our side would mean two threshold sets disagreeing about the same target, the plugin raising and Helmcentral declining to show it. ADR 0088 §1 chose one gated truth for the map for the same reason. The thresholds have one home.

**Editing the harbor profile automatically.** ADR 0058 §1, unchanged.

## Consequences

Decision 1 ships with this ADR. Decisions 3 and 4 are accepted and not yet built; the card gains the link now and the profile and at-raise line when that work lands, and this ADR is the specification for it.

The profile line makes the ADR 0058 sync visible for the first time. Until now the only way to confirm that anchoring had switched the profile was to open the plugin's webapp; the card will say `anchor` or `harbor` on its own.

The backend gains a second reason to hold the plugin's profile document, on a timer rather than only at transitions, which is one more authenticated call per interval and one more thing that fails loudly if the service account loses access. The plugin is a stated dependency of the collision alarm already (ADR 0057), so this adds no new one; with the plugin absent there are no collision alarms and nothing here renders.
