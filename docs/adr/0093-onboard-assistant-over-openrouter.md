# ADR 0093: Onboard Assistant Over OpenRouter

## Status
Accepted

Amends ADR 0065 §5, which set the house rules for the first outbound LLM call
(OpenRouter, key in the encrypted store read directly, off by default,
operator-triggered, non-blocking, failure visible) and closed with "if
outbound LLM calls later become a general capability, that earns its own
ADR." This is that ADR.

Amended by ADR 0105, which replaces §5's whole-message delivery with
token-by-token streaming (`delta`/`retract` events) and reverses the
"Token streaming in v1" rejection below.

## Context

At Hook Reef the crew wanted to know whether to visit Tongue Bay first, to
fish Hill Inlet for queenfish (best on a rising tide), or Blue Pearl Bay
first, to snorkel the last one to two hours of flood up to high slack. The
answer turns on forecast wind at each anchorage, since exposure decides
whether either is even comfortable to sit in overnight, and on the tide
times at each, since the two activities want opposite tide states. Getting
this right by hand means opening the forecast page twice, opening the tide
page twice, and doing the comparison in your head.

Helmcentral already had every input that answer needs: per-location weather
and wave providers behind a plugin registry, BOM tide predictions with a
station catalog, a forecast-warnings slot, live vessel state, and an
encrypted secrets store built exactly for holding a third-party API key
outside the process environment. What it had none of was a way to turn
"Tongue Bay" into a position, or any code path that made an outbound call to
a language model at all.

Design decisions were taken with the user before this document was written;
this ADR records them and the reasoning behind each. The plan is at
`~/.claude/plans/i-want-to-add-serialized-fox.md`.

## Decision

### 1. A core Go service, not a WASM plugin

Every other pluggable provider in this codebase (tides, weather, waves,
forecast warnings, POI) is a sandboxed WASM plugin. The assistant is not,
for three reasons specific to this feature. First, the WASM host enforces a
15-second timeout per call (`defaultWasmPluginTimeoutMS`, `wasm_plugin.go`);
a multi-round tool-calling conversation with a language model routinely
takes longer than that, and the timeout exists to bound a single provider
fetch, not an open-ended agentic loop. Second, the plugin host has no
streaming contract; every plugin call is call-in, result-out, and the
assistant's whole value to the operator is seeing "Looking up Tongue Bay…"
appear before the answer does, not staring at a blank panel for ten seconds.
Third, a WASM plugin only ever gets a secret through its own
`<name>.allowed_secrets.json` allowlist, and that file is exactly the
mechanism that makes an operator-downloaded, third-party `.wasm` binary a
plausible place for a key to leak from. Keeping the OpenRouter key inside
the core binary, which the operator did not download from a plugin author,
is a materially smaller exposure than handing it to a sandboxed but still
foreign binary.

### 2. A server-side agentic loop with three read-only tools

The backend runs the tool-calling loop itself (`assistant_run.go`), never
the browser: `find_places`, `get_wind_forecast` and `get_tides`
(`assistant_tools.go`), all read-only. The loop is capped in three ways.
Each individual chat completion is bounded to 60 seconds
(`openRouterCompletionTimeout`), independent of every other round, so one
slow response from the model never wedges the rest. The whole reply,
across every round of tool calls, is bounded to 3 minutes
(`assistantRunTimeout`), so a model that keeps calling tools slowly can
never hold a conversation open indefinitely. And the loop allows at most 8
rounds of tool calls; on the round after that, tools are withdrawn
(`tool_choice: "none"`) to force a plain-text answer. A model that still
returns a tool call on that forced round is treated as a bug in the model's
behaviour, not something to paper over, and the reply fails with an
explicit error rather than a fabricated answer.

A tool call that fails (a provider down, an unconfigured tide provider, an
Overpass rate limit) is not swallowed. Its error is handed back to the model
as ordinary tool content (`{"error": "..."}`), so the model can say "I
couldn't get tides for that position" in its own answer, and the same
failure is also emitted as a status line the operator sees live, so a dead
tool is visible even before the model gets a chance to react to it. Every
tool call is also logged server-side before and after it runs, with its
arguments, duration and result size (and, for `find_places`, the search
rung and result count it resolved to), so a failed or surprising lookup can
be diagnosed from the log after the fact rather than only from what the
operator happened to see live.

`get_wind_forecast` also takes an optional `course_deg`, the vessel's
intended course over ground: when given, every hourly row and day summary
carries the wind and wave angle relative to that course (`rel_wind`/
`rel_wave`, banded head through following) already computed. This exists
because the model was once asked about a 303°T course against a forecast SE
(135°T) wind and called it a beam-to-quarter wind, when it is in fact close
to dead astern; modular bearing subtraction is exactly the kind of
arithmetic a language model gets wrong silently, so it belongs on the host
side, not worked out by the model by eye.

### 3. A hand-rolled OpenRouter client, not go-openai

`backend/openrouter_client.go` talks to OpenRouter's chat-completions
endpoint directly rather than through `github.com/sashabaranov/go-openai`,
the client ADR 0065 §5 had named for its (separate, still-unbuilt) vision
use case. Four reasons drove this, not one: OpenRouter's `usage.include`
flag is what makes a response carry a per-reply dollar cost at all, and a
generic OpenAI client has no reason to know about an OpenRouter-specific
field; the `HTTP-Referer` and `X-Title` attribution headers OpenRouter asks
integrators to send have no home in a generic client either; several models
behind OpenRouter emit `content` as an array of parts instead of a plain
string, or `arguments` as a bare JSON object instead of the OpenAI-standard
JSON-encoded string, and both need tolerant decoding
(`openRouterContent`, `openRouterArguments`) a strict OpenAI client would
reject; and the backend stays pinned at Go 1.22, so pulling in a dependency
graph sized for the full OpenAI API surface costs more than it returns for
one endpoint. The whole client is one POST behind one `Do(req)` seam,
mirroring `overpassFetcher`'s shape so it can be faked the same way in
tests.

### 4. The key lives in `knownSecretKeys` only

`OPENROUTER_API_KEY` is appended to `knownSecretKeys` (`secrets_store.go`)
and read directly from the encrypted store with
`globalSecretsStore.Get("OPENROUTER_API_KEY")`
(`checkAssistantReadiness`, `assistant_handlers.go`). It is never added to
`coreEnvSecretKeys`, and so is never touched by `LoadIntoEnv`, which copies
that second, smaller list into the process environment for the handful of
things that still need an env var. Every WASM plugin's `${VAR}` config
expansion reads from the process environment; a key that never enters it
cannot leak through that path, the same reasoning ADR 0065 §5 already
applied to `WEATHERKIT_*`.

### 5. Progress as SSE on the POST response, not a second connection

`POST /api/assistant/conversations/:id/messages` streams `status`,
`message` and `error` events on the same response it also carries the
question in (`assistant_handlers.go` writes `text/event-stream` headers and
flushes after each `emit`). The browser's `EventSource` can only issue a
GET, and the request body here (the operator's question, which the server
combines with position, forecast and tide context) does not belong in a URL
query string. The frontend instead reads the POST response body as a
`ReadableStream` and frames it itself (`lib/sse-reader.ts`). Token-by-token
streaming of the final answer is deliberately out of scope for this
version; only whole-message and status events exist today, and streaming
the answer text itself is a follow-up.

### 6. SQLite `assistant.sqlite`, user and assistant rows only

`assistant_store.go` follows `alarm_log_store.go`'s template: a single
connection (`SetMaxOpenConns(1)`), `CREATE TABLE IF NOT EXISTS` at open, a
`conversations` table and a `messages` table with a `(conversation_id, seq)`
index. Only `user` and `assistant` role rows are ever written; the tool
round trips inside one turn's agentic loop are live SSE status only and are
never persisted. Reloading a conversation replays its stored turns as
history for the next completion request (`assistantHistoryMessages`); it
never sees the tool calls or tool results that produced them.

### 7. A top-level `assistant:` settings block, with standing notes

`Assistant{Enabled, Model, Notes}` sits at the top level of `settings.yaml`
alongside `influxdb:` and `mayara:`. `Notes` is free text the operator
writes, injected verbatim into every system prompt
(`buildAssistantSystemPrompt`), and it is where the queenfish and snorkel
tide rules and any other anchorage exposure knowledge live. A fixed rules
engine, something like a table of anchorage names mapped to wind exposure
and preferred tide state, was the alternative, and it was rejected: the
knowledge an operator wants the assistant to use is exactly the kind that
resists a schema (Tongue Bay is good on a rising tide "for queenfish",
which is itself conditional on the season and what the crew wants that day)
and would need one for every anchorage a boat ever visits. A model already
reads plain English; standing notes let the operator state exactly what
they know once, in their own words, rather than encoding it into a form the
model has to be taught to query in the first place. The identity line also
names the hull type (`anchor.hull_type`, e.g. "power catamaran") when
settings carries one it recognises, so the model reasons about comfort and
speed on the vessel it is actually aboard rather than a generic hull.

### 8. `find_places` sources waypoints, then a two-rung Overpass ladder, host does the geometry

`find_places` (`assistant_tools.go`) searches saved route waypoints by name
first. It then searches OpenStreetMap via Overpass in up to two rungs,
bbox-based rather than `around:`-based, instead of the single tagged name
regex the tool started with. Rung 1 is an exact, untagged name match
(`nwr["name"="<variant>"]`, no tag filter) over the vessel's full 100
nautical mile search radius, tried against a small set of capitalisation
variants of the query. Rung 2, the original four-clause tag-filtered name
regex, runs only when rung 1 comes back empty and no waypoint already
matched, and only over a tight 20 nautical mile box. The two rungs exist
because they were measured, live, against `overpass.openstreetmap.fr` (the
mirror this boat actually reaches; the main `overpass-api.de` refuses this
network) on 2026-09-11: an untagged exact-name match answers in about 1
second even at 100 nautical miles, because Overpass can use its name index
directly, while the tagged name regex costs a full unindexed scan of every
element in the box, answering in 2 to 5 seconds at 20 nautical miles and
timing out server-side well before 100. Adding this project's tag filter to
an exact-name query erased the win too, turning the same 1 second answer
into 20, so kind (`seamark:type`, `natural`, `place`, `leisure`, in that
priority order, or `feature` when none of the four is present) is derived
host-side instead, and nothing named is discarded for want of a recognised
tag. Distance and bearing for every candidate, waypoint or OSM feature
alike, are computed by the host, never returned by a source: this is ADR
0091's rule (a provider hands back raw features; the host alone computes
derived distance, bearing, ranking) applied here to two sources instead of
one. When a waypoint and an OSM result name the same feature within 500m,
the waypoint wins on dedupe, since it was added to the candidate list first
and an operator's own named waypoint is a more deliberate answer than
Overpass's guess at the same spot. A query that keeps a qualifier after a
comma ("Bona Bay, Gloucester Island") also tries the bare head as a rung 1
variant, and when rung 1 still finds nothing, one extra exact-name lookup
resolves the qualifier itself so rung 2's tight 20 nautical mile box can be
centred on it instead of on the vessel, which matters whenever the named
feature sits further from the vessel than that radius covers.

### 9. `time/tzdata` embedded for the static binary

The backend builds with `CGO_ENABLED=0` for a self-contained binary, which
means it carries no system zoneinfo database; `time.LoadLocation` fails for
every IANA zone name unless the binary embeds one itself. `get_tides` is the
first caller in this codebase that needs a station's own timezone (e.g.
`Australia/Brisbane`) resolved by name rather than derived from longitude,
so `backend/tzdata.go` adds `import _ "time/tzdata"`, embedding the zoneinfo
database in the binary. No other change to the Go version or build flags
was needed.

### 10. `react-markdown` + `remark-gfm`, no raw HTML

Assistant replies render through `react-markdown` with `remark-gfm`
(`assistant-markdown.tsx`), the first markdown renderer in this codebase.
GFM tables are the point: a comparison table is the natural shape of "which
anchorage" answers. `react-markdown` does not render raw HTML by default,
and `img` elements are explicitly disallowed on top of that, so a reply can
never make the browser fetch an attacker-controlled URL, or worse, as a side
effect of rendering the model's own output.

### 11. Fail fast, name the fix

Disabled, no key configured, or a blank model all produce the same shape of
answer: `GET /api/assistant/status` always returns 200 with a `problem`
string naming exactly what to do ("Enable it in Settings → Assistant", "Add
one in Settings → Assistant"), and `POST .../messages` refuses with 503 and
that same text rather than attempting a call. There is no default key, no
demo mode, and no silent fallback to a different model when the configured
one is blank or unreachable; a model id typo surfaces as a real error from
OpenRouter, not a quiet substitution.

### 12. `estimate_passage` from logged performance, not a polar

There is no polar diagram and no fuel curve anywhere in this repository, and
none is coming: hand-entering one, and keeping it current as the boat is
re-propped or re-engined, is exactly the kind of upkeep this project avoids
wherever the boat's own instruments already carry the answer. InfluxDB
(already configured for the depth and solar trend widgets) holds per-path
history for `navigation.speedOverGround`, every `propulsion.<id>.fuel.rate`
and `propulsion.<id>.revolutions`, all from SignalK. `estimate_passage`
(`assistant_tools.go`, pure maths in `assistant_performance.go`) joins the
last N days (7 to 365, default 90) of these at a 10-minute mean, converts
speed to knots and fuel rate to litres per hour summed across every
propulsion instance, buckets the result into whole-knot bands, and reads a
burn rate and rpm off the two bands nearest the requested speed by linear
interpolation, clamping past either end of what the boat has actually done.

A band needs at least 3 joined 10-minute samples before it is reported at
all; fewer than that is one or two short legs' worth of data pretending to
be a pattern. A timestamp only joins when every series has a point at it -
sog, every fuel-rate instance, and the reference rpm series - because a
quiet instrument at that instant makes the total burn unknown, not equal to
whatever the other instruments happened to report.

This is observed data, not a polar: it says what this boat actually did
across whatever wind, sea and load conditions occurred in the joined
window, not what it would do in any one named condition. A head-sea passage
and a following-sea passage both feed the same table today, and the tool's
note says so plainly rather than implying a controlled measurement. Splitting
the table by wind angle relative to the course is a real improvement and a
deliberate follow-up, once enough underway hours exist in more than one
angle to make separate bands meaningful rather than thinner ones.

Propulsion instance names ("port", "starboard") are discovered from the
SignalK snapshot the same way `fuelRatePaths` already does for the derived
fuel-economy figures (`derived_paths.go`), never hardcoded; a snapshot with
no propulsion tree yet (a dev backend with no SignalK) falls back to naming
this vessel's own two engines directly, and the tool result says so via
`instances_assumed` so the model and operator both know it is an assumption
rather than a discovery.

The fuel margin (`fuel_aboard_l`, `fuel_after_l`) comes from
`helmcentral.fuel.volume` (ADR 0084), the same derived path the Tanks tile
already reads its fuel-aboard figure from; both fields are left out of the
result entirely, rather than reported as zero, whenever that figure is not
currently defined.

### 13. OpenRouter Auto with explicit routing constraints

Mate now supports two model-selection modes in Settings: fixed model id, and
OpenRouter Auto (`openrouter/auto`).

For fixed mode, the settings UI does not present an unbounded free-text model
catalog as the primary choice. It loads the model list from OpenRouter with
`supported_parameters=tools` and offers that filtered set, because tool
calling is required for Mate's core path and models without tool support are a
known hard failure.

For Auto mode, the assistant settings include optional routing constraints:
`allowed_models`, `excluded_models`, and `cost_tier` (`low`, `medium`,
`high`, `xhigh`, `max`). These fields are normalized host-side (trimmed lists;
invalid cost tier dropped) and persisted in `settings.yaml`.

On request execution, those fields are forwarded to OpenRouter as an
auto-router plugin only when the configured model is an Auto variant
(`openrouter/auto` or `openrouter/auto-beta`). For non-Auto model ids, the
fields remain inert by design.

### Rejected

**A WASM plugin.** Covered in decision 1: the plugin timeout, the lack of a
streaming contract, and the `allowed_secrets` exfiltration surface all argue
against it.

**`go-openai`.** Covered in decision 3: no `usage.include` cost field, no
attribution headers, strict decoding that would reject real OpenRouter
responses, and a dependency footprint sized for an API surface this feature
does not use.

**A one-shot briefing endpoint.** A single non-conversational "plan my next
two days" call was considered instead of a chat. It cannot ask a follow-up
question, cannot be corrected ("actually we're leaving from Cid Harbour, not
here"), and throws away the tool-calling loop's biggest strength: the model
deciding for itself which anchorages need a second look.

**`EventSource` with the prompt in the query string.** The natural pairing
with SSE, but `EventSource` can only issue a GET, and a question plus
position plus forecast context does not belong in a URL: it would be
logged by any proxy in the path and capped by URL length limits well below
the 8000-character message cap this feature actually needs.

**WebSocket.** A full duplex channel for a strictly request-then-stream
exchange is unneeded complexity; SSE on a POST response gives the same
one-way progress feed with none of a WebSocket's connection lifecycle to
manage.

**Token streaming in v1.** Streaming the final answer character by character
would need the OpenRouter client to consume a streamed response itself,
which the tolerant-decoding work in decision 3 does not yet support, and
the status-line progress feed already tells the operator the assistant is
working. Recorded as a follow-up, not built here.

**Persisting tool transcripts.** Storing every tool call and its result
alongside the conversation was considered, for a fuller audit trail. It was
rejected for this version: the two answers a persisted transcript would
need to give correctly, "what did the model actually ask for" and "does
replaying this later still make sense once the underlying forecast has
changed", are both harder than they look, and nothing today needs either
answer. Only user and assistant turns are stored.

**The key in `coreEnvSecretKeys`.** Would put `OPENROUTER_API_KEY` into the
process environment, reachable by any WASM plugin's `${VAR}` expansion.
Covered in decision 4.

**A browser-side tool loop.** Running the agentic loop in the frontend and
having the browser call OpenRouter directly would mean the OpenRouter key
has to reach the browser at all, which defeats the entire point of keeping
it server-side and out of `coreEnvSecretKeys` in the first place.

## Consequences

- This is the first paid third-party API call in Helmcentral's request path.
  Every previous outbound call (weather, tides, POI, forecast warnings) is
  either free or the operator's own pre-paid subscription; asking the
  assistant a question now spends real money per reply.
- Cost is visible, not estimated: every assistant reply's footer shows the
  model, token count and dollar cost OpenRouter reported for that exact
  reply, summed across every tool round it took to produce.
- A readonly session (`auth.mode: signalk`, `readonly` role) can open the
  assistant panel and read past conversations, but cannot post a new message
  (`POST .../messages` is `tierWrite`): a readonly login cannot spend the
  operator's key.
- The feature needs internet to do anything. With `auth.mode: none` (this
  release's default) or no connectivity at all, the assistant simply reports
  its `problem` and the rest of the dashboard is unaffected; no path in this
  feature can degrade any other feature's behaviour.
- Every tool result is capped at 12,000 characters of JSON, trimmed by
  dropping the least essential rows (oldest hourly forecast entries, extra
  place candidates) rather than truncating raw bytes, so a large result
  degrades to a smaller, still-parseable one instead of invalid JSON.
- The full conversation history is re-sent as messages on every turn; there
  is no summarisation or trimming of old turns yet, so a very long-running
  conversation costs more per message as it grows.
- With `auth.mode: none`, anyone who can reach the dashboard on the LAN can
  spend the configured OpenRouter key by asking the assistant questions;
  this is the same trust model every other write-tier endpoint already
  has in that mode, applied to a feature that now has a literal dollar cost
  attached.

## Amendment 2026-09-25: the forced final round keeps its tools listed and tells the model why it's asking again

On v0.32.0, google/gemini-3.8-flash was asked when the exhaust temperature
and tank levels had stopped updating, ran eight perfectly sensible rounds
of `check_signalk_paths`, `get_last_recorded` and `get_path_history`, and
on the forced final round returned structured `tool_calls` again anyway.
ADR 0103's guard did exactly what it was built to do - it refused the tool
call rather than fabricating a reply, and the run failed with "the
assistant did not produce an answer within 8 tool rounds" - but the model
had everything it needed from the rounds already run, and the operator got
that error instead of an answer.

Two changes to the forced final round address this, neither of which this
ADR had recorded a reason not to do already.

First, the forced round's request now keeps `Tools: assistantToolDefinitions()`
populated and sets only `tool_choice: "none"`, rather than the original
`Tools: nil` this ADR shipped with. An empty tools list gives some
providers nothing to apply `"none"` to; the likely reason `tool_choice:
"none"` did not reliably stop Gemini from calling one anyway is that
OpenRouter, or the provider behind it, had no function-calling schema left
to put into "none" mode against.

Second, `run` (`assistant_run.go`) now appends a plain instruction to the
forced round's own copy of the system message's live suffix only - never
to any earlier round, never to the history the next turn is built from,
and never persisted or shown to the operator, since only the model's own
final answer (`reply.Content`) is ever written to the conversation store -
telling the model there is no more time to check anything further and it
must give its best answer now from what it has already found, naming
plainly anything it could not check. It goes into the system message
rather than a new trailing message for two reasons: this round's messages
end with the previous round's tool-role results, and a `user` message
immediately after a `tool` message is rejected outright by some providers
behind OpenRouter ("Unexpected role 'user' after role 'tool'"); and a
message shaped like a new user turn reads to the model as the operator
speaking, which is not who is actually asking for a wrap-up. For an
Anthropic model the instruction is appended to the second content block
only, leaving the first block - the one OpenRouter's provider-side cache
matches against - byte-for-byte unchanged.

ADR 0103's guard is otherwise unchanged: a model that still returns
`tool_calls` after both of these still fails the run with the same error
rather than falling back to whatever partial text it produced.

## Related

- ADR 0023 (encrypted secrets store): the mechanism `OPENROUTER_API_KEY`
  lives in, and the reasoning (keep it out of the process environment) this
  ADR extends to a second key.
- ADR 0037 (SignalK delta stream ingestion): the cached vessel state
  `collectAssistantPromptContext` reads for position, heading and wind,
  rather than a fresh SignalK call per question.
- ADR 0040 (SignalK delegated authentication): the `readonly`/`readwrite`
  role split this ADR's read-tier-status, write-tier-message-post split
  relies on.
- ADR 0056 (place names from OSM features, not the nearest town): the
  Overpass request-building and rate-limit handling `find_places` reuses via
  `postOverpassQuery`.
- ADR 0065 §5 (inventory records in the binary): the ADR this one amends,
  whose house rules for the first LLM call (OpenRouter, BYOK, off by
  default, key never in `coreEnvSecretKeys`) this ADR inherits and extends
  to a second, independent use case.
- ADR 0091 (points of interest as a plugin kind): the "host computes
  distance and bearing, a provider returns raw features" rule `find_places`
  applies to route waypoints and Overpass results alike.
- ADR 0092 (wall-display tiles): the most recent prior feature to add a new
  panel to the dashboard shell, whose extraction discipline `assistant-drawer.tsx`
  otherwise has no bearing on but whose panel-registration pattern
  (`PANEL_IDS`, `PANEL_NAV_ITEMS`) this feature's Assistant panel follows.
