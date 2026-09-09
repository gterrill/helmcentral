# ADR 0088: Collision Alarms Colour the AIS Marker

## Status
Accepted

Extends ADR 0057 (AIS collision alarms) and ADR 0062 (radar targets).

## Context

ADR 0057 got the AIS collision-proximity alarm onto the alarm engine: a target's `notifications.navigation.closestApproach` now surfaces as a rule-shaped alarm, gated against a stale position and stripped of the self-context sensor fault it used to carry. What that ADR deliberately left open was how the alarm reaches the display the operator actually watches when a target is closing. The alarm card names the vessel, which is enough to raise the alert. It is not enough to point at the target on the chart, and the anchor-watch map is where that check happens, against the plotter and against the water.

Every AIS contact on that map draws the same amber circle regardless of what it is doing. ADR 0062 gave radar its own answer to the same problem: a dangerous target's triangle turns red, with a ring, so the eye finds it without reading a number first. AIS had nothing equivalent. A target three minutes from a CPA breach looked exactly like a target the far side of the anchorage doing nothing.

## Decision

### 1. Read the alarm list, not the plugin's own field

Each nearby-vessel record already carries the AIS target-prioritizer's own `collision_alarm_state`, in the plugin's `warning`/`danger` vocabulary rather than SignalK's notification states. The marker does not use it. That field is ungated: it carries none of ADR 0057 §6's position-freshness check and none of the live-state check, and the plugin writes it on state change rather than continuously, so it freezes at whatever it last said. The nearby-vessels list keeps a quiet target for ten minutes where the alarm gate allows five, so for the second five minutes that frozen field would still be on the wire with no alarm behind it. Colouring the marker from it directly would put a red dot on a target the alarm card is not showing, or leave a target red long after the alarm cleared. The two displays disagreeing about the same target is worse than either display being wrong the same way, so there is exactly one gated truth here and the map reads it: the active alarm list, the same list the card renders from.

### 2. Match by SignalK vessel id, not by name

The backend's collision-alarm rule id carries the SignalK context id after an `@` (`notifications:navigation.closestApproach@urn:mrn:imo:mmsi:512213970`), and that id is the same string the nearby-vessels payload already publishes as each target's `id`. The map keys off that, plain string equality, nothing fuzzier. Two boats sharing a display name is not a hypothetical on a coast with a hundred fishing vessels named after the same three saints, and matching on name would occasionally colour the wrong marker.

### 3. One red for every live state, a ring at alarm and above

The marker turns the same red as radar's dangerous triangle for any live state on the alarm list, `alert` through `emergency`, and gains a ring once the state reaches `alarm` or `emergency`. Shape still carries identity: a red circle is still unmistakably an AIS contact, a red triangle still unmistakably radar, matching the shape convention ADR 0062 set. A colour per severity tier was considered and rejected. Amber is already AIS's identity colour on this map, so a `warn`-tier alarm cannot also be amber without losing the distinction between "in alarm" and "just an ordinary contact." The next colour along, orange, sits close enough to amber that the two are not reliably told apart at chart scale on a sunlit cockpit display. One consistent red carries the fact plainly; the severity itself is spelled out in the marker's accessible label ("collision warning", "collision alarm") rather than left for the eye to infer from a shade.

### 4. Acknowledging does not turn the marker back to amber

An acknowledged collision alarm stays on the active-alarm list at phase `acknowledged` (ADR 0038), and the marker stays red for it. Acknowledging stops the sound and the visual alert chrome elsewhere; it says nothing about whether the target is still closing, and it usually is not going to stop doing that because someone tapped a button on the helm display. The marker answers "is this still a target closing on us," not "has a human looked at this yet."

## Consequences

The map and the alarm card can no longer disagree about which AIS target is in alarm, because both read the same list. A target the ADR 0057 §6 freshness gate is suppressing, most commonly a stale position after a restart, stays amber on the map even if the plugin's own field still says `danger` underneath. That is the correct failure mode, matching what the card is (and is not) alerting on.

A boat visible on both AIS and radar can still show two red markers with two different closing figures, exactly as ADR 0062 §4 already accepted for radar and AIS in general: two instruments, two honest numbers, no invented correlation between them.
