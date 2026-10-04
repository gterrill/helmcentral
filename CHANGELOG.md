# Changelog

What changed in each Helmcentral release, written for the person upgrading the
boat. Read the **Breaking** section of every release between the one you run and
the one you are installing: it says what you have to change before or after the
upgrade.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
releases use [Semantic Versioning](https://semver.org/). While the version is
0.x, a minor release can break things.

## [Unreleased]

### Fixed

- **A switched-off radar no longer shows as present after a restart.** For the
  first few minutes after Helmcentral restarted or reconnected to the boat's
  network, a radar that was off could appear as transmitting, and the anchor
  map could fail to load its radar overlay for the rest of the session. A
  radar now appears once it is actually reporting, which can take a short
  while after Helmcentral connects.

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

- **Settings > Tiles** is now **Settings > Plugins**, at `/settings/plugins`.
  Each kind of plugin (Weather, Wave, Tide, Forecast Warnings, Place names,
  Nearby) has its own section on one page instead of a tab, and each section
  says which parts of Helmcentral use it. A bookmark to `/settings/tiles` now
  opens General settings.
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

[Unreleased]: https://github.com/gterrill/helmcentral/compare/v0.40.0...HEAD
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
