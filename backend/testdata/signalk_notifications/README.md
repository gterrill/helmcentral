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
