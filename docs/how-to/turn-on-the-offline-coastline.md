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

To delete the squares as well, choose **Clear coverage** and confirm. If the
switch is still on, the squares within 200 nm of the boat start downloading
again at once, so switch it off first when you want the space back.
