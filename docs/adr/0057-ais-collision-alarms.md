# ADR 0057: AIS Collision Alarms from Target Contexts

## Status
Proposed

Extends ADR 0038 (SignalK notifications as the alarm vocabulary).

## Context

The Furuno raised a CPA/TCPA alarm on watch and I went looking for it on the bus. It is not there, and it never was.

The TZT2BB sits at N2K address 2 and is close to a listener. Across a 45 second capture of every delta on the network it transmitted nothing, and over the whole data model it contributes three paths, all route data: `navigation.currentRoute.name`, `navigation.currentRoute.waypoints`, `navigation.courseRhumbline.nextPoint.timeToGo`. No alert PGNs (126983/126984/126985/126986) crossed the wire. No target on the model carried a CPA figure.

That is by design rather than a fault. Furuno computes CPA/TCPA on the MFD from its own target tracking and keeps it on the NavNet Ethernet side. NMEA 2000 has no standard PGN for tracked-target CPA/TCPA; the standard carrier is the 0183 `TTM` sentence, and the AIS feed here comes from a Vesper Cortex sending `VDM`/`VDO` with no `TTM`. There is no path for that alarm to reach us, so waiting for one is waiting forever.

`signalk-ais-target-prioritizer` (MIT, v0.4.17) is now installed and computes the figure independently from the AIS data already in the model. It populates all 21 targets.

### What it publishes

On each target's context, `navigation.closestApproach`:

| Property | Units | Notes |
| --- | --- | --- |
| `distance` | m | CPA. Absent when the target is opening. |
| `timeTo` | s | TCPA. Absent when the target is opening. |
| `range` | m | Current range. Always present. |
| `bearing` | **deg** | Off-spec, see below. |
| `collisionRiskRating` | float | Lower is worse. A sort key, not a score. |
| `collisionAlarmType` | `guard` \| `cpa` | Absent when not tripped. |
| `collisionAlarmState` | `warning` \| `danger` | Absent when not tripped. |

And on each target's context, `notifications.navigation.closestApproach`, state `warn` or `alarm`, message `WINGS 12 - CPA WARNING`, falling back to `normal` with message `Watching` once the target clears.

### Three things that bite

**The notifications are not on self.** `signalKNotifications` reads `snapshot.selfTree()` and walks down from there. Every collision notification this plugin raises lives on the target's own context, so as the code stands today the alarm engine sees none of them. This is the real work in this ADR; the rest is detail.

**`bearing` is degrees, not radians.** Measured across 21 live targets the values span 4.20 to 358.48, which cannot be radians. The plugin's README claims rad True and the SignalK convention is radians, but the schema-supplied `meta` only documents `distance` and `timeTo`, so nothing in the model declares a unit for `bearing` and nothing catches the mismatch. Consumed raw it is wrong by a factor of 57.

**The self context reuses the collision path for a sensor fault.** When the plugin loses our own GPS fix it raises `notifications.navigation.closestApproach` under self at state `alarm` with a "No GPS position received" message. That is a sensor-health problem wearing a collision alarm's clothes.

## Decision

### 1. Walk vessel contexts for collision notifications, keep self for everything else

`signalKNotifications` stays exactly as it is. Other-vessel notifications are a different shape and deserve a different collector rather than a widened one: they are per-target, they carry a vessel identity the existing `alarmStatus` has nowhere to put, and they arrive in unbounded number.

A sibling walks `snapshot.vesselsTree()`, skips the self context, and reads only `notifications.navigation.closestApproach`. Nothing else on a target's tree becomes an alarm. The delta stream already subscribes to `vessels.*` at a measured 2.3% of frame volume (`signalk_stream.go`), so the data is in the snapshot already and this costs no new traffic.

### 2. Normalise bearing at the boundary, and fail loudly if the units move

The ingest converts `bearing` from degrees to radians once, on the way in, so the rest of the host sees the SI value every other path already carries. Guard it: a value outside 0 to 360 is not degrees, and a plugin release that silently switches to radians would otherwise sail straight through as a plausible-looking bearing near north.

Per the fallback policy this guard does not paper over the problem. It rejects the reading and surfaces the disagreement rather than picking whichever interpretation looks reasonable.

### 3. The self-context GPS alarm is a sensor fault, not a collision

The collector skips the self context outright, which drops the "no GPS fix" alarm on the floor. That is the correct outcome for this path. Losing our own fix is already visible through the existing vessel-state staleness handling, and routing it through a collision alarm would put a klaxon on the wrong problem and teach the operator to distrust the CPA alarm.

### 4. `collisionRiskRating` is a sort key and is not displayed

Live values run to six significant figures (208628.33, 400022.70). It orders targets and nothing more. The host sorts by it and shows CPA, TCPA, range and bearing, which are the numbers an operator can actually check against the plotter.

### 5. The distress plugin needs no work

`@sailingnaturali/signalk-ais-distress` raises `notifications.received.distress.ais-<id>` under **self** at state `emergency`. That is the self tree, in standard vocabulary, at a state the alarm engine already understands. It flows through `signalKNotifications` with zero code changes, which is the ADR 0038 bargain paying out exactly as intended.

## Consequences

Helmcentral gains a CPA/TCPA alarm the Furuno was never going to give it, computed from AIS only.

**It will not agree with the Furuno, and it is not meant to.** Different thresholds, an independent calculation, and a narrower input. The Furuno tracks radar ARPA targets, and a radar-only contact transmits no AIS, so this will never see it. This is a second opinion covering AIS traffic, not a mirror of the plotter, and the UI should not imply otherwise.

The four threshold profiles (anchored, harbor, coastal, offshore) live in plugin config, not in Helmcentral. Profile selection stays in the plugin's webapp for now; surfacing it in the dashboard is a later decision.

The bearing conversion is a wart we are carrying for an upstream bug. It is worth an issue against the plugin, and the guard in step 2 is what tells us when the fix lands.
