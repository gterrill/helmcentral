# Anchor watch

Trail sampling and drag detection both run on the server, for your own vessel
and for nearby AIS targets.

That placement is the whole point. Closing the browser cannot silence a drag,
because the detection was never in the browser. A drag is an ordinary alarm: it
is logged, it is acknowledgeable, and it goes off the boat by whatever
transports you have configured. Silencing it is a server-side acknowledgement,
so a second browser left open in the saloon is not still ringing after you
dealt with it on your phone.

A lost GNSS fix never raises a drag. Position freezes at its last trusted value
during an outage rather than wandering, and the stream watchdog reports the
outage separately, as its own alarm. A dragging alert and a dead GPS are
different problems and want different reactions at 3am.

## The rode planner

A sidebar on the anchor watch page for pay-out and swing-radius planning,
against tide-corrected depth and gust-seeded wind. It works before the anchor is
down, which is when the decision actually gets made.

Its map takes pins: click open water to mark a bombie, a mooring block or the
nearest bit of shoreline, and watch the range close as you swing. Pins are
shared across every device watching the anchorage, so marking a hazard from the
bow shows up on the tablet at the helm. They expire with the anchoring rather
than accumulating across seasons.

The map keeps the view you panned to for as long as you stay in the anchorage,
so a chart you positioned deliberately is not snatched back on the next update.
Drop somewhere else and every device recentres on the new anchor.
