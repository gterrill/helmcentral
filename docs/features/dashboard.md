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
