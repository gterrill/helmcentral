# Set up a wall display

This turns an existing dashboard page into a screen on the wall that cycles
on its own, with no interaction. See [The dashboard: the kiosk
feed](../features/dashboard.md#the-kiosk-feed) for what the rotation does and
does not do.

## 1. Check the device first

Before wiring anything up, load `/kiosk-probe.html` on the actual browser and
device you plan to run the wall display on, not on a desktop. The screen is
only 360px tall, so the page itself just shows a compact PASS/FAIL grid at a
glance; read the actual results from the backend log instead, on the boat
box:

```
docker compose logs backend | grep 'kiosk probe'
```

That prints a header line (user agent, viewport, rotation), one line per
check, and a footer with the pass count. The checks are: a hardware WebGL2
context (a software renderer, such as SwiftShader or llvmpipe, is reported
as a failure), `:has()` selector support, `structuredClone`,
`EventSource`/`ResizeObserver`/`matchMedia`/timezone resolution, and a final
`Report POST` check confirming the page's own report reached the backend.
The page POSTs this report as soon as the checks finish and again every 60
seconds, so leaving it open keeps a fresh record in the log.

If everything passes, proceed. If only the informational `100svh` check
fails, proceed anyway; the kiosk root doesn't use that unit. If WebGL2,
`:has()` or `structuredClone` fails, the browser is too old for this
dashboard's baseline (Baseline 2024); a Chromium-based kiosk browser is the
usual fix on a small ARM board that ships an older WebKit by default.

Add `?rotate=180` to the probe URL to also check a physically inverted
screen: it rotates the page and embeds a camera feed, sized as one cell of
the grid, if you have one configured, so you can confirm the feed keeps
updating through an interruption without a page reload.

## 2. Flag the pages you want on the wall

With write access, in layout mode on each page you want cycling:

1. Toggle layout mode in the header (desktop widths only).
2. Tick **Kiosk** next to the page's skin and hero controls.
3. Set how long it shows, in seconds (5 to 3600).
4. Choose a condition: **Always**, or **While anchored** to only show the
   page while the anchor watch is active.

While editing a flagged page, an amber dashed line marks where a 360px-tall
screen would cut the page off, so you can see what fits before saving. Every
row above that line is what a 1920x360 strip actually shows; anything below
it is real but invisible on the wall.

The wall display never shows the pinned indicator ribbon, even on a page
that shows it everywhere else it's viewed, so the dashed line measures from
the top of your grid, not from the ribbon. If a page needs status lamps on
the wall, add a lamp-strip widget to that page's own layout in the space the
line marks as visible, rather than counting on the ribbon to carry it there.

Feed order is page order. To change which page shows first, or where a page
falls in the rotation, reorder pages the same way you always do (see
[Reorder dashboard pages](reorder-dashboard-pages.md)).

## 3. Point the wall display's browser at `/kiosk`

Open `http://<your-helmcentral-host>:<port>/kiosk` in the browser that will
run unattended. Add `?rotate=180` if the physical screen is mounted upside
down. There is nothing to click; the screen has no sidebar, no header, and
does not respond to Back or Forward.

To preview one specific page without waiting through the rotation, for
example while you're still deciding whether it fits, add `?page=<page id>`
(find the id in the address bar after selecting that page normally, at
`/dashboard/<page id>`). A pinned page never advances, which also makes this
useful for a screenshot.

A dropped connection to the server or a live alarm shows as a small pill in
the bottom corner rather than the dashboard's usual full-width banner; both
are silent otherwise.

### If you're running the wpe-webkit kiosk snap

For a small ARM board running Ubuntu Core's `wpe-webkit-mir-kiosk` snap
(what this project uses), point it at the dashboard and restart it to pick
up the change:

```
sudo snap set wpe-webkit-mir-kiosk url="http://<helmcentral-host>:<port>/kiosk?rotate=180"
sudo snap restart wpe-webkit-mir-kiosk
```

Drop `?rotate=180` if the screen isn't mounted upside down.

## 4. Leave it running

The dashboard shell is served with `Cache-Control: no-cache`, so a reload
(after a restart, a power cut, or just because the browser felt like it)
always picks up the current build rather than one that might reference asset
files a later deploy has already removed. There is nothing else to maintain;
flagging or unflagging a page, or changing its duration or condition,
applies at the wall display's next lap without touching the device itself.
