# ADR 0259: A Mate Tool Says Who May Call It

## Status

Accepted (2026-11-05). Reverses the rejection of an `Effect` field in
[ADR 0172](0172-mate-tools-are-one-record-each.md). Builds on the outside
assistant's tool policy in [ADR 0231](0231-helmcentral-is-an-mcp-server.md).

## Context

Three audiences besides the operator in chat call Mate's tools: alarm reviews
(ADR 0237), watch reviews (ADR 0227) and outside assistants over MCP. Each kept
its own list, keyed on the tool's name.

- `alarmReviewReadTools` and `watchReviewReadTools` were two map literals of 13
  and 14 names. Nothing checked them against the registry, so a renamed read tool
  dropped out of a review without a sound, and a new write tool was kept out only
  because nobody had typed its name.
- `mcpToolPolicies` had a test that every tool has a decision. It was the one
  list that could not drift.

A review runs unattended, and an outside assistant sits off the boat. A tool
that writes, proposes or draws a card reaches either by one stray line.

ADR 0172 rejected an `Effect` enum because nothing would read it. Three callers
read it now, and each wants the same answer: may this caller use this tool?

## Decision

Each record in `mateTools` carries three fields beside its handler.

- **`effect`:** `effectReads` for a tool that reads the boat, Helmcentral's
  stores or the web and changes nothing. `effectActs` for one that proposes or
  makes a change, remembers, starts work or fills a form.
- **`audiences`:** a decision for each of `alarmReview`, `watchReview` and `mcp`,
  either granted or denied.
- **`drawsCard`:** the handler puts a card in the run's card sink. A card
  changes no state, so it is not an effect, but a review has a live sink and
  would carry the card into its reply.

`effect` and `audiences` start undecided, and undecided grants nothing. A tool
reaches an audience only when it is `effectReads` and that audience is granted.
A tool that acts therefore reaches none of them, even if someone grants it by
mistake. A tool that draws a card never reaches a review, granted or not.

An outside assistant may be granted a card tool. The MCP server clears the card
sink for every call, and the handlers then draw nothing: `anchorage_details`,
`marina_details` and `assess_passage_window` are offered on that footing. A test
pins the nil sink.

The review lists and the first half of the MCP policy derive from the records:

- `alarmReviewReadTool` and `watchReviewReadTool` ask the registry. A name
  that is not a registered tool reaches no one.
- `mcpOffered` needs the registry to grant `mcp` and the policy list to name the
  tool with a status other than `never`. A tool granted in only one of them is
  not offered.
- Both reviews filter their tool list and refuse a call through one pair of
  functions, `reviewAudienceDefinitions` and `reviewAudienceExecute`. The
  refusal is one sentence, naming the tool and the review ("an alarm review", "a
  watch review"); it does not claim the tool writes, because a read tool the
  review was not granted gets it too. Each review keeps its own submit tool and
  its own submission checks, which differ.
- `get_boat_context` is the one exception. It is not a Mate tool, so it has no
  record and no audiences, and the MCP policy list alone offers it. It reads the
  equipment, alarm rules, units and memories off the boat, from the same
  readers as `get_vessel_status`. A test asserts that it stays out of the
  registry, so it cannot gain a handler unseen, and that running it draws no
  card. It cannot prove the generator has no side effect, only that nothing in
  the registry's path reaches it.

`mcpToolPolicies` keeps what is prose or a Settings choice: the Settings name,
whether a tool starts on, the reason, and the wording an outside assistant reads.

## Who may call what

The sets are the ones that stood before this change.

- **Alarm review, 13 tools:** `get_vessel_status`, `get_active_alarms`,
  `get_alarm_context`, `list_alarm_rules`, `check_signalk_paths`,
  `get_last_recorded`, `get_path_history`, `list_passages`, `get_cruise_summary`,
  `get_wind_forecast`, `get_tides`, `get_vessel_particulars`, `get_planned_stay`.
- **Watch review:** the same 13 and `search_web`, which is on offer only while
  the operator's web search setting is on. An alarm review never searches the
  web.
- **Outside assistant:** the 13, plus the other read tools listed in ADR 0231 and
  ADR 0236. Whether each starts on stays in the policy list.

A characterisation test pins the three sets by name, so a change to who can call
what is a visible edit to a test and never a side effect.

## What fails, and when

- A new tool with no `effect` or any undecided audience fails
  `TestMateToolAudience_EveryToolHasDecidedEveryAudience`.
- A tool granted to an audience that does not only read, or one that adds a
  proposal or form draft to the reply through `collect`, fails
  `TestMateToolAudience_EveryGrantedToolOnlyReads`.
- A card tool granted to a review fails
  `TestMateToolAudience_NoCardToolIsGrantedToAReview`. A source scan,
  `TestMateToolAudience_DrawsCardMatchesTheSource`, checks that a file putting a
  card holds a record saying `drawsCard`, and the reverse. It matches a file, not
  a handler.
- A policy entry naming no registry tool, or a tool granted to MCP with no policy
  entry, panics when the tool list is built, so a rename stops the server and no
  token loses a tool quietly.
- A registry grant that the MCP policy contradicts fails
  `TestMateToolAudience_MCPPolicyAgreesWithRegistry`.
- A tool nobody decided is absent from every review and from MCP. A read tool
  that was meant to be there is missing, and the tests above say so.

The "only reads" check is the two fields agreeing with each other. It does not
prove a handler is free of side effects. That stays the author's claim, and a
reviewer's job.

## Rejected

- **A global list of audience names:** It is the structure that drifted.
- **Granting by default and denying by exception:** A write tool added next month
  would be reachable until someone remembered.
- **Moving the MCP wording into the records:** The descriptions are long prose
  for one audience, and the registry's description is the one Mate's chat reads.
- **One merged review executor:** The two submissions check different things, so
  only the filtering and the refusal are shared.
