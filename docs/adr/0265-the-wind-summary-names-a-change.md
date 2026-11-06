# ADR 0265: The Wind Summary Names A Change

## Status

Accepted (2026-11-06). Replaces the first-to-last wind line that
`buildWindSummary` built for each forecast day.

## Context

The day's wind line compared the first and last hourly direction and quoted one
minimum and maximum speed for the whole day. On 9 November the forecast was
NNW 3 to 5 kts overnight, calm at 4 AM, SSE to SE at 5 to 8 AM, then E to ESE
building to 8 kts with gusts to 20 kts at 5 PM. The headline read "Winds 0 to 8
kts from the NNW-E, gusting to 20 kts." The south-easterly change vanished, and
the 0 came from one calm hour. For a boat at anchor, a turn through 170 degrees
is the thing to know about, and the line hid it.

## Decision

`buildWindSummary` describes the day in periods, from the same hourly series the
wind graph draws.

- **Calm is under 2 kts.** A calm hour has no meaningful direction, so it
  neither joins a period nor sets a speed range. The line says the wind goes
  light at that hour.
- **A period is a run of hours within 45 degrees of its own circular mean.** A
  new period starts when an hour is further out than that, or when a calm hour
  separates two runs. The circular mean means 350 and 10 degrees average to
  north, so a wrap past north is not a turn.
- **A group is a run of periods with no calm in it.** A calm always ends a
  group, so a wind that comes back after a lull is a new group with its own
  speeds: "NE 10 kts, going light around 12 PM, then NE 15 kts again from
  2 PM". "Again" is used only when the wind comes back within 45 degrees of
  where it was before the calm; otherwise it reads "then E 15 kts from 5 AM". A group also ends when a period's mean has turned 90 degrees or more
  from where the group started within three hours, so a quick veer in 45-degree
  steps is still named as a change. A slow drift stays in one group, because
  its label already reads "turning SSE around 6 AM and settling ENE"; splitting
  it gave Nov 13 a fourth clause that told the operator nothing new.
- **A change has to last.** A one-hour period is a variable hour when it turns
  90 degrees or more from the nearest longer period with a direction on each
  side, looking past calm spells and other one-hour swings. So two odd hours
  in a row, each turning from the steady wind around them, are one variable
  stretch timed at its first hour, while a coherent two-hour turn stays a
  period. A variable hour's speed always counts in the speed range of the group
  around it. The line says "briefly variable around 9 PM" when the group goes
  on afterwards, and plain "variable" when nothing follows it in the group.
  With a calm beside it, it says "light and variable" only when every hour of
  the stretch is under 5 kts (`windLightKts`); a strong swing is not light.
  A one-hour swing on the very first or last hour has nothing on one side and
  can still be a change.
- **Hours come from the clock, not from list positions.** The three-hour
  change window and the one-hour swing test use the difference in `HourOfDay`.
  A provider that sends entries three hours apart does not have its entries
  read as hourly: a single three-hour entry is not a one-hour swing, and a
  turn across nine hours is "later", not a change.
- **A change is a turn of 90 degrees or more.** Between two groups it is a
  change when the turn took three hours or less, counted from the last hour
  still within 45 degrees of where the old group started, or across a calm
  from the last hour of the old period. Otherwise it reads "then S 10 kts from
  3 PM". A change names where the wind turns to and the time of the new
  group's first hour: "turning SSE around 5 AM". It names where the group
  ends with "settling E", and the speed range covers every hour of the group.
- **A span reads in time order.** A wind that went SE then E reads "SE to E".
  A run of four or more direction hours uses the mean of its first two and its
  last two. A shorter run uses its first and last hour.
- **Equal speeds read plainly inside a clause**: "6 kts". "Around 6 kts" is
  kept for the steady one-period form.
- **The peak gust carries its hour**, in the steady form too: "gusting to 29
  kts around 2 PM". The first hour whose rounded gust reaches
  the rounded peak wins, so a raw 24.4 at 5 PM does not take the time from a
  24.2 at 3 PM. As before, the line mentions a gust only when its rounded value
  exceeds the day's rounded maximum speed.
- **A steady day keeps the short form**: "Winds 4 to 8 kts from the ENE,
  gusting to 20 kts." A day with every hour calm reads "Light winds all day."

The Nov 9 forecast now reads: "NNW to NW 3 to 5 kts, going light around 4 AM, then turning SSE around 5 AM and settling E 3 to 8 kts. Gusts to 20 kts around 5 PM."

## Why not a language model

The line is deterministic so that it matches the graph under it, gives the same
words for the same data, and cannot invent a shift that the series does not
hold. An hour with no direction (the `-1` sentinel) joins the running period for
speed and adds nothing to its direction. A day with no direction at all reads
without a "from".

## What does not change

The wind graph, the hourly series, the stored forecast and the API field
(`wind_summary`) are the same. The wording is the only difference, and nothing
stored needs rewriting.

## Alternatives rejected

- **Widen the first-to-last range to the day's min and max direction.** It
  names every point the wind touched and says nothing about when, so a boat at
  anchor cannot tell an early shift from a late one.
- **Ask Mate to summarise the day.** It costs a call per day and can word a
  change the data does not contain.

## Left for later

Shelter labels from [ADR 0243](0243-an-anchorage-shelter-claim-needs-the-shoreline-check.md) could tie a change
to the anchorage's exposure ("turning SSE around 5 AM, which is open here"). The
summary does not know the place yet, so that waits for a follow-up.
