# ADR 0128: Mate Answers Questions About Nearby Vessels

## Status

Accepted (2026-09-25). Adds an eighth tool to the onboard assistant's
tool-calling loop ([ADR 0093](0093-onboard-assistant-over-openrouter.md)),
joining the live AIS list ([ADR 0042](0042-nearby-vessel-staleness.md)) with
the sighting log its encounter-confirmation dwell protects
([ADR 0044](0044-nearby-vessel-encounter-confirmation.md)) and, where it
exists, a vessel's own logged position history.

## Context

Ask Mate "how long have Hot Chilli and Solaris been at their current
mooring" and it said it had no access to other vessels - a real gap, since
Helmcentral already keeps two independent records of exactly that: the live
AIS list behind the Nearby Vessels tile, and the sighting log that records
when a vessel first came within range, which the tile's own sighting-history
popup already reads via `listSightings`.

Both records answer a version of "how long has X been there," but neither
answers it precisely on its own:

- The live list (`fetchSignalKNearbyVessels`, `signalk.go`) only ever holds
  what is in range right now, capped at the 10 closest vessels for the
  tile's own display - too few for a tool that might need to search past the
  closest handful to find a vessel by name, and no memory of anything that
  has left range.
- The sighting log (`nearby_contacts.go`) records a row when a vessel first
  comes within 5km of *us*, after a 5-minute confirmation dwell
  ([ADR 0044](0044-nearby-vessel-encounter-confirmation.md)) - so its
  "seen_at" is a lower bound on how long the vessel has actually been
  wherever it is, not an arrival time. A boat could have dropped its own
  hook an hour before we motored into range of it, and the sighting log has
  no way to know that.

The one record that could answer precisely - how long a specific vessel has
actually been sitting at its current position, independent of when *we*
showed up - is that vessel's own logged track. Helmcentral's SignalK server
exposes exactly that through its v2 History API
(`/signalk/v2/api/history/values`), which already works for this vessel's
own `self` context (confirmed live, 2026-09-25, against
`vessels.urn:mrn:imo:mmsi:518999323`: a `context`/`paths`/`from`/`to`/`resolution`
query returns `{"data":[[iso-time, [lon,lat]], ...]}` at the requested
bucket size). Querying the same endpoint for another vessel's MMSI returns
`200` with an empty `data` array - not an error, just nothing recorded - and
`GET /signalk/v2/api/history/contexts` lists only this vessel's own context.
That is consistent with what this boat's InfluxDB writer has always done:
record self's own telemetry, never another vessel's. The History API itself
is generic and per-context; it will start answering for other vessels the
moment something writes their tracks into it. Nothing in this ADR requires
that to happen - it is a capability that lights up on its own if it ever
does, not a dependency this tool waits on.

## Decision

### `get_nearby_vessels`, the eighth tool

`assistant_nearby_vessels.go` adds `get_nearby_vessels` alongside
`find_places`/`get_wind_forecast`/`get_tides`/`estimate_passage`/`read_help`/
`search_documents`/`read_document`, following the same
`assistantToolDeps`-injected, `capToolResultJSON`-capped shape every other
tool already uses. Arguments: `name` (substring on vessel name, or an exact
MMSI), `max_results` (default 10, ceiling 25 - the tile's own display cap of
10 stays the tile's, this tool just needed room to search past it),
`include_history` (defaults to `true` only when `name` narrows the answer to
a specific vessel or two; a bare "who's nearby" never pays for a full
sighting-history read or a History API round trip per vessel).

### The sighting log is the primary source; it says so about its own limits

`in_range_since` is the sighting log's current-encounter start
(`listSightings`' newest row), reported only once that encounter has
actually confirmed - a vessel still inside `nearby_contacts.go`'s
confirmation dwell reports no `in_range_since` at all, rather than borrowing
whatever row happens to be newest (which, for a *returning* vessel, would be
its previous, already-ended encounter). `nearbyContactStore` gains
`isPending` for exactly this check, and `latestContactsByName` so a name
that matches no live target can still answer "when did we last see it" from
the log alone (`not_in_range` in the result). `recordContactIfNew` itself
also gains a small ordering fix here: a confirming candidate's row is now
inserted before its pending entry is cleared, not after, so a concurrent
`isPending` check can never observe "not pending" before the row it implies
exists is actually there. The dashboard's own Nearby Vessels tile has long
carried the same underlying gap the other way - `summaries()`'s "known
transient undercount" doc comment already accepts that its `seen_count` can
read one low for the length of the confirmation dwell - and is left as is;
Mate's tool leaves the field absent instead of one low, since a missing
figure reads honestly where a wrong one would not.

Every result carries a fixed top-level note - both in the tool's own JSON
and in the system prompt itself - stating plainly that `in_range_since` is a
lower bound: the vessel may have arrived before it came within range of us.
Putting this in the prompt as well as the result matters: a model reading a
bare ISO timestamp under a field named `in_range_since` will otherwise
report it as an arrival time, which is precisely the false precision this
ADR exists to avoid.

### The History API is the dwell source, present only when it has data

`stationary_since` is a second, independent figure, computed by
`computeStationarySince` over a vessel's own `navigation.position` history
(14 days, 600-second buckets): a robust "current position" estimate - the
per-axis median lat/lon of the most recent `assistantStationaryCentreWindow`
(5) points, not the single latest fix - and a point only counts as evidence
of a real relocation once `assistantStationaryConsecutiveOutliers` (3) of
them in a row lie more than `assistantStationaryThresholdMeters` (100m) from
that centre. Both choices exist for the same reason: a single noisy GPS fix,
or ordinary mooring/anchor swing that happens to oscillate past the
threshold and back, is not a relocation, and an algorithm that trusted one
data point at a time could not tell the difference. `history_covers_from` is
always reported alongside `stationary_since`, so a vessel with no qualifying
move found across the whole 14-day window reads honestly as "at least since
the start of the window," not as a confirmed exact arrival time.

An empty history (today, every vessel but self) reports
`position_history: "none recorded"` - a normal, expected outcome, not an
error. A genuine request failure reports `position_history_error` for that
one vessel only, never failing the whole tool call over one vessel's history
being unavailable (AGENTS.md's fallback policy: surface the failure
explicitly, scoped to what actually failed). This lookup only ever runs
alongside a name filter, never on a bare "who's nearby" listing, even if
`include_history` is explicitly set true without one - it is a real HTTP
round trip per vessel, and the result's own note says as much when that
combination is asked for. When a name filter does match more than one
vessel, their lookups run concurrently rather than one after another, so
total latency is that of the slowest single request, not their sum.

### `fetchSignalKNearbyVesselsLimit` generalizes the tile's cap

`fetchSignalKNearbyVessels` (`signalk.go`) always trimmed to the 10 closest
vessels - the tile's own display limit, baked into the fetch. Splitting a
`fetchSignalKNearbyVesselsLimit(..., limit)` underneath it (the existing
function now calls it with `10`) keeps the tile's behaviour byte-for-byte
unchanged while letting this tool ask for more. A bare, unfiltered listing
still fetches capped at `assistantNearbyVesselsMaxMaxResults` (25) - it never
needs more than that many vessels in the first place - but a name/MMSI
filter fetches with `nearbyVesselsUnlimited`, a sentinel that skips the trim
entirely, so a specific-vessel lookup always searches every vessel currently
in range rather than a second, merely-larger-but-still-finite cap that a
crowded-enough anchorage could still exceed.

Two live vessels reporting the same MMSI - a data-quality fault upstream,
not something this tool can prevent - would otherwise resolve to the same
sighting-log row for both. `fillAssistantSightingHistory` guards against
this: a vessel's sighting-log history is only attached when the log's most
recently recorded name for that MMSI matches the live vessel's own current
name, so the wrong vessel is left unenriched rather than borrowing the
other's history.

## Consequences

- Mate can now answer "how long has X been nearby," "who's around us," and
  "when did we last see X" - the three shapes of question the missing tool
  used to refuse outright.
- `in_range_since` remains, by construction, an honest lower bound rather
  than an arrival time - both the tool result and the system prompt say so,
  so a model cannot state one as the other even under a leading question.
- `stationary_since` is dormant for every vessel but this one's own, today,
  purely because nothing writes other vessels' tracks into InfluxDB yet. No
  code here assumes that changes; the day it does (a future writer, or a
  SignalK tracks-recording plugin), this tool's answers get more precise
  with no further change.
- `nearbyContactStore` gains two small read methods (`isPending`,
  `latestContactsByName`) and one ordering fix to its existing write path
  (`recordContactIfNew` inserts before clearing pending, not after); neither
  changes behaviour for its existing callers. `fetchSignalKNearbyVessels`'s
  own contract (10 closest vessels) is unchanged.

## Related

- [ADR 0093](0093-onboard-assistant-over-openrouter.md) for the tool-calling
  loop, `assistantToolDeps` and `capToolResultJSON` this tool follows.
- [ADR 0042](0042-nearby-vessel-staleness.md) and
  [ADR 0044](0044-nearby-vessel-encounter-confirmation.md) for the live list
  and the sighting log's confirmation dwell this tool reads from without
  changing.
