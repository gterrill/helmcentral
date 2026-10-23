# Turn on the offline coastline

See [Offline coastline](../features/offline-coastline.md) for what it holds and
where it stops. This page is the steps. You need to be signed in with
settings access, and the boat needs an internet connection while it fills.

## Turn it on

1. Open **Settings → Offline coastline**.
2. Switch on **Keep an offline coastline**.
3. Choose **Save** in the bar at the bottom of the page.

The status line under the switch tells you what is happening:

- **Waiting for a position fix.** Helmcentral does not yet have a position it
  trusts, so it does not know which squares to fetch. It starts when one
  arrives.
- **Downloading 7 of 25.** Seven of the 25 squares within 200 nm of the boat
  are aboard. They arrive one at a time, nearest first. Leave it running; you
  can use the rest of Helmcentral meanwhile.
- **Coverage: 31 squares · oldest 3 Oct · 12 edits since.** Everything in range
  is aboard. The date is the oldest download; the last figure is how many
  coastline or reef edits have appeared on the public map since.

The map under it shows the squares, shaded stronger the more recently each was
downloaded, the 200 nm ring around the boat, and any failed square in red. A
heavy outline marks a square with edits since.

## If a square fails

A failed square is listed under the map by its corner, with the reason the
server gave. Helmcentral tries it again by itself, waiting longer each time, up
to a few hours. A rate limit or a slow link clears on its own. A message that
the plugin is not installed or is too old does not: update the OpenStreetMap
place-names plugin as described in [Provider plugins](../reference/plugins.md).

## Update to the latest map

1. Open **Settings → Offline coastline**. The **Update** button appears when
   some squares have edits since.
2. Choose **Update**. Those squares download again, after which their edits
   since go back to nothing.

Squares without edits are left alone. The weekly check runs by itself, so the
count is current within a week.

## Turn it off or clear it

Switching **Keep an offline coastline** off and saving stops all downloads and
checks. What is aboard stays, but Mate will not use it while it is off.

To delete the squares as well, choose **Clear coverage** and confirm. If the
switch is still on, the squares within 200 nm of the boat start downloading
again at once, so switch it off first when you want the space back.
