# signalk_notifications fixtures

Captured from the boat's signalk-server 2.24.0, 2026-09-08 ~05:20Z.

| File | Source |
| --- | --- |
| `self.json` | `GET /signalk/v1/api/vessels/self/notifications` |
| `other_vessel.json` | `GET /signalk/v1/api/vessels/<AIS target id>/notifications` for JARRICKI |

Both are the raw notifications subtree body, not wrapped in another
`"notifications"` key. It is the same shape `signalKSnapshot.reconcileNotifications`
merges against.

`other_vessel.json` is the live CPA-mismatch evidence behind ADR 0086: the
server reports `navigation.closestApproach` as `state: "normal"` with
timestamp `2026-09-08T03:43:01Z`, while at capture time Helmcentral's own copy
still had that vessel's CPA alarm active. Nothing re-read the server between
the two states.

## Motion fixtures (ADR 0098)

Captured from the boat's signalk-server, 2026-09-15 ~00:15Z, for the COLREGS
encounter classifier (`collision_colregs.go`). Each is the raw response body
for the paths named, trimmed to what the classifier reads, not the full
vessel/self tree.

| File | Source |
| --- | --- |
| `other_vessel_motion.json` | `GET /signalk/v1/api/vessels/urn:mrn:imo:mmsi:352006488` (ULTRA DETERMINATION), trimmed to `navigation.courseOverGroundTrue`, `navigation.speedOverGround`, `navigation.state`, `navigation.position`, `design.aisShipType` |
| `self_motion.json` | `GET /signalk/v1/api/vessels/self`, trimmed to `navigation.headingTrue`, `navigation.speedOverGround`, `navigation.position`, `navigation.state`, `environment.wind.angleApparent` |

Both fixtures came from `GET /signalk/v1/api/vessels` first, to find a target
carrying every path the classifier needs — most AIS contacts moored in the
anchorage that morning did not.

Confirmed from these captures, not assumed:

- **`navigation.state` is a string, not a numeric code**, for both self and
  an AIS target. Every vessel in the full `/vessels` dump at capture time
  reported one of `"motoring"`, `"anchored"`, `"moored"` — none was
  `"sailing"`, `"fishing"`, `"not under command"`, `"restricted
  manouverability"` or `"constrained by draft"` (the anchorage held no such
  traffic that morning). Those five come from the upstream SignalK schema
  (`schemas/groups/navigation.json`, the `state` enum) rather than a live
  capture, and `collision_colregs.go` says so at its point of use. Note the
  schema's own spelling: `"restricted manouverability"` (missing the second
  "e") and `"constrained by draft"` — not "constrained by her draught", which
  is COLREGS Rule 3(h)'s wording, not the wire value.
- Self's `navigation.state` (`"anchored"`) comes from the
  `signalk-autostate` plugin; an AIS target's comes straight off its
  transmitted nav status (`$source: "Vesper_Cortex.AI"`, the AIS receiver).
  They are the same field but two different producers, which is why the
  classifier's "our type" and "her type" mappings are separate (ADR 0098).
- **Angles are radians, speeds are metres per second**, as everywhere else in
  this tree — `courseOverGroundTrue`, `headingTrue` and `angleApparent` all
  carry `"units": "rad"` in their `meta`; `speedOverGround` carries `"units":
  "m/s"`.
- `environment.wind.angleApparent`'s meta says "Apparent wind angle, negative
  to port" — confirmed at capture (`-2.46` rad, wind forward and to port).
  Positive is starboard, which is what the tack sign in `collision_colregs.go`
  relies on.
- Oddity worth flagging, not smoothing over: at capture, self's
  `navigation.state` read `"anchored"` while `speedOverGround` read ~5 m/s
  (~9.8 kn) and position had moved several hundred metres between the two
  vessel-tree pulls a minute apart. The boat was underway; autostate's
  classification lagged behind it. The classifier does not trust
  `navigation.state` blindly for this reason among others — it is one input,
  gated by the same SOG-below-1kt no-line rule as everything else.
