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
