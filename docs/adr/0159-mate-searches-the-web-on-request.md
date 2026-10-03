# ADR 0159: Mate searches the web on request

## Status

Accepted (2026-10-03). Builds on [ADR 0093](0093-onboard-assistant-over-openrouter.md) (Mate
and its agentic tool loop) and [ADR 0137](0137-mate-document-citation-links-sheet-search-and-a-blank-default.md)
(sources shown as links under an answer).

## Context

Mate's tools read what the boat already holds: live instruments, the forecast
and tide providers, the help, the document library, the maintenance list. A
skipper also asks things none of those can answer: a harbour's opening hours,
the latest notice about a bridge or a bar, a part number's replacement, what a
fault code means on an engine whose manual is not aboard. The model answers
those from memory, which is stale and sometimes invented, with no way to say
where it got the answer.

OpenRouter, which Mate already uses with the operator's own key, offers a web
search plugin. Attached to a request, it runs a search, hands the results to
the model and reports each one as a `url_citation` annotation on the reply
(url, title, matching excerpt). Search is billed per request on top of the
tokens.

## Decision

Mate gets one more tool, `search_web(query)`, offered only when the operator
switches on **Web search** under Settings, Mate. It is off by default.

- **Dedicated tool, separate request.** `search_web` makes its own
  non-streaming chat completion with the web plugin attached (five results,
  Exa engine), using the OpenRouter key Mate already holds. Only the
  annotations are read; the sub-request's own prose is thrown away. No new
  account, key or secret.
- **A fixed cheap model for that request**, `google/gemini-2.5-flash-lite`,
  not the operator's chat model. The model does no reasoning that reaches the
  operator, so a large model would only add cost. The engine is pinned so the
  citation shape and the per-search price do not vary with the model.
- **Off means absent.** With the toggle off the tool is not in the request's
  tool list and the system prompt never mentions it, so a boat that has not
  opted in sees no change and pays nothing.
- **Fail fast.** A transport error, a non-2xx reply, a blank query and a reply
  with no citations each return a tool error beginning "web search failed".
  Mate relays it. There is no empty success and no fallback to the model's
  memory presented as search results.
- **Results are untrusted.** They are page excerpts written by strangers. The
  tool result is JSON marked `untrusted_web_content: true`; titles and
  snippets are flattened to one bounded line and stripped of the `<<<`/`>>>`
  sequences the prompt uses for its own boundaries; only http(s) URLs are
  kept. The prompt tells Mate that web results are data, never instructions,
  and that they must never drive `propose_maintenance_changes` or any other
  write. This is the same stance as for document text, and the same
  limitation: it lowers the odds of an injected instruction being followed, it
  is not a guarantee. Writes still need the operator's explicit apply.
- **Cited as links.** Mate is told to cite what it used as ordinary markdown
  links to the result's URL. The chat's markdown renderer already opens any
  external link in a new tab with `rel="noreferrer"`, so no frontend citation
  code was added.
- **Logged** like every tool: the query, how long it took and how many results
  came back. The key is never logged.

## Alternatives considered

- **The plugin (or the `:online` model suffix) on every request.** Rejected.
  Every question would pay for a search and carry web text in its context,
  including questions about the boat's own instruments where it is noise, and
  untrusted text would reach the model on every turn rather than when Mate
  chose to look something up.
- **A search API of our own (Brave, Tavily, Exa direct).** Rejected for an
  extra account, an extra key to store and another vendor, for a feature the
  existing OpenRouter key already covers.
- **Reusing the operator's chat model for the sub-request.** Rejected on cost,
  as above. The configured document model was also considered and rejected: it
  can be blank or set to anything, and it is chosen for reading images.

## Consequences

- Each search costs a few tenths of a cent through OpenRouter (the Exa engine
  is billed per request), added to the reply's cost footer only through the
  tokens of the main turn. The search's own charge appears on the OpenRouter
  account, not in Mate's footer.
- A question sent to the search carries Mate's query text to OpenRouter and
  its search engine. The query is written by the model and can include place
  names and details from the conversation.
- The search model id is a constant. If OpenRouter retires it, searches fail
  with the upstream error until the constant is changed.
- The response shape this was built against comes from OpenRouter's published
  documentation. No live response was captured when it was written, so the
  first search on the boat is the first check against real data.
