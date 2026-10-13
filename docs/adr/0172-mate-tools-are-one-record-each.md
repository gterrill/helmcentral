# ADR 0172: Each Mate tool is one record in a registry

## Status

Accepted (2026-10-13). Supersedes the wording "every Mate tool is read-only" in
[ADR 0145](0145-mate-reads-the-maintenance-list.md), [ADR 0146](0146-mate-proposes-the-operator-applies.md)
and [ADR 0158](0158-mate-changes-records-through-changesets.md). Builds on the "off means absent" rule of
[ADR 0159](0159-mate-searches-the-web-on-request.md). Those ADRs are left as written.

## Context

Adding a Mate tool touched five or six places in `backend/`: the definitions slice, the
`execute` switch, the `describeAssistantToolCall` switch, a branch in `run` for any tool
whose result becomes a card, the web-search gate if it was conditional, and the prompt
prose that names it. Two of those were switches keyed on the tool's name as a string, and
three tools had a name constant while the rest did not, so a renamed tool compiled and
failed at run time.

The comment over `runToolRound` had drifted from the code. It said every tool only reads.
`start_watch` starts a watch and `fill_form` writes a draft row, and neither was in the
list. The comment is where the next author learns what a tool may do under concurrency,
so a wrong one is a hazard.

## Decision

1. **One record per tool.** `mateTool` has `name`, `function` (description and parameter
   schema), `execute`, `describe`, and two optional fields, `collect` and `available`.
   `mateTools` is one slice, in the order the model is offered the tools, with `search_web`
   last. The record for a tool sits in the same file as its handler. The registry stamps
   `name` into the function definition, so the name is written once.
2. **Everything derives from the registry.** The definitions offered to the model, the
   `execute` dispatch, the status line shown while a call runs, and the post-round
   collection are all lookups or filters over `mateTools`. An unknown name still returns
   `unknown tool "x"`, and `describe` falls back to "Running x…" only for a name that is
   not registered.
3. **`collect` replaces the name checks in `run`.** `propose_changes` and `fill_form`
   each set `collect`, which lifts a proposal or a form draft out of the result into the
   reply. `run` calls it for any tool that has one. A result it cannot read is still an
   error.
4. **`available` makes "off means absent" one rule.** `search_web` is available only when
   the turn's tool dependencies carry a web search function, which the operator's setting
   wires. One check, `availableTo`, against those same dependencies decides the tool list
   offered to the model (`assistantToolDefinitions`, reached through the executor the run
   will call), whether the prompt carries the `search_web` paragraph, and whether `execute`
   accepts a call. The three cannot disagree, and the runner keeps no web search flag of
   its own.
5. **The concurrency rule, restated.** All calls in a round run concurrently (semaphore of
   four, at most twenty per round). A tool that writes must be safe under that by itself:
   `start_watch` through its one-watch-per-conversation rule, `fill_form` by writing only
   its own draft row, nothing added to the document library until the operator saves.
   `propose_changes` writes nothing, because its dry run is rolled back. "Every Mate tool
   is read-only" was never the rule; this is.
6. **A test keeps the prose honest.** Every tool-shaped name in the assembled system
   prompt, with web search on and off, and in the files that name tools in their text,
   must be a registered tool.

## Consequences

- A new tool is one record and one handler, and the prompt test fails if the prose names
  it before it exists or after it is renamed.
- `assistantToolDeps` stays one struct, so production wiring and the tests' fakes are
  unchanged and `deps.execute(ctx, name, args)` keeps its signature.
- Name constants remain only where a handler's error text uses them. The record's `name`
  is the one the registry reads.
- The refusal for an unavailable tool is one shared sentence ("search_web is not
  available: it is switched off in Settings") instead of search_web's own.

## Rejected alternatives

- **A Go interface per tool.** There is no second implementation of any tool and no caller
  that holds one abstractly. It would add a type per tool for the same lookup.
- **An `Effect` enum (read, write, none) on the record.** Nothing would read it. The
  concurrency rule is a thing for the author of a writing tool, not a branch in the
  runner, so the field fails the deletion test.
- **A `PromptHint` fragment per tool.** The prompt's tool guidance is cross-tool ("find a
  place, then ask for its tides") and sits in the cached prefix, where reordering
  fragments would change bytes and break the cache. The name test covers the real risk.
- **Narrow per-tool dependencies.** Giving each handler only the deps it uses would
  rewrite every fake in the tests for no change in behaviour.
