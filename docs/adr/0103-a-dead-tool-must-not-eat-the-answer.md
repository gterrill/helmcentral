# ADR 0103: A Dead Tool Must Not Eat the Answer

## Status

Accepted. Extends ADR 0093 (the onboard assistant and its agentic loop) and
ADR 0100 (plugins declare their own settings).

## Context

On 2026-09-16 at 16:45 local, Mate was asked a straightforward cruising
question: Townsville now, is it worth pushing north to Cairns or turning
south before the cyclone season, and where is the good diving and the good
food. Eighty-eight seconds and $0.0071 later the operator got this back as
the answer:

```
<｜DSML｜ calls>
<｜DSML｜ invoke name="find_places">
<｜DSML｜ parameter name="query" string="true">Cairns</｜DSML｜ parameter>
</｜DSML｜ invoke>
</｜DSML｜ calls>
```

Three separate faults had to line up to produce that, and each one is worth
fixing on its own.

### The Overpass endpoint was dead, and nothing had pointed at the mirror

`find_places` resolves through whichever place-names plugin is configured,
which defaults to osm-overpass. That plugin's `overpass_url` was unset on
this machine, so it used its built-in default, the public
`overpass-api.de`. That host had been refusing this network since the
previous day. Seventeen `find_places` calls were dispatched over the run.
One returned data. The other sixteen failed, degrading as they went: HTTP
504, then 429 rate-limited, then flat `connection refused` against both of
the endpoint's addresses. `overpass.openstreetmap.fr`, already on the
plugin's allowed-hosts list and named in its own config-field help text as
the mirror to use, answered in 1.3 s throughout.

ADR 0100 moved `overpass_url` out of `config.json` and deleted the
`"${settings.<path>}"` reference syntax that carried it. The source tree
stopped shipping `osm-overpass.config.json` accordingly. The *built* copy
under `plugins/poi/` did not: `packaging/build-plugins.sh` copies a sidecar
when the source has one and does nothing when it doesn't, so a retired
sidecar is never removed from a directory that was built before it was
retired. The stale file sat there carrying `"${settings.overpass.url}"`,
a reference to a settings key that no longer exists, which resolved to an
empty string, which the plugin correctly reads as "unset" and answers with
the default endpoint. Every layer behaved as designed and the net effect
was a plugin quietly pinned to a server that had stopped answering.

### The loop had no failure budget, so the model spent the whole one

A failing tool call is handed back to the model as a `{"error": ...}`
tool-role message. That is right, and ADR 0093 argues for it: the model
should know a lookup failed and say so, rather than have the host decide on
its behalf what a missing forecast means.

What it does not tell the model is that the failure is permanent. DeepSeek
read sixteen consecutive `connection refused` results the way anyone reads
a flaky network, and tried again. It called `find_places` for Cairns once
in every one of the eight rounds. It reworded the query, added a `near_lat`
and `near_lon`, tried Magnetic Island, Hinchinbrook, Dunk, Lizard and
Hamilton. All of it went to the same dead host. `assistantMaxToolRounds` is
eight, and the run reached the cap having produced no prose at all. The two
calls that did work, a wind forecast and a tide lookup for Townsville, came
in the final round, by which point there was no round left to write an
answer in.

### The forced-final guard only recognises a structured tool call

Round eight is the forced final: `assistantMaxToolRounds` is reached, tools
are withdrawn, `tool_choice` is set to `"none"`, and the model is asked for
prose. ADR 0093 anticipated a model that ignores that instruction, and
`run` returns an error rather than a fabricated reply when it happens.

That guard keys off `len(choice.ToolCalls)`. DeepSeek honoured the wire
contract, returned no structured tool calls, and put the tool call in the
message body in its own markup dialect instead. Zero structured tool calls
is the loop's definition of a final answer, so the markup was accepted as
one, persisted to the conversation store, and rendered to the operator. The
guard written for exactly this situation never ran.

## Decision

### Three failures and a tool is done for that answer

`assistantMaxToolFailures` is 3. Failures are counted per tool name for the
whole run, across rounds, not per round. A tool that has failed three times
in one answer is not flaky, it is down, and the loop stops dispatching it.

Further calls to that tool are not executed. They are answered immediately
with the same `{"error": ...}` shape a real failure produces, carrying a
message that says the tool has failed three times, is unavailable for the
rest of this answer, must not be called again, and that the operator is to
be told the lookup was unavailable. The withheld call is logged and emits
its own status event, so a run that gave up on a tool reads differently in
the log from one that never called it.

This is not a fallback masking an upstream failure. The model is told
plainly that the tool is gone and instructed to say so, which is how the
failure reaches the operator. What the budget removes is the model's
ability to spend an entire answer's worth of rounds rediscovering the same
dead server. Sixteen failed calls to one host taught the loop nothing the
third one hadn't.

### A tool call in the message body is not an answer

`assistantTextToolCallMarker` scans a reply's content for the known text
tool-call dialects: DeepSeek's `<｜DSML｜` and its older
`<｜tool▁calls▁begin｜>`, the Anthropic-style `<function_calls>` and
`<invoke name="`, ChatML's `<tool_call>`, and Llama's `<|python_tag|>`. If a
final reply carries one, `run` returns an error naming the model and the
marker and pointing at Settings, instead of returning the reply. The error
reaches the browser as the SSE stream's `error` event and nothing is
written to the conversation store, so a broken reply no longer leaves a row
behind for the operator to scroll past later.

The check runs on every round that produces a final answer, not only the
forced one. A model doing this in round two is broken in the same way and
its output is no more usable.

It can false-positive on an answer that legitimately quotes one of those
markers. For a marine assistant whose tools return forecasts, tides and
operator manual pages, that is close to hypothetical, and the trade runs
strongly the other way: an operator who gets a loud error knows to change
the model, while an operator who gets a wall of markup has to work out for
themselves that the boat's assistant is broken rather than confused.

### The build prunes a retired sidecar

`packaging/build-plugins.sh` removes a sidecar from the output directory
when the source no longer ships it, instead of leaving whatever was there
from a previous build. Copy-if-present with no prune is how a file deleted
by ADR 0100 survived on disk for a fortnight.

## Rejected

**Aborting the run when a tool dies.** The fail-fast instinct says a dead
upstream should stop the work. It is wrong here: `find_places` is one of
five tools and the question was largely answerable without it. Aborting
would have turned a degraded answer into no answer, and the operator would
have learned less, not more. Telling the model the tool is gone and
requiring it to say so surfaces the failure in the place the operator is
actually looking.

**Retrying a dead tool against a different endpoint.** Silently failing
over to a mirror is precisely the masking fallback AGENTS.md rules out. The
mirror is an operator choice, it lives in the plugin's own config field
with help text explaining when to change it, and the operator is the one
who knows whether this boat's connection can reach a given host today.

**Counting failures per round rather than per run.** A per-round budget
would not have caught this at all. No round made more than four
`find_places` calls; the waste was eight rounds each making a couple.

**Detecting the markup by parsing it and executing the call it describes.**
Tempting, and wrong twice over. It would mean maintaining a parser per
model dialect, and it would reward exactly the behaviour that broke this
run by making a model that ignores the wire contract work anyway.

## Consequences

An answer can now end with Mate saying a place lookup was unavailable. That
is the intended outcome and a better one than eighty-eight seconds of
silence followed by markup.

A model that emits text tool calls fails loudly the first time rather than
on the eighth round, which makes it obvious during model selection rather
than during a passage.

The stale-sidecar prune only runs when plugins are rebuilt. An existing
deployment carrying `osm-overpass.config.json` keeps it until then; the
file is harmless, since an unresolvable reference reads as unset, but a
deployment that wants a mirror should set it in **Settings → Widgets →
Place names**, where the value has lived since ADR 0100.
