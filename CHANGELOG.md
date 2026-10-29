# Changelog

What changed in each Helmcentral release, written for the person upgrading the
boat. Read the **Breaking** section of every release between the one you run and
the one you are installing: it says what you have to change before or after the
upgrade.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
releases use [Semantic Versioning](https://semver.org/). While the version is
0.x, a minor release can break things.

## [Unreleased]

### Breaking

- **Your alarm rules are now kept in the database.** They used to be in
  `data/alarm-rules.json`. The first time you start this release, Helmcentral
  copies every rule into `data/helmcentral.sqlite`, along with the record of
  which built-in sets it has already offered, so none of them reappear. It then
  renames the file to `alarm-rules.json.imported` and leaves it in the same
  folder as a copy of what you had. The Alarms drawer works as before. What you
  need to do: back up `data/helmcentral.sqlite` from now on. A backup that only
  took `alarm-rules.json` will no longer have your rules. If the old file can't
  be read, Helmcentral won't start and says why, and the file stays as it was.

### Added

- **Outside assistants can read the boat.** A program on your own computer, such
  as Claude Code or Claude Desktop, can now connect to Helmcentral over MCP and
  ask about the house bank, the active alarms, the alarm log, how a reading has
  behaved, your passages and the maintenance schedule. It gets figures in your
  units and a description of your boat written fresh from your equipment and
  roles. It can only read, it doesn't use Mate or your OpenRouter key, and it
  never gets Mate's conversations or your contact details. Open **Settings,
  Outside assistants** to make a token for each program. The token is shown once,
  you can switch each tool on or off for it, and you can revoke it on its own.
  Until you make a token, every request is refused. It works on your boat's
  network and on Tailscale. Claude on the phone and the web can't connect yet.
- **Watch reports can show a chart.** A finding about a trend now comes with a
  chart: pressure over 12 hours when it falls, the house bank's charge and current
  over the night it measured, and a tank's level since the last report. Mate can
  attach a chart to a finding it adds. The Watches page draws it under the finding,
  and the email carries it as a picture under that finding. ntfy sends the first
  one as an attachment, and the webhook gets each chart's points and a picture. A
  chart is only drawn from readings Helmcentral recorded. When it can't read them,
  the finding says why instead. If your ntfy server won't take attachments, the
  report goes as text and the run says so. Phone push is unchanged.
- **Watches can notify you.** Each watch now has a **Notifications** section. Pick
  where its report goes: ntfy, email, a webhook or phone and browser push, the
  same channels your alarms use. A new watch starts with every channel that's
  switched on. The default is **Only when it finds something**, which sends a
  report only when it has a finding the last one didn't, so a problem that stays
  doesn't tell you every hour, and tells you again if it clears and comes back.
  A finding counts as told only once a channel has accepted the report, so if
  every channel is off or failing, the run says "Not sent" and why, and the next
  run tries again. **Every report** sends them all. A report arrives
  as its own message at normal priority. It never appears as an alarm, and it
  never goes to SignalK. Email carries the whole report, and the webhook posts it
  as data, described in the new Watch report webhook reference page. A new
  **Address for links** in the Notifications settings, empty until you set it,
  puts a link to the run in ntfy, email and the webhook. The link opens the
  Watches page at that run, even an old one. Each run on
  the Watches page says which channels it reached, or that one hasn't been
  delivered yet and why, and Helmcentral keeps trying for up to 24 hours. Watches
  you made before this send nothing until you pick channels on them. Mate can set
  the channels and the sending rule when it proposes a watch.
- **Watches.** A new **Watches** page, in the sidebar, for regular checks of the
  boat. A watch runs on a schedule you pick: hourly, every few hours, daily at a
  time, chosen days, or hours such as 22:00 to 06:00. Each run builds the same
  short report, in your units: power, anchor, alarms, tanks, weather and
  instruments, whichever you choose. Rules you can change decide what counts as a
  finding, such as an alarm raised, a sensor gone quiet, the boat outside the
  anchor watch radius, a tank moving 10 points, or pressure down 3 hPa in
  3 hours. Mate reads every report and adds its view, and may add a finding but
  never removes one. Each run shows its report, its findings, Mate's review, what
  the review cost and an **Ask Mate about this run** button. Times are on the
  boat's clock, so a watch waits when the boat has no position and says so.
  Every run is kept for a year, runs with findings are kept for good, and you can
  delete any run. Ask Mate for "a check every 2 hours while we're at anchor" and
  it shows a card to apply, and it can run a watch for you straight away. The
  timed watch Mate starts on a reading now appears here too, as a one-off watch,
  and it carries on if Helmcentral restarts.
- **Mate can show you the Depth & Tide tile.** Ask "what's our depth?" or
  "what's the tide doing?" and Mate answers in words, then puts the tile under
  the answer. The newest answer shows the live tile. An older answer shows the
  readings as they were when Mate answered, with the time, so the picture still
  matches the words. A reading the boat doesn't have shows as a dash, and Mate
  says which one is missing. More tiles will follow.

- **Ask Mate about an alarm.** Every alarm card in the Alarms drawer, and every
  row in its History, has an **Ask Mate** button. It opens a new conversation
  with the question already asked, such as "Why did House Bank Low go off?".
  Mate looks at the alarm, the level it fires at, the reading and how recent it
  is, how the reading moved in the half hour either side, how often the alarm
  has gone off in the last week and what else went off nearby, then draws the
  chart. It says whether the reading looks real or looks like a sensor problem,
  and leaves out what it can't show. The alarm card keeps its own words. If the
  level looks wrong for your boat, Mate offers a card to change it, and nothing
  changes until you tap **Apply**.

- **Mate can set up alarms for you.** Say "tell me if the house bank drops below
  50%" and Mate checks the alarms you already have, then shows a card with the
  alarm, where it fires, where it clears and how long the reading has to hold,
  in your units. Nothing is set until you tap **Apply**, and then it's watching
  straight away. Ask "what should I be monitoring?" and it suggests a few
  readings that have no alarm yet, each one a card you can tap. Tank and gauge
  alarms are still changed on the tank's equipment item or the gauge.

- **Barometer alarms say what the move means.** A 6 mb move in three hours is
  strong wind, 10 mb is a gale, and a 24 mb fall in a day is a weather bomb.
  The card says which, and gives the wind to expect.

- **Mate gives a boat status in one go.** Ask "how's the boat?" or for a quick
  update and Mate answers with figures under Power, Tanks, Outside and Boat: the
  house bank, solar and DC load, each tank, depth, true wind and gust, pressure
  and how it has moved over three hours, engines, anchor, alarms, and how far
  the boat is from your planned stay. It finishes with up to three things worth
  a look, such as a sensor gone quiet, or the house bank draining faster
  overnight than it usually does, put as a question and never as an alarm. A
  reading it can't get is left out with the reason, never filled in. Figures
  are rounded the way an instrument shows them, such as 761 W and 11.3 kn.

### Changed

- **A deck's plan picture is uploaded where it will appear.** A new deck shows
  **Upload plan** under **Locations on this plan**, in the space the picture
  will fill. Once the plan is up, **Replace plan** sits under it. The separate
  Plan image section is gone.
- **Times on screen now follow the boat's time zone, not the device's.** The
  clock, tide times, sunrise, alarm times, passage dates, the "Off since" time
  on an engine tile and every other time you read are now in the zone the boat
  is in. A phone or tablet ashore, or a wall screen left on UTC, shows the same
  time as the boat. The clock tile says "last fix" when the zone is kept from
  the last good position, and "UTC · no position yet" before the first position
  arrives. Times you recorded yourself, such as when a document was uploaded or a
  checklist step was ticked, stay on the device's own clock. There's nothing for
  you to do.
- **The Tanks tile lists tanks by name.** Within each tank type the tanks are
  sorted by name, so Fuel Port Aft and Fuel Port Fwd sit together. A faint
  line separates one tank type from the next.
- **Mate can set a tank's alarm levels.** When you ask it to change an item's
  tank warn level, alarm level, or to switch a tank's alarms off, it can now
  propose that, and the same checks apply as on the item page. The alarm level
  has to be further into the bad side than the warn level, and both stay between
  0 and 100 percent.
- **A barometer alarm's clear point moves under the card.** "Clears once the
  rise eases to 5.5 mb" now sits beside the time it was raised, in the alarms
  drawer. The banner across the top shows what is happening and no longer says
  how it clears.

- **Mate's charts read right.** Asked about the wind, Mate used to draw speed
  and direction on one chart with two scales, and the direction was in radians.
  Now readings in different units each get their own panel, stacked on one time
  scale. Directions are in degrees, marked °T or °M, drawn as dots on a scale
  that runs from north round to north. A direction is averaged properly, so a
  wind swinging between 350° and 10° comes out as north and not south. When
  there is no average direction, Mate says so. Each line also shows a pale band
  from its lowest to its highest reading, which for wind speed is the gusts.
  Charts Mate drew before this still open and show in the new layout.

- **The engine cluster's settings are split into tabs.** Dial, Readouts,
  Footer and Fuel each have their own tab, so you no longer scroll past every
  slot to reach the one you want. On a wider screen, a small picture of the
  card shows which part you're editing. The footer rows fold shut and say what each one shows, or
  **Not shown**. When **Save** is greyed out, the line beside it says what's
  missing, and a tab with an empty slot has an amber dot.

- **Mate tells your season from the Passages page.** Ask it to summarise the
  season, a cruise or a trip and it now reads the passages you've recorded, with
  their place names, distances, fuel, ratings and notes. Before, it counted runs
  of logged speed over the last 60 days, so a season came back with a fraction of
  its passages. It tells the story in order, then the longest legs and the
  longest stays. Dates you give it, such as "August", are days on the boat's
  clock. A passage still under way is mentioned apart and not counted until it
  ends. If the Passages page has nothing to read, Mate says so.

### Fixed

- **Applying Mate's changes now updates the page you are on.** If you had
  Decks, Equipment, Maintenance or the vessel details open beside Mate and
  tapped Apply, the page stayed as it was until you reloaded it. It now shows
  the new decks, locations, bins, equipment, maintenance and vessel details as
  soon as you apply them. Apply also shows a short "Applied 3 changes" message,
  so you can see it went through.

- **The boat's clock now follows daylight saving and half-hour zones.** It used
  to be worked out from longitude alone, which put a boat in Sydney an hour out
  all summer and a boat in Adelaide half an hour out. Helmcentral now finds the
  time zone for the boat's position, so Sydney is on UTC+11 in summer and
  Adelaide on UTC+9:30. Offshore it uses the ship's zone for the longitude.
  Mate, the Watches page, passages and the forecasts all use the same clock, and
  a forecast for a place you ask about uses that place's own time. If the
  position drops out, the boat keeps the zone of its last good position and Mate
  says "last fix". Before the first position ever arrives, times are UTC and
  Mate says so. There's nothing for you to do.
- **Watch reports are shorter and stick to the facts.** Repeated alarms are
  grouped by title, with how many times, the first and last time, and "active
  now" when the alarm is still going. Times say "yesterday" or the day when they
  aren't today, and a first report says "in the last 24 hours". The count covers
  every alarm in the period. Mate's review of a report is shorter, doesn't
  repeat the report, and sticks to what the figures show. It no longer says an
  alarm cleared when it didn't, or suggests changes the figures don't point to.
- **Collision Profile Not Changed no longer opens a fresh entry each time
  Helmcentral restarts.** The entry still open from before is picked up, kept
  up to date and cleared when the profile changes.
- **Collision Profile Not Changed now shows in the Alarms drawer while it
  stands.** It was raised but never listed as active.
- **The collision profile alarm no longer piles up.** When Helmcentral couldn't
  change SignalK's collision profile, it added a new entry to the alarm history
  every time the boat went from anchored to under way and back. One refusal now
  stays as one entry, and its card is updated if the reason or the state
  changes. The card also says what went wrong in plain words, such as SignalK
  refusing Helmcentral's login, and is titled Collision Profile Not Changed.
  Entries already piled up in your history stay there and don't clear by
  themselves.

- **Mate's answers about an alarm are plainer.** Asked why an alarm went off,
  Mate no longer quotes paths, rule ids or lists what it couldn't find. It says
  when an alarm keeps going off, and ends with what to check next when the cause
  points to something you can act on, such as giving Helmcentral's SignalK user
  read access. It also no longer calls Helmcentral's own checks a deleted rule.

- **Mate can give a passage distance through the islands.** Asked how far
  Goldsmith Island was from Airlie Beach, Mate used to say the coast was too
  intricate and ask you to plan shorter legs. It now gives the distance by
  water, 37 nm for that run. Mate still asks for shorter legs if a run is over
  300 nm, or if a stretch of coast is so broken that it can't search it.

- **Engine Cluster footer parts now pick their reading from the equipment.**
  Choose the starter battery for the footer and the tile sets its voltage from
  what the battery is sending. Before, you had to type the reading yourself, and
  if you didn't, the part was dropped on save and the tile showed `--`. If the
  equipment isn't sending the reading the part needs, the field says so and lists
  what it does send. Helmcentral won't save a footer part with equipment and no
  reading. Open the tile's settings and check any footer part that stayed blank.

### Added

- **Mate can draw a wind rose.** Ask "where has the wind been coming from?" and
  Mate shows petals for each compass direction, split by wind speed in your
  units, with calm and gusts noted.
- **Fix two items' sources the wrong way round in one save.** On an item's
  **Source** list, a source that another item has now shows its reading and who
  has it. Pick it and the Save button becomes **Swap sources**, or **Move
  source** when the item has nothing of that kind to hand back. Before, you had
  to clear one item, change the other and go back.

- **Mate can summarise your passages for any dates.** Ask "how far did we go in
  August?" or "where did we stay longest?" and it adds up the passages in those
  dates, with the stays worked out from the gaps between them.

## [0.56.0] - 2026-10-28

### Breaking

- **Mate now needs the offline coastline to give a passage distance.** Ask how
  far it is to a place, or for the time and fuel to get there, and Mate finds
  the shortest way by water, round the islands and headlands in the way. Before,
  a place on the far side of an island came back as the line across it, so the
  run looked shorter than it was and the time and fuel were too low. Turn on
  **Settings → Offline coastline** and let the squares round the boat download.
  Until then, or if a square is missing, a position is on land or no water joins
  the two places, Mate says it can't give a distance and why. It never falls
  back to the straight line.

- **Engine Cluster fuel bars no longer raise alarms or set their own colours.**
  A fuel bar now takes its colours from the tank's alarm levels, and the tank
  raises the alarm. Fuel bar zones you set before are ignored. Open the tank's
  equipment item and check **Tank levels**, or change the defaults for every
  fuel tank under **Settings → Alarms → Tank levels**. A fuel bar that was
  bound only to a SignalK source, with no tank equipment item behind it, no
  longer raises a low fuel alarm. Add the tank as equipment to get one.

### Added

- **Mate can measure a leg between two places.** Ask how far one place is from
  another and Mate gives the distance by water and the course of the first leg,
  starting from the place you name rather than the boat.

- **Previous and next on a passage.** Two arrows at the right of the passage's
  title step to the passage that started later or earlier, within the list you
  opened it from, and the **[** and **]** keys do the same. The ends grey out
  instead of wrapping. Unsaved edits are held, as they are with the back arrow.
- **Zoom and fit buttons on the passage map.** **Zoom in**, **Zoom out** and
  **Fit track** sit under the 3D and satellite buttons. They grey out whilst a
  replay is playing.
- **Tank alarms.** Helmcentral now raises its own alarm when a tank gets low, or
  when a waste tank gets full, for any tank that has an equipment item. Fuel and
  fresh water warn at 20 percent and alarm at 10. Waste warns at 80 and alarms
  at 90. The level has to hold for a minute before the alarm is raised, and it
  clears once the tank is 5 percentage points back. The card is named for the
  tank, such as "Fuel Port Fwd Low", and shows the level and the litres left,
  with where it clears underneath. You can change the defaults under **Settings
  → Alarms → Tank levels**, give one tank its own levels, or switch a tank's
  alarms off on its equipment page.

- **Move a sign in time.** With a sign picked in the replay's sign editor,
  **Move sign here** puts it at the moment the slider is on, and **−1 min**,
  **−10 s**, **+10 s** and **+1 min** shift it a little at a time while the
  replay jumps to the new moment. For a sign with a photo, **Photo**, then
  **Back to photo time** puts it back where the photo was taken. You no longer
  have to delete a sign and add it again to change when it appears.

### Changed

- **The Tanks tile warns fuel at 20 percent.** It used to warn at 30. The tile
  and the fuel bars now use the same levels as the tank alarms, so the colour
  you see is the alarm you get.

### Fixed

- **The Nearby map's boat trail keeps up with the boat.** The trail used to
  redraw only about once a minute, and it stopped altogether while nearby
  places were failing to load. It now follows the boat every few seconds.
- **The dashboard does much less work while it sits open.** Every time
  Helmcentral checked the forecast, the tide, the route or the CZone
  switches, it used to redraw every tile on the page, even when nothing had
  changed. Now a tile redraws only when something it shows changes, so an
  older tablet or the wall display has more to spare.
- **A wall display with a map page no longer slows down and freezes after a few
  hours.** The screen used to rebuild every tile on each page change, and a
  small kiosk board never gave that memory back. Pages now stay loaded and only
  the one showing is visible. A camera embed still loads when its page comes up
  and lets go when it leaves.
- **The passage map no longer keeps redrawing while the replay is paused.** With
  3D on, the map redrew many times a second even when nothing was playing, and
  Chrome flagged the Passages tab as slowing the browser. It now draws only
  when the boat moves.
- **Tiles no longer say there's nothing there before the boat has reported.**
  When a page first opens, the Autopilot tile used to say no autopilot was
  detected, the Solar tile said there was no controller data, and the
  Alternator tile said the engines weren't running, all before Helmcentral had
  heard a word from the boat. Each now shows a dash until its first reading
  arrives. The Alternator tile also shows `--`, not "Engines not running", when
  no engine is set up in the roles or none is reporting, because it can't tell
  either way.
- **The header no longer says "At Anchor" before the boat has reported.** When
  a page first opened, the Vessel tile showed "At Anchor" for a moment whether
  or not the boat was. It now shows only the model until the boat's state
  arrives. The clock shows dashes until it has the boat's time, rather than
  the time on the screen you are using.
- **The anchor map no longer says there's no radar before the radar has
  reported.** When a page first opened, the greyed radar button on the anchor
  map said no radar was configured. It now says it is waiting for radar data
  until the first radar report arrives.
- **The anchor watch's automatic raise notice no longer comes back on every
  reload.** After Helmcentral raised a watch on its own, opening or reloading
  the dashboard showed "Anchor watch raised automatically" again, as if it had
  just happened. It now shows only when a raise happens while the page is
  open.
- **Documents, Passages, Inventory and Settings no longer redraw on every
  instrument update.** They sit still until something on the page changes, so
  they use less battery and processor time, and the Passages map stays smooth.
- **Gauges, Engine Cluster, gauge group and lamp tiles redraw only when their
  own reading changes.** They used to redraw every second when any instrument
  updated, so a busy dashboard page now uses less battery and processor time.
  Switching pages, or the wall display moving to its next page, still shows the
  last reading straight away.
- **The alarm banner, the Alarms page and the lamps redraw only when an alarm
  changes.** Helmcentral checks the alarm list every two seconds. It used to
  redraw the whole dashboard each time, even when nothing had changed, so a
  page now uses less battery and processor time whilst alarms are up.
- **Wind, depth, position and the other boat tiles redraw only when their own
  reading changes.** A new position no longer redraws the Wind tile, and a
  reading that's only a second older doesn't redraw anything. The dashboard
  used to redraw every tile each second, so it now uses less battery and
  processor time, and the last reading still shows straight away when you
  switch pages.
- **The Alarms page no longer says All clear before it has heard from the
  boat.** Until the first alarm list arrives it says **Waiting for the alarm
  list**, and the CHK lamp shows no data instead of clear. A page you open
  later always shows the last list Helmcentral received, never an empty one.

## [0.55.0] - 2026-10-26

### Added

- **Rate a passage.** A passage's edit panel has a **How it went** section with
  **Comfortable**, **Lively** and **Shouldn't have gone**. It saves as soon as
  you press it, and pressing the chosen one again clears it. A passage you
  sailed from a Mate weather window now also shows a **Planned with Mate** line
  under its route, with the day and verdict you sailed, a way to correct what
  Helmcentral recorded, and a link back to the chat if it still exists.

- **What you did with a passage plan is kept.** On a Mate weather window card
  you can mark **Went** (for a day on the card), **Stayed** or **Just looking**,
  and change it whenever you like. Helmcentral fills it in for you, marked
  "(inferred)", when the boat leaves on a planned day, within 4 hours of the
  plan and within 2 NM at each end of the first leg. It marks the card Stayed
  once the days have passed. Cards from before this update have no line.

- **Merged passages keep their legs.** Merging passages now leaves each one
  drawn in its own colour, with a sign named for the place where they met, and
  you can merge several at once from the passages list or from View together.
  A new **Starts a new leg** switch in the sign editor starts a fresh colour
  anywhere on a passage. The colours show on the map, the replay, the exported
  stills and the blog map, and splitting within a minute of that sign cuts at it and gives both halves the name
  back.

- **Choose the corner and zoom of the overview map.** On the passage replay,
  with Overview map on, the slider icon beside it puts the small map in any
  corner and makes it wider or closer by up to three steps. The choice is saved
  with the passage, and the blog embed and the still images draw it the same
  way. A passage you have not set keeps today's look, top right at the automatic
  zoom.

- **Settings → Offline coastline links to running your own Overpass server.**
  The coastline squares come from the same map server as place names, and a
  server aboard fills them far faster than a public one. The link opens the
  how-to in the help.

### Changed

- **The weather window card shows the wave period.** A wave line now reads
  "Waves 1.8 m · 6 s abeam" when the forecast has a period, so you can tell a
  short, steep sea from a long swell of the same height. There's no new limit.

- **Tidier replay bar.** The replay length is now one list instead of four
  buttons, and every choice is named by how long the replay runs: 60 s, 30 s,
  15 s or 8 s. Slow, Normal and Fast are now 30 s, 15 s and 8 s. Overview map
  is a switch, with its placement button beside it in the same box. The
  placement button stays there, greyed out, while the overview map is off.

- **Help pages rewritten to read the way you'd say them.** Every help page now
  uses shorter sentences and everyday words. Paragraphs that walked through
  several cases are now lists, one case per point, so it's quicker to find the
  one that applies to you. The contents page also links the anchor watch,
  alarm threshold and alarm notification guides and the Tiles, Alarm rules and
  Mate lookups reference pages, which were missing from it.

- **The Anchor Watch page is all map.** Depth, tide and the low-water clearance
  warning now sit in the readout panel in the map's top-left corner, above
  current, distance, bearing and radius, instead of in a card above the map.
  Drop and Raise float over the bottom of the map rather than taking a row
  under it. Adjust is the icon in the map's control stack. The chart now
  centres the boat and anchor in the part of the screen the panel leaves clear.
  On a phone the quiet low-water clearance line is left out; a shallow-water
  warning always shows. With no GPS fix, or on a display that cannot
  draw the map, the readings card and the Drop/Raise row stay as they were.
- **The overview map on the replay now frames the coast the way the blog embed
  and images do.** It shows a little more of the surrounding coast than before
  at the automatic zoom.
- **The power catamaran on the passage replay looks like a power catamaran.**
  Boats set to the power catamaran hull type are now drawn as two slender hulls
  with fine bows, open stern steps down to swim platforms, a salon with a
  near-vertical windscreen, and a flybridge with an enclosed helm under a
  hardtop. The boat is drawn to the length overall and beam in your vessel
  details.
- **Bigger photos on passage signs.** In the passage replay and in an exported
  blog card, a sign now shows its photo across the top of the card, with the
  title and note underneath, instead of a small thumbnail beside them.
  A sign with no photo stays a compact text card.
- **The camera shot you are editing is named.** On the passage replay, with
  Camera on, a line above the shot's settings reads "Shot 2 of 3 · from 09:41
  to 10:20" (with dates on a multi-day replay). The picked mark on the scrubber is larger and ringed so it is not
  mistaken for the playhead, and hovering any mark shows its number and time.

### Fixed

- **Hours with no wave forecast no longer pass as calm.** Where the wave
  forecast has no height for an hour, the weather window card now lists wave
  height under "Not checked". And where it has a height but no direction, the
  card no longer reads the sea as coming from the north. It compares the
  height with your wave limit for each sector and uses the answer if all three
  agree, because then the direction can't matter. Otherwise it lists
  "Not checked: wave direction". The Open-Meteo Marine plugin is rebuilt with
  the change, so there's nothing to do on upgrade.

- **Sign cards near the edge of the map are no longer cut off.** A card now
  slides sideways to stay inside the map, or drops below its point when there
  is no room above it, and keeps doing so as the camera moves during replay.
- **Passage replay no longer wobbles.** The camera and the boat turn smoothly
  with the course instead of twitching at every fix.

## [0.54.0] - 2026-10-24

### Added

- **See the forecast for a marina you're booked into.** On the Forecast page,
  choose **Plan a stay**, find the place by name and give your arrival and
  departure dates. A button beside the heading then switches the page to that
  place and back to the boat. The ten days show which are yours; days past the
  forecast say "No forecast yet" and when they will arrive. Times, tide station,
  warnings and the upper-air trend are the place's, and the heading always says
  it is not the boat's position. The dashboard, the clock and the alarms keep
  using the boat's own position. The stay disappears the day after you leave.
  Mate can read it and can propose one for you to apply.
- **Use an Overpass server on your own network.** Place names, Nearby and
  Mate's place lookups can now use an Overpass server aboard over plain http,
  for example `http://192.168.1.20:12345/api/interpreter`. Plain http is
  accepted only for addresses on the boat's own network: private and Tailscale
  addresses, `localhost`, a single word such as a Docker service name, and
  names ending `.local`, `.lan`, `.internal`, `.home.arpa` or `.ts.net`. Every
  other server still has to be https. Add the server's address to the plugin's
  allowed hosts and restart Helmcentral, as for any other server. Steps are in
  [Run your own Overpass server](https://github.com/gterrill/helmcentral/blob/main/docs/how-to/run-your-own-overpass-server.md).
- **Stop showing an alarm you can't act on.** Alarms raised by other devices on
  the boat, such as the plotter's Waypoint Passed, now have a **Stop showing**
  button on their card in the alarms drawer. Confirm it and that kind of alarm
  leaves the board and the banner, and stops reaching your phone, other
  notifications and Mate, whichever device raises it. Safety alarms (man
  overboard, fire, flooding, anchor, collision, grounding, distress calls, radar
  guard zones and anything marked as an emergency) never have the button. Find
  what you stopped, with the date, under Settings → Alarms → Ignored alarms, and
  choose **Restore** to see it again. Settings → Alarms → Ignored sensors now
  shows its dates too, and your existing ignored sensors carry over on their own.

### Changed

- **Place lookups reach twice as far.** Searching for a place by its full
  name, from the Forecast page or by asking Mate, now looks up to 200 nautical
  miles from the boat instead of 100. A partial name still searches the
  nearest 20.
- **Pick a sign to edit from a list.** The Signs panel in the passage replay
  now shows every sign as a button in time order, with its photo, title and
  time. Press one to jump to it and edit it. The marks on the scrubber and the
  pins on the chart are also bigger and easier to hit.
- **The plotter's route alarms read plainly.** "Perpendicular passed" is now
  **Waypoint Passed** and "Arrival circle entered" is now **Arriving at
  Waypoint**, each with one sentence saying what it means.
- **Offline coastline no longer downloads squares that are all land.** Squares
  with no part of them within 20 nm of the sea, such as the interior of a
  continent, are skipped, so the first fill finishes sooner and uses less of
  your data. Settings → Offline coastline says how many were skipped. Squares
  you already hold are kept. Asking Mate for the shelter at an inland position
  now says the spot is inland.

### Fixed

- **A web page tile that runs a few pixels past a wall display's edge no
  longer goes blank.** It is now trimmed to the screen instead. On a wall
  mounted upside down, a live camera picture still needs the camera page's own
  wall mode (HelmCam's kiosk view) to show.
- **A sign you opened on the chart no longer stays up through the replay.**
  Pressing Play now closes it, so each sign shows only for the time it is set
  to.
- **The "SignalK found" prompt no longer appears after an update restart, and
  never offers the server you already use.** It now waits until SignalK has
  been unreachable for a full 30 seconds, with the live feed connected, before
  searching, and leaves out the server you have saved, including when you
  saved it by name such as `boat.local`. If a search fails it tries again a
  minute later, and if the settings cannot be read during a restart it keeps
  trying to read them rather than treating the boat as unconfigured.

## [0.53.0] - 2026-10-24

### Added

- **Signs go into the blog card.** The Export menu has an **Include signs**
  switch, ticked to start with, with a line such as "5 signs, 3 photos" for the
  passages you are exporting and the size of the card. Readers see your signs
  as markers on the track at rest and cards on a line from the track while the
  replay plays, by the settings you gave them, and can open a photo large while
  the replay is paused. Photos are shrunk to 960 pixels on the longest side and
  saved again as new files, so the position and camera details inside the
  original do not go on your blog. A card can be at most 3 MB: over that, Copy
  and Download stop and name the largest photos, and you untick **Include
  signs** or take photos off some signs. A photo that cannot be read stops the
  export and names the sign. A card already on your blog does not change:
  export again to pick this up.
- **The blog map plays the 3D replay.** The blog embed and the downloaded page
  now show the land standing up, your boat as a model and the camera shots you
  set, with a **3D** switch in the Export menu that starts the way the passage
  map's 3D button is set. Readers get the hills and the sky, the camera follows
  or circles the boat and slows where you set a pace, and your chosen play
  length stretches by those paces. A reader can take hold of the map and the
  camera leaves it to them until the next shot. Without shots the card plays as
  before. The reader's browser now also asks Amazon's public elevation data for
  the land heights and, with a hull type set, a public software library for the
  boat model; nothing goes through the boat. A card already on your blog does
  not change: export again to pick this up.
- **Pin notes and photos to the passage replay with signs.** A **Signs** button
  on the replay bar opens a sign editor. Add a sign at any point on the slider
  with a title and a short note, or add a photo and it is placed where it was
  taken, from the time stored in the picture (read in the boat's time zone if the
  photo has none), and the editor shows the time used. A sign shows as a card
  as the boat passes, always, or from then on, and only two show at once. Before
  you press Play every sign is a small mark on the track, and tapping a card
  while the replay is paused opens its photo. Photos you upload are kept in a
  Documents folder called Passage photos. Signs survive splitting and merging
  passages.
- **Direct the passage replay with camera shots.** A **Camera** button on the
  replay bar opens a shot editor. Add a shot at any point on the slider, then
  choose Overview, Follow (chase the boat from astern, abeam or any angle) or
  Orbit (circle her), with its own zoom and pitch, and a pace of full, half or
  quarter speed to slow a stretch down. Shots show as marks on the slider and
  survive splitting and merging passages. With none, the replay frames the
  track as before. There is also a 60 second replay length. If your device asks
  for reduced motion, the camera cuts between framings instead of moving.
- **The replay shows your boat as a model.** With 3D on, the marker becomes a
  sailing or power monohull or catamaran to match the hull type set under
  Anchor Watch settings, sized from your vessel's length and beam and pointing
  along the course she is sailing. It stays at least about 40 pixels long when
  you zoom out; with no length recorded it is drawn at that size at every
  zoom, not to scale. With no hull type set, 3D off or the terrain unavailable, the
  plain dot stays.
- **Mate can tell you which wind directions an anchorage is sheltered from.**
  Ask about a bay or a position and Mate reads the charted coastline and reefs
  around it, then reports the compass arcs it is sheltered, partly sheltered
  and exposed from, with how far the nearest land or reef is in each direction.
  When you are weighing anchorages against a forecast, it sets those arcs
  against the forecast wind for the stay. Inside a reef it gives the reef's
  width each way and says that reef shelter depends on the tide. It is an
  estimate from the chart: it cannot see wind bending round a headland, gusts
  down a valley or swell wrapping a point, and Mate says so.
- **An offline coastline, kept aboard.** A new **Offline coastline** section in
  Settings downloads the coastline and reefs for every 1° square within 200 nm
  of the boat, in the background and nearest first, and keeps squares you have
  downloaded before. Mate's shelter answer reads it, so it works with no
  signal. It is off until you switch it on, because filling it uses the
  internet. A map shows what is held, how old each square is, and any square
  that failed and why. If the public map server rate-limits or refuses the
  boat, the page says so and when it will try again, and nothing is marked as
  failed. A full set (around 50 squares at 200 nm) takes 40 to 60 minutes on a
  good link. Once a week Helmcentral checks whether the public map has
  changed inside each square; the page shows how many edits since, and an
  **Update** button downloads those squares again when you choose. **Clear
  coverage** deletes everything. Where the coastline is off, missing, or still
  downloading, Mate says exactly that instead of guessing. It needs the updated
  OpenStreetMap place-names plugin that ships with this release; with an older
  copy the page says the plugin is too old. Areas across the 180° meridian or
  near a pole are not covered.
- **The passage replay can show the land in 3D.** A 3D button beside the
  satellite button tilts the map and raises hills and headlands, with the sea
  kept flat so the chart and photographs lie level on the water. You can drag to
  turn and tilt the map. It is on by default and remembers your choice
  separately; switch it off for the flat map as before. If the elevation data
  cannot be fetched the map says "Terrain unavailable" and stays flat. The
  overview map stays flat.
- **The replay draws the track as the boat sails it.** The line is solid up to
  the boat and faint ahead of it, instead of lighting a whole passage at once.
- **The passage map can show satellite photographs.** A satellite button in the
  top right corner switches the map between the chart and aerial photography,
  with the track edged in white and place names kept on top. It is on by
  default and remembers your choice separately from the Anchor Watch map. Saved
  pictures (the 1200 × 630 and 2:1 images) follow the button and carry the
  photograph credit when photographs show; if any of the photographs cannot be
  fetched the picture is not saved, rather than saved with a patch missing.
  The blog embed is unchanged and always shows the chart. The overview map in
  the replay moves down a little to make room for the button.

### Changed

- **The anchor swing trail now shows everything since the anchor went down,
  and survives a restart.** It used to hold only the last hour and a half or
  so, which on a long night left a small scribble hidden under the boat
  arrow, and it was lost whenever Helmcentral restarted. It is now kept
  until the anchor is raised. A point is saved when the boat has moved a
  metre or a minute has passed, and the newest 10,000 points are kept and
  drawn. Moving the anchor position starts the trail again; changing only the
  radius does not. If the trail cannot be restarted, dropping, moving or
  raising the anchor now reports the failure instead of carrying on.

### Fixed

- **An upside-down wall display with room to spare starts at the top left.**
  Below magnification 1, the pages sat in the bottom right corner with an
  empty band along the top and left edges, and the bottom row could fall under
  the screen's frame. The spare room now goes along the right and bottom
  edges, as on an upright screen.
- **Mate no longer moves the page when an answer finishes.** If you scrolled
  to keep reading while an answer was still being written, the view jumped
  away from where you were reading as soon as it finished, often back to an
  earlier question. It now stays where you left it.
- **The chart works again on a new install or after clearing the map cache.**
  Carto changed the map style Helmcentral draws its chart from, and a boat with
  nothing stored yet got a blank chart. Helmcentral now reads the new style, and
  keeps place names for islands, bays, straits, marinas and peaks. Some water
  names in the Whitsundays (Cid Harbour, Nara Inlet, Hayman Channel) are not in
  Carto's new data and no longer appear.
- **The Battery & Power dawn figure no longer shows tonight's state of charge
  as tomorrow's when the bank's capacity is unknown.** Without a capacity
  there is no charge rate, and the tile treated that as a bank holding steady:
  79% at 22:00 read as 79% at dawn while the bank discharged at 35 A. It now
  shows `—`, and the hover says whether the bank's capacity or its current is
  missing. A bank sitting at rest now reads `+0.0 %/h` rather than `—`, so a
  steady bank and an unknown rate no longer look the same.

### Breaking

- **The stored chart is dropped on the first start after upgrading.** Carto's
  new map data cannot be drawn from what was saved for the old, so Helmcentral
  deletes the saved chart tiles, style and fonts and fetches them again as you
  use the chart. Do this while you have an uplink: open the chart over the
  areas you will need before heading out of range. Saved satellite imagery is
  not touched.

## [0.52.0] - 2026-10-22

### Changed

- The engine cluster no longer shows a RUN tab while the engine is healthy;
  WARN or ALARM now shows in the tile's title row. Engine hours sit in the dial
  centre, under the scale note. The card is shorter, and the tile can be
  resized down to five columns by eight rows (it was six by fourteen), so it
  no longer carries empty space above and below the card.

### Fixed

- **Disconnecting shore power no longer raises "Instruments Silent" for the
  shore charger.** When your boat reports its AC input switching to battery
  (a Victron GX does), a device whose last report was within a minute of the
  switch, before or after, is left off the card. A device that goes quiet on
  shore power, or more than a minute from the switch, is still reported.

## [0.51.0] - 2026-10-21

### Breaking

- **The Routes page is gone.** Saved routes are no longer kept; plan and
  activate routes on your chartplotter or in a SignalK app. The Route tile now
  shows the route SignalK is navigating, or the next waypoint when the plotter
  only sends a go-to, and does nothing when tapped. If SignalK names a route
  that cannot be read, the tile keeps the next waypoint and says why the route
  details are missing. The arrival time uses your
  speed over the ground alone, so it is blank while you are stopped. The
  Nearby map and the clock follow the same route. What to do on upgrade: if you
  want to keep a route you planned here, recreate it on your plotter first; the
  old `routes.json` file in the data folder is no longer read and can be
  deleted. The `ROUTES_FILE` environment variable is gone too; remove it from
  your compose file if you set it. A bookmark to `/routes` now opens the
  dashboard.

### Added

- **Rate Mate's answers.** Under each answer Mate gives you in the chat there
  are now a thumbs-up and a thumbs-down, with an optional short note about what
  was good or what was wrong. Tap the thumb you chose again to take the rating
  back. A rating stays with the answer and shows again when you reopen the
  conversation; older answers you never rated cannot be rated later. Helmcentral
  also keeps which lookups Mate made for each answer so it can be traced:
  kept for rated answers, cleared after seven days for the rest.

### Changed

- **Engine and equipment lists offer only real sources.** A source is offered
  for an engine only if it reports RPM, and likewise voltage for a battery,
  level for a tank and so on for each kind. A source that carries a single stray
  reading, such as `propulsion.0` with only exhaust temperature, is no longer
  listed. Helmcentral now keeps its own record of what the boat reports, so an
  engine that is switched off is still listed once Helmcentral has seen it
  report, marked "Not reporting now" with the day it was last seen (for up to 90 days). On a new
  install, Discovery says since when it has been watching. If you log to
  InfluxDB, Helmcentral reads that log once to start the record, so
  switched-off engines are listed straight away; InfluxDB is not required. This
  applies to Discovery, the Roles lists and the Source list on an item's page.
- **Mate no longer works from saved routes.** It no longer finds waypoints from
  saved routes when you ask about a place, and it no longer offers to create a
  route after a passage recommendation.

### Fixed

- **An engine set to a source that never reports RPM is flagged.** An item
  whose source reports other readings but has never reported its own shows a
  red warning in Discovery, under its role and on its own page, and the Engines
  step in Setup stays open and names the item and the source. Helmcentral says
  this only after watching the boat for a week with SignalK connected (or reading a
  history log); until
  then, and for a source it has never seen, the step stays done and says it
  couldn't check yet. If an engine item was set to the wrong source, choose the
  one that reports RPM on the item's page.
- **Mate's passage time estimate says which engine reading is missing.** When
  an engine's fuel rate or RPM was not logged in the window, the answer names
  the engine instead of "no underway history", and points at its source in
  Settings when that source has never reported RPM.
- **The weather window card no longer fails when Mate gives both a passage time
  and a speed.** The passage time is used and Mate is told the speed was ignored.

## [0.50.0] - 2026-10-20

### Breaking

- **Passage limits are no-go only.** **Settings → Mate → Passage limits** now
  asks for two figures per wind sector: the no-go wind and, if you want waves
  checked, the no-go wave height. Marginal is worked out as 70% of no-go, and
  gusts are judged against the no-go wind plus 10 knots, so there are no gust
  or marginal boxes any more. **Wind against tide** is now one number, **Reduce
  wind limits by**, in knots: while the stream runs against the wind, the no-go
  wind comes down by that much, marginal follows at 70% of the lowered figure,
  and gust limits do not change. A marginal cell on the weather window card
  says what made it so, such as "Wind SE 12 kn ahead (marginal from 10.5)".
  What to do on upgrade: open **Settings → Mate → Passage limits** and check
  your no-go wind and wave figures, which carry over. Your marginal and gust
  figures are dropped, because they are worked out now; compare the card's
  marginal and gust results with what you meant. If you had set a wind against
  tide band, it is dropped too, and the check stays off until you enter a
  reduction. Settings pages left open from before the upgrade must be reloaded
  before saving.

### Fixed

- **Mate now grades your passage.** Asking whether or when to go, or for the
  outlook on a trip, now makes Mate check the trip against your passage limits
  and show the go/no-go card alongside its briefing. Before, it gave the
  briefing alone and the card never appeared. If the limits are not set, Mate
  tells you to set them in **Settings → Mate → Passage limits**.

## [0.49.0] - 2026-10-20

### Added

- **Passage limits.** **Settings → Mate** has a new **Passage limits** group:
  marginal and no-go levels for wind, gusts and wave height, set separately for
  wind ahead, abeam and astern, plus a wind-against-tide limit and a
  daylight-arrival switch. Nothing is filled in for you. While Mate is on, the
  Setup list shows **Passage limits** as an optional step that never holds up
  the rest of setup.
- **Which days can we go?** Ask Mate about a trip in legs, with stays along
  the way, and the answer comes with a grid card: one row for each departure
  day, one column for each leg, and in each cell go, marginal, no-go or no
  data, with the times and the figure that decided it. Each leg is judged on
  the day it would actually sail, against your passage limits for the wind
  angle on that part of the course. Anything not checked, such as wave height
  with no wave forecast, is listed rather than passed.
- **Charts from Mate.** Ask Mate to chart any logged reading, such as
  alternator current against house bank state of charge or engine RPM against
  load, over time or one against the other, and across date ranges you name
  for a before-and-after. The chart appears under the answer and redraws when
  you change its dates.

### Fixed

- **Alarm units.** Alarm cards and notifications for readings that come from
  the boat's instruments (battery, tank, engine, wind and the rest) now show the
  reading in your units. They had been showing the raw figure with no unit and
  no conversion, such as a battery at 12 per cent as 0.12 or a pressure in
  pascals, because Helmcentral never received the unit the boat declares for
  each reading. It now does, and it remembers the last unit each reading
  declared, so an alarm on a device that is switched off, or that has not
  reported since Helmcentral restarted, still names its unit. A reading that
  has never declared a unit still shows none. Nothing to do on upgrade.
- **Silent instrument alarm.** A serial (NMEA 0183) instrument that goes quiet
  is now named for its connection, once, instead of as six "An instrument (1
  of 6)" rows. The card says what the device carried ("Vesper Cortex has stopped
  sending AIS, position, heading, wind, depth, boat speed, log and weather"), is titled "Vesper Cortex Silent" ("Partly Silent" when some of
  it still sends, "Instruments Silent" when several devices are quiet) and keeps
  that title until it clears, and has a single **Ignore Vesper Cortex**
  button.
- The **Embed** tile's settings now say "inside this tile", not "inside this
  widget".

## [0.48.0] - 2026-10-17

### Added

- **Setup list.** **Settings → SignalK** opens with a **Setup** list: connect to
  SignalK, assign engines, choose the house bank, then the charging and power
  sources. Only the next step shows its detail, with a button that goes to the
  exact control. On the dashboard, a quiet line under the header ("Setup: 2 of
  4 done · Next: House bank") opens the same place. Both are worked out from
  what is saved, so a step that comes undone, such as a deleted item or a
  cleared role, comes back by itself, and both go away when setup is done. A
  SignalK dropout does not reopen a finished setup. Neither appears on wall
  displays.
- **Roles lists what the boat reports.** Each dropdown under **Settings →
  SignalK → Roles** now lists, after your items, a **From the boat** group of
  sources no item owns, with the boat's own name and a live reading. Choosing
  one makes an equipment item named after the role and picks it; the item
  exists at once, while the role itself is saved with **Save**. When the boat
  reports nothing of that kind, or SignalK isn't connected, the dropdown says
  so.
- **This boat has none.** The alternators, shore charger, inverter, generator
  and generator AC input can be answered **This boat has none**, which finishes
  that setup step and shows no figures, the same as **Not set**. With no
  generator, its AC input is not needed and is cleared. The house bank and the
  voltage readout battery still need an item.

### Changed

- The Battery & Power tile now says "No house bank chosen" and links to
  **Settings, SignalK, Roles** when no house bank is chosen, instead of showing
  dashes that looked like a bank that had stopped reporting. If the chosen
  bank's item has lost its SignalK source, it says that instead. Solar, Loads,
  Shore and the readout battery's voltage still show. A chosen bank that is not
  reporting still shows a dash (—).
- The Generator tile's "not set up" message points to **Settings, SignalK,
  Roles**, where it used to name Settings, Vessel. With the generator answered
  **This boat has none**, it says the boat has no generator.
- Both tiles' links open the house bank and generator dropdowns directly.

## [0.47.0] - 2026-10-16

### Breaking

- **Engines, batteries, tanks and the rest are now equipment items.** On the
  first start after upgrading, Helmcentral moves your Settings → Vessel engines
  and house bank, your alternator and electrical choices, and your tank labels
  onto equipment items and a new **Roles** list in **Settings → SignalK**. Every
  battery, solar controller and tank you had only named, and every engine row you
  had not linked to an item, gets an equipment item created for it, with that
  name and mapped to that source, so no name is lost. A tank's item is named for
  what it holds, such as Fresh water PORT, Black water STBD or Fuel PORT FWD,
  because the old labels were per type. Delete any you do not want
  on the Equipment page. Then check what it could not move. Under **Roles**, choose the port and starboard
  alternators, the shore charger, the inverter, the generator and the voltage
  readout battery if they show **Not set**, then choose **Save**. Until you do,
  those figures show no data. The startup log lists every name and role it could
  not move.
- **Hour meters come from the item's source.** The hand-entered hour meter on
  an equipment item is gone. An engine or a generator reads its hours from its
  SignalK source. Where the old path named an engine's or generator's runtime
  counter it was converted; otherwise assign the item a source (**Source**
  on the item's page) to get its hours back.
- **House bank figures moved to the bank's item.** Capacity, cell count and the
  voltage overrides are now on the house bank's equipment item, under
  **SignalK source**. The conversion copies them.
- **Settings → Vessel** now holds only the boat's identity and particulars.
  **Battery names**, **Solar names** and the **Labels** on the SignalK page are
  gone: a source takes the name of the equipment item assigned to it.
- **State-of-charge bands can follow the house bank.** Rules that watched the
  old house bank were converted to follow it. Add new ones with **Follow the
  house bank's state of charge** in the Alarms drawer.

- **Tiles that read the boat's equipment now follow an equipment item.** On the
  first start after upgrading, Helmcentral switches these to the item that owns
  the source they read, keeping their layout and display choices:
  - every Engine Cluster;
  - every gauge group whose gauges all read under one source, whatever the kind
    of equipment: an engine, battery, alternator, solar controller, tank,
    generator, charger or inverter;
  - every indicator lamp, on a page strip or the ribbon, that reads under the
    source of an item.

  Then check what is left. A cluster or gauge group whose source has no item
  yet, and a fuel bar for a tank with no item, shows **Pick equipment**: open
  the tile's settings in Layout mode and choose the equipment, mapping the item
  first under **Settings → SignalK → Discovery** if it has no source. A lamp
  whose source has no item keeps its path, so it does not follow an item until
  you change it to read equipment. A gauge group whose gauges read several
  different sources stays a group of the paths you picked. A reading the old
  tile took from outside its engine, such as an exhaust temperature under
  `propulsion.0` on a boat whose engines are `port` and `starboard`, is dropped
  and has to be re-added to a tile that can read it. The startup log lists each
  tile and reading it changed or could not move.
- **Gauge zones live on the equipment profile, and none are lost.** A zone on
  one of those tiles moved to the equipment's profile when the profile had none
  for that reading, converted into the profile's unit. In every other case the
  tile keeps its zones, stored on the tile, and they keep raising the same
  alarms under the same names: when the tile has no equipment yet (it says
  **Pick equipment**, and your oil pressure, coolant, RPM and alternator alarms
  carry on from the source it read), when the equipment has no profile, when the
  profile has no gauge for the reading, and when the profile already has
  different zones for it (the profile's apply until you choose). Open the tile's
  settings and choose the equipment. Zones move to its profile when you save the
  tile; if the profile has its own for a reading you see both sets and choose
  which to keep. If the equipment has no profile yet, link one on the item's
  page, and the zones move the next time you save the tile. A reading on one of
  these tiles now always uses the profile's unit once its zones are on the
  profile. The startup log lists every tile whose zones stayed. The alarms from
  zones on the profile have new names and start with no history or
  acknowledgement; look in the Alarms drawer to confirm they are all there. A
  fuel bar keeps its own low-fuel zones, because a tank has no profile, and its
  alarm is kept. Engine cluster telltales (coolant, gearbox) now raise alarms
  from their zones too, which they did not before.
- **Apply gauge zones is gone** from the equipment page, along with the equipment
  profile dialog on the Add Tile menu and the find-and-replace in gauge groups.
  Add an Engine Cluster or a gauge group and choose the equipment instead.
- **Engine Cluster tiles are rebuilt for the new card.** The temperature strip
  under the dial is replaced by two arcs and the footer, a readout card shows one
  reading, a fuel rail shows two tanks and has no caption or edge choice (it sits
  on the outer edge for the engine's side). On the first start the coolant
  moves to the left arc and the gearbox temperature to the right arc, and any other strip reading is dropped (the log says which). Open each
  cluster's settings in Layout mode, choose **Engine side**, press **Fill slots
  from profile**, then choose the starter battery, alternator and exhaust
  equipment for the footer. The exhaust needs an equipment item mapped to its
  own source (for example `propulsion.0`), because it is not part of either
  engine. The alarms from a footer or arc reading's zones are named for its
  equipment and start with no history.
  A cluster that held several readings in a corner or more than two tanks keeps
  the first reading of each corner and the first two tanks (the startup log says
  what was dropped), and a cluster narrower than the new card is drawn six
  columns wide.

### Changed

- AIS distress alarms are now titled "AIS Distress" instead of showing the
  receiver's internal name, and they clear on their own about ten minutes
  after the beacon was last heard, never while the SignalK feed is down or has
  just reconnected. Before, a SART that had stopped
  transmitting stayed on the board indefinitely.

- The Engine Cluster tile is redrawn as one full engine card: a lit tachometer
  that reads plain rpm, four readout cards with bars drawn from each reading's
  own limits, coolant and gearbox oil temperature arcs, forward and aft fuel arcs
  with litres, and a footer with the starter battery voltage, the alternator
  (current, power, temperature, charge stage, a one-hour trace and the battery
  management system's charge limit), the gearbox oil pressure and the exhaust. A starboard
  tile is a mirror of a port one. An engine that is switched off says `OFF` once,
  in the tile's title, and the status word (RUN, WARN or ALARM) appears only
  while the engine is turning on live readings. The tile needs half the board's width. The
  alternator, battery and exhaust are equipment items like the engine: choose
  them in the tile's settings, and **Fill slots from profile** takes the
  alternator the Roles setting names for the engine's side.

### Fixed

- The Acknowledge and View buttons in the alarm banner are readable on an
  emergency. They were white on white.

### Added

- **Helmcentral lists what uses an equipment item before you clear its source or
  delete it.** Clearing or changing the item's **Source**, or deleting the item,
  now asks you to confirm when anything uses it, and names what: the tiles by
  page and title, the features that read it, the roles it plays and the alarm
  rules that watch it. An item nothing uses asks nothing more than before.
  Deleting an item also clears the roles that named it, so they show **Not
  set** instead of pointing at an item that no longer exists.
- Helmcentral now warns when an engine stumbles. If rpm at a steady throttle
  repeatedly dips and recovers (three times in ten minutes on one engine), a
  warning such as "Port Engine RPM Dips" is raised, with the count and the
  deepest dip on the card. It needs no setup: every engine that reports rpm is
  watched, the warning arrives switched on, and it clears once the engine has
  gone ten minutes without a stumble. Dips that hit both engines of a twin at
  the same moment are treated as sea state and ignored.
- **Settings → SignalK → Discovery** lists what the boat reports (engines,
  batteries, alternators, solar controllers, tanks, generators, chargers and
  inverters) with live readings and the equipment item each belongs to. Assign
  a source to an item, or create an item for it.
- **Source** on the equipment page picks the item's SignalK source from what the
  boat reports. A battery item holds its cell count and capacity.
- **Tiles pick equipment from a list.** An Engine Cluster, a gauge group set to
  show equipment, and an indicator lamp for an engine, generator, charger,
  inverter or alternator choose an item instead of a SignalK path, so a tile
  follows the item when its source changes and shares the profile's alarm limits
  with every other tile and alarm for that equipment. An item that is not mapped
  yet can be mapped from the picker. A tile whose equipment is missing, not
  mapped or not reporting says so, with a link to the item.

## [0.46.0] - 2026-10-15

### Breaking

- Choose your engines and your house bank in **Settings → Vessel** after
  upgrading. Helmcentral no longer works out which engines you have, or which
  battery is the house bank, from what the boat reports. Until you pick them,
  the Alternators tile stays hidden, anchor auto-raise does nothing, Mate says
  engines are not set up when asked for a passage estimate, and the Battery &
  Power tile shows `--` for state of charge, voltage, current, power and
  capacity, with no dawn figure. The Generator tile leaves out its Battery at
  Finish forecast during a timed run. Tick your engines under **Engines** and
  pick the bank under **Power**. If the bank's battery monitor does not report
  its capacity, enter it under **Power** too, or the charge rate and the time
  to full or empty read `--`.
- State-of-charge band alarm rules must be on the house bank you chose. The
  bands on the Battery & Power tile only read rules for that bank's state of
  charge. If your rules are on another battery, edit their **SignalK path** in
  the Alarms drawer to the chosen bank's state of charge (see Set battery
  state-of-charge bands).
- The `INFLUX_SOC_MEASUREMENT` setting is gone. The dawn projection and the
  state-of-charge history now follow the house bank, so remove the variable
  from your environment; it is ignored.

- Choose your port and starboard alternators in **Settings → Vessel →
  Alternators** after upgrading. Until you do, the Alternators tile shows
  nothing, because Helmcentral no longer assumes which of the boat's alternator
  numbers is port and which is starboard. Each list shows the alternators the
  boat reports, with their live current, so you can tell them apart with the
  engines running. Leave a side as Not set if the boat has none there.

- Choose your generator, generator AC input, shore charger, inverter and voltage
  readout battery in **Settings → Vessel → Electrical**, and name your solar
  controllers under **Solar names**, after upgrading. Helmcentral no longer
  assumes which of the boat's numbers is which. Until you do: the Generator tile
  says the generator is not set up and its Start and Stop buttons are gone
  (before, they sent the command to whichever generator was number 0); the
  Shore line, the generator's power and the AC part of Loads on the Battery &
  Power tile are blank; the voltage beside the charge rate in that tile's
  footer is left out; and each solar controller is named "Array" and its
  number. The voltage that used to be labelled 24V is now whichever battery you
  pick, with that battery's name.
- The `INFLUX_SHORE_MEASUREMENT` and `INFLUX_GENERATOR_MEASUREMENT` settings are
  gone, so remove them from your environment; they are ignored. The dawn
  projection now skips nights when shore power was connected or the generator
  ran by looking at the shore charger and generator you pick under Electrical.
  A night is not screened for one you leave as Not set.

### Added

- Passages, cruises and selected passages can be shared from the new **Export**
  button. **Copy blog embed** shows an animated map to copy and paste
  into a Ghost HTML card: readers tap play, the map drops from a view of the
  globe onto the track, and the boat follows it with the local time, each night
  at anchor shown as a short pause, and the distance, time underway, nights,
  fuel and highest gust underneath. Choose a 15, 30 or 60 second replay and
  whether the small overview map shows. The embed loads public map tiles and
  never connects to the boat. **Download embed as HTML** gives a page for
  previewing it, and two image downloads (1200 × 630 and 2:1) make a still
  picture of the track with its title and figures.
- Helmcentral now keeps its own log of where the boat has been, with speed,
  wind, house battery, fuel rate, generator and fresh water recorded alongside
  each position. It starts on its own after the upgrade and needs no setting;
  to include the house battery, choose the house bank under Vessel settings.
- A new **Passages** page in the sidebar lists every leg the boat has run,
  newest first, with the distance, time underway and the places it went from
  and to. Open one to see its track on the chart, replay it with a moving boat
  and the local time, and read its speed, fuel, wind and battery figures.
  Figures the boat did not record show `--`.
- Passages can be grouped into a **cruise**. A cruise page replays its passages
  one after another and adds nights, fuel, wind underway and at anchor,
  generator hours, the lowest house battery and its charge at dawn each night,
  and the fresh water used and taken on, with each fill marked as Watermaker or
  Dock fill.
- Passages can be edited: rename them, change the two ends, add notes, split
  one in two, trim its start or end, merge neighbours, hide one, or move it to
  another cruise.
- Trips Helmcentral did not record can be added with **Import GPX** on the
  Passages page. See the new pages on importing GPX tracks and editing
  passages in the help.

### Changed

- A phone, tablet or laptop no longer reloads itself when a new version is
  deployed. It checks every five minutes, shows an "Update available" message
  with a **Reload** button, and changes the version line in the menu to "Update
  available · Reload". If you have unsaved work, including a note draft, a new
  note or a service log entry, you are asked before the page reloads. Before,
  an unsaved note could be lost.
- The wall display and a Home Screen dashboard still reload by themselves, as
  soon as an update is found. A Home Screen app on any other page or with
  a sheet or dialog open over the dashboard, and a dashboard in the browser's
  full screen mode, ask instead.
- A screen that cannot open after an upgrade now says so and offers Reload,
  whether the update or the connection is the likely cause. Before, the whole
  page could go blank.

### Fixed

- The "Helmcentral updated to" message now appears whenever a browser first
  loads a version it hasn't shown before, including after a manual refresh, in
  a new tab and when reopening a Home Screen app. Before, it appeared only
  after the page had reloaded itself. The wall display shows it for a few
  seconds too.
- The tidal current on the Wind tile and in Anchor Watch now shows which way
  it is setting as well as its rate when the instruments report the two
  together. Before, it showed only the rate, or nothing at all.
- A moment with no GPS position, or only half of one, no longer raises a GNSS
  alarm at anchor when the next fix arrives. The boat's position holds at the
  last good fix through the gap.
- Mate now knows which page or document you are on for every question asked
  from the Mate sheet, including ones you type. Before, only a question that
  arrived by voice, from Help, Settings or a text selection carried it.
- Questions sent to Mate from Help, a Settings page or a text selection get an
  answer written for reading. Only a question you speak gets one written to be
  read aloud.
- Asking Mate a second question while it is still answering the first now says
  "Mate is still answering the last question." instead of a technical error.

## [0.45.0] - 2026-10-14

### Added

- Mate can see the active alarms, so asking it about an alarm investigates the
  sensor that raised it, not a guess from the device name on the card.

### Fixed

- The solar chargers' panel voltage (for example 72 V) no longer raises an
  "Impossible sensor reading" alarm named after an alternator regulator. A
  device that only reported an empty value for a battery no longer counts as
  sharing that battery.

### Changed

- In Inventory, clicking **Equipment** while an item is open now takes you
  back to the equipment list. It used to do nothing.
- In Inventory, the unsaved-changes prompt now appears only when a click would
  close the item you are editing or the bin or stocktake you are working in.
  Moving around Inventory while that stays on screen no longer asks.
- In Documents, choosing a folder while reading a manual section now takes one
  press of Back to undo, not two.
- **New item** in Inventory now always shows the equipment list address, rather
  than the address of the item you had open before.
- Going back to a page you opened in the last few minutes shows what it had at
  once and brings it up to date behind the scenes, instead of a spinner each
  time. Lists also stay in step with each other: adding an item from a bin, or
  moving a document, updates every other list showing it without a reload.
- When the server refuses a change, the message you see is now the server's own
  reason wherever it gives one, rather than a generic "could not save".
- If a list can't be refreshed (a dropped link to the boat, a server restart),
  Inventory, Maintenance, Documents, wall displays and the dashboard keep
  showing what they last had, with a small "Couldn't refresh" line saying why,
  instead of replacing the whole list with an error.

## [0.44.0] - 2026-10-12

### Added

- A document's Details page now shows where it is filed as a trail of
  clickable folders (**Documents** > folder > document), so there is an obvious
  way back to the list. It also has a **Move** button that files the document
  in another folder, and the **About the file** column shows its current
  folder.
- Mate knows which document you are looking at. Open Mate from a document's
  Details page and "what period does this cover?" is taken to mean that
  document; Mate reads it instead of asking which source you mean.
- The screen stays awake while the dashboard is full screen, so a tablet at
  the helm no longer dims and locks mid-watch. This works only when you open
  Helmcentral by an `https://` address such as the Tailscale name; over plain
  `http://` the screen sleeps as before. On an iPad or iPhone, where
  Helmcentral opened from a Home Screen icon has no full screen, the screen
  stays awake while that copy shows the dashboard.

### Changed

- The Documents page now has a folder pane on the left. It shows every folder
  as a tree with the number of documents in each, so you can jump to any folder
  in one click instead of stepping through the levels. The breadcrumb and the
  folder rows in the list are gone, and the list shows documents only.
  A manual's count covers every document inside it. **Unfiled notes** moved from a button in the toolbar to the bottom of the
  pane. Rename, Move, Treat as a manual and Delete for a folder are now on the
  menu beside the folder in the pane. Drag a document, or several ticked
  documents, onto a folder to file them. On a phone, tap **Folders** to open
  the pane.
- The Documents list now has an **Added** column showing when each file came
  into Helmcentral, and Name, Added and Size sort when you click their headers.
  The list opens newest first and remembers your sort.
- The Documents list no longer has a Status column. A document still being read
  shows an amber icon, a failed one a red icon, and the reason stays under its
  name. Tags show three to a row with a "+N" count for the rest.
- Mate's reply footer now shows how many tokens of the prompt came from the cache, when any did. Follow-up questions in a conversation stay with the same provider, so the cache is reused more often and replies cost less.
- The place name on the position tile now shows the marina or harbour when
  you are alongside, where it used to show a dash. A marina within about
  500 m ranks below an anchorage and above a bay. The lookup also reaches about 10 km
  from the boat, up from about 5 km, so open water off a coast is more likely
  to get a bay or island name.
- The Anchor Watch map no longer shows a Scope row in its readout panel. The
  rode planner still gives the recommended rode.

### Fixed

- In Documents, going Back to a manual section you were reading in another
  folder now reopens that section, and Forward still works afterwards.

## [0.43.0] - 2026-10-08

### Added

- The indicator ribbon now shows labelled cells instead of small dots. Each
  cell has an icon, the full name and a reading (On, Off, or `--` with no
  data), and fills green when on. A cell turns amber or red while an active
  alarm is on its signal, and tapping it opens the alarms panel. A dashed
  outline means no data or a silent source. The CHK cell now shows how many
  alarms are active and the worst one's title. Lamps can be given an icon and
  a group heading in the ribbon dialog; existing ribbons pick up icons
  automatically and need no change.
- Mate fills in PDF forms. Attach an insurer's declaration or a marina's
  application and ask it to fill it in. Mate reads the form, takes what it needs
  from your vessel details, asks you once for anything missing, and puts the
  filled-in form under its reply to open, download, save to Documents under a
  title and folder you choose, or dismiss. It works on fillable forms and on
  flat ones, where it writes after the question and ticks the box beside the
  right marina or option. It never fills a signature, and never puts the form
  in Documents until you save it. A form that is a scan cannot be read.
- Settings, Vessel has a new **Owner & insurance** section: owner name, phone
  and email, insurer, policy number, home marina, berth and storm delegate, and
  Particulars gains length overall and beam. Mate reads these for forms and can
  offer to save answers you give it in a chat.
- **Mate answers history questions from the logged instrument history.** Ask
  "when do the alternators cut off charging on the last few passages" and Mate
  finds the recent passages from the logged speed, then reads what that reading
  did on each one, instead of answering from documents and old conversations.
  It needs the history log set up.
- **Battery names** in Settings → Vessel. Each battery number the boat reports
  is listed with what the boat calls it; type your own name for it, such as
  "Port Engine Starter Battery". Alarms use it. Leave it empty to keep
  the boat's own name.
- The version in the sidebar footer now opens that release's notes.
- After Helmcentral updates itself, a message says which version it is now
  running, with a link to that release's notes.

### Changed

- The Tanks tile no longer shows fuel aboard, range and time to empty under
  the tank bars. It was showing STALE on range and time to empty with the
  engines off.
- The impossible, frozen and silent sensor alarms now name each sensor and say
  what is wrong, for example "Port Engine Starter Battery voltage 75.7 V ·
  above the 70 V any 12, 24 or 48 V bank reaches", instead of a count and a
  raw SignalK path. The Ignore button and the Ignored sensors list in Settings
  use the same names. Notifications carry the same text.

### Fixed

- **Fuel economy, range and time to empty no longer show stale with the
  engines off.** With every engine switched off they read `--`, the same as
  when stopped. A stale marker now means a reading has actually stopped, such
  as a gateway going quiet.
- A solar charger's panel voltage, which the boat's network reports under a
  battery number, no longer raises an Impossible sensor reading alarm. Battery
  numbers that only chargers report, where those chargers also feed another
  battery number, are not checked as batteries.
- An indicator cell for an inverted lamp, such as a bilge float where off is
  healthy, now reads what the signal is doing. A signal at zero reads Off and a
  signal present reads On, while the colour still shows whether that is
  healthy.
- Indicator ribbon cells no longer get cut off on a tile that is too short for
  every row. The cells scroll inside the tile, so a lamp with an active alarm is
  always reachable.
- A silent-device alarm for a device with no name set now reads "An
  instrument has stopped sending", or the maker's name when it is known,
  instead of showing a network address or plugin name.
- Saving a filled-in form from Mate again, after you deleted the document it
  made, now makes a new document instead of failing with an error.

## [0.42.0] - 2026-10-06

### Added

- A **Summarise to note** button beside the microphone in the Mate message
  box saves what a conversation worked out as a note. Mate writes up the facts
  established about your boat, what was ruled out and why, and the procedure
  that worked, and shows it to you first; nothing is saved until you press
  Save. You can edit the title and text, and tick the equipment to link the
  note to. A conversation keeps one note, and the button then reads **Update
  note**.
- Searching your Mate conversations now finds a word anywhere in a
  conversation, not just in its title. A match inside a conversation shows a
  line of the matching text under the title, on both the Mate page and the
  search from the Mate sheet.
- Mate now checks your earlier conversations before it advises on a specific
  piece of the boat's equipment, and whenever you refer to earlier work. What
  you established then, such as a fitting you showed it is not there or a
  procedure you ruled out, is treated as fact about your boat, so Mate stops
  suggesting it again in a new conversation. Mate also no longer offers
  options that depend on a valve, port or fitting it has not confirmed is
  aboard.

### Changed

- The Nearby tile's tour of places no longer loses the boat off the edge of
  the map. A place close by still gets a street-level view centred on it; for
  one further off, the view slides toward the boat so both stay on screen.
- An engine you have switched off now reads **OFF** in grey on its engine
  cluster tile, with no amber edge, instead of **STALE** with the hours since
  the engine went quiet. Hover over it for the time it was switched off.
  Gauges, gauge groups and lamps bound to that engine's readings show **--**
  with no **STALE** marker, so the tiles agree, and a stale-data alarm rule
  on those readings stays quiet. If the network feeding the engine goes quiet
  as well, the tile still shows amber **STALE**, since that is a failed link.
  The exception is an engine wired to a network of its own: if that network
  fails while the engine runs, it reads **OFF**.
- A single gauge tile no longer draws a frame inside its own border. Numeric
  and lamp gauge tiles can be made as short as two rows, and their reading
  grows and stays centred as the tile is made bigger.
- Mate now remembers a place lookup for a day, so asking about the same marina
  or anchorage again answers straight away instead of searching again. A
  search that found nothing is remembered for six hours.

### Fixed

- **Mate's finished answer stays where you were reading it.** When Mate
  finished an answer, the conversation could jump elsewhere in the thread,
  sometimes back to the top. It now stays at the start of that answer.
- **Dictating on an Android phone no longer repeats each phrase.** Speaking
  into the message box or any dictation field used to type the growing
  sentence over and over; each phrase now appears once. The microphone keeps
  listening between phrases until you tap it off.
- **A switched-off radar no longer shows as present after a restart.** For the
  first few minutes after Helmcentral restarted or reconnected to the boat's
  network, a radar that was off could appear as transmitting, and the anchor
  map could fail to load its radar overlay for the rest of the session. A
  radar now appears once it is actually reporting, which can take a short
  while after Helmcentral connects.
- **Fuel economy, time to empty and range show with only one engine running.**
  They used to stay blank because the stopped engine's last reading was still
  being counted as current. An engine that has gone completely quiet for more
  than two minutes, while the other engine and the rest of the network are
  still reporting, now counts as off. If only its fuel rate has stopped, or
  everything on its connection has gone quiet, the figures stay blank. An
  engine wired to its own separate connection is the exception: if that
  connection fails while the engine runs, it counts as off and range reads
  high.

## [0.41.0] - 2026-10-04

### Breaking

- **Settings > Tiles** is now **Settings > Plugins**, at `/settings/plugins`.
  Each kind of plugin (Weather, Wave, Tide, Forecast Warnings, Place names,
  Nearby) has its own section on one page instead of a tab, and each section
  says which parts of Helmcentral use it. A bookmark to `/settings/tiles` now
  opens General settings: update any bookmark to `/settings/plugins`.

### Added

- Mate can now watch live readings for you. Ask it to keep an eye on
  something for a few minutes, such as "watch port and starboard engine load
  for five minutes and tell me if port spikes", and it watches up to six
  readings once a second for up to 30 minutes, then explains what it saw in
  the same conversation: the range, any spikes or dips with their times, and
  any stretch where a reading dropped out. With two readings in the same units
  it compares them second by second. A line above the message box shows what is being watched
  and when it ends, with a **Stop** button. Restarting Helmcentral cancels a
  watch in progress. Nothing needs changing on upgrade.

### Fixed

- Mate looks at engine and other history at one-minute detail when the data only
  covers a short stretch, so it can see brief spikes.

## [0.40.0] - 2026-10-03

### Breaking

- **Alarm notification titles have changed.** Push notifications and emails
  for alarms raised by other instruments now carry a plain title such as
  "Radar Guard Zone 1" or "Collision Risk" instead of the instrument's
  address. If you filter or route alarm notifications on their text, in ntfy,
  your mail client or a webhook, check those filters after upgrading.

### Added

- Mate can now search the web when your boat's documents and instruments
  can't answer a question, such as a harbour's opening hours, a recent notice
  or what a fault code means. Turn on **Web search** under Settings, Mate. It
  is off by default and each search costs a small extra amount on your
  OpenRouter account. Pick which model carries the search with **Search
  model**; a cheap one is fine. The pages Mate used appear in its answer as links that
  open in a new tab. Nothing needs changing on upgrade.
- Mate can now propose changes to your equipment, locations, bins and decks, as
  well as the maintenance schedule. Ask it to file a spare into a bin, rename a
  location or set up a deck from a drawing in your documents, and it answers
  with a card that shows each change as it is now and as it will become, and
  says what a removal takes with it. Several changes that belong together, such
  as a new deck and the locations moved onto it, are one card that applies all
  of them or none. Nothing changes until you tap **Apply**. Mate can't see
  pictures, so a card that sets a deck's plan shows you the picture to check,
  and a PDF can't be used as a plan yet. Nothing needs changing on upgrade:
  cards Mate wrote earlier carry over and work as before.
- Locations can now be drawn on deck plans. Add a picture of each deck, such as
  the builder's general arrangement drawing, outline your locations on it and
  pin your bins, then find them by looking at the boat. Locations has a new
  **Plan** view next to the list, a location's page and a bin's page show where
  it sits, and the new **Decks** list, opened from the menu at the top right of
  Locations, is where you add a plan and draw on it. Nothing needs changing on
  upgrade, and locations without a plan work as before.
- Mate can now recommend a departure time that gives you a fair tidal stream
  on a passage leaving from the boat. It names the best departures and the one
  to avoid, says how much of the run has the stream with you, and warns when
  the stream sets against the wind. It works from the nearest tide station's
  high and low water times, so it knows which spell of flood or ebb you will be
  in, not how fast the stream runs. It takes the direction the flood sets from
  your standing notes, else from its own general knowledge, and says which. Add
  the local flood direction, and any lag before the stream turns, to your
  standing notes to have Mate use them.

### Changed

- Alarm cards raised by other instruments on the network now name the event
  in plain words instead of showing the instrument's address. A radar guard
  zone alarm reads "Radar Guard Zone 1", and while the radar is still
  tracking the target it gives its bearing, range and closest approach. An AIS
  collision alarm reads "Collision Risk" and names the vessel. The
  "Clears when the source clears it" line is gone, and so is the address under
  each card. With two radars running, a guard zone title adds the name you
  gave that radar. Push notifications and emails carry the same title as the
  card. The anchor drag alarm now reads "Anchor 45 m from the drop point".

### Fixed

- The Battery & Power tile's charge rate (%/h) and time to full or empty now
  need the house bank's capacity, either reported by the battery monitor or
  set in **Settings → Vessel**. Without it they show a dash, instead of a
  figure that flickered and mostly read nothing. When the battery monitor
  stops reporting current, they and the Net readout show a dash rather than a
  false 1 A discharge, and a real 1 A discharge is shown as one.

## [0.39.0] - 2026-10-02

### Added

- Helmcentral reloads itself after an update has been installed, so a screen
  never runs an old version. A phone or tablet reloads when you come back to
  it, and a wall display that stays on screen checks by itself once an hour.
  The reload waits while a Settings, document or inventory edit is unsaved, a
  stocktake scan or bin quick-add is in progress, or a Mate message is typed
  but not sent.
- On a phone, the Mate page now has **Chats** and **New** buttons at the top.
  **Chats** opens a list of your recent conversations that you can filter by
  typing, so earlier chats are reachable on a small screen.
- A new Mate conversation opens with a short list of what Mate can check,
  in place of the example question.
- While Mate is thinking and has nothing to look up, its status line shows a
  nautical phrase that changes every few seconds instead of "Thinking…".

### Changed

- The Mate message box now reads "Ask Mate".
- In **Settings → Mate**, the document indexing model is now picked from the
  catalog of models that can read images, the same way as the Mate model,
  instead of typed in. Turning Mate off now hides its other settings.
- The Inventory side menu is in two groups: **Inventory** (Equipment,
  Locations, Stocktake) and **Servicing** (Maintenance, Profiles).

### Fixed

- On a phone, picking a page from the side menu now closes the menu, so you
  land on the page instead of having to dismiss the menu to see it.

## [0.38.0] - 2026-10-02

### Added

- The header's Day and Night button now has a third setting, **Auto**. Press
  the button to step Day, Night, Auto. Auto shows the night theme from sunset to
  sunrise and the day theme otherwise, using the same sunrise and sunset as the
  Clock tile, and changes over by itself while the page is open. If the
  forecast has no sunrise and sunset, Auto keeps whichever theme is showing and
  the button's hover text says it cannot decide.
- Wall displays now check themselves against the screen they run on. Open a
  display's address on its screen once and its page under **Wall displays**
  shows "Measured on this screen", the size that screen's browser really
  reports. If the canvas times the magnification is bigger than that, the page
  says how many pixels are cut off at the right and bottom and offers **Fit to
  screen**, which sets the magnification to the largest value that fits. The
  Wall displays list flags the same problem, and the wall itself shows a small
  note when its page is larger than the screen. Magnification now takes two
  decimals. The display probe page also prints the reported viewport size
  large at the top.
- With Google Places selected, the Nearby tile now shows Google's AI-written
  overview, or its summary of reviews, for places that have no description of
  their own. These are marked as summarized by Google.

### Changed

- The Nearby map now dives in to the first highlighted place and tilts, holds
  there, then flies on to each next place in a high arc that shows the boat
  and the whole area on the way. After the last place it pulls back out to the
  view centred on the boat and rests on it before starting again. A longer
  Summary cycle (15 to 20 seconds) gives each place a longer hold.
- Tile titles now sit in a pill on the tile's top edge, with the state dot at
  the right end of the same edge, instead of in a row of their own. A tile's
  controls, such as the gear shown while editing or the Wind tile's toggles,
  sit on the same edge at the right as matching pills, and turn amber or red
  with the tile's edge. Tiles stacked in a column sit a little further apart
  so each title clears the tile above it.
- In layout mode, a tile's **Remove** and **Duplicate** buttons sit at its
  bottom right corner, clear of the title and the gear on the top edge.
- The Wind tile's gust and Drift readouts show the number alone, without
  "kts". The unit under the wind speed in the middle of the dial is larger.
- The Anchor Watch tile no longer has a **Raise** button. Raise the watch from
  the Anchor Watch page, so a stray touch on a dashboard tile can't end it.
- In full screen, the button that leaves full screen is now at the top right,
  where the full-screen button was. An alarm banner keeps its **View** and
  **Acknowledge** buttons clear of it.

### Fixed

- The Nearby tile keeps the boat at the centre of the map and zooms out far
  enough to show every place in its list. It used to sit the boat off to one
  side, zoomed in twice too far, so places further down the list fell off
  the edge.

## [0.37.0] - 2026-10-01

### Added

- Print QR labels for bins and equipment. **Print label** on a bin's page or an
  item's editor shows a label with a QR code and the bin code or item name
  printed large beneath it. **Print bin labels** in the Locations page menu
  prints every bin on one sheet, three across, to cut out. Any phone camera
  reads the code, and it opens the same page an NFC tag does, so the phone
  needs to reach the boat.
- Import from YachtWave, in **Settings → Import**. Upload YachtWave's Vessel
  Export and go through it one page per topic: vessel particulars, locations,
  equipment and spares, the service log, notes, then each document and photo.
  Nothing is written until you confirm on the last page, and your choices are
  kept if you stop halfway. Where the export can't say which engine a service
  was on, you pick. Duplicates, placeholder names and figures that can't be
  right are flagged, not corrected. YachtWave tasks come in as notes. Notes
  that hold a password or wifi key are left behind. Checklists don't come
  across, because the export doesn't include their steps. Documents and
  photos stay on YachtWave's servers, so for each one you open its link,
  save the file and paste it in, or skip it. Importing a later export brings
  across only what's new.
- Vessel particulars in **Settings → Vessel**: builder, model, year, HIN,
  flag, hailing port, hull, displacement, shore power, system voltage,
  registration, IMO, EPIRB id and date acquired. Name, MMSI, call sign,
  length, beam, draft and air height still come from the boat's instruments.
- Spares keep a part number and how many you mean to carry, and can be at
  zero on board. An item with fewer on board than you carry shows as below
  what you carry.
- Mate can read your maintenance schedule and service log. Ask what's
  overdue, when the generator was last serviced, or whether the main
  engine's schedule is complete. For a review it compares your rules with
  the manufacturer's recommended services on the item's profile and with
  your manuals, and names the gaps. If an item's running hours aren't being
  read, it says the hours are unknown instead of guessing.
- Mate can propose changes to the maintenance schedule. Ask it to set up a
  schedule from a manual, add a rule, change an interval, record when
  something was last done, log a job as done or acknowledge a rule, and it
  answers with a card listing each change in
  one line. Nothing changes until you tap **Apply**, and then every line is
  made together or none is. **Dismiss** drops the card. If a rule was edited
  after Mate wrote the card, Apply refuses and says so, and you ask Mate to
  redo it. A read-only session sees the card without the buttons. Mate can't
  delete rules or log entries, edit past log entries, or add photos, parts or
  meter replacements; those are still done in Inventory → Maintenance.
- Mate keeps a trip or weather window you've been discussing in mind for the
  rest of the conversation. When a later question brings in a fresh forecast,
  such as asking about conditions after moving anchorage, it says whether the
  window has opened, closed or shifted, and tells you when an earlier call no
  longer holds.
- **Settings → Mate**: Choose a model takes a model ID typed in directly, for
  OpenRouter models the catalog doesn't list, such as routers.
- **Settings → Mate**: a new Document indexing section lets you choose the
  model that reads and labels your documents and photos.

### Breaking

- Run `helmcentral convert-profile-rules --apply` once after upgrading,
  before starting Helmcentral (with Docker Compose:
  `docker compose run --rm helmcentral /app/helmcentral convert-profile-rules --apply`). It
  moves your equipment profiles out of the `plugins/engine-profiles` folder
  and turns maintenance rules copied from a profile into live profile jobs,
  keeping their history and any interval you had changed. Run it without
  `--apply` first to see what it will do. Helmcentral refuses to start until
  it has run. If an item points at a profile that no longer exists, the run
  stops and names it: choose the right profile on the item, or add
  `--detach-unresolved` to keep those rules as the item's own. After that
  the `plugins/engine-profiles` folder is no longer read and can be deleted.
- **Use profile schedule** is gone from an item's Maintenance block; the
  profile's jobs are already there.

- Reload the Helmcentral page in any browser tab or phone home-screen app
  that was open before you upgraded. An old tab can't tell Mate today's
  date, so its questions fail with an error naming the missing date until
  the page is reloaded.

### Changed

- The engines, house bank and particulars under **Settings → Vessel** now
  save with the **Save** bar like the rest of Settings, instead of with
  **Save Vessel Settings** and **Save particulars** buttons of their own.
  Leaving Settings with unsaved changes there now warns you, and **Discard**
  puts them back.
- Settings pages now show a save bar across the top of the screen as soon as
  you have unsaved changes, with **Save** and **Discard**. It replaces the
  **Save Settings** button at the foot of each page, and **Discard** puts every
  field, including passwords and notification settings you have typed in, back
  to what was saved. The bar goes away when there is nothing to save. Each
  settings page is now one column of headed sections, with secondary actions
  such as clearing the logs gathered under **Tools** at the bottom.
- Switching a data provider under **Settings > Tiles** now tells you when the
  change could not be saved, instead of leaving the old provider selected
  without a word.
- Inventory → Locations is now a list of your locations with how many bins
  each holds, searchable by name. Press **New location** to add one, or tap a
  location to open its own page, where you rename it, add and remove its bins,
  or delete it. Bin codes and names still edit in place. Back from a bin's
  page returns to its location.
- An item's maintenance schedule now follows its equipment profile live.
  Correct an interval on the profile and every item using it follows at
  once, instead of each item keeping the copy it took. An item's page lists
  its profile's jobs apart from the ones you added for it. A unit that
  differs can change a job's description or intervals for itself only, or
  mark it not applicable; the change is marked and **Reset to profile** puts
  it back. If an item's profile is missing or damaged, its profile jobs are
  not shown and the Maintenance list says which item and why.
- Choosing a different profile for an item first shows which jobs keep their
  history, which leave and which arrive new. A job that leaves keeps its
  history under **No longer in the profile** on the item's page. Saving a
  profile that drops a job some item has history for asks you to confirm and
  names the items, and a profile still in use can't be deleted.
- Equipment profiles are now kept with the rest of your boat's records and
  can be saved on the boat. The profiles that ship with Helmcentral are a
  catalogue: **Add from catalogue** on **Inventory → Profiles** copies one in
  as yours to edit. A profile's service job names must be lowercase letters,
  numbers, dots, underscores and hyphens.
- The service log export has a new last column saying whether each entry's
  job came from the item's profile or was added for that item.
- The Wind and Engine Cluster tiles now fill the tile you give them. Drag
  either one taller or wider and the dial and its readings grow with it, up to
  twice their usual size, centred in the tile. Wind no longer stops growing at
  its old width. On a phone or narrow window they still fit to the screen
  width.
- Nearby Vessels and Radar Targets shrink to fit their list when they are the
  lowest tile in their column. The tile is drawn only as tall as its contacts
  need, never taller than the height you set, and eases to the new height as
  contacts come and go. A tile with another tile below it keeps its height,
  and layout mode always shows the height you set.
- Nearby Vessels with nothing in range now says how far it looked, for example
  "No vessels within 5 km" (or nautical miles, if your display is set to
  imperial), in place of "No nearby targets".
- The alarms drawer now shows when an alarm from the boat's network was
  acknowledged, even if it was acknowledged at the chartplotter or before
  Helmcentral restarted. This needs SignalK 2.31 or later. On SignalK 2.33 an
  acknowledged alarm also comes back as active when it gets worse.
- The Anchor Watch tile on a dashboard page is now just the map. The large
  distance readout is gone (the map's own panel still shows distance), and
  the water-under-the-keel-at-low-tide line, including its too-shallow
  warning, now shows only on the full-page Anchor Watch view. The re-centre
  button is back on the tile's map, under the zoom buttons.
- Documents is rearranged to match the rest of Inventory. The page is titled
  with the folder you are in, with the path back up above it; **Upload** is
  the button at the top right, and **New folder** and **New note** moved
  into the menu beside it (**More actions**). Folders and documents share
  one list, folders first. Tick the boxes (or **Select all**) and a bar shows
  how many are selected with **Move to…**, **Reindex…** and **Delete**. The
  tag filter is now a drop-down, the **Search** field opens the same
  full-page search as before (you can also start typing in it), and files
  being uploaded show as a list above the table. A document's Details page
  keeps its editable fields on the left and the file and indexing facts in a
  column beside them, with Save and Discard in the save bar at the top of the
  screen while you have changes.
- The Documents list on an equipment item now shows each document with a
  thumbnail or file icon, its title, the date it was added and a type label
  such as PDF, JPG or NOTE. Click one, or choose Open from its menu, to read it
  in a panel from the right without leaving the item. The menu also has
  Download, and Remove from item, which takes the link off this item only and
  waits for the save bar.
- The silent sensor source warning no longer goes off when you turn the
  engines off. Engine computers, equipment that switches off with them
  (alternator regulators, DC-DC chargers) and SignalK plugin status are left
  out of it. A device that dies while an engine is running, or long after one
  stopped, is still raised, and so is a whole network gateway failing.
  Ignore this sensor remains the answer for something you switch off by
  hand. An engine computer that drops out while the engine is running is not
  raised by this warning; the engine tiles show dashes.
- Mate no longer shows a broken document icon when it refers to a help page.
  It names the page in words instead.
- The forecast page's chart legends and data-source lines are bigger and
  easier to read at a glance, and now sit together at the bottom of each
  chart instead of split between the top and bottom. The cloud, wind and
  wave keys sit beside their chart's title. Rain and sun, and wave height
  and the largest wave you're likely to meet, now read as one sentence each
  instead of two.
- The Equipment page's list can now be sorted by tapping a column heading,
  such as Name or Status, tapping again to reverse the order. Each row
  carries its own menu for opening or deleting that item, alongside the
  existing tap-to-open. On a phone, the list shows as a stack of cards
  instead of a cramped table.
- The duplicate Help button above the Inventory and Settings sections is
  gone. Use the `?` in the header - it opens the right page for whichever
  section you're looking at.
- The Equipment page now shows status, location and organisation details -
  deployed or stored, verified aboard, zone, bin, location detail, the tag
  address, the profile, aliases, install date and hour-meter path - in a
  side column next to the record, instead of mixed in with the rest of the
  form. A save bar replaces the header while you have unsaved changes,
  with Save and Discard, and gives the header back once you save or
  discard. Short fields such as manufacturer and model, or serial and
  quantity, now sit side by side where there is room.
- Equipment no longer has a Category or "Serviced by" choice. An item either
  has an hour meter or it doesn't: the Maintenance section now opens with an
  optional Hour meter field, shown for every item including a new one. Leave
  it blank for gear with no hour meter. If an item has service rules counted
  in hours and no hour meter, the Maintenance section warns that those rules
  can't count hours until one is set. The Equipment list loses its
  "Serviced by" column and filter, and picking a profile no longer changes
  anything but the blank manufacturer and model.
- Anchor Watch's Adjust mode no longer ties the alarm radius to the map's
  zoom. Pinch or scroll to zoom in and look around while positioning the
  anchor, the same as anywhere else on the chart, without changing the
  radius. A small − / + control under the Adjust icon changes the radius
  instead, and Cancel / Save now sit over the bottom of the map rather than
  in a bar that used to take a chunk of it. Entering Adjust no longer changes
  the zoom, resizes the map or hides the page around it: the data cards on
  the left stay up and show distance and bearing from the new position as
  you pan. The two
  radius shortcuts ("Rode + LOA", "Planner swing") are gone; use the − / +
  control, or the rode planner's own "Apply as alarm radius" button.
- **Settings → Mate**: with OpenRouter Auto on, allowed and excluded models
  show as chips you can remove one at a time, and the Clear buttons are gone.
  The cost tier is now "Cost cap", sits below them, and reads "No cost cap"
  when unset. Add models from **Manage…**.

### Fixed

- The header's `?` now opens the correct help page for the Inventory
  section you're actually on - Locations, Maintenance, Profiles and
  Stocktake, not just Equipment.
- Arrival circle and waypoint perpendicular passed alarms from the plotter's
  route no longer stay lit forever after the route is stopped or cancelled;
  they now clear themselves within a few minutes of the plotter going quiet.

## [0.36.0] - 2026-09-28

### Breaking

- **Documents, inventory, equipment, maintenance, Mate's conversations, the
  nearby-vessel sighting history, alarm history, registered phone alert
  devices and plugin settings now live in one file, `helmcentral.sqlite`.**
  They previously sat in six separate database files. After upgrading,
  Helmcentral refuses to start until you run a one-time migration:
  - Docker Compose: `docker compose run --rm helmcentral /app/helmcentral migrate-db`,
    then `docker compose up -d helmcentral`.
  - Native install: stop the service, run
    `sudo -u helmcentral HELMCENTRAL_STATE_DIR=/var/lib/helmcentral /usr/local/bin/helmcentral migrate-db`,
    then start it again.

  The migration renames your existing database into place and copies your
  Mate conversation history, sighting log, alarm history, registered phone
  alert devices and plugin settings into it; nothing is deleted - the old
  files are kept alongside it with a `.migrated` suffix. If it fails partway
  through, it puts your database back the way it found it, so you can fix
  the problem and run the same command again. If you set
  `DOCUMENTS_DB_PATH`, `ASSISTANT_DB_PATH`, `NEARBY_CONTACTS_DB_PATH`,
  `ALARM_LOG_DB`, `WEBPUSH_DB_PATH` or `PLUGIN_OVERRIDES_DB_PATH`, those are
  gone; set `HELMCENTRAL_DB_PATH` instead if you need a non-default
  location. Back up `helmcentral.sqlite` with Helmcentral stopped, or with
  `sqlite3 helmcentral.sqlite ".backup ..."` while it's running - copying
  the file directly while it runs can miss data still waiting to be written
  through. Alarm history still trims its own old entries. If you used to
  delete `alarm-log.sqlite` to clear it, don't delete `helmcentral.sqlite`
  instead: it now holds your documents, inventory and maintenance records
  too. See
  [Configuration](https://github.com/gterrill/helmcentral/blob/main/docs/reference/configuration.md)
  for details.

### Added

- The dashboard has a full screen button in the header. It hides the sidebar
  and header so the tiles fill the whole screen, for a tablet at the helm or a
  screen at the nav station. Live alarms still show. On a touchscreen, swipe
  left or right on the grid to move between pages. Press Esc, or the small
  exit button in the top corner, to bring the sidebar and header back. Not
  offered on an iPhone, which doesn't support it.
- The Nearby Vessels tile now marks any vessel with a live collision alarm:
  the row picks up a red or amber highlight and a "Collision alarm" or
  "Collision warning" label depending on severity, and that vessel moves to
  the top of the list, worst first, above everything else nearby.

### Changed

- The Wind tile's two MAX GUST cards now show their time window (10M,
  30M, 1HR, 24HR) as a small button with an up/down marker, in the same
  style as the Apparent/True and Course Up/North Up toggles. They were
  always tappable to change the window; now it's visible that they are.

### Fixed

- Solar tile Today, Yesterday and Peak Today now reset at local midnight
  instead of 10 am (for a boat on UTC+10), and each array's own Today and
  Yesterday yield show kWh rather than a wildly inflated raw figure.
- The Link and Image buttons in the note editor now open their popup where
  you can actually reach it. It was rendering behind the document viewer
  panel, so a note could not be given a link or a photo at all.
- Picking the item for a new maintenance rule, the kind or a part when
  logging service work, and a note's type when capturing one now all open
  their list where you can actually reach it. Each was rendering behind the
  dialog or sheet it lives in, so there was no way to make the pick at all.
- The Nearby map's summary cycle now actually cycles. It used to advance only
  through points that have a description, and most points of interest -
  especially from OpenStreetMap - don't have one, so on a typical live feed
  the highlight got stuck on a single point instead of moving through the
  list. It now moves through every point in the ranked list in turn, ring on
  its marker and row highlighted, showing the description only when that
  point has one. Its map markers, name labels and rank badges are also
  bigger, for reading from across the cabin on the wall display.
- A camera feed in an embed tile shows on the flybridge wall display again.
  The display's browser stopped drawing live camera video on a scaled or
  upside-down display; the embed is now drawn so that browser can show it.

## [0.35.0] - 2026-09-27

### Added

- Inventory has a new **Maintenance** section: rules for what's due on
  running hours, on the calendar, or both, grouped by Overdue, Due soon,
  Never recorded, Hours unknown, Interval not set and OK. Completing a rule
  writes a dated service log entry (with hours, cost, parts used and
  photos) and resets it to count down again; a rule can be acknowledged
  with a short reason without hiding it from the list. Certificates and
  expiries with no equipment behind them - flares, the EPIRB battery,
  insurance, registration - live in the same list under their own heading.
  An equipment record with a profile can copy its manufacturer service
  schedule in with one action, and each item's own page shows its rules
  and recent service history. The whole service log, or one item's own,
  exports as a CSV file.
  Running hours come from the engine's own hour reading on the network,
  which holds while the engine is off, or from the figure you read off
  the gauge. Record an hour meter swap once and the totals carry on
  across it.
- Mate now cites documents it found in the library as small icon links
  instead of describing them in a sentence. Tap or click one to open that
  document, and hover or focus it to see its title. A citation to a
  document that's since been removed shows a muted, broken icon instead of
  silently disappearing.
- The Mate sheet (the quick channel opened from any page's header) now has
  its own conversation search: tap the search icon for a filterable list,
  showing your 8 most recent conversations until you type something to
  narrow it.
- Documents has a full-page search overlay: a **Search** button in the
  toolbar (or ⌘K / Ctrl+K from anywhere on the page) opens a large search
  box with **Recent searches** and your most-used tags when it's empty, arrow
  keys to move between results, and Enter to open the highlighted one. The
  **All folders** scope switch moved into the overlay alongside it.
- Selecting several documents in the library now offers **Reindex…**
  alongside Move to… and Delete, with one confirmation showing the total
  page count and, if any selected document is a scan or a photo, the
  estimated OCR cost - the same information the single-document Reindex
  already shows, summed across the batch.
- The note editor's image button can now take or add a photo directly:
  **Take photo** shoots one on the spot and drops it into the note at the
  cursor, and **Add from library** does the same for photos already on the
  device, filing several in the order you pick them. Pasting a document id
  from Documents still works exactly as before.
- Alarms, Anchor Watch, Mate and Radar now have a settings button in the
  header (next to Help) that opens straight to that page's own section of
  Settings. It is left off on a phone, where the header has no room for
  it.

### Changed

- Small text on tiles, drawers and settings is easier to read at the helm.
  Secondary readouts such as voltage, charge rate, time to dawn and unit
  suffixes were a size too small to read at arm's length in glare, and are
  now the same size as the rest of the small print. Uppercase labels and
  status lines that sat at that size moved up with them.

- Opening Mate, from the sidebar panel or the header's quick sheet, now
  always starts a fresh, empty conversation rather than resuming whichever
  one was most recently updated - even if you were just looking at one a
  moment ago. Pick an earlier conversation from the list or search when you
  want to return to it. A conversation is only saved once you send its
  first message, so starting fresh and changing your mind leaves nothing
  behind. A reply already being written keeps arriving if you close and
  reopen the sheet mid-answer, rather than being cut off.
- A note's row in the document library now lines up exactly with a plain
  file's row - the type icon and title no longer sit slightly further
  right than a file's.
- The Settings menu is now grouped into Boat & app, Connections, Features and
  System, and both the Settings and Inventory menus read in plain sentence
  case instead of all capitals. The header now shows the page and, where it
  has one, the section you're on (for example Settings > Logs) instead of
  always starting with Dashboard.

### Fixed

- Settings > Logs now lists the newest line at the top.

## [0.34.0] - 2026-09-26

### Added

- Anchor Watch now warns when the boat will be too shallow at the next low
  tide: it projects the live depth reading forward to the next low and
  compares what would be left under the keel against your vessel's draft
  and a clearance margin you set in Settings → Anchor Watch (**Clearance at
  Low Water**, 0.5 m by default). The tile and the full-page anchor watch
  view show the shortfall and the time of the next low when it's too
  shallow, or the expected clearance when it isn't.
- The full-page Anchor Watch view now has an **Adjust** control (the Move
  icon on the map, or a text button in the header when the display has no
  map) for moving the anchor's saved position or changing the alarm radius.
  Pan the chart to move the anchor, pinch or scroll to size the radius, or
  tap a shortcut to match your deployed rode or the rode planner's
  recommended swing. Nothing changes on the boat's instrument network until
  you confirm, a warning appears first if the change would leave the boat
  outside the alarm circle right now, and a short undo window follows every
  save.

### Changed

- The full-page Anchor Watch view now opens with live depth and tide
  context instead of distance from the anchor: a header above the map shows
  depth, the tide's direction, the next high or low with its height and
  time, and the low-water clearance warning together. Distance, bearing,
  the alarm radius, current and recommended scope now read from the map's
  own readout panel instead. The interim −/+ alarm-radius buttons from the
  last release are gone; the new **Adjust** control above replaces them
  with a proper radius stepper plus the ability to move the anchor, and the
  rode planner's **Apply as alarm radius** still works alongside it.

### Fixed

- The Depth & Tide tile no longer skips a low tide of exactly 0.0 in favour
  of the one after it, and no longer shows a made-up high or low (at "now"
  or a day ahead) when the tide station has none left in its forecast; that
  reading now stays blank instead. Tides that fall below chart datum now
  show their real height.
- Some builder drawings and equipment manuals that previously failed to
  index (their text couldn't be read) now index normally. Documents already
  showing as failed for this reason don't fix themselves: open them in
  Documents and choose Reindex.
- A failed document in Documents now tells you what to try next instead of
  showing a raw technical error. A HEIC photo says to convert it to JPEG,
  and Mate being off or unconfigured says so directly, so you know which
  setting to fix. The original detail is still there if you want it, under
  Error details on the document's own page.
- After a long search runs out of research steps, Mate now reliably answers
  from what it already found instead of giving up. This previously could
  still fail outright with no answer at all, depending on which model
  answered the question.

## [0.33.0] - 2026-09-26

### Breaking

- **House bank capacity moved.** The Overnight electrical estimate no
  longer falls back to a built-in default capacity; it now comes only from
  the house bank you pick under Settings → Vessel → Power. Re-enter your
  bank's capacity there after upgrading. Until you do, the overnight
  estimate reports capacity as not set rather than assuming a number.
- **Drop the anchor again after upgrading.** On a Docker install, updating
  used to clear a running anchor watch and its marks without warning. The
  watch now lives with the rest of your saved settings, so from here on it
  survives updates and restarts. This upgrade clears a running watch one
  last time, on any install: drop the anchor again once you're on the new
  version. If the saved watch is ever damaged and can't be read back, the
  Anchor Watch tile and page say so plainly instead of guessing at a
  position. The rest of the boat's alarms keep running, and dropping the
  anchor again starts a fresh watch.

### Added

- Anomaly detection: three new alarm checks. A frozen, impossible or newly
  quiet sensor reading; a house bank still being charged by its alternators
  or chargers after it has already finished; and, for two or more engines
  running matched, one pulling away from the others on coolant temperature,
  oil pressure, boost pressure, load or transmission readings. Set your
  engines and house bank up under Settings → Vessel; a dead sensor can be
  excluded from its own alarm card. See
  [Anomaly detection](https://github.com/gterrill/helmcentral/blob/main/docs/features/anomaly-detection.md)
  and
  [Set up your vessel](https://github.com/gterrill/helmcentral/blob/main/docs/how-to/set-up-your-vessel.md).

### Changed

- The anchor alarm radius is now set with − / + buttons on the Anchor Watch
  page, next to the radius reading, or by choosing Apply as alarm radius in
  the rode planner. A change that fails says why, and offers Retry when
  trying again could work. Dragging the swing circle's edge and dragging the
  anchor marker are gone until a later release brings back moving the
  anchor.

### Fixed

- When a question needs many lookups (for example, tracing when several
  instrument feeds went quiet), Mate now answers from what it found instead
  of stopping with "did not produce an answer," and says plainly what it
  wasn't able to check.
- Tapping or panning the anchor watch map, on the Anchor Watch page or on a
  dashboard tile, no longer changes the alarm radius or moves the anchor.
- The anchor watch map now opens with the whole swing circle in view,
  instead of it being hidden under the anchor marker at some radii, and
  opens on the boat when no anchor is set, instead of the last anchorage.
- A rode planner change that fails to save now goes back to the saved value
  and says so, instead of showing a figure the boat never stored.

## [0.32.0] - 2026-09-25

### Added

- Mate can now answer questions about nearby vessels: who is around, how
  close, and how long they have been in range - and when you last crossed
  paths with a boat that has since moved on.
- Mate can now diagnose missing or stale vessel telemetry itself: ask "is the
  depth reading working" or "when did the tank levels stop updating" and it
  checks the live instrument connection and, with a history log configured
  for the boat, finds the last recorded time for a reading and names the
  specific feed that went quiet, instead of pointing you at the SignalK
  admin console.
- The Wind tile (previously "Apparent Wind") adds two toggles: switch the
  reading between apparent and true wind, and switch the compass between
  Course Up, which turns with your heading, and North Up, which holds true
  north at the top. True wind needs boat speed and heading feeding the wind
  instrument; where that isn't fitted, True mode reads a dash rather than
  showing the apparent figures in its place.

### Changed

- Current Conditions now shows true wind speed with an arrow for its
  direction, and its observed-gust marker reads the last hour's highest
  true wind.

### Fixed

- In Course Up, the Wind tile's set arrow now points relative to the bow,
  matching the compass beside it; before, it always pointed as if North Up.
- The rode planner's Depth field now shows the figure the plan is actually
  computed against: depth at this spot at the next high tide, plus bow
  roller height. Before, it showed the bare sounder reading labelled "Tide
  adjusted", though the recommended rode already allowed for the tide.
  Typing over it still overrides the plan, and a figure with no water left
  once tide and bow height are backed out is refused rather than saved.

## [0.31.0] - 2026-09-25

### Added

- Every storage bin has its own URL. Write it to an NFC tag from the bin page,
  and tapping the tag with a phone opens that bin.
- Equipment can carry photos, taken or picked on a phone and shown on the item
  and on its bin.
- Quick add, for putting an item into the bin you are standing at without
  opening the full editor.
- Stocktake: scan bins in turn and confirm, move or flag what is in each.

### Changed

- Removing a photo from an item, or deleting an item, only takes the picture
  off that item. The picture stays in Documents. When you delete an item, you
  can also delete photos no other item uses.
- Pictures already linked to an item as documents now also appear in its
  photo row.
- Adding a photo that is already in Documents links that picture instead of
  being refused.

### Fixed

- A photo that fails to upload stays queued against its own item until you
  retry it.
- Retrying a photo that could not be prepared no longer sends the full-size
  original.
- Leaving the bin page, quick add, stocktake or an item with photos waiting to
  retry asks first.
- A photo or name entered while quick add is still saving is no longer lost.
- "Full item" from a bin no longer asks about changes you never made.
- Write tag can be cancelled.
- Saving an item's documents can no longer unlink photos added earlier in the
  same visit.

## [0.30.0] - 2026-09-23

### Added

- Forecast Conditions tile for a wall display.
- The wall clock shows the ETA at the next waypoint when a route is active.
- A next-hour rain nowcast on the wall display.

## [0.29.0] - 2026-09-23

### Added

- Inventory: an equipment registry organised by zone and bin. Equipment
  profiles now live under the Inventory panel.
- Nearby tile shows the active route, cycles through place summaries, and
  frames the map to fit the points of interest.
- Select text in a note or manual section and ask Mate about it.
- One dictation control, shared by the Mate composer and the note editor.
- A new note opens straight into the editor.

### Changed

- Turning Mate on is the consent to index your documents. Mate indexes them in
  the background and no longer asks item by item.
- Notes are created from Documents only.

### Fixed

- An alarm on a SignalK value that goes missing reads as absent, not as -1.
- The tide chart tooltip lines up with the cursor.
- Long notes scroll in the document viewer.

## [0.28.0] - 2026-09-21

### Added

- Notes: capture, file and read notes in Documents, with photos, links between
  notes, and a rich-text editor.
- Checklists run as a checklist, from either place a note is read.
- A folder of documents is a manual.
- Save one of Mate's answers as a note. Mate sees pinned notes and knows which
  manuals exist.
- Acknowledge an alarm from the banner. Forecast warnings link to the bulletin.
- The alarm banner is coloured by the alarm's severity.
- Wall displays are managed on their own page.
- Uploaded satellite charts can be removed.
- Edit a document's details on its own page.

### Changed

- The sidebar is ordered the way the boat is run, and the manual is called
  Help.

## [0.27.0] - 2026-09-19

### Breaking

- **Wall displays have a new address.** A wall display is now set up as its
  own screen, with a name, size, magnification and orientation, and is opened
  at `/display/<name>`. The old `/kiosk` address no longer exists. After
  upgrading, create the display under Wall displays, assign its pages to it,
  and point the wall browser at the new address. See
  [Set up a wall display](https://github.com/gterrill/helmcentral/blob/main/docs/how-to/set-up-a-wall-display.md).
- **SignalK and InfluxDB credentials are no longer read from environment
  variables.** `SIGNALK_USERNAME`, `SIGNALK_PASSWORD` and `INFLUXDB_TOKEN` are
  ignored. If you set them in the environment, enter them in the SignalK and
  InfluxDB sections of Settings before upgrading, or Helmcentral will connect
  to SignalK without logging in and its alarm writes will be refused.

### Added

- More than one wall display, each with its own pages. A television display
  gets slow pixel shift against burn-in, remote-control keys to step and pause
  the rotation, and an optional screen wake lock.

### Changed

- Wall display pages are listed under their screen in the sidebar, not among
  the navigation pages.

### Security

- Plugins can no longer read the key that protects stored credentials.
- Clearing or repointing a notification destination clears the credential
  bound to it.
- Webhooks cannot be aimed at addresses on the Helmcentral host itself, and
  dashboard embeds cannot point back at Helmcentral.
- A document's contents cannot instruct Mate as if they came from you.
- A malformed PDF, an oversized satellite chart upload or a bad SignalK update
  can no longer stall or crash the helm display.

## [0.26.0] - 2026-09-18

### Added

- Document search matches by meaning as well as by keyword, when Mate is set
  up with an OpenRouter key. Search says when it ran keyword-only and offers to
  index the rest.
- A new dashboard page opens already named, with its controls at the top.

### Changed

- Everything on a dashboard page is called a tile.

[Unreleased]: https://github.com/gterrill/helmcentral/compare/v0.56.0...HEAD
[0.56.0]: https://github.com/gterrill/helmcentral/compare/v0.55.0...v0.56.0
[0.55.0]: https://github.com/gterrill/helmcentral/compare/v0.54.0...v0.55.0
[0.54.0]: https://github.com/gterrill/helmcentral/compare/v0.53.0...v0.54.0
[0.53.0]: https://github.com/gterrill/helmcentral/compare/v0.52.0...v0.53.0
[0.52.0]: https://github.com/gterrill/helmcentral/compare/v0.51.0...v0.52.0
[0.51.0]: https://github.com/gterrill/helmcentral/compare/v0.50.0...v0.51.0
[0.50.0]: https://github.com/gterrill/helmcentral/compare/v0.49.0...v0.50.0
[0.49.0]: https://github.com/gterrill/helmcentral/compare/v0.48.0...v0.49.0
[0.48.0]: https://github.com/gterrill/helmcentral/compare/v0.47.0...v0.48.0
[0.47.0]: https://github.com/gterrill/helmcentral/compare/v0.46.0...v0.47.0
[0.46.0]: https://github.com/gterrill/helmcentral/compare/v0.45.0...v0.46.0
[0.45.0]: https://github.com/gterrill/helmcentral/compare/v0.44.0...v0.45.0
[0.44.0]: https://github.com/gterrill/helmcentral/compare/v0.43.0...v0.44.0
[0.43.0]: https://github.com/gterrill/helmcentral/compare/v0.42.0...v0.43.0
[0.42.0]: https://github.com/gterrill/helmcentral/compare/v0.41.0...v0.42.0
[0.41.0]: https://github.com/gterrill/helmcentral/compare/v0.40.0...v0.41.0
[0.40.0]: https://github.com/gterrill/helmcentral/compare/v0.39.0...v0.40.0
[0.39.0]: https://github.com/gterrill/helmcentral/compare/v0.38.0...v0.39.0
[0.38.0]: https://github.com/gterrill/helmcentral/compare/v0.37.0...v0.38.0
[0.37.0]: https://github.com/gterrill/helmcentral/compare/v0.36.0...v0.37.0
[0.36.0]: https://github.com/gterrill/helmcentral/compare/v0.35.0...v0.36.0
[0.35.0]: https://github.com/gterrill/helmcentral/compare/v0.34.0...v0.35.0
[0.34.0]: https://github.com/gterrill/helmcentral/compare/v0.33.0...v0.34.0
[0.33.0]: https://github.com/gterrill/helmcentral/compare/v0.32.0...v0.33.0
[0.32.0]: https://github.com/gterrill/helmcentral/compare/v0.31.0...v0.32.0
[0.31.0]: https://github.com/gterrill/helmcentral/compare/v0.30.0...v0.31.0
[0.30.0]: https://github.com/gterrill/helmcentral/compare/v0.29.0...v0.30.0
[0.29.0]: https://github.com/gterrill/helmcentral/compare/v0.28.0...v0.29.0
[0.28.0]: https://github.com/gterrill/helmcentral/compare/v0.27.0...v0.28.0
[0.27.0]: https://github.com/gterrill/helmcentral/compare/v0.26.0...v0.27.0
[0.26.0]: https://github.com/gterrill/helmcentral/releases/tag/v0.26.0
