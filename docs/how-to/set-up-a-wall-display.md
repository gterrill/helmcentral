# Set up a wall display

This puts dashboard pages on a screen somewhere on the boat that cycles on
its own, with no interaction. See [The dashboard: wall
displays](../features/dashboard.md#wall-displays) for what a display is and
what the rotation does and does not do.

You can have several. The steps below are the same whether this is the first
screen or the fourth.

## 1. Check the device first

Before configuring anything, load `/display-probe.html` on the actual browser
and device that will drive the screen, not on a desktop. Results go to the
backend log, since the narrowest screen here is 360px tall and cannot show a
long report:

```
docker compose logs helmcentral | grep 'display probe'
```

A white screen on the wall also lands in that log: the app posts its first
startup error as a `display probe: [FAIL] boot error` line naming the message
and the script position, so a browser that cannot parse the app explains
itself without a keyboard.

The log prints a header line (browser, reported screen size, orientation),
one line per check, and a footer with the pass count. Two of the checks are
the ones you are really there for:

- **viewport** reports the screen size the browser thinks it has, next to
  what the panel actually is. These are the numbers to enter as the
  display's screen size in step 2. They are often not the panel's
  advertised resolution: the flybridge strip's browser reports 1920x1080
  against a panel that is physically 1920x360, and a television may report
  1280x720 against a 4K panel.
- **keys** is a live readout, on screen rather than in the log, of the last
  four keys pressed. Point the remote at the screen and press the direction
  keys, OK, and back; each press shows its name and code. The same lines go
  to the log so you can read them off later. This is how you confirm the
  remote drives the rotation before relying on it.

The rest are capability checks: a hardware graphics context (a software
renderer such as SwiftShader or llvmpipe counts as a failure, because the
chart and radar tiles need real hardware), modern CSS support, magnification
without a layout shift, and whether the screen can be asked to stay awake.

If everything passes, go on. If only the informational `100svh` check fails,
go on anyway; nothing uses that. If the graphics context, `:has()`,
`structuredClone` or the CSS checks fail, the browser is too old for this
dashboard. On a television that usually means driving it from a small HDMI
box running a current Chromium instead of using the set's built-in browser,
which is also the fix on an ARM board that ships an older browser by default.

Add `?rotate=180` to the probe URL to check a physically inverted screen. If
the browser reports a screen taller than the panel, add `&height=<px>` too,
for example `/display-probe.html?rotate=180&height=360`, so you are checking
the band the panel will actually show.

## 2. Add the display

Open **Wall displays** in the sidebar and choose **New display**. Fill in:

- **Name**, what you call the screen: "Flybridge", "Saloon TV".
- **Address**, the last part of its web address. `flybridge` gives
  `/display/flybridge`.
- **Screen size**, from the probe's viewport line in step 1.
- **Magnification**, how much larger to draw everything. Start at 1 for a
  screen you read close up, and 1.5 for a television across a cabin. You can
  change it after you have looked at it from where you will actually sit.
- **Upside down**, for a panel mounted inverted.
- **OLED panel**, for an OLED television, so a board left up all season does
  not burn in.
- **Keep awake**, to ask the screen not to sleep.

For a television, prefer a smaller screen size with more magnification over a
larger one at 1. Setting a saloon television to 1280x720 at 1.5 rather than
1920x1080 at 1 gives you the same picture with tiles you can lay out the way
you lay out every other page.

## 3. Put pages on it

With write access, in layout mode on each page you want cycling:

1. Toggle layout mode in the header (needs a screen 1024px wide or more).
2. Pick the display from the toolbar's display select.
3. Set how long the page shows, in seconds (5 to 3600).
4. Choose a condition: **Always**, or a vessel state such as **While
   anchored** to show the page only then.

An amber dashed line marks where that screen cuts the page off, measured for
that screen. Everything above it is what the screen shows.

Putting a page on a wall display clears its hero tile, because the hero's
extra row spends the vertical room the screen is measuring.

The wall never shows the pinned indicator ribbon. If a page needs status
lamps there, put a lamp strip tile on that page's own layout, in the space
the dashed line marks as visible.

Feed order is page order. To change which page shows first, reorder pages the
way you always do (see [Reorder dashboard
pages](reorder-dashboard-pages.md)). Pages on a display keep their place in
that order even though they no longer appear in the ordinary page list.

## 4. Point the screen's browser at it

Open `http://<your-helmcentral-host>:<port>/display/<address>` in the browser
that will run unattended, for example `/display/flybridge`. There is nothing
to click: no sidebar, no header, and no response to Back or Forward. Size and
orientation come from the display's own record, so there is nothing to add to
the address.

To hold one page while you decide whether it fits, add `?page=<page id>`
(find the id in the address bar after selecting that page normally, at
`/dashboard/<page id>`). A held page never advances, which also makes this
the way to take a screenshot.

### If you are running the wpe-webkit kiosk snap

For a small ARM board running Ubuntu Core's `wpe-webkit-mir-kiosk` snap, what
the flybridge strip uses, point it at the display and restart it:

```
sudo snap set wpe-webkit-mir-kiosk url="http://<helmcentral-host>:<port>/display/flybridge"
sudo snap restart wpe-webkit-mir-kiosk
```

## 5. Driving the rotation by hand

If the screen has a remote or a keyboard:

| Key | What it does |
| --- | --- |
| Left, right | Previous and next page. The timer restarts. |
| OK, space | Pause and resume. |
| Back | Resume, if paused. |

A brief caption names the page and its place in the feed. Confirm the codes
against the probe's key readout from step 1 before relying on a particular
remote.

## 6. Add a second screen

Repeat steps 1, 2 and 4 for the new screen, then use **Duplicate to** in the
layout toolbar to copy a page you already like onto it. You get a copy, not a
shared page, so rearranging it for the new shape leaves the original alone.
This is the intended way to build a television version of a strip page: start
from the same tiles, then spread them out.

## 7. Television settings that matter

Three things on a television set will undo the work above, and none of them
can be overridden from the dashboard:

- **Energy saving**, which dims a mostly dark picture until it is unreadable.
  Turn it off, or the wall will look broken at night.
- **Auto power off** after a few hours with no input. On LG sets this is four
  hours by default. Keep awake cannot defeat it; turn it off in the set's own
  menu.
- **Screen shift and pixel refresh**, the set's own burn-in protection. Leave
  these on. They work alongside the OLED panel option, which moves the
  dashboard's own image rather than the panel's.

## 8. Leave it running

The dashboard is served so that a reload, after a restart, a power cut, or
just because the browser felt like it, always picks up the current build.
There is nothing else to maintain. Adding or removing a page, changing a
duration or a condition, or editing the screen's size or magnification all
apply at the wall's next lap without touching the device.
