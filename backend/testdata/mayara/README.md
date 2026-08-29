# mayara fixtures

Captured from the live rig, not hand-authored. Re-capture rather than edit.

Source: mayara-server 3.10.0 (radar API 3.4.0) running natively on the Windows
ship computer at 192.168.50.81:6502, driving a Furuno DRS4D-NXT (serial 6424,
firmware 01.05) at 172.31.3.248. Captured 2026-08-28, at anchor in the
Whitsundays, radar transmitting on the 3 nm range with mayara guard zone 1
enabled from 20 m to 500 m.

| File | Source |
| --- | --- |
| `radars.json` | `GET /signalk/v2/api/vessels/self/radars` |
| `capabilities-fur6424A.json` | `GET .../radars/fur6424A/capabilities` |
| `controls-fur6424A.json` | `GET .../radars/fur6424A/controls` |
| `targets-fur6424A.json` | `GET .../radars/fur6424A/targets`, 21 live targets |
| `targets-empty.json` | the same endpoint before any acquisition |
| `diagnostics.json` | `GET .../radars/diagnostics`, gunzipped |
| `stream-handshake.txt` | WebSocket connect, subscribe and hello frame |
| `target-deltas.jsonl` | 61 raw WebSocket frames, 401 target values |
| `target-deltas-timed.jsonl` | 81 frames over 4 min, each stamped with local arrival time |
| `target-delta.json` | one tracking target |
| `target-dangerous-delta.json` | one with `is_dangerous: true` |
| `target-stationary-delta.json` | confirmed stationary, motion present and zeroed |
| `target-lost-delta.json` | `status: "lost"` |
| `target-removed-delta.json` | `value: null` |

Target 100000003 appears in the capture going tracking, then lost, then null,
so the whole eviction lifecycle is real rather than constructed.

## What these captures corrected

**`is_dangerous` is snake_case inside an otherwise camelCase object.** mayara's
Rust `ArpaTargetApi` and `TargetPositionApi` carry
`#[serde(rename_all = "camelCase")]`; `TargetDangerApi` and `TargetMotionApi`
do not. Our wire struct was tagged `isDangerous` from reading the source and
assuming the rename applied throughout. It decoded silently to `false` on every
target, so no radar CPA alarm would ever have fired. Pinned now by
`TestMayaraArpaTargetDecodesCapturedDangerousDelta`.

**The radar list envelope field is `version`, not `apiVersion`.** Same cause.

**Dual range presents two radar keys**, `fur6424A` and `fur6424B`, from one
antenna. Target ids are only unique within a radar, so the store key is
`<radarID>:<targetID>`.

**The REST and WebSocket surfaces disagree about dual range.** `GET .../targets`
returns an identical list for both `fur6424A` and `fur6424B`, but the delta
stream only ever publishes under `fur6424A` (65 distinct ids there, none under
B across the whole capture). The stream is what the store consumes, so this
does not currently double-count, but anything that polls REST per radar would.

**mayara never reads guard zones back from a Furuno.** `report.rs` parses none
and `command.rs` only sends them. A guard zone set on the plotter is invisible
to mayara, and mayara's ARPA acquisition uses its own zone state
(`BlobDetector.guard_zone_1/2` in `blob.rs`). Acquisition zones must be set
through mayara.

## mayara broadcasts in 60-second bursts, not per rotation

From `target-deltas-timed.jsonl`, four minutes of frames stamped on arrival:

    burst 0-5s (86 values)  ->  60s silence
    burst 65-70s            ->  60s silence
    burst 130-135s          ->  60s silence
    burst 195-198s

Every inter-burst gap was 60 seconds, and 224 of 878 per-target gaps exceeded
thirty seconds. Within a burst the spacing is 1.6 seconds at p50, which is
about the antenna rotation rate and is what makes the raw number misleading:
that is how often a target is restated inside a burst, not how often a burst
happens.

`radarTargetMaxAge` was 30 seconds on the rotation-rate reasoning and is now
150. At 30 every target aged out between bursts, so the map would blink once a
minute and any alarm would raise and clear with it. Pinned by
`TestRadarTargetSurvivesTheMeasuredSixtySecondBurstGap`.

Worth re-measuring if scan speed or range changes, in case the burst interval
tracks either.

## The targets in this capture are mostly clutter, and that matters

At anchor, with the guard zone from 20 to 500 m, mayara reported 21 targets:
median speed 10.5 knots, maximum 20.3, and 14 of 21 above 5 knots. Nothing was
moving. These are waves, rain and shoreline return being tracked, not vessels.

Nine of the 21 carried `is_dangerous: true`, including CPAs of 3.7 m and 5.6 m.
Wiring alarms straight to that flag today would have produced nine simultaneous
collision alarms at anchor, which is ADR 0057 section 6 and ADR 0058 repeating
on a new input: the collision calculation goes degenerate when own ship is
stationary, and a flag computed from it inherits the problem.

Treat these fixtures as correct wire shape and unrepresentative content. Before
the alarm phase ships, capture again underway, where the geometry is real, and
decide then what gating radar alarms need beyond `status == "tracking"`.

## Still missing

No capture yet of a target with `motion`, `danger`, or `position.latitude`
omitted. All 400 target values in this capture carry all three. Those branches
are exercised by hand-built values in Go, which tests our conversion logic but
not the field names, so a fixture is still wanted. Do not assume the omission
shape until one exists.
