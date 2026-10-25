# Share a passage on your blog

This puts a passage, a whole cruise or a handful of passages on your blog as a
map your readers can replay. It also gives you still pictures for the top of a
post. You do it from the boat's Passages page. Afterwards, nothing on your blog
needs to reach the boat.

## Before you start

- The blog needs to be a Ghost site where you can add an HTML card to a post.
- Only finished passages can be exported. A leg that's still underway isn't
  stored until it ends.
- Set the passage titles, cruise names and place names the way you want your
  readers to see them first. See [Edit a passage](edit-passages.md).

## Put the map in a post

1. Open **Passages** in the sidebar and open the passage or cruise. To share
   several passages together, select them in the list and open them together.
2. Press **Export**.
3. Under **Play length**, choose 15, 30 or 60 seconds. This is how long the
   boat takes to cover the time actually spent underway. Each night at anchor
   adds a short pause on top.
4. Tick or untick **Overview map in the corner**. It's the small map that shows
   where the coast and the track sit in the wider country. Its corner and zoom
   are the ones saved for the passage (see
   [Film a passage replay](film-a-passage-replay.md)), and the card and both
   images follow them.
5. Tick or untick **3D**. It starts the way the 3D button on the passage map is
   set, and it's the same setting, so changing it here changes the map too.
   When it's ticked, readers see the hills and headlands standing up, with your
   boat drawn on the water as a model (see below). When it's unticked, the card
   is the flat map it always was.
6. Tick or untick **Include signs**. It's ticked to start with. The line under
   it counts the signs in the passages you're exporting, for example "5 signs,
   3 photos", and under that the menu shows the size of the card. While the
   photos are being prepared, the size reads "about" and leans high, and a line
   shows progress. It settles to the exact size once they're done. If even the
   estimate is over 3 MB, the menu says so before you choose. See
   [Film a passage replay](film-a-passage-replay.md) for adding signs.
7. Choose **Copy blog embed**. The embed opens in a box, already selected.
   Press **Copy**. If the box says to press ⌘C (Ctrl+C), the browser wouldn't
   copy for Helmcentral, which can happen over a plain `http` address. The text
   is still selected, so press the keys yourself.
8. In your Ghost post, add an **HTML** card and paste.

To look at it before it goes into a post, choose **Download embed as HTML** and
open the saved file in a browser.

Any camera shots you've set on the replay go into the card as they are. There's
nothing to tick. See [Film a passage replay](film-a-passage-replay.md) for
setting them. If Helmcentral can't read your shots, **Copy blog embed** and
**Download embed as HTML** tell you so and stop, rather than hand you a card
without them. A passage with no shots plays as it always did.

### If the card is too big

A card can be at most 3 MB. Photos are what make it big. Each one is shrunk to
960 pixels on its longest side before it goes in, and a handful fit easily.

If the signs you're exporting carry more than the card can hold, **Copy blog
embed** and **Download embed as HTML** stop and name the largest photos and
their sizes. To fix it, either untick **Include signs**, or take photos off some
signs (pick the sign in the sign editor and remove its photo). Then export
again. If a photo can't be read, the export stops and names the sign.
Helmcentral doesn't leave the photo out and carry on.

## Save a picture

In the **Export** menu choose **Download image 1200 × 630** for a header or
social card, or **Download image 2:1** for a wide picture at twice the size
(2400 × 1200). Each shows the whole track with its title, dates and figures,
and the overview map if it's ticked.

If the satellite button on the passage map is on, the picture is drawn over the
aerial photographs and carries their credit. Switch it off first for the plain
chart. The file is named from the title and the first date, for example
`hamilton-island-to-mackay-2026-09-14.png`.

Drawing takes a few seconds, because the map tiles are fetched first, and it
needs the boat's screen to be online. If the photographs can't be fetched, even
for one patch of the map, no picture is saved and you're told so. Try again when
the boat is online, or switch the satellite button off to get the chart. The
Esri credit is added only when the picture is zoomed in far enough to show
photographs.

## What readers see

The card opens showing the whole track with a **Play** button. Nothing moves
until they tap it. The map then starts from a view of the whole globe, so that
someone who doesn't know the coast sees where in the world it is. Then it comes
down onto the track. The boat follows the line with a glow around it, and the
track behind it is drawn bold. A label shows the boat's local date and time with
its zone, for example "16 Sep 08:56 UTC+10", and the name of the passage it's
on. Each night at anchor shows as a pause captioned "Night 2 · Scawfell
Island", using the place the passage before it ended at. At the end the button
changes to **Replay**.

With **3D** ticked, the land stands up, the sea stays flat, and the sky shows
above the horizon. A model of your boat replaces the glow on the track. It's the
hull type you set under Anchor Watch settings, sized from the vessel's length
and beam, and it points the way she's going. With no length recorded, the model
is drawn at a fixed size on screen, not to scale. If the model can't be fetched,
the glowing dot stays.

Camera shots you set on the replay play for readers too. The camera eases into
each one, follows or circles the boat, and slows the replay where you set a
pace. The play length you chose is stretched by those paces. A stretch at half
pace takes twice as long to watch, so the card can run longer than the 30
seconds you picked.

Readers can take hold of the map with a mouse drag or a pinch, and the replay
leaves the camera alone until the next shot begins. A tap does nothing. Readers
can't change 3D or add satellite photographs. The map's credit line shows as a
small ⓘ in the corner, and it opens when a reader taps it. If their device asks
for reduced motion, the camera cuts between framings instead of moving.

If the hills can't be fetched for a reader, a small "Terrain unavailable" notice
shows and the map plays flat, with shots reduced to a zoom and a centre.

Signs you added to the replay show for readers when **Include signs** was
ticked.

- **Before Play and after the end:** Each sign is a small blue marker on the
  track, and tapping one opens its note.
- **While the replay plays:** Signs come up as cards on a line from their point
  on the track, by their own setting. An *always* sign stays up. A *while
  passing* sign comes up just before the boat arrives and goes after its time.
  An *after passing* sign stays up from then on. No more than two cards show at
  once.
- **When the replay is paused, or before it starts or after it ends:** Tapping a
  card's photo opens it large, and Escape or **Close** puts it away.
- **Look of the cards:** They're light, and they sit over the map in daylight or
  at night. A card near the edge of the map slides in or drops below its point
  so that it's never cut off.
- **Reduced motion:** If a reader's device asks for it, cards appear without
  fading in.

Under the map, a row of figures shows distance, time underway, nights, fuel and
the highest gust, in your Units setting. For cruises and selections, nights are
counted as midnights since the first departure, as on the cruise page. A figure
the boat didn't record is left out rather than shown as `--`.

## What goes out, and what does not

- The map tiles come from public map servers (Carto for the chart, OpenSeaMap
  for the seamarks), and the reader's own browser fetches them. Their credits
  show on the map. The map software is loaded from a public software library.
  The blog map never carries satellite photographs. Only a saved picture does.
- With **3D** ticked, the reader's browser also fetches the land heights from
  Amazon's public elevation data. If your vessel has a hull type, it also
  fetches the drawing library for the boat model, from the same public software
  library as the map software. Both come straight from those servers to the
  reader, and neither goes through the boat.
- The track and figures are written into the pasted card. Nothing in it points
  at the boat, so readers need no access to your network.
- The track is thinned to at most about 1,500 points across all the passages,
  which is enough to follow the line and keeps the card light.
- Your camera shots go out. So do the hull type, length and beam, when 3D is
  ticked and the vessel has a hull type. With 3D unticked, the hull and its size
  stay on the boat.
- With **Include signs** ticked, the title, note and time of each sign in the
  exported passages go out, with its photo. The photos are shrunk to 960 pixels
  on the longest side and saved again as new JPEG files. That removes the
  location and camera details the original carries, so a photo's GPS position
  doesn't go with it. The photos are written into the card itself, and nothing
  is fetched from the boat. A sign's setting for when it shows goes too, and
  nothing else about it does. Signs outside the passages you're exporting stay
  on the boat. Untick **Include signs** and none go.
- Only the track, the shots, the signs, the hull details and the figures above go
  out. Passage notes, edits and any other boat data stay on the boat.

## If your blog sets a content security policy

Most Ghost blogs set none, and the card just works. Yours might set one, through
a security plugin, a CDN rule or a `Content-Security-Policy` header from your
host. If it does, the card is blocked until the policy allows what it fetches. A
blocked card shows a plain map, or the map without the land heights, the boat
model or the sign photos, and the browser's console names the blocked address.
Add these to the policy, keeping anything already there:

```
script-src   'unsafe-inline' https://cdn.jsdelivr.net blob:
style-src    'unsafe-inline' https://cdn.jsdelivr.net
connect-src  https://cdn.jsdelivr.net https://s3.amazonaws.com https://basemaps.cartocdn.com https://tiles.basemaps.cartocdn.com https://tiles.openseamap.org
img-src      data: blob: https://basemaps.cartocdn.com https://tiles.basemaps.cartocdn.com https://tiles.openseamap.org
worker-src   blob:
```

Here is what each one is for:

- **Inline script and style:** The card is pasted into the page, so it needs
  inline script and style.
- **`cdn.jsdelivr.net`:** It serves the map software and, for the boat model,
  one more file.
- **`blob:`:** It lets the map's background workers and the boat model run.
- **`s3.amazonaws.com`:** It serves the land heights. Without it in
  `connect-src`, the card says "Terrain unavailable" and plays flat.
- **Carto and OpenSeaMap hosts:** They serve the chart and seamarks.
- **The boat model:** Without `cdn.jsdelivr.net` in `connect-src`, or `blob:` in
  `script-src`, the boat stays a glowing dot.

The browser's console says which of these to check, once.

## After you change a passage

The card is a snapshot. If you rename a passage, trim it or add a place name
afterwards, the card already on your blog doesn't change. Export again and paste
the new one over the old.
