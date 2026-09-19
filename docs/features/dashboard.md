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
goes quiet without failing takes 45 seconds to catch. Coming back to the
screen checks the clock again, so a phone that slept through an outage does
not wake showing a stale screen as though it were live. Only a fresh reading
clears the warning. Wi-Fi coming back does not, and neither does a link
reopening.

That banner tracks one thing: this screen's link to Helmcentral. A source
dropping off the boat's own network is a different problem, covered under
[When a source goes quiet](#when-a-source-goes-quiet). Alarms carry on
running on the Helmcentral box while a phone cannot reach it. The phone
simply cannot tell you what they are doing.

## Instrument tiles

Twenty-one built-in tiles: Vessel, Apparent Wind, Depth & Tide, Position,
Today & Now, Anchor Watch, Tanks, Route, Nearby Vessels, Radar Targets,
Battery & Power, Solar, Alternator, Generator, Switches, Hot Water,
Autopilot, Clock, Current Conditions, Forecast and Sea State.

The last four suit the wall display: a clock with sunrise, sunset, moon phase
and a route's ETA to its next waypoint; depth, apparent wind and outside
temperature each read against today's forecast range; five days of condition,
high and low; and a five-day wind-and-wave chart. Nothing stops you putting
them on a phone or tablet page too, but they were sized for the flybridge
strip's fold first, which is the narrowest wall this boat has. See [Set up a wall
display](../how-to/set-up-a-wall-display.md).

Three more tile types you configure yourself:

- **Gauges** read any value the boat's instrument network publishes and show
  it as a number, a radial dial, a bar, a lamp or a trend. Their coloured
  bands reuse the alarm severities, so setting a band into the alarm range
  defines an alarm rule: drag engine temperature into the red and it raises
  an alarm. Readings with no dedicated tile of their own, such as oil
  pressure or engine hours, live here.
- **Embed tiles** put any URL in the grid: a Grafana panel, a camera feed, a
  windrose. Tick **Frameless** in the embed's settings and the title bar and
  padding drop away so the embedded page fills the tile. That suits a wall
  page with no other chrome around it, a camera feed above all. On the
  ordinary dashboard leave it framed, or the gear icon and title go with it.
  Layout mode draws the frame whatever the setting, so an embed never
  becomes impossible to reconfigure.
- **Nearby maps** plot the points of interest around the vessel: bays,
  islands, marinas, fuel, boat ramps, moorings, historic landmarks, lookouts,
  dive and snorkel spots, and walking trails. See [Nearby
  map](#nearby-map) below.

## The indicator ribbon

One strip of status lamps and a CHK rollup, pinned above the grid on every
dashboard page, inside whatever skin that page uses. The strip belongs to the
vessel, not to the page: same lamps, same order, wherever you look, so a
glance always means the same thing. It stays off the Forecast, Routes,
Charts, Radar, Anchor Watch and Settings panels, which rely on the alarm
banner instead. It stays off wall displays too: on a 360px-tall strip it eats
about a third of the height for the least page-specific information on
screen.

Its order never changes on its own. A lamp that trips does not jump to the
front, and a lamp that clears does not vanish, because a ribbon earns its
place only if you can memorize it. Triage belongs to the alarm banner, which
counts each severity currently active, worst first, then names the alarms in
that same order.

If one page wants status lamps beyond the vessel-wide set, add a lamp-strip
tile to that page in layout mode. It configures the same way and leaves the
pinned ribbon alone. A wall page does the same, since a wall display never
draws the pinned ribbon.

Open the ribbon dialog for the first time and it fills with lamps drawn from
what your own boat publishes right now, such as each engine's revolutions and
the generator's state. Nothing is saved until you choose Save.

To add, edit or remove the ribbon, see [Pin an indicator
ribbon](../how-to/pin-an-indicator-ribbon.md).

## Arranging it

Toggle layout mode in the header, then drag, resize or remove tiles and add
them back from a picker. A toolbar opens above the grid the moment you do, in
a fixed order: the page's name, **Add Tile**, **Ribbon**, **Skin**,
**Hero**, and **Kiosk**. **Delete page** sits last behind a divider, set
apart because it is the one destructive control in the row. It is absent
while only one page exists. See [Create a dashboard
page](../how-to/create-a-dashboard-page.md) for deleting and renaming.

**New Page** creates a page immediately, named "Untitled page", with the name
field already focused. There is no separate naming dialog. An empty page
shows a short prompt pointing at Add Tile rather than a blank grid; see
[Create a dashboard page](../how-to/create-a-dashboard-page.md) for the full
walkthrough. Helmcentral saves everything on the box, not in the browser, so
a tablet at the helm and a phone in a bunk show the same arrangement and find
it again next session.

Add Tile groups the built-ins by what they are for: Navigation, Weather,
Situational, Power, Engine, Systems, At a glance, and Custom. A tile already
on the page greys out with an "On page" note. Gauge, Gauge Group, Engine
Cluster, Indicators, Embed and Nearby map go on a page more than once, so
those stay selectable regardless. Each tile lands at a size chosen for what
it shows, which saves you resizing a fresh one straight away.

Page order is shared too: the sidebar and the page dropdown follow the same
saved sequence, and new pages land at the end. Moving a page keeps it
selected and leaves its tiles where they were. See [Reorder dashboard
pages](../how-to/reorder-dashboard-pages.md).

The grid adapts to screen size, including phones, through three structurally
different arrangements. Layout mode, the toolbar and the picker need a screen
1024px wide or more, so a laptop or a landscape tablet.

A page can also nominate one placed tile as its **hero**. Pick it from the
toolbar's Hero select and it moves to its own full-width row above the rest
of the grid, enlarged. Everything else keeps the position you gave it:
picking a different hero, or clearing it back to "No hero", rearranges
nothing. Use it for the reading a page exists for, such as anchor distance on
an anchorage page or apparent wind on a passage page.

## Wall displays

A wall display is a screen somewhere on the boat that shows the dashboard and
nothing else: no sidebar, no header, no way to click into anything. It cycles
through the pages you have given it, in page order, and runs unattended. You
can have several, and they do not have to be alike: a 1920 by 360 strip at
the flybridge helm and a 55 inch television in the saloon are both wall
displays, each with its own pages.

Each one is a record you create and name, holding what is particular to that
screen: how big it is, how much to magnify it, and whether it is mounted
upside down. **Wall displays** in the sidebar lists the screens you have
configured; opening one gives you its settings and the pages it cycles
through. The fields are:

| Field | What it is for |
| --- | --- |
| Name | What you call the screen. "Flybridge", "Saloon TV". |
| Address | The last part of the screen's web address, so `flybridge` gives `/display/flybridge`. |
| Screen size | The size the screen's own browser reports, which is not always the size of the panel. The device check below tells you what to enter. |
| Magnification | How much larger to draw everything. A television read from across the saloon needs more than a strip read at arm's length. |
| Upside down | For a panel mounted inverted, as the flybridge strip is. |
| OLED panel | Shifts the image a few pixels on a slow cycle, so a board left up all season does not burn into the screen. |
| Keep awake | Asks the screen not to sleep. It cannot override the set's own power-saving menu. |

Magnification is the field worth understanding, because it is what makes a
television readable. Rather than building a page out of enormous tiles, set
the screen size to the size you want to *design* against and let the
magnification do the rest: a saloon television is usually 1280 by 720 at 1.5
times, not 1920 by 1080 at 1. Every tile then means on the television what it
means everywhere else.

### Putting pages on a display

A screen's own page is where you compose its rotation: **Add page** offers
every dashboard page not already on a screen, and the table below holds the
ones already there, each with its dwell (5 to 3600 seconds), its condition
(**Always**, or a vessel state such as **While anchored**), and arrows that
set the order the screen cycles through them. Reordering there moves a page
only against the others on that screen.

You can also assign from the page itself: in layout mode, the toolbar's
display select puts the page you are looking at onto a screen. Setting it
back to "Not on a wall" keeps the duration and condition, so putting it back
later remembers both.

A page belongs to one display. That is deliberate: a board laid out for a
1920 by 360 strip is not a board you want filling a 55 inch television, and
the layout is the page. To start a television version of a page you already
like, use **Duplicate to** in the toolbar and pick the other screen; you get
a copy you can then rearrange for its new shape.

Pages on a display leave the ordinary page list and appear under their screen
in the sidebar instead, so the list you scan at the helm stays short. They are
still ordinary pages in every other way: click one to edit it exactly as
before.

A page on a wall display has no hero tile. The hero draws an extra row above
the grid, which spends the vertical room the wall is measuring and shows the
same tile twice. Putting a page on a display clears its hero.

Deleting a display never deletes its pages. They lose their place on that
screen and return to the ordinary page list, keeping their duration.

### While it is running

Feed order is page order. Reorder pages the way you always do and the
rotation follows. A page whose condition stops being true finishes the slot
it is already showing and then drops out until it is true again, rather than
waiting for the next lap. A rotation with nothing to show, whether the
display has no pages or none currently qualify, checks again every 15 seconds
instead of sitting blank.

While editing a page that belongs to a display, an amber dashed line marks
where that screen cuts the page off, measured for that screen in particular.
Everything above the line is what the screen shows. Anything below it is real
and invisible there.

If the screen has a remote or a keyboard, four keys drive it: left and right
step to the previous and next page, and either **OK** or the space bar pauses
and resumes the rotation. A brief caption names the page and its place in the
feed. Nothing else on a wall display responds to input.

For a lost connection or an active alarm a wall display shows a compact
status pill where the ordinary dashboard shows a full banner. Both stay
hidden while the feed is connected and quiet. A wall display always draws in
the dark theme, whatever this browser has stored for the ordinary dashboard,
and leaving it never changes that stored preference. The pinned indicator
ribbon never draws on a wall display; a page that wants status lamps there
carries its own lamp strip tile.

See [Set up a wall display](../how-to/set-up-a-wall-display.md) for the
device check, mounting and orientation.

## Linking to a page

The address bar follows the panel you are on, so you can link straight to it.

| URL | Opens |
| --- | --- |
| `/` | The dashboard; on first load, this browser's remembered page if it still exists, otherwise the first page. |
| `/dashboard/<page id>` | The dashboard, that page. |
| `/forecast` | The forecast drawer. |
| `/routes` | Route planning. |
| `/charts` | Satellite charts. |
| `/radar` | Radar targets. |
| `/anchor-watch` | Anchor watch. |
| `/alarms` | The alarms panel. |
| `/settings` | Settings, General section. |
| `/settings/<section id>` | Settings, that section. |
| `/display/<address>` | That wall display: fullscreen, no sidebar, cycling through the pages assigned to it, at the size and orientation set on the screen's own record. |

The first page answers to `/` as its canonical address. Reorder the pages so
that a different one comes first and the address follows, without switching
the page you are looking at. To ask for a particular page, use
`/dashboard/<page id>`.

Back and Forward move between panels the way they move between pages on any
other site: open Forecast, then Routes, and Back returns you to Forecast
rather than closing the app. Leave a Settings change unsaved and Back still
asks first, the same dialog you get clicking away in the sidebar.

Tapping an alarm notification on your phone opens the Alarms panel, not the
dashboard.

## When a source goes quiet

The boat's instrument network keeps serving the last value a source sent it.
Stop the feed and the number sits there unchanged, easily mistaken for a live
reading. Solar output frozen at 0 W before sunrise can still read 0 W an hour
after the array started producing.

The Solar and Battery & Power tiles watch how long it has been since their
source last said anything. Past two minutes the tile dims, its header gains a
`STALE` marker with the age, and every reading on it goes to `—`. Blanking
the numbers is the point: a stale reading presented as a current measurement
is worse than no reading at all.

Solar does this per controller as well as for the array as a whole. One MPPT
dropping off marks that row and leaves the rest of the tile reporting
normally from the controllers still live.

Every gauge, gauge group, engine cluster, indicator lamp strip and the pinned
ribbon watch the same way. A standalone gauge dims and blanks exactly like
Solar or Battery & Power. A lamp goes dark with the same age marker rather
than staying lit on an old reading: a generator that shut down twenty minutes
ago must not still show green because its last report happened to say
"running". A gauge group or engine cluster badges only the reading that
actually stopped and keeps reporting the rest. The tile as a whole goes stale
only once every reading on it has stopped, so one dead sensor does not blank
a cluster that is otherwise still telling the truth.

A number the dashboard works out from other readings, fuel economy for
example, is only as current as its oldest input. Let one engine's fuel-rate
feed stop while the boat is still moving and the economy figure built from it
is running on a number that is no longer arriving. It goes stale with that
feed rather than carrying on as a live measurement.

Ages are measured against the vessel clock at the moment the reading was
taken, so a tablet with a wrong clock, or one waking from sleep, will not
invent staleness.

A tile showing `—` with no `STALE` marker means something else entirely: that
reading is not being published at all. Check the source exists on the boat's
network before you go hunting a dead link.

## Tile state

A tile's edge takes the colour of the worst alert level any of its readings
currently sits in: sky for alert, amber for warn, red for alarm, a deeper red
wash for emergency. A dot beside the title carries the same colour, since a
border alone is easy to miss along the edge of a screen.

A tile with every reading in its normal band draws no attention at all, on
the same principle as the rest of the board: colour is spent only where
something needs it. A reading that has drifted out of a healthy band with no
warn or alarm threshold set for it stays quiet too. Set the threshold in
Settings if you want that reading to raise the edge. Until you do, a normal
engine idling with no configured limits stays a normal engine, not a
permanent amber warning.

A stale tile shows its amber staleness treatment instead, whatever band its
last reading was in. A value the tile can no longer vouch for should not also
claim a state.

## Battery & Power

Four cards: State, Net, Solar and Loads, plus a Shore line that appears once
the shore charger has something to report.

**State** is the big number: state of charge, with a bar underneath and a
line to its right reading "To full", "To 20%" or "To empty" depending on
whether the bank is charging, discharging above a threshold you have set, or
discharging with no threshold set. The number and the bar turn amber, then
red, only when the reading crosses a band you configured as a battery alarm
rule (see [Set the battery state-of-charge
bands](../how-to/set-battery-state-of-charge-bands.md)). Configure no rule
and the number stays plain amber at any state of charge, because it has no
threshold to cross.

**Net**, **Solar** and the rate under State are teal: energy moving into or
out of the bank. **Loads** is plain text, because it counts what the bank is
feeding rather than a state of the bank itself.

**Shore** shows the charger's mode and AC input current once the charger
reports anything at all. At anchor with nothing plugged in, the line is
absent rather than showing blank fields. An error from the charger always
shows, in red, even before anything else about the charger has reported.

### The Dawn figure

The footer's right side projects the state of charge at sunrise, so you can
tell at a glance whether the bank holds until morning without doing the
arithmetic yourself. It reads something like:

`Dawn 6:08 · 51% · 4 nights`

`6:08` is tomorrow's sunrise and `51%` the projected state of charge at that
time. `4 nights` means your own boat's recent overnight discharge built the
figure. Helmcentral takes the typical (median) rate the bank has actually
drawn down over the most recent clean nights it can find, up to seven,
looking back as far as thirty, and applies that rate to tonight after first
running the current rate forward to sunset.

Short of that much history, the same spot reads:

`Dawn 6:08 · 61% · at current rate`

This holds the rate the bank is drawing, or charging, right now all the way
to sunrise. Read it literally. In the early afternoon with the panels putting
power in, "at current rate" can read as high as 100%, because it projects an
afternoon charging rate across hours you will spend discharging. Once the sun
goes down it becomes an accurate night estimate on its own, because by then
the current rate *is* the night's rate.

The tile switches from "at current rate" to a night count by itself, with
nothing for you to configure, once your boat has logged at least two clean
nights of state of charge. A clean night runs sunset to sunrise with no shore
power on the charger and no generator run at any point, judged straight off
those two signals, and with enough samples to trust the slope. A night in a
marina, a generator night or a gappy night is skipped rather than averaged
in, and so is a night where the bank somehow rose. Hover the figure to see
how many nights were skipped and why.

While the charger sits on shore power tonight, the line reads `Dawn 6:08 · on
shore power` instead of a percentage. The bank is being held, and projecting
a discharge would be wrong in the same way those skipped marina nights were.

Without a position there is no sunrise to compute, and the Dawn segment drops
out altogether. If the feed has gone stale, or you switched the live-rate
fallback off and there is not enough history, it reads a dash. On a screen
with a mouse, hovering shows the reason.

The estimate follows the alarm bands as well: a projected percentage falling
into a band you have set takes that band's colour. That is your cue to run
the generator or plug in before dark, rather than finding out at 3 AM.

## Tanks

Below the tank bars sits a footer with three fuel figures, which appears once
at least one fuel tank is configured: **Fuel aboard**, **Range at current
burn** and **Time to empty**.

Fuel aboard sums the level and capacity of every fuel tank reporting both, in
litres. Range and time to empty both say "at current burn" on purpose. They
come from the boat's speed and its engines' burn as they stand this second,
with no averaging. Slow down or speed up and the figures move to match at
once. Read them as what the boat could do if nothing changed from here, not
as a trip plan.

Both need the boat to be burning fuel, so both sit blank while stopped or
with the engines off. Fuel aboard keeps reporting regardless, since a tank's
level does not depend on an engine running.

Let an engine's fuel-rate reading stop updating and range and time to empty
blank out with a small stale marker beside the label, rather than showing a
number built on a burn rate that stopped being true hours ago. Fuel aboard
gets the same treatment against its own tank readings: a tank sensor gone
quiet blanks that figure too.

## Nearby map

A moving map centred on the vessel, showing the points of interest within a
range you set. Add it from the tile picker like an embed or a gauge. Each one
keeps its own range, categories, layout and toggles, so one page can show
anchorages and dive spots while another shows fuel and boat ramps.

Two layouts: map only, or map and list. The list ranks the five nearest
matches by distance, each with its category icon, name, distance and bearing,
and a one-line description where one exists. Those same five carry a numbered
badge on the map so list and chart agree on which marker is which. Every
matching feature in range still shows on the map, badge or no badge.

Categories: anchorages, bays and islands; marinas, fuel, boat ramps and
moorings; historic landmarks; lookouts; dive and snorkel spots; and walking
trails. Coverage depends on how thoroughly your cruising ground has been
mapped in the underlying data source, and it varies by category: mooring
fields and fuel docks are usually well tagged, dive and snorkel spots much
less so.

Two switches control what else appears: nearby AIS traffic, on by default,
and your own vessel's trail, off by default. Turn either off if the map feels
busy at a low zoom.

The map follows the vessel, holding north up. Should the position feed report
a fix it does not trust, the map holds its last known position rather than
jumping to an unreliable one, and shows a small amber GNSS badge until a good
fix returns.

A footer under the list names the current data provider and how old its
answer is. When a fetch fails, the list keeps showing what it last knew, aged
and marked with the error, rather than going blank or inventing a result. The
wait for a fresh answer backs off automatically while a provider is
unavailable, so a slow patch of coastline does not turn into a hammering
retry loop.

See [Add a Nearby map](../how-to/add-a-nearby-map.md) for setup, and [POI
categories](../reference/poi-categories.md) for what each category matches.

## Beyond the grid

### Anchor watch and the rode planner

Its own page. See [Anchor watch](anchor-watch.md).

### Route planning

Multi-leg waypoint sequences with per-leg distance, bearing and ETA. Activate
a saved route and Helmcentral pushes it out as the vessel's active route, for
autopilots and MFDs to follow.

You plan the waypoints yourself. Helmcentral offers no hazard avoidance, no
weather routing and no live navigation, and it needs no chart licence.

### Satellite charts

Upload your own MBTiles and Helmcentral serves them. It never fetches or
bulk-caches tiles from a live provider, which keeps it clear of the licensing
terms that make redistributing somebody else's imagery a problem.

### Autopilot

Engage and disengage, mode, heading nudges, tack and gybe either way, and
dodge. Helmcentral drives pilots through the modern autopilot interface only.
A pilot offering nothing but the older legacy steering controls will not
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

Radar is optional and stays off unless the pieces are there. Targets appear
once mayara-server is running against the radar and the mayara plugin is
installed on the boat's instrument server. Helmcentral needs no configuration
of its own and finds the radar on the feed. Without both pieces the radar
tile reads `--`.

### Max wind gust

Over a window you pick per readout: 10m, 30m, 1h or 24h.
