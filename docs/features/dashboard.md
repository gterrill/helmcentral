# The dashboard

## Connection loss

If the dashboard loses its connection to Helmcentral, a non-dismissible
**Server connection unavailable** banner appears above the page content,
including on phones. Retained telemetry, anchor-watch state and alarm status
may be out of date; they are not confirmation of the vessel's current state.

The dashboard reconnects automatically. Explicit connection errors and the
browser's offline signal show the warning immediately. A connection that
silently stops receiving events is detected after 45 seconds. Returning to
the page checks elapsed time again, so suspended phone timers do not hide a
stale connection. The warning clears only when an event arrives from the
server, not merely when Wi-Fi reconnects or a connection opens.

This warning describes the browser-to-Helmcentral connection. SignalK source
outages are separate. Server-side alarms can continue while a phone cannot
reach the dashboard, but that phone cannot confirm their current status.

## Widgets

Twenty-one built-in widgets: Vessel, Apparent Wind, Depth & Tide, Position,
Today & Now, Anchor Watch, Tanks, Route, Nearby Vessels, Radar Targets,
Battery & Power, Solar, Alternator, Generator, Switches, Hot Water,
Autopilot, Clock, Current Conditions, Forecast and Sea State.

The last four are built for the wall display: a clock with sunrise, sunset,
moon phase and a route's ETA to its next waypoint; depth, apparent wind and
outside temperature each read against today's forecast range; five days of
condition, high and low; and a five-day wind-and-wave chart. Nothing stops
you placing them on an ordinary phone or tablet page too, but they were
sized for the kiosk's seven-row fold first. See [Set up a wall
display](../how-to/set-up-a-wall-display.md).

Three additional widget types are configurable:

- **Gauge widgets** bind to any path your SignalK server publishes, with a
  numeric, radial, bar, lamp or trend display. Their coloured bands reuse the
  alarm severities, and a band set into the alarm range defines an alarm rule.
  Dragging engine temperature into the red raises an alarm. Readings such as
  oil pressure or engine hours can use gauges without a dedicated widget.
- **Embed tiles** put any URL in the grid: a Grafana panel, a camera feed, a
  windrose. Tick **Frameless** in the embed's settings to drop its title bar
  and padding so the embedded page fills the whole tile, meant for a wall
  display page with no other chrome around it, such as a camera feed; on the
  ordinary dashboard leave it framed so the gear icon and title stay
  reachable. Layout mode always shows the frame regardless of this setting,
  so an embed is never unreachable to reconfigure.
- **Nearby map** widgets show points of interest around the vessel: bays,
  islands, marinas, fuel, boat ramps, moorings, historic landmarks,
  lookouts, dive and snorkel spots, and walking trails. See [Nearby
  map](#nearby-map) below.

## The indicator ribbon

One strip of status lamps and a CHK rollup, pinned above the grid on every
dashboard page, inside whatever skin that page uses. It is vessel-level: the
same strip, the same order, wherever you look, so a glance always means the
same thing. It does not appear on the Forecast, Routes, Charts, Radar, Anchor
Watch or Settings panels; those rely on the alarm banner instead. It also
does not appear on the wall display (`/kiosk`): on a 360px-tall strip it
costs about a third of the height for the least page-specific information on
screen. A wall page that wants status lamps of its own adds a lamp-strip
widget directly to that page instead of relying on the pinned ribbon.

Its order never changes on its own. A lamp that trips does not jump to the
front and a lamp that clears does not disappear, because the whole point of a
ribbon is that its layout is something you can memorize. Triage is the alarm
banner's job instead: it lists a count for each severity currently active,
worst first, then names the alarms in that same worst-first order.

The ribbon is one strip for the whole vessel. If a page needs its own
additional status lamps alongside it, add a separate lamp strip widget to
that page in layout mode. It configures the same way and does not affect the
pinned ribbon.

Opening the ribbon dialog for the first time fills it with lamps resolved
against the paths your own vessel is currently publishing, such as each
engine's revolutions and the generator's state. Nothing is saved until you
choose Save.

To add, edit or remove the ribbon, see [Pin an indicator
ribbon](../how-to/pin-an-indicator-ribbon.md).

## Arranging it

Toggle layout mode in the header, then drag, resize or remove widgets and add
them back from a picker.

Layouts are named **pages** you switch between, for example "Anchored" and
"Underway". Everything is persisted server-side and
restored next session, so a tablet at the helm and a phone in a bunk see the
same arrangement.

Page order is shared too: the sidebar and page dropdown follow the same saved
sequence. New pages appear at the end. Moving a page keeps it selected and
does not change its widgets. See [Reorder dashboard pages](../how-to/reorder-dashboard-pages.md).

The layout adapts to screen size, including phones, with three structurally
different arrangements.

A page can also nominate one placed widget as its **hero**: pick it from the
select next to the skin picker in layout mode, and it moves to its own
full-width row above the rest of the grid, enlarged. Everything else on the
page keeps the position you gave it; picking a different hero, or clearing it
back to "No hero", never rearranges anything else. Use it to emphasise the
main reading for a page, such as anchor distance on an anchorage page or
apparent wind on a passage page.

## The kiosk feed

Any page can also join the wall display's rotation. In layout mode, tick
**Kiosk** next to the page's skin and hero controls, set how many seconds it
shows (5 to 3600), and choose a condition: **Always**, or **While anchored**
to only show the page when the anchor watch is active. Unticking the box
keeps the duration and condition it had, so re-ticking it later remembers
both.

A flagged page keeps its place in the ordinary page list; it is not moved
anywhere, and nothing about how it renders changes outside the wall display
itself. It gains a small monitor glyph next to its name, showing its
duration, in both the sidebar and the page switcher; a page whose condition
is "While anchored" also carries an anchor glyph.

Opening **Wall display** in the sidebar loads `/kiosk` in a new tab: a
fullscreen, chromeless view that cycles through every flagged page in page
order, in the exact widget arrangement it already has. A page whose
condition is "While anchored" drops out of the rotation the moment the
anchor comes up, and rejoins once it goes down again, mid-lap rather than
only on the next reload. An empty rotation (nothing flagged, or nothing whose
condition currently holds) checks again every 15 seconds rather than sitting
on a blank screen.

The wall display shows a compact status pill for a lost connection or an
active alarm, in place of the full banners the ordinary dashboard uses;
those are absent whenever the feed is connected and quiet. This is meant for
an unattended screen with no interaction of its own; there is no way to
click into a page from `/kiosk`, and it is not part of the app's normal
back/forward navigation.

`/kiosk` never renders the pinned indicator ribbon, whether or not one is
configured, even on a page that shows it everywhere else it's viewed. See
[The indicator ribbon](#the-indicator-ribbon) above. If a wall page needs
lamps, add a lamp-strip widget to that page's own layout; it counts against
the page's row budget and shows up on the fold guide like everything else.

See [Set up a wall display](../how-to/set-up-a-wall-display.md) for
mounting and orientation.

## Linking to a page

The address bar tracks the current panel, so you can link directly to it.

| URL | Opens |
| --- | --- |
| `/` | The dashboard; on initial load, restores this browser's remembered page if available, otherwise the first page. |
| `/dashboard/<page id>` | The dashboard, that page. |
| `/forecast` | The forecast drawer. |
| `/routes` | Route planning. |
| `/charts` | Satellite charts. |
| `/radar` | Radar targets. |
| `/anchor-watch` | Anchor watch. |
| `/alarms` | The alarms panel. |
| `/settings` | Settings, General section. |
| `/settings/<section id>` | Settings, that section. |
| `/kiosk` | The wall display: fullscreen, no sidebar, cycling through every kiosk-flagged page. |
| `/kiosk?rotate=180` | The wall display, rotated 180 degrees for a screen mounted upside down. |

The first page uses `/` as its canonical address. If reordering changes which
page is first, the address adjusts without switching the page you are viewing.
Use `/dashboard/<page id>` to explicitly request a particular page.

Your browser's Back and Forward buttons move between panels the same way they
move between pages on any other site: open Forecast, then Routes, and Back
returns you to Forecast rather than closing the app. If you've made a change
on a Settings page and haven't saved it, Back still asks first, the same
dialog you'd get clicking away in the sidebar.

Tapping an alarm notification on your phone opens the Alarms panel rather
than the dashboard.

## When a source goes quiet

A SignalK server keeps serving the last value it received from a source. If the
feed stops, the number remains unchanged and can be mistaken for a live reading.
For example, solar output frozen at 0 W before sunrise could remain on screen
after the array starts producing power.

The Solar and Battery & Power tiles watch how long it has been since their
source last said anything. Past two minutes the tile dims, its header gains a
`STALE` marker with the age, and every reading on it becomes `—`. The values are
blanked to avoid presenting stale readings as current measurements.

The Solar tile does this per controller as well as for the array as a whole. One
MPPT dropping off marks that row and leaves the rest of the tile reporting
normally, using readings from the controllers that are still live.

Every configurable gauge, gauge group, engine cluster, indicator lamp strip and
the pinned ribbon watches the same way. A standalone gauge dims and blanks
exactly like Solar or Battery & Power. A lamp goes dark with the same age
marker rather than staying lit on an old reading: a generator that shut down
twenty minutes ago must not still show green because the last report it sent
happened to say "running". A gauge group or an engine cluster marks only the
one reading that actually stopped, with a small badge next to its label, and
keeps reporting everything else normally; the tile as a whole only goes stale
once every reading on it has stopped updating, so one dead sensor among
several does not blank a cluster that is otherwise still telling the truth.

A number the dashboard computes from other readings, such as the boat's fuel
economy, is only as current as whichever input it used is oldest. If one
engine's fuel-rate feed stops while the boat is still moving, the economy
figure built from it is being computed from a number that is no longer
arriving, and it goes stale along with that feed rather than continuing to
read as a live measurement.

Ages are measured against the vessel clock at the moment the reading was taken,
so a tablet with a wrong clock or one waking from sleep will not cause false
staleness.

A tile showing `—` with no `STALE` marker means something different: that path
is not being published at all. Check that the source exists in SignalK before
looking for a dead link.

## Tile state

A tile's edge takes the colour of the worst configured alert level any of its
readings currently sits in: sky for alert, amber for warn, red for alarm, a
deeper red wash for emergency. A small dot beside the title carries the same
colour, since a border alone is easy to miss along the edge of a screen.

A tile with every reading in its normal band draws no attention at all, on
the same principle as the rest of the board: colour is spent only when
something needs it. The same is true of a reading that has simply drifted
outside a healthy band with no warn or alarm threshold set for it. Add the
threshold in Settings if you want that reading to raise the edge; until then
it stays quiet rather than turning a normal engine idling with no configured
limits into a permanent amber warning.

A stale tile shows its usual amber staleness treatment instead, whatever band
its last reading was in. A value the tile can no longer vouch for should not
also claim a state.

## Battery & Power

Four cards: State, Net, Solar and Loads, plus a Shore line that only appears
when the shore charger has something to report.

**State** is the big number: state of charge, with a bar underneath and a
line to its right reading "To full", "To 20%" or "To empty" depending on
whether the bank is charging, discharging above a threshold you've set, or
discharging with no threshold set. The number and the bar turn amber, then
red, only when the reading crosses a band you have configured as a battery
alarm rule (see [Set the battery state-of-charge
bands](../how-to/set-battery-state-of-charge-bands.md)). With no rule
configured, this number stays plain amber at any state of charge, because
there is no threshold yet for it to have crossed.

**Net**, **Solar** and the rate under State are teal: energy moving into or
out of the bank. **Loads** is plain text, because it is what the bank is
feeding, not a state of the bank itself.

**Shore** shows the charger's mode and AC input current once the charger
reports anything at all. At anchor with no shore power connected, this line
is absent rather than showing blank fields. If the charger reports an error,
that error always shows, in red, even if nothing else about the charger has
reported yet.

### The Dawn figure

The footer's right side projects the state of charge at sunrise, so you can
tell at a glance whether the bank will hold until morning without doing the
arithmetic yourself. It reads something like:

`Dawn 6:08 · 51% · 4 nights`

`6:08` is tomorrow's sunrise. `51%` is the projected state of charge at that
time. `4 nights` means the figure is built from your boat's own recent
overnight discharge: it takes the typical (median) rate the bank has actually
drawn down over the most recent clean nights it can find, up to seven,
looking back as far as thirty nights, and applies that rate to tonight, after
first running the current rate forward to sunset.

When there isn't yet enough history for that, the same spot instead reads:

`Dawn 6:08 · 61% · at current rate`

This holds the rate the bank is drawing (or charging) right now all the way
through to sunrise. Read that literally: in the early afternoon, with the
panels putting power in, "at current rate" can read as high as 100%, because
it is projecting an afternoon charging rate across hours you will actually
spend discharging overnight. It becomes an accurate night estimate on its
own once the sun goes down, because at that point the current rate *is* the
night's rate.

The tile switches from "at current rate" to a night count on its own, with
nothing for you to configure, once your boat has at least two clean nights of
state-of-charge history in InfluxDB. A "clean" night is one with no shore
power on the charger and no generator run at any point between sunset and
sunrise, judged from those two signals directly, and with enough samples to
trust the slope. A night in a marina, a generator night or a gappy night is
skipped rather than averaged in, and a night where the bank somehow rose is
skipped too. Hovering the figure shows how many nights were skipped and why.

While the charger is on shore power tonight, the line reads `Dawn 6:08 · on
shore power` instead of a percentage: the bank is being held, and projecting
a discharge would be wrong in the same way the skipped marina nights were.

If the boat's position is not known, no sunrise can be computed and the Dawn
segment is left out altogether. If the feed has gone stale, or the live-rate
fallback has been switched off and there is not enough history, it reads a
dash. On a screen with a mouse, hovering the figure shows the reason.

The estimate itself also follows the alarm bands: if the projected percentage
falls into a band you've set, it takes that band's colour, which is your cue
to run the generator or plug in before dark rather than finding out at 3 AM.

## Tanks

Below the tank bars, a footer with three fuel figures appears once at least
one fuel tank is configured: **Fuel aboard**, **Range at current burn** and
**Time to empty**.

Fuel aboard sums the level and capacity of every fuel tank that reports both,
in litres. Range at current burn and time to empty both read "at current
burn" on purpose: they are built from the boat's speed and its engines' fuel
burn as they stand this moment, with no averaging over time. Slow down or
speed up and the figures move immediately to match. Read them as what the
boat could do if nothing changed from here, not as a trip plan.

Range and time to empty need the boat to actually be burning fuel, so both
sit blank while stopped or with the engines off. Fuel aboard keeps reporting
regardless, since a tank's level does not depend on an engine running.

If an engine's fuel-rate reading stops updating, range and time to empty
blank out with a small stale marker next to the label rather than continuing
to show a number built from a burn rate that stopped being true hours ago.
Fuel aboard carries the same treatment against its own tank readings: a tank
sensor that has gone quiet blanks that figure too, rather than showing a
level from before it stopped reporting.

## Nearby map

A moving map centred on the vessel, showing the points of interest within a
range you set. Add it from the widget picker like an embed or a gauge; each
instance keeps its own range, categories, layout and toggles, so one page can
show anchorages and dive spots while another shows fuel and boat ramps.

Two layouts: map only, or map and list. The list ranks the five nearest
matches by distance, each with its category icon, name, distance and
bearing, and a one-line description when one is available. The same five
carry a numbered badge on the map so the list and the chart agree on which
marker is which; every matching feature within range still shows on the map,
badge or no badge.

Categories: anchorages, bays and islands; marinas, fuel, boat ramps and
moorings; historic landmarks; lookouts; dive and snorkel spots; and walking
trails. Coverage depends on how thoroughly your cruising ground has been
mapped in the underlying data source, and varies by category: mooring fields
and fuel docks are usually well tagged, dive and snorkel spots much less so.

Two switches control what else the map shows: nearby AIS traffic, and your
own vessel's trail. Both default on for AIS and off for the trail; turn
either off if the map feels busy at a low zoom.

The map follows the vessel, holding north up. If the position feed reports a
GPS fix it does not trust, the map holds its last known position instead of
jumping to an unreliable one, and shows a small amber GNSS badge until a good
fix returns.

A footer under the list names the current data provider and how old its
answer is. If a fetch fails, the list keeps showing what it last knew, aged
and marked with the error, rather than going blank or inventing a result.
The wait for a fresh answer backs off automatically while the provider is
unavailable, so a slow patch of coastline does not turn into a hammering
retry loop.

See [Add a Nearby map](../how-to/add-a-nearby-map.md) for setup, and
[POI categories](../reference/poi-categories.md) for what each category
actually matches.

## Beyond the grid

### Anchor watch and the rode planner

Its own page. See [Anchor watch](anchor-watch.md).

### Route planning

Multi-leg waypoint sequences with per-leg distance, bearing and ETA. A saved
route can be activated, which pushes it to SignalK as the vessel's active route
for autopilots and MFDs to follow.

This supports manual waypoint planning only. It provides no hazard avoidance,
weather routing or live navigation, and has no chart licensing dependency.

### Satellite charts

Upload your own MBTiles and Helmcentral serves them. It never fetches or
bulk-caches tiles from a live provider, which keeps it clear of the licensing
terms that make redistributing somebody else's imagery a problem.

### Autopilot

Engage and disengage, mode, heading nudges, tack and gybe either way, and dodge.
It speaks SignalK's v2 Autopilot API only, with no fallback to writing the
legacy `steering.autopilot.*` paths.

The tile shows the pilot's last reported state from the delta stream, not the
state requested by a command. It greys itself if steering data goes quiet and
disables, without hiding, any action the connected pilot is not currently
advertising. Anything that changes who is steering requires a press-and-hold.

### Radar targets

ARPA contacts from the ship's own radar, if you run one. They plot on the anchor
map as course-oriented triangles, distinct from the AIS circles, and list with
range, bearing and the CPA and TCPA the radar computed.

This is the marine radar. The **weather radar** is a separate embedded
Windy map centred on the vessel.

Radar is optional and stays off unless the pieces are there. Targets appear once
mayara-server is running against the radar and the mayara plugin is installed in
SignalK. Helmcentral needs no configuration of its own and detects the radar
from the SignalK stream. Without both pieces the radar tile reads `--`.

### Max wind gust

Over a window you pick per readout: 10m, 30m, 1h or 24h.
