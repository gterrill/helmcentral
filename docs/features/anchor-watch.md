# Anchor watch

Trail sampling and drag detection both run on the server, for your own vessel
and for nearby AIS targets.

Drag detection continues when the browser is closed. A drag is an ordinary
alarm: it is logged, can be acknowledged, and uses the transports you have
configured. Acknowledgement is server-side, so silencing an alarm on your phone
also silences it in a browser left open in the saloon.

A lost GNSS fix never raises a drag. Position freezes at its last trusted value
during an outage rather than wandering, and the stream watchdog reports the
outage separately, as its own alarm. A drag alert and a lost GPS fix require
different responses.

## Drop, Raise, and SignalK

**Drop** saves the watch and publishes its anchor coordinates to SignalK's
`navigation.anchor.position`. These are the stored coordinates, including the
bow offset when applied. Repositioning the anchor marker republishes the
corrected coordinates; changing radius or rode settings does not.

**Raise** publishes an explicit `null` at that path, then removes the local
watch, trail and session pins. This releases SignalK Auto-state's anchored
state. Both operations check SignalK's model before reporting success and use
the saved SignalK service credentials, independently of alarm transport settings.

If synchronization fails, an error is shown. A failed Drop/reposition leaves
the saved local watch active for safety; repeat Drop/reposition to synchronize.
A failed Raise publication retains the local watch; retry Raise. If SignalK
accepted Raise but removing the local watch failed, the error says so and Raise
can be retried. Restarting Helmcentral does not republish or clear anchor state.

## Automatic closure

When enabled, automatic closure requires all of the following for five
continuous seconds:

- At least one main engine reports a finite RPM above zero.
- A valid, non-critical GNSS position puts the vessel outside the watch radius
	plus 4.572 metres (15 feet).
- The anchor watch is active.

It does **not** wait for SignalK's navigation-state label to become `motoring`.
The RPM and position readings still arrive through SignalK. Missing engine or
position evidence does not trigger closure. A running engine suppresses only
the GNSS validator's stationary-only movement/depth heuristics; fix quality,
data-age and recovery checks still apply.

Automatic closure uses the same Raise operation and SignalK confirmation.
Failure shows an error rather than repeatedly sending requests; retry Raise
manually, or let the departure conditions reset before another automatic attempt.
Automatic closure requires an open dashboard. Server-side drag detection does
not, and remains active until the watch is successfully removed.

## The rode planner

A sidebar on the anchor watch page for pay-out and swing-radius planning,
against tide-corrected depth and gust-seeded wind. It can be used before
lowering the anchor.

Its map takes pins: click open water to mark a bombie, a mooring block or the
nearest bit of shoreline, and watch the range close as you swing. Pins are
shared across every device watching the anchorage, so marking a hazard from the
bow shows up on the tablet at the helm. They expire with the anchoring rather
than accumulating across seasons.

The map keeps the view you panned to for as long as you stay in the anchorage,
including across updates. Anchoring somewhere else recentres every device on
the new anchor.
