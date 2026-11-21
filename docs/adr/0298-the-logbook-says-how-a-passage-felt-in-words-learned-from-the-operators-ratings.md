# ADR 0298: The Logbook Says How A Passage Felt, In Words Learned From The Operator's Ratings

## Status

Accepted (2026-11-21). Builds on
[ADR 0294](0294-passages-record-motion-and-when-each-maximum-happened.md),
which stores the roll and pitch RMS and the time of each maximum,
[ADR 0295](0295-passages-record-snap-and-pounding-from-a-10-hz-motion-stream.md),
which stores roll acceleration and vertical acceleration per track point,
[ADR 0296](0296-passage-motion-is-backfilled-from-influxdb-history.md), which
fills RMS from InfluxDB history, and
[ADR 0297](0297-sideways-g-is-worked-out-from-an-operator-entered-helm-height.md),
which works out sideways g on read.

## Context

The recorder now keeps enough motion to compare passages, and the operator
already scores each passage Comfortable, Lively or Shouldn't have gone. The
logbook shows neither side of that comparison. A spike over the 19 passages with
motion (22 Oct to 17 Nov) found one figure that follows the scores: the 75th
percentile, over 15-minute spells under way, of the root of roll RMS squared plus
pitch RMS squared. The seven Comfortable passages sat at 0.2 to 1.5 degrees and
the one Lively passage at 2.1.

Two limits shape the design. The numbers that separate the words belong to the
boat, so no Pikorua value can ship as a default. And a motion figure is not a
safety verdict, so it must not stand in for the operator's score.

## Decision

### 1. The felt figure is worked out from the track points and stored

`computePassageStats` splits the under-way legs into spells. A leg counts as under
way by the existing rule (covered at one knot or better). A spell takes legs
until it holds 15 minutes of under-way time, and it closes early at a hole or at
a stop of more than five minutes, so a spell never paints over a stop. A leg's
motion is the point at its end, so the first point, whose window lies before the
passage, is left out, as in the maxima.

A spell's sway is the square root of the roll RMS squared plus the pitch RMS
squared, each weighted by the seconds of the leg it describes and divided by the
spell's measured seconds, in degrees. A point missing either RMS is skipped. A
spell with less than five minutes of measured under-way time is dropped. The
felt figure is the 75th percentile of the spells' sway, by linear
interpolation between ranks. The spell list (start, end, sway) is stored with it
for the replay strip.

The stats also gain the peak roll acceleration and the peak pounding with the time
of each, taken as the maxima are: pounding is the stored vertical acceleration
peak over standard gravity, in g.

Each of the three figures says why it is missing, and each reason comes from that
figure's own inputs, so none borrows another's. One small function per figure
builds its sentence when the stats are worked out, and the stats store the
sentence. A missing figure never carries a blank reason.

- The felt figure reads roll and pitch RMS. It says the passage was imported; had
  roll peaks but no RMS, so it was recorded before sway was kept; had only roll or
  only pitch; had under five minutes of measured time under way; or had no motion
  at all.
- Snap, and sideways g, which reuses the same function (ADR 0297), read the
  attitude feed alone. They say the passage was imported; the feed was below 8 Hz;
  it has roll peaks and no rate, so it was recorded before 10 Hz capture; it has
  roll and pitch RMS and no rate, so it was filled in from InfluxDB history, too
  coarse for snap; the feed was fast enough and no point has the acceleration; or
  no attitude was recorded.
- Pounding reads the heave alone. It says the passage was imported; heave was
  below 8 Hz; heave was fast enough and no point has a vertical acceleration; heave
  spread has no rate, so it was filled in from history; heave peaks have no rate,
  so it was recorded before 10 Hz capture; or no heave was recorded.

A slow attitude feed on a boat that sent no heave therefore says the boat sent no
heave, not that the feed was slow. The backfill stores a heave rate of about 1 Hz
with the spread, so a real filled-in passage reads "below 8 Hz"; the history
sentences cover spread or roll and pitch kept without a rate. A passage with no
track points left carries a true reason for all three figures, whether its
stored stats come from an older shape or it never had any.

The stats version moves from 2 to 4. Version 3 held a single shared reason code
in place of these sentences, so a row stored in it is worked out again too. The
felt figure is also kept in a column of its own on the passage, written with the
stats, so the bands can be learned without reading every passage's stats.

### 1a. Old passages are brought up to date once, at start-up

Helmcentral does this after the motion backfill (ADR 0296) finishes or is
skipped, in the same goroutine, so no passage is worked out before the backfill
has filled it. The pass finds every passage whose stats are missing, unreadable
or older than the current version, including hidden ones, and works each out
from its points. It logs one line: how many it worked out, how many failed, and
each failure's error. A failure on one passage is logged and leaves the others
and the server alone.

The backfill clears the stored stats and felt column of every passage whose
points it fills, in the same transaction, so a passage that held figures worked
out before the fill gets its sway the next time it is read or the next start. A
passage read before the pass reaches it is worked out on that read, as before.

### 2. The bands are learned on every read, never stored

The band words depend on all the operator's ratings, so a stored word would go
stale with each rating. The server learns the edges from the visible passages
that have a rating and a felt figure whenever it builds a list or a passage. The
learning knows only rated felt figures, so the forecast comfort that follows can
use it. The list and a passage page both ask the one function, which reads only
each passage's rating and felt column. It scans nothing and works out nothing, so
a request runs no full-table query and a passage with unreadable stats cannot
stop another page. A passage whose figures are missing joins the bands once
the start-up pass or a read has worked them out.

If that function fails, the page and the list log the error and carry it as the
bands' reason, with the figures and no band word. Neither returns a 500.

The classes run Comfortable, Lively, Shouldn't have gone. An edge exists between
two neighbouring classes only when both have a rated passage. It sits midway
between the higher class's lowest felt figure and the lower class's highest. If
the two overlap, it sits midway between the classes' medians. The bands need at
least three rated passages with a felt figure and at least one edge. Otherwise
they are "not learned yet", with the count of rated passages. If the ratings
overlap so far that the Lively edge would land at or above the Rough edge, neither
is drawn and the reason says the ratings overlap too much. Comfortable and
Shouldn't have gone alone give no edge, because Lively sits between them and
nothing was rated there.

The words are Comfortable, Lively and Rough. A passage reads Rough only above a
learned Lively to Rough edge, which needs a Shouldn't have gone rating. Without
one, a passage above the top edge reads Lively, and one above every rated passage
says "rougher than any rated passage". The mirror case, with no Comfortable
rating, reads a passage below every rated one as Lively with "gentler than any
rated passage". A figure on an edge takes the higher band.

### 3. What the API returns

- `GET /api/passages/:id` gains `felt`: the figure, its band and any note, the
  learned bands (edges, the number of rated passages they came from, and one
  sentence saying what they were learned from or why they are not learned), the
  spells each with its band, `snap` and `pounding`. Snap is the sideways g at the
  helm when the helm height is set, else the peak roll acceleration, with a note
  naming the missing height. Each carries its time for the replay. The spells,
  snap and pounding travel here and nowhere else: the page's `stats` leave them
  out.
- `GET /api/passages` gains, on each row, `felt_band`, `felt_band_note` and
  `felt_hint`, the sentence for the column's hover text. It carries no bands and
  no spells of its own.

The pages show the server's words for the bands, the notes and the hover text,
and keep none of their own.

The operator's rating is returned as it was and never written by any of this.

### 4. What the pages show

- The passage page shows a Felt row under How it went: the band word beside the
  operator's score, the figure, and snap and pounding as buttons that move the
  replay to their time, as the maxima do.
- The replay bar draws a thin strip along the slider, one stretch per spell,
  coloured from the gauge severity ladder (Comfortable as normal, Lively as
  warning, Rough as alarm) and neutral until the bands are learned. Each stretch
  is a button, so a tap, a click or the keyboard moves the replay to the start of
  the spell. The label names the band and sway, so colour is not the only signal.
  The strip is 24 px tall to hit, though the bar in it is 8 px, and a tap that
  lands between two narrow spells, or past either end, moves the replay to the
  nearest spell. It does not drag, so the slider's drag is left alone. It is drawn
  once for a set of spells, not on every frame of the replay.
- The passages table gains a Felt column with the band word, or the figure until
  the bands are learned.

## What it is not

- Not a safety verdict. Go, Marginal and No-Go stay limits only (ADR 0194).
- Not a default. No edge ships with Helmcentral, and the bands are empty on a
  boat with no ratings.
- Not an estimate. A passage with no motion has no figure and says why.

## When it can't run

- Fewer than three rated passages with a figure, or ratings in one class only: the
  band reads "not learned yet" with the reason, and the page still shows the
  figures.
- Bands not learned: every stretch of the strip is neutral.
- A passage with no spell of five measured minutes: no felt figure, with the
  reason.

## Follow-ups

- The comfort model (forecast against felt motion) is a later ADR and reuses
  these edges.
- The passages table does not show the operator's rating, so the Felt column
  stands alone. Showing both would need a rating column.
