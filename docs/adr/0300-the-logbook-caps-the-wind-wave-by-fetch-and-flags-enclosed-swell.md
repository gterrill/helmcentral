# ADR 0300: The Logbook Caps The Wind Wave By Fetch And Flags Enclosed Swell

## Status

Accepted (2026-11-22). Builds on
[ADR 0299](0299-recorded-passages-keep-the-sea-state-behind-each-spell.md), which
keeps the model's sea behind each spell and names this cap as its first
follow-up, [ADR 0203](0203-offline-coastline-and-anchorage-shelter.md),
whose rays over the offline coastline this reuses, and
[ADR 0298](0298-the-logbook-says-how-a-passage-felt-in-words-learned-from-the-operators-ratings.md),
which stores the spells.

## Context

The comfort model that comes later is fitted per boat on the sea each spell had.
ADR 0299 stored that sea as the Open-Meteo marine model's figure for a cell about
28 km across. In a bay, a river mouth or a marina that figure is several times
the sea the boat felt, because the model has no cell for the shelter. Fitted
as it stands, sheltered spells would teach the fit that the boat is gentle in
rough seas.

A wind can only build so much sea over the water it has crossed. When the shore
is charted upwind, that limit is computable, and the wind wave cannot exceed it.
This ADR applies the limit in the logbook, so the operator can judge it before
anything is fitted. It builds no model and no comfort word, and the later fit
reads which spells were checked.

## Decision

### 1. Each spell keeps the wind it had

A spell gains the mean true wind speed (`wind_kts`) and the direction the wind
came from (`wind_from_deg`). The track points already carry the true wind speed,
the heading and the true wind angle, signed and positive from starboard. The
direction at a point is the heading plus the angle. The spell averages those as
vectors with each point weighted by its wind speed, so a gust counts for more
than a lull and 350 and 010 degrees average to north. A mean that cancels out has
no direction, and the spell says the wind varied (`wind_dir_missing` is
"varied", and "no heading" when no point had a heading and an angle) instead of
saying it was unrecorded. Only points with a speed, a heading and an angle
contribute to the direction. The wind speed is taken as recorded, at the height
of the anemometer. The speed log is never corrected.

A spell worked out from now on carries `wind_kept`. A spell stored before has no
wind and no flag, so the page can say "stored before wind was kept" and not
"no wind recorded". The stats version moves from 5 to 6, so the start-up pass of
ADR 0298 works every older row out again from its track points. A row whose
points are gone is carried forward with its spells as they were, and says so.
InfluxDB is not involved: the live and backfilled points already hold the wind.

### 2. The limit is the Coastal Engineering Manual's deep-water fetch law

With U10 the wind in metres per second, C_D = 0.001 (1.1 + 0.035 U10) and
u*² = C_D U10², the manual's equations II-2-36 to II-2-38 give, for a fetch X in
metres:

- Hm0 = 0.0413 (u*²/g) (gX/u*²)^½, never above 211.5 u*²/g.
- Tp = 0.651 (u*/g) (gX/u*²)^⅓, never above 239.8 u*/g.

Twenty knots over five nautical miles gives 0.50 m and a 2.2 s peak period.
Open-Meteo gives a mean period, and a fetch-limited sea has a mean period near
the peak period over 1.2, so the cap on the period is Tp/1.2. Height and period
are each held to their own limit. A wind wave is capped when either is over, and
shows the lower of the model's figure and the limit for each. The page rounds a
height to 0.1 m and a period to a whole second, so a cut finer than that would
read as a cut to the same figure. Such a cut is not named, and a wave with no
cut left that shows reads as within the limit. The note names only what was
cut: the height, the period, or both.

### 3. The fetch is seven rays over the shore

For a wind from bearing B, the sweep casts rays at B-15 to B+15 degrees in 5
degree steps, over the coastline and reef lines of the Offline coastline within
15 nautical miles (`shelterMaxFetchNm`, as in ADR 0203). The fetch is the mean
distance. The shelter estimate and this check share one projection of the
lines (`projectShoreline`) and one ray caster. The rays are cast along the
bearing rounded to a tenth of a degree, which is also how the result is keyed,
so a stored row is exactly what was computed.

- **Open ray:** a ray that finds nothing within 15 nm makes the spell "open water
  upwind", and nothing is capped. An open ray is never counted as 15 nm, which
  would invent a cap the shore does not give.
- **Islands:** a closed coastline ring blocks like the mainland.
- **Reefs:** a reef blocks like land, and the label says the limit depends on the
  tide. A position inside a reef ring gets the reef's width as its fetch, as ADR
  0203 does.
- **Calm:** below one knot a wind has no direction worth using. The spell takes
  the longest fetch over 36 bearings. Any open bearing means no cap, since the
  sea can arrive from there, and the note reads "open water nearby", since no
  direction was used. Otherwise the cap is near zero, and the note gives the fetch
  "in a calm".
- **Ashore:** a position inside the charted coastline (water lies to the right of
  a coastline way) is "charted ashore" and is not capped.

### 4. A spell that cannot be checked says why and keeps the model's figure

The page never presents the uncapped value as checked. A wind wave carries a
`fetch` block, and the server words it: `note` is the sentence the page puts in
brackets after the wave's figures, so the wording lives in one place. The block's
`check` is one of:

- `capped`: the figures are the capped ones, with the model's own beside them
  (`hindcast_height_m`, `hindcast_period_s`), the fetch, the wind and the cap.
- `within`: the model's wave was under the cap, and stands.
- `open`: open water upwind, and the model's wave stands.
- `unchecked`: the model's figures stand, labelled "not checked against fetch"
  with the block's `reason`.

A capped or within wave over a reef also carries a `caveat`, which the note
appends.

The reasons are the Offline coastline not on this install, off, or its setting
unreadable; a square within reach that is still downloading, failed (with its
reason) or outside coverage, named by its label; a position inland or charted
ashore; no true wind recorded; a spell stored before wind was kept; a wind
direction that varied too much, or that needed a heading or wind angle the spell
lacked; and "not worked out yet", for a spell whose shore the sweep has not yet
stored (section 6). The note sits inside the page's own brackets, so no reason
carries brackets of its own. A spell with no hindcast hour keeps ADR 0299's
reason, and a spell with no wind wave has nothing to cap. The later fit leaves
unchecked spells out.

The passage's peak counts the checked spells only. The summary says how many
wind waves it left out, in `partial`, or in `reason` when no peak remains, so a
passage is never called flat for spells that were not looked at.

### 5. Enclosed water flags the swell, and changes nothing stored

When all seven rays within 30 degrees of the direction the swell comes from, in
10 degree steps, hit the shore or a reef within 15 nm, the open sea cannot reach
the boat. The swell then carries `enclosed: true` and the note "swell not judged
in enclosed water", and the label says so instead of giving its figures.
The stored row is untouched, and the flag is what the fit reads to skip the
spell. A coastline that cannot be read, or a position charted ashore, leaves the
swell unjudged, and the page does not pass that off as judged: the swell keeps
the model's figure with `unchecked: true` and the note "not checked for enclosed
water" and the reason, and the passage's largest swell leaves it out and counts
it in `partial`, as it does an unchecked wind wave.

### 6. The sweep works the shore out, and the page reads what it stored

Reading the shoreline around a spell, projecting it and casting the rays costs far
more than a page can spend, and a first version that did it on every page read made
a passage with many spells slow to open. The hourly sea sweep of ADR 0299 now does
it once, right after it fills the hindcast, and stores the result in
`sea_fetch_rays`, beside `sea_hindcast_hours`. The page only reads rows.

A row is keyed by the spell's position (to a millionth of a degree), the kind of
ray (`wind`, `calm` or `swell`), the bearing in tenths of a degree (0 for a calm),
and a stamp of the squares it was read from. It holds whether the position is
charted ashore, whether the rays found open water, whether a reef stopped them,
and the mean fetch (wind) or longest fetch (calm). A swell row holds only whether
the quarter is open. Wind speed is not in a row: the cap is worked out from it
when the page is built, so a changed speed needs no new geometry.

The stamp is a hash of the download and data dates of each ready square within 15
nm of the position, the same recipe the route cache keys on. A square downloaded
again, replaced or removed changes it, and so does one that arrives where there
was none. The old rows then no longer match, and the next sweep works the position
out again. Rows under an old stamp are not removed, which is a follow-up.

Each sweep pass runs the hindcast and then the fetch, so an hour fetched in the
pass has its shore worked out in the same pass. The fetch step:

- looks at the passages oldest first, with one coastline loader for the whole
  pass, so a square is read once however many passages lie over it;
- reads the keys already stored once, and for each spell that has a hindcast hour
  and a usable wind or swell, works out only the rows that are missing, building
  one geometry for each position and state of its squares;
- builds at most 2000 geometries a pass, checks for a stop between spells, and
  logs that the next sweep carries on when it stops short, as the hindcast does;
- leaves a spell whose squares are not all held alone, since the page words why.

The page reads the coastline switch and each square's record once, plans each
position from the records alone (`coastlineLoader.plan`, which holds no lines),
and reads the passage's rows in one query. It never reads a line of coastline. The
reasons that are cheap and always current stay at read time: the coastline off,
not on this install or unreadable, a square missing, a position inland, and the
wind reasons. A spell whose position and squares have no row reads "not worked
out yet". The coastline off reads off even where rows are stored. A new square
shows within the next hourly sweep, because a download does not trigger one.

The loader gives typed errors (`coastlineLookupError`) that name the problem
without wording it. The shelter estimate's text is unchanged, and the logbook
words the same errors for itself.

## What it is not

- Not a wave model. It limits the model's figure and never raises it, and ignores
  waves bending round headlands and wind funnelled by land.
- Not tide-aware. A reef blocks as if charted at its full extent.
- Not applied to the swell's figures. The swell is flagged or left alone.

## When it can't run

- Offline coastline is off, missing or incomplete near the spell: every wind wave
  there is labelled "not checked against fetch" with the reason, and the swell
  is labelled "not checked for enclosed water" with the same reason. With the
  coastline off or not installed, the sweep logs one line saying so, stores
  nothing, and tries again at the next hourly sweep.
- The sweep has not yet been over the spell, or the squares changed since: both
  labels read "not worked out yet" until the next sweep, which is within the hour.
  A spell with no hindcast hour has no sea and nothing to check.
- The sweep cannot read a square it holds, or cannot store a row: it logs the
  failure and counts it, the spell keeps "not worked out yet", and the next sweep
  tries again.
- The stored rows cannot be read: the page says "the stored fetch figures
  couldn't be read" and logs the error.
- A spell with no true wind, stored before the wind was kept, or whose wind swung
  too far to average is labelled unchecked and says so. It needs no sweep work.

## Follow-ups

- Pruning rows stored under an old stamp of the squares.
- The comfort model, which reads only checked spells and skips enclosed swell.
- The leg chip and the departure ranking.
