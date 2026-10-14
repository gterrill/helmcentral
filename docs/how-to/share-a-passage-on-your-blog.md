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
5. Choose **Copy blog embed**. A message confirms it is on the clipboard.
6. In your Ghost post, add an **HTML** card and paste.

To look at it before it goes into a post, choose **Download embed as HTML**
and open the saved file in a browser.

## Save a picture

In the **Export** menu choose **Download image 1200 × 630** for a header or
social card, or **Download image 2:1** for a wide picture at twice the size
(2400 × 1200). Each shows the whole track with its title, dates and figures,
and the overview map if it is ticked. The file is named from the title and the
first date, for example `hamilton-island-to-mackay-2026-09-14.png`. Drawing
takes a few seconds, because the map tiles are fetched first, and it needs the
boat's screen to be online.

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

Under the map, a row of figures shows distance, time underway, nights (for
cruises and selections), fuel and the highest gust, in your Units setting. A
figure the boat did not record is left out rather than shown as `--`.

## What goes out, and what does not

- The map tiles come from public map servers (Carto for the chart, OpenSeaMap
  for the seamarks) and are fetched by the reader's own browser. Their credits
  show on the map. The map software is loaded from a public software library.
- The track and figures are written into the pasted card. Nothing in it points
  at the boat, so readers need no access to your network.
- The track is thinned to a few hundred points, which is enough to follow the
  line and keeps the card light.
- Only the track and the figures above go out. Notes, edits and any other
  boat data stay on the boat.

## After you change a passage

The card is a snapshot. If you rename a passage, trim it or add a place name
afterwards, the card already on your blog does not change. Export again and
paste the new one over the old.
