# ADR 0191: Mate's cards come from tool results, not from the model's text

## Status

Proposed (2026-10-19). Builds on
[ADR 0172](0172-mate-tools-are-one-record-each.md) (tool registry and `collect`),
[ADR 0189](0189-mates-stream-is-one-typed-event-set.md) (typed stream),
[ADR 0158](0158-mate-changes-records-through-changesets.md) and
[ADR 0165](0165-mate-fills-in-forms.md) (proposals and form drafts as
collected cards), [ADR 0155](0155-mate-times-departures-for-a-fair-tide.md)
(fair-tide departures) and
[ADR 0166](0166-mate-reads-logged-telemetry-for-history-questions.md)
(logged telemetry for history questions).

## Context

Mate answers in markdown. That works for prose and small tables. It fails for
two questions the operator asks often enough to want a picture:

1. **"Which days can we go?"** The boat is waiting on a weather window to run
   south in legs (Cape Upstart, Bowen or Gloucester, the Whitsundays). Mate
   can call `get_wind_forecast` and `get_tides` for each leg and describe
   the result, but the answer is a wall of numbers. The skipper wants a days
   by legs grid that says go, marginal or no-go, and why.
2. **"Did the new regulator settings help?"** Comparing alternator output
   before and after a Wakespeed change means looking at amps over days next to
   house bank state of charge, or engine RPM against load. `get_path_history`
   returns about 60 buckets for the model to summarise. The operator wants a
   chart of any two logged quantities, and wants to move the date range on it
   without asking again.

We looked at OpenUI Lang, a small language in which the model writes UI that
a React renderer draws as it streams. The license (MIT) is fine. The design
does not fit (research note `.scratch/openui-lang/research.md`):

- **The model types every figure.** A gauge or chart in OpenUI holds numbers
  the model copied out of a tool result. It looks like an instrument reading,
  but nothing checks that it is one. Alarm wording already forbids showing a
  figure Helmcentral cannot vouch for.
- **It drops what it cannot parse and draws the rest.** The fallback policy
  forbids exactly that.
- **Interactive data needs browser-side tool calls.** OpenUI's Query and
  Mutation features would run tools from the browser, outside the Go registry
  and the propose/apply consent model.
- **Its cost and reliability depend on the model.** The operator swaps chat
  models (currently `openai/gpt-chat-latest`). The component library costs
  about 1-1.5k prompt tokens per uncached turn, and nobody has measured how
  well OpenRouter models write it.

Mate already has a pattern that does not have these problems. A tool's
`collect` hook lifts a proposal or a form draft out of the tool result into a
typed field on the stored message, the `message` event carries it, and
`assistant-thread.tsx` renders it as a card under the prose. The server
validated the data. The model chose to call the tool but never wrote the
card.

## Decision

**A card is collected from a tool result, never written by the model.** Two
new read tools produce cards. Each card's shape is a Go struct with a
`kind`, has a golden fixture, and is decoded strictly in TypeScript.

**Cards ride on the stored message, like proposals.** A new
`assistant_message_cards` table (message id, ordinal, kind, payload JSON)
is written in the same transaction as the assistant message and attached by
`ListMessages`. The `message` event's `message` object gains a `cards`
array. **No new SSE event**: the set in ADR 0189 is unchanged, and the
fixture for `message` grows a card.

**Unlike proposals, cards are one generic table.** Proposals and form drafts
each have a status the operator changes (applied, dismissed, saved). Cards
have none: they are read-only displays. One table with a `kind` column,
rather than one table per card, keeps adding a third card down to a struct, a
`collect` and a component. The payload is versioned by `kind`: a kind
whose shape changes incompatibly gets a new name (`passage_window_v2`), so a
stored card is never decoded against the wrong shape. With one operator and no
installed base, an unknown kind on read is an error, not a skip.

**What a card stores depends on whether its data can change underneath it.**

- **`passage_window` stores a snapshot.** The verdicts are computed from a
  forecast issued at a known time, and the prose Mate wrote is about that
  forecast. If the card recomputed itself on reopen, it could say "go" next to
  text that says "no-go". The card shows the forecast's issue time, and goes
  visibly stale once a newer forecast exists. Re-assessing is a new question
  to Mate.
- **`telemetry_chart` stores a reference.** Logged history does not change,
  and the card needs to fetch a different range when the operator moves the
  date control. It stores the series (role, resolved SignalK path, label,
  unit, axis), the starting range and optional markers. It fetches its own
  data from a new endpoint. The model never sees the samples it draws.

### `assess_passage_window`

A read tool that assesses an **itinerary**, not a single hop. Arguments:

- **Legs in order.** Each leg has a name, its route as a start, optional via
  points and an end, and either `passage_hours` or a planned speed.
- **A stay after each leg but the last,** in nights ("two nights at Cape
  Upstart"). Zero means the next leg leaves the morning after arrival, or the
  same day when the itinerary says so.
- **Candidate departure days** for the first leg (1-7), each with an earliest
  departure time.

Each row of the result is one candidate departure day. That day fixes the date
of every later leg through the stays: leave Thursday, two nights at Cape
Upstart, Bowen on Saturday. Each cell is one leg on its scheduled date. The
row's verdict is its worst cell. Dates beyond the forecast horizon are
`no_data`. Mate can call the tool again with different stays to compare
variants. Each call is its own card.

For each leg on its date, the tool:

- reads the wind (and wave, where the provider gives it) forecast at the
  start, each via point and the end, over the hours that part of the leg would
  be under way, and takes the worst;
- checks arrival against sunset using `sunTimes`, when the operator requires
  a daylight arrival;
- checks wind against tide using the fair-tide stream from
  `plan_tidal_departure`, when the leg has a known flood set.

Each cell is `go`, `marginal` or `no_go`, with **the limiting factor** in the
operator's units ("SE 25 kn gusting 32 on the nose, Bowen leg"). The tool gives
no combined score. A number like 7/10 hides which limit failed, and that is
what the skipper acts on.

**The limits depend on where the wind and sea come from.** Twenty-five knots
from astern is a fast, easy run. Fifteen knots on the nose is a wet, slow one.
The limits are therefore set per sector of true wind angle relative to the
course over ground: **ahead** (0-60° off the bow), **abeam** (60-120°)
and **astern** (120-180°). Each sector has a marginal and a no-go threshold
for sustained wind, gust and significant wave height. The wave height uses the
wave direction where the forecast gives it, or the wind sector where it does
not. The angle is taken per segment of the leg, from that segment's bearing,
so a leg that turns the corner at a headland is judged on both courses. There
is also a wind-against-tide limit, and a daylight-arrival toggle.

**The limits live in Mate's settings,** as a Passage limits group. Nothing
ships with Pikorua's values. If no limits are set, the tool returns an error
naming what is missing, and Mate says so. It does not assess against invented
defaults. A sector left blank is an error for any leg segment that falls in
it, not a pass.

**Missing data is shown, not filled.** A leg with no forecast for its date
shows `no data` in that cell, not `go`. The same applies to a missing wave
forecast, which never counts as calm seas.

### `show_telemetry_chart`

A generic read tool for any logged series, in two views:

- **Over time:** one to four series against time.
- **X against Y:** one series against another (amps against SOC, RPM against
  engine load, boat speed against RPM), each point one time bucket in which
  both were logged.

Arguments:

- **Series.** Each is a logged SignalK path, or a role the server resolves
  through the vessel view: the house bank's paths
  (`houseBankSocPath`), the alternator instances from
  [ADR 0180](0180-alternator-instances-are-a-vessel-setting.md), and the
  engines. An unassigned role is an error naming the role.
- **A date range.**
- **Optional comparison ranges,** each with a label ("before the regulator
  change", "after"). The operator gives Mate the ranges; Mate does not search
  for the date of the change. Over time, each range is a panel or an overlay.
  X against Y, each range is a separately coloured set of points, which is how a
  before/after comparison stays like for like.
- **The view:** `time`, or `xy` with the series for each axis.

The server checks that each series is actually logged in each range, and
returns an error naming any that is not. Values are converted from SignalK's
SI units into the operator's display units (Hz to RPM, ratio to %, K to
°C) with the existing quantity conversions. Series are assigned to axes by
unit. More than two units on a time chart is an error, not a chart with a
hidden axis. The tool's result to the model is a short summary: per series
and per range, the min, mean and max; for `xy`, a fitted line or binned means
for each range. That keeps the prose and the chart in agreement.

**The chart's data comes from a new endpoint**,
`GET /api/telemetry/series?path=…&path=…&from=…&to=…`. It returns bucketed
means for each path from the same Influx query as `get_path_history`, but
with its own bucket ladder. The tool's ladder caps a result at about 60 buckets
to keep the model's context small. The chart is never sent to the model, so it
targets about 400 buckets. A week at 1 h buckets keeps the daily charge cycle
that 12 h buckets would average away. Each series reports whether it was found
in the range, so a chart whose date control moves back past the start of one
series' logging still draws the others and says which one is missing. Only a
request where no series has any data is an error. The range is clamped at
now, so the live edge is not drawn as a sensor gap. The endpoint needs InfluxDB
and answers 503 without it, and the card shows that error rather than an empty
chart. The existing `/api/telemetry/history` keeps its fixed windows for
tiles.

**The card** stores the reference (series, ranges, view), not the samples. It
draws the view it was given, with a toggle between `time` and `xy` when both
make sense, and up to two y-axes on the time view (as `sea-state-chart.tsx`
already does). It has a date-range control that re-fetches without going back
through the model.

### Prompt

One line for each tool in its description: the card is attached, so do not
repeat its contents as a table; describe what it shows and what to do about
it. Spoken answers and summary notes ignore cards, so Mate's prose must stand
alone without them.

## Alternatives considered

- **OpenUI Lang.** Rejected for the reasons in Context. Reconsider only if
  Mate needs layouts the model must choose, and then with layout-only
  components and data passed by reference.
- **A Helmcentral markdown fence** (```` ```mate-chart ````). Cheap, but the
  model still types the numbers, and a fence that fails to parse has to be
  either dropped or shown raw.
- **A new `card` SSE event.** It adds a stream event for something that is
  only complete when the tool returns. Cards arriving on `message`, like
  proposals, means one path for live and reopened conversations.
- **A per-day score.** Rejected above: it hides the limit that failed.
- **A chart tool per pairing** (an "alternator chart", an "engine chart").
  Rejected: the pairings worth plotting are open-ended, and the comparison
  logic is the same for all of them.
- **Fixed sectors with no headwind limit,** or a single wind limit. Rejected:
  a single limit is either too strict downwind or too loose upwind.
- **Storing chart samples on the card.** Rejected: it fixes the range, and a
  week of four series is a lot of rows to keep for a picture the endpoint can
  redraw.
- **Recomputing the passage card on reopen.** Rejected: the card and the
  prose next to it would disagree.

## Consequences

- Adding a card is a struct, a `collect`, a fixture, a decoder case and a
  component. Neither the stream nor the prompt structure changes.
- The cards work on any model that can call tools, because the model never
  writes them.
- Passage limits become a new group in Mate's settings: six thresholds for
  each of three sectors, plus the tide and daylight limits. That is a lot to
  fill in, so the settings page shows them as a sector table rather than a
  long form. Until the operator fills it in,
  `assess_passage_window` refuses to run.
- The first-run setup list (ADR 0190) gains its first **optional** step,
  "Passage limits", shown while Mate is on. An optional step that is still open
  does not stop setup counting as complete.
- `/api/telemetry/series` is a second history endpoint. It is not a
  replacement for the tile endpoint, which keeps fixed windows for polling.
- Conversation search, recall and summary notes see only the prose. A card's
  contents are not searchable.
