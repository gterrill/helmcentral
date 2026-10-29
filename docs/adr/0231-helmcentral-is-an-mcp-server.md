# ADR 0231: Helmcentral Is An MCP Server

## Status

Accepted. Adds a credential type to ADR 0111's threat model (see the addendum
there). Builds on ADR 0172 (the Mate tool registry) and ADR 0184 (store
transactions).

## Context

An operator wants to ask the boat questions from tools they already use, such
as Claude Code on a laptop aboard, without opening Mate. Two directions were
on the table:

1. Helmcentral is the MCP server, and an outside assistant connects to it.
2. Mate consumes upstream SignalK and InfluxDB MCP servers.

The upstream servers sit below Helmcentral's semantic layer. They return raw
SignalK paths or raw Flux and SQL rows. None of them knows which battery is
the house bank, what the operator calls the port engine, which alarm the card
shows and in what words, what a passage is, or what is in the maintenance log.
ADR 0175 already forbids handing a model raw paths to guess from. The InfluxDB
servers also target InfluxDB 3, where Helmcentral talks Flux to InfluxDB 2, and
they would make InfluxDB the path for history against the rule that InfluxDB
stays optional.

Mate's safeguards live in its prompt (`backend/assistant_prompt.go`). An
outside assistant never sees that prompt, so a tool result that is only read
correctly because of a prompt rule is a trap there.

## Decision

### Helmcentral is the server, read-only

Helmcentral serves MCP over Streamable HTTP at `/mcp`, on the backend's
existing port, with the official Go SDK (`github.com/modelcontextprotocol/go-sdk`
v1.8.0). The SDK negotiates the 2026-07-28 and 2025-11-25 revisions. The server
runs stateless: every request is authenticated and gets a server built from
that token's tool list, which suits 2026-07-28 and costs 2025-11-25 clients
nothing. `tools/list` is in name order. The `Origin` header is checked: a
request carrying an Origin other than the app's own, or one listed in
`CORS_ALLOWED_ORIGINS`, is refused with 403. A native client sends no Origin.

It reuses the Mate registry's `execute` functions and forks none of them. It
adds no write or proposal tool. It calls no model and works with Mate off.

Reach is the boat's LAN and Tailscale, the same listener as everything else.
Claude's phone and web connectors connect from Anthropic's cloud and need a
public address, so they do not work. That would need a public tunnel, which
steps outside ADR 0111's model and gets its own ADR.

### Per-client tokens, a new credential

- **Shape.** `hcmcp_` plus 32 random bytes, base64url. The store keeps the
  SHA-256 of it, a name, `created_at` and `last_used_at`. The secret is shown
  once, in the response to its creation.
- **Check.** `Authorization: Bearer`. The token's hash is looked up by index and
  the match is confirmed in constant time. `last_used_at` is written at most
  once a minute per token.
- **Tier.** Read only. A token can only call the tools on its own list.
- **Revocation.** One token at a time. Revoking deletes the row and its tool
  list, and the token stops working at once.
- **Separate from logins.** A user session never opens `/mcp`, and a token
  never opens `/api`. With `auth.mode: none`, `/mcp` is still closed.
- **Storage.** `helmcentral.sqlite`, in `backend/mcp_store.go`, in the ADR 0184
  style: one store mutex, and `RunTx` is the only way a write begins a
  transaction.
- **Managing them** is admin tier: `/api/outside-assistants/*`.

### No master switch

There is nothing to turn on. With no tokens, `/mcp` refuses every request with
401. Creating a token is the consent. A switch next to the tokens would be a
second thing to forget.

### One tool list per token

A new token's list is seeded from a curated default set. Settings switches
tools per token. `backend/mcp_tools.go` holds one decision per Mate tool, with
its reason (the table below), and the decision is enforced on the server:

- A tool that is **never** offered is not built into the server for any token.
  A token whose stored list names one (put there past the Settings API) still
  does not get it, and a call to it is refused as unknown.
- The Settings API rejects a never-offered name.
- A test fails when a Mate tool is added without a decision, so no tool reaches
  an outside assistant by accident.

Never offered, as a rule:

- Mate conversations and chat history.
- Anything that writes or proposes.
- Anything that calls OpenRouter and spends the operator's key (web search,
  semantic document search, watch review).
- The owner's contact details. They can be separated cleanly, so
  `get_vessel_particulars` stays on offer with the owner's name, phone, email,
  insurer, policy number and storm delegate removed from the result itself.

### Results that stand alone

Each candidate tool was read against Mate's prompt. Where the result is only
read correctly because of a prompt rule, the wording moved into the tool's
description on the MCP surface (the registry's descriptions are unchanged), or
the tool stays off. For example, `get_active_alarms` carries the rule that an
alarm's `investigate` list names what to read next and that the alarm's own
path is never used, and `list_passages` carries the rule that the logbook is the
record and passages are not counted from logged speed. The go/no-go reading of
`assess_passage_window` (limits set to no-go only) is a prompt rule, so that
tool stays off this cycle.

### The boat context

`backend/mcp_context.go` generates a Markdown document on every read from the
same readers as `get_vessel_status`: equipment items and roles, the units (the
operator's display units, and the units the boat declares in SignalK meta),
the particulars without contact details, the alarm rules, the tank warn and
alarm levels, and the boat's time zone. It is never hand-written, so it cannot
drift from the boat. A part that cannot be read says so in place and the
others still appear.

It is served three ways, because resources are application-driven and many
clients never read them unprompted:

- the resource `helmcentral://boat/context`, offered to a token that has
  `get_boat_context`;
- the tool `get_boat_context`;
- the server's instructions, which carry the rules that apply outside Mate:
  figures are in the operator's units as given, "missing" means unknown and is
  never guessed, alarms are reported as worded, never guess a SignalK path, and
  times are boat-local.

The context follows the token's tool list, so it is never a way round it. A
section drawn from a switchable tool is left out, saying nothing, when that
tool is off for the token:

- the particulars need `get_vessel_particulars`;
- the alarm rules need `list_alarm_rules`;
- the tank levels need `get_vessel_status`;
- the list of other equipment items needs `find_equipment`.

A call that carries no token list gets none of these sections.

The test is that an outside assistant given only this server answers "how's the
house bank" with the role's battery in the operator's units, or says why it
cannot.

The maintenance tools need the operator's local date, which Mate takes from the
browser. An outside assistant has no browser, so the date comes from the boat's
time zone (ADR 0230). With no position fix ever seen the tool fails and says so
rather than use UTC.

### Activity log

Every request is logged, off the request path through a buffered queue with one
writer. If the buffer ever fills, the entry is dropped with a loud log line and
a count, never silently. Each entry has the token's name, the tool or resource, the time, the
outcome (`ok`, `refused`, `tool_error` or `error`) and the duration. Rows are
kept 30 days and pruned daily. Settings shows them beside the tokens.

## Tool decisions

"Default on" tools start enabled on a new token. "Offered off" tools can be
switched on per token. "Never" tools cannot be offered. Tanks have no tool of
their own: their levels and alarm levels are in `get_vessel_status` and the boat
context.

| Tool | Offered | Why |
| --- | --- | --- |
| `get_vessel_status` | default on | One snapshot built from the operator's roles and items. Its result carries units and missing reasons itself. |
| `get_active_alarms` | default on | Titles and bodies are worded on the server for the watchkeeper. The investigate hint names tools that are also on offer. |
| `get_alarm_context` | default on | Reads the alarm log (raised and cleared history). Evidence wording moved into the description. |
| `get_path_history` | default on | Recorded history of one reading. Needs InfluxDB and says so when it is not configured. |
| `get_last_recorded` | default on | Finds the last recorded point per source. Result is self-describing. |
| `list_passages` | default on | Reads the Passages logbook. The wording about what the logbook is moved into the description. |
| `get_cruise_summary` | default on | Totals from the Passages logbook. The wording about what the logbook is moved into the description. |
| `list_maintenance` | default on | Status worked out on the server. The date used is the boat's local date, taken from the boat's time zone. |
| `get_maintenance_log` | default on | Reads the log. Self-describing. |
| `find_equipment` | default on | Gives the equipment ids the two maintenance tools need. Reads the equipment registry only. |
| `get_vessel_particulars` | default on | Length, beam, builder, hull and registration. The owner's name, phone, email, insurer, policy number and storm delegate are removed from the result, not just from the description. |
| `get_boat_context` | default on | Generated from the same readers as get_vessel_status. Tells an outside assistant how this boat is set up, in what units and in what time zone. |
| `check_signalk_paths` | offered off | Raw paths and sources. Useful for diagnosis, but it invites path guessing, so it starts off. |
| `list_alarm_rules` | offered off | Lists what the boat monitors. The description is rewritten so it does not invite proposals, which are never offered. |
| `search_conversations` | never | Mate's chat history is the operator's, not an outside assistant's. |
| `read_conversation` | never | Mate's chat history is the operator's, not an outside assistant's. |
| `propose_changes` | never | Writes and proposals are out of scope: this server is read-only. |
| `fill_form` | never | Drives Helmcentral's own forms; writes. |
| `inspect_form` | never | Reads a form on the operator's screen, which an outside assistant has no screen for. |
| `start_watch` | never | Creates a watch: a write. |
| `run_watch_now` | never | Runs a watch's model review, which spends the OpenRouter key. |
| `list_watches` | never | Watches and their reports are Mate's; they hold Mate's own conversation. |
| `get_watch_runs` | never | Watch reports are written by Mate's model; not offered this cycle. |
| `search_web` | never | Calls OpenRouter and spends the operator's key. |
| `search_documents` | never | Semantic search embeds the query through OpenRouter and spends the key, and what documents may leave the boat is undecided. |
| `read_document` | never | What documents may leave the boat is undecided. |
| `list_records` | never | Reaches every record type, the vessel record among them, and so the owner's contact details. |
| `get_record` | never | Reaches every record type, the vessel record among them, and so the owner's contact details. |
| `describe_record_type` | never | Exists to prepare proposals, which are never offered. |
| `show_tile` | never | Draws a card in Mate's chat; the result is a picture an outside assistant cannot show. |
| `show_telemetry_chart` | never | Draws a card in Mate's chat; use get_path_history for the figures. |
| `show_wind_rose` | never | Draws a card in Mate's chat. |
| `read_help` | never | Help pages are written for Mate's prompt rules (page ids, no links); not audited this cycle. |
| `find_places` | never | Needs Mate's rules on choosing and naming a candidate; not moved into the result yet. |
| `anchorage_shelter` | never | Estimate wording lives in Mate's prompt; not moved into the result yet. |
| `get_wind_forecast` | never | Dates are local to the place and the forecast horizon is a prompt rule; not moved into the result yet. |
| `get_tides` | never | Dates are local to the place; not moved into the result yet. |
| `estimate_passage` | never | Reading a passage estimate depends on Mate's routing and fuel rules; not moved into the result yet. |
| `route_distance` | never | Routing wording lives in Mate's prompt; not moved into the result yet. |
| `plan_tidal_departure` | never | Depends on estimate_passage's course in Mate's prompt; not moved into the result yet. |
| `assess_passage_window` | never | Go and no-go reading (limits set to no-go only) is a Mate prompt rule; not moved into the result yet. |
| `get_nearby_vessels` | never | Collision and mooring wording lives in Mate's prompt; not moved into the result yet. |
| `get_planned_stay` | never | Read with Mate's forecast rules; not moved into the result yet. |

## Alternatives rejected

- **Mate consumes upstream SignalK and InfluxDB MCP servers.** They are below
  the semantic layer, mask staleness (the signalk.org server stamps a reading
  with no timestamp with the current time), drop units, and in InfluxDB's case
  target a different database version. Raw SignalK for an operator who wants it
  can sit beside Helmcentral's server without Helmcentral proxying it.
- **A master switch.** See above.
- **A public tunnel now.** It leaves the trusted-LAN model and needs its own
  decision about transport security and abuse. Phone and web connectors wait
  for it.
- **Reusing user sessions or ADR 0111's tiers for MCP clients.** A program on a
  laptop should not hold a person's login, and a person's login should not open
  a program-facing endpoint.
- **Hand-written context.** It would drift from the boat the first time the
  operator changed a role.
- **Stateful sessions.** They add a second thing to authenticate and to expire,
  and the 2026-07-28 revision drops them.

## Consequences

- A tool added to the Mate registry needs a decision in `mcp_tools.go` or the
  tests fail.
- The activity log is in the same database as everything else, so back up
  `helmcentral.sqlite` as before. Tokens are in it, as hashes.
- The go-sdk is a new dependency.
- Phone and web Claude clients cannot connect until a tunnel is decided.
- Proposal tools, if ever offered, will need their own ADR and a token tier
  above read.
