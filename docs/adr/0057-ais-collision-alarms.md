# ADR 0057: AIS Collision Alarms from Target Contexts

## Status
Accepted

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

### 6. A target that stops transmitting stops holding its alarm up

Decision 1 above closes the gap between the plugin and the self walk. It opens a different one: nothing in that walk ever stops looking at a context once it starts.

Three facts compound into a permanent alarm:

- The snapshot never evicts a context. There is no `delete(s.contexts, ...)` anywhere in `signalk_snapshot.go`. A target that transmitted once and then left AIS range stays in `vesselsTree()` forever.
- The plugin writes `notifications.navigation.closestApproach` on a **state change**, not on a timer. A target that closes steadily for half an hour gets exactly one write. Nothing re-sends it, and nothing on the plugin's side notices when the target stops transmitting, because the plugin has no timer either: it reacts to AIS updates, and once those stop arriving there is nothing left to react to.
- Consequently there is no plugin-side clear for a departed target. The `normal`/`"Watching"` write that closes the alarm only happens if the plugin is still evaluating that target, and a target that left range is not being evaluated by anything, on either side of this integration.

Put together: a target closes to somewhere inside the danger tier, the plugin writes one `alarm` node, the target then leaves AIS range, and that `alarm` node sits in the tree at its last value indefinitely. `signalKCollisionNotifications` keeps finding it on every poll, it never leaves the bus watcher's live set, and the clear branch at the bottom of `busNotificationWatcher.check` is only reached when a rule id drops out of that set. It never does. The alarm outlives the target by however long the process keeps running.

**The gate ages the target's position, not the notification.** The notification is exactly the thing that stopped changing: that is what "written on a state change" means once the state stops changing. Ageing it against itself would compare its own last-write time to now and silently clear an alarm that is still entirely correct, since a target closing steadily for thirty minutes carries a thirty-minute-old notification and is not stale. It is simply not being talked about again because nothing changed. Position is the fact that keeps moving. A target in AIS range re-reports its position on a timer for as long as it is transmitting, so position age is what actually answers "is this target still out there," which is the only question this gate needs to answer. `aisTargetPositionFresh` (`signalk.go`) reads `navigation.position`'s last receive time off the same `pathSeen` map the tile already uses and reports whether it is within a cutoff: one predicate, shared by both callers.

**The cutoff is not the tile's cutoff.** The tile (`nearbyVesselMaxAge`, decision predating this one) already carries this exact gate at 10 minutes, because the ghost-contact bug it fixes is the same shape as this one, discovered on the tile first and only now traced to the alarm path too. But a CPA-worthy target and a tile-worthy neighbour are not the same target. The tile lists every AIS contact in range, including a boat sitting at anchor two berths over, and a stationary Class A or B still transmits at least every ~3 minutes, so 10 minutes is a handful of missed reports and a sensible margin for "is this boat still nearby." A target that trips a **collision** alarm, by contrast, is by definition closing fast enough to matter, and a moving AIS target reports every 5 to 30 seconds (Class A close to the maximum rate, Class B a little slower), both far more often than a stationary one. Five minutes against that reporting rate is a dozen missed transmissions, not a near miss on the cutoff. And the case where a tight cutoff could cost something, a stationary target that alarms and then goes quiet between its slow ~3-minute reports, is exactly the case ADR 0058's speed filter already removes from the danger tier entirely, so tightening this cutoff loses nothing there. `collisionTargetMaxAge` is therefore 5 minutes, not `nearbyVesselMaxAge` reused, and the two constants living apart is the point, not an oversight.

**Consequence, stated on purpose rather than left as a surprise.** After a restart, a target can carry a frozen `warn` or `alarm` node with no `navigation.position` delta received yet this run: the snapshot starts empty and `pathSeen` has nothing in it until deltas arrive again. Under this gate that target's alarm is suppressed until its first position delta lands, which for a stationary Class B can be up to ~3 minutes. That is deliberate, not a gap to close later. It matches the same restart behaviour the tile already has, and a CPA figure with no corroborating position update is not something this process can vouch for. Staying quiet about an unverifiable figure is the right failure mode, not a masking one: nothing is being hidden that this process could otherwise confirm.

## Consequences

Helmcentral gains a CPA/TCPA alarm the Furuno was never going to give it, computed from AIS only.

**It will not agree with the Furuno, and it is not meant to.** Different thresholds, an independent calculation, and a narrower input. The Furuno tracks radar ARPA targets, and a radar-only contact transmits no AIS, so this will never see it. This is a second opinion covering AIS traffic, not a mirror of the plotter, and the UI should not imply otherwise.

The four threshold profiles (anchored, harbor, coastal, offshore) live in plugin config, not in Helmcentral. Profile selection stays in the plugin's webapp for now; surfacing it in the dashboard is a later decision.

The bearing conversion is a wart we are carrying for an upstream bug. It is worth an issue against the plugin, and the guard in step 2 is what tells us when the fix lands.
