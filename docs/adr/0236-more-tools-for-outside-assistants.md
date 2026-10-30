# ADR 0236: More Tools For Outside Assistants

## Status

Accepted (2026-10-31). Amends the tool table in ADR 0231. Leaves ADR 0232's
Funnel route and ADR 0233's sign-in as they were.

## Context

ADR 0231 kept eleven Mate tools out of the MCP server because their results
read correctly only with rules that lived in Mate's prompt: forecasts, tides,
places, routing, passage estimates, nearby vessels and the planned stay. An
outside assistant never sees that prompt, so it would have quoted a straight
line as a passage distance or called a "go" a recommendation.

## Decision

Each rule moves into the tool, so Mate and an outside assistant read the same
words. A rule that is the same on every call goes in the tool's description,
which Mate's prompt cache holds. A rule that depends on the result goes in a
`reading` string in that result. The MCP surface has no description of its own
for these tools.

`assess_passage_window` builds a card only where a run takes one (Mate's chat).
Over MCP there is no card sink, so the tool skips the card and says nothing
about one, and the grid figures stay in the result. Its description says "when
Helmcentral shows a card" so that it is true on both paths.

Three tools stay off until the operator opts in per token, because they are
passage decisions: `estimate_passage`, `plan_tidal_departure` and
`assess_passage_window`. The other eight start on for new tokens.

None of the eleven calls OpenRouter or spends the key. `find_places` reads the
place-names plugin, `get_wind_forecast` and `get_tides` use the weather and tide
providers, and the rest use the offline coastline, the boat's log or its own
records.

A token's tool list is written when the token is made. The Settings API seeds it
from the default set at that moment and nothing re-reads the default later, so
an existing token keeps its list and the new tools are simply absent from it.
Only a token made after this change starts with the eight.

| Tool | Offered | Rule | Where it moved |
| --- | --- | --- | --- |
| `read_help` | default on | A page has no link: name it in plain words. An unknown page returns the valid ids. Pages describe Helmcentral, not the boat. | Description. The prompt keeps its sentence about document links, which is about Mate's citations. |
| `find_places` | default on | Say which candidate you chose. A name that will not resolve is said so, and the nearest resolved feature stands in, told to the operator. | Description. |
| `anchorage_shelter` | default on | Shelter is inferred from charted shoreline, not surveyed. Compare sheltered arcs with the wind direction. | Description, plus a `reading` that dates the coastline. Removed from the prompt. |
| `get_wind_forecast` | default on | A model forecast, not observed. Dates are local to the place. It reaches about ten days. | Description. |
| `get_tides` | default on | Predicted, not observed. Heights are on the tide-table datum, not charted depth. The station can be some way off. | Description, plus a `reading` that names the station, its distance and true bearing from the place asked about, and its zone. |
| `route_distance` | default on | Routed over water. The straight line is never a passage distance, and there is no fallback to it. | Description. |
| `get_nearby_vessels` | default on | AIS targets only. Names are broadcast and can be wrong. CPA is the closest point of approach. `in_range_since` is a lower bound. | Description. The prompt keeps its sentence about the lower bound, which a test pins. |
| `get_planned_stay` | default on | Dates are local to the place. The forecast reaches about ten days. | Description. Removed from the prompt. |
| `estimate_passage` | offered off | Routed over water, no straight-line fallback. Time and fuel come from the boat's own log, so head seas add to both. | Description. |
| `plan_tidal_departure` | offered off | Label the set direction by its source. The stream's rate is not modelled. Check against the stream atlas. Ask for the course and hours if there is no estimate. | Description, plus a `reading` for wind against tide, which depends on the forecast. |
| `assess_passage_window` | offered off | The limits are no-go limits only. A go is not a go-ahead. `no_data` is not a good day. | Description. |

Each rule has one home: the description when it is constant, the reading when
it depends on the figures, never both. The prompt's routing, tidal-departure
and anchorage paragraphs now say only when to call the tool, which arguments to
pass and what to do on an error.

Rules that are about how Mate behaves stay in the prompt: when to fetch a
forecast, how to shape a briefing, and what to say about passage limits being
set.

## Consequences

- The descriptions are longer, and Mate's prompt cache holds them. Only the
  tide, shelter and tidal-departure results carry a short reading.
- A rule is now written once. A change to it reaches Mate and every outside
  assistant together.
- Settings lists the new tools by plain name and needs no change of its own,
  because it draws the list from the server.
