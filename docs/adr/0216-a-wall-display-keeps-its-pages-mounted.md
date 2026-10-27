# ADR 0216: A wall display keeps its pages mounted

## Status

Accepted (2026-10-27). Changes how [ADR 0110](0110-wall-displays-are-records.md) shows a
page: the rotation still picks the page, but it no longer builds and destroys
the tiles.

## Context

The flybridge kiosk is a 3.7 GB ODROID running WPE WebKit 2.44. With the
"Wall: Nearby" page in its rotation the browser's WebProcess grew about
576 MB an hour and sat near 120% CPU. With Nearby removed it grew about 0 MB an
hour and sat near 30%. After roughly four hours the board thrashed and froze.

Each rotation swapped the active page, so every tile on the outgoing page
unmounted and every tile on the incoming page mounted fresh. The Nearby page
holds a map tile, so a lap (about 105 s) created and destroyed a WebGL map.
The JavaScript side is clean: `map.remove()` runs, the canvas leaves the DOM and
the JS heap stays flat in Chrome. WPE keeps the native and GPU memory anyway.

## Decision

On a wall display every page of the feed is mounted once and stays mounted.
Only the active page is visible.

- **Feed membership decides mounting.** A page mounts when it enters the feed
  and unmounts when it leaves (its `show_when` condition fails, it is
  unassigned or deleted). The page on screen stays until the next step, as it
  did before. A page pinned with `?page=` mounts alone.
- **Resting pages keep their layout size.** They sit absolutely positioned over
  the active page at the stack's width, hidden with `visibility: hidden`, with
  `inert`, `aria-hidden` and `pointer-events: none`. `inert` removes them from
  focus and from assistive technology.
- **`display: none` was rejected for the hiding.** A hidden element has zero
  size. MapLibre and react-grid-layout measure their container, so each would
  see a 0-wide box and have to be told to resize on every lap. The map tile
  already tracks its box with a `ResizeObserver`; with layout size kept, it
  never sees a change.
- **Embeds let go of their frame while their page rests.** An embed's iframe is
  portalled to `document.body`, outside the stack, so hiding the page would not
  hide it. A tile reads a context that says whether its page is showing, and
  the embed unmounts its iframe while it is not. The Helmcam MJPEG server
  serves one client at a time, so a hidden but live iframe would hold the
  stream and lock the next viewer out. The embed reloads on each lap, exactly
  as it did before this change. Nothing is gained or lost for embeds.
- **The polls follow the mounted pages.** Trails and the CZone switches poll
  when any mounted wall page has a tile that reads them, not only the page on
  screen.

The ordinary dashboard is untouched. It renders one board for the active page,
through the same function the wall uses for each of its pages.

## Rejected

- **A periodic `location.reload()`.** It masks the leak: the page would still
  grow for hours and then reload in front of the watchkeeper. It also hides the
  same growth from any later tile.
- **Keeping only the map alive.** Narrower, but it leaves the pattern in place.
  Any future WebGL or video tile would leak the same way, and each would need
  its own exception.

## Cost

The shell re-renders on live-data ticks ([ADR 0215](0215-tiles-subscribe-to-live-data.md))
and now renders the board of every feed page, not one. Tiles are memoized and
subscribe to their own data, so the added work is the grid and tile element
creation, not tile renders. Hidden maps stay alive and still apply data
updates, but MapLibre draws only when something changes and `visibility:
hidden` is not painted.

## Not verified

The fix is covered by tests for mounting, hiding and the embed. It has not been
run on the ODROID. The memory and CPU figures above are the baseline to compare
against after deploying.
