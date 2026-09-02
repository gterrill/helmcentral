# The dashboard

Seventeen built-in widgets: Vessel, Apparent Wind, Depth & Tide, Position,
Today & Now, Anchor Watch, Tanks, Route, Nearby Vessels, Radar Targets,
Battery & Power, Solar, Alternator, Generator, Switches, Hot Water and
Autopilot.

Two more are not fixed:

- **Gauge widgets** bind to any path your SignalK server publishes, with a
  numeric, radial, bar, lamp or trend display. Their coloured bands reuse the
  alarm severities, and a band set into the alarm range *is* the alarm rule, so
  dragging engine temperature into the red raises a real alarm rather than
  colouring a dial. Oil pressure or engine hours do not have to wait for
  somebody to write a widget for them.
- **Embed tiles** put any URL in the grid: a Grafana panel, a camera feed, a
  windrose.

## Arranging it

Toggle layout mode in the header, then drag, resize or remove widgets and add
them back from a picker.

Layouts are named **pages** you switch between, because "Anchored" and
"Underway" want different screens. Everything is persisted server-side and
restored next session, so a tablet at the helm and a phone in a bunk see the
same arrangement.

The layout stays usable down to a phone, with three structurally different
arrangements across the range rather than one grid that squashes.

## When a source goes quiet

A SignalK server keeps serving the last value it received from a source. If the
feed behind that source stops, the number does not disappear, it stops moving,
and a tile that renders it looks exactly like a tile reporting a live reading.
That is harmless for a value you would notice being wrong. It is not harmless
for solar output, where a frozen 0 W from before sunrise is entirely plausible
and will happily sit there all morning while the array works.

So the Solar and Battery & Power tiles watch how long it has been since their
source last said anything. Past two minutes the tile dims, its header gains a
`STALE` marker with the age, and every reading on it becomes `—`. The values are
blanked deliberately: a stale reading presented as a measurement is worse than
no reading at all.

The Solar tile does this per controller as well as for the array as a whole. One
MPPT dropping off marks that row and leaves the rest of the tile reporting
normally, because the other controllers are still live and their total is still
real.

Ages are measured against the vessel clock at the moment the reading was taken,
so a tablet with a wrong clock or one waking from sleep will not cause false
staleness.

A tile showing `—` with no `STALE` marker means something different: that path
is not being published at all. Check that the source exists in SignalK before
looking for a dead link.

## Beyond the grid

### Anchor watch and the rode planner

Its own page. See [Anchor watch](anchor-watch.md).

### Route planning

Multi-leg waypoint sequences with per-leg distance, bearing and ETA. A saved
route can be activated, which pushes it to SignalK as the vessel's active route
for autopilots and MFDs to follow.

This is manual waypoint planning and nothing more. No hazard avoidance, no
weather routing, no chart licensing dependency, and Helmcentral does no live
navigation itself.

### Satellite charts

Upload your own MBTiles and Helmcentral serves them. It never fetches or
bulk-caches tiles from a live provider, which keeps it clear of the licensing
terms that make redistributing somebody else's imagery a problem.

### Autopilot

Engage and disengage, mode, heading nudges, tack and gybe either way, and dodge.
It speaks SignalK's v2 Autopilot API only, with no fallback to writing the
legacy `steering.autopilot.*` paths.

The tile shows what the pilot last reported on the delta stream, never what a
command asked for, so it cannot show you an engagement that did not happen. It
greys itself if steering data goes quiet, and disables rather than hides any
action the connected pilot is not currently advertising, so a missing button is
visibly missing. Anything that changes who is steering takes a deliberate
press-and-hold.

### Radar targets

ARPA contacts from the ship's own radar, if you run one. They plot on the anchor
map as course-oriented triangles, distinct from the AIS circles, and list with
range, bearing and the CPA and TCPA the radar computed.

This is the marine radar. The **weather radar** is a separate thing: an embedded
Windy map centred on the vessel.

Radar is optional and stays off unless the pieces are there. Targets appear once
mayara-server is running against the radar and the mayara plugin is installed in
SignalK. Helmcentral needs no configuration of its own and detects the radar
from the SignalK stream. Without both pieces the radar tile reads `--`.

### Max wind gust

Over a window you pick per readout: 10m, 30m, 1h or 24h.
