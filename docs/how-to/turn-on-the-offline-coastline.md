# Turn on the offline coastline

See [Offline coastline](../features/offline-coastline.md) for what it holds and
where it stops. This page is the steps. You need to be signed in with
settings access, and the boat needs an internet connection while it fills.

## Turn it on

1. Open **Settings → Offline coastline**.
2. Switch on **Keep an offline coastline**.
3. Choose **Save** in the bar at the bottom of the page.

The status line under the switch tells you what is happening:

- **Waiting for a position fix.** Helmcentral doesn't yet have a position it
  trusts, so it doesn't know which squares to fetch. It starts when one
  arrives.
- **Downloading 7 of 49.** Seven of the 49 squares within 200 nm of the boat
  are aboard. The count depends on where you are, and 40 to 60 is usual. The
  squares arrive one at a time, nearest first, about 40 seconds each with a
  pause between, so a full set takes 40 to 60 minutes on a good link. Leave it
  running. You can use the rest of Helmcentral meanwhile.
- **The map server is rate-limiting requests from this boat** (or **is
  refusing connections**). The public map server has refused the job as a
  whole, whether by rate limit, an outage, or turning this boat away.
  Helmcentral stops asking, says when it will try again, and carries on by
  itself. Nothing is marked as failed. If the server keeps refusing, the wait
  grows to hours and the message stays. It doesn't work through the squares one
  by one.
- **The map server turned the boat away. Choose another Overpass server in the
  plugin settings.** The server answered that it won't serve this boat (for
  example, "only available to white-listed usages"). Every square would get the
  same answer, so Helmcentral stops and doesn't mark any square as failed. Pick
  a different Overpass server in the OpenStreetMap plugin's settings. It tries
  again by itself once the wait is over.
- **Coverage: 31 squares · oldest 3 Oct · 12 edits since.** Everything in range
  is aboard. The date is the oldest download, and the last figure is how many
  coastline or reef edits have appeared on the public map since.

Under the status, a muted line says how many squares in range are all land and
so are skipped, for example "12 squares that are all land are skipped." They
aren't among the squares to download, so they don't count toward the "of 49" in
the progress figure. An all-land square you already hold from earlier is still
held and still counted under Coverage.

The map under it shows the squares aboard, shaded stronger the more recently
each was downloaded. It also shows the 200 nm ring around the boat, and any
square that failed to download in red. A square inside the ring that isn't
downloaded yet is drawn as an outline only. A heavy outline marks a square with edits since.
Scroll the page as normal over the map. To zoom the map, hold Ctrl (or Command)
while scrolling, or pinch with two fingers.

## If a square fails

A square that didn't download is listed under the map by its corner, with the
reason the server gave. Helmcentral tries it again by itself, waiting longer
each time, up to a few hours. A square you already hold whose weekly check for
edits failed stays held and is listed separately. Its coverage is kept. When the
map server as a whole is rate-limiting or refusing the boat, the status line
says so and when it will try again, instead of listing squares. A slow link
clears on its own. A message that the plugin isn't installed or is too old
doesn't clear on its own. For that one, update the OpenStreetMap place-names
plugin as described in [Provider plugins](../reference/plugins.md).

## Download squares ahead of the boat

Use this before a passage, while you have a good link, to fill a coast the boat
hasn't reached. The switch has to be on.

1. Open **Settings → Offline coastline** and zoom the map until it shows about
   30° across or less. Until then the page says **Zoom in to select squares**.
2. Choose **Select squares**. A grid of the squares you don't hold appears. The
   map still moves as normal: drag to pan it, pinch or scroll to zoom, or use
   the **+** and **−** buttons at the top right.
3. Tap or click a square to select it, and tap it again to drop it.
4. To select a block with a mouse, hold Shift and drag to the opposite corner.
   On a touch screen, press and hold on the first square until it shades (a
   phone gives a short buzz), then drag to the opposite corner and let go. If
   you move before the hold takes, the map pans and nothing is selected. If you
   let go without dragging, the square you held is selected, and it stays
   selected if it already was.
5. Choose **Download** with the count, for example **Download 12**, and read the
   confirm. It says how many all-land squares it will skip. Confirm to start.
   Once you confirm, the map goes back to normal. If you cancel the confirm, your
   selection stays.
6. To back out, choose **Cancel** or press Escape while you're on the map or its
   buttons. Either one drops your selection and ends select mode.

A selection can include squares you already hold. Then the bar also shows
**Remove** with a count, and choosing it deletes those squares after a confirm.
If one is within 200 nm of the boat, the confirm says it will download again.
**Remove** also works on a square you asked for that is still waiting or has
failed. It cancels the request. Once you confirm a removal, the map goes back
to normal too.

You can ask for 100 squares at a time. Squares you asked for show in amber on the
map. The line under the switch says how many are waiting and how many failed,
for example "Requested: 9 waiting, 1 failed". A failed one is red and
lists its reason, like any other failed square. Requested squares download
before the squares around the boat, in the order you asked. They wait if the
boat has no signal, and they carry on by themselves when it returns. If you
switch the offline coastline off they wait, and the line says so.

## Update to the latest map

1. Open **Settings → Offline coastline**. The **Update** button appears when
   some squares have edits since.
2. Choose **Update**. Those squares download again, after which their edits
   since go back to nothing.

Squares without edits are left alone. The weekly check runs by itself, so the
count is current within a week.

## Turn it off or clear it

Switching **Keep an offline coastline** off and saving stops all downloads and
checks. What is aboard stays, but Mate won't use it while the switch is off.

To delete the squares as well, choose **Clear coverage** and confirm. That also
cancels any squares you asked for that are still waiting. If the
switch is still on, the squares within 200 nm of the boat start downloading
again at once, so switch it off first when you want the space back.
