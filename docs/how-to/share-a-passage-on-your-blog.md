# Share a passage on your blog

This puts a passage, a whole cruise or a handful of passages on your blog as a
map your readers can replay, and gives you still pictures for the top of a
post. It works from the boat's Passages page, and nothing on your blog needs
to reach the boat afterwards.

## Before you start

- The blog needs to be a Ghost site where you can add an HTML card to a post.
- Only finished passages can be exported; a leg still underway is not stored
  until it ends.
- Set the passage titles, cruise names and place names the way you want your
  readers to see them first. See [Edit a passage](edit-passages.md).

## Put the map in a post

1. Open **Passages** in the sidebar and open the passage or cruise. To share
   several passages together, select them in the list and open them together.
2. Press **Export**.
3. Under **Play length**, choose 15, 30 or 60 seconds. This is how long the
   boat takes to cover the time actually spent underway. Each night at anchor
   adds a short pause on top.
4. Tick or untick **Overview map in the corner**. It is the small map that
   shows where the coast and the track sit in the wider country.
5. Tick or untick **3D**. It starts the way the 3D button on the passage map is
   set, and it is the same setting, so changing it here changes the map too.
   Ticked, readers see the hills and headlands standing up, with your boat
   drawn on the water as a model (see below). Unticked, the card is the flat
   map it always was.
6. Choose **Copy blog embed**. The embed opens in a box, already selected.
   Press **Copy**. If the box says to press ⌘C (Ctrl+C), the browser would not
   copy for Helmcentral, which can happen over a plain `http` address. The
   text is still selected, so press the keys yourself.
7. In your Ghost post, add an **HTML** card and paste.

To look at it before it goes into a post, choose **Download embed as HTML**
and open the saved file in a browser.

Any camera shots you have set on the replay go into the card as they are; there
is nothing to tick. See [Film a passage replay](film-a-passage-replay.md)
for setting them. If Helmcentral cannot read your shots, **Copy blog embed** and
**Download embed as HTML** tell you so and stop, rather than hand you a card
without them. A passage with no shots plays as it always did.

## Save a picture

In the **Export** menu choose **Download image 1200 × 630** for a header or
social card, or **Download image 2:1** for a wide picture at twice the size
(2400 × 1200). Each shows the whole track with its title, dates and figures,
and the overview map if it is ticked. If the satellite button on the
passage map is on, the picture is drawn over the aerial photographs and carries
their credit; switch it off first for the plain chart. The file is named from the title and the
first date, for example `hamilton-island-to-mackay-2026-09-14.png`. Drawing
takes a few seconds, because the map tiles are fetched first, and it needs the
boat's screen to be online. If the photographs cannot be fetched, even for one
patch of the map, no picture is saved and you are told so; try again when the
boat is online, or switch the satellite button off to get the chart. The Esri
credit is added only when the picture is zoomed in far enough to show photographs.

## What readers see

The card opens showing the whole track with a **Play** button. Nothing moves
until they tap it. The map then starts from a view of the whole globe, so
someone who does not know the coast sees where in the world it is, and comes
down onto the track. The boat follows the line with a glow around it, and
the track behind it is drawn bold. A label shows the boat's local date and
time with its zone, for example "16 Sep 08:56 UTC+10", and the name of the
passage it is on. Each night at anchor shows as a pause captioned "Night 2 ·
Scawfell Island", using the place the passage before it ended at. At the end
the button changes to **Replay**.

With **3D** ticked the land stands up, the sea stays flat, and the sky shows
above the horizon. The glow on the track is replaced by a model of your boat,
the hull type you set under Anchor Watch settings, sized from the vessel's
length and beam and pointing the way she is going. With no length recorded the
model is drawn at a fixed size on screen, not to scale. If the model cannot be
fetched, the glowing dot stays.

Camera shots you set on the replay play for readers too: the camera eases into
each one, follows or circles the boat, and slows the replay where you set a
pace. The play length you chose is stretched by those paces: a stretch at half
pace takes twice as long to watch, so the card can run longer than the 30
seconds you picked. Readers can take hold of the
map with a mouse drag or a pinch, and the replay leaves the camera alone until
the next shot begins; a tap does nothing. Readers cannot change 3D or add
satellite photographs. The map's credit line shows as a small ⓘ in the corner,
and opens when a reader taps it. If their device asks for reduced motion, the camera cuts
between framings instead of moving.

If the hills cannot be fetched for a reader, a small "Terrain unavailable"
notice shows and the map plays flat, with shots reduced to a zoom and a centre.

Under the map, a row of figures shows distance, time underway, nights (for
cruises and selections, counted as midnights since the first departure, as on the
cruise page), fuel and the highest gust, in your Units setting. A
figure the boat did not record is left out rather than shown as `--`.

## What goes out, and what does not

- The map tiles come from public map servers (Carto for the chart, OpenSeaMap
  for the seamarks) and are fetched by the reader's own browser. Their credits
  show on the map. The map software is loaded from a public software library.
  The blog map never carries satellite photographs; only a saved picture does.
- With **3D** ticked the reader's browser also fetches the land heights from
  Amazon's public elevation data, and, if your vessel has a hull type, the
  drawing library for the boat model from the same public software library
  as the map software. Both come straight from those servers to the reader, and
  neither goes through the boat.
- The track and figures are written into the pasted card. Nothing in it points
  at the boat, so readers need no access to your network.
- The track is thinned to at most about 1,500 points across all the passages,
  which is enough to follow the line and keeps the card light.
- Your camera shots go out, and so do the hull type, length and beam when 3D is
  ticked and the vessel has a hull type. With 3D unticked the hull and its size
  stay on the boat.
- Only the track, the shots, the hull details and the figures above go out.
  Notes, edits and any other boat data stay on the boat.

## If your blog sets a content security policy

Most Ghost blogs set none and the card just works. If yours does (a security
plugin, a CDN rule or a `Content-Security-Policy` header from your host), the
card is blocked until the policy allows what it fetches. A blocked card shows a
plain map, or the map without the land heights or the boat model, and the
browser's console names the blocked address. Add these to the policy, keeping
anything already there:

```
script-src   'unsafe-inline' https://cdn.jsdelivr.net blob:
style-src    'unsafe-inline' https://cdn.jsdelivr.net
connect-src  https://cdn.jsdelivr.net https://s3.amazonaws.com https://basemaps.cartocdn.com https://tiles.basemaps.cartocdn.com https://tiles.openseamap.org
img-src      data: blob: https://basemaps.cartocdn.com https://tiles.basemaps.cartocdn.com https://tiles.openseamap.org
worker-src   blob:
```

What each is for: the card is pasted into the page, so it needs inline script and
style; `cdn.jsdelivr.net` serves the map software and, for the boat model, one
more file; `blob:` lets the map's background workers and the boat model run;
`s3.amazonaws.com` serves the land heights; the Carto and OpenSeaMap hosts serve
the chart and seamarks. Without `s3.amazonaws.com` in `connect-src` the card says
"Terrain unavailable" and plays flat; without `cdn.jsdelivr.net` in `connect-src`
or `blob:` in `script-src` the boat stays a glowing dot. The browser's console
says which of these to check, once.

## After you change a passage

The card is a snapshot. If you rename a passage, trim it or add a place name
afterwards, the card already on your blog does not change. Export again and
paste the new one over the old.
