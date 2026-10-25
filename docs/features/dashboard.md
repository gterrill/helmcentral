# The dashboard

The dashboard is the screen you look at whilst the boat is in operation. It
shows the readings that are relevant to what you're doing at that moment. That
means depth and wind when you're conning, battery state and solar yield over an
overnight anchor, and engine temperature and fuel burn on passage. You set up
the screens yourself so that the readings in front of you are the ones that
actually matter.

Each layout is a **page**, and pages are how you switch jobs. Most boats end up
with a handful: Anchored, Underway, an engine watch. Every page is a grid of
instrument tiles reading live off the boat's own instrument network.

## Connection loss

If this screen loses its link to Helmcentral, a **Server connection
unavailable** banner appears across the top of the page, on a phone as well as
at the helm. You can't dismiss it. Everything below it is the last thing that
arrived, whether that's telemetry, anchor-watch state or alarm status. Read it
as history, not as the state of the boat right now.

The dashboard reconnects on its own. Here is how it spots a lost link and how
the warning clears:

- **Instant warning:** If the link fails outright, or the device reports itself
  offline, the banner appears immediately.
- **Silent drops:** If the link goes quiet without failing, it takes 45 seconds
  to catch.
- **Waking up your device:** When you come back to the screen, the dashboard
  checks the clock again. A phone that slept through an outage therefore
  doesn't wake up showing a stale screen as though it were live.
- **Clearing the warning:** Only a fresh reading clears it.

The banner tracks one thing, which is this screen's link to Helmcentral. A
source dropping off the boat's own network is a different problem, covered
under [When a source goes quiet](#when-a-source-goes-quiet). Alarms carry on
running on the Helmcentral box while a phone can't reach it. The phone just
can't tell you what they're doing.

## Instrument tiles

Twenty built-in tiles cover the boat's core systems, from Vessel and Wind
through Battery & Power, Autopilot and Forecast Conditions. You can also build
six more types yourself, including these:

- **Gauge:** Shows any single reading that has no dedicated tile of its own.
- **Engine cluster and gauge group:** Each shows one piece of your equipment.
- **Embed:** Shows a URL, such as a camera feed or a Grafana panel.
- **Nearby map:** Shows the points of interest around the vessel (see [Nearby
  map](#nearby-map) below).

See [Tiles](../reference/tiles.md) for the full catalogue and every tile's
fields.

## The indicator ribbon

The indicator ribbon is one strip of labelled status cells and a CHK cell,
pinned above the grid on every dashboard page, inside whatever skin that page
uses. The strip belongs to the vessel, not to the page. It has the same lamps in
the same order wherever you look, so a glance always means the same thing.

It stays off the Forecast, Radar, Anchor Watch and Settings panels, which rely
on the alarm banner instead. It also stays off wall displays, where it would
spend a third of a 360px-tall strip on the least page-specific information on
screen.

Each cell shows an icon, the full name of the system and a short reading: On,
Off, or `--` when nothing is reporting.

- **Colour:** A cell fills green when the system is on and stays dark when it's
  off.
- **No data:** A dashed outline means the boat isn't getting data for it, which
  isn't the same as off. A source that goes quiet never keeps showing its last
  "on".
- **Alarms:** While an active alarm is on a cell's signal, the cell turns amber
  or red to match the alarm, whether the system is running or not. That way the
  ribbon never disagrees with the alarm card. Tapping a cell with an alarm opens
  the alarms panel.
- **Groups:** Cells can be given a group such as Propulsion or Power, and
  neighbouring cells in the same group share one heading.
- **Narrow screens:** The row wraps onto more lines on a narrow screen instead
  of scrolling.

The CHK cell sits first. When it's quiet, it reads CHK. With alarms active it
widens, takes the colour of the worst one, and reads the number active and that
alarm's title.

The order never changes on its own. A lamp that trips doesn't jump to the
front, and a lamp that clears doesn't vanish, because a ribbon only earns its
place if you can memorise it. Sorting out which alarm matters most is the alarm
banner's job, and it counts each severity currently active, worst first.

If one page wants status lamps beyond the vessel-wide set, add an indicator
tile to that page directly. It's configured the same way and leaves the pinned
ribbon alone. See [Pin an indicator
ribbon](../how-to/pin-an-indicator-ribbon.md).

## Day, night and auto

The button at the right of the header sets the theme for this browser. Each
press steps Day, then Night, then Auto. Auto shows the night theme from sunset
to sunrise and the day theme the rest of the time, using the sunrise and sunset
on the Clock tile. A helm display therefore dims itself at dusk without anyone
touching it. The button reads "Auto" with a sun or moon for the theme showing
now, and its hover text gives the time the theme ends.

Auto needs a forecast with sunrise and sunset. Without one it keeps the theme
that was showing and doesn't guess. The hover text then says sunrise and sunset
aren't available. Day or Night will hold the theme you want until the forecast
returns.

## Arranging it

Toggle layout mode in the header to drag, resize, remove and add tiles, name or
delete the page, switch its skin, or put it on a wall display. You can also
promote one tile to **hero**, which is an enlarged, full-width row above the
rest of the grid. It's useful for the one reading a page exists for, such as
anchor distance on an anchorage page. Everything else keeps the position you
gave it.

The Wind and Engine Cluster tiles are drawn at a fixed proportion and fill
whatever size you give them, centred in the tile, up to twice their usual size.
Drag one larger and the dial and its readings grow with it. Make it smaller and
they shrink. On a phone or a narrow window they fit the width of the screen as
before.

Nearby Vessels and Radar Targets are lists that grow and shrink as contacts
come and go. How tall one is drawn depends on what's under it:

- **Lowest in its column:** When nothing is below it, the tile is drawn only as
  tall as its list needs, down to a header and one line. It's never taller than
  the height you set, and the space below it is left empty rather than padded.
- **Another tile underneath:** The tile keeps the height you gave it, so nothing
  on the page shifts as a contact appears or leaves.
- **Layout mode:** Every tile shows the height you set, so dragging and resizing
  work on your own numbers.

Helmcentral saves everything on the box as you make each change, not in the
browser. A tablet at the helm and a phone in a bunk therefore show the same
arrangement and find it again next session. Page order is shared the same way:
the sidebar and the page dropdown follow one saved sequence.

The grid itself shows and updates live on any screen. Layout mode, the toolbar
and the tile picker need a screen at least 1024px wide, so a laptop or a
landscape tablet.

See [Create a dashboard page](../how-to/create-a-dashboard-page.md) for the
full walkthrough, including renaming and deleting, and [Reorder dashboard
pages](../how-to/reorder-dashboard-pages.md) for changing the sequence.

## Full screen

Tap the full screen button (four outward corners) in the header and the
sidebar, header and browser bars all go, leaving the tiles on the whole screen.
A tablet mounted at the helm or a screen at the nav station then reads like a
dedicated instrument. Here is how full screen behaves:

- **Alarms:** A live alarm still shows over the grid, because full screen never
  quiets one.
- **Coming back:** To bring the sidebar and header back, press Esc. On a screen
  with no keyboard, tap the small exit button that appears in the corner.
  Following an alarm banner into the alarms list also brings them back, since
  you need the sidebar to get anywhere else from there.
- **Swiping between pages:** On a touchscreen, swipe left or right anywhere on
  the grid to move to the next or previous page. They're in the same order as
  they're listed normally. A page name and its position ("2 / 4") show briefly
  in the middle of the screen, so you know where you landed. A map or a chart
  keeps its own drag for panning or reading values off it. Swiping across one of
  those moves the map or the chart instead of the page.
- **Keeping the screen awake:** While full screen is on, the screen stays awake
  instead of dimming and locking on its usual timeout. That only works if you
  reached Helmcentral by an `https://` address (the Tailscale name, for
  example). Over a plain `http://` address the browser won't hold the screen on,
  and it sleeps as it normally would. A phone in low-power mode may still dim it.
- **iPhone and Home Screen:** Full screen isn't offered on an iPhone, which
  doesn't support it, or in Helmcentral opened from an iPad's Home Screen icon.
  A Home Screen copy on either already fills the screen with no browser bars,
  and it keeps the screen awake while it shows the dashboard, again only over an
  `https://` address. Open another section, such as Settings or Forecast, and
  the screen sleeps on its usual timeout again.

## Wall displays

A wall display is a screen somewhere on the boat that shows the dashboard and
nothing else: no sidebar, no header, no way to click into anything. It cycles
through the pages you give it, in page order, and runs unattended. You can have
several, and they don't have to be alike. A 1920 by 360 strip at the flybridge
helm and a 55 inch television in the saloon are both wall displays, each with
its own pages.

- **One display per page:** A page belongs to one display at a time. A board
  laid out for a helm strip isn't a board you want filling a television. So a
  page you like moves to another screen as a duplicate, and you then rearrange it
  for its new shape.
- **No hero tile:** A page loses its hero tile once it's on a display, since the
  hero's extra row spends the vertical room the wall is measuring.
- **Deleting a display:** This never deletes its pages. They return to the
  ordinary page list.
- **No ribbon:** The pinned indicator ribbon never draws on a wall display,
  whatever page is showing. If a page needs status lamps there, it carries its
  own indicator tile.
- **Always dark:** A wall display always runs the dark theme, whatever this
  browser has stored for the ordinary dashboard.

See [Set up a wall display](../how-to/set-up-a-wall-display.md) for the device
check, mounting and adding a screen, and
[Tiles](../reference/tiles.md#wall-display-fields) for what each field on a
display's record does.

## Linking to a page

The address bar follows the panel you are on, so you can link straight to it.

| URL | Opens |
| --- | --- |
| `/` | The dashboard. On first load, this browser's remembered page if it still exists, otherwise the first page. |
| `/dashboard/<page id>` | The dashboard, that page. |
| `/forecast` | The forecast drawer. |
| `/radar` | Radar targets. |
| `/anchor-watch` | Anchor watch. |
| `/alarms` | The alarms panel. |
| `/settings` | Settings, General section. |
| `/settings/<section id>` | Settings, that section. |
| `/display/<address>` | That wall display: fullscreen, no sidebar, cycling the pages assigned to it. |

The first page answers to `/` as its canonical address. If you reorder the pages
so a different one comes first, the address follows, without switching the page
you're looking at.

Back and Forward move between panels the way they move between pages on any
other site. If you open Forecast, then Radar, Back returns you to Forecast
rather than closing the app. Tapping an alarm notification on your phone opens
the Alarms panel, not the dashboard.

## When a source goes quiet

The boat's instrument network keeps serving the last value a source sent it. If
you stop the feed, the number sits there unchanged, and it's easily mistaken for
a live reading. Solar output frozen at 0 W before sunrise can still read 0 W an
hour after the array started producing.

Every tile watches how long it has been since its source last said anything.
Past two minutes, its header gains a `STALE` marker with the age and the tile
dims. Its readings go to `—`, because a stale reading shown as current is worse
than no reading at all.

A tile reading `—` with no `STALE` marker means something else entirely. That
reading isn't being published at all, so check the source before you go hunting
for a dead link. See [Staleness](../reference/tiles.md#staleness) for how this
plays out on Solar, gauge groups and engine clusters, and on numbers the
dashboard works out from more than one reading.

## Tile state

A tile's edge takes the colour of the worst alert level any of its readings is
currently in. That's sky for alert, amber for warn, red for alarm, and a deeper
red wash for emergency. A dot beside the title carries the same colour, since a
border alone is easy to miss along the edge of a screen.

A tile with every reading in its normal band draws no attention at all, because
colour is spent only where something needs it. A reading with no warn or alarm
threshold set for it stays quiet too, until you set one in Settings. See [Tile
state colours](../reference/tiles.md#tile-state-colours) for the full rule,
including how a stale tile overrides its band colour.

## Battery & Power

The tile has four cards: State, Net, Solar and Loads. A Shore line joins them
once the shore charger has something to report. State is the big number, the
state of charge, with a bar underneath. Neither turns amber or red until you set
a matching alarm rule (see [Set the battery state-of-charge
bands](../how-to/set-battery-state-of-charge-bands.md)).

The footer projects the same state of charge at sunrise, the **Dawn figure**, so
you can tell whether the bank holds until morning. See [Battery & Power
fields](../reference/tiles.md#battery--power-fields) for what each card shows
and how the Dawn figure is worked out.

## Tanks

Each tank gets a bar showing its level in percent, plus a state word that stays
readable in glare. Fuel and water warn as they run low, and waste warns as it
fills.

## Nearby map

The Nearby map is a moving map centred on the vessel. It shows the points of
interest within a range you set: anchorages, marinas, fuel, dive sites and more.
You add it from the tile picker like an embed or a gauge. You can add more than
one to a page, or to different pages, each with its own range and categories.

- **Switches:** Two switches control nearby AIS traffic and your own vessel's
  trail.
- **Footer:** It names the current data provider and how old its answer is. The
  age keeps counting visibly rather than going blank when a fetch fails.
- **Framing:** The map keeps the vessel at the centre with the five nearest
  matches in view, but it never zooms in tighter than the range.
- **Highlighted match:** In the map-and-list layout, one row at a time is
  highlighted. It moves to the next match every few seconds and goes back to the
  first once it reaches the end, and that match's marker gets a ring. The row's
  description shows too when it has one, though many points of interest,
  especially from OpenStreetMap, don't have any.
- **Camera tour:** The map dives in to the first match and tilts, then holds
  there. It flies straight on to each next match in a high arc that rises far
  enough to show the boat and the whole area on the way. After the last match,
  it pulls back out to the view centred on the boat and rests there before
  starting again.
- **Summary cycle:** A longer Summary cycle, 15 to 20 seconds, gives each place
  a longer hold.
- **Route:** The route SignalK is navigating shows as a line, with the leg
  you're on drawn brighter.

See [Add a Nearby map](../how-to/add-a-nearby-map.md) for setup, and [POI
categories](../reference/poi-categories.md) for what each category matches and
how well your cruising ground is likely to be mapped.

## Beyond the grid

### Anchor watch and the rode planner

Its own page. See [Anchor watch](anchor-watch.md).

### Following the route

Helmcentral doesn't plan or activate routes. Plan them on your chartplotter, or
in a SignalK app such as Freeboard, and activate them there. The Route tile
shows the route SignalK is navigating, with the distance left to its end and the
arrival time at your speed over the ground. When the plotter only sends the
waypoint you're heading for, the tile shows that waypoint instead. See
[Tiles](../reference/tiles.md).

### Autopilot

The Autopilot tile covers engage and disengage, mode, heading nudges, tack and
gybe either way, and dodge. Helmcentral drives pilots through the modern
autopilot interface only. A pilot offering nothing but the older legacy
steering controls won't respond, and Helmcentral won't quietly fall back to
them.

- **What it reports:** The tile reports what the pilot last said about itself,
  never what your last press asked for.
- **Quiet steering data:** It greys itself out if steering data goes quiet.
- **Unavailable actions:** It disables, without hiding, any action the connected
  pilot isn't currently advertising.
- **Changing who is steering:** Anything that changes who is steering takes a
  press and hold.

### Radar targets

These are ARPA contacts from the ship's own radar, if you run one. They plot on
the anchor map as course-oriented triangles, distinct from the AIS circles, and
list with range, bearing and the CPA and TCPA the radar computed.

This is the marine radar. The **weather radar** is a separate embedded Windy map
centred on the vessel.

Radar is optional and stays off unless the pieces are there: mayara-server
running against the physical radar, and its plugin installed on the boat's
instrument server. Helmcentral needs no configuration of its own beyond that,
and it finds the radar on the feed. Without both pieces the tile reads `—`.

### Max wind gust

Over a window you pick per readout: 10m, 30m, 1h or 24h.
