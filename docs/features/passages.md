# Passages

The Passages page is the boat's logbook. It shows every leg you have run,
where you went, how long it took, what the engines, battery and wind did on
the way, and what the nights at anchor between legs cost in power and water.
Use it to remember last season, to write up a trip, or to see how a change to
the boat showed up in the figures.

## What gets recorded

Helmcentral keeps its own log of where the boat has been. It notes a position
whenever the boat has moved about 25 m, and every ten minutes while she sits
still, along with whatever it can read at that moment: speed and course, wind
and gusts, house battery charge, fuel rate, whether the generator is running
and how much fresh water is in the tanks. This runs on its own whether or not
anyone has a screen open, and it starts when Helmcentral does. A position with
no GPS fix, or one that jumps further than the boat could have travelled, is
left out.

A reading that was too old when the position was noted is left out of that
point. It is never copied from an earlier one, so the page shows `--` rather
than a figure that might be wrong.

The log begins on the day you install the release that adds it. Earlier trips
can be added from a chartplotter's GPX files, see
[Import GPX tracks](../how-to/import-gpx-tracks.md).

## How a passage is found

A passage starts when the boat holds 3 knots for two minutes, and it is dated
from the last moment she was at rest before that. It ends when she has been
below 1 knot for an hour, or when you set the anchor, whichever comes first,
and the arrival is the first moment she stopped. A stop of less than an hour,
a lock or a fuel dock, stays inside the passage. Anything shorter than 15
minutes or half a nautical mile is not kept, so shifting around a marina does
not clutter the list.

The names for the two ends come from the place names source you have selected
in Settings. If there is none, or the position is not near anything named, the
end shows `--` and you can type a name yourself.

## The list

The left side lists **All passages**, each cruise, and **Unfiled**, which is
every passage not yet in a cruise. The list shows the newest first, with the
date, where the passage went from and to, the distance, the time underway and
its cruise. Dates and times on this page are the boat's own local time, taken
from where the passage began, and a time that appears with a zone label such
as UTC+10 is in that zone.

Tick several passages to **View together** on one map, to **Save as cruise**,
or to **Move to cruise**.

## One passage

Opening a passage shows its track on the chart, with the same basemap and
OpenSeaMap marks as Anchor Watch. A gap in the log is never bridged with a
line: if the log has a hole, the track stops and starts again after it.

The map opens with the whole track framed. Pressing **Play** from the start
brings the map down from high above onto the track, then runs the replay with a
boat marker. The slider scrubs to any point, and Play from the middle just
carries on with no zoom. The readout shows the local date and time at that
point. Slow, Normal and Fast set how long the whole replay takes: 30, 15 or 8
seconds. **Overview map** puts a small map of the surrounding coast in the
corner, with a box where the track is. It stays on or off the next time you
open a passage.

Under the map are the passage's figures:

- distance and time underway
- mean and maximum speed
- fuel burned
- mean wind and the strongest gust
- house battery at the start and end, and its lowest point

A figure Helmcentral could not read shows `--`. It is never estimated.

You can give the passage a title, change its two ends and keep notes on it.
Splitting, trimming, merging, hiding and moving are in
[Edit a passage](../how-to/edit-passages.md).

## Cruises

A cruise is a named group of passages, such as "Whitsundays with Steve and
Kim". A passage is in at most one cruise, and cruises do not nest. A cruise's
page replays all its passages one after another, leaving out the days between
them, and adds the figures that only make sense across a whole trip:

- first departure to last arrival, time underway and the number of nights
- total distance and fuel
- wind while underway and wind while at anchor, each as a mean and the
  strongest gust
- generator hours across the whole span, anchorages included
- the lowest the house battery went, and its charge at dawn on each night
- fresh water used, and fresh water taken on

Water taken on is each rise in the tank levels of more than 1 percent. Each
rise is listed with when it happened and how much it added, and you say whether
it was the **Watermaker** or a **Dock fill**. Helmcentral suggests Dock fill
only when the rise happened beside a marina, and suggests nothing otherwise,
because open water does not prove a watermaker was running. The suggestion
stays a suggestion until you press a button to confirm it.

Deleting a cruise deletes only the grouping. Its passages stay and become
unfiled.

## Sharing

A passage, a cruise or a selection of passages can go on your blog as an
animated map your readers replay, or out as a still picture for a post's
header. The **Export** button on each page makes both. See
[Share a passage on your blog](../how-to/share-a-passage-on-your-blog.md).

## Limits

- Tracks imported from GPX files carry no readings, so their battery, wind,
  fuel and water figures show `--`. Speed is worked out from the positions.
- Fuel, battery and water figures need the matching sensors on the boat's
  network, and the house bank chosen under Settings → SignalK → Roles for the battery.
- Distances are in nautical miles and speeds in knots. Volumes follow the
  Units choice in Settings, General: litres for Metric, US gallons for
  Imperial.
- A hidden passage does not come back by itself. Its recorded track is kept.
