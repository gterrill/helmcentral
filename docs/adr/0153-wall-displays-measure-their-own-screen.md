# ADR 0153: Wall Displays Measure Their Own Screen

## Status

Accepted

## Context

A wall display draws a fixed canvas of width x height logical pixels,
multiplied by a magnification (ADR 0110). If width x scale or height x scale
is larger than the window the screen's browser actually has, the right and
bottom are clipped and nothing says so.

The real case: the saloon TV, an LG webOS browser, was set to 1920x1080 at 1.
Its browser reports 1536x856 (the set's own UI scaling is 1.25), so the right
column and the bottom were cut off. The fix was magnification 0.79, the floor
to two places of min(1536/1920, 856/1080). The probe page (`display-probe.html`)
had shown the viewport, but its report went only to the server log and was
never tied to a display record, and the how-to gave "screen size from the
probe" and a 1280x720 at 1.5 example without stating the rule that makes the
example work.

## Decision

1. **The wall route reports its own viewport.** While `/display/<slug>` is
   showing, the shell sends `window.innerWidth`/`innerHeight` to
   `PUT /api/displays/:id/viewport` on mount and after a resize settles
   (debounced, and only when the size differs from the last one sent). A
   failed report is logged to the console, not swallowed.
2. **The record keeps a measurement apart from the configuration.** A display
   gains an optional `viewport` object: `w`, `h`, `measured_at`, and the user
   agent cut to 200 bytes. It lives in the same `dashboard-pages.json` file as
   the rest of the display. A record with no `viewport` loads as it always
   did. The handler never touches width, height, scale or `updated_at`, and a
   PATCH of the configured fields copies the measurement through untouched.
3. **Validation is strict.** `w` and `h` are required integers from 100 to
   10000, the body is capped at 2 KiB, and the display must exist (404
   otherwise). A malformed body is a 400, never a partial write.
4. **The editor does the arithmetic and offers the fix.** `displayFit` in
   `lib/displays.ts` is a pure function: a canvas that fits (including any
   canvas smaller than the window, and the zero "full viewport" canvas) says
   nothing; one that overflows reports the pixels over on each axis and a fit
   magnification of floor(min(vw/w, vh/h) x 100)/100, or none when that is
   under the 0.5 minimum. The editor shows the measurement, the warning and a
   **Fit to screen** button that patches the magnification only. The Wall
   displays list flags overflow next to the canvas.
5. **The wall says so itself.** When the live window is smaller than the
   canvas footprint, the overlay box draws a short note at its top left (the
   part of an oversized canvas that is still visible). It never shows for a
   canvas that fits or a zero canvas.
6. **The probe page prints the viewport large at the top**, so it can be read
   on a screen the rest of the probe overflows.

### Which auth tier

The route is `tierRead`, not `tierWrite`. The wall browser is an unattended
client that only ever reads (`GET /api/displays`, the pages, the telemetry
stream); giving it a write-capable session to report a window size would mean
issuing a stronger credential to the least supervised device on the boat. The
route can therefore be reached by any session that can read, and the handler is
built so that is acceptable: it can write only the measurement fields of a
display that exists, with bounded values and a bounded body, and it cannot
change the canvas, the pages or anything else.

The worst case is a client faking a measurement. That changes a suggestion the
operator sees (a warning, or the number Fit to screen would apply), and Fit to
screen is itself a `tierWrite` PATCH the operator clicks. Nothing renders
differently on the wall because of a measurement.

## Rejected

- **Scaling silently at render time.** The shell could shrink the canvas to
  whatever fits. That hides a wrong configuration, so the record would say
  1920x1080 at 1 while the screen showed 0.79, and the editor, the fold guide
  and the operator's mental model would all disagree with what is on the wall.
  It would also change authored grids under the operator when a TV's UI scaling
  moves. A visible warning with a one-click fix keeps the record true.
- **Applying the fit automatically when a measurement arrives.** The same
  problem, and a wall page could rewrite configuration without anyone present.
- **Reusing `PATCH /api/displays/:id`.** That is `tierWrite` and validates
  geometry; a measurement is not an edit and should not bump `updated_at`.
- **Tying the existing probe report to a display.** The probe runs before a
  display exists, to learn which numbers to enter. The wall route is what
  knows which display it is.
- **Warning whenever the canvas differs from the viewport.** The flybridge
  strip is deliberately 1920x360 in a window that reports 1920x1080. Only
  overflow is a fault.

## Consequences

- Each wall page load, and each settled resize, writes the display file once.
  A reload loop on a wall would write on every load; the cost is one small
  JSON file.
- The editor shows the measurement from the display list it already fetched, so
  a fresh report appears on the next load of that list.
- A browser that reports a different size from the panel it drives (the
  ODROID strip) is measured as the browser reports it, which is the number the
  canvas has to fit.
