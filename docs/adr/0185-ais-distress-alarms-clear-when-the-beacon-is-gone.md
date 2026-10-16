# ADR 0185: AIS Distress Alarms Clear When The Beacon Is Gone

## Status

Accepted

## Context

The `signalk-ais-distress` plugin raises `notifications.received.distress.<stamp>-<MMSI>`
when it hears a distress beacon (a SART, MOB or EPIRB transmitting over AIS)
and never withdraws it. The leaf is `state: "emergency"` with `status.canClear:
false`, so nothing on the server side ends it either.

Live evidence, 2026-10-16: a SART (MMSI 970028911) stopped transmitting, and
SignalK no longer held its vessel context, but
`received.distress.ais-2026-10-16T000510416Z-970028911` stayed live. The alarm
list showed it under the humanised path ("Received Distress Ais-2026-10-16
T000510416 Z-970028911"), and the emergency banner's buttons were white text
on a white `bg-card` fill and could not be read.

This is the same shape as ADR 0144: the producer never says the condition
ended, so only the absence of its evidence can.

## Decision

**A `received.distress.ais-*` notification whose key ends in a nine-digit MMSI
is over when its beacon has not been heard for ten minutes.** "Heard" is the
latest receive time of any delta for `vessels.urn:mrn:imo:mmsi:<MMSI>`
(`signalKSnapshot.contextHeard`, zero if the context is absent or evicted),
`distressBeaconGoneAfter`. Measuring from the receive time rather than waiting
for `evictStaleVesselContexts` (an hour) matters: on 2026-10-16 the SART's last
collision alarm was at 01:27Z, SignalK's own reconcile removed the beacon's
notification at 01:35Z, and the context was evicted only at 02:42Z.

**Silence only counts while the feed is healthy.** `distressEvidence` trusts
absence only when the stream is connected, the current connection (reset on
every reconnect, `markStreamConnected`; not `listeningSince`, which is the
first delta of the process) is older than the ten-minute window, and the
stream has delivered a delta within the last minute. During an outage or just
after a reconnect nothing is declared gone.

**`contextHeard` ignores replays.** An update whose own timestamp is more than
ten minutes old (a cached value replayed on reconnect) does not count as the
beacon being heard, the same reasoning as `sentenceSeen` (ADR 0144).

**The decision is latched, and released on fresh evidence.** A label declared
gone stays gone while the feed cannot be trusted, so a reconnect replay cannot
re-raise it. Once the feed is trusted again and the beacon has been heard
within the window, the latch is released and the alarm is live again: a SART
that drifts out of range and returns raises no new key for the same event, so
it would otherwise stay hidden for good. A latch entry is pruned once its
label has left the notifications tree, so the set stays bounded.

**Scope.** Only `received.distress.ais-*` keys that end in an MMSI. Any other
`received.distress.*` key, or one that does not parse, is never suppressed
(fail visible).

**One decision site.** `distressChecker.gone` is called from
`signalKNotifications` (alarm list, `worst`, the bus watcher's
raise/clear/dispatch, so every transport sees an ordinary clear) and
`staleNMEANotificationLabels` (so the watcher logs it). It reads the snapshot
once per call. The watcher logs once, naming the path and the MMSI, and logs
once more when a beacon heard again brings its alarm back.

**Titles.** `received.distress.ais-*` is "AIS Distress"; any other
`received.distress.*` is "Distress Received". The key carries a timestamp and
an MMSI, which stay off the card (ADR 0157); the body is the producer's
sentence.

**Banner buttons.** The alarm banner's buttons carry an explicit
`text-foreground`. The emergency fill sets white text on its container, which
the `outline` variant (no text colour, `bg-card` fill) inherited. The shared
Button variant is unchanged.

## Rejected

**Clearing the notification on the server.** `canClear` is `false`, and
writing `normal` to a path the plugin owns is the mistake ADR 0038 and ADR
0144 already rejected.

**Timing the alarm out from its own timestamp.** The beacon's context is the
evidence of whether the transmission continues; the leaf's timestamp only says
when it was written (ADR 0144).

**Acknowledging or silencing it for the operator.** Helmcentral does not
acknowledge emergencies; unchanged.

**Waiting for context eviction (one hour), or for the process to have listened
an hour.** Too slow for a beacon that is plainly gone, and tied to process
start rather than to the health of the feed.

## Consequences

- A beacon that stops transmitting is withdrawn about ten minutes after it was
  last heard, if the feed is healthy; otherwise the alarm stays until it is.
- A beacon heard again on a healthy feed brings the same alarm back; one
  that transmits a new event raises a new key.
- The latch is in memory: after a restart the reconcile may bring a cleared
  notification back, and it clears again once the feed has been up ten minutes.
