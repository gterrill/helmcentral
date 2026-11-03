# ADR 0252-a: One time format, and tools give boat-time labels

## Status

Accepted (2026-11-03). Builds on [ADR 0230](0230-boat-time-comes-from-position.md),
which chose the boat's zone, and amends the tool results described in
[ADR 0219](0219-mate-reads-the-passages-logbook.md) and
[ADR 0231](0231-helmcentral-is-an-mcp-server.md).

## Context

A morning report said fresh water fell "between 02:00 and 04:00 on 2 Nov". It
fell at 1.30 PM boat time, which is UTC+10. `get_path_history` gave each bucket a
UTC `t`, such as `2026-11-02T03:30:00Z`, beside a `timezone` field reading
"AEST (UTC+10)". The review prompt said every report time was boat time. Mate
read the UTC stamp as boat time, took the hour off it and wrote 02:00 to 04:00.
The zone field was there, and the model still used the stamp as it stood.

The app also wrote times three ways. Reports, Mate's prompts and the telemetry
charts used 24 hours (`13:30`). The anchor, battery and depth tiles used 12 hours
(`1:30 PM`). Sunrise and sunset used `1:30PM`. A reader moving between them had
to switch, and the code carried a 12/24 option on one helper and a dozen
separate layouts elsewhere.

## Decision

**One format, with no setting.** Every time an operator reads is `1.30 PM`: a dot
between hour and minutes, capital AM or PM, the minutes always present, no
leading zero on the hour. `10.00 PM`, `12.00 AM` for midnight, `12.00 PM` for
noon. The same form covers Mate's answers, report findings, schedule sentences,
alarm text, emails and tiles. With a day it is `Mon 2 Nov 1.30 PM`.

A chart axis is shorter: `1 PM` on the hour, `1.30 PM` when the tick is not on
the hour. A chart's tooltip uses the full form.

**Tools give finished labels, not UTC.** Each instant in a tool result for Mate
and for the MCP server is a label such as `Mon 2 Nov 1.30 PM`, written on the
boat's clock (or on the clock of the place the record belongs to, as a passage
does). A bucket's `t` in `get_path_history` is that label. Mate is told to quote
a label as given, never to work a time out from one, and to write any other time
in the same form.

UTC survives only where the next tool call needs it, and those fields end in
`_iso`: `start_iso`, `end_iso`, `first_seen_iso`, `last_seen_iso`, a passage's
`start_iso` and `end_iso`, the `iso` of a tide extreme, `timestamp_iso` on a
SignalK path, and `arrive_iso` and `leave_iso` on an anchorage. They are for
passing back as arguments and are never for quoting. A field that a card draws
from, such as the anchorage depth checks' `lowest_water_iso` and
`tide_checked_until_iso`, keeps its instant in an `_iso` field and gives Mate
a label under the plain name. A tool that hands Mate a card's payload as well,
such as the wind rose's strongest gust and the tile card's `captured_at`, gives
Mate its own label and leaves the instant in the card.

When no fix has ever been seen the boat has no zone, so the label is written on
UTC and says UTC (`Mon 2 Nov 3.30 AM UTC`), and the existing `timezone_note`
still warns. One helper, `boatTime.DayClock`, writes the label for every tool
that uses the boat's clock, so none of them forgets the word. A passage's label
is the exception: it is written on the passage's own zone, or on UTC with the
word when the passage recorded none.

**The year, and seconds, where they matter.** A logbook label names its year
when the record is not in the boat's current year (`Sat 14 Sep 2024 9.00 AM`),
because a logbook can span years and `Sat 14 Sep` would be ambiguous. A watch
samples once a second, so its gaps, excursions, start and end carry seconds
(`1.30.05 PM`). Nothing else does.

The labels are built in one place, `boatClock` and its day and zone forms in
`boat_time.go` on the server and `formatTimeOfDay` in `vessel-time.ts` in the
browser. Both build the text from the hour and minute, so a locale cannot change
the dot or the case.

## What stayed as it was

- **Time inputs and cron fields.** A time box is the browser's own control and a
  cron string is typed by the operator, so they keep their shape. The sentence
  and the preview beside them use the new form.
- **A tool's own arguments.** `depart_time` on the passage window tool still
  takes `HH:MM`, and range arguments still take RFC3339, because Mate writes them
  and the server parses them.
- **The clock tile's face.** The large clock with seconds is an instrument, not
  a time label.
- **Log lines, stored records and machine formats.** Reports and alarm bodies
  already stored keep their wording; nothing is rewritten.

## Consequences

- A tool that returns an instant must return a label and, only if another tool
  takes it, an `_iso` field. A test for a new tool should check no bare UTC stamp
  reaches the result outside an `_iso` field.
- The weather payload sends sunrise and sunset as RFC3339 instants, not as
  labels. The browser formats them with `formatTimeOfDay` in the boat's zone and
  Auto theme reads the time of day from them; nothing parses display text. The
  weather cache holds the provider's raw instants and the response is built per
  request, so no stored state carries the old strings.
- A watch report's webhook keeps `generated_at` as its machine time and gains
  `since_at`, the instant behind `since`. `boat_time` and `since` are labels in
  the new form, so a receiver that parsed them must read the UTC fields instead.
  `run.boat_time` has no twin of its own: the run starts when the report is
  generated, so `report.generated_at` is that instant.
- A stay's forecast shows sunrise and sunset in the stay's zone, not the boat's.
  The instants carry their own offset, but the browser formats them in a named
  zone, so the drawer passes the stay's.
- The clock tile shows `--` for sunrise and sunset when the browser does not
  know the boat's zone, the same as its clock, date and ETA, and the tile says
  why beside the clock. That is deliberate: there is no quiet fall back to the
  instant's own offset.
- Error messages that quote a range or a tide extreme still write it as UTC with
  a `Z`. They say what they are, and they are not part of a successful result.
- The tile card's snapshot is the tile's own data and keeps the tide service's
  strings, so a snapshot can still hold an instant. Mate is told in the note to
  quote the display figures, which are labels.
- Gap ranges from `get_path_history` are relabelled in the tool, because the same
  gap code feeds the telemetry API that keeps RFC3339.
