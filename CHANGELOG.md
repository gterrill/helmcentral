# Changelog

What changed in each Helmcentral release, written for the person upgrading the
boat. Read the **Breaking** section of every release between the one you run and
the one you are installing: it says what you have to change before or after the
upgrade.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
releases use [Semantic Versioning](https://semver.org/). While the version is
0.x, a minor release can break things.

## [Unreleased]

### Added

- Anchor Watch now warns when the boat will be too shallow at the next low
  tide: it projects the live depth reading forward to the next low and
  compares what would be left under the keel against your vessel's draft
  and a clearance margin you set in Settings → Anchor Watch (**Clearance at
  Low Water**, 0.5 m by default). The tile and the full-page anchor watch
  view show the shortfall and the time of the next low when it's too
  shallow, or the expected clearance when it isn't.

### Fixed

- The Depth & Tide tile no longer skips a low tide of exactly 0.0 in favour
  of the one after it, and no longer shows a made-up low a day ahead when
  the tide station has no low left in its forecast.

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

[Unreleased]: https://github.com/gterrill/helmcentral/compare/v0.33.0...HEAD
[0.33.0]: https://github.com/gterrill/helmcentral/compare/v0.32.0...v0.33.0
[0.32.0]: https://github.com/gterrill/helmcentral/compare/v0.31.0...v0.32.0
[0.31.0]: https://github.com/gterrill/helmcentral/compare/v0.30.0...v0.31.0
[0.30.0]: https://github.com/gterrill/helmcentral/compare/v0.29.0...v0.30.0
[0.29.0]: https://github.com/gterrill/helmcentral/compare/v0.28.0...v0.29.0
[0.28.0]: https://github.com/gterrill/helmcentral/compare/v0.27.0...v0.28.0
[0.27.0]: https://github.com/gterrill/helmcentral/compare/v0.26.0...v0.27.0
[0.26.0]: https://github.com/gterrill/helmcentral/releases/tag/v0.26.0
