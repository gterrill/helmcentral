# The dashboard

The dashboard is the screen you watch while the boat is working. It holds the
readings that matter for whatever you are doing right now: depth and wind
while you are conning, battery state and solar yield overnight at anchor,
engine temperature and fuel burn on passage. You lay each screen out
yourself, so the numbers in front of you are the ones you actually need.

Each layout is a **page**, and pages are how you switch jobs. Most boats end
up with a handful: Anchored, Underway, an engine watch. Every page is a grid
of instrument tiles reading live off the boat's own instrument network.

## Connection loss

Lose the link between this screen and Helmcentral and a **Server connection
unavailable** banner takes the top of the page, on a phone as well as at the
helm. You cannot dismiss it. Everything below it is the last thing that
arrived: telemetry, anchor-watch state and alarm status alike. Read them as
history, not as the state of the boat right now.

The dashboard reconnects on its own. It raises the warning immediately when
the link fails outright or the device reports itself offline. A link that
goes quiet without failing takes 45 seconds to catch, and coming back to the
screen checks the clock again, so a phone that slept through an outage does
not wake showing a stale screen as though it were live. Only a fresh reading
clears the warning.

That banner tracks one thing: this screen's link to Helmcentral. A source
dropping off the boat's own network is a different problem, covered under
[When a source goes quiet](#when-a-source-goes-quiet). Alarms carry on
running on the Helmcentral box while a phone cannot reach it; the phone
simply cannot tell you what they are doing.

## Instrument tiles

Twenty-one built-in tiles cover the boat's core systems, from Vessel and
Apparent Wind through Battery & Power, Autopilot and Forecast. Six more
types you build yourself, among them a **gauge** for any single reading with
no dedicated tile of its own, an **embed** for a URL such as a camera feed or
a Grafana panel, and a **Nearby map** for the points of interest around the
vessel (see [Nearby map](#nearby-map) below). See [Tiles](../reference/tiles.md)
for the full catalog and every tile's fields.

## The indicator ribbon

One strip of status lamps and a CHK rollup, pinned above the grid on every
dashboard page, inside whatever skin that page uses. The strip belongs to the
vessel, not to the page: same lamps, same order, wherever you look, so a
glance always means the same thing. It stays off the Forecast, Routes, Radar,
Anchor Watch and Settings panels, which rely on the alarm banner instead, and
off wall displays, where it would spend a third of a 360px-tall strip on the
least page-specific information on screen.

Its order never changes on its own: a lamp that trips does not jump to the
front, and a lamp that clears does not vanish, because a ribbon earns its
place only if you can memorize it. Triage belongs to the alarm banner
instead, which counts each severity currently active, worst first.

If one page wants status lamps beyond the vessel-wide set, add an indicator
tile to that page directly. It configures the same way and leaves the pinned
ribbon alone. See [Pin an indicator ribbon](../how-to/pin-an-indicator-ribbon.md).

## Arranging it

Toggle layout mode in the header to drag, resize, remove and add tiles, name
or delete the page, switch its skin, put it on a wall display, or promote one
tile to **hero**: an enlarged, full-width row above the rest of the grid,
useful for the one reading a page exists for, such as anchor distance on an
anchorage page. Everything else keeps the position you gave it.

Helmcentral saves everything on the box as you make each change, not in the
browser, so a tablet at the helm and a phone in a bunk show the same
arrangement and find it again next session. Page order is shared the same
way: the sidebar and the page dropdown follow one saved sequence.

The grid itself shows and updates live on any screen, but layout mode, the
toolbar and the tile picker need a screen at least 1024px wide, so a laptop
or a landscape tablet.

See [Create a dashboard page](../how-to/create-a-dashboard-page.md) for the
full walkthrough, including renaming and deleting, and [Reorder dashboard
pages](../how-to/reorder-dashboard-pages.md) for changing the sequence.

## Wall displays

A wall display is a screen somewhere on the boat that shows the dashboard and
nothing else: no sidebar, no header, no way to click into anything. It cycles
through the pages you give it, in page order, and runs unattended. You can
have several, and they do not have to be alike: a 1920 by 360 strip at the
flybridge helm and a 55 inch television in the saloon are both wall displays,
each with its own pages.

A page belongs to one display at a time. A board laid out for a helm strip is
not a board you want filling a television, so a page you like moves to
another screen as a duplicate you then rearrange for its new shape, and it
loses its hero tile once it is on a display, since the hero's extra row
spends the vertical room the wall is measuring. Deleting a display never
deletes its pages; they return to the ordinary page list.

The pinned indicator ribbon never draws on a wall display, whatever page is
showing. If a page needs status lamps there, it carries its own indicator
tile. A wall display always runs the dark theme, whatever this browser has
stored for the ordinary dashboard.

See [Set up a wall display](../how-to/set-up-a-wall-display.md) for the
device check, mounting and adding a screen, and [Tiles](../reference/tiles.md#wall-display-fields)
for what each field on a display's record does.

## Linking to a page

The address bar follows the panel you are on, so you can link straight to it.

| URL | Opens |
| --- | --- |
| `/` | The dashboard; on first load, this browser's remembered page if it still exists, otherwise the first page. |
| `/dashboard/<page id>` | The dashboard, that page. |
| `/forecast` | The forecast drawer. |
| `/routes` | Route planning. |
| `/radar` | Radar targets. |
| `/anchor-watch` | Anchor watch. |
| `/alarms` | The alarms panel. |
| `/settings` | Settings, General section. |
| `/settings/<section id>` | Settings, that section. |
| `/display/<address>` | That wall display: fullscreen, no sidebar, cycling the pages assigned to it. |

The first page answers to `/` as its canonical address; reorder the pages so
a different one comes first and the address follows, without switching the
page you are looking at.

Back and Forward move between panels the way they move between pages on any
other site: open Forecast, then Routes, and Back returns you to Forecast
rather than closing the app. Tapping an alarm notification on your phone
opens the Alarms panel, not the dashboard.

## When a source goes quiet

The boat's instrument network keeps serving the last value a source sent it.
Stop the feed and the number sits there unchanged, easily mistaken for a live
reading: solar output frozen at 0 W before sunrise can still read 0 W an hour
after the array started producing.

Every tile watches how long it has been since its source last said anything.
Past two minutes it dims, its header gains a `STALE` marker with the age, and
its readings go to `—`, because a stale reading shown as current is worse
than no reading at all. A tile reading `—` with no `STALE` marker means
something else entirely: that reading is not being published at all, worth
checking on the source before you go hunting a dead link. See
[Staleness](../reference/tiles.md#staleness) for how this plays out on
Solar, gauge groups and engine clusters, and on numbers the dashboard works
out from more than one reading.

## Tile state

A tile's edge takes the colour of the worst alert level any of its readings
currently sits in: sky for alert, amber for warn, red for alarm, a deeper red
wash for emergency. A dot beside the title carries the same colour, since a
border alone is easy to miss along the edge of a screen. A tile with every
reading in its normal band draws no attention at all, on the same principle
as the rest of the board: colour is spent only where something needs it. A
reading with no warn or alarm threshold set for it stays quiet too, until you
set one in Settings. See [Tile state colours](../reference/tiles.md#tile-state-colours)
for the full rule, including how a stale tile overrides its band colour.

## Battery & Power

Four cards, State, Net, Solar and Loads, plus a Shore line once the shore
charger has something to report. State is the big number, state of charge,
with a bar underneath; neither turns amber or red until you set a matching
alarm rule, see [Set the battery state-of-charge
bands](../how-to/set-battery-state-of-charge-bands.md). The footer projects
the same state of charge at sunrise, the **Dawn figure**, so you can tell at
a glance whether the bank holds until morning. See [Battery & Power
fields](../reference/tiles.md#battery--power-fields) for what each card
shows and how the Dawn figure is worked out.

## Tanks

Below the tank bars, a footer adds three fuel figures once at least one tank
is configured: fuel aboard, and range and time to empty at the current burn
rate. Any of the three goes blank, rather than showing a plausible-looking
number built on a reading that stopped arriving, if the figure behind it
stops updating. See [Tanks footer
fields](../reference/tiles.md#tanks-footer-fields) for exactly what each one
sums and when it goes blank.

## Nearby map

A moving map centred on the vessel, showing the points of interest within a
range you set: anchorages, marinas, fuel, dive sites and more. Add it from
the tile picker like an embed or a gauge, and add more than one to a page, or
to different pages, each with its own range and categories. Two switches
control nearby AIS traffic and your own vessel's trail, and a footer names
the current data provider and how old its answer is, aging visibly rather
than going blank when a fetch fails.

The map keeps the vessel and the five nearest matches in view, but never
zooms in tighter than the range. In the map-and-list layout one row at a time
shows its description, moving to the next described match every few seconds,
and that match's marker gets a ring. An active route shows as a line, with
the leg you're on drawn brighter.

See [Add a Nearby map](../how-to/add-a-nearby-map.md) for setup, and [POI
categories](../reference/poi-categories.md) for what each category matches
and how well your cruising ground is likely to be mapped.

## Beyond the grid

### Anchor watch and the rode planner

Its own page. See [Anchor watch](anchor-watch.md).

### Route planning

Multi-leg waypoint sequences with per-leg distance, bearing and ETA. Activate
a saved route and Helmcentral pushes it out as the vessel's active route, for
autopilots and MFDs to follow.

You plan the waypoints yourself. Helmcentral offers no hazard avoidance, no
weather routing and no live navigation, and it needs no chart licence.

### Autopilot

Engage and disengage, mode, heading nudges, tack and gybe either way, and
dodge. Helmcentral drives pilots through the modern autopilot interface only;
a pilot offering nothing but the older legacy steering controls will not
respond, and Helmcentral will not quietly fall back to them.

The tile reports what the pilot last said about itself, never what your last
press asked for. It greys itself out if steering data goes quiet, and it
disables, without hiding, any action the connected pilot is not currently
advertising. Anything that changes who is steering takes a press and hold.

### Radar targets

ARPA contacts from the ship's own radar, if you run one. They plot on the
anchor map as course-oriented triangles, distinct from the AIS circles, and
list with range, bearing and the CPA and TCPA the radar computed.

This is the marine radar. The **weather radar** is a separate embedded Windy
map centred on the vessel.

Radar is optional and stays off unless the pieces are there: mayara-server
running against the physical radar, and its plugin installed on the boat's
instrument server. Helmcentral needs no configuration of its own beyond
that and finds the radar on the feed; without both pieces the tile reads
`—`.

### Max wind gust

Over a window you pick per readout: 10m, 30m, 1h or 24h.
