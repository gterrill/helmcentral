# ADR 0219: Mate reads the Passages logbook, not the telemetry, for passages

## Status

Accepted (2026-10-28). Replaces the data source of `list_passages` from
[ADR 0166](0166-mate-reads-logged-telemetry-for-history-questions.md). Builds on
[ADR 0176](0176-helmcentral-records-its-own-track.md) (the logbook),
[ADR 0211](0211-recorded-passage-decisions.md) (ratings) and
[ADR 0172](0172-mate-tools-are-one-record-each.md) (one record per Mate tool).

## Context

The operator asked Mate to "summarize our cruising season". Mate answered with 11
passages and about 297 nm. The Passages logbook holds 64 named passages, from
Pittwater on 28 February to Haslewood Island, about 1,968 nm and 734 L of fuel.

Three faults stacked up:

1. **`list_passages` read the wrong record.** ADR 0166 gave Mate a tool that
   detected runs in InfluxDB's speed over ground, because Mate needed time
   windows and the logbook did not exist yet. The logbook arrived with ADR 0176.
   The tool kept its 60-day ceiling, its approximate distances and its lack of
   place names.
2. **No tool read the logbook at all.** Mate had the names, ratings and fuel
   figures one query away and no way to ask.
3. **Mate filled the gap with guesses.** It said the earlier passages "predate the
   telemetry", wrote that the boat went "north into the Whitsundays" (they lie
   south of Townsville), and described the route from its own idea of the coast.

## Decision

**Replace the source of `list_passages` and add `get_cruise_summary`.** Both read
the logbook through `passageStore`, the store behind `/api/passages`.

### Replace, not add beside

A second passage tool next to the telemetry one would leave Mate choosing between
two answers to "what were our passages" that disagree by a factor of six. The
telemetry tool's other job, handing `get_path_history` a start and end, is done
by the logbook's own start and end, which are the times the operator sees and has
edited. So `list_passages` keeps its name and its place in the prompt, and
changes its data. The detector, its constants and its tests are deleted.

The logbook is SQLite ([ADR 0141](0141-one-business-database.md)), so the tool
also works with InfluxDB off. `get_path_history` still needs InfluxDB, and says so.

### `list_passages`

Arguments: `from` and `to` (a `YYYY-MM-DD` date or RFC3339) and `limit` (default 20,
at most 100). A bare date is a day on the boat's clock: the whole-hour zone the
Passages page uses for a passage, its start longitude over 15, rounded. So
"August" is the boat's August and not UTC's. An RFC3339 bound is an instant. A
passage is in the range when it departs in it. Hidden passages are not in the
listing, as on the page.

Each passage returns start and end in UTC and on the boat's clock
(`start_local`, `end_local`, such as `2026-10-27 10:16 UTC+10`, so the model does
no zone arithmetic), the recorded from and to names, title, cruise name, rating,
notes, duration, time under way, distance in NM, mean and max speed, mean true
wind and gust, and fuel burned in litres. The rating is the label the page shows
("Shouldn't have gone"), not the stored id. A figure the log did not record is
`null`. The tool never fills it in.

**A passage under way.** The logbook stores a passage when it ends, so a run in
progress is not in it. The tools run the passage detector over the track points
since it last stopped, as the recorder does, and report a run that is long enough
to count as `under_way_now` (start, start on the boat's clock, distance so far).
This reads the track log in SQLite, not the telemetry. The run is kept out of the
count and the totals, and the result's note says so.

### `get_cruise_summary`

Same range arguments, everything in the logbook by default. It adds up in Go,
so the model quotes the totals and does not sum 64 rows:

- passage count, total distance, time under way, and the first place left and
  last place reached (the last place and arrival are from the passage that
  finished last)
- fuel burned over the passages that recorded it, with how many did and did not
- the route: the recorded place names in order, repeats collapsed, blanks left out
- the cruises touched, and the three longest passages
- stays, and the longest ten of them

A **stay** is a gap of six hours or more between one passage's end and the next
one's start. The place is where the earlier passage arrived. If the next passage
left from a differently named place, the stay carries both names and the tool
chooses neither. Shorter gaps are the same day's hops. Overlapping passages make
no stay. A hidden passage inside a gap was a move the operator left off the page,
so the stay carries `hidden_passage_between`; the tool does not guess whether it
was real. The logbook does not record anchor, mooring or dock, so the tool does
not either.

### What the tools do not know

- **No geography.** Places are the names on the records. A blank name stays blank:
  the route leaves it out, a stay there has a blank place, and the result's note
  says so. Nothing infers a direction
  of travel, a region or a bearing between places.
- **Time under way is not engine hours.** The tool names the field
  `underway_hours` and the note says so.

### Failure modes

Per the fallback policy, an empty logbook is reported and not worked around.

- **No passages at all:** `status: "empty_logbook"` and a note telling Mate to say
  so and not look in the telemetry.
- **None in the range:** `status: "none_in_range"` with the logbook's first
  departure and last arrival, so Mate can say what the logbook does cover.
- **A bad date, or `from` not before `to`:** an error the model sees.
- **The passage log not running:** an error, as the `/api/passages` handlers give.

### The prompt rule

A fixed paragraph (identical every turn) sits after the ADR 0166 history rule.
For a season, cruise, trip or where-have-we-been question, Mate calls
`get_cruise_summary` first and `list_passages` for detail. It tells the passages
story: where we left, where we went in order, notable stays and longest legs, then
the totals. It says what the logbook lacks. It never guesses the direction of
travel or a region, and never says passages predate the telemetry. The history
rule keeps its sentence and notes that `list_passages` reads the logbook.

## Consequences

- A season question takes one or two tool calls and returns the same totals the
  Passages page shows.
- Mate sees passage notes and ratings. It can answer "which passages did we rate
  Lively". A passage note's text reaches the model provider when it is
  returned, as document text already does. The help page says Mate reads them.
- A passage under way is reported apart and is not counted until it ends.
- A time that needs the telemetry, such as when the alternators cut off, still
  goes through `get_path_history` with the passage's start and end. A boat with
  no InfluxDB gets the passage list and not the readings.
- `list_passages` loses `days_back`. A model that sends it gets an unknown
  argument ignored and the default window of the whole logbook.
- Fixtures for the tests are trimmed from the live `/api/passages`, so the real
  shapes (blank names, null fuel, a rating, differently named stay ends) are
  covered.

## Alternatives considered

- **Keep both tools and tell the prompt which to use.** Rejected. It leaves the
  wrong answer one model decision away, and keeps a detector nobody needs.
- **Fall back to the telemetry when the logbook is empty.** Rejected. It is the
  behaviour that produced 11 passages. An empty logbook is a fact to report.
- **Store stays in the logbook.** Rejected for now. Deriving them from gaps needs
  no migration, and a stored stay would need the anchor, mooring or dock kind
  the boat does not record.
- **Let the model add up the figures.** Rejected. Sixty-four rows of arithmetic
  is where a model slips, and the page already shows the sums.
