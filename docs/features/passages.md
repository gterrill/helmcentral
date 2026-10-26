# Passages

The Passages page is the boat's logbook. It shows every leg you've run, where
you went and how long it took. It also shows what the engines, battery and wind
did on the way, and what the nights at anchor between legs cost in power and
water. Use it to remember last season, to write up a trip, or to see how a
change to the boat showed up in the figures.

## What gets recorded

Helmcentral keeps its own log of where the boat has been. It notes a position
whenever the boat has moved about 25 m, and every ten minutes while she sits
still. Along with each position it notes whatever it can read at that moment:
speed and course, wind and gusts, house battery charge, fuel rate, whether the
generator is running and how much fresh water is in the tanks.

This runs on its own whether or not anyone has a screen open, and it starts when
Helmcentral does. A position with no GPS fix is left out. So is one that jumps
further than the boat could have travelled.

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
minutes or half a nautical mile isn't kept, so shifting around a marina does
not clutter the list.

The names for the two ends come from the place names source you have selected
in Settings. If there is none, or the position isn't near anything named, the
end shows `--` and you can type a name yourself.

## The list

The left side lists **All passages**, each cruise, and **Unfiled**, which is
every passage not yet in a cruise. The list shows the newest first, with the
date, where the passage went from and to, the distance, the time underway and
its cruise. Dates and times on this page are the boat's own local time, taken
from where the passage began. A time that appears with a zone label such as
UTC+10 is in that zone.

Tick several passages to **View together** on one map, to **Merge** them, to
**Save as cruise**, or to **Move to cruise**.

## One passage

Under the route and date, a passage you sailed from a Mate weather window shows
a **Planned with Mate** line, such as "Planned with Mate · Fri 23 Oct · GO". It
tells you which departure day you went on and what the forecast said for that
day. Next to it is what Helmcentral recorded about the plan, and it says
"(inferred)" when Helmcentral worked that out itself. If the chat that made the
plan still exists, an **Open chat** link takes you back to it. A passage that
was never planned with Mate shows no line, and if the plans can't be read, the
page says so instead of leaving the line out.

In the edit panel, **How it went** lets you rate the passage as **Comfortable**,
**Lively** or **Shouldn't have gone**. The rating is yours alone. Helmcentral
never fills it in, and Mate doesn't read it yet. When you merge passages, the
rating that stays is the more cautious one.

Opening a passage shows its track on the chart, with the same basemap and
OpenSeaMap marks as Anchor Watch. A gap in the log is never bridged with a
line: if the log has a hole, the track stops and starts again after it.

The satellite button in the top right corner of the map switches between the
chart and aerial photography. It is on the first time you open a passage, and
the map remembers your choice from then on, separately from the Anchor Watch
map.

The photographs begin to fade in when the map is about a hundred nautical miles
across and are fully in at about half that. So a regional view stays the chart,
and a bay or approach shows the shore. The track gets a white edge while the
photographs show, so that it stays clear against them. Place names stay on top.
The photographs come from the boat's internet connection, so the map shows the
chart alone when the boat is offline.

The 3D button beside the satellite button tilts the map and raises the land:
hills, headlands and mountains stand up from the chart, exaggerated half again
so a coastline is easy to read. It is on by default and remembers your choice
on its own.

- **3D on:** You can drag to turn the map and tilt it up to 70 degrees. The sea
  stays flat, so the chart and the photographs lie level on the water.
- **3D off:** The map goes back to the flat, north-up view.
- **No elevation data:** The elevation data comes over the boat's internet
  connection and is kept once fetched. If it can't be fetched, the map shows
  "Terrain unavailable" in the top left corner and carries on flat, level and
  north up. It tries the elevation data again every minute, and the notice goes
  once it arrives.

With 3D on, the boat marker becomes a model of your own hull: a sailing
monohull, sailing catamaran, power monohull or power catamaran, set under
**Settings → Anchor Watch → Hull type**. It sits level on the water. Without a
hull type set, or with 3D off or the terrain unavailable, the marker is the
plain dot.

- **Size:** It is sized from the length overall and beam in your vessel details
  (or the Length Overall under Anchor Watch settings), and drawn true to size
  when you zoom in close. It is kept at least about 40 pixels long when you zoom
  out, so you can still find it.
- **Missing figures:** With a length but no beam, the beam is estimated from a
  typical proportion for the hull type. With no length recorded, the boat is
  drawn at that fixed 40 pixels at every zoom, and isn't to scale.
- **Heading:** The bow points along the course the boat is sailing. When she is
  stopped it points the way she is about to go, or keeps its last heading if she
  goes nowhere more.

During a replay the track is drawn as the boat sails it: solid up to the boat,
and a faint line for the water still ahead. Before Play, and when you view a
passage without replaying, the whole track shows as before.

The map opens with the whole track framed. Pressing **Play** from the start
brings the map down from high above onto the track, then runs the replay with a
boat marker. The slider scrubs to any point, and Play from the middle just
carries on with no zoom. The readout shows the local date and time at that
point. The length list sets how long the whole replay takes, 60, 30, 15 or 8
seconds, however long the passage was. The **Overview map** switch puts a small
map of the surrounding coast in the corner, with a box where the track is. It
stays on or off the next time you open a passage.

With the overview map on, the slider icon beside its switch lets you put it in
any corner of the map (top left, top right, bottom left or bottom right). You
can also make it wider or closer than the automatic fit, up to three steps
either way.

- **Saved with the passage:** The corner and the step are saved with the
  passage, so it looks the same the next time you open it and in everything you
  share from it.
- **Blog embed and stills:** The blog embed and the two still images draw the
  overview in the same corner at the same zoom. In every corner it keeps clear
  of the map buttons, the readout and the map credit.
- **Default:** A passage you haven't set shows the overview top right at the
  automatic zoom.
- **Unreadable choice:** If the saved choice can't be read, the overview stays
  off and a Retry button appears beside it rather than showing a guess.

### Camera shots

By default the camera frames the whole track. Camera shots let you direct it,
for a replay you want to watch or film. You can open on the whole passage,
chase the boat through a narrow pass, circle her at the anchorage, or slow down
for the part worth seeing. The **Camera** button on the replay bar opens the
shot editor.

A shot starts at a moment of the passage and lasts until the next shot. It has
one of three modes:

- **Overview** frames the whole track.
- **Follow** keeps the boat in the middle and turns with her. The angle sets the
  view: 0 is from astern, 90 is from abeam, negative angles look from the other
  side.
- **Orbit** circles the moving boat at a set number of degrees every second you
  watch, either way round. A faster replay doesn't spin it faster.

Follow and Orbit have their own zoom and pitch. Each shot also has a **Pace**:
full, half or quarter speed. At half pace the passage runs half as fast for as
long as the shot lasts, so the replay takes longer to watch. The slider stays
in passage time.

Shots show as marks on the slider. Pick one to jump to it and edit it. The
picked mark is larger and ringed, and a line above its settings names it, for
example "Shot 2 of 3 · from 09:41 to 10:20". Hover a mark to see its number
and time, and on a replay that spans more than one day the date too. The
camera eases into each shot over about a second and a half. Scrubbing puts the
camera straight into the shot you land in.

If you drag, zoom or tilt the map yourself during playback (a simple tap doesn't
count), the replay leaves the camera with you. It stays with you until the next
shot starts or you press Play again. The next shot then eases in from wherever
you left the view. With 3D on, the tilted map has a sky that fades into the
horizon.

Shots belong to the time of day, not to one passage record, so splitting or
merging passages keeps them. If you trim a passage so a shot falls outside it,
the shot is hidden and comes back if you undo the trim. With 3D off or the
terrain unavailable, shots only move and zoom the flat map. If your device asks
for reduced motion, the camera cuts to each shot instead of moving, and cuts
again if the boat nears the edge of the view. With no shots the replay frames
the track as before. See [Film a passage replay](../how-to/film-a-passage-replay.md).

### Signs

A sign is a short note with an optional photo, pinned to a moment of the
passage, for the whale off the bow or the anchorage with the good beach. It
appears on the map as a card on a thin line from its place on the track, with
the photo across the top and the title and note beneath it. A card near the
edge of the map moves over to stay in view, or sits below its place when there
is no room above. It follows the camera as it moves. The **Signs** button on the
replay bar opens the sign editor.

Each sign chooses when it shows:

- **While passing:** This is the default. It brings the sign up just before the
  boat reaches the spot and keeps it for some seconds you set, from 1 to 30.
- **After passing:** It shows the sign from the spot to the end.
- **Always:** It shows the sign for the whole replay.

The seconds are seconds you watch, so a faster replay doesn't make a sign flash
by. Only two cards show at once. When a third is due, the one that has been up
longest steps aside. A sign set to **Always** doesn't step aside, so two of them
leave no room for others.

Before you press Play, each sign is a small mark on its place on the track, and
a tap opens its card. While the replay is paused, a tap on a card opens its
photo full size, and Escape closes it. Signs show as diamonds on the slider, apart
from the marks for camera shots. Picking one jumps to it and opens it for
editing.

A photo is either uploaded, into a Documents folder called **Passage photos**
that is made the first time, or chosen from a photo already in Documents.

- **Placing the sign:** When you make a sign from a photo, it is placed at the
  moment the photo was taken, if that is inside the replay. The camera's clock is
  used as it is.
- **No time zone:** If the photo doesn't say which time zone it was taken in, it
  is read in the boat's zone, the one shown beside the replay time. The editor
  shows the time it used.
- **No time, or outside the replay:** The sign goes where the slider is, and the
  editor says so.
- **Position:** The photo's own position isn't used, because the track is what
  the boat recorded.
- **Deleted or unloadable photo:** If you delete the photo in Documents, the
  sign keeps its title and note and shows no picture. If a photo is still there
  but can't be loaded, the card says "Photo not available" instead of hiding
  it.

Signs belong to the time of day, not to one passage record, so splitting or
merging passages keeps them. If you trim a passage so a sign falls outside it,
the sign is hidden and comes back if you undo the trim. Signs are only removed
when you delete them. If your device asks for reduced motion, signs appear with
no fade. See [Film a passage replay](../how-to/film-a-passage-replay.md).

### Legs

A passage can hold more than one leg. A leg is a stretch of the track drawn in
its own colour. A day with a stop in the middle then reads as two runs on the
map, even though it's one passage.

- **How legs appear:** Merging passages makes one leg from each, and puts a
  sign at the place where they met, named for that stop. You can also turn on
  **Starts a new leg** for any sign in the sign editor. That starts a new
  colour at that spot, for the moment you stopped to fish or to wait out a
  squall.
- **Colours:** Every leg gets its own colour on the passage map, the cruise
  map, the replay, the legend, the exported still and the blog map. The colours
  run on from one leg to the next across everything on the map, and they begin
  again after six.
- **No legs:** A passage with no such signs is one leg, one colour, as it
  always was.
- **Names:** A sign that starts a leg can wait for a name. It reads "Awaiting
  a name" and shows no label until Helmcentral has looked the place up from the
  track, or you've typed one.
- **Where it stops:** The figures under the map cover the whole passage. They
  aren't split by leg.

Under the map are the passage's figures:

- distance and time underway
- mean and maximum speed
- fuel burned
- mean wind and the strongest gust
- house battery at the start and end, and its lowest point

A figure Helmcentral couldn't read shows `--`. It is never estimated.

You can give the passage a title, change its two ends and keep notes on it.
Splitting, trimming, merging, hiding and moving are in
[Edit a passage](../how-to/edit-passages.md).

## Cruises

A cruise is a named group of passages, such as "Whitsundays with Steve and
Kim". A passage is in at most one cruise, and cruises don't nest. Legs are how you show stretches inside one passage, as
described above. A cruise's
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
because open water doesn't prove a watermaker was running. The suggestion
stays a suggestion until you press a button to confirm it.

Deleting a cruise deletes only the grouping. Its passages stay and become
unfiled.

## Sharing

A passage, a cruise or a selection of passages can go on your blog as an
animated map your readers replay, or out as a still picture for a post's
header. The **Export** button on each page makes both. Each leg is drawn in its
own colour in both. A still picture shows
the satellite photographs when the map is showing them, with the credit added
along the bottom. The blog map is always the chart.

The blog map plays the same 3D land, boat model and camera shots as the replay
on the boat. It plays your signs too, if **Include signs** is ticked. The notes
and the photos are shrunk, have their location details removed, and are written
into the card. A card can be at most 3 MB. See
[Share a passage on your blog](../how-to/share-a-passage-on-your-blog.md).

## Limits

- Tracks imported from GPX files carry no readings, so their battery, wind,
  fuel and water figures show `--`. Speed is worked out from the positions.
- Fuel, battery and water figures need the matching sensors on the boat's
  network, and the house bank chosen under Settings → SignalK → Roles for the battery.
- Distances are in nautical miles and speeds in knots. Volumes follow the
  Units choice in Settings, General: litres for Metric, US gallons for
  Imperial.
- A hidden passage doesn't come back by itself. Its recorded track is kept.
