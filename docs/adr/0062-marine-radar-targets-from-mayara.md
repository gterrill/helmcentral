# ADR 0062: Marine Radar Targets from mayara-server

## Status
Accepted

Extends ADR 0057 (AIS collision alarms from target contexts) and ADR 0029 (SignalK discovery by unicast sweep, not mDNS).

## Context

ADR 0057 said this out loud in its own consequences:

> The Furuno tracks radar ARPA targets, and a radar-only contact transmits no AIS, so this will never see it. This is a second opinion covering AIS traffic, not a mirror of the plotter, and the UI should not imply otherwise.

That was honest and it was also a hole. A steel fishing boat with a dead transponder, a tender, an unlit mooring ball, a squall cell: the DRS4D-NXT paints all of them and Helmcentral sees none of them. The alarms we have are the ones that only fire for vessels considerate enough to announce themselves.

### mayara closes it without a protocol project

[mayara-server](https://github.com/MarineYachtRadar/mayara-server) is an Apache-2.0 Rust server that speaks the proprietary radar wire formats and republishes them as a Signal K flavoured HTTP and WebSocket API. It carries a full ARPA implementation: blob detection off the spokes, an IMM filter running three Kalman models, target lifecycle, and CPA/TCPA. The Furuno DRS-NXT series is on its tested-against-real-hardware list.

That means the expensive part is already written by someone with the hardware to test it. What is left for us is a client.

### It is already running, and it already works

Measured 2026-08-28 against mayara 3.10.0 on the Windows ship computer, radar API 3.4.0:

| Check | Result |
| --- | --- |
| Radar found | Furuno DRS4D-NXT, serial 6424, firmware 01.05, at 172.31.3.248, listener `Active` |
| `GET .../radars` from the helmcentral container | 200, full radar list |
| WebSocket `/signalk/v1/stream?subscribe=none` | Opens, accepts the target subscription, sends a hello frame |
| `http://mayara.local:6502` from the helmcentral container | `wget: bad address` |

The container sits on `172.25.0.2` behind a Docker bridge and reaches `192.168.50.81:6502` by plain outbound NAT. Everything the integration needs is already reachable from where the code will run.

### Dual range is not hypothetical

One physical radar presents two keys:

```json
{"version":"3.4.0","radars":{
  "fur6424A":{"name":"DRS4D-NXT 6424","model":"DRS4DNXT","radarIpAddress":"172.31.3.248"},
  "fur6424B":{"name":"DRS4D-NXT 6424 B","model":"DRS4DNXT","radarIpAddress":"172.31.3.248"}}}
```

mayara numbers targets within a radar. Two ranges on one antenna means target id 3 exists twice on day one, on a boat that owns exactly one radar.

That same capture corrected the envelope field. Reading mayara's Rust source suggested `apiVersion`; the wire says `version`. The fixture rule earned its keep before a line of client code existed.

### Why it produces nothing today

`GET .../targets` returns `[]` on both ranges, and that is correct behaviour rather than a fault. Two independent causes, both on the radar side:

`diagnostics.json` reports `"navigation_address": null`. mayara has no own-ship position or heading, and it will not track without one. CPA and TCPA are not computable without one either. It is meant to find a SignalK server over mDNS, and the ship computer has seven interfaces: a dead 169.254 link-local, the Furuno network on 172.31.3.150, Tailscale, an NVR segment on 192.168.1.1, the Peplink LAN on 192.168.50.81, ZeroTier, and loopback. Leaving it to guess across that set is not a plan.

The radar is transmitting (`power: 2`) but `doppler: 0` and both guard zones report `enabled: false`. ARPA acquires through a guard zone, through Doppler, or through a manual MARPA click. With all three unavailable there is nothing to acquire with.

Neither is a Helmcentral problem, and neither is fixed by writing client code. They are listed here because the integration is unverifiable until they are resolved, and because a reader finding an empty target list six months from now should find the explanation before the bug report.

## Decision

### 1. Consume ARPA targets and radar status. Leave the spokes alone.

The target stream is JSON over the Signal K WebSocket. The spoke stream is protobuf at the antenna's rotation rate and exists to draw a radar picture. We want collision awareness, not a second plotter, and the boat already has a plotter that renders this radar properly. Taking the targets and skipping the spokes avoids protobuf codegen, a high-bandwidth stream, and a canvas renderer, for no loss against the actual goal.

Radar presence, model, range and transmit state come along because an empty target list is ambiguous without them. "No contacts" and "the radar is in standby" must not look alike.

### 2. Trust mayara's CPA and TCPA.

`danger.cpa`, `danger.tcpa` and `danger.isDangerous` are passed through unchanged.

This is not a shortcut, it is the pattern already in the building. Helmcentral computes no CPA anywhere: ADR 0057 ingests figures from `signalk-ais-target-prioritizer` and reads its notification. Radar targets trusting mayara is the same arrangement with a different producer. Recomputing would mean writing a collision solver to second-guess an IMM Kalman filter fed by data we do not have, which is a worse calculation dressed as a safer one.

The consequence is accepted deliberately: radar alarms use mayara's IMO defaults (CPA under 0.5 nm, TCPA within 6 minutes) and do not follow the vessel-state profile ADR 0058 built for AIS. The two alarm families will disagree. They are different instruments.

### 3. Find mayara by unicast sweep, and let the operator type an address.

> **Superseded 2026-08-29, see the amendment at the end.** The sweep was right while Helmcentral talked to mayara directly. The mayara SignalK plugin removes the need to find anything, and the configuration with it. The reasoning below is kept because it still explains why mDNS is not an option on this stack.

ADR 0029 ruled out mDNS for SignalK on this stack: musl with no NSS-mDNS, a bridge container, multicast that never reaches the LAN. Nothing about mayara changes the constraint, and the container test above reproduces it for mayara specifically rather than inheriting the argument. `mayara.local` does not resolve from where the code runs.

There is a second reason that survives even where mDNS works. `mayara.local` resolves to two addresses on that host, and from the development Mac it answered with the ZeroTier address ahead of the LAN one. A name on a seven-interface machine is a coin toss. An IP is not.

So `POST /api/radar/discover` sweeps port 6502 across the derived `/24`, reusing `resolveDiscoveryNetwork`, `hostsInNetwork` and the RFC1918 refusal from ADR 0029 unchanged. A TCP open is not evidence: the responder has to answer the radars endpoint with a decodable envelope, and the discovered radar names are reported so the operator can tell their radar from a neighbour's. Discovery persists nothing; accepting a result saves through `POST /api/settings` as ADR 0028 requires.

ADR 0029 left this door open on purpose: "If it ever runs natively, an mDNS source could sit behind this same endpoint alongside the sweep." It still can.

### 4. Radar targets are their own thing, not AIS targets with a flag.

A new `radarTarget` struct and a new `radar-targets` telemetry event, not extra fields on `nearbyVessel`. A radar contact and an AIS contact are different observations from different instruments with different failure modes, and the UI labels them apart: AIS keeps its filled circle, radar draws a triangle oriented on course, which is the ARPA convention and readable without relying on colour.

Store keys are `<radarID>:<targetID>` because of dual range, and the payload keeps `position_derived` so a projected position is never mistaken for a reported one.

No correlation between the two. A vessel carrying AIS that the radar also paints will produce two contacts and, if it is closing, two alarms with different numbers. That is the honest rendering of two instruments that genuinely disagree, and inventing a merge would hide the disagreement rather than resolve it. Each alarm row carries a visible source tag so the operator knows why there are two. If correlation is ever wanted it belongs on bearing and range agreement plus SOG and COG agreement, and it should be built after watching real radar and AIS side by side, not before.

### 5. Age targets against our own clock, and evict three ways.

> **Superseded 2026-08-29, see the amendment at the end.** Polling returns the full target set, so absence is the eviction signal and the three below collapse to one staleness guard. The clock discipline in this section still holds and still matters.

`9cc5926` and ADR 0057 section 6 established the failure mode: a target source without eviction discipline does not lose targets, it strands alarms lit forever. Three signals, because each covers what the others cannot:

`value: null` in the delta is mayara's explicit removal, and it is only received if we are connected at that instant. `status: "lost"` is a separate producer behaviour, and from outside we cannot tell which one mayara emits when, so handling only one is a bet. A wall-clock sweep at 150 seconds is the backstop, and it is the only one that catches mayara hanging with the socket open: no FIN, no read error, no reconnect, no null, every entry frozen at its last value with its alarm still ringing.

The eviction reference is local delta receive time. Never mayara's `firstSeen` or `lastSeen`, which are mayara's clock on a Windows box we do not synchronise. ADR 0042 already paid for that lesson, where a failed timestamp parse reported a dead target as "0s ago" indefinitely. `lastSeen` is carried to the wire for display and drives no decision.

Disconnection does not purge. A one second blip must not blank the map, so the payload source flips to `mayara-unreachable` and the age filter clears the targets within half a minute.

### 6. Alarms reuse the bus watcher, and get a second tier we own.

`radarCollisionNotifications` feeds both `activeAlarms()` and `busNotificationWatcher.check()`, which buys the 5 second dwell, edge-triggered raise, escalate and clear, the alarm log row and the off-boat push without new machinery.

Rule ids are prefixed `radar:` and carry no `@`, so `splitNotificationVessel` returns them whole with an empty vessel id, exactly as it already does for a self-tree notification. No SignalK path can start with `radar:` because paths are dotted segments and `:` never appears in one, so the two schemes cannot collide in either direction.

mayara gives one boolean and Helmcentral alarms have two tiers. `isDangerous` maps to `alarm`. A softer ring that we own, CPA under 1 nm with TCPA inside 15 minutes, maps to `warn`, and it can only fire on targets mayara has already declared not dangerous, so it never contradicts the producer. A single tier was rejected because the bus watcher escalates on rank increase alone, so a contact going from worth-a-look to act-now would raise no second push.

Radar alarms are not published back to SignalK. The dispatch transport skip that covers `alarmSourceSignalK` extends to cover radar, because publishing `notifications.radar.<id>.cpa` to our own server would have `signalKNotifications` read it back on the next tick and re-raise it.

### 7. Guard zones are not claimed until a capture proves them.

mayara broadcasts guard-zone notifications to its upstream SignalK server, which is the same server Helmcentral reads. If that holds, `signalKNotifications` already picks them up as ordinary bus notifications with dwell, clear, log and push, for no new code, and consuming them from mayara's socket as well would double-raise every one.

That is a plausible reading of the source and it is not a measurement. Until a capture shows where those notifications actually land, guard-zone support is neither built nor advertised. Claiming a safety feature on an unverified assumption is the masking behaviour AGENTS.md forbids, and it is worse here than elsewhere because the failure is silent.

### 7a. On Furuno, guard zones are mayara's, not the radar's.

Measured 2026-08-28 while trying to make the radar acquire anything. Enabling guard zone 1 on the Furuno MFD changed nothing that mayara could see: `guardZone1` stayed all zeros with its timestamp frozen at startup, while `range` updated live in the same breath when it was changed from 1.5 to 3 nm. So the control channel is healthy and the guard zone simply is not on it.

The source says why. `src/lib/brand/furuno/report.rs` parses no guard zone at all, so there is no read path; `src/lib/brand/furuno/command.rs` only ever sends one. And mayara's ARPA acquisition does not consult the radar's zones in any case: `BlobDetector` in `src/lib/radar/target/blob.rs` holds its own `guard_zone_1` and `guard_zone_2` and tags each blob with `in_guard_zones` from mayara's own control state.

The consequence is operational and belongs in the runbook rather than the code. A guard zone set on the plotter drives the plotter's alarm and contributes nothing to Helmcentral. Acquisition zones have to be set through mayara, in its GUI or over its API. The two alarm surfaces are independent, and an operator who sets one and expects the other is going to conclude this integration is broken.

Note also `command.rs` carries `// TODO: verify range unit empirically on hardware` on the Furuno guard-zone command. The bearing conversion goes through `radians_to_spokes` and looks sound; the range unit is unvalidated upstream. If a zone lands at the wrong distance, suspect that before suspecting our client.

### 7b. The danger flag is not trustworthy at anchor, and the first capture proves it.

Decision 2 says we trust mayara's CPA and TCPA. That still holds, and it needs a boundary drawn around it.

First live capture, 2026-08-28, at anchor in the Whitsundays with mayara's guard zone 1 set from 20 to 500 m. mayara reported 21 targets: median speed 10.5 knots, maximum 20.3, fourteen of twenty-one above 5 knots. Nothing in the anchorage was moving. Those are waves, rain and shoreline return being tracked as vessels.

Nine of the twenty-one carried `is_dangerous: true`, with CPAs down to 3.7 m and 111 seconds to go.

Wiring the alarm path to that flag today would have produced nine simultaneous collision alarms on a boat at anchor. That is ADR 0057 section 6 and the whole of ADR 0058 arriving again on a new input, and for the same underlying reason: with own ship stationary the relative velocity vector is noise, CPA collapses toward present range, and TCPA becomes range divided by jitter. A boolean computed from that inherits every bit of it.

So: `status == "tracking"` and a non-nil `danger` are necessary and are visibly not sufficient. What else radar alarms need is deliberately not decided here, because the honest answer is that we do not yet have the data to decide it. The capture that would settle it is one taken underway, where the geometry is real. Until then the alarm phase stays unshipped, which is what decision 8 already sequences.

Two candidates worth weighing when that capture exists, neither adopted yet: gate on `navigation.state` the way ADR 0058 gates the AIS profile, since "anchored" is exactly when this degenerates; or require a minimum track age, since a real vessel persists across many rotations and a wave does not. The second is attractive because it needs no external input and mayara already carries a promotion threshold internally.

### 8. Alarms ship last.

Settings, then the stream, then the tile, then the map markers, then the alarms.

ADR 0057's own lesson is that an alarm whose underlying data nobody can inspect is an alarm the crew learns to ignore. The first radar CPA alarm should fire against a target already visible on the map, at a range and bearing that can be checked against the plotter.

## Consequences

Helmcentral gains collision awareness of contacts that transmit nothing at all, which is the category ADR 0057 explicitly could not cover.

The AIS and radar alarm families will disagree, by construction. Different producers, different thresholds, different inputs, and no shared profile. A vessel carrying AIS can raise both at once with different figures. This is deliberate and the UI labels the source, but it will look like a bug the first time it happens at 0200.

**The integration depends on a Windows machine nobody is watching.** mayara has to run on the ship computer because that is what shares an ethernet segment with the radar, it has to start with the machine, and it has to be given `-n ws:192.168.50.240:3000` explicitly rather than left to find SignalK by mDNS across seven interfaces. If mayara stops, radar targets fade within two and a half minutes and the tile says OFFLINE, which is the right behaviour, but nothing brings it back automatically. `--parent <PID>` exists for supervision if Helmcentral ever launches it, and that is out of scope here.

**mayara depends on SignalK, so a SignalK outage takes the radar with it.** No own-ship position means no tracking at all, not degraded tracking. The single point of failure moved, it did not go away.

An assumption is carried rather than resolved: that the radar's position is own-ship position. mayara reports distance "from radar" and Helmcentral has no antenna-offset setting. At ARPA ranges a 5 to 10 metre offset is inside the plot error, so this is fine until someone wants the radar overlay to line up at 100 metres. The projection cross-check will surface it as a constant-magnitude disagreement rather than a mystery, which is the point of having the check.

Bearing is documented as radians true and treated as such. If it turns out to be relative to the bow, every projected position is wrong by the heading, and at anchor the error rotates as the boat swings. Rather than guessing, the client projects from bearing and range even when mayara supplies a fix, compares the two, and logs loudly when they disagree beyond about 50 metres. That follows `collisionBearingRadians`, which rejects an out-of-range bearing rather than reinterpreting it.

`radarTargetMaxAge` is 150 seconds, and it was 30 until a capture corrected it. The original figure came from reasoning about antenna rotation: 24 to 48 rpm means a target should refresh every 1.25 to 2.5 seconds, so 30 seconds looked like a dozen missed rotations of headroom.

mayara does not work that way. Measured across a four-minute capture of the live DRS4D-NXT on 2026-08-28 (`backend/testdata/mayara/target-deltas-timed.jsonl`), it broadcasts in bursts: about five seconds of frames carrying every tracked target, then sixty seconds of silence. Frames arrived at 0-5s, 65-70s, 130-135s and 195-198s, and 224 of 878 per-target gaps exceeded thirty seconds. The 1.25 to 2.5 second figure is real but it is the spacing within a burst, not the refresh interval.

At 30 seconds every target aged out between bursts, so the map would have blinked once a minute and any alarm would have raised and cleared with it. 150 seconds is two and a half missed bursts: ordinary jitter or one dropped burst changes nothing, and a departed target still does not linger. If the burst interval turns out to track scan speed or range, this wants deriving rather than hardcoding.

The payload is capped at 20 targets by range, mirroring the AIS cap of 10. Alarm evaluation is not capped, because the nearest contact is not always the most dangerous one and a cap there would be a silent safety hole.

The no-fix sentinel needed guarding, and the AIS path has been getting away without it. `fetchSignalKVesselState` initialises `vesselStateData` with `Latitude: -1, Longitude: -1` to mean nothing is known yet, and a range check passes that through because -1,-1 is a real point in the Gulf of Guinea. Nothing downstream of `buildNearbyVesselsPayload` catches it either; AIS survives by accident, because with own ship at -1,-1 every genuine contact falls outside the 5 km range gate and the list comes back empty. Degraded, but not wrong.

Radar projects target positions from own ship, so the same sentinel would place every derived contact off West Africa and plot it confidently. `ownShipFixFromVesselState` rejects it explicitly. Null island stays a valid fix, because it is a real coordinate and the sentinel is the thing that actually needs excluding. Worth fixing on the AIS side too, where it is currently a silent empty list rather than a stated outage.

No radar trails. `use-ais-trails.ts` keys on vessel name and radar targets have no name. Keying on `radar_id:target_id` would actually be the better pattern, which is worth remembering when trails are next touched.


---

## Amendment, 2026-08-29: onto the SignalK plugin

[mayara-server-signalk-plugin](https://github.com/MarineYachtRadar/mayara-server-signalk-plugin) was installed on the SignalK server after this ADR shipped. It changes the transport and removes the configuration. It does not change decisions 1, 2, 4, 6, 7 or 8.

### What the plugin does, measured before deciding

| Check | Result |
| --- | --- |
| `vessels.self.radars` in the delta tree | Present, `fur6424A` and `fur6424B`, each carrying **`controls` only** |
| `vessels.self.radars.<id>.targets` | Absent |
| Delta stream, 25 s watch on `radars.*` | 78 values across 38 path shapes. The only target-named paths are `controls.targetSeparation` and `controls.targetTrails`, which are radar settings. |
| `GET {signalk}/signalk/v2/api/.../radars/{id}/targets` | 200, proxied through to mayara |
| `GET {signalk}/signalk/v2/api/.../radars/{id}/spokes` | 404. No WebSocket proxy. |

The plugin registers as a Radar API provider: it proxies mayara's REST surface and publishes radar controls as deltas. It publishes no ARPA targets, and its README calls that future work. So it does not replace this integration; it replaces the plumbing underneath it.

### The changes

**Targets are polled through SignalK, not streamed from mayara.** `GET {signalk}/signalk/v2/api/vessels/self/radars/{id}/targets` every two seconds. There is no push path through the plugin, so this is not a preference.

It is also not a downgrade. Decision 5 recorded that mayara broadcasts in sixty-second bursts; polling reads the tracker's live state, so it is fresher than the stream it replaces. And each response is the whole target set, so a target absent from it is gone. The `value: null` branch, the `status: "lost"` branch and a `radarTargetMaxAge` inferred from broadcast cadence all stop being load-bearing. A wall-clock guard survives at thirty seconds, covering only a failing poll, and it survives for the reason decision 5 gave: an alarm stranded by missing eviction is worse than a target briefly missing.

**Availability is detected, not configured.** Radars present in `vessels.self.radars` means radar support; absent means none. That is the capability-detection shape `autopilot.go` already uses for `steering.autopilot.availableActions`, and it is the reason there is no longer a `radar:` settings block, no host, no port and no toggle. Deleting the unicast sweep with them.

The cost is a coupling stated plainly: no plugin, no radar in Helmcentral. That is acceptable because the plugin is also how mayara gets configured, so anyone running the radar has it.

### A third instance of the same failure, and the fix

Measured 2026-08-28 21:50Z, immediately after the rework was specified. The radar had gone to standby about half an hour earlier (`power: 0`). mayara was still serving **50 targets**, statuses `tracking` and `lost`, **five of them still flagged `is_dangerous`**, positions drifting smoothly under Kalman extrapolation. Captured as `backend/testdata/mayara/targets-standby-stale.json` alongside `controls-standby.json`.

Ageing against our own receive time does not catch this. The poll succeeds every two seconds and returns the same extrapolated set, so the target never looks stale from where we stand. This is ADR 0057 section 6 arriving a third time: data that keeps turning up on schedule while meaning nothing. The first was a plugin writing a notification once on state change, the second was mayara bursting every sixty seconds, and this is a radar in standby.

The fix is available without trusting mayara's clock absolutely. Both of these come from mayara and were captured in the same second:

    controls.power.timestamp   2026-08-28T21:49:54Z    live, still advancing
    newest target lastSeen     2026-08-28T21:16:51Z    33 minutes behind

Decision 5 said never to age against `lastSeen` because it is mayara's clock on a machine we do not synchronise. That still holds for absolute time. A *difference* between two mayara timestamps is unaffected by skew, so comparing a target's `lastSeen` against mayara's own current time, taken as the freshest control timestamp for that radar in the SignalK snapshot, is sound where comparing it against ours would not be.

So targets are gated two ways, and both are free because the controls already stream as deltas:

1. Drop any target whose `lastSeen` trails the radar's freshest control timestamp by more than about a minute. This catches a radar in standby and an individual contact that went unobserved while transmitting, using one rule.
2. Report the radar's own `power` state in the payload, so the tile can say STANDBY rather than implying an empty list means clear water.

Without this the tile would have shown fifty half-hour-old phantoms as live contacts, and had the alarm phase already shipped, five of them would have been ringing at anchor with the radar switched off.

### Correction, 2026-08-29: the plugin does publish targets

The table above says `vessels.self.radars.<id>.targets` is absent and that the delta stream carries no target paths. **That is wrong, and the decision to poll rests on it.**

Re-measured once the radar had actually acquired something:

    GET /signalk/v1/api/vessels/self/radars/fur6424A/targets
      -> 928 target ids, 53 with a non-null value

    delta stream, 30 s on radars.*
      -> 928 target values, 875 of them null
      -> path shape: radars.<radar>.targets.<id>

The plugin publishes ARPA targets as ordinary deltas, with `value: null` as the removal signal, which is precisely the shape the original mayara-direct client consumed.

The earlier finding was not a misreading; it was a measurement taken while the radar had produced no targets, on a system where the branch does not exist until something populates it. An absent branch was read as an absent feature. Every check that produced the table was taken during such a window, so the error was consistent and looked solid.

What follows from it:

- Polling is not wrong and is not broken. It works, and it is what is deployed. But it is the more complicated option: it adds traffic, and it discards the `value: null` removal signal in favour of inferring absence from a full-state response.
- Reading targets from the snapshot needs no new connection, no polling, and no configuration, since the deltas already arrive on the stream Helmcentral subscribes to. Decision 5's three eviction signals become relevant again, because `null` is back.
- The tree accumulates removed targets as null-valued keys: 875 of 928 on one read. A snapshot reader must treat null as absence rather than as a target, and should expect the key count to grow well past the live count.

Whether to move from polling to the snapshot is a separate decision and is not taken here. What is recorded here is that the premise for polling was false.

### Measured 2026-08-30: what actually filters the clutter

The first capture with the radar transmitting continuously, once TimeZero Pro and mayara were separated onto different IPs (Furuno permits one connection per address, and the two clients had been trading the radar back and forth in roughly forty-second turns).

Three and a half minutes at anchor, own ship making 0.3 knots, sampled every 6 seconds. Raw capture in `backend/testdata/mayara/target-persistence-anchored.jsonl`.

**Track age is a powerful filter.** 111 distinct target ids appeared across 36 samples. The median id was present in 4 of them, about 24 seconds. Requiring a target to persist cuts the population hard:

| Minimum persistence | Targets surviving | Ever flagged dangerous |
| --- | --- | --- |
| none | 111 | 29 |
| 30 s | 51 | 20 |
| 60 s | 3 | 2 |
| 120 s | 2 | 2 |

A 60-second floor removes 97% of them. The clutter is ephemeral: it appears for half a minute and is gone.

**But it is not sufficient, and the survivor proves why.** Of the three that lasted a minute, the one flagged dangerous was:

    id=100000001  36/36 samples  range 155->161 m  bearing 56->55 deg
                  sog 0.3 kn     cpa 156 m  tcpa 112 s  is_dangerous=true

Constant range, constant bearing, stationary. Almost certainly an anchored boat or a fixed object. And its CPA of 156 m against a present range of 155 m is the exact signature ADR 0058 recorded for the AIS path: with both vessels stopped the relative velocity vector is noise, CPA collapses to present range, and TCPA becomes range over GPS jitter. Persistence filtering keeps this target precisely because it is stationary, which is the opposite of what is wanted.

So the two candidates in decision 7b are not alternatives. Track age kills the ephemeral clutter that dominates by count; something else has to kill the degenerate stationary case that survives it. ADR 0058 solved the same problem for AIS with a speed floor, and its lesson applies unchanged: `speed: 0` there did not mean "no minimum", it disabled the filter and let a motionless boat raise a collision alarm.

Deliberately not fixing thresholds here. What the measurement settles is the shape of the answer: a persistence requirement plus a relative-motion or speed floor, not either alone. What it cannot settle is the numbers, because every capture so far is at anchor, where own-ship motion is the degenerate input. That part still wants a passage.

### Measured 2026-08-31: two failures of retention, one ours

The ship's computer was off overnight. Both problems come from something retaining state that has stopped being true.

**mayara accumulates dead target ids and republishes them all on subscribe.** 6,152 of them in one frame, 402 KB, about 67 bytes per dead id, and it grows for as long as the process runs. Helmcentral survives it because `signalKStreamReadLimit` is 4 MB (`signalk_stream.go`), well above the default, with a test already covering oversized deltas.

That headroom is finite and the arithmetic is worth stating, because it has a date on it. 4 MB divided by 67 bytes is roughly 62,700 ids. The persistence capture measured 111 distinct ids in 3.5 minutes at anchor, about 46,000 a day, so **on the order of a day and a half of continuous radar before the subscribe frame exceeds the read limit**. The churn was measured in heavy clutter and would likely be lower on passage, so treat that as an order of magnitude rather than a deadline. The failure mode is what makes it matter: the frame is sent *on subscribe*, so past the threshold every reconnection attempt dies on the first message, and Helmcentral loses the entire SignalK stream, not just radar. Worth reporting upstream, and worth watching the frame size if the radar ever runs for days.

It also argues against the change the previous correction implied. Reading targets from the snapshot instead of polling would mean consuming that frame and filtering 875-of-928 nulls on every reconnect, and holding every dead id in our own snapshot, which never evicts either. Polling reads the live set and nothing else. The correction stands, in that the premise for polling was false, but the conclusion happens to survive on grounds nobody had measured yet.

**Our own presence detection had the same disease.** `radarsFromSnapshot` read `vessels.self.radars` with no freshness check. The SignalK tree never evicts, so with mayara gone it still listed both radars, controls 17.9 hours old, power frozen at Transmit. Two consequences: the poller kept polling a radar that no longer existed, logging a 404 every two seconds against the live server, and `Transmitting` read true off an eighteen-hour-old value, feeding the standby gate exactly the stale input it exists to reject. Only mayara's 404 stopped stale targets reaching the map.

This is ADR 0057 section 6 for the fourth time in this integration, and the first time in code written for it. Presence is now gated on `radarPresenceMaxAge` (5 minutes) judged on `snapshot.lastSeen`, our own receive clock, matching `aisTargetPositionFresh`.

One subtlety that receive-time freshness does not remove: SignalK replays the whole retained tree on subscribe, measured as 76 control deltas all arriving at 0.0 s and nothing afterwards. So every reconnect refreshes the receive time of stale values, and a dead radar reads as present for up to `radarPresenceMaxAge` after each one. That is a bounded window rather than a hole, and it fails safe, but it means "we received it recently" is a weaker statement than it looks on a protocol that replays.

Poll failures are now logged on their edges rather than every tick. The fallback policy requires retry behaviour to be loud, and the transition into failure and the recovery are both still reported; what is dropped is the identical line every two seconds for as long as the radar is switched off.

### On ADR 0037

ADR 0037 says all vessel data arrives on the delta stream and there is deliberately no REST read path, so a dropped stream surfaces as an outage instead of being papered over.

This poll is not that. No delta carries radar targets, from the plugin or from anything else, so REST is the only path rather than a fallback concealing a broken one. The distinction matters and is written down here because the next reader would otherwise be right to call it a violation. If the plugin ever does publish targets as deltas, this should move back onto the stream and the poll should go.

### Still true, and still the open question

Decision 7b stands untouched: the first live capture at Hamilton Island produced 21 targets tracing Front Street, the breakwater and Marina Walk, nine of them flagged dangerous, on a boat at anchor with nothing moving. Anchored later somewhere without a marina inside the guard zone, the same radar produced one. The clutter is location-dependent rather than inherent, which weakens the case for gating on `navigation.state` since both readings were taken anchored. Alarms still wait for a capture underway.
