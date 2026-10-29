# ADR 0228: Charts in watch reports

## Status

Accepted (2026-10-29). Builds on [ADR 0227](0227-watches.md) (watches, their
runs and their notifications) and [ADR 0191](0191-mate-shows-cards-with-its-answers.md)
(the chart card under a Mate reply). Cycle 7 of the agent-first plan.

## Context

A watch report says "pressure is down 4 hPa in 3 hours" or "the house bank
drained faster than usual". A skipper reading that at 02:00 wants to see the
curve, because the shape (a steady slide or a sudden step) is the evidence, and
the sentence cannot carry it. Mate already draws charts in chat, but those cards
hold a reference and fetch their points when they open, which does not work for a
report. A report is read days later, on a phone, from an email, and it has to
show what the boat's history said at the time it was written.

## Decision

**A finding may carry a chart, stored as data.** A chart is a title, a unit, one
or two series of `[unix ms, value]` points in the operator's units, the time
window, the boat clock's offset, and an optional threshold line. Charts are stored
in a `charts` column of `watch_runs` as JSON, each naming its finding by index.
Every series is bucketed to at most 200 points, which keeps a chart near 10 KB.
At most two charts are made for a run: findings are tried in order until two
charts are drawn. A chart that cannot be read does not use up one of the two. A
finding after the second chart says "Only two charts per report". The reads get
their own 30 second deadline, started once the review is over, so a slow review
cannot starve them.

**Rules say what to chart, and the same code reads it as reads the report.**
Each rule's finding carries a chart spec, which is a path, a window and a
conversion, and a separate step reads it:

- Pressure fall: the barometer ring buffer over 12 hours, with the 3 hour fall
  limit as the threshold line. Pascals are converted with the same unit table
  alarms use, not from a ratio of rounded display figures, so the line and the
  threshold are exact.
- Overnight draw: house bank charge and current over the night window the
  observation itself used, as two stacked panels.
- Tank change: that tank's level since the previous run, at least one hour, with
  its warning level as the threshold.

Paths other than the barometer are read through `get_path_history`'s own query, so
the bucket widths, the angle handling and the Flux shape (no `map()` before the
first `aggregateWindow`) are the ones already proved.

**Mate may ask for a chart on a finding it adds.** `submit_watch_review` accepts
`chart: {path, hours (1 to 48), label}`. The label is required, so a card never
shows a raw SignalK path. The backend reads the series itself, converts it to the
operator's unit and attaches it. Mate never supplies points. A path with no
display unit is not plotted in SI: the finding gets "No history to chart: no
display unit for this reading". A percent series is scaled once for the whole
series, with the tank level helper, so a tank at 0.5 % stays 0.5 %.
The review prompt says to attach one only where the trend is the evidence.

**No history means no chart, and the finding says why.** InfluxDB is optional, the
barometer buffer starts empty on restart, and a window can hold one reading. In
each case the finding carries `chart_problem`: "No history to chart: <reason>".
Nothing is interpolated, filled from an older reading or invented. A series with
fewer than two points is a problem, not a dot.

**The PNG is drawn in Go when something needs one.** The renderer uses
`golang.org/x/image` (the Go regular font through `opentype`) and `image/draw`.
It is pure Go with no cgo and no browser. Lines are drawn without anti-aliasing,
so the output is identical on every machine and a test can look at exact pixels.
The image is 800 by 400, white, with 13 px axis text, an 18 px title, the boat's
UTC offset in the title and on the time axis, a dashed threshold, and two fixed
colour-blind-safe series colours (blue and vermilion from the Okabe-Ito set), with
the threshold in reddish purple. Two units stack as two panels. A gap in the record
longer than four times the usual step is left as a gap, and the page breaks the
line at the same gaps. The PNG is drawn when a notification is sent. There is no
route that serves it, since nothing would call one.

**Notifications.**

- Email with charts is `multipart/related`, holding a `multipart/alternative` (the
  existing plain text, plus HTML) and one inline PNG for each chart, referenced by
  Content-ID under its own finding. An email with no charts is the plain message
  it was.
- ntfy gets the first chart as an attachment: a PUT with the PNG as the body, a
  `Filename` header and the short text in the `Message` header, with the title,
  priority, tags and click link as before. If the server refuses the upload with a
  4xx (or a 507) that mentions attachments, Helmcentral logs it and sends the
  text-only message instead, and the run records "ntfy: sent without the chart
  (reason)". That is a delivery choice, not a data fallback: the report is more
  use arriving as text than not arriving, and the run says so. Any other failure
  is retried as before.
- The webhook payload gains `charts`, each with `finding_index`, `title`, `unit`,
  `series`, `threshold` and `png_base64`. The queue stores the data, and the PNG
  is drawn when the message is sent.
- A chart that cannot be drawn never holds a report back, on any channel. The
  report goes out without that chart and says so: a line "Chart left out:
  <reason>" in the email text and HTML and in the ntfy message, and a `problem`
  field in place of `png_base64` on that webhook entry. The run also records
  "<channel>: sent without the chart (<reason>)". The same rule holds for an
  attachment the ntfy server refuses. The operator approved this exception to
  the fail-fast policy on 2026-10-30. There is no flag, because a send that left
  out a chart is never silent: the message, the run and the log all say so.
- Web push is unchanged. The link opens the run, where the page draws the chart.

**The Watches page draws the stored data** with a chart component that shares the
Mate chart card's look (series tokens, axis text, stacked panels with one unit
each). It does not reuse the card itself because that holds a reference and
fetches, with range controls that mean nothing for a stored report.

## Rejected

- **A headless browser on the box.** It would give a pixel match with the page,
  at the cost of a Chromium install and its memory on a boat computer, for an
  image the operator sees small on a phone.
- **gonum plot.** It was not in the module cache or the dependency graph, and a
  new dependency tree for one chart type was not worth it. The renderer is about
  300 lines over a package already in the graph.
- **Storing PNGs.** A 800 by 400 image is tens of kilobytes against a few for the
  points, it cannot be restyled, and the page and the notifications would draw
  from different things. Storing the data lets the page, the email, the phone and
  the webhook agree.
- **Charts on alarm notifications.** An alarm card is read in seconds and has its
  own presenter and context. It is left for later.
- **A made-up line when history is missing.** Against the fail-fast rule.

## Consequences

- The runs list carries chart data, a few kilobytes for each chart, so a page of
  runs is larger. The history space figure now counts it.
- Chart colours in the PNG are literal values, since an image has no theme. They
  are fixed, light-background colours, not the page's tokens.
