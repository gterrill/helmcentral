# Boat time

Every time Helmcentral shows you or tells Mate is on the boat's own clock, not
the clock of the phone, tablet or wall screen you're looking at. This page says
how that clock is worked out, where you see it, and where it stops.

## How the boat's clock is chosen

Helmcentral looks up where the boat is and uses the time zone that place keeps.
That follows daylight saving, so a boat in New South Wales moves to UTC+11 in
summer. It also follows half-hour zones, so a boat in Adelaide keeps UTC+9:30.

- **Near land:** The boat uses the local zone, for example Australia/Brisbane.
  Within about 12 nautical miles of the coast the local zone still applies.
- **Offshore:** Beyond that the boat uses the ship's zone for its longitude,
  for example UTC+11 in the Coral Sea. These zones have no daylight saving and
  no abbreviation, so a time shows as "UTC+11".
- **A place you ask about:** A planned stay, a tide station or a passage
  waypoint keeps its own zone, not the boat's. A forecast for a marina is in
  that marina's time, and a note says so when it differs from the boat's.

## Where you see it

Every screen reads the zone from the boat, so a phone ashore and a wall display
left on UTC both show the boat's time. That covers the clock tile, tide times,
sunrise and sunset, alarm times, passage dates and the times on the Forecast
page. A passage or a planned stay shows its own place's time, as described
above.

- **Times you recorded yourself:** When a document was uploaded, indexed or
  added, and when you ticked a checklist step, show on the device you're using.
  Those times record when you did something, not where the boat was.
- **Before the boat has reported a zone:** For a moment after you open
  Helmcentral, times that need the zone show as "--". They fill in as soon as
  the boat's first update arrives.

## When the boat has no position

- **The fix drops out:** The boat keeps the zone of its last good position.
  That zone is remembered when Helmcentral restarts too. Mate's clock line says
  "last fix" while this applies, and the clock tile shows "last fix" beside the
  time.
- **There has never been a fix:** Times are UTC, and Mate says it's UTC because
  there's no position yet. The clock tile shows "UTC · no position yet". Watches
  don't run until the first fix, because a schedule can't be read without a
  clock.

A change of zone while underway shows up as soon as the boat crosses the line.
A passage keeps the zone of the place it began.

## Where the zone data comes from

Helmcentral finds the zone from boundary data built from OpenStreetMap by the
timezone-boundary-builder project. The data is copyright OpenStreetMap
contributors and is available under the Open Database License (ODbL). The
boundaries come to Helmcentral through the tzf library, which is released under
the MIT licence. The full notices are in `THIRD_PARTY_NOTICES.md`, in the
Helmcentral source.
