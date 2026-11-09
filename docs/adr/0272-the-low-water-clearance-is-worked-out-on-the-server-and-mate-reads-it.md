# ADR 0272: The Low-Water Clearance Is Worked Out on the Server, and Mate Reads It

## Status

Accepted (2026-11-08). Amends [ADR 0135](0135-anchor-watch-warns-too-shallow-at-low-water.md):
the arithmetic moves from the browser to the backend, and Mate reads its result.
Every rule in ADR 0135 about which inputs count, in what order and when the tide
is stale carries over unchanged. Amended by
[ADR 0274](0274-the-anchor-watch-learns-the-local-tide-from-its-own-sounder.md):
once the sounder's own readings have narrowed enough, the depth at the next low
comes from them, with an interval, in place of the tide station's prediction.

## Context

On 2026-11-08 the operator was anchored with the Anchor Watch page open, which
read "Too shallow at low water". They opened Mate from the side panel and asked
"Why is it too shallow here?". Mate said there was no charted depth from the
boat's own observations and it couldn't say how much water there would be under
the keel. Later it offered to check whether a watch was running. Both were
wrong. The sounder read 4.6 m, Helmcentral has no chart, and the operator was
looking at the watch.

Three faults lined up:

- **The verdict lived only in the browser.** `computeLowWaterClearance` ran in
  the Anchor Watch drawer. Mate never saw the draft, the margin or the verdict,
  so it could not explain a figure it had no way to read.
- **The screen context said nothing useful.** For this page the prompt said only
  "The operator is looking at the Anchor Watch panel."
- **The depth guard and its wording sent Mate away from its own sounder.** The
  guard that stops an invented anchorage depth did not count a depth read from
  the live sounder this turn (the Depth & Tide tile or the vessel status) as a
  source, so a good draft was sent back. Its correction, and the shared depth
  rule, told Mate to "check the chart".

## Decision

### 1. One calculation, on the server

`computeLowWaterClearance` in `backend/anchor_low_water.go` is the only copy. It
is a port of the browser function with the same rules, except as noted below:
`no_depth`, `no_draft`, a missing tide and `tide_stale` in that order, the -1
tide sentinel, a fall clamped at zero, and a low already passed counting as
stale. `too_shallow` is judged on the clearance rounded to a tenth as displayed,
as `anchorage_details` judges it, so 0.46 m meets a 0.5 m margin. A clearance
below zero, the keel under the bottom on paper, is `too_shallow` whatever the
margin. The browser function and its tests are deleted. The 30-minute age rule
on the tide reading is dropped: the server builds the tide fresh for every
request, so no reading is older than its own request and the rule could never
fire.

A missing tide has three reasons, each its own word: `no_tide_station` (none is
set up), `tide_unreachable` (the provider failed) and `no_tide` (it answered
with no usable height). Its reason labels and the under-keel wording
stay in `frontend/src/lib/low-water-clearance.ts`.

The Anchor Watch page reads `GET /api/anchor-watch/low-water` every 15 seconds,
because the depth in the verdict is live. The answer carries `status` (`ok`, `too_shallow` or
`unknown`), a `reason` when unknown, and the figures in metres: `depth_now_m`,
`tide_fall_m`, `depth_at_low_m`, `draft_m`, `clearance_m`, `margin_m`,
`low_tide_time` and `worked_out_at`. An unknown carries only the inputs that
were there.

Page and Mate share one gate, `lowWaterApplies`: the boat is anchored or moored,
or an anchor watch is set. When it is shut the endpoint answers
`{"status":"unknown","reason":"not_anchored"}` without reading the margin or
building the tide, and the page shows no clearance line for that reason.

### 2. Mate reads the same result

`get_vessel_status` gives `boat.anchor.low_water` when the boat is anchored or
moored or a watch is set, whatever the watch's state, because the check is about
the boat's own spot. Under way with no watch it is left out, the tide is not
read, and `low_water_missing` says "the boat is not anchored and no anchor watch
is set". Each figure is a value in metres
with a display figure in the operator's units, as the rest of the tool reports
them, and `low_tide_time` is a boat-time label. The tool's description says this
is the Anchor Watch's own check and that the margin is the operator's setting,
linkable as `[Anchor Watch settings](/settings/anchor-watch)`.

### 3. The Anchor Watch page tells Mate where it is

For the `anchor-watch` panel the screen sentence names the page, says whether a
watch is set and its radius (read by the server, never from the client), and
tells Mate that questions about depth, "too shallow" or "here" are about the
boat's current position. Mate calls `get_vessel_status` and answers in this
order: the depth now, the estimated depth at the next low and its time, less the
draft, against the operator's clearance, with the settings link when it is too
shallow. It does not offer to check whether a watch is running.

### 4. The guard counts the boat's own depth

The "anchorage depth" check stays quiet when this run has a successful call that
read the boat's own depth and made no call about another place. A read of the
boat's own depth is the Depth & Tide tile with a depth in its snapshot, or the
vessel status with a surroundings depth or a low-water check that has a
`depth_now`. A call about another place is `find_places`, `marina_details`,
`anchorage_details`, `save_place`, `plan_tidal_departure`, `route_distance`,
`assess_passage_window`, `estimate_passage`, `read_document`, `search_documents`,
`search_web`, `get_planned_stay` (they bring in other places' depths), and `get_tides` or
`get_wind_forecast` when the call names coordinates. A failed call counts as
neither. The existing sources, a known `anchorage_details` depth or a `depth_m`
the operator gave, stand.

This rule has a known gap, accepted. The check cannot tell which place a figure
in the draft is about, so "Bay X has 4 m of water" passes after a plain status
read when no place call ran. Matching each figure in the draft to the figures
the read returned was built and dropped: it passed draft wording by luck of
rounding and failed on honest ones.

For a place elsewhere with no saved or operator figure, the correction says the
depth there is unknown because Helmcentral holds no charted depth. No
instruction to Mate mentions a chart for depth.

## Failure modes

- **No depth, no draft, no tide, a passed low:** an `unknown` with its reason,
  never a substituted value. The page shows the reason label. Mate gets the
  reason and a sentence saying why.
- **The sounder has stopped:** the server counts a depth older than 120 seconds
  as `no_depth`, the page's own `STALE_AFTER_SECONDS`, so the verdict and the
  Depth reading beside it go quiet together. An unknown age is no depth, the
  same rule the vessel status gives its depth reading, and that limit is the
  status tool's own, so the two cannot disagree. The browser used to keep
  reasoning from the last depth it had.
- **A poll fails:** the query keeps its last answer. The page keeps showing that
  verdict, faded and marked "not updated since" the time in `worked_out_at`, so a
  live warning is neither hidden nor passed off as current. This is a labelled
  stale display, not a stand-in. Before any good answer the page shows
  "Low-water clearance unavailable".
- **The boat's state or the Settings file cannot be read:** the endpoint answers
  with an error and the page shows "Low-water clearance unavailable". The margin
  is never defaulted, since a default would pass for the operator's own figure.
  In `get_vessel_status` the section is absent with a reason instead.
- **A tide that cannot be built:** an `unknown` with `no_tide_station` or
  `tide_unreachable`, shown as "No tide station" and "Tide unavailable", and, for
  Mate, a sentence saying which.

## Consequences

- The page and Mate cannot disagree about the clearance, because neither works
  it out.
- A tide that cannot be built reads as an unknown with its cause in `detail`,
  which is the fail-fast outcome. `tide_stale` still fires for a low that has
  passed or a time that does not parse.
- Every poll reads the tide service, whose provider has a TTL cache. The
  verdict's depth can trail the live depth by up to one poll, 15 seconds.
- `get_vessel_status` reads the tide only while the gate is open, including for
  the watch reports that build it.
- ADR 0135's other consequences stand: the check looks one low ahead, is a
  display and not an alarm, and runs slightly conservative because
  `belowTransducer` under-reads.
