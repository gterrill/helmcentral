# Hierarchy of sensor needs

This page shows what Helmcentral can do for you with the instruments your boat
already has, and what each new sensor adds. It's laid out like a pyramid. The
lower levels are the ones almost every boat has, and each level up builds on
the ones below it. You don't need the whole pyramid. A boat with only a GPS on
its network still gets an anchor watch, a logbook and a forecast.

Everything here reaches Helmcentral through SignalK, so a sensor counts once
SignalK is reading it. A few features also need a SignalK plugin, and those
are named where they come up.

| Level | What you add | What it opens up |
| --- | --- | --- |
| 0 | Nothing but Helmcentral | Inventory, maintenance by the calendar, documents, notes, the manual, import, the Clock and Embed tiles |
| 1 | GPS, and the autostate plugin | Position, boat time, the anchor watch, passages, the forecast and tides, place names, the Nearby map, wall pages that change with the boat |
| 2 | Depth sounder and compass | Depth & Tide, low-water clearance, local tide calibration, the anchor placed at the bow, North Up on the Wind tile |
| 3 | Wind instrument, speed log and barometer | The Wind tile, true wind, the Current tile, pressure tendency and the heavy-weather rules |
| 4 | AIS receiver | Nearby Vessels, AIS on the Nearby map, other boats' swing circles at anchor, collision alarms |
| 5 | Battery monitor, then the charging sources | Battery & Power, the Dawn figure, the Solar, Alternator and Generator tiles, the full-bank charging check |
| 6 | Engines and tanks | The Engine Cluster, running hours for maintenance, engine anomaly checks, the Tanks tile and tank alarms |
| 7 | Digital switching, autopilot and a chartplotter route | The Switches, Autopilot and Route tiles, arrival times |
| 8 | InfluxDB | Unusual engine readings, engines drifting apart, overnight battery history, Mate's history lookups |

When a sensor is missing, the tiles that need it say so. You'll see `—` or a
plain message such as "No tank data available", never an old or invented
number. See [Staleness](tiles.md#staleness) for how a tile looks when a
sensor stops sending.

## Level 0: no sensors at all

These parts work on a computer with no instruments connected, and most of them
work with no internet connection either:

- **Looking after the boat:** [Inventory](../features/inventory-tracking.md),
  [Documents](../features/documents.md),
  [Notes and the manual](../features/notes-and-the-manual.md) and
  [Import](../features/import.md) from YachtWave or GPX files.
- **Maintenance by the calendar:** Jobs that fall due every so many months.
  Jobs that fall due on running hours need an engine's hour meter, or hours
  you read off the engine and type in. See level 6.
- **Tiles:** The Clock tile shows the time and date. Once it has a position
  and a forecast, it adds sunrise, sunset, moon phase and where the boat is. The Embed tile shows any web page
  you point it at, such as a camera feed.
- **Mate:** Mate can answer from your inventory, maintenance log and
  documents. It needs an internet connection for every question.

The Hot Water tile doesn't read anything yet. It always says Off, and its
buttons do nothing.

## Level 1: a GPS

A GPS position is the foundation for nearly everything else, because almost
every feature needs to know where the boat is:

- **Position tile:** Latitude, longitude and the place you're in.
- **Boat time:** The boat's clock follows the time zone at its position. Until
  the first fix it reads UTC and says so. See [Boat time](boat-time.md).
- **Anchor watch:** Drag detection runs on the server all night. If the fix is
  lost, the position holds at the last good one and a separate Anchor Position
  Lost alarm sounds after a minute. See [Anchor watch](../features/anchor-watch.md).
- **Passages:** Speed over the ground starts and ends each leg. Going 3 knots
  for two minutes starts one, and less than 1 knot for an hour ends it. See
  [Passages](../features/passages.md).
- **Forecast and tides:** Today & Now, Forecast Conditions, the rain strip and
  the tide predictions are all for the boat's position. They also need an
  internet connection. See [Forecast](../features/forecast.md).
- **Place names and the Nearby map:** The bay or marina you're in, and the
  places around you. They need an Overpass server, either the public one or
  your own. See [Place names](../features/place-names.md).

### The autostate plugin

Helmcentral doesn't work out for itself whether the boat is anchored, moored,
motoring or sailing. It takes that from the `signalk-autostate` plugin, which
decides it from the GPS and other readings. Without the plugin:

- **Vessel tile:** No status is shown.
- **Current tile:** It reads `—` and says "No navigation state".
- **Wall displays:** A page set to show **While anchored**, or any other boat
  state, never comes up. Only pages set to **Always** show.

## Level 2: a depth sounder and a compass

A depth sounder adds the water under the keel:

- **Depth & Tide and Current Conditions:** The live depth, beside the tide.
- **Low-water clearance:** At anchor, Helmcentral works out how much water
  you'll have under the keel at the next low tide. It also needs a tide
  reading, and the boat's draft as the instrument network reports it.
- **Local tide calibration:** While the anchor watch is set, Helmcentral
  compares the sounder against the tide station and learns how the tide runs
  where you are. See [Anchor watch](../features/anchor-watch.md).

A compass heading doesn't open up a tile of its own, but it makes several
better:

- **Anchor watch:** The anchor is dropped at the bow rather than at the GPS
  antenna. Without a heading it stays at the antenna, and the screen says so.
- **Wind tile:** North Up needs a heading to place the bow marker and the
  apparent wind arrow. Without a heading they drop off the ring, and the speed
  and angle keep reading. Course Up works without one.
- **Current tile:** The angle the current comes from needs a heading. The
  speed and the helping or hindering figure don't.

## Level 3: wind, a speed log and a barometer

An anemometer gives you the [Wind tile](tiles.md#wind-fields) in Apparent
mode, with its gust history. Helmcentral doesn't work out true wind itself.
The `signalk-derived-data` plugin does, and it needs your speed through the
water and a compass heading as well as the wind instrument. True wind then
lights up:

- **Wind tile in True mode:** With its own gust history.
- **Current Conditions:** True wind speed against today's forecast.
- **Passages and watch reports:** Wind speed and gusts for each leg, and in
  the Weather section of a watch report.

No built-in tile reads a speed log directly, though a Gauge tile can show it.
With the compass and the GPS,
`signalk-derived-data` uses it to work out the water current, and that drives
the [Current tile](../features/dashboard.md#current). A log that reads high
shows up as a foul current that isn't there.

A barometer adds the pressure tendency and the heavy-weather alarm rules that
watch how fast the pressure is falling. The history behind them is kept in
memory, so it starts again whenever Helmcentral restarts. The trend shows
after half an hour, and the three-hour tendency after about three hours. The
rules that look back twelve and twenty-four hours stay quiet for most of a
day. The outside temperature on Current Conditions comes from
the forecast, not from a sensor, so an air temperature sensor only shows on a
Gauge tile.

## Level 4: an AIS receiver

AIS tells you about the boats around you:

- **Nearby Vessels tile:** The traffic around the boat. It says "AIS silent"
  after ten minutes with nothing heard.
- **Nearby map:** With **Show AIS traffic** on, the map draws the vessels
  around you. The map-and-list layout also lists up to four of them within
  5 km.
- **Other boats at anchor:** With your anchor watch set, Anchor Watch draws the
  swing circles of the boats anchored near you.
- **Collision alarms:** These also need the `signalk-ais-target-prioritizer`
  plugin.

## Level 5: a battery monitor and the charging sources

A battery monitor on the house bank is what the power tiles are built on. You
choose it under Settings, SignalK, Roles, and then you get:

- **Battery & Power:** State of charge, the time to full or empty, and the
  net flow in or out. See [Battery & Power fields](tiles.md#battery--power-fields).
- **The Dawn figure:** The charge you'll have at sunrise. It needs the bank's
  current and its capacity in amp-hours. If the monitor doesn't report a
  capacity, type it into the house bank's equipment record.
- **The full-bank charging check:** It warns when something is still charging
  a full bank hard. It needs the bank's cell count and profile.

Each charging source then adds its own part:

- **Solar charge controllers:** The Solar tile, with a row per controller, and
  the Solar card on Battery & Power.
- **Alternators:** The Alternator tile, and the electrical footer on the Engine
  Cluster. The tile also needs the engine's rpm to know whether it's running.
- **Shore charger:** The Shore line on Battery & Power.
- **Inverter/charger:** The AC loads.
- **Generator:** The Generator tile, with Start and Stop. A Victron GX needs
  `signalk-venus-plugin` to pass these on. Start and Stop also need the
  SignalK login you gave Helmcentral to be allowed to make changes.

With InfluxDB, the shore charger and the inverter also keep nights on shore
power, or with the generator charging, out of the Dawn figure's history. See
level 8.

If the boat hasn't got one of these, answer **This boat has none** for it
under Settings, SignalK, Roles, so the tiles stop asking.

## Level 6: engines and tanks

An engine gateway on the NMEA network gives you each engine's rpm,
temperatures, oil pressure, fuel rate and hours:

- **Engine Cluster:** One engine's dial and readouts. See
  [Engine Cluster](../features/engine-cluster.md).
- **Running hours for maintenance:** Jobs fall due on the engine's own hour
  meter. Without one, you can type in the hours you read off the engine. See
  [Maintenance](../features/maintenance.md).
- **Anomaly checks:** Frozen and impossible readings, and an engine
  stumbling. See [Anomaly detection](../features/anomaly-detection.md).
- **Anchor watch:** It raises itself once you motor away from the anchor at 3
  knots or more. It needs an engine's rpm to know you're motoring, so without
  one you raise the watch by hand.
- **Fuel figures:** Fuel economy, range and time to empty, and fuel used on
  each passage.

Tank senders give you the Tanks tile and the tank alarms. Each tank needs an
equipment record before it's listed. The fuel tanks also feed the fuel arcs on
the Engine Cluster, and fresh water is counted on each passage. See
[Change a tank's alarm levels](../how-to/change-tank-alarm-levels.md).

## Level 7: switching, the autopilot and the chartplotter

These are the systems you control or steer by:

- **Digital switching:** The Switches tile shows and works the circuits on a
  system such as CZone.
- **Autopilot:** The Autopilot tile shows the pilot and offers only the
  commands your pilot supports. Without one it says "No autopilot detected on
  this SignalK server."
- **A route from the chartplotter:** The Route tile shows the distance and
  time to the end. The Clock tile adds the arrival time, and the Nearby map
  draws the route. A TimeZero route only passes on the next waypoint, so the
  arrival is for that waypoint. See [Tiles](tiles.md#built-in-tiles).

The switches and the autopilot commands send changes through SignalK, so they
need the SignalK login you gave Helmcentral to be allowed to make changes.

## Level 8: a longer history in InfluxDB

InfluxDB doesn't add a sensor. It keeps a long history of every reading, and
that lets Helmcentral compare tonight with the nights before:

- **Unusual engine readings:** An engine running hotter or harder than its own
  past.
- **Engines drifting apart:** Port and starboard compared against how they
  usually differ.
- **The Dawn figure:** Based on the boat's own clean nights instead of only
  tonight's rate. With a shore charger and an inverter picked, nights on shore
  power or with the generator charging are left out.
- **Mate's history lookups:** Charts of a reading, what happened around an
  alarm, when a reading stopped, the wind rose, and passage times and fuel.

Setup and everything on the lower levels work without it. See
[Keep a longer history in InfluxDB](../how-to/keep-longer-history-in-influxdb.md).
